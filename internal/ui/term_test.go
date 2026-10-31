package ui

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// TestEnvLookup checks the lookup over a list of assignments.
//
// Parameters:
//   - t: test to report to
func TestEnvLookup(t *testing.T) {
	get := envLookup([]string{"A=1", "B=", "C=x=y", "bad", "A=2"})

	tests := []struct{ key, want string }{
		{"A", "2"}, {"B", ""}, {"C", "x=y"}, {"bad", ""}, {"missing", ""},
	}

	for _, tt := range tests {
		if got := get(tt.key); got != tt.want {
			t.Errorf("get(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

// TestParseColorName checks the accepted names of TOTE_COLOR.
//
// Parameters:
//   - t: test to report to
func TestParseColorName(t *testing.T) {
	tests := []struct {
		in   string
		want colorprofile.Profile
		ok   bool
	}{
		{"truecolor", colorprofile.TrueColor, true},
		{"24BIT", colorprofile.TrueColor, true},
		{" true ", colorprofile.TrueColor, true},
		{"256", colorprofile.ANSI256, true},
		{"ansi256", colorprofile.ANSI256, true},
		{"16", colorprofile.ANSI, true},
		{"ansi", colorprofile.ANSI, true},
		{"none", colorprofile.ASCII, true},
		{"mono", colorprofile.ASCII, true},
		{"0", colorprofile.ASCII, true},
		{"", colorprofile.Unknown, false},
		{"rainbow", colorprofile.Unknown, false},
	}

	for _, tt := range tests {
		got, ok := parseColorName(tt.in)

		if got != tt.want || ok != tt.ok {
			t.Errorf("parseColorName(%q) = %v, %v, want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// TestDetectProfile checks the order of the rules that pick the color profile.
//
// Parameters:
//   - t: test to report to
func TestDetectProfile(t *testing.T) {
	tests := []struct {
		name  string
		env   []string
		probe func() string
		want  colorprofile.Profile
	}{
		{
			"override wins",
			[]string{"TOTE_COLOR=256", "NO_COLOR=1", "TERM=xterm"},
			nil,
			colorprofile.ANSI256,
		},
		{
			"override none",
			[]string{"TOTE_COLOR=none", "COLORTERM=truecolor"},
			nil,
			colorprofile.ASCII,
		},
		{"no color", []string{"NO_COLOR=1", "COLORTERM=truecolor"}, nil, colorprofile.ASCII},
		{
			"empty no color is ignored",
			[]string{"NO_COLOR=", "TERM=dumb"},
			nil,
			colorprofile.NoTTY,
		},
		{"not a terminal", []string{"TERM=xterm-256color"}, nil, colorprofile.NoTTY},
		{
			"unknown override falls through",
			[]string{"TOTE_COLOR=x", "NO_COLOR=1"},
			nil,
			colorprofile.ASCII,
		},
		{"tmux without probe", []string{"TMUX=/tmp/t,1,0"}, nil, colorprofile.NoTTY},
	}

	for _, tt := range tests {
		got := detectProfile(tt.env, &bytes.Buffer{}, tt.probe)

		if got != tt.want {
			t.Errorf("%s: detectProfile = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestHasFeature checks the search in a feature list.
//
// Parameters:
//   - t: test to report to
func TestHasFeature(t *testing.T) {
	tests := []struct {
		list string
		want bool
	}{
		{"256,RGB,mouse", true},
		{"RGB", true},
		{"256 RGB", true},
		{"RGBA,256", false},
		{"", false},
		{"rgb", false},
	}

	for _, tt := range tests {
		if got := hasFeature(tt.list, "RGB"); got != tt.want {
			t.Errorf("hasFeature(%q) = %v, want %v", tt.list, got, tt.want)
		}
	}
}

// TestUTF8Locale checks which locale variable decides.
//
// Parameters:
//   - t: test to report to
func TestUTF8Locale(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want bool
	}{
		{"none", nil, false},
		{"lang utf8", []string{"LANG=fr_FR.UTF-8"}, true},
		{"lang utf8 no dash", []string{"LANG=en_US.utf8"}, true},
		{"lang latin", []string{"LANG=fr_FR.ISO-8859-1"}, false},
		{"C", []string{"LANG=C"}, false},
		{"POSIX", []string{"LANG=POSIX"}, false},
		{"lc_all wins", []string{"LC_ALL=C", "LANG=en_US.UTF-8"}, false},
		{"lc_ctype before lang", []string{"LC_CTYPE=en_US.UTF-8", "LANG=C"}, true},
	}

	for _, tt := range tests {
		if got := utf8Locale(envLookup(tt.env)); got != tt.want {
			t.Errorf("%s: utf8Locale = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestUseASCII checks when the ASCII glyphs are chosen.
//
// Parameters:
//   - t: test to report to
func TestUseASCII(t *testing.T) {
	utf := "LANG=en_US.UTF-8"

	tests := []struct {
		name   string
		forced bool
		env    []string
		want   bool
	}{
		{"forced", true, []string{utf}, true},
		{"utf8 xterm", false, []string{utf, "TERM=xterm"}, false},
		{"linux console", false, []string{utf, "TERM=linux"}, true},
		{"dumb", false, []string{utf, "TERM=dumb"}, true},
		{"no locale", false, []string{"TERM=xterm"}, true},
	}

	for _, tt := range tests {
		if got := useASCII(tt.forced, envLookup(tt.env)); got != tt.want {
			t.Errorf("%s: useASCII = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestColors checks which profiles can show colors.
//
// Parameters:
//   - t: test to report to
func TestColors(t *testing.T) {
	for p, want := range map[colorprofile.Profile]bool{
		colorprofile.NoTTY: false, colorprofile.ASCII: false, colorprofile.ANSI: true,
		colorprofile.ANSI256: true, colorprofile.TrueColor: true,
	} {
		if got := colors(p); got != want {
			t.Errorf("colors(%v) = %v, want %v", p, got, want)
		}
	}
}

// TestTmuxFeatures checks the query with a fake tmux first in the path, and without any.
//
// Parameters:
//   - t: test to report to
func TestTmuxFeatures(t *testing.T) {
	dir := t.TempDir()

	t.Setenv("PATH", dir)

	if got := tmuxFeatures(); got != "" {
		t.Errorf("without tmux = %q, want empty", got)
	}

	script := filepath.Join(dir, "tmux")

	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'RGB,mouse'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	if got := tmuxFeatures(); got != "RGB,mouse" {
		t.Errorf("with a fake tmux = %q, want RGB,mouse", got)
	}
}

// TestGlyphs checks that the ASCII set holds nothing but ASCII and that every field is set.
//
// Parameters:
//   - t: test to report to
func TestGlyphs(t *testing.T) {
	for _, ascii := range []bool{true, false} {
		g := newGlyphs(ascii)
		fields := []string{
			g.marker, g.ellipsis, g.sep, g.upDown, g.enter, g.arrow, g.cursor,
			g.warn, g.prompt, g.crumb, g.tl, g.tr, g.bl, g.br, g.h, g.v,
		}
		fields = append(fields, g.logo[:]...)

		for i, f := range fields {
			if f == "" {
				t.Errorf("ascii %v: glyph %d is empty", ascii, i)
			}

			if ascii && !isPlain(f) {
				t.Errorf("ascii set holds %q", f)
			}
		}

		if g.ascii != ascii {
			t.Errorf("newGlyphs(%v).ascii = %v", ascii, g.ascii)
		}
	}
}

// TestColorNames checks the values of TOTE_COLOR, and that the words for "no color" give the
// plain profile even when the terminal could show colors.
//
// Parameters:
//   - t: test to report to
func TestColorNames(t *testing.T) {
	tests := []struct {
		name string
		want colorprofile.Profile
		ok   bool
	}{
		{"truecolor", colorprofile.TrueColor, true},
		{"24bit", colorprofile.TrueColor, true},
		{"256", colorprofile.ANSI256, true},
		{"16", colorprofile.ANSI, true},
		{"none", colorprofile.ASCII, true},
		{"mono", colorprofile.ASCII, true},
		{"0", colorprofile.ASCII, true},
		{"never", colorprofile.ASCII, true},
		{"off", colorprofile.ASCII, true},
		{"no", colorprofile.ASCII, true},
		{"false", colorprofile.ASCII, true},
		{" Never ", colorprofile.ASCII, true},
		{"OFF", colorprofile.ASCII, true},
		{"always", colorprofile.Unknown, false},
		{"auto", colorprofile.Unknown, false},
		{"on", colorprofile.Unknown, false},
		{"1", colorprofile.Unknown, false},
		{"", colorprofile.Unknown, false},
		{"rainbow", colorprofile.Unknown, false},
	}

	for _, tt := range tests {
		got, ok := parseColorName(tt.name)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseColorName(%q) = %v, %v; want %v, %v", tt.name, got, ok, tt.want,
				tt.ok)
		}
	}

	for _, v := range []string{"never", "off", "no", "false", "0"} {
		env := []string{"TERM=xterm-256color", "COLORTERM=truecolor", "TOTE_COLOR=" + v}
		if got := detectProfile(env, io.Discard, nil); got != colorprofile.ASCII {
			t.Errorf("TOTE_COLOR=%s gives %v, want the plain profile", v, got)
		}
	}

	// An unknown value falls back to the detection, as the warning on stderr says.
	env := []string{
		"TERM=xterm-256color", "COLORTERM=truecolor", "TOTE_COLOR=never!",
		"CLICOLOR_FORCE=1",
	}
	if got := detectProfile(env, io.Discard, nil); got != colorprofile.TrueColor {
		t.Errorf("an unknown value gives %v, want the detected true color", got)
	}
}

// TestColorWords checks the words of TOTE_COLOR that choose how the colors are detected: auto
// keeps the detection and NO_COLOR, always detects without NO_COLOR and gives 256 colors when
// nothing is detected, and neither is reported as an unknown value.
//
// Parameters:
//   - t: test to report to
func TestColorWords(t *testing.T) {
	rich := []string{"TERM=xterm-256color", "COLORTERM=truecolor", "CLICOLOR_FORCE=1"}

	tests := []struct {
		name string
		env  []string
		want colorprofile.Profile
	}{
		{"auto", append([]string{"TOTE_COLOR=auto"}, rich...), colorprofile.TrueColor},
		{
			"auto and NO_COLOR", append([]string{"TOTE_COLOR=auto", "NO_COLOR=1"}, rich...),
			colorprofile.ASCII,
		},
		{"always", append([]string{"TOTE_COLOR=always"}, rich...), colorprofile.TrueColor},
		{
			"always and NO_COLOR", append([]string{"TOTE_COLOR=always", "NO_COLOR=1"}, rich...),
			colorprofile.TrueColor,
		},
		{
			"always on a dumb terminal",
			[]string{"TOTE_COLOR=always", "TERM=dumb"},
			colorprofile.ANSI256,
		},
		{"on", append([]string{"TOTE_COLOR=on"}, rich...), colorprofile.TrueColor},
		{"yes, in capitals", []string{"TOTE_COLOR=YES", "TERM=dumb"}, colorprofile.ANSI256},
		{"one", []string{"TOTE_COLOR=1", "TERM=dumb"}, colorprofile.ANSI256},
		{"a profile wins", append([]string{"TOTE_COLOR=16"}, rich...), colorprofile.ANSI},
	}

	for _, tt := range tests {
		if got := detectProfile(tt.env, io.Discard, nil); got != tt.want {
			t.Errorf("%s: detectProfile = %v, want %v", tt.name, got, tt.want)
		}

		if v, bad := UnknownColor(tt.env); bad {
			t.Errorf("%s: reported as unknown (%q)", tt.name, v)
		}
	}
}
