package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// writeConfig writes content to a config.toml in a temporary directory.
//
// Parameters:
//   - t: test that owns the temporary directory
//   - content: TOML text to store
//
// Returns the path of the file.
func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// TestDefaultSettings checks every default value.
//
// Parameters:
//   - t: test to report to
func TestDefaultSettings(t *testing.T) {
	got := DefaultSettings()
	want := Settings{
		Schema:  1,
		Connect: ConnectConfig{PrefixMin: 2, AfterDisconnect: "menu", Keepalive: true},
		Repos:   ReposConfig{AfterOpen: "return"},
		Update:  UpdateConfig{Check: true},
		UI: UIConfig{
			Theme: "auto", Welcome: "box", Compact: "auto", Width: "auto",
			Mouse: true,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultSettings() = %+v, want %+v", got, want)
	}
	if got.Shell.CompatMN {
		t.Error("CompatMN default is true, want false")
	}
	if err := got.Validate(); err != nil {
		t.Errorf("defaults do not validate: %v", err)
	}
}

// TestLoadSettingsMissing checks that a missing file yields the defaults.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsMissing(t *testing.T) {
	got, err := LoadSettings(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}
	if !reflect.DeepEqual(got, DefaultSettings()) {
		t.Errorf("LoadSettings() = %+v, want the defaults", got)
	}
}

// TestLoadSettingsUnreadable checks the error for a path that is a directory.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsUnreadable(t *testing.T) {
	_, err := LoadSettings(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "read ") {
		t.Errorf("LoadSettings() error = %v, want a read error", err)
	}
}

// TestLoadSettingsFull checks a complete file and the defaults of absent keys.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsFull(t *testing.T) {
	full := writeConfig(t, `
schema = 1
lang = "fr"
editor = "code -w"
repos_root = "~/src"
[connect]
prefix_min = 3
after_disconnect = "exit"
keepalive = false
[repos]
after_open = "cd"
[shell]
compat_mn = true
extra_names = ["mn", "t"]
[update]
check = false
[ui]
theme = "mono"
welcome = "off"
compact = "never"
width = "160"
mouse = false
`)
	got, err := LoadSettings(full)
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}
	want := Settings{
		Schema: 1, Lang: "fr", Editor: "code -w", ReposRoot: "~/src",
		Connect: ConnectConfig{PrefixMin: 3, AfterDisconnect: "exit", Keepalive: false},
		Repos:   ReposConfig{AfterOpen: "cd"},
		Shell:   ShellConfig{CompatMN: true, ExtraNames: []string{"mn", "t"}},
		Update:  UpdateConfig{Check: false},
		UI:      UIConfig{Theme: "mono", Welcome: "off", Compact: "never", Width: "160"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadSettings() = %+v, want %+v", got, want)
	}

	partial := writeConfig(t, "[ui]\ntheme = \"dark\"\n")
	got, err = LoadSettings(partial)
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}
	want = DefaultSettings()
	want.UI.Theme = "dark"
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadSettings() = %+v, want %+v", got, want)
	}
}

// TestWidthSetting checks ui.width: the default, an absent key, every value the screen offers,
// the refusal of the others, and that a value written by the store reads back.
//
// Parameters:
//   - t: test to report to
func TestWidthSetting(t *testing.T) {
	if got := DefaultSettings().UI.Width; got != "auto" {
		t.Errorf("default ui.width = %q, want \"auto\"", got)
	}

	absent := []string{"", "[ui]\ntheme = \"dark\"\n", "lang = \"en\"\n"}
	for _, content := range absent {
		got, err := LoadSettings(writeConfig(t, content))
		if err != nil || got.UI.Width != "auto" {
			t.Errorf("file %q: ui.width = %q, error %v, want \"auto\"", content, got.UI.Width, err)
		}
	}

	for _, v := range []string{"auto", "120", "160"} {
		got, err := LoadSettings(writeConfig(t, "[ui]\nwidth = \""+v+"\"\n"))
		if err != nil || got.UI.Width != v {
			t.Errorf("width %q: read %q, error %v", v, got.UI.Width, err)
		}

		path := filepath.Join(t.TempDir(), "config.toml")
		if err := store.PatchTable(path, "ui", map[string]any{"width": v}, nil); err != nil {
			t.Fatalf("width %q: PatchTable() error = %v", v, err)
		}

		got, err = LoadSettings(path)
		if err != nil || got.UI.Width != v {
			t.Errorf("width %q: round trip read %q, error %v", v, got.UI.Width, err)
		}
	}

	for _, v := range []string{"", "80", "140", "FULL", "full", "auto ", "AUTO", "0"} {
		s := DefaultSettings()
		s.UI.Width = v

		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), "ui.width") ||
			!strings.Contains(err.Error(), "auto, 120, 160") {
			t.Errorf("width %q: Validate() error = %v, want ui.width and its allowed values", v,
				err)
		}
	}
}

