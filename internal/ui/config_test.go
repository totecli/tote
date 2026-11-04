package ui

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/i18n"
)

// configRig builds a rig on the settings screen, with a config.toml and the settings it holds.
//
// Parameters:
//   - t: running test
//   - content: text of config.toml, empty for no file
//   - env: environment variables to add
func configRig(t *testing.T, content string, env ...string) (*rig, *sandbox) {
	t.Helper()

	s := newSandbox(t)

	if content != "" {
		writeFile(t, s.Opts.Paths.ConfigFile(), content)

		loaded, err := config.LoadSettings(s.Opts.Paths.ConfigFile())
		if err == nil {
			s.Opts.Settings = loaded
		}
	}

	s = s.with(func(o *Options) {
		o.Start = StartConfig
		o.Environ = append(append([]string(nil), o.Environ...), env...)
	})

	return newRig(t, s), s
}

// rowOf returns the position of a setting on the screen.
//
// Parameters:
//   - t: running test
//   - id: identifier of the setting
func rowOf(t *testing.T, id string) int {
	t.Helper()

	for i, d := range settingDefs {
		if d.id == id {
			return i
		}
	}

	t.Fatalf("no setting %q", id)

	return 0
}

// selectRow moves the cursor of the settings screen to a setting.
//
// Parameters:
//   - g: rig on the settings screen
//   - id: identifier of the setting
func selectRow(g *rig, id string) {
	g.t.Helper()
	g.press("home")

	for range rowOf(g.t, id) {
		g.press("down")
	}
}

