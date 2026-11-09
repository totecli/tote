package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// skipLine matches the comment line that shell.Definitions writes for a record it refuses to
// turn into code because of its name, so that the report says what the shell really does.
var skipLine = regexp.MustCompile(
	`^# tote: (.+) '(.*)' skipped \((reserved name|duplicate name)\)$`)

// files checks config.toml and the four data files.
func (d *diagnosis) files() {
	d.add(d.configFile())

	recs, f := inspect(d, "file.connections", d.paths.ConnectionsFile(), store.OpenConnections)
	d.conns = recs

	if f.Level == levelOK && f.Msg.Key == "cli_doctor_file_ok" {
		n := 0

		for _, c := range recs {
			if c.NeedsReview {
				n++
			}
		}

		if n > 0 {
			name := filepath.Base(d.paths.ConnectionsFile())
			f = note(f.ID, levelWarn, "cli_doctor_file_review", name, n).
				fix("cli_doctor_fix_review")
		}
	}

	d.add(f)
	d.add(d.via())

	d.aliases, f = inspect(d, "file.aliases", d.paths.AliasesFile(), store.OpenAliases)
	d.add(f)

	d.repos, f = inspect(d, "file.repos", d.paths.ReposFile(), store.OpenRepos)
	d.add(d.dangling(f, d.deadRepos))

	d.funcs, f = inspect(d, "file.funcs", d.paths.FuncsFile(), store.OpenFuncs)
	d.add(d.dangling(f, d.deadScripts))
}

// dangling adds to the result of a file check the records that point at nothing.
//
// Parameters:
//   - f: result of the check of the file
//   - find: looks for the dead records and returns the warning about them, false when there is
//     none
//
// Returns f when nothing is dead, else f and the warning combined: the file stays valid, but the
// report says which records will fail when they are used.
func (d *diagnosis) dangling(f finding, find func() (finding, bool)) finding {
	if f.Level == levelFail {
		return f
	}

	gone, found := find()
	if !found {
		return f
	}

	return combine(f.ID, f, gone)
}

// deadRepos finds the valid repositories whose path is not an existing directory.
//
// Returns a warning that lists up to maxListed repositories with their path, then the number of
// the others, and true; false when every path is there. A path that cannot be examined for
// another reason than its absence (no right to enter a parent directory) is not reported. A
// stored path that is not absolute is reported, because "repos open" refuses it.
func (d *diagnosis) deadRepos() (finding, bool) {
	var gone []store.Repo

	for _, r := range d.repos {
		if r.Validate() != nil {
			continue
		}

		path := shell.ExpandHome(r.Path, d.env.Home)
		if !filepath.IsAbs(path) {
			gone = append(gone, r)

			continue
		}

		info, err := os.Stat(path)
		if err == nil && !info.IsDir() || err != nil && missing(err) {
			gone = append(gone, r)
		}
	}

	if len(gone) == 0 {
		return finding{}, false
	}

	f := note("file.repos", levelWarn, "cli_doctor_repos_gone", len(gone)).
		fix("cli_doctor_fix_repos_gone")

	for i, r := range gone {
		if i == maxListed {
			f = f.detail("cli_doctor_gone_more", len(gone)-maxListed)

			break
		}

		f = f.detail("cli_doctor_gone_item", r.Name, d.scrub(r.Path))
	}

	return f, true
}

// deadScripts finds the valid functions of kind source whose script is not an existing file.
//
// A function of kind exec is not looked at, because the program it names lives in the bin
// directory or in PATH and "funcs add" accepts it before it is installed.
//
// Returns a warning that lists up to maxListed functions with their script, then the number of
// the others, and true; false when every script is there.
func (d *diagnosis) deadScripts() (finding, bool) {
	var gone []store.Func

	for _, fn := range d.funcs {
		if fn.Kind != store.KindSource || fn.Validate() != nil {
			continue
		}

		info, err := os.Stat(shell.ExpandHome(fn.Script, d.env.Home))
		if err == nil && !info.Mode().IsRegular() || err != nil && missing(err) {
			gone = append(gone, fn)
		}
	}

	if len(gone) == 0 {
		return finding{}, false
	}

	f := note("file.funcs", levelWarn, "cli_doctor_funcs_gone", len(gone)).
		fix("cli_doctor_fix_funcs_gone")

	for i, fn := range gone {
		if i == maxListed {
			f = f.detail("cli_doctor_gone_more", len(gone)-maxListed)

			break
		}

		f = f.detail("cli_doctor_gone_item", fn.Name, d.scrub(fn.Script))
	}

	return f, true
}

