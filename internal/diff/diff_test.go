package diff

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"clip-patch/internal/parser"
	"clip-patch/internal/patch"
)

func TestUnifiedIdentical(t *testing.T) {
	if got := Unified("f.txt", "a\nb\n", "a\nb\n", 3); got != "" {
		t.Fatalf("expected empty diff, got %q", got)
	}
}

func TestUnifiedSimpleChange(t *testing.T) {
	got := Unified("f.txt", "a\nb\nc\n", "a\nB\nc\n", 1)
	want := "--- a/f.txt\n+++ b/f.txt\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedAddAtEnd(t *testing.T) {
	got := Unified("f.txt", "a\n", "a\nb\n", 3)
	want := "--- a/f.txt\n+++ b/f.txt\n@@ -1,1 +1,2 @@\n a\n+b\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedDeleteAll(t *testing.T) {
	got := Unified("f.txt", "a\n", "", 3)
	want := "--- a/f.txt\n+++ b/f.txt\n@@ -1,1 +0,0 @@\n-a\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedSeparateHunks(t *testing.T) {
	var old, nw []string
	for i := 1; i <= 20; i++ {
		old = append(old, fmt.Sprintf("line%d", i))
		nw = append(nw, fmt.Sprintf("line%d", i))
	}
	nw[1] = "CHANGED2"
	nw[17] = "CHANGED18"
	got := Unified("f.txt", strings.Join(old, "\n")+"\n", strings.Join(nw, "\n")+"\n", 1)
	if n := strings.Count(got, "\n@@ -"); n != 2 {
		t.Fatalf("expected 2 hunks, got %d:\n%s", n, got)
	}
}

func TestUnifiedLargeChangeFallsBack(t *testing.T) {
	var old, nw strings.Builder
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&old, "old%d\n", i)
		fmt.Fprintf(&nw, "new%d\n", i)
	}
	got := Unified("big.txt", old.String(), nw.String(), 3)
	if !strings.Contains(got, "-old0\n") || !strings.Contains(got, "+new2999\n") {
		t.Fatalf("fallback diff is missing expected lines")
	}
}

// TestRoundTrip checks that for random inputs, applying the generated unified
// diff (after parsing it) to the old text yields exactly the new text.
func TestRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabet := []string{"a", "b", "c", "d", ""}
	randText := func(n int) string {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
			sb.WriteByte('\n')
		}
		return sb.String()
	}
	for iter := 0; iter < 300; iter++ {
		oldText := randText(rng.Intn(14))
		newText := randText(rng.Intn(14))
		d := Unified("f.txt", oldText, newText, 3)
		if d == "" {
			continue
		}
		p, err := parser.Parse(d, patch.FormatUnifiedDiff)
		if err != nil {
			t.Fatalf("iter %d: generated diff does not parse: %v\nold=%q\nnew=%q\ndiff:\n%s", iter, err, oldText, newText, d)
		}
		got, _, err := patch.ApplyHunks("f.txt", oldText, p.Files[0].Hunks)
		if err != nil {
			t.Fatalf("iter %d: generated diff does not apply: %v\nold=%q\nnew=%q\ndiff:\n%s", iter, err, oldText, newText, d)
		}
		if got != newText {
			t.Fatalf("iter %d: round trip mismatch\nold=%q\nnew=%q\ngot=%q\ndiff:\n%s", iter, oldText, newText, got, d)
		}
	}
}