// TestAfterDisconnect checks that the interface returns to the list unless the file says "exit".
//
// Parameters:
//   - t: test to report to
func TestAfterDisconnect(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"missing key", "[connect]\nprefix_min = 3\n", "menu"},
		{"no table", "lang = \"en\"\n", "menu"},
		{"menu", "[connect]\nafter_disconnect = \"menu\"\n", "menu"},
		{"exit", "[connect]\nafter_disconnect = \"exit\"\n", "exit"},
	}

	for _, tt := range tests {
		got, err := LoadSettings(writeConfig(t, tt.content))
		if err != nil {
			t.Fatalf("%s: LoadSettings() error = %v", tt.name, err)
		}

		if got.Connect.AfterDisconnect != tt.want {
			t.Errorf("%s: after_disconnect = %q, want %q", tt.name, got.Connect.AfterDisconnect,
				tt.want)
		}
	}
}

// TestLoadSettingsErrors checks the faults that refuse the whole file: the schema and syntax.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		line    int
		want    []string
	}{
		{"schema 2", "lang = \"en\"\nschema = 2\n", 2, []string{"schema is 2", "only schema 1"}},
		{"schema 0", "schema = 0\n", 1, []string{"schema is 0"}},
		{"schema is a string", "schema = \"1\"\n", 1, []string{"schema must be the integer 1"}},
		{"schema 2 with an unknown key", "schema = 2\nfuture = 1\n", 1, []string{"schema is 2"}},
		{
			"syntax error carries its position", "schema = 1\n[ui\ntheme = \"auto\"\n", 2,
			[]string{"config.toml:2:4:"},
		},
		{"bad value syntax", "[ui]\ntheme = \n", 2, []string{"config.toml:2:"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, warns, err := LoadSettingsWarn(writeConfig(t, tt.content))
			checkErrorContains(t, err, tt.want)

			var pe *ParseError
			if !errors.As(err, &pe) || pe.Line != tt.line {
				t.Errorf("error %#v, want a ParseError at line %d", err, tt.line)
			}
			if len(warns) != 0 {
				t.Errorf("warnings %v with a refused file", warns)
			}
		})
	}
}

