package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeEnv struct {
	goos      string
	env       map[string]string
	installed map[string]bool
	outputs   map[string]string // tool name -> stdout
	failing   map[string]bool
	ran       []string
	lastArgs  []string
}

func (f *fakeEnv) system() *System {
	return &System{
		GOOS:   f.goos,
		Getenv: func(k string) string { return f.env[k] },
		LookPath: func(name string) (string, error) {
			if f.installed[name] {
				return "/usr/bin/" + name, nil
			}
			return "", fmt.Errorf("%s: not found", name)
		},
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			f.ran = append(f.ran, name)
			f.lastArgs = args
			if f.failing[name] {
				return nil, errors.New("exit status 1")
			}
			return []byte(f.outputs[name]), nil
		},
		Timeout: time.Second,
	}
}

func TestStaticReader(t *testing.T) {
	var r Reader = Static{Text: "hi"}
	got, err := r.ReadText()
	if err != nil || got != "hi" {
		t.Fatalf("got %q, %v", got, err)
	}
	r = Static{Err: errors.New("boom")}
	if _, err := r.ReadText(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLinuxPrefersWaylandTool(t *testing.T) {
	f := &fakeEnv{
		goos:      "linux",
		env:       map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"},
		installed: map[string]bool{"wl-paste": true, "xclip": true},
		outputs:   map[string]string{"wl-paste": "from wayland", "xclip": "from x11"},
	}
	got, err := f.system().ReadText()
	if err != nil || got != "from wayland" {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(f.ran) != 1 || f.ran[0] != "wl-paste" {
		t.Fatalf("ran %v", f.ran)
	}
}

func TestLinuxX11FallsBackToXsel(t *testing.T) {
	f := &fakeEnv{
		goos:      "linux",
		env:       map[string]string{"DISPLAY": ":0"},
		installed: map[string]bool{"xsel": true},
		outputs:   map[string]string{"xsel": "via xsel"},
	}
	got, err := f.system().ReadText()
	if err != nil || got != "via xsel" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLinuxToolFailureFallsThrough(t *testing.T) {
	f := &fakeEnv{
		goos:      "linux",
		env:       map[string]string{"DISPLAY": ":0"},
		installed: map[string]bool{"xclip": true, "xsel": true},
		outputs:   map[string]string{"xsel": "second"},
		failing:   map[string]bool{"xclip": true},
	}
	got, err := f.system().ReadText()
	if err != nil || got != "second" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLinuxAllToolsFail(t *testing.T) {
	f := &fakeEnv{
		goos:      "linux",
		env:       map[string]string{"DISPLAY": ":0"},
		installed: map[string]bool{"xclip": true},
		failing:   map[string]bool{"xclip": true},
	}
	_, err := f.system().ReadText()
	if err == nil || !strings.Contains(err.Error(), "xclip failed") {
		t.Fatalf("got %v", err)
	}
}

func TestLinuxHeadless(t *testing.T) {
	f := &fakeEnv{goos: "linux", env: map[string]string{}}
	_, err := f.system().ReadText()
	if err == nil || !strings.Contains(err.Error(), "--stdin") {
		t.Fatalf("expected a message suggesting --stdin, got %v", err)
	}
}

func TestLinuxNoToolInstalled(t *testing.T) {
	f := &fakeEnv{goos: "linux", env: map[string]string{"DISPLAY": ":0"}, installed: map[string]bool{}}
	_, err := f.system().ReadText()
	if err == nil || !strings.Contains(err.Error(), "xclip") || !strings.Contains(err.Error(), "Install") {
		t.Fatalf("expected installation hint, got %v", err)
	}
}

func TestMacOS(t *testing.T) {
	f := &fakeEnv{
		goos:      "darwin",
		installed: map[string]bool{"pbpaste": true},
		outputs:   map[string]string{"pbpaste": "mac text"},
	}
	got, err := f.system().ReadText()
	if err != nil || got != "mac text" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestWindowsUsesPowerShell(t *testing.T) {
	f := &fakeEnv{
		goos:      "windows",
		installed: map[string]bool{"powershell": true},
		outputs:   map[string]string{"powershell": "win text"},
	}
	got, err := f.system().ReadText()
	if err != nil || got != "win text" {
		t.Fatalf("got %q, %v", got, err)
	}
	if !strings.Contains(strings.Join(f.lastArgs, " "), "Get-Clipboard") {
		t.Fatalf("PowerShell was not asked for the clipboard: %v", f.lastArgs)
	}
}

func TestInvalidUTF8Rejected(t *testing.T) {
	f := &fakeEnv{
		goos:      "darwin",
		installed: map[string]bool{"pbpaste": true},
		outputs:   map[string]string{"pbpaste": "bad \xff\xfe bytes"},
	}
	if _, err := f.system().ReadText(); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("got %v", err)
	}
}

func TestReadTextRejectsExcessiveCustomRunOutput(t *testing.T) {
	f := &fakeEnv{
		goos:      "darwin",
		installed: map[string]bool{"pbpaste": true},
		outputs:   map[string]string{"pbpaste": strings.Repeat("x", maxClipboardBytes+1)},
	}

	if _, err := f.system().ReadText(); err == nil || !strings.Contains(err.Error(), "exceeding") {
		t.Fatalf("expected clipboard output limit error, got %v", err)
	}
}

func TestRunCommandSuccess(t *testing.T) {
	out, err := runCommand(
		context.Background(),
		os.Args[0],
		"-test.run=^TestRunCommandHelper$",
		"--",
		"success",
	)
	if err != nil {
		t.Fatalf("runCommand failed: %v", err)
	}
	if string(out) != "helper output" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunCommandFailure(t *testing.T) {
	_, err := runCommand(
		context.Background(),
		os.Args[0],
		"-test.run=^TestRunCommandHelper$",
		"--",
		"failure",
	)
	if err == nil {
		t.Fatal("expected command failure")
	}
	if !strings.Contains(err.Error(), "helper failure") {
		t.Fatalf("expected helper failure in error, got %v", err)
	}
}

func TestRunCommandTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := runCommand(
		ctx,
		os.Args[0],
		"-test.run=^TestRunCommandHelper$",
		"--",
		"sleep",
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}

func TestRunCommandHelper(t *testing.T) {
	mode := ""
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}

	switch mode {
	case "success":
		_, _ = os.Stdout.Write([]byte("helper output"))
		os.Exit(0)
	case "failure":
		_, _ = os.Stderr.Write([]byte("helper failure"))
		os.Exit(2)
	case "sleep":
		time.Sleep(5 * time.Second)
		os.Exit(0)
	case "large-stdout":
		chunk := make([]byte, 32*1024)
		for i := 0; i < maxClipboardBytes/(32*1024)+1; i++ {
			_, _ = os.Stdout.Write(chunk)
		}
		os.Exit(0)
	case "large-stderr":
		chunk := make([]byte, 32*1024)
		for i := 0; i < maxStderrBytes/(32*1024)+1; i++ {
			_, _ = os.Stderr.Write(chunk)
		}
		os.Exit(2)
	}
}

func TestRunCommandRejectsLargeStdout(t *testing.T) {
	out, err := runCommand(
		context.Background(),
		os.Args[0],
		"-test.run=^TestRunCommandHelper$",
		"--",
		"large-stdout",
	)

	if err == nil {
		t.Fatal("expected output size limit error")
	}
	if out != nil {
		t.Fatalf("expected nil output, got %d bytes", len(out))
	}
	if !strings.Contains(err.Error(), "exceeds the") {
		t.Fatalf("expected output size limit error, got %v", err)
	}
}

func TestRunCommandBoundsLargeStderr(t *testing.T) {
	_, err := runCommand(
		context.Background(),
		os.Args[0],
		"-test.run=^TestRunCommandHelper$",
		"--",
		"large-stderr",
	)

	if err == nil {
		t.Fatal("expected command failure")
	}
	if !strings.Contains(err.Error(), "stderr truncated") {
		t.Fatalf("expected stderr truncation notice, got %v", err)
	}
	if len(err.Error()) > maxStderrBytes+1024 {
		t.Fatalf("error message is unexpectedly large: %d bytes", len(err.Error()))
	}
}

func TestReadTextStopsAfterTimeout(t *testing.T) {
	f := &fakeEnv{
		goos:      "linux",
		env:       map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"},
		installed: map[string]bool{"wl-paste": true, "xclip": true},
		failing:   map[string]bool{"wl-paste": true},
	}

	system := f.system()
	system.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.ran = append(f.ran, name)
		return nil, context.DeadlineExceeded
	}

	_, err := system.ReadText()
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if len(f.ran) != 1 {
		t.Fatalf("expected one command attempt after timeout, got %v", f.ran)
	}
	if f.ran[0] != "wl-paste" {
		t.Fatalf("expected wl-paste to be attempted first, got %v", f.ran)
	}
}

func TestReadTextStopsAfterCancellation(t *testing.T) {
	f := &fakeEnv{
		goos:      "linux",
		env:       map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"},
		installed: map[string]bool{"wl-paste": true, "xclip": true},
	}

	system := f.system()
	system.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.ran = append(f.ran, name)
		return nil, context.Canceled
	}

	_, err := system.ReadText()
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if len(f.ran) != 1 {
		t.Fatalf("expected one command attempt after cancellation, got %v", f.ran)
	}
}

func TestReadTextStopsAfterOutputLimit(t *testing.T) {
	f := &fakeEnv{
		goos:      "linux",
		env:       map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"},
		installed: map[string]bool{"wl-paste": true, "xclip": true},
	}

	system := f.system()
	system.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.ran = append(f.ran, name)
		return nil, fmt.Errorf("%w: %d bytes", errClipboardOutputTooLarge, maxClipboardBytes)
	}

	_, err := system.ReadText()
	if err == nil {
		t.Fatal("expected clipboard output limit error")
	}
	if len(f.ran) != 1 {
		t.Fatalf("expected one command attempt after output limit, got %v", f.ran)
	}
}

func TestReadTextStopsAfterProcessTimeout(t *testing.T) {
	f := &fakeEnv{
		goos:      "linux",
		env:       map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"},
		installed: map[string]bool{"wl-paste": true, "xclip": true},
	}

	system := f.system()
	system.Timeout = 500 * time.Millisecond
	system.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.ran = append(f.ran, name)
		return runCommand(
			ctx,
			os.Args[0],
			"-test.run=^TestRunCommandHelper$",
			"--",
			"sleep",
		)
	}

	_, err := system.ReadText()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if len(f.ran) != 1 {
		t.Fatalf("expected one command attempt after process timeout, got %v", f.ran)
	}
	if f.ran[0] != "wl-paste" {
		t.Fatalf("expected wl-paste to be attempted first, got %v", f.ran)
	}
}
