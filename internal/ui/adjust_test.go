package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// writes returns the results of the writes of settings that the rig has played, in order.
//
// Parameters:
//   - g: rig to read
func writes(g *rig) []settingWroteMsg {
	var out []settingWroteMsg

	for _, m := range g.Seen {
		if w, ok := m.(settingWroteMsg); ok {
			out = append(out, w)
		}
	}

	return out
}

// written returns the values of the writes that the rig has played, in order.
//
// Parameters:
//   - g: rig to read
func written(g *rig) []string {
	var out []string

	for _, w := range writes(g) {
		out = append(out, w.Value)
	}

	return out
}

// press1 delivers one key to the model without running the command it returns, so that a test
// can press several keys before the write of the first one has ended.
//
// Parameters:
//   - g: rig to drive
//   - name: key name, as for press
func press1(g *rig, name string) tea.Cmd {
	g.t.Helper()

	_, cmd := g.r.Update(keyMsg(name))

	return cmd
}

// play runs the commands that press1 returned and delivers what they produce.
//
// Parameters:
//   - g: rig to drive
//   - cmds: commands to run, in order; nil ones are skipped
func play(g *rig, cmds ...tea.Cmd) {
	g.t.Helper()

	for _, c := range cmds {
		for _, m := range collect(c) {
			g.send(m)
		}
	}
}

// idle reports whether no step is asked for or being written.
//
// Parameters:
//   - g: rig to read
func idle(g *rig) bool {
	return g.r.e.Steps.empty()
}

// TestAdjustSwitch steps a switch with the left and right keys: right turns it on, left turns it
// off, and a key that would leave it as it is writes nothing and says nothing.
//
// Parameters:
//   - t: test to report to
func TestAdjustSwitch(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		file  string
		key   string
		want  string
		write bool
	}{
		{"left turns off", "keepalive", "schema = 1\n", "left", "false", true},
		{"h turns off", "keepalive", "schema = 1\n", "h", "false", true},
		{"right when on", "keepalive", "schema = 1\n", "right", "", false},
		{"l when on", "keepalive", "schema = 1\n", "l", "", false},
		{
			"left when off", "keepalive", "schema = 1\n[connect]\nkeepalive = false\n", "left", "",
			false,
		},
		{
			"right turns on", "keepalive", "schema = 1\n[connect]\nkeepalive = false\n", "right",
			"true", true,
		},
		{
			"l turns on", "keepalive", "schema = 1\n[connect]\nkeepalive = false\n", "l", "true",
			true,
		},
		{"mouse left", "mouse", "schema = 1\n", "left", "false", true},
		{"mouse right when on", "mouse", "schema = 1\n", "right", "", false},
		{"check left", "check", "schema = 1\n", "left", "false", true},
		{"check right", "check", "schema = 1\n[update]\ncheck = false\n", "right", "true", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, s := configRig(t, tt.file)
			d, _ := findSetting(tt.id)

			selectRow(g, tt.id)

			before := readConfig(t, s)

			g.press(tt.key)

			if !tt.write {
				g.settle()

				if got := writes(g); len(got) != 0 {
					t.Errorf("%s wrote %v", tt.key, got)
				}

				if readConfig(t, s) != before {
					t.Errorf("%s changed the file:\n%s", tt.key, readConfig(t, s))
				}

				if g.toastText() != "" || g.top() != nil {
					t.Errorf("%s said %q or opened %T", tt.key, g.toastText(), g.top())
				}

				return
			}

			g.await("the write", func() bool { return d.value(g.r.e.Settings) == tt.want })

			if got := readConfig(t, s); !strings.Contains(got, d.key+" = "+tt.want) {
				t.Errorf("config.toml lacks %s = %s:\n%s", d.key, tt.want, got)
			}

			if want := d.applyText(g.r.e); !strings.Contains(g.toastText(), want) {
				t.Errorf("toast = %q, want the scope %q", g.toastText(), want)
			}

			if !idle(g) {
				t.Errorf("the screen still waits for a write: %+v", g.r.scr)
			}
		})
	}
}