// readConfig returns the text of config.toml of a sandbox.
//
// Parameters:
//   - t: running test
//   - s: sandbox
func readConfig(t *testing.T, s *sandbox) string {
	t.Helper()

	data, err := os.ReadFile(s.Opts.Paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// TestSettingDefsCatalog checks that every row of the settings screen has the texts it needs in
// both languages: its name, its short and its full description, its note when it has one, the
// prompt of a text and the name of every value of a choice.
//
// Parameters:
//   - t: test to report to
func TestSettingDefsCatalog(t *testing.T) {
	seen := map[string]bool{}

	for _, lang := range []string{"en", "fr"} {
		tr := i18n.New(lang)

		need := func(key string) {
			if tr.T(key) == key {
				t.Errorf("%s: no text for %s", lang, key)
			}
		}

		for _, d := range settingDefs {
			if lang == "en" && seen[d.id] {
				t.Errorf("setting %q is listed twice", d.id)
			}

			seen[d.id] = true

			need(d.nameKey())
			need(d.briefKey())
			need(d.descKey())

			if d.note {
				need(d.noteKey())
			} else if d.noteKey() != "" {
				t.Errorf("setting %q has a note key without a note", d.id)
			}

			if d.writable() && d.apply == applyNone {
				t.Errorf("setting %q is stored but has no apply scope", d.id)
			}

			if d.kind == settingText {
				need("ui_setp_" + d.id)
			}

			for _, c := range d.choices {
				need(optionKey(d, c))
			}
		}
	}
}

// TestSettingDefsMatchLoader checks that the rows read and write the keys the loader knows: for
// every choice, the screen never takes a value that the loader refuses, takes every value it
// offers, and what it writes is what it reads back.
//
// Parameters:
//   - t: test to report to
func TestSettingDefsMatchLoader(t *testing.T) {
	e := newRig(t, newSandbox(t)).r.e

	for _, d := range settingDefs {
		if !d.writable() {
			continue
		}

		values := slices.Clone(d.choices)

		switch d.kind {
		case settingBool:
			values = []string{"true", "false"}
		case settingChoice:
			values = append(values, "bogus")
		default:
			continue
		}

		for _, v := range values {
			path := t.TempDir() + "/config.toml"
			paths := config.Paths{Config: strings.TrimSuffix(path, "/config.toml")}
			verr := writeSetting(e.Tr, paths, d, v, false)

			_, lerr := config.LoadSettings(path)
			refused := invalidSetting(e, d, v) != ""

			if lerr != nil && !refused {
				t.Errorf("%s=%q: the loader refuses it (%v) but the screen takes it", d.id, v, lerr)
			}

			if slices.Contains(d.choices, v) && refused {
				t.Errorf("%s=%q: the screen refuses a value it offers", d.id, v)
			}

			if !refused && verr == nil {
				got, err := config.LoadSettings(path)
				if err != nil {
					t.Errorf("%s=%q: %v", d.id, v, err)
					continue
				}

				if d.value(got) != v {
					t.Errorf("%s=%q: read back %q", d.id, v, d.value(got))
				}
			}
		}
	}
}

// TestInvalidSetting checks the rules of the text settings and that a bad choice is refused.
//
// Parameters:
//   - t: test to report to
func TestInvalidSetting(t *testing.T) {
	e := newRig(t, newSandbox(t)).r.e

	tests := []struct {
		id    string
		value string
		ok    bool
	}{
		{"lang", "", true},
		{"lang", "en", true},
		{"lang", "fr", true},
		{"lang", "de", false},
		{"editor", "", true},
		{"editor", "vim", true},
		{"editor", "code -n", true},
		{"editor", "   ", false},
		{"editor", "vi\x00m", false},
		{"editor", "vi\nm", false},
		{"editor", "vi\x1bm", false},
		{"editor", "\xff\xfe", false},
		{"repos_root", "", true},
		{"repos_root", "/srv/work", true},
		{"repos_root", "~/work", true},
		{"repos_root", "work", false},
		{"repos_root", "./work", false},
		{"repos_root", "/srv/\x07", false},
		{"after_disconnect", "menu", true},
		{"after_disconnect", "exit", true},
		{"after_disconnect", "quit", false},
		{"after_disconnect", "", false},
		{"after_open", "cd", true},
		{"after_open", "stay", false},
		{"theme", "mono", true},
		{"theme", "blue", false},
		{"welcome", "off", true},
		{"welcome", "big", false},
		{"compact", "always", true},
		{"compact", "sometimes", false},
		{"check", "true", true},
		{"check", "false", true},
	}

	for _, tt := range tests {
		d := settingDefs[rowOf(t, tt.id)]

		why := invalidSetting(e, d, tt.value)
		if (why == "") != tt.ok {
			t.Errorf("%s=%q: reason %q, want accepted %v", tt.id, tt.value, why, tt.ok)
		}

		if why != "" && sanitize(why) != why {
			t.Errorf("%s=%q: the reason holds unsafe text: %q", tt.id, tt.value, why)
		}
	}
}

// TestSourceOf checks where each row says its value comes from, and which origins lock a row.
//
// Parameters:
//   - t: test to report to
func TestSourceOf(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		env    []string
		id     string
		origin origin
		name_  string
		locked bool
	}{
		{"lang default", "", []string{"LANG=C"}, "lang", originDefault, "", false},
		{"lang locale", "", []string{"LANG=fr_FR.UTF-8"}, "lang", originEnv, "LANG", false},
		{
			"lang file", "schema = 1\nlang = \"fr\"\n",
			[]string{"LANG=en_US.UTF-8"},
			"lang",
			originFile, "", false,
		},
		{
			"lang override", "schema = 1\nlang = \"fr\"\n",
			[]string{"TOTE_LANG=en"},
			"lang",
			originEnv, "TOTE_LANG", true,
		},
		{"editor default", "", nil, "editor", originDefault, "", false},
		{"editor visual", "", []string{"VISUAL=nano"}, "editor", originEnv, "VISUAL", false},
		{
			"editor file", "schema = 1\neditor = \"nano\"\n",
			[]string{"EDITOR=vi"},
			"editor",
			originFile, "", false,
		},
		{
			"table key", "schema = 1\n[ui]\ncompact = \"never\"\n", nil, "compact", originFile, "",
			false,
		},
		{
			"table key absent", "schema = 1\n[ui]\ncompact = \"never\"\n", nil, "theme",
			originDefault, "", false,
		},
		{"colors env", "", []string{"TOTE_COLOR=16"}, "colors", originEnv, "TOTE_COLOR", true},
		{
			"colors no_color", "",
			[]string{"TOTE_COLOR=", "NO_COLOR=1"},
			"colors", originEnv,
			"NO_COLOR", true,
		},
		{"colors none", "", []string{"TOTE_COLOR="}, "colors", originDefault, "", false},
	}

	for _, tt := range tests {
		g, s := configRig(t, tt.file, tt.env...)
		g.r.e.Environ = append(slices.DeleteFunc(slices.Clone(g.r.e.Environ), func(v string) bool {
			return strings.HasPrefix(v, "TOTE_COLOR=") && tt.id != "colors"
		}), tt.env...)

		st := readFileState(s.Opts.Paths.ConfigFile())
		got := sourceOf(g.r.e, st, settingDefs[rowOf(t, tt.id)])

		if got.origin != tt.origin || got.name != tt.name_ || got.locked != tt.locked {
			t.Errorf("%s: source %+v, want origin %d name %q locked %v", tt.name, got, tt.origin,
				tt.name_, tt.locked)
		}
	}
}

// TestWriteSetting checks the edits of config.toml: the comments and the other keys stay, a
// missing table is added, a reset removes the key, and a file that cannot be read is not touched.
//
// Parameters:
//   - t: test to report to
func TestWriteSetting(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		id    string
		value string
		reset bool
		want  []string
		not   []string
	}{
		{
			"root key keeps comments", fixtureConfig, "editor", "code -n", false,
			[]string{
				"# my own note", "# trailing note", `editor = "code -n"`,
				`repos_root = "/srv/work"`, `after_disconnect = "exit"`,
			},
			[]string{"nano"},
		},
		{
			"table key", fixtureConfig, "after_disconnect", "menu", false,
			[]string{"# my own note", `after_disconnect = "menu"`, `theme = "dark"`},
			[]string{`"exit"`},
		},
		{
			"new table", fixtureConfig, "compact", "always", false,
			[]string{`compact = "always"`, "[ui]", `theme = "dark"`},
			nil,
		},
		{
			"new file", "", "after_open", "quit", false,
			[]string{"[repos]", `after_open = "quit"`},
			nil,
		},
		{
			"bool", fixtureConfig, "check", "false", false,
			[]string{"[update]", "check = false"},
			nil,
		},
		{
			"reset root key", fixtureConfig, "repos_root", "", true,
			[]string{"# my own note", `editor = "nano -w"`},
			[]string{"repos_root"},
		},
		{
			"reset table key", fixtureConfig, "theme", "", true,
			[]string{`after_disconnect = "exit"`},
			[]string{"theme"},
		},
		{
			"empty text removes", fixtureConfig, "editor", "", false,
			[]string{"# my own note"},
			[]string{"editor", "nano"},
		},
		{
			"reset absent key", fixtureConfig, "welcome", "", true,
			[]string{`editor = "nano -w"`, `theme = "dark"`},
			nil,
		},
	}

	for _, tt := range tests {
		s := newSandbox(t)

		if tt.file != "" {
			writeFile(t, s.Opts.Paths.ConfigFile(), tt.file)
		}

		e := newRig(t, s).r.e
		d := settingDefs[rowOf(t, tt.id)]

		if err := writeSetting(e.Tr, s.Opts.Paths, d, tt.value, tt.reset); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}

		got := readConfig(t, s)

		for _, w := range tt.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: file lacks %q:\n%s", tt.name, w, got)
			}
		}

		for _, n := range tt.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: file still holds %q:\n%s", tt.name, n, got)
			}
		}

		if _, err := config.LoadSettings(s.Opts.Paths.ConfigFile()); err != nil {
			t.Errorf("%s: the loader refuses what was written: %v\n%s", tt.name, err, got)
		}

		info, err := os.Stat(s.Opts.Paths.ConfigFile())
		if err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: mode of config.toml: %v, %v", tt.name, info, err)
		}
	}

	broken := "schema = 1\nlang = \"fr\n"
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.ConfigFile(), broken)

	e := newRig(t, s).r.e

	err := writeSetting(e.Tr, s.Opts.Paths, settingDefs[rowOf(t, "editor")], "vi", false)
	if err == nil {
		t.Error("a file with a syntax error was written")
	}

	if got := readConfig(t, s); got != broken {
		t.Errorf("a refused write changed the file:\n%s", got)
	}

	var ue *userError
	if err != nil && !asUserError(err, &ue) {
		t.Errorf("the refusal is not a message for the user: %T", err)
	}
}

