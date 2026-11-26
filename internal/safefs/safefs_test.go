package safefs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateRelPath(t *testing.T) {
	good := map[string]string{
		"src/main.py":      "src/main.py",
		"./src/main.py":    "src/main.py",
		"src\\main.py":     "src/main.py",
		"a/b/../../c.txt":  "", // contains ".." -> rejected, see below
		"file with space":  "file with space",
		"dir/.gitignore":   "dir/.gitignore",
		"dir/.github/x.md": "dir/.github/x.md",
	}
	for in, want := range good {
		got, err := ValidateRelPath(in)
		if want == "" {
			if err == nil {
				t.Errorf("%q: expected rejection", in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}

	bad := []struct{ in, why string }{
		{"", "empty"},
		{"   ", "blank"},
		{"../etc/passwd", "traversal"},
		{"..", "traversal"},
		{"a/../../b", "traversal"},
		{"..\\..\\windows\\system32\\x", "windows traversal"},
		{"/etc/passwd", "absolute"},
		{"\\etc\\passwd", "absolute backslash"},
		{"C:\\Windows\\x.txt", "drive letter"},
		{"c:/x", "drive letter slash"},
		{"\\\\server\\share\\x", "UNC"},
		{"//server/share/x", "UNC slashes"},
		{".git/config", "git metadata"},
		{"sub/.GIT/hooks/pre-commit", "git metadata case-insensitive"},
		{"file.txt:stream", "alternate data stream"},
		{"dir/NUL", "reserved name"},
		{"con.txt", "reserved name with extension"},
		{"a\x00b", "NUL byte"},
		{".", "dot"},
		{"./", "dot slash"},
	}
	for _, tc := range bad {
		if got, err := ValidateRelPath(tc.in); err == nil {
			t.Errorf("%q (%s): expected rejection, got %q", tc.in, tc.why, got)
		}
	}
}

func setup(t *testing.T) (rootAbs, rootReal string) {
	t.Helper()
	dir := t.TempDir()
	a, r, err := ResolveRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return a, r
}

func write(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func TestLoadNormal(t *testing.T) {
	root, real := setup(t)
	write(t, filepath.Join(root, "src", "a.txt"), []byte("hello\nworld\n"), 0o644)
	f, err := Load(root, real, "src/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if f.Text != "hello\nworld\n" || f.BOM || f.CRLF {
		t.Errorf("unexpected file: %+v", f)
	}
}

func TestLoadErrors(t *testing.T) {
	root, real := setup(t)
	write(t, filepath.Join(root, "ok.txt"), []byte("x\n"), 0o644)
	if err := os.Mkdir(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "bin.dat"), []byte("ab\x00cd"), 0o644)
	write(t, filepath.Join(root, "latin1.txt"), []byte("caf\xe9\n"), 0o644)
	write(t, filepath.Join(root, "utf16.txt"), []byte{0xFF, 0xFE, 'a', 0, '\n', 0}, 0o644)
	write(t, filepath.Join(root, "mixed.txt"), []byte("a\r\nb\nc\r\n"), 0o644)
	write(t, filepath.Join(root, "barecr.txt"), []byte("a\rb\r"), 0o644)

	tests := []struct{ rel, want string }{
		{"missing.txt", "does not exist"},
		{"adir", "directory"},
		{"bin.dat", "binary"},
		{"latin1.txt", "UTF-8"},
		{"utf16.txt", "UTF-16"},
		{"mixed.txt", "mixed line endings"},
		{"barecr.txt", "bare CR"},
	}
	for _, tc := range tests {
		_, err := Load(root, real, tc.rel)
		if err == nil {
			t.Errorf("%s: expected error", tc.rel)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not contain %q", tc.rel, err.Error(), tc.want)
		}
	}
}

func TestLoadReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only semantics differ on Windows; covered by the attribute check on that platform")
	}
	root, real := setup(t)
	p := filepath.Join(root, "ro.txt")
	write(t, p, []byte("x\n"), 0o444)
	if _, err := Load(root, real, "ro.txt"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only error, got %v", err)
	}
}

func TestLoadSymlinks(t *testing.T) {
	root, real := setup(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.txt"), []byte("secret\n"), 0o644)
	write(t, filepath.Join(root, "inside.txt"), []byte("in\n"), 0o644)

	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "inside.txt"), filepath.Join(root, "inlink.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	for _, rel := range []string{"link.txt", "linkdir/secret.txt", "inlink.txt"} {
		if _, err := Load(root, real, rel); err == nil {
			t.Errorf("%s: expected rejection of symlink path", rel)
		}
	}
}

func TestDecodeAndRender(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		bom  bool
		crlf bool
	}{
		{"lf", []byte("a\nb\n"), false, false},
		{"crlf", []byte("a\r\nb\r\n"), false, true},
		{"bom lf", append([]byte{0xEF, 0xBB, 0xBF}, "a\nb\n"...), true, false},
		{"bom crlf", append([]byte{0xEF, 0xBB, 0xBF}, "a\r\nb\r\n"...), true, true},
		{"no final newline", []byte("a\nb"), false, false},
		{"empty", []byte(""), false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text, bom, crlf, reason := decode(tc.raw)
			if reason != "" {
				t.Fatalf("unexpected reason %q", reason)
			}
			if bom != tc.bom || crlf != tc.crlf {
				t.Fatalf("bom=%v crlf=%v, want %v/%v", bom, crlf, tc.bom, tc.crlf)
			}
			if strings.Contains(text, "\r") {
				t.Fatalf("text still contains CR: %q", text)
			}
			f := &File{BOM: bom, CRLF: crlf}
			if got := Render(f, text); string(got) != string(tc.raw) {
				t.Fatalf("render round trip: got %q, want %q", got, tc.raw)
			}
		})
	}
}

func TestResolveRootErrors(t *testing.T) {
	if _, _, err := ResolveRoot(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected error for missing root")
	}
	f := filepath.Join(t.TempDir(), "file")
	write(t, f, []byte("x"), 0o644)
	if _, _, err := ResolveRoot(f); err == nil {
		t.Error("expected error when root is a file")
	}
}