// inspect checks one data file: that it parses, that its records are valid and that its mode
// lets tote use it.
//
// Parameters:
//   - d: running diagnosis
//   - id: stable name of the check
//   - path: the file
//   - open: opens the collection of the kind
//
// Returns the records in file order, none when the file cannot be read, and the finding. A file
// that does not parse is a failure, because tote then keeps it read-only and the shell ignores
// its records; a record that is invalid, or a mode that lets others change the file or that
// forbids tote to save, is a warning.
func inspect[T store.Record](
	d *diagnosis, id, path string, open func(string) (*store.Collection[T], error),
) ([]T, finding) {
	name := filepath.Base(path)

	li, err := os.Lstat(path)
	if missing(err) {
		return nil, note(id, levelOK, "cli_doctor_file_missing", name)
	}

	if err != nil {
		return nil, note(id, levelFail, "cli_doctor_file_unreadable", name, d.scrub(err.Error()))
	}

	info := li

	if li.Mode()&os.ModeSymlink != 0 {
		if info, err = os.Stat(path); err != nil {
			return nil, note(id, levelFail, "cli_doctor_file_unreadable", name,
				d.scrub(err.Error())).fix("cli_doctor_fix_link_file")
		}
	}

	c, err := open(path)
	if err == nil {
		err = c.Err()
	}

	if err != nil {
		return nil, parseFailure(d, id, name, err)
	}

	recs := c.List()
	parts := []finding{note(id, levelOK, "cli_doctor_file_ok", name, len(recs))}

	if invalid := c.Invalid(); len(invalid) > 0 {
		f := note(id, levelWarn, "cli_doctor_file_invalid", name, len(invalid)).
			fix("cli_doctor_fix_invalid")

		for _, r := range recs {
			if e := invalid[r.Key()]; e != nil {
				f = f.detail("cli_doctor_file_invalid_item", c.Line(r.Key()), r.Key(), e.Error())
			}
		}

		parts = append(parts, f)
	}

	if f, bad := fileMode(d, id, path, name, info); bad {
		parts = append(parts, f)
	}

	return recs, combine(id, parts...)
}

// parseFailure turns the error of a collection into a failure with the position of the fault.
//
// Parameters:
//   - d: running diagnosis
//   - id: stable name of the check
//   - name: file name
//   - err: result of Open or of Err
func parseFailure(d *diagnosis, id, name string, err error) finding {
	var perr *store.ParseError

	if !errors.As(err, &perr) {
		return note(id, levelFail, "cli_doctor_file_unreadable", name, d.scrub(err.Error()))
	}

	f := note(id, levelFail, "cli_doctor_file_syntax", name, perr.Line, perr.Column, perr.Msg)
	if perr.Line == 0 {
		f = note(id, levelFail, "cli_doctor_file_error", name, perr.Msg)
	}

	return f.detail("cli_doctor_file_readonly").fix("cli_doctor_fix_syntax")
}

// fileMode checks that other users cannot change a file and that its owner can.
//
// Parameters:
//   - d: running diagnosis
//   - id: stable name of the check
//   - path: the file, shown in the hint
//   - name: file name
//   - info: result of Stat on it
//
// Returns the warning and true, or false when the mode is fine.
func fileMode(d *diagnosis, id, path, name string, info os.FileInfo) (finding, bool) {
	perm := info.Mode().Perm()

	switch {
	case perm&0o022 != 0:
		return note(id, levelWarn, "cli_doctor_file_shared", name, modeText(perm)).
			fix("cli_doctor_fix_chmod_file", d.word(path)), true
	case perm&0o200 == 0:
		return note(id, levelWarn, "cli_doctor_file_locked", name, modeText(perm)).
			fix("cli_doctor_fix_unlock", d.word(path)), true
	}

	return finding{}, false
}

