package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clip-patch/internal/clipboard"
)

type harness struct {
	t       *testing.T
	root    string
	backups string
	clip    string
	clipErr error
	stdin   string
	tty     bool   // whether a terminal is available for the prompt
	answer  string // what the user types at the prompt
}

func newHarness(t *testing.T, files map[string]string) *harness {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &harness{t: t, root: root, backups: filepath.Join(t.TempDir(), "backups")}
}

func (h *harness) run(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	env := Env{
		Name:      "clip-patch",
		Version:   "0.1.0",
		Stdin:     strings.NewReader(h.stdin),
		Stdout:    &out,
		Stderr:    &errb,
		Clipboard: clipboard.Static{Text: h.clip, Err: h.clipErr},
		Getwd:     func() (string, error) { return h.root, nil },
		DefaultBackupDir: func() (string, error) {
			return h.backups, nil
		},
	}
	if h.tty {
		answer := h.answer
		env.Interactive = func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(answer)), nil
		}
	}
	code = Run(args, env)
	return code, out.String(), errb.String()
}

func (h *harness) file(rel string) string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.root, filepath.FromSlash(rel)))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

func (h *harness) backupsCreated() bool {
	entries, err := os.ReadDir(h.backups)
	return err == nil && len(entries) > 0
}

func srText(file, search, replace string) string {
	return "Here you go:\n\n### FILE: " + file + "\n\n<<<<<<< SEARCH\n" + search + "\n=======\n" + replace + "\n>>>>>>> REPLACE\n\nDone.\n"
}

func contains(t *testing.T, what, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(strings.ToLower(s), strings.ToLower(sub)) {
			t.Errorf("%s does not contain %q:\n%s", what, sub, s)
		}
	}
}

func TestHelpAndVersion(t *testing.T) {
	h := newHarness(t, nil)
	code, out, _ := h.run("--help")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	contains(t, "help", out, "USAGE", "--dry-run", "--yes", "--format", "--root", "--backup-dir", "--stdin")

	code, out, _ = h.run("-h")
	if code != ExitOK || !strings.Contains(out, "USAGE") {
		t.Fatalf("-h: exit %d, out %q", code, out)
	}

	code, out, _ = h.run("--version")
	if code != ExitOK || !strings.Contains(out, "0.1.0") {
		t.Fatalf("--version: exit %d, out %q", code, out)
	}
}

func TestInvalidArguments(t *testing.T) {
	h := newHarness(t, nil)
	for _, args := range [][]string{
		{"--bogus"},
		{"--format", "nope"},
		{"unexpected-positional"},
		{"--dry-run", "--yes"},
		{"--color", "sometimes"},
		{"--root"},
	} {
		code, _, errOut := h.run(args...)
		if code != ExitUsage {
			t.Errorf("%v: exit %d, want %d", args, code, ExitUsage)
		}
		if !strings.Contains(errOut, "--help") {
			t.Errorf("%v: stderr should point to --help: %q", args, errOut)
		}
	}
}

func TestEmptyClipboard(t *testing.T) {
	h := newHarness(t, nil)
	h.clip = "  \n\t\n"
	code, _, errOut := h.run()
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", errOut, "clipboard is empty", "No files were modified")
}

func TestClipboardUnavailableSuggestsStdin(t *testing.T) {
	h := newHarness(t, nil)
	h.clipErr = errors.New("no display")
	code, _, errOut := h.run()
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", errOut, "could not read the clipboard", "--stdin", "No files were modified")
}

func TestStdinDryRun(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
	h.stdin = srText("a.txt", "world", "there")
	code, out, errOut := h.run("--stdin", "--dry-run")
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	contains(t, "stdout", out, "Patch detected: SEARCH / REPLACE", "[MODIFIED] a.txt", "-world", "+there", "Preview complete", "Dry run: no files were modified")
	if h.file("a.txt") != "hello\nworld\n" {
		t.Fatal("--dry-run modified a file")
	}
	if h.backupsCreated() {
		t.Fatal("--dry-run created a backup")
	}
}