// TestAdjustChoice steps a choice with the left and right keys, for 2, 3 and 4 values, wrapping
// around at both ends, and starting from the default when the value is not a choice.
//
// Parameters:
//   - t: test to report to
func TestAdjustChoice(t *testing.T) {
	const (
		exitFile   = "schema = 1\n[connect]\nafter_disconnect = \"exit\"\n"
		neverFile  = "schema = 1\n[ui]\ncompact = \"never\"\n"
		alwaysFile = "schema = 1\n[ui]\ncompact = \"always\"\n"
	)

	tests := []struct {
		name string
		id   string
		file string
		key  string
		want string
	}{
		{"two right", "after_disconnect", "schema = 1\n", "right", "exit"},
		{"two right wraps", "after_disconnect", exitFile, "right", "menu"},
		{"two left wraps", "after_disconnect", "schema = 1\n", "left", "exit"},
		{"two left", "after_disconnect", exitFile, "left", "menu"},
		{"three right", "compact", "schema = 1\n", "right", "always"},
		{"three right wraps", "compact", neverFile, "right", "auto"},
		{"three left wraps", "compact", "schema = 1\n", "left", "never"},
		{"three left", "compact", alwaysFile, "left", "auto"},
		{"three l", "welcome", "schema = 1\n", "l", "compact"},
		{"three h", "after_open", "schema = 1\n", "h", "cd"},
		{"four right", "theme", "schema = 1\n[ui]\ntheme = \"dark\"\n", "right", "light"},
		{"four right wraps", "theme", "schema = 1\n[ui]\ntheme = \"mono\"\n", "right", "auto"},
		{"four left wraps", "theme", "schema = 1\n", "left", "mono"},
		{"language right", "lang", "schema = 1\n", "right", "en"},
		{"language right wraps", "lang", "schema = 1\nlang = \"fr\"\n", "right", ""},
		{"language left wraps", "lang", "schema = 1\n", "left", "fr"},
		{"language left", "lang", "schema = 1\nlang = \"en\"\n", "left", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, s := configRig(t, tt.file, "TOTE_LANG=")
			d, _ := findSetting(tt.id)

			selectRow(g, tt.id)

			if d.value(g.r.e.Settings) == tt.want {
				t.Fatalf("the case does not change %s", tt.id)
			}

			g.press(tt.key)
			g.await("the write", func() bool { return d.value(g.r.e.Settings) == tt.want })

			got := readConfig(t, s)

			switch {
			case tt.want == "":
				if strings.Contains(got, d.key) {
					t.Errorf("the key %s is still in config.toml:\n%s", d.key, got)
				}
			case !strings.Contains(got, d.key+" = \""+tt.want+"\""):
				t.Errorf("config.toml lacks %s = %q:\n%s", d.key, tt.want, got)
			}

			if !idle(g) {
				t.Errorf("the screen still waits for a write")
			}
		})
	}
}

// TestAdjustChoiceStart checks where a step starts when the value is not one of the choices or is
// empty: from the default of the setting, as if it were set to it.
//
// Parameters:
//   - t: test to report to
func TestAdjustChoiceStart(t *testing.T) {
	tests := []struct {
		name string
		cur  string
		key  string
		want string
	}{
		{"unknown right", "weird", "right", "dark"},
		{"unknown left", "weird", "left", "mono"},
		{"empty right", "", "right", "dark"},
		{"empty left", "", "left", "mono"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, _ := configRig(t, "schema = 1\n")
			selectRow(g, "theme")

			g.r.e.Settings.UI.Theme = tt.cur

			g.press(tt.key)
			g.await("the write", func() bool { return g.r.e.Settings.UI.Theme == tt.want })
		})
	}
}

// TestStepChoice walks the choices of a setting directly: two, three and ten values, the wrap
// at both ends, and a value that is not a choice, for a default that is not the first value.
//
// Parameters:
//   - t: test to report to
func TestStepChoice(t *testing.T) {
	ten := []string{"a0", "a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9"}

	for _, n := range []int{2, 3, 10} {
		d := settingDef{id: "none", choices: ten[:n]}

		for i, cur := range d.choices {
			if got, want := stepChoice(d, cur, 1), d.choices[(i+1)%n]; got != want {
				t.Errorf("%d values: right of %s = %s, want %s", n, cur, got, want)
			}

			if got, want := stepChoice(d, cur, -1), d.choices[(i+n-1)%n]; got != want {
				t.Errorf("%d values: left of %s = %s, want %s", n, cur, got, want)
			}
		}

		for _, cur := range []string{"", "zz"} {
			if got := stepChoice(d, cur, 1); got != d.choices[1] {
				t.Errorf("%d values: right of %q = %s, want %s", n, cur, got, d.choices[1])
			}

			if got := stepChoice(d, cur, -1); got != d.choices[n-1] {
				t.Errorf("%d values: left of %q = %s, want %s", n, cur, got, d.choices[n-1])
			}
		}
	}

	// The default of the theme is "auto", the third value here.
	d := settingDef{id: "theme", choices: []string{"dark", "light", "auto", "mono"}}

	if got := stepChoice(d, "zz", 1); got != "mono" {
		t.Errorf("right of an unknown theme = %s, want mono", got)
	}

	if got := stepChoice(d, "", -1); got != "light" {
		t.Errorf("left of an empty theme = %s, want light", got)
	}
}

