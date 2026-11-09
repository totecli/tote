package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/update"
)

// shellFiles lists the startup files searched for the line that loads tote.
var shellFiles = []string{".bashrc", ".bash_profile", ".profile", ".zshrc", ".zprofile"}

// uninstallFlags holds the options of "tote uninstall".
type uninstallFlags struct {
	yes   bool
	shell bool
	purge bool
}

// shellHit is a startup file that holds a line that loads tote.
//
// path is the file, lines the 1-based numbers of the lines that --shell deletes and left the
// numbers of the lines that run tote init but do more, which are never edited.
type shellHit struct {
	path  string
	lines []int
	left  []int
}

// uninstallPlan is everything "tote uninstall" would touch.
//
// binary is the program, links the symbolic links named tote in the PATH that lead to it,
// receipt the install receipt to remove or empty when it describes another program, shell the
// startup files with the loading line, and data the directories whose tote files are deleted by
// --purge.
type uninstallPlan struct {
	binary  string
	links   []string
	receipt string
	shell   []shellHit
	data    []update.DataDir
}

// dataDirs lists the directories where tote keeps files, and the names it created in each.
//
// Parameters:
//   - p: resolved tote directories
//
// Returns the config, data, state and cache directories from config.Paths.Owned. Only these names
// are ever purged; any other file found there is the user's and keeps its directory in place.
func dataDirs(p config.Paths) []update.DataDir {
	owned := p.Owned()
	out := make([]update.DataDir, len(owned))

	for i, d := range owned {
		out[i] = update.DataDir{Dir: d.Dir, Entries: d.Entries}
	}

	return out
}

// buildPlan collects what an uninstall would remove.
//
// Parameters:
//   - ic: description of the install
//
// Returns the plan, or an error when a startup file cannot be read.
func (a *app) buildPlan(ic installCtx) (uninstallPlan, error) {
	plan := uninstallPlan{binary: ic.exe, links: a.pathLinks(ic.exe)}

	if ic.paths.State != "" && !ic.foreign {
		if _, err := os.Lstat(ic.paths.ReceiptFile()); err == nil {
			plan.receipt = ic.paths.ReceiptFile()
		}
	}

	if ic.paths.State != "" {
		plan.data = dataDirs(ic.paths)
	}

	for _, name := range shellFiles {
		path := filepath.Join(a.Home, name)

		lines, err := update.ShellLines(path)
		if err != nil {
			return plan, err
		}

		left, err := update.ShellMentions(path)
		if err != nil {
			return plan, err
		}

		if len(lines) > 0 || len(left) > 0 {
			plan.shell = append(plan.shell, shellHit{path: path, lines: lines, left: left})
		}
	}

	return plan, nil
}

// pathLinks finds the symbolic links named tote, in the directories of the PATH, that lead to
// the program. Removing the program alone would leave them dangling on the PATH.
//
// Parameters:
//   - exe: path of the program, links resolved
func (a *app) pathLinks(exe string) []string {
	var out []string

	seen := map[string]bool{}

	for _, dir := range filepath.SplitList(a.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}

		p := filepath.Join(dir, "tote")

		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink == 0 || seen[p] {
			continue
		}

		if resolved, err := filepath.EvalSymlinks(p); err == nil && resolved == exe {
			seen[p] = true
			out = append(out, p)
		}
	}

	return out
}

// existingData lists the entries of the data directories that a purge removes and the ones it
// leaves, as full paths.
//
// Parameters:
//   - dirs: directories and entries from dataDirs
//   - skip: path already announced elsewhere, left out of the lists
func existingData(dirs []update.DataDir, skip string) (remove, leave []string) {
	for _, d := range dirs {
		r, l := d.Existing()

		for _, p := range r {
			if p != skip {
				remove = append(remove, p)
			}
		}

		leave = append(leave, l...)
	}

	return remove, leave
}