// TestLoadSettingsWarnings checks that an unknown key, a wrong type or a value that is not allowed
// is a warning with its line, that the key keeps its default and that the other keys apply.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsWarnings(t *testing.T) {
	type warn struct {
		key  string
		line int
		kind string
	}

	unknown, invalid := WarnUnknownKey, WarnInvalidValue

	tests := []struct {
		name    string
		content string
		want    []warn
		check   func(Settings) bool
	}{
		{
			name:    "a table that 1.1 may add",
			content: "schema = 1\n[shell]\nbind = true\ncompat_mn = true\n",
			want:    []warn{{"shell.bind", 3, unknown}},
			check:   func(s Settings) bool { return s.Shell.CompatMN },
		},
		{
			name:    "unknown keys are all named in file order",
			content: "colour = 1\n[ui]\nthem = \"dark\"\nmouse = false\n[extra]\nx = 1\n",
			want:    []warn{{"colour", 1, unknown}, {"ui.them", 3, unknown}, {"extra", 5, unknown}},
			check:   func(s Settings) bool { return !s.UI.Mouse },
		},
		{
			name:    "dotted unknown key",
			content: "ui.future = 1\n",
			want:    []warn{{"ui.future", 1, unknown}},
			check:   func(s Settings) bool { return s.UI.Theme == "auto" },
		},
		{
			name:    "unknown enum value keeps the default",
			content: "[ui]\ntheme = \"neon\"\nwelcome = \"off\"\n",
			want:    []warn{{"ui.theme", 2, invalid}},
			check:   func(s Settings) bool { return s.UI.Theme == "auto" && s.UI.Welcome == "off" },
		},
		{
			name: "every enum",
			content: "[connect]\nafter_disconnect = \"stay\"\n[repos]\nafter_open = \"kill\"\n" +
				"[ui]\ncompact = \"x\"\nwidth = \"200\"\nwelcome = \"big\"\n",
			want: []warn{
				{"connect.after_disconnect", 2, invalid},
				{"repos.after_open", 4, invalid},
				{"ui.compact", 6, invalid},
				{"ui.width", 7, invalid},
				{"ui.welcome", 8, invalid},
			},
			check: func(s Settings) bool { return reflect.DeepEqual(s, DefaultSettings()) },
		},
		{
			name: "wrong types keep the defaults",
			content: "[ui]\nwidth = 160\nmouse = \"yes\"\n[connect]\nkeepalive = 1\n" +
				"prefix_min = \"two\"\n",
			want: []warn{
				{"ui.width", 2, invalid},
				{"ui.mouse", 3, invalid},
				{"connect.keepalive", 5, invalid},
				{"connect.prefix_min", 6, invalid},
			},
			check: func(s Settings) bool { return reflect.DeepEqual(s, DefaultSettings()) },
		},
		{
			name:    "prefix_min below 1",
			content: "[connect]\nprefix_min = 0\n",
			want:    []warn{{"connect.prefix_min", 2, invalid}},
			check:   func(s Settings) bool { return s.Connect.PrefixMin == 2 },
		},
		{
			name:    "prefix_min above the longest name",
			content: "[connect]\nprefix_min = 99999\n",
			want:    []warn{{"connect.prefix_min", 2, invalid}},
			check:   func(s Settings) bool { return s.Connect.PrefixMin == 2 },
		},
		{
			name:    "prefix_min at the longest name",
			content: "[connect]\nprefix_min = 64\n",
			check:   func(s Settings) bool { return s.Connect.PrefixMin == 64 },
		},
		{
			name:    "unknown language",
			content: "lang = \"xx\"\n",
			want:    []warn{{"lang", 1, invalid}},
			check:   func(s Settings) bool { return s.Lang == "" },
		},
		{
			name:    "known languages, written as a locale or in capitals",
			content: "lang = \"fr_FR.UTF-8\"\n",
			check:   func(s Settings) bool { return s.Lang == "fr_FR.UTF-8" },
		},
		{
			name:    "language in capitals",
			content: "lang = \"EN\"\n",
			check:   func(s Settings) bool { return s.Lang == "EN" },
		},
		{
			name:    "a table in place of a value and a value in place of a table",
			content: "lang = { x = 1 }\nui = 3\n",
			want:    []warn{{"lang", 1, invalid}, {"ui", 2, invalid}},
			check:   func(s Settings) bool { return reflect.DeepEqual(s, DefaultSettings()) },
		},
		{
			name:    "hostile extra names are dropped with the whole key",
			content: "[shell]\nextra_names = [\"ok\", \"x; rm\", \"-n\"]\ncompat_mn = true\n",
			want:    []warn{{"shell.extra_names", 2, invalid}},
			check:   func(s Settings) bool { return s.Shell.ExtraNames == nil && s.Shell.CompatMN },
		},
		{
			name:    "extra names of the wrong type",
			content: "[shell]\nextra_names = [1]\n",
			want:    []warn{{"shell.extra_names", 2, invalid}},
			check:   func(s Settings) bool { return s.Shell.ExtraNames == nil },
		},
		{
			name: "plugin tables",
			content: "[plugins.gft]\nenabled = false\nnote = 1\n[plugins.Bad]\n[plugins.ok]\n" +
				"enabled = \"no\"\n",
			want: []warn{
				{"plugins.gft.note", 3, unknown},
				{"plugins.Bad", 4, invalid},
				{"plugins.ok.enabled", 6, invalid},
			},
			check: func(s Settings) bool {
				_, bad := s.Plugins["Bad"]

				return !s.PluginEnabled("gft") && !s.PluginEnabled("ok") && !bad
			},
		},
		{
			name:    "a plugin entry that is not a table is off",
			content: "[plugins]\ngft = false\n",
			want:    []warn{{"plugins.gft", 2, invalid}},
			check:   func(s Settings) bool { return !s.PluginEnabled("gft") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warns, err := LoadSettingsWarn(writeConfig(t, tt.content))
			if err != nil {
				t.Fatalf("LoadSettingsWarn() error = %v", err)
			}

			var have []warn
			for _, w := range warns {
				have = append(have, warn{w.Key, w.Line, w.Kind})
				if w.Kind == WarnInvalidValue && w.Msg == "" {
					t.Errorf("warning %+v has no reason", w)
				}
			}
			if !reflect.DeepEqual(have, tt.want) {
				t.Errorf("warnings = %v, want %v", have, tt.want)
			}
			if !tt.check(got) {
				t.Errorf("settings = %+v", got)
			}

			again, err := LoadSettings(writeConfig(t, tt.content))
			if err != nil || !reflect.DeepEqual(again, got) {
				t.Errorf("LoadSettings() = %+v, %v; want the same settings", again, err)
			}
		})
	}
}

