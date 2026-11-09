package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/totecli/tote/internal/ui"
	"github.com/totecli/tote/internal/update"
)

// doctorCheck is the JSON form of one check of "tote doctor --json".
//
// Status is ok, warn or fail, Message and Fix are written in the language of the user and Fix
// and Details are left out when empty.
type doctorCheck struct {
	ID      string   `json:"id"`
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Fix     string   `json:"fix,omitempty"`
	Details []string `json:"details,omitempty"`
}

// doctorSummary counts the checks of a report by outcome.
type doctorSummary struct {
	OK   int `json:"ok"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

// doctorReport is the JSON form of "tote doctor --json".
//
// OK is false when at least one check failed, which is also when the exit code is 1.
type doctorReport struct {
	Schema  int           `json:"schema"`
	Version string        `json:"version"`
	OK      bool          `json:"ok"`
	Summary doctorSummary `json:"summary"`
	Checks  []doctorCheck `json:"checks"`
}

// now returns the current time.
func (a *app) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}

	return time.Now()
}

// lookPath finds a program in PATH.
//
// Parameters:
//   - name: program name
func (a *app) lookPath(name string) (string, error) {
	if a.LookPath != nil {
		return a.LookPath(name)
	}

	return exec.LookPath(name)
}

// terminalSize asks for the size of the terminal.
//
// Returns the width, the height and true, or false when there is no terminal.
func (a *app) terminalSize() (int, int, bool) {
	if a.TermSize != nil {
		return a.TermSize()
	}

	return termSize()
}

// sttySane restores the line discipline of a terminal input.
//
// Parameters:
//   - in: standard input, which must be a terminal file
//
// Returns the error of stty, or nil when the input is not a file.
func sttySane(in io.Reader) error {
	f, ok := in.(*os.File)
	if !ok {
		return nil
	}

	return ui.SaneTerminal(f)
}

// diagEnv reads the machine into the environment of the checks.
func (a *app) diagEnv() diagEnv {
	environ := os.Environ
	if a.Environ != nil {
		environ = a.Environ
	}

	euid := os.Geteuid
	if a.Geteuid != nil {
		euid = a.Geteuid
	}

	exe, err := a.executable()
	w, h, sized := a.terminalSize()

	goos, goarch := runtime.GOOS, runtime.GOARCH
	if a.Updater.OS != "" {
		goos = a.Updater.OS
	}

	if a.Updater.Arch != "" {
		goarch = a.Updater.Arch
	}

	env := diagEnv{
		Getenv:   a.Getenv,
		Environ:  environ(),
		Home:     a.Home,
		Version:  a.Version,
		GOOS:     goos,
		GOARCH:   goarch,
		Exe:      exe,
		ExeErr:   err,
		Now:      a.now(),
		UID:      euid(),
		Keys:     a.releaseKeys,
		LookPath: a.lookPath,
		TmuxClip: a.TmuxClipboard,
		Marker:   update.MarkerFile,
		Interactive: a.IsTerminal(a.Stdout) || a.IsTerminal(a.Stderr) ||
			a.IsInputTerminal(a.Stdin),
		Width:  w,
		Height: h,
		SizeOK: sized,
	}

	env.Term = ui.DescribeTerminal(env.Environ, a.ascii, a.TmuxFeatures)

	return env
}

// runDoctor implements "tote doctor [--json] [--reset]".
//
// Parameters:
//   - args: arguments after the verb
//
// Returns the exit code of the verb: 0 when no check failed, 1 when one did or when the
// terminal could not be reset, 2 for a misuse.
func (a *app) runDoctor(args []string) int {
	o, err := parseOptions(args, []opt{{long: "json"}, {long: "reset"}})
	if err == nil {
		err = wantArgs(o.pos, 0, 0)
	}

	if err == nil && o.set["json"] && o.set["reset"] {
		err = &optError{Key: "cli_doctor_reset_alone"}
	}

	if err != nil {
		return a.usageErr(err, "doctor")
	}

	if o.help {
		return a.verbHelp("doctor")
	}

	if o.set["reset"] {
		return a.resetTerminal()
	}

	found := diagnose(a.diagEnv())
	counts := tally(found)

	if o.set["json"] {
		return a.printDoctorJSON(found, counts)
	}

	a.printDoctor(found, counts)

	if counts.Fail > 0 {
		return exitFail
	}

	return exitOK
}

// tally counts the findings by outcome.
//
// Parameters:
//   - found: findings of a run
func tally(found []finding) doctorSummary {
	var s doctorSummary

	for _, f := range found {
		switch f.Level {
		case levelWarn:
			s.Warn++
		case levelFail:
			s.Fail++
		default:
			s.OK++
		}
	}

	return s
}

// render translates a message and its values.
//
// Parameters:
//   - m: message with its values; a value that is a message is translated too and a string is
//     sanitized, since it may come from a file of the user
func (a *app) render(m msg) string {
	args := make([]any, len(m.Args))

	for i, v := range m.Args {
		switch x := v.(type) {
		case msg:
			args[i] = a.render(x)
		case string:
			args[i] = sanitize(x)
		default:
			args[i] = v
		}
	}

	return a.t(m.Key, args...)
}

// label returns the word that heads a line of the report for an outcome.
//
// Parameters:
//   - l: outcome
func (a *app) label(l level) string {
	switch l {
	case levelWarn:
		return a.t("cli_doctor_label_warn")
	case levelFail:
		return a.t("cli_doctor_label_fail")
	}

	return a.t("cli_doctor_label_ok")
}

// printDoctorJSON writes the report as JSON.
//
// Parameters:
//   - found: findings of a run
//   - counts: their count by outcome
//
// Returns the exit code of the verb.
func (a *app) printDoctorJSON(found []finding, counts doctorSummary) int {
	rep := doctorReport{
		Schema: listSchema, Version: a.Version, OK: counts.Fail == 0, Summary: counts,
		Checks: make([]doctorCheck, 0, len(found)),
	}

	for _, f := range found {
		c := doctorCheck{ID: f.ID, Status: f.Level.String(), Message: a.render(f.Msg)}

		if f.Fix.Key != "" {
			c.Fix = a.render(f.Fix)
		}

		for _, d := range f.Details {
			c.Details = append(c.Details, a.render(d))
		}

		rep.Checks = append(rep.Checks, c)
	}

	if err := writeJSON(a.Stdout, rep); err != nil {
		return a.fail(err)
	}

	if counts.Fail > 0 {
		return exitFail
	}

	return exitOK
}

// pad fills a string with spaces on the right up to a width counted in characters.
//
// Parameters:
//   - s: text to pad
//   - width: wanted number of characters
func pad(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}

	return s
}

// printDoctor writes the checklist as text, one line per check with the hint and the details
// under it, then the count.
//
// Parameters:
//   - found: findings of a run
//   - counts: their count by outcome
func (a *app) printDoctor(found []finding, counts doctorSummary) {
	w := a.Stdout
	labels := 0
	ids := 0

	for _, f := range found {
		labels = max(labels, utf8.RuneCountInString(a.label(f.Level)))
		ids = max(ids, len(f.ID))
	}

	indent := strings.Repeat(" ", labels+ids+4)

	fmt.Fprintln(w, a.t("cli_doctor_header"))
	fmt.Fprintln(w)

	for _, f := range found {
		fmt.Fprintf(w, "%s  %s  %s\n", pad(a.label(f.Level), labels), pad(f.ID, ids),
			a.render(f.Msg))

		for _, d := range f.Details {
			fmt.Fprintln(w, indent+"- "+a.render(d))
		}

		if f.Fix.Key != "" {
			fmt.Fprintln(w, indent+a.t("cli_doctor_fix", a.render(f.Fix)))
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, a.t("cli_doctor_summary", counts.OK, counts.Warn, counts.Fail))
}

// resetTerminal puts a broken terminal back into a usable state and does nothing else.
//
// It writes the sequences that switch off the mouse, the focus reports, the bracketed paste and
// the keyboard protocols, show the cursor, leave the alternate screen and clear the screen, then
// runs "stty sane" when the input is a terminal. It reads no file of tote, so it works when the
// configuration is broken.
//
// Returns exitOK, or exitFail when neither the output nor the error output is a terminal, since
// the sequences would then end up in a file, or when they cannot be written.
func (a *app) resetTerminal() int {
	var out io.Writer

	switch {
	case a.IsTerminal(a.Stdout):
		out = a.Stdout
	case a.IsTerminal(a.Stderr):
		out = a.Stderr
	default:
		return a.failMsg(exitFail, "cli_doctor_reset_no_terminal")
	}

	if _, err := io.WriteString(out, ui.ResetSequence); err != nil {
		return a.fail(err)
	}

	if a.IsInputTerminal(a.Stdin) {
		stty := a.Stty
		if stty == nil {
			stty = sttySane
		}

		_ = stty(a.Stdin)
	}

	a.say("cli_doctor_reset_done")

	return exitOK
}
