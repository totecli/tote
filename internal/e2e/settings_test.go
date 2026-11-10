package e2e

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// userConfig is a config.toml with comments that a change made in the interface must keep.
	userConfig = `# my own note
schema = 1
editor = "nano -w"  # trailing note

[connect]
after_disconnect = "menu"  # keep this one

[ui]
compact = "never"
`
)

// configPath returns the config.toml of a sandbox.
//
// Parameters:
//   - sb: sandbox
func configPath(sb *sandbox) string {
	return filepath.Join(sb.Dir, "config", "config.toml")
}

// TestConfigScreen opens the settings from the welcome page, changes a choice and the language
// through the interface, and checks config.toml on disk: the new values are there, the comments
// and the other keys are still there, and the language changed on screen at once.
//
// Parameters:
//   - t: test to report to
func TestConfigScreen(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, configPath(sb), userConfig)

	tm := newTerm(t, sb, startOptions{W: 100, H: 30, Env: []string{"TOTE_LANG="}}, "tote")

	tm.waitText("Connections")
	tm.typeText(",")
	tm.waitText("Settings")
	tm.waitText("After a connection")
	tm.waitText("nano -w")

	// After a connection: the fourth row.
	tm.key("Down", "Down", "Down")
	tm.key("Enter")
	tm.waitText("exit (quit on a normal end)")
	tm.key("Down", "Enter")
	tm.waitText("saved: After a connection")

	got := tm.waitFile("../config/config.toml", func(c string) bool {
		return strings.Contains(c, `after_disconnect = "exit"`)
	})

	for _, want := range []string{
		"# my own note", "# trailing note", `editor = "nano -w"`,
		`compact = "never"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config.toml lost %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, `"menu"`) {
		t.Errorf("config.toml still holds the old value:\n%s", got)
	}

	// The language applies at once.
	tm.key("Home", "Enter")
	tm.waitText("Fran")
	tm.key("Down", "Down", "Enter")
	tm.waitText("R\u00e9glages")
	tm.waitText("Langue")

	got = tm.waitFile("../config/config.toml", func(c string) bool {
		return strings.Contains(c, `lang = "fr"`)
	})

	if !strings.Contains(got, "# my own note") ||
		!strings.Contains(got, `after_disconnect = "exit"`) {
		t.Errorf("config.toml after the language change:\n%s", got)
	}

	// Back to the default: the letter asks first, then the key is removed.
	tm.key("d")
	tm.waitText("R\u00e9tablir la valeur par d\u00e9faut")
	tm.key("y")
	tm.waitText("Settings")

	got = tm.waitFile("../config/config.toml", func(c string) bool {
		return !strings.Contains(c, "lang")
	})

	if !strings.Contains(got, "# trailing note") {
		t.Errorf("config.toml after the reset:\n%s", got)
	}

	tm.key("Escape")
	tm.waitText("Your terminal tools")
	tm.quit()
}

// TestConfigArrows presses the left and right keys on the settings screen of the real binary: the
// right key selects the next value of a choice and wraps around at the end, the left key goes back
// and turns a switch off, the letter l turns it on again, the selected row shows its value between
// angle quotes, and config.toml keeps its comments and its other keys.
//
// Parameters:
//   - t: test to report to
func TestConfigArrows(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, configPath(sb), userConfig)

	tm := newTerm(t, sb, startOptions{W: 100, H: 30, Env: []string{"TOTE_LANG="}}, "tote")

	tm.waitText("Connections")
	tm.typeText(",")
	tm.waitText("After a connection")

	has := func(want string) func(string) bool {
		return func(c string) bool { return strings.Contains(c, want) }
	}

	// After a connection: the fourth row, two values, "menu" in the file.
	tm.key("Down", "Down", "Down")
	tm.waitText("\u2039 menu (back to the list) \u203a")
	tm.key("Right")
	tm.waitText("saved: After a connection")
	tm.waitFile("../config/config.toml", has(`after_disconnect = "exit"`))
	tm.waitText("\u2039 exit (quit on a normal end) \u203a")

	tm.key("Right")
	tm.waitFile("../config/config.toml", has(`after_disconnect = "menu"`))

	tm.key("Left")
	tm.waitFile("../config/config.toml", has(`after_disconnect = "exit"`))

	// SSH keepalive: a switch that is on, so the left key turns it off and the letter l on again.
	tm.key("Down")
	tm.waitText("\u2039 Yes \u203a")
	tm.key("Left")
	tm.waitFile("../config/config.toml", has("keepalive = false"))
	tm.waitText("\u2039 No \u203a")

	tm.typeText("l")
	got := tm.waitFile("../config/config.toml", has("keepalive = true"))

	for _, want := range []string{
		"# my own note", "# trailing note", `editor = "nano -w"`,
		`compact = "never"`, `after_disconnect = "exit"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config.toml lost %q:\n%s", want, got)
		}
	}

	tm.key("Escape")
	tm.waitText("Your terminal tools")
	tm.quit()
}

// TestConfigLockedAndBroken checks that a setting an environment variable fixes cannot be
// changed, and that a config.toml with a syntax error is shown and left alone.
//
// Parameters:
//   - t: test to report to
func TestConfigLockedAndBroken(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.write(t, configPath(sb), userConfig)

	tm := newTerm(t, sb, startOptions{}, "tote")
	tm.waitText("Connections")
	tm.typeText(",")
	tm.waitText("TOTE_LANG")
	tm.key("Enter")
	tm.waitText("is set by TOTE_LANG")
	tm.quit()

	if got := readFile(t, configPath(sb)); got != userConfig {
		t.Errorf("a locked setting changed the file:\n%s", got)
	}

	sb = newSandbox(t)

	const broken = "schema = 1\nlang = \"fr\n"

	sb.write(t, configPath(sb), broken)

	tm = newTerm(t, sb, startOptions{}, "tote")
	tm.waitText("Connections")
	tm.typeText(",")
	tm.waitText("read-only")
	tm.key("Down", "Enter")
	tm.waitText("syntax error")
	tm.quit()

	if got := readFile(t, configPath(sb)); got != broken {
		t.Errorf("a broken file was changed:\n%s", got)
	}
}

