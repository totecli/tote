package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/i18n"
)

// TestSettingScopes pins, for every row of the registry, when a change takes effect, and checks
// that the words for it exist in both languages and differ.
//
// Parameters:
//   - t: test to report to
func TestSettingScopes(t *testing.T) {
	want := map[string]settingApply{
		"lang": applyNow, "editor": applyNow, "repos_root": applyNow, "after_disconnect": applyNow,
		"keepalive": applyNow, "after_open": applyNow, "theme": applyNow, "welcome": applyNow,
		"compact": applyNow, "width": applyNow, "mouse": applyNow, "check": applyRestart,
		"colors": applyNone, "update": applyNone,
	}

	if len(want) != len(settingDefs) {
		t.Fatalf("%d scopes pinned for %d rows", len(want), len(settingDefs))
	}

	words := map[string]map[settingApply]string{}

	for _, lang := range []string{"en", "fr"} {
		e := bareEnv(t, false)
		e.Tr = i18n.New(lang)
		words[lang] = map[settingApply]string{}

		for _, d := range settingDefs {
			scope, ok := want[d.id]
			if !ok {
				t.Errorf("setting %q has no pinned scope", d.id)

				continue
			}

			if d.apply != scope {
				t.Errorf("setting %q: scope %d, want %d", d.id, d.apply, scope)
			}

			got := d.applyText(e)

			if (got == "") != (scope == applyNone) {
				t.Errorf("%s %s: text %q for scope %d", lang, d.id, got, scope)
			}

			words[lang][d.apply] = got
		}
	}

	for _, lang := range []string{"en", "fr"} {
		if words[lang][applyNow] == words[lang][applyRestart] {
			t.Errorf("%s: both scopes are worded %q", lang, words[lang][applyNow])
		}
	}
}

// TestSettingScopeHasOneSource checks that no code but the registry words the scope of a setting:
// the catalog keys of the scope are read in settings.go only.
//
// Parameters:
//   - t: test to report to
func TestSettingScopeHasOneSource(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "settings.go" {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(string(data), "ui_apply_") {
			t.Errorf("%s words the scope of a setting; only settings.go may", f)
		}
	}
}

// inFrench switches the language of a rig on the settings screen to French through the screen.
//
// Parameters:
//   - g: rig on the settings screen
func inFrench(g *rig) {
	g.t.Helper()

	selectRow(g, "lang")
	g.press("enter", "end", "enter")
	g.await("the language", func() bool { return g.r.e.Tr.Lang() == "fr" })
}

// TestSettingScopeShown checks that the scope is in the toast of a change and of a reset, and in
// the muted hint under the list, in English and in French.
//
// Parameters:
//   - t: test to report to
func TestSettingScopeShown(t *testing.T) {
	tests := []struct {
		name string
		lang string
		id   string
		keys []string
		want string
	}{
		{
			"change applied now", "en", "after_disconnect",
			[]string{"enter", "up", "enter"},
			"saved: After a connection (applied now)",
		},
		{
			"switch after restart", "en", "check",
			[]string{"enter"},
			"saved: Update check at start (after restart)",
		},
		{
			"reset after restart", "en", "check",
			[]string{"d", "y"},
			"Update check at start is back to its default (after restart)",
		},
		{
			"change in French", "fr", "after_disconnect",
			[]string{"enter", "up", "enter"},
			"appliqu\u00e9 tout de suite",
		},
		{"switch in French", "fr", "check", []string{"enter"}, "apr\u00e8s red\u00e9marrage"},
	}

	for _, tt := range tests {
		g, _ := configRig(t, fixtureConfig)

		if tt.lang == "fr" {
			inFrench(g)
		}

		selectRow(g, tt.id)

		if tt.name == "reset after restart" {
			g.press("enter")
			g.await("the switch", func() bool { return !g.r.e.Settings.Update.Check })
		}

		g.press(tt.keys...)
		g.await(tt.name, func() bool { return strings.Contains(g.toastText(), tt.want) })
	}

	for _, tt := range []struct {
		lang, id, want string
	}{
		{"en", "theme", "applied now"},
		{"en", "check", "after restart"},
		{"fr", "theme", "appliqu\u00e9 tout de suite"},
		{"fr", "check", "apr\u00e8s red\u00e9marrage"},
	} {
		g, _ := configRig(t, fixtureConfig)

		if tt.lang == "fr" {
			inFrench(g)
		}

		selectRow(g, tt.id)

		if text := strings.Join(strings.Fields(g.text()), " "); !strings.Contains(text, tt.want) {
			t.Errorf("%s %s: the panel lacks %q:\n%s", tt.lang, tt.id, tt.want, g.text())
		}
	}

	g, _ := configRig(t, fixtureConfig)
	selectRow(g, "update")

	for _, scope := range []string{"applied now", "after restart"} {
		if strings.Contains(g.text(), scope) {
			t.Errorf("a row that is not stored names a scope:\n%s", g.text())
		}
	}
}
