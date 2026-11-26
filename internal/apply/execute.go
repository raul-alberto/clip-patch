package apply

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"clip-patch/internal/safefs"
)

// Ops are the side-effecting primitives used by Execute. Tests replace them
// to inject failures; production code uses DefaultOps.
type Ops struct {
	CreateTemp func(dir, pattern string) (*os.File, error)
	Rename     func(oldpath, newpath string) error
	NewID      func() string
}

// DefaultOps returns the real implementations.
func DefaultOps() Ops {
	return Ops{
		CreateTemp: os.CreateTemp,
		Rename:     os.Rename,
		NewID:      newBackupID,
	}
}

func newBackupID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// Result describes a successful Execute.
type Result struct {
	BackupDir string
	Written   []string // relative paths, in write order
}

// ExecError is returned when applying fails. It states precisely what state
// the files are in.
type ExecError struct {
	Err        error
	Modified   bool // some target files may still be modified
	RolledBack bool // a failure after writing began, and originals were restored
	BackupDir  string
}

func (e *ExecError) Error() string { return e.Err.Error() }
func (e *ExecError) Unwrap() error { return e.Err }

// DefaultBackupDir returns the per-user application data directory used for
// backups when --backup-dir is not given.
func DefaultBackupDir() (string, error) {
	home, err := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "clip-patch", "backups"), nil
		}
		if err != nil {
			return "", fmt.Errorf("cannot determine backup directory: %w", err)
		}
		return filepath.Join(home, "AppData", "Local", "clip-patch", "backups"), nil
	case "darwin":
		if err != nil {
			return "", fmt.Errorf("cannot determine backup directory: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "clip-patch", "backups"), nil
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, "clip-patch", "backups"), nil
		}
		if err != nil {
			return "", fmt.Errorf("cannot determine backup directory: %w", err)
		}
		return filepath.Join(home, ".local", "share", "clip-patch", "backups"), nil
	}
}

type manifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Mode   string `json:"mode"`
}

type manifest struct {
	Tool      string         `json:"tool"`
	CreatedAt string         `json:"created_at"`
	Root      string         `json:"root"`
	Format    string         `json:"format"`
	Files     []manifestFile `json:"files"`
}

