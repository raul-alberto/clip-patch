// Package clipboard reads text from the operating system clipboard by running
// the platform's standard clipboard tool. It has no third-party dependencies.
//
// Clipboard text is only ever returned to the caller as data. It is never
// interpreted as a command.
package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

// Reader is the abstraction used by the CLI so tests never need a desktop.
type Reader interface {
	ReadText() (string, error)
}

// Static is a Reader returning fixed content; used in tests.
type Static struct {
	Text string
	Err  error
}

// ReadText implements Reader.
func (s Static) ReadText() (string, error) { return s.Text, s.Err }

// command describes one way of reading the clipboard.
type command struct {
	name string
	args []string
}

var errClipboardOutputTooLarge = errors.New("clipboard command output exceeds the configured limit")

const (
	maxClipboardBytes     = 8 << 20
	maxStderrBytes        = 64 << 10
	defaultCommandTimeout = 5 * time.Second
)

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()

	if remaining <= 0 {
		b.exceeded = true
		return len(p), nil
	}

	if len(p) > remaining {
		b.exceeded = true
		_, _ = b.buffer.Write(p[:remaining])
		return len(p), nil
	}

	return b.buffer.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buffer.Bytes()
}

// System reads the real clipboard. Its fields exist so tests can substitute
// the environment; use NewSystem for production.
type System struct {
	GOOS     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Run      func(ctx context.Context, name string, args ...string) ([]byte, error)
	Timeout  time.Duration
}

// NewSystem returns a System bound to the real OS.
func NewSystem() *System {
	return &System{
		GOOS:     runtime.GOOS,
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		Run:      runCommand,
		Timeout:  defaultCommandTimeout,
	}
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout limitedBuffer
	stdout.limit = maxClipboardBytes

	var stderr limitedBuffer
	stderr.limit = maxStderrBytes

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
	}

	if stdout.exceeded {
		return nil, fmt.Errorf(
			"%w: %d bytes",
			errClipboardOutputTooLarge,
			maxClipboardBytes,
		)
	}

	if err != nil {
		msg := strings.TrimSpace(string(stderr.Bytes()))

		if stderr.exceeded {
			msg += " (stderr truncated)"
		}

		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}

		return nil, err
	}

	return stdout.Bytes(), nil
}

const powershellScript = "[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false; " +
	"$t = Get-Clipboard -Raw; if ($null -ne $t) { [Console]::Out.Write($t) }"

// candidates returns the commands to try, in order, plus a hint to show if
// none is available.
func (s *System) candidates() ([]command, string) {
	switch s.GOOS {
	case "windows":
		args := []string{"-NoProfile", "-NonInteractive", "-Command", powershellScript}
		return []command{
			{"powershell", args},
			{"pwsh", args},
		}, "PowerShell was not found on PATH."
	case "darwin":
		return []command{{"pbpaste", nil}}, "pbpaste was not found on PATH."
	default:
		var cmds []command
		wayland := s.Getenv("WAYLAND_DISPLAY") != ""
		x11 := s.Getenv("DISPLAY") != ""
		if wayland {
			cmds = append(cmds, command{"wl-paste", []string{"--no-newline"}})
		}
		if x11 {
			cmds = append(cmds,
				command{"xclip", []string{"-selection", "clipboard", "-out"}},
				command{"xsel", []string{"--clipboard", "--output"}},
			)
		}
		return cmds, "Install wl-clipboard (Wayland) or xclip / xsel (X11)."
	}
}

// ReadText implements Reader.
func (s *System) ReadText() (string, error) {
	cmds, hint := s.candidates()
	if len(cmds) == 0 {
		return "", errors.New("no graphical session detected (neither WAYLAND_DISPLAY nor DISPLAY is set); use --stdin instead")
	}
	var available []command
	for _, c := range cmds {
		if _, err := s.LookPath(c.name); err == nil {
			available = append(available, c)
		}
	}
	if len(available) == 0 {
		names := make([]string, 0, len(cmds))
		for _, c := range cmds {
			names = append(names, c.name)
		}
		return "", fmt.Errorf("no clipboard tool found (looked for: %s). %s", strings.Join(names, ", "), hint)
	}

	var lastErr error
	for _, c := range available {
		timeout := s.Timeout
		if timeout <= 0 {
			timeout = defaultCommandTimeout
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		out, err := s.Run(ctx, c.name, c.args...)
		cancel()
		if err != nil {
			wrappedErr := fmt.Errorf("%s failed: %w", c.name, err)

			if errors.Is(err, context.DeadlineExceeded) ||
				errors.Is(err, context.Canceled) ||
				errors.Is(err, errClipboardOutputTooLarge) {
				return "", wrappedErr
			}

			lastErr = wrappedErr
			continue
		}
		if len(out) > maxClipboardBytes {
			return "", fmt.Errorf(
				"%s returned clipboard data exceeding the %d-byte limit",
				c.name,
				maxClipboardBytes,
			)
		}
		if !utf8.Valid(out) {
			return "", fmt.Errorf("%s returned data that is not valid UTF-8 text", c.name)
		}
		return string(out), nil
	}
	return "", lastErr
}
