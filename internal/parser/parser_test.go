package parser

import (
	"strings"
	"testing"

	"clip-patch/internal/patch"
)

func join(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func mustParse(t *testing.T, input string, f patch.Format) *patch.Patch {
	t.Helper()
	p, err := Parse(input, f)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	return p
}

func expectErr(t *testing.T, input string, f patch.Format, substr string) {
	t.Helper()
	p, err := Parse(input, f)
	if err == nil {
		t.Fatalf("expected error containing %q, got patch %+v", substr, p)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err.Error(), substr)
	}
}

// ---------------- SEARCH / REPLACE ----------------

func TestSearchReplaceSingleWithProse(t *testing.T) {
	in := join(
		"Sure! Here is the change you asked for.",
		"",
		"### FILE: src/main.py",
		"",
		"<<<<<<< SEARCH",
		"def greet():",
		"    print(\"Hello\")",
		"=======",
		"def greet(name=\"World\"):",
		"    print(f\"Hello, {name}\")",
		">>>>>>> REPLACE",
		"",
		"Hope that helps!",
	)
	p := mustParse(t, in, patch.FormatAuto)
	if p.Format != patch.FormatSearchReplace || len(p.Files) != 1 {
		t.Fatalf("unexpected patch: %+v", p)
	}
	fp := p.Files[0]
	if fp.Path != "src/main.py" || len(fp.Edits) != 1 {
		t.Fatalf("unexpected file patch: %+v", fp)
	}
	e := fp.Edits[0]
	if e.Search != "def greet():\n    print(\"Hello\")" {
		t.Errorf("search = %q", e.Search)
	}
	if e.Replace != "def greet(name=\"World\"):\n    print(f\"Hello, {name}\")" {
		t.Errorf("replace = %q", e.Replace)
	}
	if e.DeleteLines {
		t.Errorf("DeleteLines should be false")
	}
}

func TestSearchReplaceMultipleBlocksAndFiles(t *testing.T) {
	in := join(
		"### FILE: a.py",
		"<<<<<<< SEARCH", "one", "=======", "1", ">>>>>>> REPLACE",
		"### FILE: b.py",
		"<<<<<<< SEARCH", "two", "=======", "2", ">>>>>>> REPLACE",
		"Some text between blocks.",
		"### FILE: a.py",
		"<<<<<<< SEARCH", "three", "=======", "3", ">>>>>>> REPLACE",
	)
	p := mustParse(t, in, patch.FormatSearchReplace)
	if len(p.Files) != 2 {
		t.Fatalf("want 2 files, got %d", len(p.Files))
	}
	if p.Files[0].Path != "a.py" || len(p.Files[0].Edits) != 2 {
		t.Errorf("a.py: %+v", p.Files[0])
	}
	if p.Files[1].Path != "b.py" || len(p.Files[1].Edits) != 1 {
		t.Errorf("b.py: %+v", p.Files[1])
	}
	if p.ChangeCount() != 3 {
		t.Errorf("ChangeCount = %d, want 3", p.ChangeCount())
	}
}

func TestSearchReplaceEmptyReplacement(t *testing.T) {
	in := join("### FILE: a.txt", "<<<<<<< SEARCH", "gone", "=======", ">>>>>>> REPLACE")
	e := mustParse(t, in, patch.FormatAuto).Files[0].Edits[0]
	if e.Replace != "" || !e.DeleteLines {
		t.Errorf("zero-line replacement should set DeleteLines: %+v", e)
	}

	in = join("### FILE: a.txt", "<<<<<<< SEARCH", "gone", "=======", "", ">>>>>>> REPLACE")
	e = mustParse(t, in, patch.FormatAuto).Files[0].Edits[0]
	if e.Replace != "" || e.DeleteLines {
		t.Errorf("one blank replacement line should not set DeleteLines: %+v", e)
	}
}