func TestDryRunNeedsNoTerminal(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "x\n"})
	h.clip = srText("a.txt", "x", "y")
	h.tty = false
	if code, _, errOut := h.run("--dry-run"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestInteractiveApproval(t *testing.T) {
	for _, answer := range []string{"y\n", "Y\n", "yes\n", "  YES  \n"} {
		h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
		h.clip = srText("a.txt", "world", "there")
		h.tty = true
		h.answer = answer
		code, out, errOut := h.run()
		if code != ExitOK {
			t.Fatalf("answer %q: exit %d, stderr %q", answer, code, errOut)
		}
		contains(t, "stdout", out, "Apply these changes? [y/N]", "Applied changes to 1 file(s)", "Backup of the originals:")
		if h.file("a.txt") != "hello\nthere\n" {
			t.Fatalf("answer %q: file not modified: %q", answer, h.file("a.txt"))
		}
		if !h.backupsCreated() {
			t.Fatalf("answer %q: no backup created", answer)
		}
	}
}

func TestInteractiveRejection(t *testing.T) {
	// Enter alone, "n", garbage and EOF must all decline.
	for _, answer := range []string{"\n", "n\n", "no\n", "maybe\n", "", "yy\n"} {
		h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
		h.clip = srText("a.txt", "world", "there")
		h.tty = true
		h.answer = answer
		code, out, _ := h.run()
		if code != ExitError {
			t.Errorf("answer %q: exit %d, want %d", answer, code, ExitError)
		}
		contains(t, "stdout", out, "Cancelled. No files were modified.")
		if h.file("a.txt") != "hello\nworld\n" {
			t.Fatalf("answer %q: file modified after declining", answer)
		}
		if h.backupsCreated() {
			t.Errorf("answer %q: backup created after declining", answer)
		}
	}
}

func TestNoTerminalRequiresYesOrDryRun(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
	h.clip = srText("a.txt", "world", "there")
	h.tty = false
	code, _, errOut := h.run()
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", errOut, "--yes", "--dry-run", "No files were modified")
	if h.file("a.txt") != "hello\nworld\n" {
		t.Fatal("file modified without confirmation")
	}
}

func TestYesAppliesWithBackup(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
	h.stdin = srText("a.txt", "world", "there")
	code, out, errOut := h.run("--stdin", "--yes")
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	contains(t, "stdout", out, "Applied changes to 1 file(s)")
	if h.file("a.txt") != "hello\nthere\n" {
		t.Fatalf("file = %q", h.file("a.txt"))
	}
	if !h.backupsCreated() {
		t.Fatal("--yes skipped the backup")
	}
}

func TestYesStillValidates(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\n"})
	h.stdin = srText("a.txt", "does not exist", "x")
	code, _, errOut := h.run("--stdin", "--yes")
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", errOut, "ERROR: Patch context does not match", "File: a.txt", "No files were modified", "Possible causes")
	if h.file("a.txt") != "hello\n" || h.backupsCreated() {
		t.Fatal("--yes bypassed validation")
	}
}

func TestStdinWithInteractivePrompt(t *testing.T) {
	// The patch comes from stdin; the answer comes from the terminal.
	h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
	h.stdin = srText("a.txt", "world", "there")
	h.tty = true
	h.answer = "y\n"
	if code, _, errOut := h.run("--stdin"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if h.file("a.txt") != "hello\nthere\n" {
		t.Fatal("file not modified")
	}
}

func TestValidationFailureInMultiFilePatchModifiesNothing(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"})
	h.stdin = srText("a.txt", "one", "1") + srText("b.txt", "missing", "2")
	code, _, errOut := h.run("--stdin", "--yes")
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", errOut, "File: b.txt", "No files were modified")
	if h.file("a.txt") != "one\n" || h.file("b.txt") != "two\n" {
		t.Fatal("a file was modified although validation failed")
	}
}

func TestAmbiguousSearchRejected(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "x = 1\nx = 1\n"})
	h.stdin = srText("a.txt", "x = 1", "x = 2")
	code, _, errOut := h.run("--stdin", "--yes")
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", errOut, "ambiguous")
	if h.file("a.txt") != "x = 1\nx = 1\n" {
		t.Fatal("file modified despite ambiguity")
	}
}