// TestHelpScreen opens the help with ? from a list, from the filter with F1, scrolls to the end,
// and closes it with the key that opened it and with Esc.
//
// Parameters:
//   - t: test to report to
func TestHelpScreen(t *testing.T) {
	t.Parallel()

	tm := newTerm(t, newSandbox(t), startOptions{W: 100, H: 24}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.typeText("?")
	tm.waitText("Quick start")
	tm.waitText("put at prompt")
	tm.waitText("next user")

	tm.key("End")
	tm.waitText("tote uninstall")

	tm.typeText("?")
	tm.waitGone("Quick start")
	tm.waitText(cmdOf("alpha"))

	tm.typeText("/")
	tm.typeText("al?")
	tm.waitText("al?")
	tm.key("F1")
	tm.waitText("Quick start")
	tm.key("Escape")
	tm.waitGone("Quick start")
	tm.waitText("al?")

	tm.key("Escape")
	tm.waitGone("No result")
	tm.waitText(cmdOf("alpha"))
	tm.quit()
}

// TestCopyAndCommand checks the keys C and c in a tote started without the shell wrapper: C cannot
// use the clipboard of this terminal (a plain screen terminal) and shows the text to select, and
// c cannot hand the command line to a shell that is not listening, so the interface stays open and
// shows the command with the line that enables the wrapper.
//
// Parameters:
//   - t: test to report to
func TestCopyAndCommand(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	tm := newTerm(t, sb, startOptions{W: 140, H: 24}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.typeText("C")
	tm.waitText("install xclip, xsel or wl-clipboard, or press v to select it")

	tm.typeText("u")
	tm.waitText("next connection as root")
	tm.typeText("c")
	tm.waitText("tote init bash")

	if s := tm.look(); !s.Alt || !s.has("ssh -l root -- alpha.example.com") ||
		!s.has("shell wrapper is not active") {
		t.Errorf("the interface did not stay open with the command:\n%s", s.Text)
	}

	tm.quit()
}

// TestCopyKeyTool checks the key C in a real terminal where a clipboard program is found: the
// program gets the command on its standard input and the toast names it, so that the terminal is
// not asked to set the clipboard.
//
// Parameters:
//   - t: test to report to
func TestCopyKeyTool(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	installXclip(t, sb)

	tm := newTerm(t, sb, startOptions{W: 140, H: 24, Env: []string{"DISPLAY=:0"}}, "tote", "co")

	tm.waitText(cmdOf("alpha"))
	tm.typeText("C")
	tm.waitText("Copied with xclip")

	got := tm.waitFile("clip.txt", func(c string) bool { return c != "" })
	if got != "ssh -l deploy -- alpha.example.com" {
		t.Errorf("xclip got %q, want the command of the connection", got)
	}

	tm.quit()
}

// TestUpdateKeys builds a real binary, publishes a newer release on a file:// mirror and checks
// the keys u and U: u only reads what is known, U asks the mirror now even though the start-up
// check is switched off, and the failure of a check is a message.
//
// Parameters:
//   - t: test to report to
func TestUpdateKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}

	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	id := bytes.Repeat([]byte{7}, 8)
	work := t.TempDir()
	bin := filepath.Join(work, "tote")

	goBuild(t, bin, "1.0.0", "")

	mirror := t.TempDir()
	releaseMirror(t, mirror, bin, "1.1.0", priv, id)

	run := func(t *testing.T, base string) *term {
		t.Helper()

		sb := newSandbox(t)
		sb.write(t, configPath(sb), "schema = 1\n[update]\ncheck = false\n")

		env := []string{"TOTE_UPDATE_BASE=" + base}

		return newTerm(t, sb, startOptions{W: 110, H: 24, Env: env}, bin)
	}

	t.Run("a newer release", func(t *testing.T) {
		t.Parallel()

		tm := run(t, "file://"+mirror)
		tm.waitText("Connections")

		tm.typeText("u")
		tm.waitText("no update known, press U to check now")

		tm.typeText("U")
		tm.waitText("update 1.1.0 available, install it with: tote update")

		if _, err := os.Stat(filepath.Join(tm.Sb.Dir, "state", "install.json")); err == nil {
			t.Error("a check wrote an install receipt")
		}

		// The notice stays on the status line once the message is gone, and u shows it again.
		tm.typeText("1")
		tm.waitText("alpha.example.com")
		tm.waitGone("install it with: tote update")
		tm.waitText("update 1.1.0 available")
		tm.key("Escape")
		tm.waitText("Your terminal tools")
		tm.typeText("u")
		tm.waitText("install it with: tote update")
		tm.quit()
	})

	t.Run("a failed check", func(t *testing.T) {
		t.Parallel()

		tm := run(t, "file://"+filepath.Join(work, "no-such-mirror"))
		tm.waitText("Connections")
		tm.typeText("U")
		tm.waitText("update check failed")
		tm.quit()
	})

	t.Run("from the settings", func(t *testing.T) {
		t.Parallel()

		tm := run(t, "file://"+mirror)
		tm.waitText("Connections")
		tm.typeText(",")
		tm.waitText("Check for update")
		tm.key("End")
		tm.key("Enter")
		tm.waitText("update 1.1.0 available")
		tm.quit()
	})
}
