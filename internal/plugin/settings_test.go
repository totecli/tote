package plugin

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// settingsManifest declares one setting of each type, with limits that the tests probe.
const settingsManifest = `
[[setting]]
id = "owner"
type = "string"
label = "Owner"
pattern = "^[a-z0-9-]+$"
max_len = 12
default = "someone"

[[setting]]
id = "verbose"
type = "bool"
label = "Verbose"

[[setting]]
id = "limit"
type = "int"
label = "Limit"
min = 1
max = 50
default = 10

[[setting]]
id = "mode"
type = "enum"
label = "Mode"
choices = ["fast", "safe"]

[[setting]]
id = "where"
type = "path"
label = "Where"

[[setting]]
id = "dir"
type = "path"
label = "Dir"
must_exist = true

[[setting]]
id = "tags"
type = "list"
label = "Tags"
pattern = "^[a-z]+$"
max_items = 3
default = ["x"]
`

// settingsFor decodes the settings manifest as the plugin "demo".
//
// Parameters:
//   - t: test to report to
func settingsFor(t *testing.T) *Manifest {
	t.Helper()

	m, rep := decode(decl("", nil, settingsManifest))
	if m == nil {
		t.Fatalf("settings manifest refused: %v", rep.Issues)
	}

	return m
}

// envOf builds an environment lookup from a map.
//
// Parameters:
//   - env: variables
func envOf(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

// valueOf finds one setting of a view.
//
// Parameters:
//   - t: test to report to
//   - v: view
//   - id: setting id
func valueOf(t *testing.T, v SettingsView, id string) SettingValue {
	t.Helper()

	for _, s := range v.Values {
		if s.ID == id {
			return s
		}
	}

	t.Fatalf("no setting %q in %+v", id, v.Values)

	return SettingValue{}
}

// TestSettingsDefaults checks the effective value of every type when nothing is set: the declared
// default, or the zero value of the type.
//
// Parameters:
//   - t: test to report to
func TestSettingsDefaults(t *testing.T) {
	v := EffectiveSettings(settingsFor(t), SettingsInput{PluginID: "demo", Lookup: envOf(nil)})

	want := map[string]any{
		"owner": "someone", "verbose": false, "limit": int64(10), "mode": "fast",
		"where": "", "dir": "", "tags": []string{"x"},
	}

	for id, w := range want {
		got := valueOf(t, v, id)
		if !reflect.DeepEqual(got.Value, w) || got.Source != SourceDefault || len(got.Rejected) != 0 {
			t.Errorf("%s = %#v from %s (%v), want %#v from default", id, got.Value, got.Source,
				got.Rejected, w)
		}
	}

	if v.Exists || v.Problem != nil || len(v.Unknown) != 0 || len(v.Values) != 7 {
		t.Errorf("view = %+v", v)
	}
}

// TestSettingsZeroValues checks the zero value of each type when the manifest gives no default,
// the int being moved inside its bounds.
//
// Parameters:
//   - t: test to report to
func TestSettingsZeroValues(t *testing.T) {
	src := `
[[setting]]
id = "s"
type = "string"
label = "S"
[[setting]]
id = "i"
type = "int"
label = "I"
min = 5
max = 9
[[setting]]
id = "j"
type = "int"
label = "J"
min = -9
max = -5
[[setting]]
id = "k"
type = "int"
label = "K"
[[setting]]
id = "l"
type = "list"
label = "L"
`

	m, rep := decode(decl("", nil, src))
	if m == nil {
		t.Fatal(rep.Issues)
	}

	v := EffectiveSettings(m, SettingsInput{PluginID: "demo", Lookup: envOf(nil)})

	want := map[string]any{"s": "", "i": int64(5), "j": int64(-5), "k": int64(0), "l": []string{}}
	for id, w := range want {
		if got := valueOf(t, v, id).Value; !reflect.DeepEqual(got, w) {
			t.Errorf("%s = %#v, want %#v", id, got, w)
		}
	}
}

// TestSettingsDefaultIsACopy checks that a list default of the manifest cannot be changed through
// the view.
//
// Parameters:
//   - t: test to report to
func TestSettingsDefaultIsACopy(t *testing.T) {
	m := settingsFor(t)
	in := SettingsInput{PluginID: "demo", Lookup: envOf(nil)}

	first := valueOf(t, EffectiveSettings(m, in), "tags").Value.([]string)
	first[0] = "changed"

	if got := valueOf(t, EffectiveSettings(m, in), "tags").Value.([]string); got[0] != "x" {
		t.Errorf("default = %v", got)
	}
}

// TestSettingsPrecedence checks environment over file over default, and the fall-back to the next
// source when a value is rejected, with the rejection recorded.
//
// Parameters:
//   - t: test to report to
func TestSettingsPrecedence(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "demo.toml")
	must(t, os.WriteFile(file, []byte("schema = 1\nowner = \"from-file\"\nlimit = 20\n"), 0o600))

	m := settingsFor(t)

	t.Run("file over default", func(t *testing.T) {
		v := EffectiveSettings(m, SettingsInput{PluginID: "demo", File: file, Lookup: envOf(nil)})

		if got := valueOf(t, v, "owner"); got.Value != "from-file" || got.Source != SourceFile {
			t.Errorf("owner = %+v", got)
		}

		if got := valueOf(t, v, "limit"); got.Value != int64(20) || got.Source != SourceFile {
			t.Errorf("limit = %+v", got)
		}

		if got := valueOf(t, v, "verbose"); got.Source != SourceDefault {
			t.Errorf("verbose = %+v", got)
		}

		if !v.Exists || v.Problem != nil {
			t.Errorf("view = %+v", v)
		}
	})

	t.Run("environment over file", func(t *testing.T) {
		env := map[string]string{"TOTE_PLUGIN_DEMO_OWNER": "from-env"}
		v := EffectiveSettings(m, SettingsInput{PluginID: "demo", File: file, Lookup: envOf(env)})

		got := valueOf(t, v, "owner")
		if got.Value != "from-env" || got.Source != SourceEnvironment || len(got.Rejected) != 0 {
			t.Errorf("owner = %+v", got)
		}
	})

	t.Run("rejected environment falls back to the file", func(t *testing.T) {
		env := map[string]string{"TOTE_PLUGIN_DEMO_LIMIT": "999"}
		v := EffectiveSettings(m, SettingsInput{PluginID: "demo", File: file, Lookup: envOf(env)})

		got := valueOf(t, v, "limit")
		if got.Value != int64(20) || got.Source != SourceFile || len(got.Rejected) != 1 ||
			got.Rejected[0] != (SettingRejection{SourceEnvironment, RejectRange, "999"}) {
			t.Errorf("limit = %+v", got)
		}
	})

	t.Run("both rejected falls back to the default", func(t *testing.T) {
		bad := filepath.Join(dir, "bad.toml")
		must(t, os.WriteFile(bad, []byte("limit = \"ten\"\n"), 0o600))

		env := map[string]string{"TOTE_PLUGIN_DEMO_LIMIT": "ten"}
		v := EffectiveSettings(m, SettingsInput{PluginID: "demo", File: bad, Lookup: envOf(env)})

		got := valueOf(t, v, "limit")
		if got.Value != int64(10) || got.Source != SourceDefault || len(got.Rejected) != 2 ||
			got.Rejected[0].Code != RejectType || got.Rejected[1].Source != SourceFile {
			t.Errorf("limit = %+v", got)
		}
	})

	t.Run("empty variable is unset", func(t *testing.T) {
		env := map[string]string{"TOTE_PLUGIN_DEMO_OWNER": ""}
		v := EffectiveSettings(m, SettingsInput{PluginID: "demo", File: file, Lookup: envOf(env)})

		if got := valueOf(t, v, "owner"); got.Source != SourceFile {
			t.Errorf("owner = %+v", got)
		}
	})

	t.Run("variable of another plugin", func(t *testing.T) {
		env := map[string]string{"TOTE_PLUGIN_OTHER_OWNER": "x", "TOTE_PLUGIN_DEMO_OWNERS": "y"}
		v := EffectiveSettings(m, SettingsInput{PluginID: "demo", Lookup: envOf(env)})

		if got := valueOf(t, v, "owner"); got.Source != SourceDefault {
			t.Errorf("owner = %+v", got)
		}
	})
}

