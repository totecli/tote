package config

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// boolPtr returns a pointer to v.
//
// Parameters:
//   - v: value to point to
func boolPtr(v bool) *bool { return &v }

// TestValidPluginID checks the identifier grammar on accepted and refused values.
//
// Parameters:
//   - t: test to report to
func TestValidPluginID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		ok   bool
	}{
		{"short official id", "gft", true},
		{"hyphen inside", "ssh-tools", true},
		{"digits first", "0a", true},
		{"all digits", "42", true},
		{"two bytes", "ab", true},
		{"trailing hyphen", "ab-", true},
		{"double hyphen", "a--b", true},
		{"thirty two bytes", strings.Repeat("a", 32), true},
		{"empty", "", false},
		{"one byte", "a", false},
		{"thirty three bytes", strings.Repeat("a", 33), false},
		{"upper case", "Gft", false},
		{"leading hyphen", "-gft", false},
		{"underscore", "a_b", false},
		{"dot", "a.b", false},
		{"dot dot", "..", false},
		{"traversal", "../data", false},
		{"slash", "a/b", false},
		{"backslash", `a\b`, false},
		{"leading dot", ".staging", false},
		{"mutex name", ".mutex", false},
		{"space", "a b", false},
		{"trailing newline", "gft\n", false},
		{"nul byte", "a\x00b", false},
		{"escape byte", "a\x1bb", false},
		{"non ascii letter", "caf\u00e9", false},
		{"right to left override", "ab\u202ecd", false},
		{"full width letters", "\uff47\uff46\uff54", false},
		{"percent", "a%2e", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidPluginID(tt.id)
			if (err == nil) != tt.ok {
				t.Fatalf("ValidPluginID(%q) = %v, want ok=%v", tt.id, err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidPluginID) {
				t.Errorf("error %v does not wrap ErrInvalidPluginID", err)
			}
		})
	}
}

// TestInvalidIDMessageIsSafe checks that a hostile identifier is escaped and cut in the message.
//
// Parameters:
//   - t: test to report to
func TestInvalidIDMessageIsSafe(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"escape sequence", "a\x1b[31mred"},
		{"newline", "a\nb"},
		{"bidi override", "a\u202eb"},
		{"very long", strings.Repeat("x", 100000) + "!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := ValidPluginID(tt.id).Error()
			if len(msg) > 200 {
				t.Errorf("message is %d bytes, want it cut", len(msg))
			}
			for _, r := range msg {
				if r < ' ' || r == 0x7f || r == '\u202e' {
					t.Errorf("message %q holds the raw character %U", msg, r)
				}
			}
		})
	}
}

