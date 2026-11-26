package apply

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"clip-patch/internal/parser"
	"clip-patch/internal/patch"
)

func newProject(t *testing.T, files map[string]string) string {
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
	return root
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sr(edits map[string][2]string, order ...string) *patch.Patch {
	p := &patch.Patch{Format: patch.FormatSearchReplace}
	for _, path := range order {
		e := edits[path]
		p.Files = append(p.Files, patch.FilePatch{
			Path:  path,
			Edits: []patch.Edit{{Search: e[0], Replace: e[1]}},
		})
	}
	return p
}

func noTempLeftovers(t *testing.T, root string) {
	t.Helper()
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && strings.HasPrefix(info.Name(), ".clip-patch-") {
			t.Errorf("leftover temporary file: %s", path)
		}
		return nil
	})
}

func TestExecuteNormalWithBackup(t *testing.T) {
	root := newProject(t, map[string]string{"src/a.txt": "hello\nworld\n"})
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(root, "src", "a.txt"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := BuildPlan(root, sr(map[string][2]string{"src/a.txt": {"world", "there"}}, "src/a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(t.TempDir(), "backups")
	res, err := Execute(plan, backups, Ops{})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "src/a.txt"); got != "hello\nthere\n" {
		t.Fatalf("file content = %q", got)
	}
	b, err := os.ReadFile(filepath.Join(res.BackupDir, "files", "src", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello\nworld\n" {
		t.Fatalf("backup content = %q", b)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(filepath.Join(root, "src", "a.txt"))
		if fi.Mode().Perm() != 0o640 {
			t.Errorf("file permissions changed: %v", fi.Mode().Perm())
		}
		bi, _ := os.Stat(filepath.Join(res.BackupDir, "files", "src", "a.txt"))
		if bi.Mode().Perm() != 0o640 {
			t.Errorf("backup permissions differ: %v", bi.Mode().Perm())
		}
	}
	mb, err := os.ReadFile(filepath.Join(res.BackupDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(mb, &m); err != nil || len(m.Files) != 1 || m.Files[0].Path != "src/a.txt" {
		t.Fatalf("bad manifest: %v %s", err, mb)
	}
	if len(res.Written) != 1 || res.Written[0] != "src/a.txt" {
		t.Errorf("Written = %v", res.Written)
	}
	noTempLeftovers(t, root)
}

func TestPreservesBOMAndCRLF(t *testing.T) {
	orig := "\xEF\xBB\xBFa\r\nb\r\nc\r\n"
	root := newProject(t, map[string]string{"f.txt": orig})
	plan, err := BuildPlan(root, sr(map[string][2]string{"f.txt": {"b", "b1\nb2"}}, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(plan, filepath.Join(t.TempDir(), "bk"), Ops{}); err != nil {
		t.Fatal(err)
	}
	want := "\xEF\xBB\xBFa\r\nb1\r\nb2\r\nc\r\n"
	if got := read(t, root, "f.txt"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPreservesLFAndMissingFinalNewline(t *testing.T) {
	root := newProject(t, map[string]string{"f.txt": "a\nb"})
	plan, err := BuildPlan(root, sr(map[string][2]string{"f.txt": {"b", "c"}}, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(plan, filepath.Join(t.TempDir(), "bk"), Ops{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "f.txt"); got != "a\nc" {
		t.Fatalf("got %q", got)
	}
}

func TestMultiFileWithInvalidTargetModifiesNothing(t *testing.T) {
	root := newProject(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"})
	p := sr(map[string][2]string{
		"a.txt":       {"one", "1"},
		"b.txt":       {"nonexistent text", "2"},
		"missing.txt": {"x", "y"},
	}, "a.txt", "b.txt", "missing.txt")
	if _, err := BuildPlan(root, p); err == nil {
		t.Fatal("expected BuildPlan to fail")
	}
	if read(t, root, "a.txt") != "one\n" || read(t, root, "b.txt") != "two\n" {
		t.Fatal("files were modified despite validation failure")
	}
}

func TestUnsafePathsRejectedBeforeAnyWrite(t *testing.T) {
	root := newProject(t, map[string]string{"a.txt": "one\n"})
	for _, bad := range []string{"../a.txt", "/etc/passwd", "C:\\x.txt", ".git/config", "..\\a.txt"} {
		p := sr(map[string][2]string{"a.txt": {"one", "1"}, bad: {"x", "y"}}, "a.txt", bad)
		if _, err := BuildPlan(root, p); err == nil {
			t.Errorf("%q: expected rejection", bad)
		}
	}
	if read(t, root, "a.txt") != "one\n" {
		t.Fatal("file modified")
	}
}

func TestDuplicateTargetsConflict(t *testing.T) {
	root := newProject(t, map[string]string{"a.txt": "one\ntwo\n"})
	p := sr(map[string][2]string{"a.txt": {"one", "1"}, "./a.txt": {"two", "2"}}, "a.txt", "./a.txt")
	_, err := BuildPlan(root, p)
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("expected duplicate-target error, got %v", err)
	}
}

func TestFileChangedAfterPreview(t *testing.T) {
	root := newProject(t, map[string]string{"a.txt": "one\n"})
	plan, err := BuildPlan(root, sr(map[string][2]string{"a.txt": {"one", "1"}}, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\nsomeone edited me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(t.TempDir(), "bk")
	_, err = Execute(plan, backups, Ops{})
	var xe *ExecError
	if !errors.As(err, &xe) || !strings.Contains(err.Error(), "changed since the preview") {
		t.Fatalf("expected 'changed since the preview' ExecError, got %v", err)
	}
	if xe.Modified || xe.RolledBack {
		t.Errorf("state flags wrong: %+v", xe)
	}
	if got := read(t, root, "a.txt"); got != "one\nsomeone edited me\n" {
		t.Fatalf("file was overwritten: %q", got)
	}
	if _, err := os.Stat(backups); err == nil {
		t.Error("a backup directory was created even though nothing was applied")
	}
}

func TestBackupCollisionAborts(t *testing.T) {
	root := newProject(t, map[string]string{"a.txt": "one\n"})
	plan, err := BuildPlan(root, sr(map[string][2]string{"a.txt": {"one", "1"}}, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(t.TempDir(), "bk")
	if err := os.MkdirAll(filepath.Join(backups, "fixed-id"), 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(backups, "fixed-id", "precious.txt")
	if err := os.WriteFile(marker, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := DefaultOps()
	ops.NewID = func() string { return "fixed-id" }
	if _, err := Execute(plan, backups, ops); err == nil {
		t.Fatal("expected failure when the backup directory already exists")
	}
	if read(t, root, "a.txt") != "one\n" {
		t.Fatal("file modified although the backup could not be created")
	}
	if b, _ := os.ReadFile(marker); string(b) != "keep me" {
		t.Fatal("existing backup was overwritten")
	}
}

func TestBackupCreationFailureAborts(t *testing.T) {
	root := newProject(t, map[string]string{"a.txt": "one\n"})
	plan, err := BuildPlan(root, sr(map[string][2]string{"a.txt": {"one", "1"}}, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// A regular file where the backup directory should be.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Execute(plan, filepath.Join(blocker, "sub"), Ops{})
	if err == nil {
		t.Fatal("expected backup failure")
	}
	if read(t, root, "a.txt") != "one\n" {
		t.Fatal("file modified although the backup failed")
	}
}

func TestTempFileFailureLeavesFilesUntouched(t *testing.T) {
	root := newProject(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"})
	plan, err := BuildPlan(root, sr(map[string][2]string{"a.txt": {"one", "1"}, "b.txt": {"two", "2"}}, "a.txt", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ops := DefaultOps()
	calls := 0
	ops.CreateTemp = func(dir, pattern string) (*os.File, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("injected: disk full")
		}
		return os.CreateTemp(dir, pattern)
	}
	_, err = Execute(plan, filepath.Join(t.TempDir(), "bk"), ops)
	var xe *ExecError
	if !errors.As(err, &xe) {
		t.Fatalf("expected ExecError, got %v", err)
	}
	if xe.Modified || xe.RolledBack {
		t.Errorf("nothing should have been written: %+v", xe)
	}
	if read(t, root, "a.txt") != "one\n" || read(t, root, "b.txt") != "two\n" {
		t.Fatal("files modified after a staging failure")
	}
	noTempLeftovers(t, root)
}

func TestRenameFailureRollsBack(t *testing.T) {
	root := newProject(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"})
	plan, err := BuildPlan(root, sr(map[string][2]string{"a.txt": {"one", "1"}, "b.txt": {"two", "2"}}, "a.txt", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ops := DefaultOps()
	calls := 0
	ops.Rename = func(oldpath, newpath string) error {
		calls++
		if calls == 2 {
			return errors.New("injected: rename failed")
		}
		return os.Rename(oldpath, newpath)
	}
	_, err = Execute(plan, filepath.Join(t.TempDir(), "bk"), ops)
	var xe *ExecError
	if !errors.As(err, &xe) {
		t.Fatalf("expected ExecError, got %v", err)
	}
	if !xe.RolledBack || xe.Modified {
		t.Errorf("expected RolledBack and not Modified: %+v", xe)
	}
	if read(t, root, "a.txt") != "one\n" || read(t, root, "b.txt") != "two\n" {
		t.Fatalf("rollback did not restore originals: a=%q b=%q", read(t, root, "a.txt"), read(t, root, "b.txt"))
	}
	noTempLeftovers(t, root)
}

func TestNoOpPatchWritesNothing(t *testing.T) {
	root := newProject(t, map[string]string{"a.txt": "one\n"})
	plan, err := BuildPlan(root, sr(map[string][2]string{"a.txt": {"one", "one"}}, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ChangedFiles()) != 0 {
		t.Fatal("expected no changed files")
	}
	backups := filepath.Join(t.TempDir(), "bk")
	res, err := Execute(plan, backups, Ops{})
	if err != nil || res.BackupDir != "" {
		t.Fatalf("expected empty result, got %+v, %v", res, err)
	}
	if _, err := os.Stat(backups); err == nil {
		t.Error("backup dir created for a no-op patch")
	}
}

func TestUnifiedDiffThroughPlan(t *testing.T) {
	root := newProject(t, map[string]string{"x.py": "def f():\n    return 1\n\nprint(f())\n"})
	diff := "--- a/x.py\n+++ b/x.py\n@@ -1,2 +1,2 @@\n def f():\n-    return 1\n+    return 2\n"
	p, err := parser.Parse(diff, patch.FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(plan, filepath.Join(t.TempDir(), "bk"), Ops{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "x.py"); got != "def f():\n    return 2\n\nprint(f())\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultBackupDirIsAbsolute(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("uses XDG_DATA_HOME")
	}
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	got, err := DefaultBackupDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(data, "clip-patch", "backups"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
