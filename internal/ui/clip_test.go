package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/totecli/tote/internal/shell"
)

// fakeTool writes an executable shell script that stands for a clipboard program: it records its
// arguments and its standard input in files next to it, and fails when told to.
//
// Parameters:
//   - t: running test
//   - dir: directory of the PATH
//   - name: file name of the program
//   - fail: true for a program that exits with an error
func fakeTool(t *testing.T, dir, name string, fail bool) {
	t.Helper()

	script := "#!/bin/sh\necho \"$@\" > \"" + filepath.Join(dir, name+".args") + "\"\n" +
		"cat > \"" + filepath.Join(dir, name+".in") + "\"\n"
	if fail {
		script += "exit 1\n"
	}

	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

// TestCopyWithTools checks the order of the clipboard programs, which ones are tried for which
// display server, and what happens when they fail.
//
// Parameters:
//   - t: test to report to
func TestCopyWithTools(t *testing.T) {
	tests := []struct {
		name  string
		env   []string
		fail  []string
		want  string
		ran   []string
		stdin string
	}{
		{
			"wayland first",
			[]string{"WAYLAND_DISPLAY=wayland-0", "DISPLAY=:0"},
			nil, "wl-copy",
			[]string{"wl-copy"},
			"wl-copy",
		},
		{"x11 only", []string{"DISPLAY=:0"}, nil, "xclip", []string{"xclip"}, "xclip"},
		{
			"wl-copy fails",
			[]string{"WAYLAND_DISPLAY=wayland-0", "DISPLAY=:0"},
			[]string{"wl-copy"},
			"xclip",
			[]string{"wl-copy", "xclip"},
			"xclip",
		},
		{
			"two fail",
			[]string{"WAYLAND_DISPLAY=wayland-0", "DISPLAY=:0"},
			[]string{"wl-copy", "xclip"},
			"xsel",
			[]string{"wl-copy", "xclip", "xsel"},
			"xsel",
		},
		{
			"all fail",
			[]string{"WAYLAND_DISPLAY=wayland-0", "DISPLAY=:0"},
			[]string{"wl-copy", "xclip", "xsel"},
			"",
			[]string{"wl-copy", "xclip", "xsel"},
			"",
		},
		{"no display", nil, nil, "", nil, ""},
	}

	for _, tt := range tests {
		dir := t.TempDir()

		for _, name := range []string{"wl-copy", "xclip", "xsel"} {
			fakeTool(t, dir, name, slices.Contains(tt.fail, name))
		}

		environ := append([]string{"PATH=" + dir + ":/usr/bin:/bin"}, tt.env...)
		text := "line one\nline two \u65E5\u672C"

		if got := copyWithTools(context.Background(), environ, text); got != tt.want {
			t.Errorf("%s: tool = %q, want %q", tt.name, got, tt.want)
		}

		for _, name := range []string{"wl-copy", "xclip", "xsel"} {
			_, err := os.Stat(filepath.Join(dir, name+".in"))
			if ran := err == nil; ran != slices.Contains(tt.ran, name) {
				t.Errorf("%s: %s ran = %v", tt.name, name, ran)
			}
		}

		if tt.stdin != "" {
			got, err := os.ReadFile(filepath.Join(dir, tt.stdin+".in"))
			if err != nil || string(got) != text {
				t.Errorf("%s: %s read %q (%v), want the text", tt.name, tt.stdin, got, err)
			}
		}
	}
}

// TestCopyWithToolsArguments checks the fixed arguments that each program gets.
//
// Parameters:
//   - t: test to report to
func TestCopyWithToolsArguments(t *testing.T) {
	for _, tt := range []struct {
		env  string
		fail []string
		tool string
		want string
	}{
		{"DISPLAY=:0", nil, "xclip", "-selection clipboard -in"},
		{"DISPLAY=:0", []string{"xclip"}, "xsel", "--clipboard --input"},
		{"WAYLAND_DISPLAY=w", nil, "wl-copy", ""},
	} {
		dir := t.TempDir()

		for _, name := range []string{"wl-copy", "xclip", "xsel"} {
			fakeTool(t, dir, name, slices.Contains(tt.fail, name))
		}

		copyWithTools(context.Background(), []string{"PATH=" + dir + ":/usr/bin:/bin", tt.env},
			"x")

		got, err := os.ReadFile(filepath.Join(dir, tt.tool+".args"))
		if err != nil || strings.TrimSpace(string(got)) != tt.want {
			t.Errorf("%s: arguments %q (%v), want %q", tt.tool, got, err, tt.want)
		}
	}
}

// TestLookProgramForTools checks which files of a PATH the clipboard chain runs: executable
// regular files in absolute directories only.
//
// Parameters:
//   - t: test to report to
func TestLookProgramForTools(t *testing.T) {
	dir := t.TempDir()
	plain := t.TempDir()

	fakeTool(t, dir, "xclip", false)

	script := []byte("#!/bin/sh\n")

	if err := os.WriteFile(filepath.Join(plain, "xclip"), script, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(plain, "xsel"), 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		tool string
		want string
		ok   bool
	}{
		{"found", dir, "xclip", filepath.Join(dir, "xclip"), true},
		{"later directory", plain + ":" + dir, "xclip", filepath.Join(dir, "xclip"), true},
		{"not executable", plain, "xclip", "", false},
		{"directory", plain, "xsel", "", false},
		{"missing", dir, "wl-copy", "", false},
		{"relative entry", "bin:.:" + "", "xclip", "", false},
		{"empty entry", ":", "xclip", "", false},
		{"empty path", "", "xclip", "", false},
	}

	for _, tt := range tests {
		got, ok := shell.LookProgram(tt.path, tt.tool)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: LookProgram = %q, %v, want %q, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

// TestCopyChain presses C on the real chain of clipboard programs, with fake ones in the PATH:
// the first program that takes the text is named in the toast and the terminal is not told, and
// with none in the PATH the sequence of the terminal comes next, then the text itself.
//
// Parameters:
//   - t: test to report to
func TestCopyChain(t *testing.T) {
	const (
		cmd  = "ssh -l deploy -- prod.example.com"
		both = "WAYLAND_DISPLAY=wayland-0"
		x11  = "DISPLAY=:0"
		all  = "wl-copy xclip xsel"
		hint = "No copy tool: install xclip, xsel or wl-clipboard, or press v to select it: " +
			cmd
	)

	tests := []struct {
		name   string
		env    []string
		tools  string
		fail   string
		toast  string
		stdin  string
		osc    bool
		absent string
	}{
		{
			"wayland",
			[]string{both, x11},
			all, "", "Copied with wl-copy: " + cmd, "wl-copy", false,
			"",
		},
		{"x11", []string{x11}, all, "", "Copied with xclip: " + cmd, "xclip", false, ""},
		{"xsel only", []string{x11}, "xsel", "", "Copied with xsel: " + cmd, "xsel", false, ""},
		{
			"first one fails",
			[]string{both, x11},
			"wl-copy xclip", "wl-copy",
			"Copied with xclip: " + cmd, "xclip", false, "",
		},
		{
			"wayland tool without wayland",
			[]string{x11},
			"wl-copy", "",
			"Sent to the terminal clipboard (OSC 52): " + cmd, "", true, "",
		},
		{
			"no tool",
			[]string{both, x11},
			"", "",
			"Sent to the terminal clipboard (OSC 52): " + cmd, "", true, "",
		},
		{
			"no tool, dumb terminal",
			[]string{both, x11, "TERM=dumb"},
			"", "", hint, "", false,
			"Copied with",
		},
		{
			"every tool fails",
			[]string{x11, "TERM=dumb"},
			"xclip xsel", "xclip xsel", hint, "xsel",
			false, "Copied with",
		},
	}

	for _, tt := range tests {
		dir := t.TempDir()

		// The fake programs call cat, and the PATH holds nothing else, so that no real clipboard
		// program of the machine can be found.
		cat, err := exec.LookPath("cat")
		if err != nil {
			t.Skip("cat is not installed")
		}

		if err := os.Symlink(cat, filepath.Join(dir, "cat")); err != nil {
			t.Fatal(err)
		}

		for _, name := range strings.Fields(tt.tools) {
			fakeTool(t, dir, name, slices.Contains(strings.Fields(tt.fail), name))
		}

		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = StartConnections
			o.Environ = append(append(append([]string(nil), o.Environ...), "PATH="+dir), tt.env...)
			o.TmuxClipboard = func() string { return "" }
		}))
		g.press("C")
		g.await("the copy", func() bool { return g.toastText() != "" })

		if got := g.toastText(); !strings.Contains(got, tt.toast) {
			t.Errorf("%s: toast %q, want it to hold %q", tt.name, got, tt.toast)
		}

		if tt.absent != "" && strings.Contains(g.toastText(), tt.absent) {
			t.Errorf("%s: toast %q holds %q", tt.name, g.toastText(), tt.absent)
		}

		if _, sent := clipboardOf(g); sent != tt.osc {
			t.Errorf("%s: the terminal was told = %v, want %v", tt.name, sent, tt.osc)
		}

		if tt.stdin != "" {
			got, err := os.ReadFile(filepath.Join(dir, tt.stdin+".in"))
			if err != nil || string(got) != cmd {
				t.Errorf("%s: %s read %q (%v), want the command", tt.name, tt.stdin, got, err)
			}
		}
	}
}