// TestPluginPaths checks every path method against plain, XDG and TOTE_*_DIR bases.
//
// Parameters:
//   - t: test to report to
func TestPluginPaths(t *testing.T) {
	const tree = "0123456789abcdef0123456789abcdef"

	tests := []struct {
		name string
		env  map[string]string
		want map[string]string
	}{
		{
			name: "defaults from home",
			want: map[string]string{
				"dir":      "/home/u/.local/share/tote/plugins/gft",
				"tree":     "/home/u/.local/share/tote/plugins/gft/" + tree,
				"settings": "/home/u/.config/tote/plugins/gft.toml",
				"state":    "/home/u/.local/state/tote/plugins/gft",
				"host":     "/home/u/.local/state/tote/plugins-host/gft",
				"cache":    "/home/u/.cache/tote/plugins/gft",
				"lock":     "/home/u/.local/state/tote/plugins.lock",
				"procs":    "/home/u/.local/state/tote/plugin-procs",
				"staging":  "/home/u/.local/share/tote/plugins/.staging",
				"mutex":    "/home/u/.local/share/tote/plugins/.mutex",
			},
		},
		{
			name: "xdg bases",
			env: map[string]string{
				"XDG_CONFIG_HOME": "/x/c", "XDG_DATA_HOME": "/x/d",
				"XDG_STATE_HOME": "/x/s", "XDG_CACHE_HOME": "/x/k",
			},
			want: map[string]string{
				"dir":      "/x/d/tote/plugins/gft",
				"tree":     "/x/d/tote/plugins/gft/" + tree,
				"settings": "/x/c/tote/plugins/gft.toml",
				"state":    "/x/s/tote/plugins/gft",
				"host":     "/x/s/tote/plugins-host/gft",
				"cache":    "/x/k/tote/plugins/gft",
				"lock":     "/x/s/tote/plugins.lock",
				"procs":    "/x/s/tote/plugin-procs",
				"staging":  "/x/d/tote/plugins/.staging",
				"mutex":    "/x/d/tote/plugins/.mutex",
			},
		},
		{
			name: "tote overrides win and are not suffixed",
			env: map[string]string{
				"XDG_DATA_HOME": "/x/d", "TOTE_CONFIG_DIR": "/t/config/",
				"TOTE_DATA_DIR": "/t/data", "TOTE_STATE_DIR": "/t/state",
				"TOTE_CACHE_DIR": "/t/cache",
			},
			want: map[string]string{
				"dir":      "/t/data/plugins/gft",
				"tree":     "/t/data/plugins/gft/" + tree,
				"settings": "/t/config/plugins/gft.toml",
				"state":    "/t/state/plugins/gft",
				"host":     "/t/state/plugins-host/gft",
				"cache":    "/t/cache/plugins/gft",
				"lock":     "/t/state/plugins.lock",
				"procs":    "/t/state/plugin-procs",
				"staging":  "/t/data/plugins/.staging",
				"mutex":    "/t/data/plugins/.mutex",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Resolve(envMap(tt.env), "/home/u")
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}

			got := map[string]string{
				"lock":    p.LockFile(),
				"procs":   p.ProcsFile(),
				"staging": p.StagingDir(),
				"mutex":   p.MutexFile(),
			}
			for name, fn := range map[string]func(string) (string, error){
				"dir": p.PluginDir, "settings": p.PluginSettingsFile, "state": p.PluginStateDir,
				"host": p.PluginHostDir, "cache": p.PluginCacheDir,
			} {
				path, err := fn("gft")
				if err != nil {
					t.Fatalf("%s: error = %v", name, err)
				}
				got[name] = path
			}
			path, err := p.PluginTree("gft", tree)
			if err != nil {
				t.Fatalf("tree: error = %v", err)
			}
			got["tree"] = path

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("paths = %v\nwant %v", got, tt.want)
			}
		})
	}
}

// TestPluginPathsRefuseBadIDs checks that no path method builds a path from a bad identifier.
//
// Parameters:
//   - t: test to report to
func TestPluginPathsRefuseBadIDs(t *testing.T) {
	p := Paths{Config: "/c", Data: "/d", State: "/s", Cache: "/k"}
	const tree = "0123456789abcdef0123456789abcdef"

	methods := map[string]func(string) (string, error){
		"PluginDir":          p.PluginDir,
		"PluginSettingsFile": p.PluginSettingsFile,
		"PluginStateDir":     p.PluginStateDir,
		"PluginHostDir":      p.PluginHostDir,
		"PluginCacheDir":     p.PluginCacheDir,
		"PluginTree":         func(id string) (string, error) { return p.PluginTree(id, tree) },
	}
	ids := []string{
		"", "a", "../data", "..", "a/b", "/abs", "-x", ".staging", ".mutex", "A", "a b",
		"gft/../../x", "gft\n", "a\x00b", strings.Repeat("a", 33),
	}

	for name, fn := range methods {
		for _, id := range ids {
			t.Run(name+" "+strings.ReplaceAll(id, "\x00", "NUL"), func(t *testing.T) {
				path, err := fn(id)
				if !errors.Is(err, ErrInvalidPluginID) || path != "" {
					t.Errorf("%s(%q) = %q, %v; want an ErrInvalidPluginID error", name, id, path, err)
				}
			})
		}
	}
}

// TestPluginTreeName checks the validation of the tree directory name.
//
// Parameters:
//   - t: test to report to
func TestPluginTreeName(t *testing.T) {
	p := Paths{Data: "/d"}

	tests := []struct {
		name string
		tree string
		ok   bool
	}{
		{"lower hex", "0123456789abcdef0123456789abcdef", true},
		{"all zero", strings.Repeat("0", 32), true},
		{"empty", "", false},
		{"one short", strings.Repeat("a", 31), false},
		{"one long", strings.Repeat("a", 33), false},
		{"full sha256", strings.Repeat("a", 64), false},
		{"upper case", strings.Repeat("A", 32), false},
		{"non hex letter", strings.Repeat("g", 32), false},
		{"traversal", ".." + strings.Repeat("a", 30), false},
		{"slash", strings.Repeat("a", 31) + "/", false},
		{"dot dot padded", strings.Repeat("a", 15) + "/../" + strings.Repeat("a", 13), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := p.PluginTree("gft", tt.tree)
			if (err == nil) != tt.ok {
				t.Fatalf("PluginTree() = %q, %v, want ok=%v", path, err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidPluginTree) {
				t.Errorf("error %v does not wrap ErrInvalidPluginTree", err)
			}
			if err == nil && path != filepath.Join("/d/plugins/gft", tt.tree) {
				t.Errorf("path = %q", path)
			}
		})
	}
}

