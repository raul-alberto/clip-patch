package preview

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"clip-patch/internal/apply"
	"clip-patch/internal/patch"
)

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	return len(p) - 1, nil
}

func TestRenderSuccess(t *testing.T) {
	var buf bytes.Buffer

	plan := &apply.Plan{
		Format: patch.FormatSearchReplace,
	}

	err := Render(&buf, "test", plan, false)
	if err != nil {
		t.Fatalf("Render() returned unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "Preview complete.") {
		t.Fatal("preview completion message is missing")
	}
}

func TestRenderPropagatesWriteError(t *testing.T) {
	expected := errors.New("write failed")

	plan := &apply.Plan{
		Format: patch.FormatSearchReplace,
	}

	err := Render(failingWriter{err: expected}, "test", plan, false)
	if !errors.Is(err, expected) {
		t.Fatalf("Render() error = %v, want %v", err, expected)
	}
}

func TestRenderDetectsShortWrite(t *testing.T) {
	plan := &apply.Plan{
		Format: patch.FormatSearchReplace,
	}

	err := Render(shortWriter{}, "test", plan, false)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Render() error = %v, want %v", err, io.ErrShortWrite)
	}
}
