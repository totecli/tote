package plugin

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
)

// cacheDirMode is the mode of the cache directory of a plugin when a suggestions run creates it.
const cacheDirMode = 0o700

// Choices builds the run that lists the suggestions of one text field, the one a user asks for
// with a key press on the field. It starts nothing: the host hands the result to the launcher of
// the suggestions, which calls Guard first. Prepare and Run never start a choices_from.
//
// The state of the plugin is computed again from the files, as Prepare does, and the plugin must
// be enabled. Only the fields that come before the field in file order count: call.Values for the
// field itself and for the later ones, which a form that is being filled in still sends, are
// ignored, and a value for a name that is no field of the contribution is refused. An earlier field
// that the argv names must hold a valid value, else the call fails with input_invalid and the id of
// that field. A placeholder that is empty and not optional fails with unavailable, the placeholder
// and "empty", such as {repo.slug} outside a saved repository. ChoicesWaiting tells these two from
// a real failure.
//
// The argv is expanded with the strict rules of a run; allow_dash does not apply to a text field.
// {repo.*} uses call.Repo, the saved repository of an item action, else the current repository;
// reading it may start git, as Prepare may, and nothing else starts. The run happens in the cache
// directory of the plugin, never in a repository, with the environment described on Prepared.
//
// Parameters:
//   - call: the contribution and the values of its form; its Item is not used
//   - fieldID: id of the text field that has a choices_from
//
// Returns the prepared run, with Output OutputChoices, or an *Error: bad_id, not_runnable, no_entry
// (the entry, or a field that is not a text field with a choices_from), input_invalid,
// unavailable, placeholder_syntax, argv0_invalid, exec_self, argv.shell, exec_uncovered,
// argv.ssh_no_dashdash, produced_invalid, dash_refused, cwd_invalid or dependency_missing.
func (r *Registry) Choices(call Call, fieldID string) (Prepared, error) {
	if err := config.ValidPluginID(call.Plugin); err != nil {
		return Prepared{}, wrapError(CodeBadID, err, call.Plugin)
	}

	p, ctx, ok := r.recheck(call.Plugin)
	if !ok {
		return Prepared{}, newError(CodeNotRunnable, call.Plugin, "absent")
	}

	if !p.State.Runnable() || p.Manifest == nil {
		return Prepared{}, notRunnable(p)
	}

	e, list, err := findEntry(p.Manifest, call)
	if err != nil {
		return Prepared{}, err
	}

	at := -1

	for i := range e.Fields {
		if f := &e.Fields[i]; f.ID == fieldID && f.Type == "text" && len(f.ChoicesFrom) > 0 {
			at = i
		}
	}

	if at < 0 {
		return Prepared{}, newError(CodeNoEntry, "field", fieldID)
	}

	pc, err := r.newPrepAt(p, ctx, call, e, list, at)
	if err != nil {
		return Prepared{}, err
	}

	out, err := pc.buildChoices()
	if err != nil {
		return Prepared{}, err
	}

	return out, nil
}

// ChoicesWaiting tells whether a failure of Registry.Choices only means that the form is not ready:
// an earlier field that the argv names is not valid yet, or a placeholder it needs is empty.
//
// Parameters:
//   - err: the error Registry.Choices returned
//
// Returns the name of the field or placeholder to wait for, such as slug or repo.slug, and true;
// false for every other error, which is a failure to report.
func ChoicesWaiting(err error) (string, bool) {
	var e *Error
	if !errors.As(err, &e) || len(e.Args) == 0 {
		return "", false
	}

	switch {
	case e.Code == CodeInputInvalid && (len(e.Args) < 2 || e.Args[1] != RejectUnknown):
		return e.Args[0], true
	case e.Code == CodeUnavailable && len(e.Args) > 1 && e.Args[1] == "empty":
		return e.Args[0], true
	}

	return "", false
}

// buildChoices builds the run of the suggestions of the field pc.suggest.
func (pc *prep) buildChoices() (Prepared, *Error) {
	f := pc.suggest

	if err := pc.earlierFields(); err != nil {
		return Prepared{}, err
	}

	name, perr := pc.programOf(f.ChoicesFrom[0])
	if perr != nil {
		return Prepared{}, perr
	}

	argv, perr := pc.expandArgv(name, f.ChoicesFrom, map[string]bool{})
	if perr != nil {
		return Prepared{}, perr
	}

	dir, perr := pc.cacheDir()
	if perr != nil {
		return Prepared{}, perr
	}

	abs, id, perr := pc.locate(name, dir)
	if perr != nil {
		return Prepared{}, perr
	}

	out := Prepared{
		PluginID: pc.p.ID, Table: pc.e.Table, EntryID: pc.e.ID, FieldID: f.ID, Output: OutputChoices,
		Name: name, Argv: append([]string{abs}, argv[1:]...), Display: shell.PrettyCommand(argv),
		Dir: dir, Timeout: ChoicesTimeout, Mono: pc.call.Mono, tree: pc.p.Record.TreeSHA256,
		surface: pc.p.Surface, program: id,
	}

	if pc.list != nil {
		out.ListID = pc.list.ID
	}

	plan := envPlan{Lang: pc.call.Lang, Captured: true, Mono: pc.call.Mono, Suggest: true}
	out.Env, out.secrets = pc.r.buildEnv(pc.p, pc.ctx.pathEnv, pc.avoid, plan)

	return out, nil
}

// earlierFields validates the values of the fields before the suggestions field, in the order of
// the form, and fills pc.vals with them. A field that the argv does not name never stops the
// call, so that an unrelated invalid field cannot keep a list from showing.
func (pc *prep) earlierFields() *Error {
	known := map[string]bool{}
	for _, f := range pc.e.Fields {
		known[f.ID] = true
	}

	for _, k := range sortedKeys(pc.call.Values) {
		if !known[k] {
			return newError(CodeInputInvalid, k, RejectUnknown)
		}
	}

	named := map[string]bool{}

	for _, raw := range pc.suggest.ChoicesFrom {
		for _, h := range pc.tmpl[raw].Placeholders() {
			if h.Kind == KindField {
				named[h.Name] = true
			}
		}
	}

	for i := 0; i < pc.before; i++ {
		f := &pc.e.Fields[i]

		raw, given := pc.call.Values[f.ID]
		if !given {
			raw = pc.defaultOf(f)
		}

		fv, err := checkField(f, raw)

		switch {
		case err != nil && named[f.ID]:
			return err
		case err != nil:
			fv = FieldValue{Type: f.Type}
		}

		pc.vals.Fields[f.ID] = fv
	}

	return nil
}

// cacheDir gives the working directory of a suggestions run: the cache directory of the plugin,
// created with mode 0700 when it is missing. It must be an absolute directory and not a link, so
// that a plugin cannot point the run at another place by replacing it.
func (pc *prep) cacheDir() (string, *Error) {
	dir, err := pc.r.opts.Paths.PluginCacheDir(pc.p.ID)
	if err != nil || !filepath.IsAbs(dir) {
		return "", newError(CodeCwdInvalid, dir)
	}

	if err := os.MkdirAll(dir, cacheDirMode); err != nil {
		return "", wrapError(CodeCwdInvalid, err, dir)
	}

	if info, serr := os.Lstat(dir); serr != nil || !info.IsDir() {
		return "", newError(CodeCwdInvalid, dir)
	}

	return filepath.Clean(dir), nil
}
