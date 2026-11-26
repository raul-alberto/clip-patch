package patch

import (
	"strings"
	"testing"
)

func TestApplyEdits(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		edits   []Edit
		want    string
		wantErr string
	}{
		{
			name:  "single replacement keeps indentation",
			text:  "def greet():\n    print(\"Hello\")\n",
			edits: []Edit{{Search: "    print(\"Hello\")", Replace: "    print(\"Hi\")"}},
			want:  "def greet():\n    print(\"Hi\")\n",
		},
		{
			name: "multiple edits, order independent",
			text: "one\ntwo\nthree\n",
			edits: []Edit{
				{Search: "three", Replace: "3"},
				{Search: "one", Replace: "1"},
			},
			want: "1\ntwo\n3\n",
		},
		{
			name:  "empty replacement deletes whole lines",
			text:  "a\nfoo\nb\n",
			edits: []Edit{{Search: "foo", Replace: "", DeleteLines: true}},
			want:  "a\nb\n",
		},
		{
			name:  "empty replacement without DeleteLines leaves an empty line",
			text:  "a\nfoo\nb\n",
			edits: []Edit{{Search: "foo", Replace: ""}},
			want:  "a\n\nb\n",
		},
		{
			name:  "multiline replacement",
			text:  "start\nold1\nold2\nend\n",
			edits: []Edit{{Search: "old1\nold2", Replace: "new1\nnew2\nnew3"}},
			want:  "start\nnew1\nnew2\nnew3\nend\n",
		},
		{
			name:  "unicode",
			text:  "héllo wörld ✓\n",
			edits: []Edit{{Search: "wörld", Replace: "wörld 🌍"}},
			want:  "héllo wörld 🌍 ✓\n",
		},
		{
			name:  "tabs",
			text:  "func f() {\n\tx := 1\n}\n",
			edits: []Edit{{Search: "\tx := 1", Replace: "\tx := 2"}},
			want:  "func f() {\n\tx := 2\n}\n",
		},
		{
			name:    "search not found",
			text:    "abc\n",
			edits:   []Edit{{Search: "xyz", Replace: "q"}},
			wantErr: "does not match",
		},
		{
			name:    "whitespace differences are not tolerated",
			text:    "    x = 1\n",
			edits:   []Edit{{Search: "\tx = 1", Replace: "z"}},
			wantErr: "does not match",
		},
		{
			name:    "duplicate match is ambiguous",
			text:    "x = 1\ny = 2\nx = 1\n",
			edits:   []Edit{{Search: "x = 1", Replace: "x = 3"}},
			wantErr: "ambiguous",
		},
		{
			name:    "overlapping occurrences are ambiguous",
			text:    "aaa\n",
			edits:   []Edit{{Search: "aa", Replace: "b"}},
			wantErr: "ambiguous",
		},
		{
			name:    "empty search rejected",
			text:    "abc\n",
			edits:   []Edit{{Search: "", Replace: "x"}},
			wantErr: "Empty SEARCH",
		},
		{
			name: "overlapping edits conflict",
			text: "abcdef\n",
			edits: []Edit{
				{Search: "abcd", Replace: "X"},
				{Search: "cdef", Replace: "Y"},
			},
			wantErr: "overlap",
		},
		{
			name: "later edit cannot match text produced by an earlier edit",
			text: "a\n",
			edits: []Edit{
				{Search: "a", Replace: "b"},
				{Search: "b", Replace: "c"},
			},
			wantErr: "does not match",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ApplyEdits("f.txt", tc.text, tc.edits)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got result %q", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// hunk builds a Hunk from lines like " ctx", "-old", "+new".
func hunk(oldStart, oldCount, newStart, newCount int, lines ...string) Hunk {
	h := Hunk{OldStart: oldStart, OldCount: oldCount, NewStart: newStart, NewCount: newCount}
	for _, l := range lines {
		h.Lines = append(h.Lines, HunkLine{Op: l[0], Text: l[1:]})
	}
	return h
}

func TestApplyHunks(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		hunks        []Hunk
		want         string
		wantWarnings int
		wantErr      string
	}{
		{
			name: "add and remove lines",
			text: "def greet():\n    print(\"Hello\")\n",
			hunks: []Hunk{hunk(1, 2, 1, 3,
				" def greet():",
				"-    print(\"Hello\")",
				"+    print(\"Hello, World!\")",
				"+    print(\"Welcome!\")")},
			want: "def greet():\n    print(\"Hello, World!\")\n    print(\"Welcome!\")\n",
		},
		{
			name: "multiple hunks",
			text: "a\nb\nc\nd\ne\nf\n",
			hunks: []Hunk{
				hunk(1, 2, 1, 2, "-a", "+A", " b"),
				hunk(5, 2, 5, 2, " e", "-f", "+F"),
			},
			want: "A\nb\nc\nd\ne\nF\n",
		},
		{
			name:         "wrong line number but unique context applies with a warning",
			text:         "a\nb\nc\nd\n",
			hunks:        []Hunk{hunk(9, 2, 9, 2, " c", "-d", "+D")},
			want:         "a\nb\nc\nD\n",
			wantWarnings: 1,
		},
		{
			name:    "context not found",
			text:    "a\nb\n",
			hunks:   []Hunk{hunk(1, 2, 1, 2, " x", "-y", "+z")},
			wantErr: "does not match",
		},
		{
			name:    "wrong line number and ambiguous context",
			text:    "x\ny\nx\ny\n",
			hunks:   []Hunk{hunk(9, 2, 9, 2, " x", "-y", "+z")},
			wantErr: "several places",
		},
		{
			name:  "stated line disambiguates repeated context",
			text:  "x\ny\nx\ny\n",
			hunks: []Hunk{hunk(3, 2, 3, 2, " x", "-y", "+z")},
			want:  "x\ny\nx\nz\n",
		},
		{
			name:  "insert into empty file",
			text:  "",
			hunks: []Hunk{hunk(0, 0, 1, 2, "+a", "+b")},
			want:  "a\nb\n",
		},
		{
			name:  "missing final newline is preserved",
			text:  "a\nb",
			hunks: []Hunk{hunk(1, 2, 1, 2, "-a", "+x", " b")},
			want:  "x\nb",
		},
		{
			name:  "delete everything",
			text:  "a\n",
			hunks: []Hunk{hunk(1, 1, 0, 0, "-a")},
			want:  "",
		},
		{
			name: "overlapping hunks conflict",
			text: "a\nb\nc\n",
			hunks: []Hunk{
				hunk(1, 2, 1, 2, " a", "-b", "+B"),
				hunk(2, 2, 2, 2, " b", "-c", "+C"),
			},
			wantErr: "overlaps",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings, err := ApplyHunks("f.txt", tc.text, tc.hunks)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got result %q", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if len(warnings) != tc.wantWarnings {
				t.Fatalf("got %d warnings %v, want %d", len(warnings), warnings, tc.wantWarnings)
			}
		})
	}
}
