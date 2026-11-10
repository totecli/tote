package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clipRig puts fake clipboard programs in a directory of the fixture, makes it the PATH and sets
// variables. The programs are empty scripts that nothing runs: the check only looks for them.
//
// Parameters:
//   - tools: file names of the programs to create
//   - kv: variable names and values, in turns, set after the PATH so that one can replace it
func clipRig(tools []string, kv ...string) func(*fixture) {
	return func(f *fixture) {
		f.mkdir("clip", 0o755)

		for _, name := range tools {
			f.write("clip/"+name, "#!/bin/sh\n", 0o755)
		}

		f.vars["PATH"] = f.path("clip")

		for i := 0; i < len(kv); i += 2 {
			f.vars[kv[i]] = kv[i+1]
		}
	}
}

// clipScenarios covers the clipboard note: each program with the display variable it needs, the
// PATH entries that are not trusted, the terminals that take OSC 52 or not, tmux, and the cases
// where no selection happens.
func clipScenarios() []scenario {
	const (
		tool   = "cli_doctor_clip_tool"
		osc    = "cli_doctor_clip_osc"
		none   = "cli_doctor_clip_none"
		unused = "cli_doctor_clip_unused"
		fix    = "cli_doctor_fix_clip"
	)

	vte := []string{"VTE_VERSION", "7600"}
	mouseOff := func(f *fixture) { f.conf("config.toml", "schema = 1\n[ui]\nmouse = false\n") }
	tmux := func(option string, kv ...string) func(*fixture) {
		return func(f *fixture) {
			base := []string{"TERM", "tmux-256color", "TMUX", "/tmp/t,1,0"}
			clipRig(nil, append(base, kv...)...)(f)
			f.env.TmuxClip = func() string { return option }
		}
	}
	relative := func(f *fixture) {
		clipRig([]string{"xclip"}, append([]string{"DISPLAY", ":0"}, vte...)...)(f)

		rel, err := filepath.Rel(mustGetwd(f), f.path("clip"))
		if err != nil {
			f.t.Fatal(err)
		}

		f.vars["PATH"] = rel
	}

	return []scenario{
		{
			"wl-copy on Wayland", clipRig([]string{"wl-copy"}, "WAYLAND_DISPLAY", "wayland-0"),
			"clipboard", levelOK, tool, "",
			[]string{"wl-copy"},
		},
		{
			"xclip on X11", clipRig([]string{"xclip"}, "DISPLAY", ":0"), "clipboard", levelOK, tool,
			"",
			[]string{"xclip"},
		},
		{
			"xsel on X11", clipRig([]string{"xsel"}, "DISPLAY", ":0"), "clipboard", levelOK, tool,
			"",
			[]string{"xsel"},
		},
		{
			"every program, both displays", clipRig([]string{"wl-copy", "xclip", "xsel"},
				"WAYLAND_DISPLAY", "wayland-0", "DISPLAY", ":0"), "clipboard", levelOK, tool, "",
			[]string{"wl-copy"},
		},
		{
			"xclip and xsel", clipRig([]string{"xsel", "xclip"}, "DISPLAY", ":0"), "clipboard",
			levelOK, tool, "",
			[]string{"xclip"},
		},
		{"a program beats VTE", clipRig([]string{"xclip"}, append([]string{"DISPLAY", ":0"},
			vte...)...), "clipboard", levelOK, tool, "", []string{"xclip"}},

		{
			"xclip without DISPLAY on a plain terminal", clipRig([]string{"xclip"}), "clipboard",
			levelOK, osc, "", nil,
		},
		{"xclip with the Wayland display only", clipRig([]string{"xclip"},
			"WAYLAND_DISPLAY", "wayland-0"), "clipboard", levelOK, osc, "", nil},
		{
			"wl-copy with the X11 display only", clipRig([]string{"wl-copy"}, "DISPLAY", ":0"),
			"clipboard", levelOK, osc, "", nil,
		},
		{
			"no program on a plain terminal", clipRig(nil, "DISPLAY", ":0"), "clipboard", levelOK,
			osc, "", nil,
		},
		{"no PATH on a plain terminal", noPath, "clipboard", levelOK, osc, "", nil},

		{
			"VTE without a program", clipRig(nil, append([]string{"DISPLAY", ":0"}, vte...)...),
			"clipboard", levelWarn, none, fix, nil,
		},
		{
			"VTE without DISPLAY", clipRig([]string{"xclip"}, vte...), "clipboard", levelWarn, none,
			fix, nil,
		},
		{"relative PATH entry", relative, "clipboard", levelWarn, none, fix, nil},
		{"program that is not executable", func(f *fixture) {
			clipRig(nil, append([]string{"DISPLAY", ":0"}, vte...)...)(f)
			f.write("clip/xclip", "#!/bin/sh\n", 0o644)
		}, "clipboard", levelWarn, none, fix, nil},
		{
			"Apple terminal", clipRig(nil, "TERM_PROGRAM", "Apple_Terminal"), "clipboard",
			levelWarn,
			none, fix, nil,
		},
		{
			"GNU screen", clipRig(nil, "TERM", "screen-256color"), "clipboard", levelWarn, none,
			fix,
			nil,
		},
		{"tmux that passes the sequences on", tmux("on"), "clipboard", levelOK, osc, "", nil},
		{
			"tmux that keeps the sequences", tmux("external"), "clipboard", levelWarn, none, fix,
			nil,
		},
		{"tmux that does not answer", tmux(""), "clipboard", levelWarn, none, fix, nil},
		{
			"a program inside tmux", tmux("off", "DISPLAY", ":0"), "clipboard", levelWarn, none,
			fix,
			nil,
		},

		{"mouse off by the setting and no program", func(f *fixture) {
			clipRig(nil, vte...)(f)
			mouseOff(f)
		}, "clipboard", levelOK, unused, "", nil},
		{"mouse off by the setting and a program", func(f *fixture) {
			clipRig([]string{"xsel"}, "DISPLAY", ":0")(f)
			mouseOff(f)
		}, "clipboard", levelOK, tool, "", []string{"xsel"}},
		{"mouse off by the setting and OSC 52", mouseOff, "clipboard", levelOK, osc, "", nil},
		{"dumb terminal", clipRig(nil, "TERM", "dumb"), "clipboard", levelOK, unused, "", nil},
		{"no TERM", clipRig(nil, "TERM", ""), "clipboard", levelOK, unused, "", nil},
		{"linux console", clipRig(nil, "TERM", "linux"), "clipboard", levelOK, unused, "", nil},
		{"no terminal and VTE", func(f *fixture) {
			clipRig(nil, vte...)(f)
			f.env.Interactive = false
		}, "clipboard", levelOK, unused, "", nil},
		{
			"no terminal and OSC 52", func(f *fixture) { f.env.Interactive = false }, "clipboard",
			levelOK, unused, "", nil,
		},
		{"no terminal and a program", func(f *fixture) {
			clipRig([]string{"xclip"}, "DISPLAY", ":0")(f)
			f.env.Interactive = false
		}, "clipboard", levelOK, tool, "", []string{"xclip"}},
		{"no terminal in tmux does not query tmux", func(f *fixture) {
			tmux("on")(f)
			f.env.Interactive = false
			f.env.TmuxClip = func() string {
				f.t.Error("tmux was queried without a terminal")
				return "on"
			}
		}, "clipboard", levelOK, unused, "", nil},
		{"mouse off in tmux does not query tmux", func(f *fixture) {
			tmux("on")(f)
			mouseOff(f)
			f.env.TmuxClip = func() string {
				f.t.Error("tmux was queried with the mouse off")
				return "on"
			}
		}, "clipboard", levelOK, unused, "", nil},
		{
			"dumb terminal and a program", clipRig([]string{"xclip"}, "TERM", "dumb", "DISPLAY",
				":0"),
			"clipboard", levelOK, tool, "",
			[]string{"xclip"},
		},
	}
}

