// Package diff produces unified diffs for previews. It uses a simple LCS
// diff on the region between the common prefix and suffix, and falls back to
// a coarse (but still correct) replace-all diff for very large changed
// regions to bound memory use.
package diff

import (
	"fmt"
	"strings"

	"clip-patch/internal/patch"
)

// maxCells bounds the LCS table size (n*m) at about 16 MB of int32.
const maxCells = 4_000_000

type op struct {
	kind byte // ' ', '-', '+'
	text string
}

// Unified returns a unified diff of oldText -> newText, or "" if the line
// content is identical.
func Unified(path, oldText, newText string, context int) string {
	a := patch.SplitLines(oldText)
	b := patch.SplitLines(newText)
	ops := compute(a, b)

	changed := false
	for _, o := range ops {
		if o.kind != ' ' {
			changed = true
			break
		}
	}
	if !changed {
		return ""
	}

	// oldPos[k] / newPos[k]: number of old/new lines before ops[k].
	oldPos := make([]int, len(ops)+1)
	newPos := make([]int, len(ops)+1)
	for k, o := range ops {
		oldPos[k+1] = oldPos[k]
		newPos[k+1] = newPos[k]
		if o.kind != '+' {
			oldPos[k+1]++
		}
		if o.kind != '-' {
			newPos[k+1]++
		}
	}

	type rng struct{ s, e int }
	var ranges []rng
	for k, o := range ops {
		if o.kind == ' ' {
			continue
		}
		s := max(0, k-context)
		e := min(len(ops), k+context+1)
		if n := len(ranges); n > 0 && s <= ranges[n-1].e {
			if e > ranges[n-1].e {
				ranges[n-1].e = e
			}
		} else {
			ranges = append(ranges, rng{s, e})
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- a/%s\n+++ b/%s\n", path, path)
	for _, r := range ranges {
		oldCount := oldPos[r.e] - oldPos[r.s]
		newCount := newPos[r.e] - newPos[r.s]
		oldStart := oldPos[r.s] + 1
		if oldCount == 0 {
			oldStart = oldPos[r.s]
		}
		newStart := newPos[r.s] + 1
		if newCount == 0 {
			newStart = newPos[r.s]
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, o := range ops[r.s:r.e] {
			sb.WriteByte(o.kind)
			sb.WriteString(o.text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func compute(a, b []string) []op {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	midA := a[pre : len(a)-suf]
	midB := b[pre : len(b)-suf]

	ops := make([]op, 0, len(a)+len(b))
	for _, t := range a[:pre] {
		ops = append(ops, op{' ', t})
	}
	ops = append(ops, middle(midA, midB)...)
	for _, t := range a[len(a)-suf:] {
		ops = append(ops, op{' ', t})
	}
	return ops
}

func middle(a, b []string) []op {
	n, m := len(a), len(b)
	var ops []op
	if n == 0 || m == 0 || n*m > maxCells {
		for _, t := range a {
			ops = append(ops, op{'-', t})
		}
		for _, t := range b {
			ops = append(ops, op{'+', t})
		}
		return ops
	}
	// dp[i][j] = length of the LCS of a[i:] and b[j:].
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				dp[i][j] = dp[i+1][j+1] + 1
			case dp[i+1][j] >= dp[i][j+1]:
				dp[i][j] = dp[i+1][j]
			default:
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j]})
	}
	return ops
}