// TestSettingsEnvironmentValues checks the text forms of the variables, type by type: accepted
// forms, the value they give, and the refusals.
//
// Parameters:
//   - t: test to report to
func TestSettingsEnvironmentValues(t *testing.T) {
	tests := []struct {
		setting string
		raw     string
		want    any
		reject  string
	}{
		{"verbose", "1", true, ""},
		{"verbose", "true", true, ""},
		{"verbose", "yes", true, ""},
		{"verbose", "on", true, ""},
		{"verbose", "TRUE", true, ""},
		{"verbose", "On", true, ""},
		{"verbose", "0", false, ""},
		{"verbose", "false", false, ""},
		{"verbose", "no", false, ""},
		{"verbose", "off", false, ""},
		{"verbose", "OFF", false, ""},
		{"verbose", "2", nil, RejectType},
		{"verbose", "y", nil, RejectType},
		{"verbose", "truee", nil, RejectType},
		{"verbose", " true", nil, RejectType},
		{"limit", "7", int64(7), ""},
		{"limit", "-0", nil, RejectRange},
		{"limit", "+7", int64(7), ""},
		{"limit", "50", int64(50), ""},
		{"limit", "51", nil, RejectRange},
		{"limit", "0", nil, RejectRange},
		{"limit", "1", int64(1), ""},
		{"limit", "7.5", nil, RejectType},
		{"limit", "0x10", nil, RejectType},
		{"limit", "1e3", nil, RejectType},
		{"limit", "seven", nil, RejectType},
		{"limit", "99999999999999999999", nil, RejectType},
		{"mode", "safe", "safe", ""},
		{"mode", "Safe", nil, RejectChoice},
		{"mode", "other", nil, RejectChoice},
		{"owner", "abc-1", "abc-1", ""},
		{"owner", "ABC", nil, RejectPattern},
		{"owner", "a b", nil, RejectPattern},
		{"owner", "abcdefghijklm", nil, RejectLength},
		{"owner", "abc\x1b[2J", nil, RejectText},
		{"tags", "a,b", []string{"a", "b"}, ""},
		{"tags", " a , b ,c", []string{"a", "b", "c"}, ""},
		{"tags", "a", []string{"a"}, ""},
		{"tags", "a,,b", nil, RejectText},
		{"tags", "a,b,c,d", nil, RejectItems},
		{"tags", "A", nil, RejectPattern},
		{"tags", "a,", nil, RejectText},
		{"where", "/tmp/x", "/tmp/x", ""},
		{"where", "~/x", "~/x", ""},
		{"where", "~", "~", ""},
		{"where", "relative/x", nil, RejectPath},
		{"where", "~root/x", nil, RejectPath},
		{"where", "/tmp/\x00", nil, RejectText},
		{"where", "/" + times("a", 5000), nil, RejectLength},
	}

	m := settingsFor(t)

	for _, tt := range tests {
		t.Run(tt.setting+"="+tt.raw, func(t *testing.T) {
			env := map[string]string{SettingEnvName("demo", tt.setting): tt.raw}
			v := EffectiveSettings(m, SettingsInput{PluginID: "demo", Lookup: envOf(env)})
			got := valueOf(t, v, tt.setting)

			if tt.reject != "" {
				if got.Source != SourceDefault || len(got.Rejected) != 1 ||
					got.Rejected[0].Code != tt.reject {
					t.Errorf("got %+v, want a %s rejection", got, tt.reject)
				}

				return
			}

			if got.Source != SourceEnvironment || !reflect.DeepEqual(got.Value, tt.want) {
				t.Errorf("got %+v, want %#v", got, tt.want)
			}
		})
	}
}