func TestSearchReplaceUnicodeAndTabs(t *testing.T) {
	in := join("### FILE: x.go", "<<<<<<< SEARCH", "\tmsg := \"héllo ✓\"", "=======", "\tmsg := \"wörld 🌍\"", ">>>>>>> REPLACE")
	e := mustParse(t, in, patch.FormatAuto).Files[0].Edits[0]
	if e.Search != "\tmsg := \"héllo ✓\"" || e.Replace != "\tmsg := \"wörld 🌍\"" {
		t.Errorf("unexpected edit: %+v", e)
	}
}

func TestSearchReplaceCRLFInputAndFences(t *testing.T) {
	in := "Here:\r\n```\r\n### FILE: a.txt\r\n<<<<<<< SEARCH\r\nold\r\n=======\r\nnew\r\n>>>>>>> REPLACE\r\n```\r\n"
	e := mustParse(t, in, patch.FormatAuto).Files[0].Edits[0]
	if e.Search != "old" || e.Replace != "new" {
		t.Errorf("unexpected edit: %+v", e)
	}
}

func TestSearchReplaceBackslashAndQuotedPath(t *testing.T) {
	in := join("### FILE: `src\\main.py`", "<<<<<<< SEARCH", "a", "=======", "b", ">>>>>>> REPLACE")
	if got := mustParse(t, in, patch.FormatAuto).Files[0].Path; got != "src/main.py" {
		t.Errorf("path = %q", got)
	}
}

func TestSearchReplaceErrors(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty search", join("### FILE: a", "<<<<<<< SEARCH", "=======", "x", ">>>>>>> REPLACE"), "empty SEARCH"},
		{"missing divider", join("### FILE: a", "<<<<<<< SEARCH", "x", ">>>>>>> REPLACE"), "before the '=======' divider"},
		{"missing replace terminator", join("### FILE: a", "<<<<<<< SEARCH", "x", "=======", "y"), "not terminated"},
		{"truncated in search", join("### FILE: a", "<<<<<<< SEARCH", "x"), "not terminated"},
		{"no file header", join("<<<<<<< SEARCH", "x", "=======", "y", ">>>>>>> REPLACE"), "FILE"},
		{"stray replace marker", join("### FILE: a", ">>>>>>> REPLACE"), "without a matching SEARCH"},
		{"nested search", join("### FILE: a", "<<<<<<< SEARCH", "x", "<<<<<<< SEARCH"), "inside an unfinished"},
		{"second divider", join("### FILE: a", "<<<<<<< SEARCH", "x", "=======", "y", "=======", "z", ">>>>>>> REPLACE"), "second '======='"},
		{"missing end after next block", join("### FILE: a", "<<<<<<< SEARCH", "x", "=======", "y", "<<<<<<< SEARCH"), "marker is missing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectErr(t, tc.in, patch.FormatSearchReplace, tc.want)
		})
	}
}

// ---------------- Unified diff ----------------

const simpleDiff = `diff --git a/src/main.py b/src/main.py
index 1111111..2222222 100644
--- a/src/main.py
+++ b/src/main.py
@@ -1,2 +1,3 @@
 def greet():
-    print("Hello")
+    print("Hello, World!")
+    print("Welcome!")
`

func TestUnifiedDiffSingleFile(t *testing.T) {
	p := mustParse(t, simpleDiff, patch.FormatAuto)
	if p.Format != patch.FormatUnifiedDiff || len(p.Files) != 1 {
		t.Fatalf("unexpected patch: %+v", p)
	}
	fp := p.Files[0]
	if fp.Path != "src/main.py" || len(fp.Hunks) != 1 {
		t.Fatalf("unexpected file patch: %+v", fp)
	}
	h := fp.Hunks[0]
	if h.OldStart != 1 || h.OldCount != 2 || h.NewStart != 1 || h.NewCount != 3 || len(h.Lines) != 4 {
		t.Errorf("unexpected hunk: %+v", h)
	}
	wantOps := []byte{' ', '-', '+', '+'}
	for i, l := range h.Lines {
		if l.Op != wantOps[i] {
			t.Errorf("line %d op = %q, want %q", i, l.Op, wantOps[i])
		}
	}
}

