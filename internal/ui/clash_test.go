package ui

import (
	"strings"
	"testing"
)

// clashRig builds a rig on the settings screen with the cursor on the theme row.
//
// Parameters:
//   - t: running test
//   - theme: value of ui.theme
//   - lang: language of the interface, "en" or "fr"
//   - colors: value of TOTE_COLOR
func clashRig(t *testing.T, theme, lang, colors string) *rig {
	t.Helper()

	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartConfig
		o.Width, o.Height = 100, 40
		o.Printer = newPrinter(lang)
		o.Settings.UI.Theme = theme
		o.Environ = append(o.Environ, "TOTE_COLOR="+colors, "TOTE_LANG="+lang)
	}))

	g.press("down", "down", "down", "down", "down", "down")

	return g
}

// clashTexts are the warnings of each scheme, in each language.
var clashTexts = map[string]map[string]string{
	"en": {
		"dark":  "Dark theme may be unreadable on this light terminal: d goes back to auto.",
		"light": "Light theme may be unreadable on this dark terminal: d goes back to auto.",
	},
	"fr": {
		"dark": "Le th\u00e8me sombre peut \u00eatre illisible sur ce fond clair\u00a0: " +
			"d revient \u00e0 auto.",
		"light": "Le th\u00e8me clair peut \u00eatre illisible sur ce fond sombre\u00a0: " +
			"d revient \u00e0 auto.",
	},
}

// TestThemeClash checks every branch of the warning of the theme row: a fixed scheme against the
// opposite background that the terminal answered shows it, and nothing else does, in both
// languages and whatever the order of the change and of the answer.
//
// Parameters:
//   - t: test to report to
func TestThemeClash(t *testing.T) {
	tests := []struct {
		name   string
		theme  string
		colors string
		answer string
		want   string
	}{
		{"dark on dark", "dark", "truecolor", "dark", ""},
		{"dark on light", "dark", "truecolor", "light", "dark"},
		{"light on dark", "light", "truecolor", "dark", "light"},
		{"light on light", "light", "truecolor", "light", ""},
		{"dark on light, 16 colors", "dark", "16", "light", "dark"},
		{"auto on dark", "auto", "truecolor", "dark", ""},
		{"auto on light", "auto", "truecolor", "light", ""},
		{"mono on dark", "mono", "truecolor", "dark", ""},
		{"mono on light", "mono", "truecolor", "light", ""},
		{"dark on light, no colors", "dark", "none", "light", ""},
		{"light on dark, no colors", "light", "none", "dark", ""},
		{"dark, no answer", "dark", "truecolor", "", ""},
		{"light, no answer", "light", "truecolor", "", ""},
	}

	for _, lang := range []string{"en", "fr"} {
		for _, tt := range tests {
			g := clashRig(t, tt.theme, lang, tt.colors)

			if tt.answer != "" {
				g.send(backgroundMsg(tt.answer == "dark"))
			}

			assertClash(t, g, lang, tt.name, tt.want)
		}
	}
}

// assertClash checks that the settings screen shows the warning for a scheme, or none.
//
// Parameters:
//   - t: running test
//   - g: rig on the theme row
//   - lang: language of the interface
//   - name: name of the case
//   - want: "dark" or "light" for the warning of that scheme, empty for none
func assertClash(t *testing.T, g *rig, lang, name, want string) {
	t.Helper()

	frame := stripStyles(g.r.render())

	for scheme, text := range clashTexts[lang] {
		if has := strings.Contains(frame, text); has != (scheme == want) {
			t.Errorf("%s (%s): warning for %s shown = %v, want %v:\n%s", name, lang, scheme, has,
				scheme == want, frame)
		}
	}
}

// TestThemeClashFollows checks that the warning follows the change of the setting and the arrival
// of the answer, whichever comes first, and that a guess made when the terminal stays silent is
// not an answer.
//
// Parameters:
//   - t: test to report to
func TestThemeClashFollows(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		g := clashRig(t, "dark", lang, "truecolor")
		assertClash(t, g, lang, "dark, nothing answered", "")

		g.send(backgroundMsg(false))
		assertClash(t, g, lang, "answer arrives after the change", "dark")

		g.press("right")
		assertClash(t, g, lang, "light chosen on a light terminal", "")

		g.send(backgroundMsg(true))
		assertClash(t, g, lang, "the terminal turns dark", "light")

		g.press("left")
		assertClash(t, g, lang, "dark chosen on a dark terminal", "")

		g.press("d", "y")
		assertClash(t, g, lang, "default is auto", "")

		if g.r.e.Settings.UI.Theme != "auto" {
			t.Errorf("%s: d left the theme at %q, want auto", lang, g.r.e.Settings.UI.Theme)
		}

		g.press("right", "right")
		assertClash(t, g, lang, "light on a dark terminal, the answer was kept", "light")

		g.press("right")
		assertClash(t, g, lang, "mono", "")
	}
}