// TestConfigChoiceFlow changes a choice through the interface: the pick overlay writes the file
// and the running interface follows.
//
// Parameters:
//   - t: test to report to
func TestConfigChoiceFlow(t *testing.T) {
	g, s := configRig(t, fixtureConfig)

	selectRow(g, "after_disconnect")
	g.press("enter")

	if _, ok := g.top().(*pickOverlay); !ok {
		t.Fatalf("enter opened %T, want the pick overlay", g.top())
	}

	if !strings.Contains(g.text(), "exit (quit on a normal end)") ||
		!strings.Contains(g.text(), "current") {
		t.Errorf("the pick lacks the values or the current mark:\n%s", g.text())
	}

	g.press("up", "enter")
	g.await("the write", func() bool { return g.r.e.Settings.Connect.AfterDisconnect == "menu" })

	if got := readConfig(t, s); !strings.Contains(got, `after_disconnect = "menu"`) ||
		!strings.Contains(got, "# my own note") {
		t.Errorf("config.toml after the change:\n%s", got)
	}

	if !strings.Contains(g.toastText(), "After a connection") {
		t.Errorf("toast = %q", g.toastText())
	}

	if g.top() != nil {
		t.Errorf("the overlay is still open: %T", g.top())
	}

	// A cancelled pick writes nothing.
	before := readConfig(t, s)

	g.press("enter", "down", "esc")

	if readConfig(t, s) != before {
		t.Error("esc in the pick changed the file")
	}
}