// TestAdjustRefused checks that the left and right keys refuse what Enter refuses, with the same
// message: a row that an environment variable fixes and a config.toml that cannot be used.
//
// Parameters:
//   - t: test to report to
func TestAdjustRefused(t *testing.T) {
	t.Run("locked by a variable", func(t *testing.T) {
		g, s := configRig(t, fixtureConfig, "TOTE_LANG=en")
		before := readConfig(t, s)

		for _, k := range []string{"left", "right", "h", "l"} {
			g.send(toastMsg{})
			g.press(k)

			if !g.r.toast.err || !strings.Contains(g.toastText(), "TOTE_LANG") {
				t.Errorf("%s: toast = %q, want the variable named", k, g.toastText())
			}
		}

		g.press("enter")

		enter := g.toastText()

		g.press("right")

		if g.toastText() != enter {
			t.Errorf("right says %q, Enter says %q", g.toastText(), enter)
		}

		if got := writes(g); len(got) != 0 || readConfig(t, s) != before {
			t.Errorf("a locked row was written: %v", got)
		}

		if foot := g.footerText(); strings.Contains(foot, g.r.e.Glyphs.leftRight) {
			t.Errorf("the footer of a locked row lists the arrows: %q", foot)
		}

		selectRow(g, "theme")

		if foot := g.footerText(); !strings.Contains(foot, g.r.e.Glyphs.leftRight) {
			t.Errorf("the footer of a row that is not locked lacks the arrows: %q", foot)
		}
	})

	t.Run("file that cannot be used", func(t *testing.T) {
		g, s := configRig(t, "")
		writeFile(t, s.Opts.Paths.ConfigFile(), "schema = 1\nlang = \"fr\n")
		g.send(reloadMsg{})

		before := readConfig(t, s)

		for _, id := range []string{"lang", "keepalive", "theme"} {
			selectRow(g, id)

			for _, k := range []string{"left", "right"} {
				g.send(toastMsg{})
				g.press(k)

				if !g.r.toast.err || !strings.Contains(g.toastText(), "config.toml") {
					t.Errorf("%s %s: toast = %q, want the reason", id, k, g.toastText())
				}

				if g.top() != nil {
					t.Errorf("%s %s opened %T", id, k, g.top())
				}
			}
		}

		if got := writes(g); len(got) != 0 || readConfig(t, s) != before {
			t.Errorf("a broken file was written: %v", got)
		}
	})
}

// TestAdjustIgnored checks that the left and right keys do nothing on the rows they cannot change:
// a text, a row of information and the check for an update. No message, no overlay, no write, and
// the screen stays the settings screen: the keys never go back or home.
//
// Parameters:
//   - t: test to report to
func TestAdjustIgnored(t *testing.T) {
	for _, id := range []string{"editor", "repos_root", "colors", "update"} {
		t.Run(id, func(t *testing.T) {
			g, s := configRig(t, fixtureConfig)
			selectRow(g, id)

			before := readConfig(t, s)
			seen := len(g.Seen)

			for _, k := range []string{"left", "right", "h", "l"} {
				g.press(k)

				if g.toastText() != "" || g.top() != nil || g.r.checking {
					t.Errorf("%s: toast %q, overlay %T, checking %v", k, g.toastText(), g.top(),
						g.r.checking)
				}
			}

			if _, ok := g.r.scr.(*configScreen); !ok {
				t.Errorf("the keys left the settings screen for %T", g.r.scr)
			}

			for _, m := range g.Seen[seen:] {
				switch m.(type) {
				case tea.KeyPressMsg:
				default:
					t.Errorf("the keys made the message %T", m)
				}
			}

			if got := writes(g); len(got) != 0 || readConfig(t, s) != before {
				t.Errorf("the row was written: %v", got)
			}
		})
	}
}

