package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/totecli/tote/internal/config"
)

// TestDescribeTerminal checks the description of a terminal for the environments that decide
// the colors and the glyphs, none of which depends on the terminfo database of the host.
//
// Parameters:
//   - t: test to report to
func TestDescribeTerminal(t *testing.T) {
	tests := []struct {
		name  string
		env   []string
		ascii bool
		want  Terminal
	}{
		{
			"truecolor",
			[]string{"TERM=xterm-256color", "COLORTERM=truecolor", "LANG=en_US.UTF-8"},
			false,
			Terminal{Color: "truecolor", UTF8: true},
		},
		{
			"256 colors",
			[]string{"TERM=xterm-256color", "LC_ALL=C.UTF-8"},
			false,
			Terminal{Color: "256", UTF8: true},
		},
		{
			"override",
			[]string{"TERM=xterm", "TOTE_COLOR=24bit", "LANG=C"},
			false,
			Terminal{Color: "truecolor", Override: "TOTE_COLOR", ASCII: true},
		},
		{
			"no color",
			[]string{"TERM=xterm-256color", "NO_COLOR=1", "LANG=fr_FR.UTF-8"},
			false,
			Terminal{Color: "none", Override: "NO_COLOR", UTF8: true},
		},
		{
			"bad override",
			[]string{"TERM=xterm-256color", "TOTE_COLOR=rainbow", "LANG=C.UTF-8"},
			false,
			Terminal{Color: "256", BadColor: true, UTF8: true},
		},
		{
			"dumb",
			[]string{"TERM=dumb", "LANG=C.UTF-8"},
			false,
			Terminal{Color: "none", ASCII: true, UTF8: true, Dumb: true},
		},
		{
			"unset", nil, false, Terminal{Color: "none", ASCII: true, Dumb: true},
		},
		{
			"forced ascii",
			[]string{"TERM=xterm-256color", "LANG=C.UTF-8"},
			true,
			Terminal{Color: "256", ASCII: true, UTF8: true},
		},
	}

	for _, tt := range tests {
		got := DescribeTerminal(tt.env, tt.ascii, func() string { return "" })
		if got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

// TestDescribeTerminalTmux checks that a tmux client with RGB support raises 256 colors.
//
// Parameters:
//   - t: test to report to
func TestDescribeTerminalTmux(t *testing.T) {
	env := []string{"TERM=screen-256color", "TMUX=/tmp/tmux-1/default,1,0", "LANG=C.UTF-8"}

	if got := DescribeTerminal(env, false, func() string { return "256,RGB" }); got.Color !=
		"truecolor" {
		t.Errorf("tmux with RGB: color %q, want truecolor", got.Color)
	}

	if got := DescribeTerminal(env, false, func() string { return "256" }); got.Color != "256" {
		t.Errorf("tmux without RGB: color %q, want 256", got.Color)
	}
}

// TestLayoutName checks the names of the three layouts.
//
// Parameters:
//   - t: test to report to
func TestLayoutName(t *testing.T) {
	always := config.DefaultSettings()
	always.UI.Compact = "always"

	tests := []struct {
		w, h int
		s    config.Settings
		want string
	}{
		{100, 30, config.DefaultSettings(), "full"},
		{60, 30, config.DefaultSettings(), "compact"},
		{100, 15, config.DefaultSettings(), "compact"},
		{100, 30, always, "compact"},
		{23, 30, config.DefaultSettings(), "tiny"},
		{100, 6, config.DefaultSettings(), "tiny"},
		{24, 7, config.DefaultSettings(), "compact"},
	}

	for _, tt := range tests {
		if got := LayoutName(tt.w, tt.h, tt.s); got != tt.want {
			t.Errorf("LayoutName(%d, %d) = %q, want %q", tt.w, tt.h, got, tt.want)
		}
	}
}

// TestMinSize checks that the exported minimum is the one that decides the layout: a terminal of
// that size has an interface, and one column or one row less has none.
//
// Parameters:
//   - t: test to report to
func TestMinSize(t *testing.T) {
	w, h := MinSize()
	s := config.DefaultSettings()

	if got := LayoutName(w, h, s); got == "tiny" {
		t.Errorf("LayoutName(%d, %d) = %q at the minimum", w, h, got)
	}

	for _, size := range [][2]int{{w - 1, h}, {w, h - 1}} {
		if got := LayoutName(size[0], size[1], s); got != "tiny" {
			t.Errorf("LayoutName(%d, %d) = %q, want tiny", size[0], size[1], got)
		}
	}
}

// TestResetSequence checks that the exported reset is the one written after a crash.
//
// Parameters:
//   - t: test to report to
func TestResetSequence(t *testing.T) {
	if ResetSequence != panicReset {
		t.Errorf("ResetSequence = %q, want %q", ResetSequence, panicReset)
	}
}

// TestClipboardRoute checks the route that a copy would take, for every program of the table with
// its display variable, for the PATH entries that are not trusted, and for the terminals that
// take the sequence OSC 52 or not. Fake programs stand in a temporary directory and are never
// run.
//
// Parameters:
//   - t: test to report to
func TestClipboardRoute(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dirs := map[string]string{}

	for name, tools := range map[string][]string{
		"wl": {"wl-copy"}, "xclip": {"xclip"}, "xsel": {"xsel"}, "x11": {"xclip", "xsel"},
		"all": {"wl-copy", "xclip", "xsel"}, "empty": nil,
	} {
		dirs[name] = t.TempDir()

		for _, tool := range tools {
			fakeTool(t, dirs[name], tool, false)
		}
	}

	rel, err := filepath.Rel(cwd, dirs["xclip"])
	if err != nil {
		t.Fatal(err)
	}

	plain := t.TempDir()
	script := []byte("#!/bin/sh\n")
	if err := os.WriteFile(filepath.Join(plain, "xclip"), script, 0o600); err != nil {
		t.Fatal(err)
	}

	tmuxEnv := []string{"TERM=tmux-256color", "TMUX=/tmp/tmux-1/default,1,0"}

	tests := []struct {
		name   string
		env    []string
		option string
		route  string
		tool   string
	}{
		{
			"wl-copy on Wayland",
			[]string{"PATH=" + dirs["wl"], "WAYLAND_DISPLAY=wayland-0"},
			"",
			"tool", "wl-copy",
		},
		{"xclip on X11", []string{"PATH=" + dirs["xclip"], "DISPLAY=:0"}, "", "tool", "xclip"},
		{"xsel on X11", []string{"PATH=" + dirs["xsel"], "DISPLAY=:0"}, "", "tool", "xsel"},
		{"xclip before xsel", []string{"PATH=" + dirs["x11"], "DISPLAY=:0"}, "", "tool", "xclip"},
		{"wl-copy before the others", []string{
			"PATH=" + dirs["all"], "WAYLAND_DISPLAY=wayland-0", "DISPLAY=:0",
		}, "", "tool", "wl-copy"},
		{"wl-copy without WAYLAND_DISPLAY", []string{
			"PATH=" + dirs["wl"], "DISPLAY=:0", "TERM=xterm-256color",
		}, "", "osc", ""},
		{"xclip without DISPLAY", []string{
			"PATH=" + dirs["xclip"], "WAYLAND_DISPLAY=wayland-0", "TERM=xterm-256color",
		}, "", "osc", ""},
		{"xsel with an empty DISPLAY", []string{
			"PATH=" + dirs["xsel"], "DISPLAY=", "TERM=xterm-256color",
		}, "", "osc", ""},
		{"a program beats VTE", []string{
			"PATH=" + dirs["xclip"], "DISPLAY=:0", "TERM=xterm-256color", "VTE_VERSION=7600",
		}, "", "tool", "xclip"},
		{"a program beats a dumb terminal", []string{
			"PATH=" + dirs["xclip"], "DISPLAY=:0", "TERM=dumb",
		}, "", "tool", "xclip"},
		{"relative PATH entry", []string{
			"PATH=" + rel, "DISPLAY=:0", "TERM=xterm-256color", "VTE_VERSION=7600",
		}, "", "none", ""},
		{"current directory in PATH", []string{
			"PATH=.:", "DISPLAY=:0", "TERM=xterm-256color", "VTE_VERSION=7600",
		}, "", "none", ""},
		{"program that is not executable", []string{
			"PATH=" + plain, "DISPLAY=:0", "TERM=xterm-256color", "VTE_VERSION=7600",
		}, "", "none", ""},
		{
			"empty PATH",
			[]string{"PATH=", "DISPLAY=:0", "TERM=xterm-256color", "VTE_VERSION=7600"},
			"", "none", "",
		},
		{
			"no PATH",
			[]string{"DISPLAY=:0", "TERM=xterm-256color", "VTE_VERSION=7600"},
			"",
			"none", "",
		},
		{"no program on a terminal that takes OSC 52", []string{
			"PATH=" + dirs["empty"], "DISPLAY=:0", "TERM=xterm-256color",
		}, "", "osc", ""},
		{"VTE", []string{
			"PATH=" + dirs["empty"], "DISPLAY=:0", "TERM=xterm-256color", "VTE_VERSION=7600",
		}, "", "none", ""},
		{"dumb terminal", []string{"PATH=" + dirs["empty"], "TERM=dumb"}, "", "none", ""},
		{"no terminal", []string{"PATH=" + dirs["empty"]}, "", "none", ""},
		{"linux console", []string{"PATH=" + dirs["empty"], "TERM=linux"}, "", "none", ""},
		{"Apple terminal", []string{
			"PATH=" + dirs["empty"], "TERM=xterm-256color", "TERM_PROGRAM=Apple_Terminal",
		}, "", "none", ""},
		{"GNU screen", []string{"PATH=" + dirs["empty"], "TERM=screen-256color"}, "", "none", ""},
		{
			"tmux passing the sequences on", append([]string{"PATH=" + dirs["empty"]}, tmuxEnv...),
			"on", "osc", "",
		},
		{
			"tmux keeping the sequences", append([]string{"PATH=" + dirs["empty"]}, tmuxEnv...),
			"external", "none", "",
		},
		{
			"tmux that does not answer", append([]string{"PATH=" + dirs["empty"]}, tmuxEnv...), "",
			"none", "",
		},
		{"a program inside tmux", append([]string{
			"PATH=" + dirs["xsel"], "DISPLAY=:0",
		}, tmuxEnv...), "off", "tool", "xsel"},
	}

	for _, tt := range tests {
		option := func() string { return tt.option }

		route, tool := ClipboardRoute(envLookup(tt.env), option)
		if route != tt.route || tool != tt.tool {
			t.Errorf("%s: ClipboardRoute = %q, %q, want %q, %q", tt.name, route, tool, tt.route,
				tt.tool)
		}
	}
}

// TestClipboardRouteTmuxQuery checks that the option of tmux is asked only inside tmux and only
// when no program can take the text.
//
// Parameters:
//   - t: test to report to
func TestClipboardRouteTmuxQuery(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "xclip", false)

	tests := []struct {
		name string
		env  []string
		want int
	}{
		{"outside tmux", []string{"PATH=" + dir, "TERM=xterm-256color"}, 0},
		{"inside tmux", []string{"PATH=" + dir, "TERM=tmux-256color", "TMUX=x"}, 1},
		{"a program was found", []string{
			"PATH=" + dir, "DISPLAY=:0", "TERM=tmux-256color", "TMUX=x",
		}, 0},
	}

	for _, tt := range tests {
		asked := 0
		option := func() string { asked++; return "on" }

		ClipboardRoute(envLookup(tt.env), option)

		if asked != tt.want {
			t.Errorf("%s: tmux asked %d times, want %d", tt.name, asked, tt.want)
		}
	}
}
