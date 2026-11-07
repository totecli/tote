package plugin

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestFieldRule checks the rule that FieldRule names for each kind of field and value, in the
// order the run applies them.
//
// Parameters:
//   - t: test to report to
func TestFieldRule(t *testing.T) {
	text := Field{ID: "x", Type: "text", MaxLen: 5, Pattern: "^[a-z]+$"}
	need := Field{ID: "x", Type: "text", MaxLen: 5, Required: true}
	choice := Field{ID: "x", Type: "choice", Choices: []string{"a", "b"}}
	flag := Field{ID: "x", Type: "bool", Flag: "--yes"}
	must := Field{ID: "x", Type: "bool", Flag: "--yes", Required: true}

	tests := []struct {
		name  string
		field Field
		raw   string
		want  string
	}{
		{"text ok", text, "abc", ""},
		{"text empty", text, "", ""},
		{"text too long", text, "abcdefg", "max_len"},
		{"text pattern", text, "A1", "pattern"},
		{"text control", text, "a\tb", "control"},
		{"text utf8", text, "a\xffb", "utf8"},
		{"required empty", need, "", "required"},
		{"required given", need, "z", ""},
		{"choice ok", choice, "a", ""},
		{"choice other", choice, "c", "choice"},
		{"bool true", flag, "true", ""},
		{"bool false", flag, "false", ""},
		{"bool word", flag, "maybe", "bool"},
		{"bool required off", must, "false", "required"},
		{"bool required on", must, "true", ""},
		{"huge", text, strings.Repeat("a", 1<<17), "max_len"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FieldRule(tc.field, tc.raw); got != tc.want {
				t.Errorf("FieldRule(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestCheckSettingText checks the values typed in a form for the settings of every one line type.
//
// Parameters:
//   - t: test to report to
func TestCheckSettingText(t *testing.T) {
	m := settingsFor(t)
	by := map[string]Setting{}

	for _, s := range m.Settings {
		by[s.ID] = s
	}

	tests := []struct {
		id   string
		raw  string
		want any
		code string
	}{
		{"owner", "me", "me", ""},
		{"owner", "Me!", nil, RejectPattern},
		{"owner", strings.Repeat("a", 13), nil, RejectLength},
		{"verbose", "true", true, ""},
		{"verbose", "false", false, ""},
		{"verbose", "TRUE", true, ""},
		{"verbose", "on", nil, RejectType},
		{"limit", "20", int64(20), ""},
		{"limit", " 20 ", int64(20), ""},
		{"limit", "0", nil, RejectRange},
		{"limit", "x", nil, RejectType},
		{"mode", "fast", "fast", ""},
		{"mode", "slow", nil, RejectChoice},
		{"where", "~/x", "~/x", ""},
		{"where", "rel/x", nil, RejectPath},
		{"where", "", "", ""},
		{"dir", "/nonexistent-dir-for-test", nil, RejectMissing},
		{"tags", "a", nil, RejectType},
	}

	for _, tc := range tests {
		t.Run(tc.id+" "+tc.raw, func(t *testing.T) {
			got, code := CheckSettingText(by[tc.id], t.TempDir(), tc.raw)
			if code != tc.code || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("= %#v, %q; want %#v, %q", got, code, tc.want, tc.code)
			}
		})
	}
}

// TestCheckSettingItems checks the items of a list as they are typed and as a whole.
//
// Parameters:
//   - t: test to report to
func TestCheckSettingItems(t *testing.T) {
	var tags Setting

	for _, s := range settingsFor(t).Settings {
		if s.ID == "tags" {
			tags = s
		}
	}

	tests := []struct {
		name  string
		items []string
		item  string
		want  string
	}{
		{"fine", []string{"a", "b"}, "ab", ""},
		{"empty item", []string{"a", ""}, "", RejectText},
		{"pattern", []string{"A"}, "A", RejectPattern},
		{"too many", []string{"a", "b", "c", "d"}, "d", ""},
		{"none", nil, "x", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantAll := tc.want
			if tc.name == "too many" {
				wantAll = RejectItems
			}

			if got := CheckSettingItems(tags, tc.items); got != wantAll {
				t.Errorf("CheckSettingItems = %q, want %q", got, wantAll)
			}

			if got := CheckSettingItem(tags, tc.item); got != tc.want {
				t.Errorf("CheckSettingItem(%q) = %q, want %q", tc.item, got, tc.want)
			}
		})
	}
}

// TestSettingText checks the text a form shows for each type of value.
//
// Parameters:
//   - t: test to report to
func TestSettingText(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{"a", "a"}, {true, "true"}, {int64(7), "7"}, {[]string{"a", "b"}, "a, b"}, {nil, ""},
	}

	for _, tc := range tests {
		if got := SettingText(tc.in); got != tc.want {
			t.Errorf("SettingText(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// settingsFile reads plugins/demo.toml of a lab.
//
// Parameters:
//   - t: test to report to
//   - l: the lab
func settingsFile(t *testing.T, l *lab) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(l.root, "config", "plugins", "demo.toml"))
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// TestSaveSettings checks the edit of the settings file: only the keys that change are touched,
// comments stay, and a value equal to the default removes its key.
//
// Parameters:
//   - t: test to report to
func TestSaveSettings(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", settingsManifest))
	l.writeFile(filepath.Join(l.root, "config", "plugins", "demo.toml"),
		"schema = 1\n\n# mine\nowner = \"filed\" # note\nlimit = 30\nother = 1\n")

	reg := l.registry(nil)
	p, _ := reg.Load().Get("demo")

	err := reg.SaveSettings(p, map[string]any{
		"owner": "someone", "limit": int64(31), "verbose": true,
		"tags": []string{"x", "y"}, "mode": "safe",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := settingsFile(t, l)

	for _, want := range []string{
		"# mine", "limit = 31", "verbose = true", "other = 1",
		`mode = "safe"`, `tags = ["x", "y"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("file lacks %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "owner") {
		t.Errorf("a value equal to the default stays in the file:\n%s", got)
	}

	view := reg.Settings(p)
	if v := valueOf(t, view, "limit"); v.Value != int64(31) || v.Source != SourceFile {
		t.Errorf("limit = %+v", v)
	}
}

// TestSaveSettingsCreatesTheFile checks that a plugin without a settings file gets one with the
// schema line, and that saving the defaults of a missing file writes nothing.
//
// Parameters:
//   - t: test to report to
func TestSaveSettingsCreatesTheFile(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", settingsManifest))

	reg := l.registry(nil)
	p, _ := reg.Load().Get("demo")
	file := filepath.Join(l.root, "config", "plugins", "demo.toml")

	if err := reg.SaveSettings(p, map[string]any{"limit": int64(10)}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(file); err == nil {
		t.Error("the default of a missing file created it")
	}

	if err := reg.SaveSettings(p, map[string]any{"limit": int64(11)}); err != nil {
		t.Fatal(err)
	}

	if got := settingsFile(t, l); !strings.HasPrefix(got, "schema = 1\n") ||
		!strings.Contains(got, "limit = 11") {
		t.Errorf("file = %q", got)
	}
}

// TestSaveSettingsRefusals checks what is refused and that nothing is written when it is.
//
// Parameters:
//   - t: test to report to
func TestSaveSettingsRefusals(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
		code   string
		arg    string
	}{
		{"range", map[string]any{"limit": int64(99)}, CodeInputInvalid, RejectRange},
		{"type", map[string]any{"limit": "x"}, CodeInputInvalid, RejectType},
		{"choice", map[string]any{"mode": "slow"}, CodeInputInvalid, RejectChoice},
		{"unknown", map[string]any{"nope": "x"}, CodeInputInvalid, RejectType},
		{
			"items",
			map[string]any{"tags": []string{"a", "b", "c", "d"}},
			CodeInputInvalid,
			RejectItems,
		},
		{
			"one bad one good",
			map[string]any{"limit": int64(20), "owner": "No!"},
			CodeInputInvalid,
			RejectPattern,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := newLab(t)
			l.install("demo", demoFiles("demo", settingsManifest))

			reg := l.registry(nil)
			p, _ := reg.Load().Get("demo")

			e := asError(t, reg.SaveSettings(p, tc.values))
			if e.Code != tc.code || e.Args[len(e.Args)-1] != tc.arg {
				t.Errorf("error = %+v, want %s %s", e, tc.code, tc.arg)
			}

			file := filepath.Join(l.root, "config", "plugins", "demo.toml")
			if _, err := os.Stat(file); err == nil {
				t.Error("a refused save wrote the file")
			}
		})
	}
}

// TestSaveSettingsBrokenFile checks that a file that does not parse is refused and left alone,
// and that a plugin that changed, was removed or called tote itself is not written for.
//
// Parameters:
//   - t: test to report to
func TestSaveSettingsBrokenFile(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", settingsManifest))

	file := filepath.Join(l.root, "config", "plugins", "demo.toml")
	l.writeFile(file, "schema = 1\nlimit = [\n")

	reg := l.registry(nil)
	p, _ := reg.Load().Get("demo")

	e := asError(t, reg.SaveSettings(p, map[string]any{"limit": int64(12)}))
	if e.Code != CodeSettingsFile || e.Args[0] != RejectSyntax {
		t.Errorf("error = %+v", e)
	}

	if got := settingsFile(t, l); got != "schema = 1\nlimit = [\n" {
		t.Errorf("the broken file was changed: %q", got)
	}
}

// TestSaveSettingsStaleAndGuard checks the refusals that do not depend on the values.
//
// Parameters:
//   - t: test to report to
func TestSaveSettingsStaleAndGuard(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", settingsManifest))

	reg := l.registry(nil)
	p, _ := reg.Load().Get("demo")

	changed := p
	changed.Record.TreeSHA256 = strings.Repeat("0", 64)

	twelve := map[string]any{"limit": int64(12)}

	if e := asError(t, reg.SaveSettings(changed, twelve)); e.Code != CodeStale {
		t.Errorf("a plugin that changed: %+v", e)
	}

	if e := asError(t, reg.SaveSettings(Plugin{ID: "../x"}, nil)); e.Code != CodeBadID {
		t.Errorf("a bad id: %+v", e)
	}

	if e := asError(t, reg.SaveSettings(Plugin{ID: "other"}, nil)); e.Code != CodeNotInstalled {
		t.Errorf("a plugin that is not installed: %+v", e)
	}

	l.env["TOTE_PLUGIN_ID"] = "demo"

	if e := asError(t, reg.SaveSettings(p, twelve)); e.Code != CodeFromPlugin {
		t.Errorf("a call from a plugin: %+v", e)
	}
}

// TestSaveSettingsWidensInt checks that an int that is not an int64 is taken as the number it is.
//
// Parameters:
//   - t: test to report to
func TestSaveSettingsWidensInt(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", settingsManifest))

	reg := l.registry(nil)
	p, _ := reg.Load().Get("demo")

	if err := reg.SaveSettings(p, map[string]any{"limit": 12}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	if got := settingsFile(t, l); !strings.Contains(got, "limit = 12") {
		t.Errorf("file = %q, want limit = 12", got)
	}
}
