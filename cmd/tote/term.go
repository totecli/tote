package main

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/ui"
)

// maxStartupFile is the largest startup file read, in bytes.
const maxStartupFile = 1 << 20

// loadLine matches a line that loads tote into the shell, in any of the usual spellings, and
// captures the shell it is written for.
var loadLine = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./-])tote init (bash|zsh)\b`)

// terminal checks the terminal: TERM, the locale, the colors, the mouse, the clipboard, the size
// and tmux.
func (d *diagnosis) terminal() {
	d.add(d.termName())
	d.add(d.locale())
	d.add(d.colors())
	d.add(d.mouse())
	d.add(d.clipboard())
	d.add(d.size())
	d.add(d.multiplexer())
}

// termName checks TERM.
//
// Returns a warning when it is unset or "dumb", which leave tote without colors and keys.
func (d *diagnosis) termName() finding {
	t := d.env.Getenv("TERM")

	switch t {
	case "":
		return note("term", levelWarn, "cli_doctor_term_unset").fix("cli_doctor_fix_term")
	case "dumb":
		return note("term", levelWarn, "cli_doctor_term_dumb").fix("cli_doctor_fix_term")
	case "linux":
		return note("term", levelOK, "cli_doctor_term_linux")
	}

	return note("term", levelOK, "cli_doctor_term", t)
}

// locale checks that the locale is UTF-8, which the interface needs for its glyphs.
//
// Returns a warning when it is not, the interface then drawing with plain ASCII.
func (d *diagnosis) locale() finding {
	name := ""

	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := d.env.Getenv(k); v != "" {
			name = k + "=" + v
			break
		}
	}

	if name == "" {
		name = "-"
	}

	if !d.env.Term.UTF8 {
		return note("locale", levelWarn, "cli_doctor_locale_ascii", name).
			fix("cli_doctor_fix_locale")
	}

	f := note("locale", levelOK, "cli_doctor_locale_ok", name)
	if d.env.Term.ASCII {
		f = f.detail("cli_doctor_locale_forced")
	}

	return f
}

// colors reports the color profile the interface would choose.
//
// Returns a warning when TOTE_COLOR holds a value that names no profile, or when nothing but
// TERM leaves the interface without color.
func (d *diagnosis) colors() finding {
	t := d.env.Term
	profile := msg{Key: profileKey(t.Color)}

	switch {
	case t.BadColor:
		return note("color", levelWarn, "cli_doctor_color_bad", d.env.Getenv("TOTE_COLOR")).
			fix("cli_doctor_fix_color").detail("cli_doctor_color_chosen", profile)
	case t.Override == "TOTE_COLOR":
		return note("color", levelOK, "cli_doctor_color_tote", profile, d.env.Getenv("TOTE_COLOR"))
	case t.Override == "NO_COLOR":
		return note("color", levelOK, "cli_doctor_color_nocolor")
	case t.Color == "none" && !t.Dumb:
		return note("color", levelWarn, "cli_doctor_color_none").fix("cli_doctor_fix_color")
	}

	return note("color", levelOK, "cli_doctor_color_chosen", profile)
}

// mouse reports whether the interface would use the mouse.
//
// Returns a note only: a mouse that is off is a choice (ui.mouse) or a terminal that has none.
func (d *diagnosis) mouse() finding {
	switch ui.MouseState(d.settings, d.env.Getenv) {
	case "setting":
		return note("mouse", levelOK, "cli_doctor_mouse_setting")
	case "term":
		if t := d.env.Getenv("TERM"); t != "" {
			return note("mouse", levelOK, "cli_doctor_mouse_term", t)
		}

		return note("mouse", levelOK, "cli_doctor_mouse_noterm")
	}

	return note("mouse", levelOK, "cli_doctor_mouse_on")
}

// clipboard reports how a text selected with the mouse would reach the clipboard, following the
// chain of the interface (ui.ClipboardRoute) without running a program.
//
// Where no selection happens (the mouse is off, or there is no terminal at all) the tmux option is
// not queried, and a missing program is only a note.
//
// Returns a note when a program or the sequence of the terminal takes the text, a note too when
// there is no program and no selection can happen, and a warning, with what to install, when the
// interface could only show the selection.
func (d *diagnosis) clipboard() finding {
	unused := ui.MouseState(d.settings, d.env.Getenv) != "on" || !d.env.Interactive
	option := d.env.TmuxClip

	if unused {
		option = func() string { return "" }
	}

	route, tool := ui.ClipboardRoute(d.env.Getenv, option)

	switch {
	case route == "tool":
		return note("clipboard", levelOK, "cli_doctor_clip_tool", tool)
	case route == "osc" && d.env.Interactive:
		return note("clipboard", levelOK, "cli_doctor_clip_osc")
	case unused:
		return note("clipboard", levelOK, "cli_doctor_clip_unused")
	}

	return note("clipboard", levelWarn, "cli_doctor_clip_none").fix("cli_doctor_fix_clip")
}

// profileKey gives the catalog key of the name of a color profile.
//
// Parameters:
//   - name: "truecolor", "256", "16" or "none", as ui.Terminal gives it
func profileKey(name string) string {
	switch name {
	case "truecolor":
		return "cli_doctor_profile_truecolor"
	case "256":
		return "cli_doctor_profile_256"
	case "16":
		return "cli_doctor_profile_16"
	}

	return "cli_doctor_profile_none"
}

// size reports the size of the terminal and the layout the interface gives it.
//
// Returns a warning when the terminal is too small for the interface.
func (d *diagnosis) size() finding {
	if !d.env.Interactive || !d.env.SizeOK {
		return note("size", levelOK, "cli_doctor_size_none")
	}

	w, h := d.env.Width, d.env.Height

	switch ui.LayoutName(w, h, d.settings) {
	case "tiny":
		minW, minH := ui.MinSize()

		return note("size", levelWarn, "cli_doctor_size_tiny", w, h, minW, minH).
			fix("cli_doctor_fix_size")
	case "compact":
		return note("size", levelOK, "cli_doctor_size_compact", w, h)
	}

	return note("size", levelOK, "cli_doctor_size_full", w, h)
}

// multiplexer reports whether tote runs inside tmux or GNU screen.
//
// Returns a warning when TERM is not one a multiplexer sets, which loses colors and keys.
func (d *diagnosis) multiplexer() finding {
	t := d.env.Getenv("TERM")
	inside := strings.HasPrefix(t, "tmux") || strings.HasPrefix(t, "screen")

	switch {
	case d.env.Getenv("TMUX") != "":
		if !inside {
			return note("tmux", levelWarn, "cli_doctor_tmux_term", t).fix("cli_doctor_fix_tmux")
		}

		return note("tmux", levelOK, "cli_doctor_tmux_inside", t)
	case d.env.Getenv("STY") != "":
		if !inside {
			return note("tmux", levelWarn, "cli_doctor_screen_term", t).fix("cli_doctor_fix_tmux")
		}

		return note("tmux", levelOK, "cli_doctor_screen_inside", t)
	}

	return note("tmux", levelOK, "cli_doctor_tmux_none")
}

// startupFiles lists the files in which a user loads tote, for the shell tote supports.
//
// Parameters:
//   - k: the shell
func (d *diagnosis) startupFiles(k shell.Kind) []string {
	home := d.env.Home

	if k == shell.Zsh {
		dir := home
		if z := d.env.Getenv("ZDOTDIR"); z != "" && filepath.IsAbs(z) {
			dir = z
		}

		return []string{
			filepath.Join(dir, ".zshrc"), filepath.Join(dir, ".zprofile"),
			filepath.Join(dir, ".zshenv"), filepath.Join(dir, ".zlogin"),
		}
	}

	return []string{
		filepath.Join(home, ".bashrc"), filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".bash_login"), filepath.Join(home, ".profile"),
	}
}

// loadHit is a line of a startup file that loads tote.
//
// path is the file, line its 1-based number and kind the shell the line is written for.
type loadHit struct {
	path string
	line int
	kind shell.Kind
}

// findLoadLines searches startup files for the lines that load tote. Lines that are comments
// are skipped, and a file that is missing or unreadable has none.
//
// Parameters:
//   - files: startup files to read
func findLoadLines(files []string) []loadHit {
	var out []loadHit

	for _, path := range files {
		data, err := readCapped(path, maxStartupFile)
		if err != nil {
			continue
		}

		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}

			if m := loadLine.FindStringSubmatch(line); m != nil {
				out = append(out, loadHit{path: path, line: i + 1, kind: shell.Kind(m[1])})
			}
		}
	}

	return out
}

// shellIntegration checks that the wrapper function of "tote init" is loaded in this shell.
//
// The wrapper runs the program with TOTE_SHELL set to the name of the shell, so that variable
// is the proof that it is loaded. Without it, the startup files of the user's shell are searched
// for the line that loads tote, without changing anything.
//
// Returns a warning, with the exact line to add, when the wrapper is not in use.
func (d *diagnosis) shellIntegration() finding {
	const id = "shell"

	if v := d.env.Getenv("TOTE_SHELL"); v != "" {
		if _, err := shell.ParseKind(v); err != nil {
			return note(id, levelWarn, "cli_doctor_shell_marker", v).
				fix("cli_doctor_fix_shell_marker")
		}

		return note(id, levelOK, "cli_doctor_shell_active", v)
	}

	name := d.env.Getenv("SHELL")

	kind, err := shell.DetectKind(name)
	if err != nil {
		if name == "" {
			return note(id, levelWarn, "cli_doctor_shell_unset")
		}

		return note(id, levelWarn, "cli_doctor_shell_unsupported", filepath.Base(name))
	}

	file := d.startupFiles(kind)[0]
	line := `eval "$(tote init ` + string(kind) + `)"`
	add := msg{Key: "cli_doctor_fix_shell_add", Args: []any{d.word(file), line}}

	var own, other *loadHit

	hits := findLoadLines(d.startupFiles(kind))
	for i := range hits {
		switch {
		case hits[i].kind == kind && own == nil:
			own = &hits[i]
		case hits[i].kind != kind && other == nil:
			other = &hits[i]
		}
	}

	switch {
	case own != nil:
		return note(id, levelWarn, "cli_doctor_shell_inactive", d.tilde(own.path), own.line).
			fix("cli_doctor_fix_shell_reload", string(kind))
	case other != nil:
		f := note(id, levelWarn, "cli_doctor_shell_wrong", d.tilde(other.path), other.line,
			string(other.kind), string(kind))
		f.Fix = add

		return f
	}

	f := note(id, levelWarn, "cli_doctor_shell_missing")
	f.Fix = add

	return f
}