// configFile checks config.toml the way the settings loader reads it.
//
// Returns a failure when the file is ignored, with each problem and its line, a warning when
// others can change it.
func (d *diagnosis) configFile() finding {
	path := d.paths.ConfigFile()
	name := filepath.Base(path)

	li, err := os.Lstat(path)
	if missing(err) {
		return note("file.config", levelOK, "cli_doctor_config_missing", name)
	}

	if err != nil {
		return note("file.config", levelFail, "cli_doctor_file_unreadable", name,
			d.scrub(err.Error()))
	}

	s, warns, err := config.LoadSettingsWarn(path)
	if err != nil {
		return d.configFailure(name, err)
	}

	d.settings = s
	parts := []finding{note("file.config", levelOK, "cli_doctor_config_ok", name, s.Schema)}

	if len(warns) > 0 {
		parts[0] = configWarnings(name, warns)
	}

	info := li
	if li.Mode()&os.ModeSymlink != 0 {
		info, _ = os.Stat(path)
	}

	if info != nil && info.Mode().Perm()&0o022 != 0 {
		parts = append(parts, note("file.config", levelWarn, "cli_doctor_file_shared", name,
			modeText(info.Mode())).fix("cli_doctor_fix_chmod_file", d.word(path)))
	}

	return combine("file.config", parts...)
}

// configWarnings describes the settings of a config.toml that are read but ignored.
//
// Parameters:
//   - name: file name
//   - warns: warnings of config.LoadSettingsWarn, not empty
//
// Returns a warning with one detail per ignored setting, with its line when it is known.
func configWarnings(name string, warns []config.Warning) finding {
	f := note("file.config", levelWarn, "cli_doctor_config_ignored", name).
		fix("cli_doctor_fix_config")

	for _, w := range warns {
		m := msg{Key: "cli_doctor_config_unknown_key", Args: []any{w.Key}}
		if w.Kind == config.WarnInvalidValue {
			m = msg{Key: "cli_doctor_config_raw", Args: []any{w.Msg}}
		}

		if w.Line > 0 {
			f = f.detail("cli_doctor_config_at_line", w.Line, m)
		} else {
			f = f.detail("cli_doctor_config_problem", m)
		}
	}

	return f
}

// configFailure describes a config.toml that the loader refuses.
//
// Parameters:
//   - name: file name
//   - err: error of config.LoadSettingsWarn
func (d *diagnosis) configFailure(name string, err error) finding {
	var perr *config.ParseError

	if errors.As(err, &perr) {
		return note("file.config", levelFail, "cli_doctor_config_syntax", name, perr.Line,
			perr.Column, perr.Msg).fix("cli_doctor_fix_config")
	}

	return note("file.config", levelFail, "cli_doctor_config_invalid", name).
		fix("cli_doctor_fix_config").
		detail("cli_doctor_config_problem", msg{
			Key:  "cli_doctor_config_raw",
			Args: []any{d.scrub(err.Error())},
		})
}