// createBackup stores the exact original bytes of every changed file under a
// fresh directory, verifies each copy, and writes a manifest. It returns the
// backup directory. It never overwrites an existing backup.
func createBackup(plan *Plan, changes []*Change, backupRoot string, ops Ops) (string, error) {
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return "", fmt.Errorf("cannot create backup directory %q: %w", backupRoot, err)
	}
	session := ""
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		cand := filepath.Join(backupRoot, ops.NewID())
		err := os.Mkdir(cand, 0o700)
		if err == nil {
			session = cand
			break
		}
		lastErr = err
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("cannot create backup directory %q: %w", cand, err)
		}
	}
	if session == "" {
		return "", fmt.Errorf("could not allocate a unique backup directory under %q: %w", backupRoot, lastErr)
	}

	m := manifest{
		Tool:      "clip-patch",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Root:      plan.RootAbs,
		Format:    string(plan.Format),
	}
	for _, c := range changes {
		dst := filepath.Join(session, "files", filepath.FromSlash(c.File.Rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return "", fmt.Errorf("cannot create backup subdirectory for %s: %w", c.File.Rel, err)
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", fmt.Errorf("cannot create backup of %s: %w", c.File.Rel, err)
		}
		_, werr := out.Write(c.File.Raw)
		if werr == nil {
			werr = out.Sync()
		}
		cerr := out.Close()
		if werr == nil {
			werr = cerr
		}
		if werr != nil {
			return "", fmt.Errorf("cannot write backup of %s: %w", c.File.Rel, werr)
		}
		back, err := os.ReadFile(dst)
		if err != nil {
			return "", fmt.Errorf("cannot verify backup of %s: %w", c.File.Rel, err)
		}
		if !bytes.Equal(back, c.File.Raw) {
			return "", fmt.Errorf("backup verification failed for %s: the copy differs from the original", c.File.Rel)
		}
		// Preserve permissions; a failure here is not fatal to data safety.
		_ = os.Chmod(dst, c.File.Mode.Perm())
		sum := safefs.Fingerprint(c.File.Raw)
		m.Files = append(m.Files, manifestFile{
			Path:   c.File.Rel,
			SHA256: hex.EncodeToString(sum[:]),
			Mode:   fmt.Sprintf("%04o", c.File.Mode.Perm()),
		})
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cannot encode backup manifest: %w", err)
	}
	mf, err := os.OpenFile(filepath.Join(session, "manifest.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("cannot create backup manifest: %w", err)
	}
	_, werr := mf.Write(append(data, '\n'))
	cerr := mf.Close()
	if werr != nil {
		return "", fmt.Errorf("cannot write backup manifest: %w", werr)
	}
	if cerr != nil {
		return "", fmt.Errorf("cannot write backup manifest: %w", cerr)
	}
	return session, nil
}

type staged struct {
	c   *Change
	tmp string
}

// Execute applies a validated plan.
//
// Order of operations: re-verify fingerprints, create and verify backups,
// stage every new file in a temporary file next to its target, then rename
// each over its target. If any step fails, staged files are removed and any
// target that was already replaced is restored from the in-memory original.
// Multi-file application is therefore best-effort, not globally atomic.
func Execute(plan *Plan, backupRoot string, ops Ops) (*Result, error) {
	def := DefaultOps()
	if ops.CreateTemp == nil {
		ops.CreateTemp = def.CreateTemp
	}
	if ops.Rename == nil {
		ops.Rename = def.Rename
	}
	if ops.NewID == nil {
		ops.NewID = def.NewID
	}

	changes := plan.ChangedFiles()
	if len(changes) == 0 {
		return &Result{}, nil
	}
	if err := plan.VerifyUnchanged(); err != nil {
		return nil, &ExecError{Err: err}
	}
	session, err := createBackup(plan, changes, backupRoot, ops)
	if err != nil {
		return nil, &ExecError{Err: err}
	}

	var stagedFiles []staged
	removeTemps := func() {
		for _, s := range stagedFiles {
			_ = os.Remove(s.tmp)
		}
	}
	for _, c := range changes {
		tf, err := ops.CreateTemp(filepath.Dir(c.File.Abs), ".clip-patch-*.tmp")
		if err != nil {
			removeTemps()
			return nil, &ExecError{Err: fmt.Errorf("cannot create a temporary file for %s: %w", c.File.Rel, err), BackupDir: session}
		}
		tmp := tf.Name()
		stagedFiles = append(stagedFiles, staged{c: c, tmp: tmp})
		_, werr := tf.Write(c.NewBytes)
		if werr == nil {
			werr = tf.Sync()
		}
		cerr := tf.Close()
		if werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = os.Chmod(tmp, c.File.Mode.Perm())
		}
		if werr != nil {
			removeTemps()
			return nil, &ExecError{Err: fmt.Errorf("cannot write the new content of %s: %w", c.File.Rel, werr), BackupDir: session}
		}
	}

	var done []staged
	for _, s := range stagedFiles {
		if err := ops.Rename(s.tmp, s.c.File.Abs); err != nil {
			removeTemps()
			rbErr := rollback(done)
			return nil, rollbackResult(fmt.Errorf("cannot replace %s: %w", s.c.File.Rel, err), done, rbErr, session)
		}
		done = append(done, s)
	}

	// Post-write verification.
	for _, s := range done {
		got, err := os.ReadFile(s.c.File.Abs)
		if err != nil || !bytes.Equal(got, s.c.NewBytes) {
			rbErr := rollback(done)
			reason := "the written content does not match the expected content"
			if err != nil {
				reason = err.Error()
			}
			return nil, rollbackResult(fmt.Errorf("verification of %s failed after writing: %s", s.c.File.Rel, reason), done, rbErr, session)
		}
	}

	res := &Result{BackupDir: session}
	for _, s := range done {
		res.Written = append(res.Written, s.c.File.Rel)
	}
	return res, nil
}

func rollbackResult(cause error, done []staged, rbErr error, session string) error {
	e := &ExecError{Err: cause, BackupDir: session}
	switch {
	case rbErr != nil:
		e.Modified = true
		e.Err = fmt.Errorf("%w (rollback also failed: %v)", cause, rbErr)
	case len(done) > 0:
		e.RolledBack = true
	}
	return e
}

// rollback restores the original bytes of every file in done, newest first.
func rollback(done []staged) error {
	var msgs []string
	for i := len(done) - 1; i >= 0; i-- {
		c := done[i].c
		if err := restore(c); err != nil {
			msgs = append(msgs, fmt.Sprintf("%s: %v", c.File.Rel, err))
		}
	}
	if len(msgs) > 0 {
		return errors.New(strings.Join(msgs, "; "))
	}
	return nil
}

func restore(c *Change) error {
	tf, err := os.CreateTemp(filepath.Dir(c.File.Abs), ".clip-patch-restore-*.tmp")
	if err != nil {
		return err
	}
	tmp := tf.Name()
	_, werr := tf.Write(c.File.Raw)
	if werr == nil {
		werr = tf.Sync()
	}
	cerr := tf.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, c.File.Mode.Perm())
	}
	if werr == nil {
		werr = os.Rename(tmp, c.File.Abs)
	}
	if werr != nil {
		_ = os.Remove(tmp)
	}
	return werr
}