// noPath leaves PATH unset in the fixture.
//
// Parameters:
//   - f: fixture to change
func noPath(f *fixture) {
	delete(f.vars, "PATH")
}

// mustGetwd returns the working directory of the test.
//
// Parameters:
//   - f: fixture whose test fails when the directory is unknown
func mustGetwd(f *fixture) string {
	f.t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		f.t.Fatal(err)
	}

	return dir
}

// clipLines cuts the lines of the clipboard note out of a doctor report.
//
// Parameters:
//   - report: text output of the verb
func clipLines(report string) string {
	return noteLines(report, "clipboard")
}

// noteLines cuts the lines of one note out of a doctor report: the line of the note and the lines
// indented under it.
//
// Parameters:
//   - report: text output of the verb
//   - id: identifier of the note, the second column of its line
func noteLines(report, id string) string {
	var out []string

	for _, l := range strings.Split(report, "\n") {
		switch {
		case len(strings.Fields(l)) > 1 && strings.Fields(l)[1] == id:
			out = append(out, l)
		case len(out) > 0 && strings.HasPrefix(l, " "):
			out = append(out, l)
		case len(out) > 0:
			return strings.Join(out, "\n") + "\n"
		}
	}

	return strings.Join(out, "\n") + "\n"
}

// TestDoctorClipboardGolden checks the report of a terminal that cannot copy, the warning and its
// hint, in both languages against reference files.
//
// Parameters:
//   - t: test to report to
func TestDoctorClipboardGolden(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		f := newFixture(t)
		clipRig(nil, "DISPLAY", ":0", "VTE_VERSION", "7600")(f)

		r := newRig(t, f)
		r.f.vars["TOTE_LANG"] = lang
		r.ttyOut = true

		if code := r.run("doctor"); code != 0 {
			t.Fatalf("%s: code = %d, want 0; stderr %q", lang, code, r.errb.String())
		}

		golden(t, "doctor_clip_"+lang+".txt", clipLines(r.out.String()))
	}
}
