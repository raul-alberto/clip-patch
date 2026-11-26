package cli

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"clip-patch/internal/clipboard"
)

// Main runs the program with the real operating system environment and
// returns the exit code. Both cmd/clip-patch and cmd/cpatch call it.
func Main(version string) int {
	env := Env{
		Name:        toolName(),
		Version:     version,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Clipboard:   clipboard.NewSystem(),
		Interactive: openTerminal,
		ColorAuto:   colorWanted(),
	}
	return Run(os.Args[1:], env)
}

func toolName() string {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if base == "cpatch" {
		return "cpatch"
	}
	return "clip-patch"
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// colorWanted decides the --color=auto behaviour: colour only on a terminal,
// never when NO_COLOR is set, and on Windows only in terminals known to
// understand ANSI escapes.
func colorWanted() bool {
	if os.Getenv("NO_COLOR") != "" || !isTerminal(os.Stdout) {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	if runtime.GOOS == "windows" {
		return os.Getenv("WT_SESSION") != "" || os.Getenv("ANSICON") != "" || os.Getenv("TERM") != ""
	}
	return true
}

// openTerminal opens the controlling terminal for reading the confirmation
// answer. Using the terminal directly (rather than stdin) lets --stdin work
// together with the interactive prompt.
func openTerminal() (io.ReadCloser, error) {
	name := "/dev/tty"
	if runtime.GOOS == "windows" {
		name = "CONIN$"
	}
	return os.Open(name)
}