// TestSettingsFileValues checks the same rules on values that come from the file: the TOML type
// matters, and a string never stands for an int or a list.
//
// Parameters:
//   - t: test to report to
func TestSettingsFileValues(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		id     string
		want   any
		reject string
	}{
		{"string", `owner = "abc"`, "owner", "abc", ""},
		{"string as int", `owner = 5`, "owner", nil, RejectType},
		{"string as list", `owner = ["a"]`, "owner", nil, RejectType},
		{"empty string", `owner = ""`, "owner", "", ""},
		{"bool", `verbose = true`, "verbose", true, ""},
		{"bool as string", `verbose = "true"`, "verbose", nil, RejectType},
		{"bool as int", `verbose = 1`, "verbose", nil, RejectType},
		{"int", `limit = 5`, "limit", int64(5), ""},
		{"int as string", `limit = "5"`, "limit", nil, RejectType},
		{"int as float", `limit = 5.0`, "limit", nil, RejectType},
		{"int low", `limit = 0`, "limit", nil, RejectRange},
		{"int high", `limit = 51`, "limit", nil, RejectRange},
		{"enum", `mode = "safe"`, "mode", "safe", ""},
		{"enum other", `mode = "slow"`, "mode", nil, RejectChoice},
		{"enum as int", `mode = 1`, "mode", nil, RejectType},
		{"list", `tags = ["a", "b"]`, "tags", []string{"a", "b"}, ""},
		{"empty list", `tags = []`, "tags", []string{}, ""},
		{"list of ints", `tags = [1]`, "tags", nil, RejectType},
		{"list as string", `tags = "a,b"`, "tags", nil, RejectType},
		{"list too long", `tags = ["a", "b", "c", "d"]`, "tags", nil, RejectItems},
		{"list with empty item", `tags = ["a", ""]`, "tags", nil, RejectText},
		{"list item pattern", `tags = ["a", "B"]`, "tags", nil, RejectPattern},
		{"nested list", `tags = [["a"]]`, "tags", nil, RejectType},
		{"path", `where = "/tmp"`, "where", "/tmp", ""},
		{"path relative", `where = "tmp"`, "where", nil, RejectPath},
		{"datetime", `owner = 2026-11-03T10:15:00Z`, "owner", nil, RejectType},
		{"table", "owner = { a = 1 }", "owner", nil, RejectType},
		{"control character", `owner = "a\u001bb"`, "owner", nil, RejectText},
		{"bidi character", `owner = "a\u202eb"`, "owner", nil, RejectText},
	}

	m := settingsFor(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "demo.toml")
			must(t, os.WriteFile(file, []byte("schema = 1\n"+tt.line+"\n"), 0o600))

			v := EffectiveSettings(m, SettingsInput{PluginID: "demo", File: file, Lookup: envOf(nil)})
			got := valueOf(t, v, tt.id)

			if v.Problem != nil {
				t.Fatalf("file problem %+v", v.Problem)
			}

			if tt.reject != "" {
				if got.Source != SourceDefault || len(got.Rejected) != 1 ||
					got.Rejected[0].Code != tt.reject || got.Rejected[0].Source != SourceFile {
					t.Errorf("got %+v, want a %s rejection", got, tt.reject)
				}

				return
			}

			if got.Source != SourceFile || !reflect.DeepEqual(got.Value, tt.want) {
				t.Errorf("got %+v, want %#v", got, tt.want)
			}
		})
	}
}