func TestUnifiedDiffMultipleFilesAndHunksWithProseAndFence(t *testing.T) {
	in := "Here is the diff:\n\n```diff\n" +
		"--- a/one.txt\n+++ b/one.txt\n" +
		"@@ -1,2 +1,2 @@\n a\n-b\n+B\n" +
		"@@ -10,2 +10,2 @@\n x\n-y\n+Y\n" +
		"--- a/two.txt\n+++ b/two.txt\n" +
		"@@ -1 +1 @@\n-old\n+new\n" +
		"```\n\nLet me know if that works.\n"
	p := mustParse(t, in, patch.FormatAuto)
	if len(p.Files) != 2 {
		t.Fatalf("want 2 files, got %d: %+v", len(p.Files), p)
	}
	if p.Files[0].Path != "one.txt" || len(p.Files[0].Hunks) != 2 {
		t.Errorf("one.txt: %+v", p.Files[0])
	}
	if p.Files[1].Path != "two.txt" || len(p.Files[1].Hunks) != 1 {
		t.Errorf("two.txt: %+v", p.Files[1])
	}
	h := p.Files[1].Hunks[0]
	if h.OldCount != 1 || h.NewCount != 1 {
		t.Errorf("implicit counts should default to 1: %+v", h)
	}
}

func TestUnifiedDiffBlankContextLine(t *testing.T) {
	// Some tools strip the single space of an empty context line.
	in := "--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n a\n\n-c\n+C\n"
	h := mustParse(t, in, patch.FormatUnifiedDiff).Files[0].Hunks[0]
	if len(h.Lines) != 4 || h.Lines[1].Op != ' ' || h.Lines[1].Text != "" {
		t.Errorf("unexpected hunk lines: %+v", h.Lines)
	}
}

func TestUnifiedDiffCRLFInput(t *testing.T) {
	in := strings.ReplaceAll(simpleDiff, "\n", "\r\n")
	p := mustParse(t, in, patch.FormatAuto)
	if p.Files[0].Hunks[0].Lines[0].Text != "def greet():" {
		t.Errorf("CR was not stripped: %q", p.Files[0].Hunks[0].Lines[0].Text)
	}
}

func TestUnifiedDiffEmptyFileInsertion(t *testing.T) {
	in := "--- a/empty.txt\n+++ b/empty.txt\n@@ -0,0 +1,2 @@\n+line1\n+line2\n"
	h := mustParse(t, in, patch.FormatUnifiedDiff).Files[0].Hunks[0]
	if h.OldStart != 0 || h.OldCount != 0 || h.NewCount != 2 {
		t.Errorf("unexpected hunk: %+v", h)
	}
}