// TestAdjustRapid presses a key several times before the write of the first press has ended: each
// press goes one value further, whatever the stale settings say, a value is never written twice,
// and the last value asked for is the one the file ends with.
//
// Parameters:
//   - t: test to report to
func TestAdjustRapid(t *testing.T) {
	t.Run("choice", func(t *testing.T) {
		g, s := configRig(t, fixtureConfig)
		selectRow(g, "theme")

		first := press1(g, "right")
		second := press1(g, "right")
		third := press1(g, "right")

		if first == nil || second != nil || third != nil {
			t.Fatalf("one write at a time: %v %v %v", first != nil, second != nil, third != nil)
		}

		// From dark: light, mono, then auto.
		if got := g.r.e.Steps.want["theme"]; got != "auto" {
			t.Errorf("after three presses the screen wants %q, want auto", got)
		}

		if g.r.e.Settings.UI.Theme != "dark" {
			t.Errorf("the settings changed before the write ended: %s", g.r.e.Settings.UI.Theme)
		}

		play(g, first)
		g.await("the last value", func() bool { return g.r.e.Settings.UI.Theme == "auto" })

		if got := written(g); !slices.Equal(got, []string{"light", "auto"}) {
			t.Errorf("written = %v, want [light auto]", got)
		}

		if got := readConfig(t, s); !strings.Contains(got, `theme = "auto"`) {
			t.Errorf("config.toml:\n%s", got)
		}

		if !idle(g) {
			t.Error("the screen still waits for a write")
		}
	})

	t.Run("left and right", func(t *testing.T) {
		g, s := configRig(t, "schema = 1\n")
		selectRow(g, "keepalive")

		first := press1(g, "left")
		second := press1(g, "left")
		third := press1(g, "right")

		if first == nil || second != nil || third != nil {
			t.Fatalf("one write at a time: %v %v %v", first != nil, second != nil, third != nil)
		}

		play(g, first)
		g.settle()

		if got := written(g); !slices.Equal(got, []string{"false", "true"}) {
			t.Errorf("written = %v, want [false true]", got)
		}

		if got := readConfig(t, s); !strings.Contains(got, "keepalive = true") {
			t.Errorf("config.toml:\n%s", got)
		}

		if !g.r.e.Settings.Connect.Keepalive || !idle(g) {
			t.Error("the switch is not on again, or the screen still waits")
		}
	})

	t.Run("enter twice", func(t *testing.T) {
		g, _ := configRig(t, "schema = 1\n")
		selectRow(g, "mouse")

		first := press1(g, "enter")
		second := press1(g, "enter")

		if first == nil || second != nil {
			t.Fatalf("one write at a time: %v %v", first != nil, second != nil)
		}

		play(g, first)
		g.settle()

		if got := written(g); !slices.Equal(got, []string{"false", "true"}) {
			t.Errorf("written = %v, want [false true]", got)
		}

		if !g.r.e.Settings.UI.Mouse {
			t.Error("two presses of Enter did not give the switch back")
		}
	})

	t.Run("same key on a switch", func(t *testing.T) {
		g, _ := configRig(t, "schema = 1\n")
		selectRow(g, "check")

		first := press1(g, "left")
		second := press1(g, "left")

		if first == nil || second != nil {
			t.Fatalf("a second left must write nothing: %v %v", first != nil, second != nil)
		}

		play(g, first)
		g.settle()

		if got := written(g); !slices.Equal(got, []string{"false"}) {
			t.Errorf("written = %v, want [false]", got)
		}
	})
}

// TestAdjustFailedWrite checks that a failed write is forgotten along with the steps that waited
// behind it, so that the next step starts from the value that is really in force.
//
// Parameters:
//   - t: test to report to
func TestAdjustFailedWrite(t *testing.T) {
	g, s := configRig(t, fixtureConfig)
	selectRow(g, "theme")

	// The file is replaced by one that no write may touch, after the screen read it.
	broken := "schema = 1\ntheme = \"dark\n"
	writeFile(t, s.Opts.Paths.ConfigFile(), broken)

	first := press1(g, "right")
	second := press1(g, "right")

	if first == nil || second != nil {
		t.Fatalf("one write at a time: %v %v", first != nil, second != nil)
	}

	play(g, first)

	if len(writes(g)) != 1 || writes(g)[0].Err == nil {
		t.Fatalf("writes = %+v, want one failure", writes(g))
	}

	if !g.r.toast.err {
		t.Errorf("no error toast: %q", g.toastText())
	}

	if !idle(g) {
		t.Errorf("the failed write left a step behind: %+v", g.r.scr)
	}

	if g.r.e.Settings.UI.Theme != "dark" {
		t.Errorf("theme = %s after a failed write, want dark", g.r.e.Settings.UI.Theme)
	}

	// With the file back, the step starts from dark again, not from what was asked for.
	writeFile(t, s.Opts.Paths.ConfigFile(), fixtureConfig)
	g.send(reloadMsg{})
	g.press("right")
	g.await("the write", func() bool { return g.r.e.Settings.UI.Theme == "light" })
}

