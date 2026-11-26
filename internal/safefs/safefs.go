// Package safefs validates untrusted paths from patches and loads target
// files in a way that preserves BOMs and line endings.
package safefs

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"clip-patch/internal/patch"
)

// MaxFileSize is the largest target file clip-patch will modify.
const MaxFileSize = 32 << 20 // 32 MiB

// File is a loaded target file.
type File struct {
	Rel  string // cleaned, slash-separated path relative to the root
	Abs  string // absolute path on disk
	Mode fs.FileMode
	Raw  []byte // exact original bytes
	Hash [sha256.Size]byte
	BOM  bool
	CRLF bool
	Text string // BOM removed, line endings normalised to LF
}

// Fingerprint returns the SHA-256 of b.
func Fingerprint(b []byte) [sha256.Size]byte { return sha256.Sum256(b) }

// ResolveRoot returns the absolute and symlink-resolved project root.
func ResolveRoot(root string) (abs, real string, err error) {
	abs, err = filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve project root %q: %w", root, err)
	}
	real, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve project root %q: %w", root, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", "", fmt.Errorf("cannot access project root %q: %w", root, err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("project root %q is not a directory", root)
	}
	return abs, real, nil
}

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// ValidateRelPath checks an untrusted path taken from a patch and returns it
// as a cleaned, slash-separated relative path. It rejects absolute paths,
// drive letters, UNC paths, ".." segments, ".git" segments, colons (Windows
// alternate data streams) and Windows reserved device names. The check is the
// same on every platform on purpose.
func ValidateRelPath(p string) (string, error) {
	orig := p
	bad := func(reason string) (string, error) {
		return "", &patch.Error{Title: "Unsafe file path in patch.", File: orig, Reason: reason + ". The file was not touched."}
	}
	if strings.TrimSpace(p) == "" {
		return bad("the path is empty")
	}
	if strings.ContainsRune(p, 0) {
		return bad("the path contains a NUL byte")
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(p, "/") {
		return bad("absolute paths and UNC paths are not allowed")
	}
	if len(p) >= 2 && p[1] == ':' && ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z')) {
		return bad("Windows drive-letter paths are not allowed")
	}
	if strings.Contains(p, ":") {
		return bad("colons are not allowed in paths (drive letters and alternate data streams)")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return bad("directory traversal ('..') is not allowed")
		}
		if strings.EqualFold(seg, ".git") {
			return bad("modifying Git metadata (.git) is not allowed")
		}
	}
	clean := path.Clean(p)
	if clean == "." || clean == "" {
		return bad("the path does not name a file")
	}
	base := path.Base(clean)
	stem := base
	if idx := strings.IndexByte(stem, '.'); idx >= 0 {
		stem = stem[:idx]
	}
	if reservedNames[strings.ToUpper(stem)] {
		return bad("the path uses a reserved device name")
	}
	return clean, nil
}

func within(rootReal, real string) bool {
	rel, err := filepath.Rel(rootReal, real)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Load validates and reads one target file.
func Load(rootAbs, rootReal, rel string) (*File, error) {
	abs := filepath.Join(rootAbs, filepath.FromSlash(rel))
	fail := func(reason string, causes ...string) error {
		return &patch.Error{Title: "Cannot use target file.", File: rel, Reason: reason, Causes: causes}
	}

	li, err := os.Lstat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fail("the file does not exist", "The patch may refer to a different project directory (see --root).", "clip-patch does not create new files.")
		}
		return nil, fail(err.Error())
	}
	if li.Mode()&os.ModeSymlink != 0 {
		return nil, fail("the target is a symbolic link; symlinks are not supported as patch targets")
	}
	if li.IsDir() {
		return nil, fail("the target is a directory, not a regular file")
	}
	if !li.Mode().IsRegular() {
		return nil, fail("the target is not a regular file")
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fail(err.Error())
	}
	if !within(rootReal, real) {
		return nil, &patch.Error{
			Title:  "Path escapes the project root.",
			File:   rel,
			Reason: "the file resolves (through a symlinked directory) to a location outside the project root. The file was not touched.",
		}
	}
	if li.Size() > MaxFileSize {
		return nil, fail(fmt.Sprintf("the file is larger than the %d MiB limit", MaxFileSize>>20))
	}
	if li.Mode().Perm()&0o200 == 0 {
		return nil, fail("the file is read-only")
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, fail(err.Error())
	}
	text, bom, crlf, reason := decode(raw)
	if reason != "" {
		return nil, fail(reason)
	}
	return &File{
		Rel:  rel,
		Abs:  abs,
		Mode: li.Mode(),
		Raw:  raw,
		Hash: Fingerprint(raw),
		BOM:  bom,
		CRLF: crlf,
		Text: text,
	}, nil
}

// decode returns the LF-normalised text of a UTF-8 file, or a non-empty
// reason if the encoding or line endings are unsupported.
func decode(raw []byte) (text string, bom, crlf bool, reason string) {
	if len(raw) >= 2 && ((raw[0] == 0xFF && raw[1] == 0xFE) || (raw[0] == 0xFE && raw[1] == 0xFF)) {
		return "", false, false, "UTF-16 encoded files are not supported"
	}
	body := raw
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		bom = true
		body = raw[3:]
	}
	if bytes.IndexByte(body, 0) >= 0 {
		return "", false, false, "the file looks binary (it contains NUL bytes)"
	}
	if !utf8.Valid(body) {
		return "", false, false, "the file is not valid UTF-8"
	}
	s := string(body)
	crlfCount := strings.Count(s, "\r\n")
	lfOnly := strings.Count(s, "\n") - crlfCount
	loneCR := strings.Count(s, "\r") - crlfCount
	if loneCR > 0 {
		return "", false, false, "the file contains bare CR line endings, which are not supported"
	}
	if crlfCount > 0 && lfOnly > 0 {
		return "", false, false, "the file has mixed line endings (CRLF and LF), which are not supported"
	}
	if crlfCount > 0 {
		crlf = true
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	return s, bom, crlf, ""
}

// Render converts new LF-normalised text back to the file's original BOM and
// line-ending conventions.
func Render(f *File, newText string) []byte {
	if f.CRLF {
		newText = strings.ReplaceAll(newText, "\n", "\r\n")
	}
	out := make([]byte, 0, len(newText)+3)
	if f.BOM {
		out = append(out, 0xEF, 0xBB, 0xBF)
	}
	return append(out, newText...)
}
