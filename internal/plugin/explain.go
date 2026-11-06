package plugin

import (
	"path/filepath"
	"slices"

	"github.com/totecli/tote/internal/shell"
)

// Contribution is what check shows for one contribution of a plugin: the template it was written
// with and the argv, directory or URL it gives with the values of the request, or the reason it
// cannot be built.
//
// Table is command, item_action, action, list or list.action; List is the id of the list of a
// list action. Run and Open are the templates as written. For a contribution that tote runs, Argv
// holds the elements after substitution with the program name first, as the confirmation shows
// them, Dir the working directory and URL the address an open contribution would show; for a
// contribution that the plugin's own program answers, Argv is that program and its fixed
// arguments. Display is Argv written with shell.PrettyCommand, or the template when Argv could not
// be built. Program is the absolute path found on PATH, empty when the program is not installed.
// Available is false when the contribution cannot be built; Why then holds the reason, and it also
// holds dependency_missing for one whose program is not installed, because its argv is still
// exact. Confirm is the question the confirmation would ask, empty for the generic one.
type Contribution struct {
	Table           string   `json:"table"`
	ID              string   `json:"id"`
	List            string   `json:"list,omitempty"`
	Label           string   `json:"label"`
	Output          string   `json:"output,omitempty"`
	Run             []string `json:"run,omitempty"`
	Open            string   `json:"open,omitempty"`
	Env             []string `json:"env,omitempty"`
	Argv            []string `json:"argv,omitempty"`
	Display         string   `json:"display"`
	Dir             string   `json:"dir,omitempty"`
	URL             string   `json:"url,omitempty"`
	Program         string   `json:"program,omitempty"`
	ConfirmRequired bool     `json:"confirm_required,omitempty"`
	Confirm         string   `json:"confirm,omitempty"`
	Available       bool     `json:"available"`
	Why             *Reason  `json:"why,omitempty"`
}

// slot is a contribution of a manifest with the list it belongs to.
type slot struct {
	e    *Entry
	list *List
}

// contributions explains every contribution of a manifest, in manifest order: commands, item
// actions, actions, then each list followed by its actions.
//
// Parameters:
//   - res: result being filled, whose findings are returned with the new ones added
//   - p: the plugin
//   - lc: inputs of the pass
//   - req: the request, with the field values and the repository
func (r *Registry) contributions(res *CheckResult, p Plugin, lc *loadContext,
	req CheckRequest,
) ([]Contribution, []Finding) {
	m := p.Manifest
	used := map[string]bool{}

	var out []Contribution

	for i := range m.Commands {
		out = append(out, r.explain(p, lc, req, used, slot{&m.Commands[i], nil}))
	}

	for i := range m.ItemActions {
		out = append(out, r.explain(p, lc, req, used, slot{&m.ItemActions[i], nil}))
	}

	for i := range m.Actions {
		out = append(out, r.explainExec(p, req, "action", &m.Actions[i], nil))
	}

	for i := range m.Lists {
		l := &m.Lists[i]
		out = append(out, r.explainExec(p, req, "list", nil, l))

		for j := range l.Actions {
			out = append(out, r.explain(p, lc, req, used, slot{&l.Actions[j], l}))
		}
	}

	return out, append(res.Findings, unusedSet(req, used)...)
}

// unusedSet reports the values of the request that no contribution has a field for.
//
// Parameters:
//   - req: the request
//   - used: field ids that some contribution took
func unusedSet(req CheckRequest, used map[string]bool) []Finding {
	var out []Finding

	for _, k := range sortedKeys(req.Set) {
		if !used[k] {
			out = append(out, Finding{
				Scope: ScopeRequest, Code: CodeSetUnknown,
				Severity: SeverityWarning, Args: []string{k},
			})
		}
	}

	return out
}

// valuesFor selects the values of the request that a contribution has a field for.
//
// Parameters:
//   - req: the request
//   - e: the contribution
//   - used: receives the ids that were taken
func valuesFor(req CheckRequest, e *Entry, used map[string]bool) map[string]string {
	var out map[string]string

	for _, f := range e.Fields {
		if v, ok := req.Set[f.ID]; ok {
			if out == nil {
				out = map[string]string{}
			}

			out[f.ID], used[f.ID] = v, true
		}
	}

	return out
}