func TestUnifiedDiffErrors(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"malformed hunk header", "--- a/f\n+++ b/f\n@@ -x +y @@\n a\n", "malformed hunk header"},
		{"truncated hunk", "--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n a\n-b\n", "truncated"},
		{"truncated headers", "diff --git a/f b/f\nindex 1..2 100644\n", "expected '--- '"},
		{"too many lines for header", "--- a/f\n+++ b/f\n@@ -1,1 +1,1 @@\n-a\n-b\n+c\n", "more lines than its @@ header"},
		{"extra lines after hunk", "--- a/f\n+++ b/f\n@@ -1,1 +1,1 @@\n-a\n+c\n+d\n", "outside any hunk"},
		{"garbage inside hunk", "--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n a\n?what\n", "unexpected line"},
		{"no hunks", "--- a/f\n+++ b/f\n", "no hunks"},
		{"binary (git)", "diff --git a/x.png b/x.png\nindex 1..2 100644\nBinary files a/x.png and b/x.png differ\n", "binary"},
		{"binary patch", "diff --git a/x.bin b/x.bin\nindex 1..2 100644\nGIT binary patch\nliteral 1\n", "binary"},
		{"binary (plain)", "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+b\nBinary files a/x and b/x differ\n", "binary"},
		{"new file", "diff --git a/n.txt b/n.txt\nnew file mode 100644\nindex 0..1\n--- /dev/null\n+++ b/n.txt\n@@ -0,0 +1 @@\n+hi\n", "creating new files"},
		{"new file without git header", "--- /dev/null\n+++ b/n.txt\n@@ -0,0 +1 @@\n+hi\n", "creating new files"},
		{"deleted file", "diff --git a/d.txt b/d.txt\ndeleted file mode 100644\nindex 1..0\n--- a/d.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n", "deleting files"},
		{"rename", "diff --git a/o.txt b/n.txt\nsimilarity index 100%\nrename from o.txt\nrename to n.txt\n", "renaming"},
		{"rename via headers", "--- a/o.txt\n+++ b/n.txt\n@@ -1 +1 @@\n-a\n+b\n", "renaming"},
		{"mode change", "diff --git a/f b/f\nold mode 100644\nnew mode 100755\n", "mode changes"},
		{"no newline marker", "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n", "No newline"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectErr(t, tc.in, patch.FormatUnifiedDiff, tc.want)
		})
	}
}

// ---------------- Detection ----------------

func TestAutoDetectAmbiguous(t *testing.T) {
	in := simpleDiff + "\n" + join("### FILE: a", "<<<<<<< SEARCH", "x", "=======", "y", ">>>>>>> REPLACE")
	expectErr(t, in, patch.FormatAuto, "Ambiguous patch format")
	// An explicit format resolves the ambiguity.
	mustParse(t, in, patch.FormatSearchReplace)
	mustParse(t, in, patch.FormatUnifiedDiff)
}

func TestNoPatchFound(t *testing.T) {
	expectErr(t, "just some prose, nothing to apply\n", patch.FormatAuto, "No supported patch found")
	expectErr(t, "   \n\n", patch.FormatAuto, "empty")
}

func TestParseFormat(t *testing.T) {
	for _, s := range []string{"auto", "search-replace", "unified-diff", " AUTO "} {
		if _, err := patch.ParseFormat(s); err != nil {
			t.Errorf("ParseFormat(%q): %v", s, err)
		}
	}
	if _, err := patch.ParseFormat("bogus"); err == nil {
		t.Error("expected error for bogus format")
	}
}

// ---------------- Fuzzing ----------------

// FuzzParse checks that the parser never panics and that every accepted patch
// is internally consistent, whatever the input.
func FuzzParse(f *testing.F) {
	f.Add(simpleDiff)
	f.Add(join("### FILE: a", "<<<<<<< SEARCH", "x", "=======", "y", ">>>>>>> REPLACE"))
	f.Add("--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+b\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, in string) {
		for _, format := range []patch.Format{patch.FormatAuto, patch.FormatSearchReplace, patch.FormatUnifiedDiff} {
			p, err := Parse(in, format)
			if err != nil {
				continue
			}
			if len(p.Files) == 0 {
				t.Fatalf("accepted patch with no files (format %s)", format)
			}
			for _, fp := range p.Files {
				if len(fp.Edits)+len(fp.Hunks) == 0 {
					t.Fatalf("accepted file %q with no changes", fp.Path)
				}
				for _, h := range fp.Hunks {
					old, nw := 0, 0
					for _, l := range h.Lines {
						if l.Op != '+' {
							old++
						}
						if l.Op != '-' {
							nw++
						}
					}
					if old != h.OldCount || nw != h.NewCount {
						t.Fatalf("hunk counts disagree with lines: %+v", h)
					}
				}
			}
		}
	})
}