// TestPluginHostDirIsNotPluginWritable checks that the host and plugin directories never overlap.
//
// Parameters:
//   - t: test to report to
func TestPluginHostDirIsNotPluginWritable(t *testing.T) {
	p := Paths{Config: "/c", Data: "/d", State: "/s", Cache: "/k"}

	host, _ := p.PluginHostDir("gft")
	state, _ := p.PluginStateDir("gft")
	if host == state || strings.HasPrefix(host, state+"/") || strings.HasPrefix(state, host+"/") {
		t.Errorf("host dir %q and state dir %q overlap", host, state)
	}

	for _, name := range []string{p.StagingDir(), p.MutexFile()} {
		base := filepath.Base(name)
		if err := ValidPluginID(base); err == nil {
			t.Errorf("%q is a valid plugin id, it would collide with a plugin directory", base)
		}
	}
}

// TestLoadSettingsPlugins checks the accepted layouts of the [plugins] table.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsPlugins(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]PluginConfig
	}{
		{name: "no table", content: "lang = \"fr\"\n", want: nil},
		{
			name:    "disabled",
			content: "[plugins.gft]\nenabled = false\n",
			want:    map[string]PluginConfig{"gft": {Enabled: boolPtr(false)}},
		},
		{
			name:    "explicitly enabled",
			content: "[plugins.gft]\nenabled = true\n",
			want:    map[string]PluginConfig{"gft": {Enabled: boolPtr(true)}},
		},
		{
			name:    "empty table keeps the key unset",
			content: "[plugins.ghrel]\n",
			want:    map[string]PluginConfig{"ghrel": {}},
		},
		{
			name:    "several plugins",
			content: "[plugins.gft]\nenabled = false\n[plugins.ghsec]\nenabled = true\n[plugins.ghrel]\n",
			want: map[string]PluginConfig{
				"gft": {Enabled: boolPtr(false)}, "ghsec": {Enabled: boolPtr(true)}, "ghrel": {},
			},
		},
		{
			name:    "dotted key",
			content: "plugins.gft.enabled = false\n",
			want:    map[string]PluginConfig{"gft": {Enabled: boolPtr(false)}},
		},
		{
			name:    "inline table",
			content: "[plugins]\ngft = { enabled = false }\n",
			want:    map[string]PluginConfig{"gft": {Enabled: boolPtr(false)}},
		},
		{
			name:    "quoted id",
			content: "[plugins.\"ssh-tools\"]\nenabled = false\n",
			want:    map[string]PluginConfig{"ssh-tools": {Enabled: boolPtr(false)}},
		},
		{
			name:    "reserved but well formed id",
			content: "[plugins.tote]\nenabled = false\n",
			want:    map[string]PluginConfig{"tote": {Enabled: boolPtr(false)}},
		},
		{
			name:    "uninstalled id",
			content: "[plugins.never-installed]\nenabled = false\n",
			want:    map[string]PluginConfig{"never-installed": {Enabled: boolPtr(false)}},
		},
		{
			name:    "other settings are unaffected",
			content: "lang = \"fr\"\n[ui]\ntheme = \"dark\"\n[plugins.gft]\nenabled = false\n",
			want:    map[string]PluginConfig{"gft": {Enabled: boolPtr(false)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadSettings(writeConfig(t, tt.content))
			if err != nil {
				t.Fatalf("LoadSettings() error = %v", err)
			}
			if !reflect.DeepEqual(got.Plugins, tt.want) {
				t.Errorf("Plugins = %v, want %v", got.Plugins, tt.want)
			}
		})
	}
}

