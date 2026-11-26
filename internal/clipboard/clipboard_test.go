package clipboard

import (
	"context"
	"errors"
	"fmt"
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
