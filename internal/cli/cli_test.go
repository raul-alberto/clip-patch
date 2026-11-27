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

type errorReader struct {
	data []byte
	err  error
	done bool
}

type failWriter struct {
	bytes.Buffer
	marker []byte
	err    error
}

func (w *failWriter) Write(p []byte) (int, error) {
	if len(w.marker) == 0 || bytes.Contains(p, w.marker) {
		return 0, w.err
	}
	return w.Buffer.Write(p)
}

func (r *errorReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	n := copy(p, r.data)
	return n, r.err
}

type harness struct {
	t         *testing.T
	root      string
	backups   string
	clip      string
	clipErr   error
	stdin     string
	tty       bool   // whether a terminal is available for the prompt
	answer    string // what the user types at the prompt
	answerErr error
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
	code = Run(args, h.environment(&out, &errb))
	return code, out.String(), errb.String()
}

func (h *harness) environment(stdout, stderr io.Writer) Env {
	env := Env{
		Name:      "clip-patch",
		Version:   "0.1.0",
		Stdin:     strings.NewReader(h.stdin),
		Stdout:    stdout,
		Stderr:    stderr,
		Clipboard: clipboard.Static{Text: h.clip, Err: h.clipErr},
		Getwd:     func() (string, error) { return h.root, nil },
		DefaultBackupDir: func() (string, error) {
			return h.backups, nil
		},
	}
	if h.tty {
		answer := h.answer
		env.Interactive = func() (io.ReadCloser, error) {
			if h.answerErr != nil {
				return io.NopCloser(&errorReader{
					data: []byte(answer),
					err:  h.answerErr,
				}), nil
			}
			return io.NopCloser(strings.NewReader(answer)), nil
		}
	}
	return env
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

func TestStdinRejectsInvalidUTF8BeforePlanning(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
	h.stdin = srText("a.txt", "world", "there\xff")

	code, _, errOut := h.run("--stdin", "--yes")
	if code != ExitError {
		t.Fatalf("exit %d, want %d", code, ExitError)
	}
	contains(t, "stderr", errOut, "standard input is not valid UTF-8", "No files were modified")
	if got := h.file("a.txt"); got != "hello\nworld\n" {
		t.Fatalf("file changed after invalid UTF-8 input: %q", got)
	}
	if h.backupsCreated() {
		t.Fatal("backup created after invalid UTF-8 input")
	}
}

func TestStdinInputLimit(t *testing.T) {
	h := newHarness(t, nil)
	h.stdin = strings.Repeat("x", MaxInputSize+1)

	code, _, errOut := h.run("--stdin")
	if code != ExitError {
		t.Fatalf("exit %d, want %d", code, ExitError)
	}
	contains(t, "stderr", errOut, "larger than 8 MiB", "No files were modified")
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
	for _, answer := range []string{"y\n", "y", "Y\n", "yes\n", "  YES  \n"} {
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

func TestInteractiveReadErrorIsReportedAndRejects(t *testing.T) {
	for _, answer := range []string{"", "y"} {
		t.Run(answer, func(t *testing.T) {
			h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
			h.clip = srText("a.txt", "world", "there")
			h.tty = true
			h.answer = answer
			h.answerErr = errors.New("injected read error")

			code, _, errOut := h.run()
			if code != ExitError {
				t.Fatalf("exit %d, want %d; stderr %q", code, ExitError, errOut)
			}
			contains(t, "stderr", errOut, "cannot ask for confirmation", "injected read error")

			if got := h.file("a.txt"); got != "hello\nworld\n" {
				t.Fatalf("file modified after read error: %q", got)
			}
			if h.backupsCreated() {
				t.Fatal("backup created after read error")
			}
		})
	}
}

func TestInteractiveConfirmationInputIsBounded(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
	h.clip = srText("a.txt", "world", "there")
	h.tty = true
	h.answer = strings.Repeat("x", maxConfirmationBytes+1) + "\ny\n"

	code, _, errOut := h.run()
	if code != ExitError {
		t.Fatalf("exit %d, want %d", code, ExitError)
	}
	contains(t, "stderr", errOut, "confirmation response exceeds", "No files were modified")
	if got := h.file("a.txt"); got != "hello\nworld\n" {
		t.Fatalf("file changed after oversized confirmation: %q", got)
	}
	if h.backupsCreated() {
		t.Fatal("backup created after oversized confirmation")
	}
}

func TestPreviewWriteFailureAbortsBeforeApply(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
	h.stdin = srText("a.txt", "world", "there")

	var stderr bytes.Buffer
	stdout := &failWriter{err: errors.New("injected preview write failure")}
	code := Run([]string{"--stdin", "--yes"}, h.environment(stdout, &stderr))

	if code != ExitError {
		t.Fatalf("exit %d, want %d", code, ExitError)
	}
	contains(t, "stderr", stderr.String(), "cannot write preview", "injected preview write failure")
	if got := h.file("a.txt"); got != "hello\nworld\n" {
		t.Fatalf("file changed after preview write failure: %q", got)
	}
	if h.backupsCreated() {
		t.Fatal("backup created after preview write failure")
	}
}

func TestPromptWriteFailureAbortsBeforeApply(t *testing.T) {
	h := newHarness(t, map[string]string{"a.txt": "hello\nworld\n"})
	h.clip = srText("a.txt", "world", "there")
	h.tty = true
	h.answer = "y\n"

	var stderr bytes.Buffer
	stdout := &failWriter{
		marker: []byte("Apply these changes?"),
		err:    errors.New("injected prompt write failure"),
	}
	code := Run(nil, h.environment(stdout, &stderr))

	if code != ExitError {
		t.Fatalf("exit %d, want %d", code, ExitError)
	}
	contains(t, "stderr", stderr.String(), "cannot ask for confirmation", "injected prompt write failure")
	if got := h.file("a.txt"); got != "hello\nworld\n" {
		t.Fatalf("file changed after prompt write failure: %q", got)
	}
	if h.backupsCreated() {
		t.Fatal("backup created after prompt write failure")
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