// checkPurge refuses, before anything is removed, a directory that the removal would refuse
// later and leave the uninstall half done.
//
// Parameters:
//   - plan: what the uninstall would touch
//   - f: options of the command
//   - ic: description of the install
//
// Returns nil, or the error of the first refused directory.
func (a *app) checkPurge(plan uninstallPlan, f uninstallFlags, ic installCtx) error {
	for _, d := range plan.data {
		if !f.purge && (plan.receipt == "" || d.Dir != ic.paths.State) {
			continue
		}

		if err := update.CheckDir(d.Dir, a.Home); err != nil {
			return err
		}
	}

	return nil
}

// runUninstall implements "tote uninstall [--yes] [--shell] [--purge] [--dry-run]".
//
// Parameters:
//   - args: arguments after the verb
//
// Returns the exit code of the verb.
func (a *app) runUninstall(args []string) int {
	o, err := parseOptions(args,
		[]opt{{long: "yes"}, {long: "shell"}, {long: "purge"}, {long: "dry-run"}})
	if err == nil {
		err = wantArgs(o.pos, 0, 0)
	}

	if err != nil {
		return a.usageErr(err, "uninstall")
	}

	if o.help {
		return a.verbHelp("uninstall")
	}

	f := uninstallFlags{yes: o.set["yes"], shell: o.set["shell"], purge: o.set["purge"]}

	ic, err := a.installContext()
	if err != nil {
		return a.failMsg(exitFail, "cli_update_exe", sanitize(err.Error()))
	}

	if ic.install.Managed {
		return a.failMsg(exitFail, "cli_uninstall_managed", ic.install.Method,
			ic.install.RemoveCommand)
	}

	plan, err := a.buildPlan(ic)
	if err != nil {
		return a.fail(err)
	}

	if err := a.checkPurge(plan, f, ic); err != nil {
		return a.fail(err)
	}

	for _, p := range append([]string{plan.binary}, plan.links...) {
		if err := update.CanRemove(p); err != nil {
			var nw *update.NotWritableError

			dir := filepath.Dir(p)
			if errors.As(err, &nw) {
				dir = nw.Dir
			}

			return a.failMsg(exitFail, "cli_uninstall_not_writable",
				sanitize(p), sanitize(dir), sanitize(shell.Quote(p)))
		}
	}

	a.printPlan(plan, f, ic)

	if o.set["dry-run"] {
		return exitOK
	}

	if !f.yes {
		if ok, rc := a.askYes(a.t("cli_uninstall_confirm")); !ok {
			return rc
		}
	}

	if f.purge && !f.yes {
		if ok, rc := a.askYes(a.t("cli_uninstall_confirm_purge")); !ok {
			return rc
		}
	}

	return a.executePlan(plan, f, ic)
}

// printPlan lists what the uninstall removes and what it leaves.
//
// Parameters:
//   - plan: what the uninstall would touch
//   - f: options of the command
//   - ic: description of the install
func (a *app) printPlan(plan uninstallPlan, f uninstallFlags, ic installCtx) {
	w := a.Stdout

	fmt.Fprintln(w, a.t("cli_uninstall_header"))
	fmt.Fprintln(w, "  "+a.t("cli_uninstall_item_binary", sanitize(plan.binary)))

	for _, l := range plan.links {
		fmt.Fprintln(w, "  "+a.t("cli_uninstall_item_link", sanitize(l)))
	}

	if plan.receipt != "" {
		fmt.Fprintln(w, "  "+a.t("cli_uninstall_item_receipt", sanitize(plan.receipt)))
	} else if ic.foreign && !f.purge {
		fmt.Fprintln(w, "  "+a.t("cli_uninstall_receipt_other", sanitize(ic.paths.ReceiptFile())))
	}

	for _, hit := range plan.shell {
		for _, n := range hit.lines {
			if f.shell {
				fmt.Fprintln(w, "  "+a.t("cli_uninstall_item_shell", sanitize(hit.path), n))
			} else {
				fmt.Fprintln(w, "  "+a.t("cli_uninstall_kept_shell", sanitize(hit.path), n))
			}
		}

		a.printLeft(hit.path, hit.left)
	}

	data, leave := existingData(plan.data, plan.receipt)
	if f.purge && len(data) > 0 {
		fmt.Fprintln(w, a.t("cli_uninstall_header_purge"))

		for _, p := range data {
			fmt.Fprintln(w, "  "+sanitize(p))
		}
	}

	if f.purge {
		for _, p := range leave {
			fmt.Fprintln(w, a.t("cli_uninstall_left_dir", sanitize(p)))
		}
	}

	if !f.purge {
		fmt.Fprintln(w, a.t("cli_uninstall_kept_data"))
	}
}