// TestLoadSettingsPluginWarnings checks that a fault in [plugins] is a warning with its line, that
// a switch that cannot be read turns the plugin off and that a bad identifier is ignored.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsPluginWarnings(t *testing.T) {
	long := strings.Repeat("a", 33)

	tests := []struct {
		name    string
		content string
		key     string
		line    int
		kind    string
		off     string
	}{
		{
			"enabled must be a bool", "[plugins.gft]\nenabled = \"yes\"\n",
			"plugins.gft.enabled", 2, WarnInvalidValue, "gft",
		},
		{
			"enabled integer", "\n\n[plugins.gft]\nenabled = 0\n", "plugins.gft.enabled", 4,
			WarnInvalidValue, "gft",
		},
		{
			"plugin entry must be a table", "[plugins]\ngft = true\n", "plugins.gft", 2,
			WarnInvalidValue, "gft",
		},
		{
			"unknown key", "[plugins.gft]\nenable = false\n", "plugins.gft.enable", 2,
			WarnUnknownKey, "",
		},
		{
			"unknown sub table", "[plugins.gft]\nenabled = true\n\n[plugins.gft.extra]\nx = 1\n",
			"plugins.gft.extra", 4, WarnUnknownKey, "",
		},
		{
			"upper case id in a header", "schema = 1\n\n[plugins.Bad_ID]\nenabled = false\n",
			"plugins.Bad_ID", 3, WarnInvalidValue, "",
		},
		{"one byte id", "[plugins.x]\n", "plugins.x", 1, WarnInvalidValue, ""},
		{"long id", "[plugins." + long + "]\n", "plugins." + long, 1, WarnInvalidValue, ""},
		{"leading hyphen", "[plugins.\"-gft\"]\n", "plugins.-gft", 1, WarnInvalidValue, ""},
		{
			"id with a space", "[plugins.\"a b\"]\nenabled = false\n", "plugins.a b", 1,
			WarnInvalidValue, "",
		},
		{
			"dotted key", "lang = \"en\"\nplugins.UP.enabled = false\n", "plugins.UP", 2,
			WarnInvalidValue, "",
		},
		{
			"inline table", "[plugins]\n  UP = { enabled = false }\n", "plugins.UP", 2,
			WarnInvalidValue, "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, warns, err := LoadSettingsWarn(writeConfig(t, tt.content))
			if err != nil {
				t.Fatalf("LoadSettingsWarn() error = %v", err)
			}

			if len(warns) != 1 {
				t.Fatalf("warnings = %+v, want one", warns)
			}

			w := warns[0]
			if w.Key != tt.key || w.Line != tt.line || w.Kind != tt.kind {
				t.Errorf("warning = %+v, want key %q line %d kind %s", w, tt.key, tt.line, tt.kind)
			}

			if tt.off != "" && s.PluginEnabled(tt.off) {
				t.Errorf("plugin %s is enabled, a switch that cannot be read fails closed", tt.off)
			}

			if tt.kind == WarnInvalidValue && w.Msg == "" {
				t.Error("no reason given")
			}
		})
	}
}

// TestLoadSettingsPluginSyntax checks that a syntax error in [plugins] still refuses the file,
// with its line.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsPluginSyntax(t *testing.T) {
	_, err := LoadSettings(writeConfig(t, "schema = 1\n\n[plugins.gft]\nenabled = \n"))
	checkErrorContains(t, err, []string{"config.toml:4:"})
}

// TestLoadSettingsHostileIDMessage checks that an escape byte in an identifier is escaped in the
// reason, so that the warning cannot drive a terminal.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsHostileIDMessage(t *testing.T) {
	_, warns, err := LoadSettingsWarn(writeConfig(t, "[plugins.\"a\\u001b[31mb\"]\n"))
	if err != nil || len(warns) != 1 {
		t.Fatalf("warnings = %+v, error %v", warns, err)
	}

	if !strings.Contains(warns[0].Msg, `\x1b`) || strings.ContainsRune(warns[0].Msg, '\x1b') {
		t.Errorf("reason %q is not escaped", warns[0].Msg)
	}
}