// TestAdjustKeepsTheSelection checks that the end of a write does not take the selection back to
// the row that was written: a key that moves down right after a step, before its write ended, is
// not undone.
//
// Parameters:
//   - t: test to report to
func TestAdjustKeepsTheSelection(t *testing.T) {
	tests := []struct {
		name string
		id   string
		keys []string
	}{
		{"step then down", "keepalive", []string{"left", "down"}},
		{"enter then down", "keepalive", []string{"enter", "down"}},
		{"step then end", "keepalive", []string{"left", "end"}},
		{"choice step then up", "theme", []string{"right", "up"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, _ := configRig(t, "schema = 1\n")
			selectRow(g, tt.id)

			var cmds []tea.Cmd

			for _, k := range tt.keys {
				cmds = append(cmds, press1(g, k))
			}

			want := g.r.scr.(*configScreen).sel

			play(g, cmds...)
			g.settle()

			if got := g.r.scr.(*configScreen).sel; got != want {
				t.Errorf("the selection is row %d after the write, want row %d", got, want)
			}
		})
	}
}

// TestAdjustSurvivesTheScreen checks that a step which waits behind a running write is still
// written when the settings screen was left in the meantime, and that quitting is asked about while
// such a step waits, even in the gap between the end of the write and its result.
//
// Parameters:
//   - t: test to report to
func TestAdjustSurvivesTheScreen(t *testing.T) {
	t.Run("leave the screen", func(t *testing.T) {
		g, s := configRig(t, fixtureConfig)
		selectRow(g, "theme")

		first := press1(g, "right")
		second := press1(g, "right")

		if first == nil || second != nil {
			t.Fatalf("one write at a time: %v %v", first != nil, second != nil)
		}

		g.press("esc")

		if _, ok := g.r.scr.(*homeScreen); !ok {
			t.Fatalf("esc led to %T, want the home screen", g.r.scr)
		}

		play(g, first)
		g.await("the steps", func() bool { return idle(g) })

		if got := written(g); !slices.Equal(got, []string{"light", "mono"}) {
			t.Errorf("written = %v, want [light mono]", got)
		}

		if got := readConfig(t, s); !strings.Contains(got, `theme = "mono"`) {
			t.Errorf("config.toml:\n%s", got)
		}
	})

	t.Run("quit in the gap", func(t *testing.T) {
		g, _ := configRig(t, fixtureConfig)
		selectRow(g, "theme")

		first := press1(g, "right")
		press1(g, "right")

		// The write ends, and its result is not yet delivered to the model.
		res := first()

		if g.r.e.Writes.Load() != 0 {
			t.Fatalf("%d writes still run", g.r.e.Writes.Load())
		}

		if !g.r.e.pendingWrites() {
			t.Error("the step that waits is not counted as pending")
		}

		g.press("ctrl+d")

		if g.Quit || len(g.r.overlays) != 1 {
			t.Errorf("quit with a step waiting: quit %v overlays %d", g.Quit, len(g.r.overlays))
		}

		g.press("esc")
		g.send(res)
		g.await("the steps", func() bool { return idle(g) })

		if g.r.e.pendingWrites() {
			t.Error("the step that waited is still pending")
		}

		if got := written(g); !slices.Equal(got, []string{"light", "mono"}) {
			t.Errorf("written = %v, want [light mono]", got)
		}
	})
}