// TestCopyManualFrench checks that key C, with nothing to take the command, tells in French what
// to install and shows the command (the rig draws with ASCII, so the typography is not checked).
//
// Parameters:
//   - t: test to report to
func TestCopyManualFrench(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		o.Printer = newPrinter("fr")
		o.Environ = append(append([]string(nil), o.Environ...), "PATH="+t.TempDir(), "TERM=dumb")
		o.TmuxClipboard = func() string { return "" }
	}))
	g.press("C")
	g.await("the copy", func() bool { return g.toastText() != "" })

	got := g.toastText()
	for _, want := range []string{"installez xclip, xsel ou wl-clipboard", "ssh -l"} {
		if !strings.Contains(got, want) {
			t.Errorf("toast = %q, want it to hold %q", got, want)
		}
	}
}

// TestCopyToolTimeout checks that a clipboard program that never returns is given up after
// clipToolTimeout and that the next one is tried.
//
// Parameters:
//   - t: test to report to
func TestCopyToolTimeout(t *testing.T) {
	dir := t.TempDir()

	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep is not installed")
	}

	if err := os.Symlink(sleep, filepath.Join(dir, "sleep")); err != nil {
		t.Fatal(err)
	}

	hang := "#!/bin/sh\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "wl-copy"), []byte(hang), 0o700); err != nil {
		t.Fatal(err)
	}

	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat is not installed")
	}

	if err := os.Symlink(cat, filepath.Join(dir, "cat")); err != nil {
		t.Fatal(err)
	}

	fakeTool(t, dir, "xclip", false)

	start := time.Now()
	got := copyWithTools(context.Background(),
		[]string{"PATH=" + dir, "WAYLAND_DISPLAY=w", "DISPLAY=:0"}, "text")
	elapsed := time.Since(start)

	if got != "xclip" {
		t.Errorf("tool = %q, want xclip after the first gave up", got)
	}

	if elapsed < clipToolTimeout || elapsed > clipToolTimeout+10*time.Second {
		t.Errorf("took %v, want about %v", elapsed, clipToolTimeout)
	}
}