// TestValidatePluginIDs checks Validate on settings built in code.
//
// Parameters:
//   - t: test to report to
func TestValidatePluginIDs(t *testing.T) {
	s := DefaultSettings()
	s.Plugins = map[string]PluginConfig{"gft": {}, "ssh-tools": {Enabled: boolPtr(false)}}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	s.Plugins = map[string]PluginConfig{"zz/z": {}, "AA": {}, "ok": {}, "..": {}}
	err := s.Validate()
	if !errors.Is(err, ErrInvalidPluginID) {
		t.Fatalf("Validate() error = %v, want ErrInvalidPluginID", err)
	}

	msg := err.Error()
	for _, id := range []string{`"zz/z"`, `"AA"`, `".."`} {
		if !strings.Contains(msg, id) {
			t.Errorf("error %q does not name %s", msg, id)
		}
	}
	if strings.Contains(msg, `"ok"`) {
		t.Errorf("error %q names a valid id", msg)
	}
	if strings.Index(msg, `".."`) > strings.Index(msg, `"AA"`) {
		t.Errorf("errors are not sorted by id: %q", msg)
	}
}

// TestPluginEnabled checks the enabled semantics of config.toml.
//
// Parameters:
//   - t: test to report to
func TestPluginEnabled(t *testing.T) {
	s := DefaultSettings()
	s.Plugins = map[string]PluginConfig{
		"off": {Enabled: boolPtr(false)}, "on": {Enabled: boolPtr(true)}, "bare": {},
	}

	tests := []struct {
		id   string
		want bool
	}{
		{"off", false},
		{"on", true},
		{"bare", true},
		{"unknown", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := s.PluginEnabled(tt.id); got != tt.want {
				t.Errorf("PluginEnabled(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}

	if !DefaultSettings().PluginEnabled("gft") {
		t.Error("a plugin must be enabled when config.toml has no plugins table")
	}
}

// TestNoPlugins checks the session switch variable, which fails closed.
//
// Parameters:
//   - t: test to report to
func TestNoPlugins(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"unset", nil, false},
		{"empty", map[string]string{"TOTE_NO_PLUGINS": ""}, false},
		{"one", map[string]string{"TOTE_NO_PLUGINS": "1"}, true},
		{"yes", map[string]string{"TOTE_NO_PLUGINS": "yes"}, true},
		{"zero still turns plugins off", map[string]string{"TOTE_NO_PLUGINS": "0"}, true},
		{"other variable", map[string]string{"TOTE_NO_PLUGIN": "1"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NoPlugins(envMap(tt.env)); got != tt.want {
				t.Errorf("NoPlugins() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestKeyLine checks the line lookup on tables, dotted keys, inline tables and odd layouts.
//
// Parameters:
//   - t: test to report to
func TestKeyLine(t *testing.T) {
	tests := []struct {
		name     string
		doc      string
		key      []string
		wantLine int
		wantCol  int
	}{
		{"header", "a = 1\n[plugins.gft]\n", []string{"plugins", "gft"}, 2, 1},
		{"key below a header", "[plugins.gft]\nx = 1\n", []string{"plugins", "gft", "x"}, 2, 1},
		{"dotted root key", "\nplugins.gft.x = 1\n", []string{"plugins", "gft", "x"}, 2, 1},
		{"inline table", "[plugins]\n gft = {x = 1}\n", []string{"plugins", "gft"}, 2, 2},
		{"quoted header", "[plugins.\"a.b\"]\n", []string{"plugins", "a.b"}, 1, 1},
		{"crlf", "a = 1\r\n[plugins.gft]\r\n", []string{"plugins", "gft"}, 2, 1},
		{"comment lines are skipped", "# [plugins.gft]\n[plugins.gft]\n", []string{"plugins"}, 2, 1},
		{"array of tables", "[[plugins]]\n", []string{"plugins"}, 1, 1},
		{"the path must start at the root", "[a]\nx = 1\n[b]\nx = 1\n", []string{"x"}, 0, 0},
		{"missing", "a = 1\n", []string{"b"}, 0, 0},
		{"key under another table", "[b]\nx = 1\n", []string{"a", "x"}, 0, 0},
		{"continuation lines are skipped", "a = [\n  1,\n  2,\n]\nb = 1\n", []string{"b"}, 5, 1},
		{"empty document", "", []string{"a"}, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, col := keyLine([]byte(tt.doc), tt.key)
			if line != tt.wantLine || col != tt.wantCol {
				t.Errorf("keyLine() = %d:%d, want %d:%d", line, col, tt.wantLine, tt.wantCol)
			}
		})
	}
}