// TestThemeClashGuess checks that the guess made when the terminal does not answer in time never
// raises the warning.
//
// Parameters:
//   - t: test to report to
func TestThemeClashGuess(t *testing.T) {
	g := clashRig(t, "dark", "en", "truecolor")
	g.r.e.Environ = append(g.r.e.Environ, "COLORFGBG=0;15")

	g.send(backgroundTimeoutMsg{})

	if g.r.e.Backdrop != backdropUnknown {
		t.Errorf("backdrop = %d after a timeout, want unknown", g.r.e.Backdrop)
	}

	assertClash(t, g, "en", "timeout on a light guess", "")

	g.send(backgroundMsg(false))

	if g.r.e.Backdrop != backdropLight {
		t.Errorf("backdrop = %d after the answer, want light", g.r.e.Backdrop)
	}

	assertClash(t, g, "en", "late answer", "dark")
}

// TestThemeClashPlace checks where the warning is shown: on the theme row only, as a fourth line of
// the panel, in the status line when the panel is hidden and in the details.
//
// Parameters:
//   - t: test to report to
func TestThemeClashPlace(t *testing.T) {
	text := clashTexts["en"]["dark"]

	g := clashRig(t, "dark", "en", "truecolor")
	g.send(backgroundMsg(false))

	lines := strings.Split(stripStyles(g.r.render()), "\n")
	at := -1

	for i, l := range lines {
		if strings.Contains(l, text) {
			at = i
		}
	}

	if at < 3 || !strings.HasPrefix(lines[at], "\u2502 "+g.r.e.Glyphs.warn+" ") ||
		!strings.HasPrefix(lines[at+1], "\u2570") || !strings.Contains(lines[at-1], "values: ") {
		t.Errorf("the warning is not the fourth line of the panel (line %d):\n%s", at,
			strings.Join(lines, "\n"))
	}

	g.press("up")
	assertClash(t, g, "en", "another row", "")

	g.press("down", "v")

	if frame := stripStyles(g.r.render()); !strings.Contains(frame, text) {
		t.Errorf("the details lack the warning:\n%s", frame)
	}

	g.press("esc")

	g.r.resize(60, 12)

	if frame := stripStyles(g.r.render()); !strings.Contains(frame, "ui.theme: ! Dark theme may") {
		t.Errorf("the status line lacks the warning:\n%s", frame)
	}
}

// TestThemeDefaultIsAuto checks that the warning is right to say that the reset key goes back to
// auto.
//
// Parameters:
//   - t: test to report to
func TestThemeDefaultIsAuto(t *testing.T) {
	d, _ := findSetting("theme")

	if got := defaultValue(d); got != "auto" {
		t.Errorf("the default theme is %q, the warning says auto", got)
	}
}

// TestThemeDescriptionShown checks how the long description of the theme is shown: the panel cuts
// it with an ellipsis where it does not fit, and the details, opened with the v key, give it whole
// down to a terminal of 60 columns; below it the wrapped lines of a field are capped, as for every
// long description.
//
// Parameters:
//   - t: test to report to
func TestThemeDescriptionShown(t *testing.T) {
	tests := []struct {
		lang string
		end  string
	}{
		{"en", "dark is for a dark terminal, light for a light one."},
		{"fr", "sombre convient \u00e0 un terminal sombre, clair \u00e0 un terminal clair."},
	}

	for _, tt := range tests {
		for _, size := range [][2]int{{60, 20}, {80, 24}, {100, 40}} {
			g := clashRig(t, "dark", tt.lang, "truecolor")
			g.r.resize(size[0], size[1])
			g.press("v")

			frame := strings.ReplaceAll(stripStyles(g.r.render()), "\u2502", " ")
			got := strings.Join(strings.Fields(strings.ReplaceAll(frame, "\u00a0", " ")), " ")
			want := strings.Join(strings.Fields(strings.ReplaceAll(tt.end, "\u00a0", " ")), " ")

			if !strings.Contains(got, want) {
				t.Errorf("%s %v: the details lack %q:\n%s", tt.lang, size, tt.end, frame)
			}
		}
	}
}