// TestSettingsPathMustExist checks the must_exist rule, with the home directory expanded.
//
// Parameters:
//   - t: test to report to
func TestSettingsPathMustExist(t *testing.T) {
	home := t.TempDir()
	must(t, os.Mkdir(filepath.Join(home, "there"), 0o700))

	m := settingsFor(t)

	for raw, ok := range map[string]bool{
		"~/there": true, "~": true, home + "/there": true,
		"~/gone": false, "/nonexistent/x": false,
	} {
		env := map[string]string{"TOTE_PLUGIN_DEMO_DIR": raw}
		v := EffectiveSettings(m, SettingsInput{PluginID: "demo", Lookup: envOf(env), Home: home})
		got := valueOf(t, v, "dir")

		if (got.Source == SourceEnvironment) != ok {
			t.Errorf("dir=%s: %+v, accepted = %v", raw, got, ok)
		}

		if !ok && (len(got.Rejected) != 1 || got.Rejected[0].Code != RejectMissing) {
			t.Errorf("dir=%s: rejected = %+v", raw, got.Rejected)
		}
	}
}

// TestSettingsFileProblems checks the files that are ignored as a whole: a syntax error, the
// wrong schema, an oversize file, pathological structure, something that is not a regular file.
// The defaults apply and the problem is reported, with a position for a syntax error.
//
// Parameters:
//   - t: test to report to
func TestSettingsFileProblems(t *testing.T) {
	dir := t.TempDir()

	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		must(t, os.WriteFile(p, []byte(content), 0o600))

		return p
	}

	tests := []struct {
		name string
		path string
		code string
		line int
	}{
		{"syntax", write("syntax.toml", "schema = 1\nowner = \n"), RejectSyntax, 2},
		{"schema 2", write("schema.toml", "schema = 2\nowner = \"x\"\n"), RejectSchema, 0},
		{"schema string", write("schema2.toml", "schema = \"1\"\n"), RejectSchema, 0},
		{"too large", write("big.toml", "owner = \""+times("a", maxSettingsFile)+"\"\n"), RejectRead, 0},
		{"header bomb", write("hdr.toml", "["+times("a.", 5000)+"a]\n"), RejectShape, 1},
		{
			"nesting bomb", write("nest.toml", "tags = "+times("[", 5000)+times("]", 5000)+"\n"),
			RejectShape, 1,
		},
		{"dotted key bomb", write("dot.toml", times("a.", 5000)+"a = 1\n"), RejectShape, 1},
		{"a directory", dir, RejectRead, 0},
		{"binary", write("bin.toml", "\x00\x01\x02"), RejectSyntax, 0},
	}

	m := settingsFor(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := EffectiveSettings(m, SettingsInput{PluginID: "demo", File: tt.path, Lookup: envOf(nil)})

			if v.Problem == nil || v.Problem.Code != tt.code || !v.Exists {
				t.Fatalf("problem = %+v, want %s", v.Problem, tt.code)
			}

			if tt.line > 0 && v.Problem.Line != tt.line {
				t.Errorf("line = %d, want %d", v.Problem.Line, tt.line)
			}

			if got := valueOf(t, v, "owner"); got.Source != SourceDefault {
				t.Errorf("a file with a problem applied: %+v", got)
			}
		})
	}
}