// TestAdjustWritesOneKey checks that a step changes one key of config.toml and keeps the comments
// and every other key, the way a choice from the list does.
//
// Parameters:
//   - t: test to report to
func TestAdjustWritesOneKey(t *testing.T) {
	g, s := configRig(t, fixtureConfig)
	selectRow(g, "after_disconnect")

	g.press("left")
	g.await("the write", func() bool { return g.r.e.Settings.Connect.AfterDisconnect == "menu" })

	want := strings.Replace(fixtureConfig, `after_disconnect = "exit"`,
		`after_disconnect = "menu"`, 1)

	if got := readConfig(t, s); got != want {
		t.Errorf("config.toml after a step:\n%s\nwant:\n%s", got, want)
	}

	for _, id := range []string{"editor", "repos_root"} {
		selectRow(g, id)
		g.press("left", "right", "h", "l")
	}

	if got := readConfig(t, s); got != want {
		t.Errorf("the keys changed a text:\n%s", got)
	}
}

// TestAdjustFollowsTheScreen checks that the language, which a step changes like Enter does, is
// followed at once, and that the footer and the help speak it.
//
// Parameters:
//   - t: test to report to
func TestAdjustFollowsTheScreen(t *testing.T) {
	g, _ := configRig(t, "schema = 1\n", "TOTE_LANG=")
	selectRow(g, "lang")

	g.press("right", "right")
	g.await("the language", func() bool { return g.r.e.Tr.Lang() == "fr" })

	if !strings.Contains(g.footerText(), "r\u00e9gler") {
		t.Errorf("the footer is not French: %q", g.footerText())
	}
}

// settingLine returns the line of the screen where the row of a setting is drawn, without styles.
//
// Parameters:
//   - g: rig on the settings screen
//   - d: setting to look for
func settingLine(g *rig, d settingDef) (string, bool) {
	e := g.r.e
	name := e.t(d.nameKey())

	for _, l := range strings.Split(g.text(), "\n") {
		t := strings.TrimLeft(strings.TrimPrefix(strings.TrimLeft(l, " "), e.Glyphs.marker), " ")

		if strings.HasPrefix(t, name+"  ") {
			return strings.TrimRight(l, " "), true
		}
	}

	return "", false
}

// isFramed reports whether a line ends with a value between the angle quotes.
//
// Parameters:
//   - g: rig, for the glyph set
//   - line: line of a row, without trailing spaces
func isFramed(g *rig, line string) bool {
	return strings.Contains(line, g.r.e.Glyphs.prev+" ") &&
		strings.HasSuffix(line, " "+g.r.e.Glyphs.next)
}

// TestAdjustArrows checks that the selected row of a switch or a choice shows its value between
// angle quotes, in the glyph set in use, that no other row does, and that the columns do not move
// when the selection does.
//
// Parameters:
//   - t: test to report to
func TestAdjustArrows(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, w := range []int{80, 100, 120} {
			s := newSandbox(t).with(func(o *Options) {
				o.Start, o.Width, o.Height, o.ASCII = StartConfig, w, 30, ascii
			})
			writeFile(t, s.Opts.Paths.ConfigFile(), fixtureConfig)

			g := newRig(t, s)
			e := g.r.e
			ends := map[int]bool{}

			for i, d := range settingDefs {
				selectRow(g, d.id)

				line, ok := settingLine(g, d)
				if !ok {
					t.Fatalf("w=%d ascii=%v %s: no row:\n%s", w, ascii, d.id, g.text())
				}

				want := d.kind == settingBool || d.kind == settingChoice

				if got := isFramed(g, line); got != want {
					t.Errorf("w=%d ascii=%v %s (row %d): framed = %v, want %v: %q", w, ascii, d.id,
						i, got, want, line)
				}

				if want {
					src := sourceOf(e, g.r.scr.(*configScreen).file, d)
					framed := e.Glyphs.prev + " " + valueText(e, d, src) + " " + e.Glyphs.next

					if !strings.HasSuffix(line, framed) {
						t.Errorf("w=%d %s: the row does not end with %q: %q", w, d.id, framed, line)
					}
				}

				ends[e.Metric.width(line)] = true

				for _, other := range settingDefs {
					if l, found := settingLine(g, other); other.id != d.id && found &&
						isFramed(g, l) {
						t.Errorf("w=%d %s selected: row %s is framed too: %q", w, d.id, other.id, l)
					}
				}
			}

			if len(ends) != 1 {
				t.Errorf("w=%d ascii=%v: the right edge of the values moves with the selection: %v",
					w, ascii, ends)
			}
		}
	}
}

