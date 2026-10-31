package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// styleOf returns a style that has the color of a tone as foreground.
//
// Parameters:
//   - t: tone to apply
//   - p: color profile
func styleOf(t tone, p colorprofile.Profile) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.color(p))
}

// TestNewThemeProfiles checks, for every profile and scheme, which colors the theme uses.
//
// Parameters:
//   - t: test to report to
func TestNewThemeProfiles(t *testing.T) {
	profiles := []colorprofile.Profile{
		colorprofile.NoTTY, colorprofile.ASCII, colorprofile.ANSI, colorprofile.ANSI256,
		colorprofile.TrueColor,
	}

	for _, p := range profiles {
		for _, mode := range []themeMode{modeNeutral, modeDark, modeLight} {
			th := newTheme(mode, p)

			if !colors(p) && th.Mode != modeNeutral {
				t.Errorf("profile %v: mode %d kept without colors", p, th.Mode)
			}

			styles := []lipgloss.Style{
				th.Text, th.Muted, th.Bold, th.Title, th.Marker,
				th.Border, th.Selected, th.Err, th.OK, th.Warn, th.Key,
			}

			for i, st := range styles {
				out := st.Render("x")

				if !strings.Contains(out, "x") {
					t.Errorf("profile %v mode %d style %d lost its text: %q", p, mode, i, out)
				}

				if th.Mode == modeNeutral && strings.Contains(out, "38;") {
					t.Errorf("profile %v: neutral style %d draws a color: %q", p, i, out)
				}
			}

			if th.Mode == modeNeutral && !strings.Contains(th.Selected.Render("x"), "\x1b[") {
				t.Errorf("profile %v: the neutral selection has no attribute", p)
			}
		}
	}
}

// TestNewThemeColors checks that each profile gets its own explicit value of a tone.
//
// Parameters:
//   - t: test to report to
func TestNewThemeColors(t *testing.T) {
	tests := []struct {
		name string
		mode themeMode
		pal  palette
	}{{"dark", modeDark, darkPalette}, {"light", modeLight, lightPalette}}

	for _, tt := range tests {
		for _, p := range []colorprofile.Profile{
			colorprofile.ANSI, colorprofile.ANSI256, colorprofile.TrueColor,
		} {
			th := newTheme(tt.mode, p)

			if got, want := th.Err.Render("x"), styleOf(tt.pal.err, p).Render("x"); got != want {
				t.Errorf("%s %v: err = %q, want %q", tt.name, p, got, want)
			}

			got, want := th.Muted.Render("x"), styleOf(tt.pal.muted, p).Render("x")
			if got != want {
				t.Errorf("%s %v: muted = %q, want %q", tt.name, p, got, want)
			}
		}
	}

	a := newTheme(modeDark, colorprofile.ANSI256).Err.Render("x")
	b := newTheme(modeDark, colorprofile.TrueColor).Err.Render("x")

	if a == b {
		t.Error("256 colors and true color give the same error style")
	}
}

// TestGradient checks that the gradient exists at true color only.
//
// Parameters:
//   - t: test to report to
func TestGradient(t *testing.T) {
	tc := newTheme(modeDark, colorprofile.TrueColor)
	low := newTheme(modeDark, colorprofile.ANSI256)

	if tc.from == nil || tc.to == nil {
		t.Fatal("true color has no gradient")
	}

	if low.from != nil {
		t.Error("256 colors has a gradient")
	}

	got := tc.gradient("tote")

	if stripStyles(got) != "tote" || strings.Count(got, "38;2;") < 4 {
		t.Errorf("gradient = %q", got)
	}

	if got := low.gradient("tote"); stripStyles(got) != "tote" || strings.Count(got, "38;2;") != 0 {
		t.Errorf("flat title = %q", got)
	}

	if got := tc.gradient(""); stripStyles(got) != "" {
		t.Errorf("empty gradient = %q", got)
	}

	if got := stripStyles(tc.bag("ab", 1, 3)); got != "ab" {
		t.Errorf("bag line = %q", got)
	}

	if got := stripStyles(low.bag("ab", 0, 3)); got != "ab" {
		t.Errorf("flat bag line = %q", got)
	}

	if got := stripStyles(tc.bag("ab", 0, 0)); got != "ab" {
		t.Errorf("bag without lines = %q", got)
	}
}

// TestThemeStaysInItsFile checks that no other file of the package writes a color value.
//
// Parameters:
//   - t: test to report to
func TestThemeStaysInItsFile(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source files: %v", err)
	}

	hex := regexp.MustCompile("#[0-9A-Fa-f]{6}")
	call := "lipgloss." + "Color("

	for _, f := range files {
		if f == "theme.go" {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}

		if hex.Match(data) || strings.Contains(string(data), call) {
			t.Errorf("%s writes a color value; colors belong to theme.go", f)
		}
	}

	if darkPalette.text == lightPalette.text || darkPalette.selBG == lightPalette.selBG {
		t.Error("the dark and light palettes share a tone")
	}
}