// TestSettingsMissingFile checks that no file, an empty path and a missing directory are no
// problem.
//
// Parameters:
//   - t: test to report to
func TestSettingsMissingFile(t *testing.T) {
	m := settingsFor(t)

	for _, path := range []string{
		"", filepath.Join(t.TempDir(), "nope.toml"),
		filepath.Join(t.TempDir(), "no", "dir", "nope.toml"),
	} {
		v := EffectiveSettings(m, SettingsInput{PluginID: "demo", File: path, Lookup: envOf(nil)})
		if v.Exists || v.Problem != nil {
			t.Errorf("%q: exists %v problem %+v", path, v.Exists, v.Problem)
		}
	}
}

// TestSettingsSymlinkedFile checks that a settings file may be a symbolic link into a dotfiles
// repository, which is why it is read with the rules of a user file and written by the host only
// through the store.
//
// Parameters:
//   - t: test to report to
func TestSettingsSymlinkedFile(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles.toml")
	must(t, os.WriteFile(real, []byte("owner = \"linked\"\n"), 0o600))

	link := filepath.Join(dir, "demo.toml")
	must(t, os.Symlink(real, link))

	v := EffectiveSettings(settingsFor(t), SettingsInput{
		PluginID: "demo", File: link,
		Lookup: envOf(nil),
	})

	if got := valueOf(t, v, "owner"); got.Value != "linked" || got.Source != SourceFile {
		t.Errorf("owner = %+v", got)
	}
}

// TestSettingsUnknownKeys checks that keys no setting declares are listed, sorted and cleaned, and
// that neither the schema key nor a key that starts with an underscore, which belongs to tote, is
// one of them.
//
// Parameters:
//   - t: test to report to
func TestSettingsUnknownKeys(t *testing.T) {
	file := filepath.Join(t.TempDir(), "demo.toml")
	content := "schema = 1\n_pin = \"x\"\nzeta = 1\nalpha = 2\nowner = \"x\"\n\"a\\u001bb\" = 3\n"
	must(t, os.WriteFile(file, []byte(content), 0o600))

	v := EffectiveSettings(settingsFor(t), SettingsInput{
		PluginID: "demo", File: file,
		Lookup: envOf(nil),
	})

	if !slices.Equal(v.Unknown, []string{"a?b", "alpha", "zeta"}) {
		t.Errorf("Unknown = %q", v.Unknown)
	}
}

// TestSettingsRejectionIsCleaned checks that the value kept in a rejection cannot carry an escape
// sequence or flood a screen.
//
// Parameters:
//   - t: test to report to
func TestSettingsRejectionIsCleaned(t *testing.T) {
	env := map[string]string{"TOTE_PLUGIN_DEMO_LIMIT": "\x1b]52;c;AAAA\x07" + times("9", 500)}
	v := EffectiveSettings(settingsFor(t), SettingsInput{PluginID: "demo", Lookup: envOf(env)})

	raw := valueOf(t, v, "limit").Rejected[0].Raw
	if strings.ContainsAny(raw, "\x1b\x07") || len([]rune(raw)) > issueArgRunes {
		t.Errorf("Raw = %q", raw)
	}
}