// TestAdjustArrowsHidden checks that the angle quotes are not shown for a row that cannot be
// changed now: one that an environment variable fixes, and every row when config.toml cannot be
// used. A narrow screen shows them when they fit and never overflows.
//
// Parameters:
//   - t: test to report to
func TestAdjustArrowsHidden(t *testing.T) {
	g, _ := configRig(t, fixtureConfig, "TOTE_LANG=en")
	prev := g.r.e.Glyphs.prev
	selectRow(g, "lang")

	if strings.Contains(g.text(), prev+" ") {
		t.Errorf("a locked row is framed:\n%s", g.text())
	}

	g, s := configRig(t, "")
	writeFile(t, s.Opts.Paths.ConfigFile(), "schema = 1\nlang = \"fr\n")
	g.send(reloadMsg{})

	for _, id := range []string{"keepalive", "theme"} {
		selectRow(g, id)

		if strings.Contains(g.text(), prev+" ") {
			t.Errorf("%s is framed on a file that cannot be used:\n%s", id, g.text())
		}
	}

	g, _ = configRig(t, fixtureConfig)
	g.resize(60, 24)
	selectRow(g, "keepalive")

	line, ok := rowLine(strings.Split(g.text(), "\n"), g.r.e.t("ui_set_keepalive"))
	if !ok || !strings.Contains(line, prev+" Yes "+g.r.e.Glyphs.next) {
		t.Errorf("a short value is not framed at 60 columns (%v): %q", ok, line)
	}

	checkScreen(t, "arrows 60x24", g.r.render(), 60, 24, false)
}

// TestAdjustFooter checks the hint of the left and right keys on the footer and in the help: it
// is there for a switch and a choice, it is not for a text, an information row, the check for an
// update or a file that cannot be used, and it is worded in both languages.
//
// Parameters:
//   - t: test to report to
func TestAdjustFooter(t *testing.T) {
	tests := []struct {
		lang string
		want string
	}{{"en", "adjust"}, {"fr", "r\u00e9gler"}}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			s := newSandbox(t).with(func(o *Options) {
				o.Start, o.Printer, o.Width, o.Height = StartConfig, newPrinter(tt.lang), 120, 30
			})
			writeFile(t, s.Opts.Paths.ConfigFile(), fixtureConfig)

			g := newRig(t, s)
			hint := g.r.e.Glyphs.leftRight + " " + tt.want

			for _, d := range settingDefs {
				selectRow(g, d.id)

				has := strings.Contains(g.footerText(), hint)
				if want := d.kind == settingBool || d.kind == settingChoice; has != want {
					t.Errorf("%s: footer has the hint = %v, want %v: %q", d.id, has, want,
						g.footerText())
				}
			}

			selectRow(g, "lang")
			g.press("?")

			for _, want := range []string{g.r.e.Glyphs.leftRight, tt.want} {
				if !strings.Contains(g.text(), want) {
					t.Errorf("the help lacks %q:\n%s", want, g.text())
				}
			}
		})
	}

	t.Run("file that cannot be used", func(t *testing.T) {
		g, s := configRig(t, "")
		writeFile(t, s.Opts.Paths.ConfigFile(), "schema = 1\nlang = \"fr\n")
		g.send(reloadMsg{})

		if strings.Contains(g.footerText(), g.r.e.Glyphs.leftRight) {
			t.Errorf("the footer offers the keys on a file that cannot be used: %q",
				g.footerText())
		}
	})

	t.Run("ascii", func(t *testing.T) {
		s := newSandbox(t).with(func(o *Options) {
			o.Start, o.ASCII, o.Width, o.Height = StartConfig, true, 120, 30
		})

		g := newRig(t, s)

		if !strings.Contains(g.footerText(), "left/right adjust") {
			t.Errorf("the ASCII footer lacks the hint: %q", g.footerText())
		}
	})
}

// TestAdjustHintIsNotAClick checks that the hint of the left and right keys, which is two keys,
// is not a spot of the footer that presses one: the mouse does not gain a new action.
//
// Parameters:
//   - t: test to report to
func TestAdjustHintIsNotAClick(t *testing.T) {
	g, _ := configRig(t, fixtureConfig)

	_, spans := g.r.footerDraw(g.r.e.Width)

	for _, sp := range spans {
		if sp.press == "left" || sp.press == "right" || sp.press == "h" || sp.press == "l" {
			t.Errorf("a hint presses %q", sp.press)
		}
	}
}