// checkErrorContains fails the test unless err is non-nil and mentions every expected text.
//
// Parameters:
//   - t: test to report to
//   - err: error returned by LoadSettings
//   - want: texts that the error message must contain
func checkErrorContains(t *testing.T, err error, want []string) {
	t.Helper()

	if err == nil {
		t.Fatal("LoadSettings() error = nil, want an error")
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not contain %q", err, w)
		}
	}
}

// TestValidateJoinsErrors checks that Validate reports several problems at once.
//
// Parameters:
//   - t: test to report to
func TestValidateJoinsErrors(t *testing.T) {
	s := DefaultSettings()
	s.Schema = 3
	s.UI.Theme = "x"
	s.UI.Welcome = "y"
	s.UI.Compact = "z"
	s.UI.Width = "w"

	err := s.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want an error")
	}
	for _, w := range []string{"schema", "ui.theme", "ui.welcome", "ui.compact", "ui.width"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not contain %q", err, w)
		}
	}
}

// TestEffectiveEditor checks the editor precedence and the splitting.
//
// Parameters:
//   - t: test to report to
func TestEffectiveEditor(t *testing.T) {
	tests := []struct {
		name   string
		editor string
		env    map[string]string
		want   []string
	}{
		{"setting wins", "code -w", map[string]string{"VISUAL": "nano"}, []string{"code", "-w"}},
		{
			"visual", "",
			map[string]string{"VISUAL": "nano -l", "EDITOR": "ed"},
			[]string{"nano", "-l"},
		},
		{"editor", "", map[string]string{"EDITOR": "ed"}, []string{"ed"}},
		{"blank setting is skipped", "   ", map[string]string{"EDITOR": "ed"}, []string{"ed"}},
		{"default", "", nil, []string{"vim"}},
		{"no shell evaluation", "vi $(id)", nil, []string{"vi", "$(id)"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveEditor(Settings{Editor: tt.editor}, envMap(tt.env))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EffectiveEditor() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestEffectiveLang checks the language precedence and normalisation.
//
// Parameters:
//   - t: test to report to
func TestEffectiveLang(t *testing.T) {
	tests := []struct {
		name string
		lang string
		env  map[string]string
		want string
	}{
		{"default is english", "", nil, "en"},
		{"tote lang wins", "en", map[string]string{"TOTE_LANG": "fr", "LC_ALL": "en_US"}, "fr"},
		{"setting beats locale", "fr", map[string]string{"LANG": "en_US.UTF-8"}, "fr"},
		{"lc_all", "", map[string]string{"LC_ALL": "fr_FR.UTF-8", "LANG": "en_US"}, "fr"},
		{"lc_messages", "", map[string]string{"LC_MESSAGES": "fr_CA", "LANG": "en_US"}, "fr"},
		{"lang", "", map[string]string{"LANG": "fr_FR.UTF-8"}, "fr"},
		{
			"unsupported locale is skipped", "",
			map[string]string{"LC_ALL": "de_DE", "LANG": "fr"},
			"fr",
		},
		{"C locale gives english", "", map[string]string{"LANG": "C"}, "en"},
		{"unsupported everywhere", "de", map[string]string{"TOTE_LANG": "es"}, "en"},
		{"case is ignored", "", map[string]string{"TOTE_LANG": "FR"}, "fr"},
		{"modifier suffix", "", map[string]string{"LANG": "fr@euro"}, "fr"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveLang(Settings{Lang: tt.lang}, envMap(tt.env))
			if got != tt.want {
				t.Errorf("EffectiveLang() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLoadSettingsParseError checks that a syntax error is a typed error with its position.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("lang = \"fr\"\neditor = \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSettings(path)
	var pe *ParseError
	if !errors.As(err, &pe) || pe.File != path || pe.Line != 2 || pe.Column == 0 {
		t.Fatalf("LoadSettings error = %#v", err)
	}
}

// TestExtraNamesRules checks the names accepted in shell.extra_names.
//
// Parameters:
//   - t: test to report to
func TestExtraNamesRules(t *testing.T) {
	good := DefaultSettings()
	good.Shell.ExtraNames = []string{"gs", "my-tool", "_x1"}
	if err := good.Validate(); err != nil {
		t.Errorf("Validate = %v", err)
	}

	for _, names := range [][]string{{"a", "a"}, {"1a"}, {""}, {"a b"}} {
		bad := DefaultSettings()
		bad.Shell.ExtraNames = names
		if bad.Validate() == nil {
			t.Errorf("extra_names %q accepted", names)
		}
	}
}

// TestLoadSettingsTooLarge checks that a config file above the size cap is refused.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsTooLarge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := strings.Repeat("# padding\n", maxSettingsSize/10+10)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadSettings(path); err == nil {
		t.Error("oversized config accepted")
	}
}

// TestMouseSetting checks that the mouse is on by default and that the key turns it off.
//
// Parameters:
//   - t: test to report to
func TestMouseSetting(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"absent key keeps the default", "schema = 1\n", true},
		{"other ui keys keep the default", "[ui]\ntheme = \"dark\"\n", true},
		{"explicit true", "[ui]\nmouse = true\n", true},
		{"explicit false", "[ui]\nmouse = false\n", false},
	}

	if !DefaultSettings().UI.Mouse {
		t.Error("DefaultSettings().UI.Mouse = false, want true")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadSettings(writeConfig(t, tt.content))
			if err != nil {
				t.Fatalf("LoadSettings() error = %v", err)
			}

			if got.UI.Mouse != tt.want {
				t.Errorf("UI.Mouse = %v, want %v", got.UI.Mouse, tt.want)
			}
		})
	}
}

// TestKeepaliveSetting checks that the keepalive is on by default and that the key turns it off.
//
// Parameters:
//   - t: test to report to
func TestKeepaliveSetting(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"absent key keeps the default", "schema = 1\n", true},
		{"other connect keys keep the default", "[connect]\nprefix_min = 3\n", true},
		{"explicit true", "[connect]\nkeepalive = true\n", true},
		{"explicit false", "[connect]\nkeepalive = false\n", false},
	}

	if !DefaultSettings().Connect.Keepalive {
		t.Error("DefaultSettings().Connect.Keepalive = false, want true")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadSettings(writeConfig(t, tt.content))
			if err != nil {
				t.Fatalf("LoadSettings() error = %v", err)
			}

			if got.Connect.Keepalive != tt.want {
				t.Errorf("Connect.Keepalive = %v, want %v", got.Connect.Keepalive, tt.want)
			}
		})
	}
}
