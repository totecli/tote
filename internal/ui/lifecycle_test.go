package ui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestReviewRestoreErrorKeepsInterface pins that a program that ended well but whose terminal
// could not be restored never ends the interface, whatever the hand-off asked for: the user must
// see the error instead of landing in a half restored terminal. It kills the mutant that drops
// the Restore check from execDone.
//
// Parameters:
//   - t: test to report to
func TestReviewRestoreErrorKeepsInterface(t *testing.T) {
	tests := []struct {
		name string
		next after
		res  RunResult
	}{
		{"quit, clean end", after{Quit: true}, RunResult{}},
		{"tolerant quit, clean end", after{Quit: true, Tolerant: true}, RunResult{}},
		{
			"tolerant quit, remote status",
			after{Quit: true, Tolerant: true},
			RunResult{ExitCode: 3},
		},
	}

	for _, tt := range tests {
		g := newRig(t, newSandbox(t))

		g.send(execDoneMsg{
			Label: "ssh prod", Result: tt.res, After: tt.next,
			Restore: errors.New("cannot restore"),
		})

		if g.Quit {
			t.Errorf("%s: the interface quit after a restore failure", tt.name)
		}

		if g.toastText() == "" {
			t.Errorf("%s: no message about the restore failure", tt.name)
		}
	}
}

// TestReviewTypedKeysStayText pins that the keys that open the settings and the help are text
// while a filter has the focus: the filter holds the typed character and the list stays on
// screen. It kills the mutant that lets "," open the settings from a typing screen.
//
// Parameters:
//   - t: test to report to
func TestReviewTypedKeysStayText(t *testing.T) {
	for _, typed := range []string{",", "?", "q", "1", "u", "U"} {
		g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))

		g.press("/")
		g.typeText(typed)

		list, ok := g.r.scr.(*listScreen)
		if !ok {
			t.Errorf("%q: typing in the filter left the list: %T", typed, g.r.scr)

			continue
		}

		if got := list.flt.String(); got != typed || !list.flt.typing {
			t.Errorf("%q: the filter holds %q (typing %v)", typed, got, list.flt.typing)
		}

		if g.Quit || len(g.r.overlays) != 0 {
			t.Errorf("%q: quit %v, overlays %d", typed, g.Quit, len(g.r.overlays))
		}
	}
}

// TestReviewLanguageSwitchReachesData pins that the language chosen in the settings reaches the
// printer of the data set, whose messages (kind names, read-only reasons, write errors) are shown
// outside the settings screen. It kills the mutant that leaves the data set with the old printer.
//
// Parameters:
//   - t: test to report to
func TestReviewLanguageSwitchReachesData(t *testing.T) {
	g, _ := configRig(t, fixtureConfig)

	english := g.r.e.Data.kindName(kindConn)

	selectRow(g, "lang")
	g.press("enter", "end", "enter")
	g.await("the language", func() bool { return g.r.e.Tr.Lang() == "fr" })

	french := g.r.e.Data.kindName(kindConn)
	if french == english || french != g.r.e.Tr.T(kindConn.singularKey()) {
		t.Errorf("kind name %q after the switch, was %q: the data set kept the old printer",
			french, english)
	}
}

// TestReviewResetAbsentKeyWritesNothing pins that "d" on a row whose key is not in config.toml
// says that it already has its default and leaves the file byte for byte as it was, while "d" on a
// key that is there says it is back to its default. The toast of the existing test only checks
// for the word "default", which both messages hold. It kills the mutant that writes anyway.
//
// Parameters:
//   - t: test to report to
func TestReviewResetAbsentKeyWritesNothing(t *testing.T) {
	g, s := configRig(t, fixtureConfig)

	before := readConfig(t, s)

	selectRow(g, "welcome")
	g.press("d")

	if got := g.toastText(); !strings.Contains(got, "already has its default value") {
		t.Errorf("toast for an absent key = %q", got)
	}

	if got := readConfig(t, s); got != before {
		t.Errorf("config.toml changed:\n%s", got)
	}

	selectRow(g, "theme")
	g.press("d", "y")
	g.await("the reset", func() bool { return g.r.e.Settings.UI.Theme == "auto" })

	if got := g.toastText(); !strings.Contains(got, "back to its default") {
		t.Errorf("toast for a present key = %q", got)
	}
}