// printLeft lists the lines of a startup file that load tote and that the uninstall does not
// delete, which the user has to remove by hand.
//
// Parameters:
//   - path: startup file
//   - lines: 1-based numbers of the lines
func (a *app) printLeft(path string, lines []int) {
	for _, n := range lines {
		fmt.Fprintln(a.Stdout, "  "+a.t("cli_uninstall_left_shell", sanitize(path), n))
	}
}

// executePlan removes what the plan lists. The program and its links go last, after the
// receipt, the shell lines and the data, so that an uninstall that fails halfway leaves a tote
// that can be run again to finish it.
//
// Parameters:
//   - plan: what to remove
//   - f: options of the command
//   - ic: description of the install
//
// Returns exitOK, or exitFail when a step failed. The steps that can still be done are all tried
// and reported; the program is kept when one of the data steps failed.
func (a *app) executePlan(plan uninstallPlan, f uninstallFlags, ic installCtx) int {
	rc := exitOK

	if plan.receipt != "" {
		if r := a.purge(update.DataDir{
			Dir: ic.paths.State, Entries: []string{filepath.Base(plan.receipt)},
		}); r != exitOK {
			rc = r
		}
	}

	if f.shell {
		for _, hit := range plan.shell {
			if len(hit.lines) == 0 {
				continue
			}

			if _, err := update.RemoveShellLines(hit.path); err != nil {
				rc = a.fail(err)
				continue
			}

			a.removed(hit.path)
		}
	}

	if f.purge {
		for _, d := range plan.data {
			if r := a.purge(d); r != exitOK {
				rc = r
			}
		}
	}

	if rc != exitOK {
		a.say("cli_uninstall_incomplete", sanitize(plan.binary))

		return rc
	}

	for _, p := range append([]string{plan.binary}, plan.links...) {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return a.fail(err)
		}

		a.removed(p)
	}

	for _, hit := range plan.shell {
		// Deleting the loading lines moved the ones below them: read the numbers again.
		lines, err := update.ShellMentions(hit.path)
		if err != nil {
			lines = hit.left
		}

		a.printLeft(hit.path, lines)
	}

	fmt.Fprintln(a.Stdout, a.t("cli_uninstall_done"))

	return exitOK
}

// removed reports one removed item.
//
// Parameters:
//   - path: what was removed
func (a *app) removed(path string) {
	fmt.Fprintln(a.Stdout, a.t("cli_uninstall_removed", sanitize(path)))
}

// purge removes the tote entries of one directory and reports them.
//
// What was removed is reported also when an entry could not be, and every path that failed is
// printed on its own line with its reason, so that nothing is hidden behind the first error.
//
// Parameters:
//   - d: directory and entries
//
// Returns exitOK, or exitFail when a path is refused or an entry cannot be removed.
func (a *app) purge(d update.DataDir) int {
	out, err := update.PurgeDir(d, a.Home)

	for _, p := range out.Removed {
		a.removed(p)
	}

	if err != nil {
		for _, e := range flatten(err) {
			a.fail(e)
		}

		return exitFail
	}

	if out.Kept {
		fmt.Fprintln(a.Stdout, a.t("cli_uninstall_kept_dir", sanitize(d.Dir)))
	}

	return exitOK
}

// flatten splits an error made of several errors, joined at any depth, into the errors of the
// individual failures.
//
// Parameters:
//   - err: error to split
//
// Returns err alone when it joins nothing.
func flatten(err error) []error {
	j, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []error{err}
	}

	var out []error

	for _, e := range j.Unwrap() {
		out = append(out, flatten(e)...)
	}

	return out
}
