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
		Timeout:  5 * time.Second,
	}
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
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
		ctx, cancel := context.WithTimeout(context.Background(), s.Timeout)
		out, err := s.Run(ctx, c.name, c.args...)
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("%s failed: %w", c.name, err)
			continue
		}
		if !utf8.Valid(out) {
			return "", fmt.Errorf("%s returned data that is not valid UTF-8 text", c.name)
		}
		return string(out), nil
	}
	return "", lastErr
}