// TestConfigLanguageLive checks that the language applies at once, in the screen, the key
// labels and the footer, and that a reset gives the locale back.
//
// Parameters:
//   - t: test to report to
func TestConfigLanguageLive(t *testing.T) {
	g, s := configRig(t, fixtureConfig)

	if !strings.Contains(g.text(), "Settings") {
		t.Fatalf("not in English at the start:\n%s", g.text())
	}

	selectRow(g, "lang")
	g.press("enter", "end", "enter")
	g.await("the language", func() bool { return g.r.e.Tr.Lang() == "fr" })

	if got := readConfig(t, s); !strings.Contains(got, `lang = "fr"`) {
		t.Errorf("config.toml lacks the language:\n%s", got)
	}

	text := g.text()
	words := []string{"R\u00e9glages", "Langue", "Langue de l\u2019interface", "retour"}

	for _, want := range words {
		if !strings.Contains(text, want) {
			t.Errorf("the screen is not French (%q missing):\n%s", want, text)
		}
	}

	if strings.Contains(text, "Settings") || strings.Contains(text, "back") {
		t.Errorf("English text remains:\n%s", text)
	}

	if h := g.r.e.Keys.Back.Help().Desc; h == "back" || h == "" {
		t.Errorf("the key map still speaks English: %q", h)
	}

	g.press("d", "y")
	g.await("the reset", func() bool { return g.r.e.Tr.Lang() == "en" })

	if got := readConfig(t, s); strings.Contains(got, "lang") {
		t.Errorf("the reset left the key:\n%s", got)
	}

	if !strings.Contains(g.text(), "Settings") {
		t.Errorf("not back to English:\n%s", g.text())
	}
}

// TestConfigLocked checks that a row an environment variable overrides cannot be changed, that
// the screen says why, and that nothing is written.
//
// Parameters:
//   - t: test to report to
func TestConfigLocked(t *testing.T) {
	g, s := configRig(t, fixtureConfig, "TOTE_LANG=en")
	before := readConfig(t, s)

	g.press("enter")

	if g.top() != nil {
		t.Errorf("a locked row opened %T", g.top())
	}

	if tt := g.toastText(); !strings.Contains(tt, "TOTE_LANG") {
		t.Errorf("toast = %q, want the variable named", tt)
	}

	g.press("d")

	if !strings.Contains(g.toastText(), "TOTE_LANG") {
		t.Errorf("reset toast = %q", g.toastText())
	}

	if readConfig(t, s) != before {
		t.Error("a locked row was written")
	}

	if !strings.Contains(g.text(), "TOTE_LANG") {
		t.Errorf("the source column does not name the variable:\n%s", g.text())
	}
}