func TestTraversalRejectedViaCLI(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "one\n"})
	outside := filepath.Join(filepath.Dir(h.root), "outside-"+filepath.Base(h.root)+".txt")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)
	h.stdin = srText("../"+filepath.Base(outside), "secret", "pwned")
	code, _, errOut := h.run("--stdin", "--yes")
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", errOut, "Unsafe file path", "No files were modified")
	if b, _ := os.ReadFile(outside); string(b) != "secret\n" {
		t.Fatal("file outside the root was modified")
	}
}

func TestRootAndBackupDirFlags(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
	elsewhere := t.TempDir()
	custom := filepath.Join(t.TempDir(), "my-backups")
	h.stdin = srText("a.txt", "world", "there")
	h.root = elsewhere // Getwd points to the wrong place...
	realRoot := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"}).root
	code, out, errOut := h.run("--stdin", "--yes", "--root", realRoot, "--backup-dir", custom)
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	contains(t, "stdout", out, custom)
	if b, _ := os.ReadFile(filepath.Join(realRoot, "a.txt")); string(b) != "hello\nthere\n" {
		t.Fatalf("--root was ignored: %q", b)
	}
	entries, err := os.ReadDir(custom)
	if err != nil || len(entries) != 1 {
		t.Fatalf("--backup-dir was ignored: %v %v", entries, err)
	}
}

func TestFormatFlagMismatch(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\n"})
	h.stdin = srText("a.txt", "hello", "bye")
	code, _, errOut := h.run("--stdin", "--dry-run", "--format", "unified-diff")
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", errOut, "ERROR:", "No files were modified")
}

func TestUnifiedDiffEndToEnd(t *testing.T) {
	h := newHarness(t, map[string]string{"src/main.py": "def greet():\n    print(\"Hello\")\n"})
	h.stdin = "diff --git a/src/main.py b/src/main.py\n--- a/src/main.py\n+++ b/src/main.py\n@@ -1,2 +1,3 @@\n def greet():\n-    print(\"Hello\")\n+    print(\"Hello, World!\")\n+    print(\"Welcome!\")\n"
	code, out, errOut := h.run("--stdin", "--yes")
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	contains(t, "stdout", out, "Patch detected: unified diff")
	if h.file("src/main.py") != "def greet():\n    print(\"Hello, World!\")\n    print(\"Welcome!\")\n" {
		t.Fatalf("file = %q", h.file("src/main.py"))
	}
}

func TestNoOpPatch(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\n"})
	h.stdin = srText("a.txt", "hello", "hello")
	code, out, _ := h.run("--stdin", "--yes")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stdout", out, "no changes", "[UNCHANGED] a.txt")
	if h.backupsCreated() {
		t.Fatal("backup created for a no-op")
	}
}

func TestColorFlag(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
	h.stdin = srText("a.txt", "world", "there")
	_, out, _ := h.run("--stdin", "--dry-run", "--color", "always")
	if !strings.Contains(out, "\x1b[") {
		t.Error("--color always produced no escape codes")
	}
	h.stdin = srText("a.txt", "world", "there")
	_, out, _ = h.run("--stdin", "--dry-run", "--color", "never")
	if strings.Contains(out, "\x1b[") {
		t.Error("--color never produced escape codes")
	}
}

func TestEmptyStdin(t *testing.T) {
	h := newHarness(t, nil)
	code, _, errOut := h.run("--stdin")
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	contains(t, "stderr", errOut, "Standard input is empty")
}