// TestSettingEnvName checks the name of the variable of a setting.
//
// Parameters:
//   - t: test to report to
func TestSettingEnvName(t *testing.T) {
	if got := SettingEnvName("my-plugin", "max_items"); got != "TOTE_PLUGIN_MY__PLUGIN_MAX_ITEMS" {
		t.Errorf("SettingEnvName = %q", got)
	}
}

// TestRegistrySettings checks that the registry reads the settings file of the plugin from the
// configuration directory and the TOTE_PLUGIN variables from its own environment, and gives
// nothing for a plugin without a manifest.
//
// Parameters:
//   - t: test to report to
func TestRegistrySettings(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", settingsManifest))
	l.writeFile(filepath.Join(l.root, "config", "plugins", "demo.toml"),
		"schema = 1\nowner = \"filed\"\nlimit = 30\n")

	l.env["TOTE_PLUGIN_DEMO_LIMIT"] = "40"

	reg := l.registry(nil)
	p, _ := reg.Load().Get("demo")

	if p.State != StateEnabled {
		t.Fatalf("state %s %v", p.State, p.Reasons)
	}

	v := reg.Settings(p)

	if got := valueOf(t, v, "owner"); got.Value != "filed" || got.Source != SourceFile {
		t.Errorf("owner = %+v", got)
	}

	if got := valueOf(t, v, "limit"); got.Value != int64(40) || got.Source != SourceEnvironment {
		t.Errorf("limit = %+v", got)
	}

	if v.File != filepath.Join(l.root, "config", "plugins", "demo.toml") || !v.Exists {
		t.Errorf("File = %q exists %v", v.File, v.Exists)
	}

	if got := reg.Settings(Plugin{ID: "demo"}); len(got.Values) != 0 {
		t.Errorf("settings of a plugin without a manifest: %+v", got)
	}
}

// FuzzEffectiveSettings feeds hostile file content and environment values to the settings reader
// and checks the invariant that matters: the effective value of every setting satisfies the
// setting, whatever the sources hold, and nothing panics. The file is capped before it is read.
//
// Parameters:
//   - f: fuzzing driver
func FuzzEffectiveSettings(f *testing.F) {
	f.Add("owner = \"abc\"\nlimit = 7\n", "TOTE_PLUGIN_DEMO_LIMIT", "99")
	f.Add("tags = [\"a\", \"b\"]\n", "TOTE_PLUGIN_DEMO_TAGS", "a,,b")
	f.Add("mode = 3\n", "TOTE_PLUGIN_DEMO_MODE", "safe")
	f.Add("[a.b.c]\nx = 1\n", "TOTE_PLUGIN_DEMO_VERBOSE", "On")
	f.Add("owner = "+times("[", 40)+"\n", "TOTE_PLUGIN_DEMO_WHERE", "~/x\x00")
	f.Add("limit = 99999999999999999999\n", "TOTE_PLUGIN_DEMO_OWNER", "\x1b[31m")

	m, rep := decode(decl("", nil, settingsManifest))
	if m == nil {
		f.Fatal(rep.Issues)
	}

	dir := f.TempDir()

	f.Fuzz(func(t *testing.T, content, name, value string) {
		if len(content) > 1<<14 || len(value) > 1<<14 {
			t.Skip()
		}

		file := filepath.Join(dir, "demo.toml")
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		v := EffectiveSettings(m, SettingsInput{
			PluginID: "demo", File: file,
			Lookup: envOf(map[string]string{name: value}),
		})

		for i, s := range v.Values {
			chk := checker{s: &m.Settings[i]}

			in := s.Value

			if l, ok := s.Value.([]string); ok {
				items := make([]any, len(l))
				for j, it := range l {
					items[j] = it
				}

				in = items
			}

			// The defaults are checked by the manifest validator; a value from a source is
			// checked here again, with the rules of the setting.
			if s.Source != SourceDefault {
				if _, code := chk.check(in); code != "" {
					t.Fatalf("%s: effective value %#v from %s fails %s", s.ID, s.Value, s.Source,
						code)
				}
			}
		}
	})
}