// TestConfigReadOnly checks that a config.toml with a syntax error is shown, says why, and is
// never written.
//
// Parameters:
//   - t: test to report to
func TestConfigReadOnly(t *testing.T) {
	broken := "schema = 1\nlang = \"fr\n"
	g, s := configRig(t, "")
	writeFile(t, s.Opts.Paths.ConfigFile(), broken)
	g.send(reloadMsg{})

	if !strings.Contains(g.text(), "config.toml") || !strings.Contains(g.text(), "line 2") {
		t.Errorf("the screen does not say why the file is read-only:\n%s", g.text())
	}

	for _, id := range []string{"lang", "editor", "check"} {
		selectRow(g, id)
		g.press("enter")

		if g.top() != nil {
			t.Errorf("%s: the row opened %T on a broken file", id, g.top())
		}

		if !g.r.toast.err {
			t.Errorf("%s: no error toast: %q", id, g.toastText())
		}
	}

	g.press("d")

	if got := readConfig(t, s); got != broken {
		t.Errorf("the broken file was changed:\n%s", got)
	}

	foot := g.view()[len(g.view())-1]
	if strings.Contains(stripStyles(foot), "change") {
		t.Errorf("the footer offers a change on a read-only file: %q", stripStyles(foot))
	}
}

// TestConfigTextFlow checks the prompt of a text setting: it starts from the current value,
// shows the reason a value is refused, refuses to save it, and writes a good one.
//
// Parameters:
//   - t: test to report to
func TestConfigTextFlow(t *testing.T) {
	g, s := configRig(t, fixtureConfig)

	selectRow(g, "repos_root")
	g.press("enter")

	if !strings.Contains(g.text(), "/srv/work") {
		t.Errorf("the prompt does not start from the current value:\n%s", g.text())
	}

	for range len("/srv/work") {
		g.press("backspace")
	}

	g.typeText("rel/dir")

	if !strings.Contains(g.text(), "absolute") {
		t.Errorf("no reason for a relative path:\n%s", g.text())
	}

	before := readConfig(t, s)

	g.press("enter")

	if _, ok := g.top().(*promptOverlay); !ok {
		t.Fatalf("enter left the prompt on a refused value: %T", g.top())
	}

	if readConfig(t, s) != before {
		t.Error("a refused value was written")
	}

	for range len("rel/dir") {
		g.press("backspace")
	}

	g.typeText("~/code")
	g.press("enter")
	g.await("the write", func() bool { return g.r.e.Settings.ReposRoot == "~/code" })

	if got := readConfig(t, s); !strings.Contains(got, `repos_root = "~/code"`) ||
		!strings.Contains(got, "# trailing note") {
		t.Errorf("config.toml:\n%s", got)
	}

	selectRow(g, "editor")
	g.press("enter", "esc")

	if g.top() != nil || strings.Contains(readConfig(t, s), "esc") {
		t.Error("esc in the prompt did not just close it")
	}
}

// TestConfigPromptKeysAreText checks that "?", "q" and "," are characters in a prompt, not
// commands, and that F1 still opens the help over it.
//
// Parameters:
//   - t: test to report to
func TestConfigPromptKeysAreText(t *testing.T) {
	g, _ := configRig(t, fixtureConfig)

	selectRow(g, "editor")
	g.press("enter")
	g.typeText("q?,")

	if _, ok := g.top().(*promptOverlay); !ok || g.Quit {
		t.Fatalf("a typed key left the prompt: %T quit=%v", g.top(), g.Quit)
	}

	if !strings.Contains(g.text(), "nano -wq?,") {
		t.Errorf("the characters were not typed:\n%s", g.text())
	}

	g.press("f1")

	if _, ok := g.top().(*helpOverlay); !ok {
		t.Errorf("F1 over a prompt opened %T", g.top())
	}
}