// names checks that the names of the aliases, repositories, functions and connections do not
// clash.
//
// The shell side is not worked out again: the definitions are generated as "tote init" does and
// the lines it writes for the records it skips are read, so that the answer is what the shell
// gets. Two connections that share a name or an alias are found with store.SharedName.
//
// Returns a warning that lists each skipped record and each shared word.
func (d *diagnosis) names() finding {
	defs := shell.Defs{
		Aliases: d.aliases, Repos: d.repos, Funcs: d.funcs, BinDir: d.paths.BinDir(),
		ExtraNames: extraNames(d.settings),
	}

	var parts []finding

	for _, line := range strings.Split(shell.Definitions(shell.Bash, defs), "\n") {
		m := skipLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		key, fix := "cli_doctor_names_duplicate", "cli_doctor_fix_names"
		if m[3] == "reserved name" {
			key, fix = "cli_doctor_names_reserved", "cli_doctor_fix_rename"
		}

		parts = append(parts, note("names", levelWarn, key, msg{Key: kindKey(m[1])}, m[2]).fix(fix))
	}

	parts = append(parts, d.commandWords()...)

	for i, c := range d.conns {
		if word, owner, found := store.SharedName(d.conns[:i], "", c); found {
			parts = append(parts, note("names", levelWarn, "cli_doctor_names_shared", c.Name,
				word, owner).fix("cli_doctor_fix_names"))
		}
	}

	if len(parts) == 0 {
		count := len(d.conns) + len(d.aliases) + len(d.repos) + len(d.funcs)

		return note("names", levelOK, "cli_doctor_names_ok", count)
	}

	return combine("names", parts...)
}

// commandWords lists the connections, aliases and repositories whose name is one that tote keeps
// for its command line, which "add" refuses but a file edited by hand may hold.
//
// The shell already skips an alias or a repository that starts with a reserved prefix, and the
// check of the generated code reports it, so only the words are looked for there.
//
// Returns one warning per record, in the order connections, aliases, repositories.
func (d *diagnosis) commandWords() []finding {
	var out []finding

	warn := func(kind, name string) {
		out = append(out, note("names", levelWarn, "cli_doctor_names_command",
			msg{Key: kind}, name).fix("cli_doctor_fix_rename"))
	}

	for _, c := range d.conns {
		if store.ReservedRecordName(c.Name) {
			warn("kind_connection", c.Name)
		}

		for _, w := range c.Aliases {
			if store.ReservedRecordName(w) {
				out = append(out, note("names", levelWarn, "cli_doctor_names_command_alias",
					w, c.Name).fix("cli_doctor_fix_rename"))
			}
		}
	}

	for _, a := range d.aliases {
		if store.ReservedWord(a.Name) {
			warn("kind_alias", a.Name)
		}
	}

	for _, r := range d.repos {
		if store.ReservedWord(r.Name) {
			warn("kind_repo", r.Name)
		}
	}

	return out
}

// via checks that the program of every connection with a via can be found in the PATH of the
// environment, without running anything: a via is started without a shell, so a name that is a
// shell alias or function is not found and the connection fails when it is opened.
//
// A connection that is invalid is skipped, because file.connections reports it and tote ignores
// it.
//
// Returns a warning that lists up to maxListed connections with their program, then the number of
// the others, with what to write instead; OK when every program is found or no connection has a
// via other than ssh.
func (d *diagnosis) via() finding {
	pathVar := d.env.Getenv("PATH")
	used := 0

	var lacking []store.Connection

	for _, c := range d.conns {
		prog := shell.ViaProgram(c)
		if prog == "" || c.Validate() != nil {
			continue
		}

		used++

		if _, ok := shell.LookProgram(pathVar, prog); !ok {
			lacking = append(lacking, c)
		}
	}

	switch {
	case used == 0:
		return note("via", levelOK, "cli_doctor_via_none")
	case len(lacking) == 0:
		return note("via", levelOK, "cli_doctor_via_ok")
	}

	f := note("via", levelWarn, "cli_doctor_via_missing", len(lacking)).fix("cli_via_hint")

	for i, c := range lacking {
		if i == maxListed {
			f = f.detail("cli_doctor_via_more", len(lacking)-maxListed)

			break
		}

		f = f.detail("cli_doctor_via_item", c.Name, shell.ViaProgram(c))
	}

	return f
}

// kindKey gives the catalog key of the name of a kind of record, as the init output labels it.
//
// Parameters:
//   - label: "alias", "repo", "func" or "extra name"
func kindKey(label string) string {
	switch label {
	case "alias":
		return "kind_alias"
	case "repo":
		return "kind_repo"
	case "func":
		return "kind_func"
	}

	return "cli_doctor_kind_extra"
}