// TestReviewCopyAndCommandCarryKeepalive pins that the command put in the clipboard and the one
// put at the prompt are the command that Enter would run, keepalive options included, so that
// the user pastes what tote itself would have started. It kills the mutants that leave the
// options out of either.
//
// Parameters:
//   - t: test to report to
func TestReviewCopyAndCommandCarryKeepalive(t *testing.T) {
	const want = "ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -l deploy -- " +
		"prod.example.com"

	rec := &recorder{out: "serveraliveinterval 0\nserveralivecountmax 3\n"}
	open := func() *rig {
		g := newRig(t, newSandbox(t).with(func(o *Options) {
			o.Start = StartConnections
			o.Probe = rec.probe
		}))

		return g
	}

	g := open()
	g.press("C")

	if got, ok := clipboardOf(g); !ok || got != want {
		t.Errorf("clipboard = %q (%v), want %q", got, ok, want)
	}

	g = open()
	g.press("c")

	if g.r.result.Payload != want {
		t.Errorf("prompt = %q, want %q", g.r.result.Payload, want)
	}
}

// TestReviewSourceOfPrecedence pins the order in which the sources of a setting are named when
// several apply, which has to be the order in which the program really reads them: the locale
// variables LC_ALL, LC_MESSAGES then LANG, and VISUAL before EDITOR. A key of the file that holds
// an empty text is still a key of the file. It kills the mutants that swap these orders.
//
// Parameters:
//   - t: test to report to
func TestReviewSourceOfPrecedence(t *testing.T) {
	tests := []struct {
		name string
		file string
		env  []string
		id   string
		from string
		kind origin
	}{
		{
			"LC_ALL first", "",
			[]string{"LC_ALL=fr_FR.UTF-8", "LC_MESSAGES=en", "LANG=en"},
			"lang",
			"LC_ALL", originEnv,
		},
		{
			"LC_MESSAGES before LANG", "",
			[]string{"LC_MESSAGES=fr", "LANG=en"},
			"lang",
			"LC_MESSAGES", originEnv,
		},
		{
			"unsupported locale skipped", "",
			[]string{"LC_ALL=de_DE", "LANG=fr"},
			"lang", "LANG",
			originEnv,
		},
		{
			"VISUAL before EDITOR", "",
			[]string{"EDITOR=vi", "VISUAL=nano"},
			"editor", "VISUAL",
			originEnv,
		},
		{
			"blank VISUAL skipped", "",
			[]string{"EDITOR=vi", "VISUAL= "},
			"editor", "EDITOR",
			originEnv,
		},
		{
			"empty key of the file", "schema = 1\nlang = \"\"\n",
			[]string{"LANG=fr"},
			"lang", "",
			originFile,
		},
	}

	for _, tt := range tests {
		g, s := configRig(t, tt.file, tt.env...)
		g.r.e.Environ = append(slices.DeleteFunc(slices.Clone(g.r.e.Environ), func(v string) bool {
			return strings.HasPrefix(v, "LANG=") || strings.HasPrefix(v, "LC_") ||
				strings.HasPrefix(v, "VISUAL=") || strings.HasPrefix(v, "EDITOR=") ||
				strings.HasPrefix(v, "TOTE_LANG=")
		}), tt.env...)

		got := sourceOf(g.r.e, readFileState(s.Opts.Paths.ConfigFile()),
			settingDefs[rowOf(t, tt.id)])

		if got.origin != tt.kind || got.name != tt.from || got.locked {
			t.Errorf("%s: source %+v, want origin %d from %q, not locked", tt.name, got, tt.kind,
				tt.from)
		}
	}
}

// TestReviewWriteFailureKeepsState pins what the settings screen does when config.toml cannot be
// written (a directory without write permission): an error message, the running settings and
// the file untouched, and the interface still usable. The branch is not covered by the other
// tests. The test is skipped for the super user, to whom permissions mean nothing.
//
// Parameters:
//   - t: test to report to
func TestReviewWriteFailureKeepsState(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not apply to the super user")
	}

	g, s := configRig(t, fixtureConfig)
	dir := filepath.Dir(s.Opts.Paths.ConfigFile())
	before := readConfig(t, s)

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	selectRow(g, "check")
	g.press("enter")
	g.await("the error", func() bool { return g.r.toast.err && g.toastText() != "" })

	if !g.r.e.Settings.Update.Check {
		t.Error("the running settings changed although the write failed")
	}

	if got := readConfig(t, s); got != before {
		t.Errorf("config.toml changed:\n%s", got)
	}

	if g.Quit || len(g.r.overlays) != 0 {
		t.Errorf("the interface left its state: quit %v, overlays %d", g.Quit, len(g.r.overlays))
	}
}