// TestConfigSwitchAndEffects flips the boolean, then checks that the theme and the layout
// follow a change at once.
//
// Parameters:
//   - t: test to report to
func TestConfigSwitchAndEffects(t *testing.T) {
	g, s := configRig(t, fixtureConfig)

	selectRow(g, "check")
	g.press("enter")
	g.await("the switch", func() bool { return !g.r.e.Settings.Update.Check })

	if !strings.Contains(readConfig(t, s), "check = false") {
		t.Errorf("config.toml:\n%s", readConfig(t, s))
	}

	g.press("enter")
	g.await("the switch back", func() bool { return g.r.e.Settings.Update.Check })

	selectRow(g, "theme")
	g.press("enter")

	for g.top().(*pickOverlay).cur < 2 {
		g.press("down")
	}

	g.press("enter")
	g.await("the theme", func() bool { return g.r.e.Settings.UI.Theme == "light" })

	if g.r.mode != modeLight {
		t.Errorf("mode = %d after theme = light, want %d", g.r.mode, modeLight)
	}

	selectRow(g, "compact")
	g.press("enter", "down", "enter")
	g.await("the layout", func() bool { return g.r.e.Settings.UI.Compact == "always" })

	if g.r.e.Layout != layoutCompact {
		t.Errorf("layout = %d after compact = always", g.r.e.Layout)
	}
}

// TestConfigReset checks the key d: it asks, then removes a key that the file sets, and says so
// when the row already has its default.
//
// Parameters:
//   - t: test to report to
func TestConfigReset(t *testing.T) {
	g, s := configRig(t, fixtureConfig)

	selectRow(g, "welcome")
	g.press("d")

	if !strings.Contains(g.toastText(), "default") {
		t.Errorf("toast = %q, want the default named", g.toastText())
	}

	selectRow(g, "after_disconnect")
	g.press("d")

	if _, ok := g.top().(*ask); !ok {
		t.Fatalf("d did not ask before the reset: %T", g.top())
	}

	g.press("y")
	g.await("the reset", func() bool { return g.r.e.Settings.Connect.AfterDisconnect == "menu" })

	if got := readConfig(t, s); strings.Contains(got, "after_disconnect") ||
		!strings.Contains(got, "[connect]") {
		t.Errorf("config.toml after the reset:\n%s", got)
	}

	if !strings.Contains(g.toastText(), "default") {
		t.Errorf("toast = %q", g.toastText())
	}
}

// TestConfigMovement checks the keys that move on the settings screen and the way back.
//
// Parameters:
//   - t: test to report to
func TestConfigMovement(t *testing.T) {
	g, _ := configRig(t, "")
	c := g.r.scr.(*configScreen)

	g.press("down", "down")

	if c.sel != 2 {
		t.Errorf("sel = %d after two downs", c.sel)
	}

	g.press("end")

	if c.sel != len(settingDefs)-1 {
		t.Errorf("end: sel = %d", c.sel)
	}

	g.press("down")

	if c.sel != len(settingDefs)-1 {
		t.Errorf("down past the end: sel = %d", c.sel)
	}

	g.press("home", "up")

	if c.sel != 0 {
		t.Errorf("home then up: sel = %d", c.sel)
	}

	g.press("pgdown")

	if c.sel == 0 {
		t.Error("pgdown did not move")
	}

	g.press("esc")

	if _, ok := g.r.scr.(*homeScreen); !ok {
		t.Errorf("esc went to %T", g.r.scr)
	}

	g.press(",")

	if _, ok := g.r.scr.(*configScreen); !ok {
		t.Errorf("\",\" opened %T", g.r.scr)
	}
}