// explain builds the argv, directory or URL of a contribution that tote runs.
//
// Parameters:
//   - p: the plugin
//   - lc: inputs of the pass
//   - req: the request
//   - used: receives the field ids of the request that were taken
//   - s: the contribution and its list
func (r *Registry) explain(p Plugin, lc *loadContext, req CheckRequest, used map[string]bool,
	s slot,
) Contribution {
	e := s.e

	c := Contribution{
		Table: e.Table, ID: e.ID, Output: e.Output, Run: e.Run, Open: e.Open,
		Env:   envTableLines(e.Env),
		Label: p.Catalog.Text(req.lang(), entryKeyBase(e, s.list)+".label", e.Label),
	}

	if e.Open != "" {
		c.Display = e.Open
	} else {
		c.Display = shell.PrettyCommand(e.Run)
	}

	if s.list != nil {
		c.List = s.list.ID

		return c.unavailable(newError(CodeUnavailable, "item", "row"))
	}

	call := Call{
		Plugin: p.ID, Table: e.Table, Entry: e.ID, Values: valuesFor(req, e, used),
		Repo: req.Repo, Lang: req.lang(),
	}

	pc, perr := r.newPrep(p, lc, call, e, nil)
	if perr != nil {
		return c.unavailable(perr)
	}

	return r.resolve(c, pc)
}

// resolve fills a contribution from a prepared call without starting anything.
//
// Parameters:
//   - c: contribution with its template and label
//   - pc: prepared call
func (r *Registry) resolve(c Contribution, pc *prep) Contribution {
	if perr := pc.fields(); perr != nil {
		return c.unavailable(perr)
	}

	conf := pc.confirmation()
	c.ConfirmRequired, c.Confirm = conf.Required, conf.Text

	if pc.e.Open != "" {
		url, perr := pc.openURL()
		if perr != nil {
			return c.unavailable(perr)
		}

		c.URL, c.Display, c.Available = url, url, true

		return c
	}

	name, perr := pc.programName()
	if perr != nil {
		return c.unavailable(perr)
	}

	argv, perr := pc.expand(name)
	if perr != nil {
		return c.unavailable(perr)
	}

	c.Argv, c.Display = argv, shell.PrettyCommand(argv)

	dir, perr := pc.directory()
	if perr != nil {
		return c.unavailable(perr)
	}

	c.Dir, c.Available = dir, true

	avoid := pc.ctx.avoid
	avoid.Exactly = append(slices.Clone(avoid.Exactly), filepath.Clean(dir))

	if path, found := LookBin(pc.ctx.pathEnv, name, avoid); found {
		c.Program = path
	} else {
		c.Why = &Reason{CodeDepMissing, []string{name}}
	}

	return c
}

// unavailable marks a contribution that cannot be built, with the reason.
//
// Parameters:
//   - err: why, an *Error from the preparation
func (c Contribution) unavailable(err *Error) Contribution {
	c.Available = false
	c.Why = &Reason{err.Code, err.Args}

	return c
}

// explainExec describes a contribution that the program of an exec plugin answers: an [[action]]
// or a [[list]]. tote starts the program with its fixed arguments and sends the request on
// stdin, so there is nothing to substitute.
//
// Parameters:
//   - p: the plugin
//   - req: the request
//   - table: action or list
//   - e: the action, nil for a list
//   - l: the list, nil for an action
func (r *Registry) explainExec(p Plugin, req CheckRequest, table string, e *Entry,
	l *List,
) Contribution {
	c := Contribution{Table: table, Available: true}

	if e != nil {
		c.ID, c.Output = e.ID, e.Output
		c.Label = p.Catalog.Text(req.lang(), "action."+e.ID+".label", e.Label)
	} else {
		c.ID = l.ID
		c.Label = p.Catalog.Text(req.lang(), "list."+l.ID+".label", l.Label)
	}

	c.Argv = execArgv(p.Manifest, r.opts.Host)
	c.Display = shell.PrettyCommand(c.Argv)

	return c
}

// execArgv gives the program of an exec plugin for the host, relative to the package, followed by
// its fixed arguments.
//
// Parameters:
//   - m: validated manifest
//   - host: running tote, to pick the [[platform]] entry
func execArgv(m *Manifest, host Host) []string {
	switch {
	case m.Exec != nil:
		return append([]string{m.Exec.Bin}, m.Exec.Args...)
	default:
		for _, b := range m.Builds {
			if b.OS == host.OS && b.Arch == host.Arch {
				return []string{b.Bin}
			}
		}
	}

	return nil
}