// TestConfigInfoRows checks the rows that are not settings: the colors in use and the update.
//
// Parameters:
//   - t: test to report to
func TestConfigInfoRows(t *testing.T) {
	g, s := configRig(t, fixtureConfig, "TOTE_COLOR=256")
	before := readConfig(t, s)

	selectRow(g, "colors")
	g.press("enter", "d")

	if g.top() != nil || readConfig(t, s) != before {
		t.Error("the colors row changed something")
	}

	if !strings.Contains(g.text(), "256") || !strings.Contains(g.text(), "TOTE_COLOR") {
		t.Errorf("the colors row does not report the profile and its variable:\n%s", g.text())
	}

	selectRow(g, "update")
	g.press("enter")

	if !strings.Contains(g.toastText(), "cannot check") {
		t.Errorf("update row without a check: toast %q", g.toastText())
	}
}

// TestConfigHostileFile checks that a config.toml built to make the TOML library allocate a lot
// is refused before it is decoded, and shown as read-only: a dotted table header of thousands of
// parts costs hundreds of megabytes to decode, and nesting costs stack.
//
// Parameters:
//   - t: test to report to
func TestConfigHostileFile(t *testing.T) {
	files := map[string]string{
		"dotted header": "schema = 1\n[" + strings.Repeat("a.", 4000) + "a]\n",
		"dotted key":    "schema = 1\n" + strings.Repeat("a.", 4000) + "a = 1\n",
		"nested arrays": "schema = 1\nx = " + strings.Repeat("[", 5000) +
			strings.Repeat("]", 5000) + "\n",
		"nested tables": "schema = 1\nx = " + strings.Repeat("{a=", 3000) + "1" +
			strings.Repeat("}", 3000) + "\n",
	}

	for name, content := range files {
		s := newSandbox(t)
		writeFile(t, s.Opts.Paths.ConfigFile(), content)

		st := readFileState(s.Opts.Paths.ConfigFile())
		if st.err == nil {
			t.Errorf("%s: the file was accepted", name)
		}

		g := newRig(t, s.with(func(o *Options) { o.Start = StartConfig }))
		g.press("enter")

		if g.top() != nil || !g.r.toast.err {
			t.Errorf("%s: a setting of a hostile file can be changed (%T, %q)", name, g.top(),
				g.toastText())
		}

		if got := readConfig(t, s); got != content {
			t.Errorf("%s: the file was rewritten", name)
		}
	}
}

// sgrOf returns the escape sequence that starts a style.
//
// Parameters:
//   - st: the style, rendered on a probe text
func sgrOf(st interface{ Render(...string) string }) string {
	out := st.Render("X")

	return out[:strings.Index(out, "X")]
}

// rowLine returns the line of the screen where a row of the list is drawn.
//
// Parameters:
//   - screen: the lines of the screen
//   - name: the text of the name of the row
func rowLine(screen []string, name string) (string, bool) {
	for _, l := range screen {
		if strings.HasPrefix(strings.TrimLeft(strings.TrimPrefix(strings.TrimLeft(l, " "),
			"\u276f"), " "), name+"  ") {
			return l, true
		}
	}

	return "", false
}

// TestConfigRows checks the rows of the settings screen: the name in bold, the description muted
// and the effective value aligned at the right edge, the same column for every row, and the
// description dropped before the name or the value are cut when the screen is narrow.
//
// Parameters:
//   - t: test to report to
func TestConfigRows(t *testing.T) {
	g, _ := configRig(t, fixtureConfig)
	e := g.r.e

	g.resize(130, 30)
	g.press("end")

	styled := strings.Split(g.r.render(), "\n")
	plain := strings.Split(g.text(), "\n")
	edge := -1

	for _, d := range settingDefs {
		name, brief := e.t(d.nameKey()), e.t(d.briefKey())
		if d.id == "colors" {
			brief = e.t("ui_src_locked", "TOTE_COLOR")
		}

		line, ok := rowLine(plain, name)

		if !ok {
			t.Errorf("%s: no row on the screen:\n%s", d.id, g.text())

			continue
		}

		line = strings.TrimRight(line, " ")

		if !strings.Contains(line, brief) {
			t.Errorf("%s: the row lacks its description %q: %q", d.id, brief, line)
		}

		if end := e.Metric.width(line); edge < 0 {
			edge = end
		} else if end != edge {
			t.Errorf("%s: the value ends at column %d, the others at %d: %q", d.id, end, edge, line)
		}
	}

	row := -1

	for i, l := range styled {
		if strings.Contains(stripStyles(l), "Editor  ") {
			row = i
		}
	}

	if row < 0 {
		t.Fatalf("no row for the editor:\n%s", g.text())
	}

	if !strings.Contains(styled[row], sgrOf(e.Theme.Bold)+"Editor") {
		t.Errorf("the name is not bold: %q", styled[row])
	}

	if !strings.Contains(styled[row], sgrOf(e.Theme.Muted)+e.t("ui_setb_editor")) {
		t.Errorf("the description is not muted: %q", styled[row])
	}

	g.resize(60, 24)

	for _, l := range strings.Split(g.text(), "\n") {
		if strings.Contains(l, "Program that") {
			t.Errorf("a narrow screen keeps the description: %q", l)
		}
	}

	narrow := strings.Split(g.text(), "\n")

	for _, d := range settingDefs {
		if _, ok := rowLine(narrow, e.t(d.nameKey())); !ok {
			t.Errorf("%s: the narrow screen lost the row", d.id)
		}
	}
}

// TestConfigLockedRow checks that the row an environment variable overrides names the variable in
// place of its description, and that the screen refuses to edit it.
//
// Parameters:
//   - t: test to report to
func TestConfigLockedRow(t *testing.T) {
	g, s := configRig(t, fixtureConfig, "TOTE_LANG=en")
	g.resize(130, 30)
	g.press("end")

	line, ok := rowLine(strings.Split(g.text(), "\n"), g.r.e.t("ui_set_lang"))
	if !ok || !strings.Contains(line, "TOTE_LANG (read-only)") {
		t.Errorf("the row does not name the variable (%v): %q", ok, line)
	}

	before := readConfig(t, s)

	selectRow(g, "lang")
	g.press("enter")

	if g.top() != nil || readConfig(t, s) != before {
		t.Errorf("the locked row opened %T or changed the file", g.top())
	}
}

// TestSettingDetails checks that v shows the selected setting whole at 80 columns, for every
// setting and in both languages: the panel under the rows cuts a long description, and nothing
// else shows its end.
//
// Parameters:
//   - t: test to report to
func TestSettingDetails(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		t.Run(lang, func(t *testing.T) {
			g := newRig(t, newSandbox(t).with(func(o *Options) {
				o.Width, o.Height = 80, 24
				o.Printer = newPrinter(lang)
			}))
			g.send(openConfigMsg{})

			e := g.r.e
			c := g.r.scr.(*configScreen)

			for i, d := range settingDefs {
				c.sel = i

				g.press("v")

				if _, ok := g.top().(*infoOverlay); !ok {
					t.Fatalf("%s: v opened %T", d.path(), g.top())
				}

				text := g.text()
				if strings.Contains(text, e.Glyphs.ellipsis) {
					t.Errorf("%s: the details are cut:\n%s", d.path(), text)
				}

				words := strings.Fields(c.facts(e).desc)
				if last := words[len(words)-1]; !strings.Contains(text, last) {
					t.Errorf("%s: the details lack the end of the description %q:\n%s", d.path(),
						last, text)
				}

				g.press("v")

				if g.top() != nil {
					t.Fatalf("%s: a second v left %T open", d.path(), g.top())
				}
			}
		})
	}
}

// TestSettingDetailsSmall checks that the details of a setting fit the smallest terminal, and
// that the keys of the screen list the key that opens them.
//
// Parameters:
//   - t: test to report to
func TestSettingDetailsSmall(t *testing.T) {
	g, _ := configRig(t, "")
	g.resize(24, 7)
	selectRow(g, "compact")
	g.press("v")

	if _, ok := g.top().(*infoOverlay); !ok {
		t.Fatalf("v opened %T", g.top())
	}

	checkScreen(t, "setting details", g.r.render(), 24, 7, false)
	g.press("esc")

	found := false

	for _, b := range configBindings(g.r.e, settingDefs[0], false, false) {
		found = found || b.Help().Key == "v"
	}

	if !found {
		t.Error("the keys of the settings screen lack v")
	}
}
