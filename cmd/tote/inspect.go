package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/plugin"
	"github.com/totecli/tote/internal/store"
)

// checkOpts lists the options of the check verb.
var checkOpts = []opt{
	{long: "strict"},
	{long: "argv"},
	{long: "set", value: true},
	{long: "repo", value: true},
	{long: "official"},
}

// findingDoc is the JSON form of a finding: the data of the plugin layer and its message.
type findingDoc struct {
	Scope        string          `json:"scope"`
	Code         string          `json:"code"`
	Severity     plugin.Severity `json:"severity"`
	Path         string          `json:"path,omitempty"`
	Line         int             `json:"line,omitempty"`
	Column       int             `json:"column,omitempty"`
	Args         []string        `json:"args"`
	Incompatible bool            `json:"incompatible,omitempty"`
	Message      string          `json:"message"`
}

// checkDoc is the JSON form of "plugin check <target>": the result of the check as it is, with the
// schema, the verdict, the counts and the messages of the findings. The keys id, version,
// tree_sha256 and surface_sha256 stay at the top, where scripts read them.
type checkDoc struct {
	Schema   int          `json:"schema"`
	OK       bool         `json:"ok"`
	Errors   int          `json:"errors"`
	Warnings int          `json:"warnings"`
	Findings []findingDoc `json:"findings"`
	*plugin.CheckResult
}

// diagnosisDoc is the JSON form of "plugin check" without a target.
type diagnosisDoc struct {
	Schema    int           `json:"schema"`
	OK        bool          `json:"ok"`
	Strict    bool          `json:"strict"`
	Problems  []reasonDoc   `json:"problems"`
	Plugins   []checkDoc    `json:"plugins"`
	Orphans   []orphanDoc   `json:"orphans"`
	Leftovers []leftoverDoc `json:"leftovers"`
	Findings  []findingDoc  `json:"findings"`
}

// orphanDoc is the JSON form of an entry that is not a plugin.
type orphanDoc struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// leftoverDoc is the JSON form of something a plugin left behind.
type leftoverDoc struct {
	Kind       string `json:"kind"`
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	AgeSeconds int64  `json:"age_seconds"`
}

// findingDocs converts findings and adds their messages.
//
// Parameters:
//   - fs: findings of a check
func (a *app) findingDocs(fs []plugin.Finding) []findingDoc {
	out := make([]findingDoc, 0, len(fs))

	for _, f := range fs {
		out = append(out, findingDoc{
			Scope: f.Scope, Code: f.Code, Severity: f.Severity,
			Path: f.Path, Line: f.Line, Column: f.Column, Args: orEmpty(f.Args),
			Incompatible: f.Incompatible, Message: a.findingText(f),
		})
	}

	return out
}

// findingArgs prepares the arguments of a finding of the settings for its message.
//
// Parameters:
//   - f: the finding
func (a *app) findingArgs(f plugin.Finding) []string {
	switch f.Code {
	case plugin.CodeSettingsIgnore:
		return []string{
			arg(f.Args, 0), a.word("plugin_source_", arg(f.Args, 1)),
			a.word("plugin_reject_", arg(f.Args, 2)),
		}
	case plugin.CodeSettingsFile:
		return []string{a.word("plugin_reject_", arg(f.Args, 0))}
	}

	return f.Args
}

// findingText gives the message of a finding. A finding of the state or the environment scope
// carries a reason code of the state machine, the others carry an error or issue code.
//
// Parameters:
//   - f: the finding
func (a *app) findingText(f plugin.Finding) string {
	if f.Scope == plugin.ScopeState || f.Scope == plugin.ScopeEnv {
		return a.reasonText(plugin.Reason{Code: f.Code, Args: f.Args})
	}

	return a.errText(f.Code, a.findingArgs(f), nil)
}

// findingMessage gives the message of a finding with its position.
//
// Parameters:
//   - f: the finding
func (a *app) findingMessage(f plugin.Finding) string {
	if f.Scope == plugin.ScopeState || f.Scope == plugin.ScopeEnv {
		return a.findingText(f)
	}

	return a.issueText(f.Path, f.Line, f.Code, a.findingArgs(f))
}

// toCheckDoc converts a result for JSON.
//
// Parameters:
//   - res: result of a check
func (a *app) toCheckDoc(res *plugin.CheckResult) checkDoc {
	return checkDoc{
		Schema: pluginSchema, OK: !res.Failed(), Errors: res.Errors(),
		Warnings: res.Warnings(), Findings: a.findingDocs(res.Findings), CheckResult: res,
	}
}

// parseSets reads the --set options as a map from field to value.
//
// Parameters:
//   - values: every --set, as field=value
//
// Returns the map, or an *optError for a value without an equal sign.
func parseSets(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}

	out := map[string]string{}

	for _, v := range values {
		k, val, ok := strings.Cut(v, "=")
		if !ok || k == "" {
			return nil, &optError{Key: "cli_plugin_bad_set", Args: []any{v}}
		}

		out[k] = val
	}

	return out, nil
}

// pluginCheck implements "tote plugin check [<id|dir|archive>] [--strict] [--official] [--argv
// [--set field=value]... [--repo NAME]]".
//
// It runs no plugin code. With a target it examines that plugin and prints what it would run and
// the hashes an author publishes; without one it checks every installed plugin and lists what is
// left on disk.
//
// Parameters:
//   - args: arguments after check
//
// Returns the exit code of the verb: 0, or 1 when the check found an error (a warning too under
// --strict).
func (a *app) pluginCheck(args []string) int {
	v, ok, rc := a.pluginParse(args, checkOpts, 0, 1, "arg_id")
	if !ok {
		return rc
	}

	sets, err := parseSets(v.o.all["set"])
	if err == nil && (len(sets) > 0 || v.o.set["repo"]) && !v.o.set["argv"] {
		err = &optError{Key: "cli_plugin_argv_needed"}
	}

	if err != nil {
		if v.js {
			return a.usageJSON(err)
		}

		return a.usageErr(err, "plugin")
	}

	reg, rerr := a.pluginRegistry()
	if rerr != nil {
		return a.pluginFail(rerr, v.js)
	}

	if name := v.o.vals["repo"]; name != "" {
		if rc, missing := a.unknownRepo(name, v.js); missing {
			return rc
		}
	}

	ctx, cancel := a.context()
	defer cancel()

	if len(v.o.pos) == 0 {
		return a.diagnose(a.hoistSharedDir(reg.Diagnose(ctx, v.o.set["strict"])), v.js)
	}

	res, cerr := reg.Check(ctx, plugin.CheckRequest{
		Target: v.o.pos[0], Strict: v.o.set["strict"],
		Official: v.o.set["official"], Lang: a.tr().Lang(), Set: sets, Repo: v.o.vals["repo"],
	})
	if cerr != nil {
		return a.pluginFail(cerr, v.js)
	}

	if v.js {
		doc := a.toCheckDoc(res)
		if err := writeJSON(a.Stdout, doc); err != nil {
			return a.fail(err)
		}
	} else {
		a.checkLines(res)
	}

	return a.checkExit(res.Failed(), res.Findings)
}

// unknownRepo tells the user that --repo names a repository that is not saved. A store that
// cannot be read is not reported here: the check goes on and shows why a command cannot be built.
//
// Parameters:
//   - name: value of --repo
//   - js: true for JSON
//
// Returns the exit code and true when the repository does not exist.
func (a *app) unknownRepo(name string, js bool) (int, bool) {
	paths, err := a.cfgPaths()
	if err != nil {
		return exitOK, false
	}

	repos, err := store.OpenRepos(paths.ReposFile())
	if err != nil || repos.Err() != nil {
		return exitOK, false
	}

	if _, found := repos.Get(name); found {
		return exitOK, false
	}

	if js {
		msg := a.t("cli_not_found", a.t("kind_repo"), sanitize(name))

		return a.reportJSON(codeNotFound, msg, exitFail), true
	}

	var names []string
	for _, r := range repos.List() {
		names = append(names, r.Name)
	}

	return a.notFound("kind_repo", name, names), true
}

// checkExit gives the exit code of a check: 0 when it found nothing that fails, 5 when a finding
// is an integrity failure (files that were modified, an archive that is bad or too large) and 1
// for any other failure.
//
// Parameters:
//   - failed: true when the check fails
//   - fs: the findings
func (a *app) checkExit(failed bool, fs []plugin.Finding) int {
	if !failed {
		return exitOK
	}

	for _, f := range fs {
		if f.Severity == plugin.SeverityError && plugin.ExitClass(f.Code) == plugin.ExitIntegrity {
			return plugin.ExitIntegrity
		}
	}

	return exitFail
}

// findingLines prints the findings of a check, each indented.
//
// Parameters:
//   - fs: the findings
func (a *app) findingLines(fs []plugin.Finding) {
	for _, f := range fs {
		sev := a.t("cli_plugin_severity_error")
		if f.Severity == plugin.SeverityWarning {
			sev = a.t("cli_plugin_severity_warning")
		}

		text := a.findingMessage(f)
		fmt.Fprintln(a.Stdout, "  "+sev+a.colon()+" "+text)
	}
}

// verdictLine prints the last line of a check.
//
// Parameters:
//   - name: what was checked
//   - res: the result
func (a *app) verdictLine(name string, res *plugin.CheckResult) {
	errs, warns := res.Errors(), res.Warnings()

	if errs == 0 && warns == 0 {
		fmt.Fprintln(a.Stdout, a.t("cli_plugin_check_ok", sanitize(name)))

		return
	}

	fmt.Fprintln(a.Stdout, a.t("cli_plugin_check_summary", sanitize(name), errs, warns))
}

// headText writes the first line of the check of a plugin: its id, its version when it has one
// and a word in brackets.
//
// Parameters:
//   - id: plugin id
//   - version: its version, possibly empty
//   - tail: tier or state, already in the language of the run
func (a *app) headText(id, version, tail string) string {
	if version == "" {
		return a.t("cli_plugin_check_bare", sanitize(id), tail)
	}

	return a.t("cli_plugin_check_head", sanitize(id), sanitize(version), tail)
}

// checkLines prints the result of the check of one plugin.
//
// Parameters:
//   - res: the result
func (a *app) checkLines(res *plugin.CheckResult) {
	fmt.Fprintln(a.Stdout, a.t("cli_plugin_check_target", a.targetWord(res.Kind),
		sanitize(res.Target)))

	if res.ID != "" {
		fmt.Fprintln(a.Stdout, a.headText(res.ID, res.Version, a.word("plugin_tiername_",
			res.Tier)))
	}

	if res.State != "" {
		fmt.Fprintln(a.Stdout,
			a.fieldLabel("cli_plugin_label_state")+a.stateWord(string(res.State)))

		for _, r := range res.Reasons {
			fmt.Fprintln(a.Stdout, "  - "+a.reasonText(r))
		}
	}

	a.findingLines(res.Findings)
	a.contributionLines(res.Contributions)
	a.requirementLines(res.Requires)

	if len(res.Caps) > 0 {
		fmt.Fprintln(a.Stdout, a.t("cli_plugin_caps_head"))

		for _, c := range res.Caps {
			fmt.Fprintln(a.Stdout, "  - "+sanitize(c))
		}
	}

	a.hashLines(hashesDoc{
		Archive: res.ArchiveSHA256, Tree: res.TreeSHA256,
		Surface: res.SurfaceSHA256,
	})

	if res.Grant != "" {
		fmt.Fprintln(a.Stdout, a.fieldLabel("cli_plugin_label_grant")+res.Grant)
	}

	if res.Kind == plugin.TargetArchive && res.ArchiveSHA256 != "" {
		asset := filepath.Base(res.Target)
		fmt.Fprintln(a.Stdout, a.t("cli_plugin_sidecar", sanitize(asset)))
		fmt.Fprint(a.Stdout, "  "+plugin.Sidecar(res.ArchiveSHA256, sanitize(asset)))
	}

	a.verdictLine(res.Target, res)
}

// targetWord names a kind of target.
//
// Parameters:
//   - kind: one of the Target constants of the plugin package
func (a *app) targetWord(kind string) string {
	switch kind {
	case plugin.TargetInstalled:
		return a.t("cli_plugin_target_installed")
	case plugin.TargetArchive:
		return a.t("cli_plugin_target_archive")
	case plugin.TargetBundled:
		return a.t("cli_plugin_target_bundled")
	}

	return a.t("cli_plugin_target_directory")
}

// contributionLines prints the exact command of every contribution, or why it cannot be built.
//
// Parameters:
//   - cs: contributions of a plugin
func (a *app) contributionLines(cs []plugin.Contribution) {
	if len(cs) == 0 {
		return
	}

	fmt.Fprintln(a.Stdout, a.t("cli_plugin_contrib_head"))

	for _, c := range cs {
		head := "  - " + a.word("plugin_table_", c.Table) + " " + sanitize(c.ID) + a.colon() + " "

		switch {
		case c.Display != "":
			fmt.Fprintln(a.Stdout, head+sanitize(c.Display))
		default:
			fmt.Fprintln(a.Stdout, head+sanitize(strings.Join(c.Run, " ")))
		}

		if c.Dir != "" {
			fmt.Fprintln(a.Stdout, "    "+a.t("cli_plugin_contrib_dir", sanitize(c.Dir)))
		}

		if c.Why != nil {
			fmt.Fprintln(a.Stdout, "    "+a.t("cli_plugin_contrib_why", a.reasonText(*c.Why)))
		}
	}
}

// requirementLines prints the programs a plugin needs and whether PATH has them.
//
// Parameters:
//   - rs: requirements
func (a *app) requirementLines(rs []plugin.Requirement) {
	if len(rs) == 0 {
		return
	}

	fmt.Fprintln(a.Stdout, a.t("cli_plugin_label_requires")+a.colon())

	for _, r := range rs {
		switch {
		case r.Found:
			fmt.Fprintln(a.Stdout, "  - "+a.t("cli_plugin_req_found", sanitize(r.Bin),
				sanitize(r.Path)))
		case r.Optional:
			fmt.Fprintln(a.Stdout, "  - "+a.t("cli_plugin_req_optional", sanitize(r.Bin)))
		default:
			fmt.Fprintln(a.Stdout, "  - "+a.t("cli_plugin_req_missing", sanitize(r.Bin)))
		}

		if !r.Found && r.Hint != "" {
			fmt.Fprintln(a.Stdout, "    "+a.t("plugin_hint_label", sanitize(r.Hint)))
		}
	}
}

// diagnose prints the result of a check of every installed plugin.
//
// Parameters:
//   - d: what the plugin layer found
//   - js: true for JSON
func (a *app) diagnose(d plugin.Diagnosis, js bool) int {
	if js {
		return a.diagnoseJSON(d)
	}

	fmt.Fprintln(a.Stdout, a.t("cli_plugin_diag_head"))

	for _, r := range d.Problems {
		fmt.Fprintln(a.Stdout, "  "+a.t("cli_plugin_problem", a.reasonText(r)))
	}

	for _, res := range d.Plugins {
		fmt.Fprintln(a.Stdout, a.headText(res.ID, res.Version, a.stateWord(string(res.State))))
		a.findingLines(res.Findings)

		// A broken plugin whose only finding went up to the problems above has nothing to say.
		if res.State != plugin.StateBroken || len(res.Findings) > 0 {
			a.verdictLine(res.ID, res)
		}
	}

	a.findingLines(d.Findings)

	for _, o := range d.Orphans {
		fmt.Fprintln(a.Stdout, a.t("cli_plugin_diag_orphan", sanitize(o.Name),
			a.word("plugin_orphan_", o.Kind)))
	}

	for _, l := range d.Leftovers {
		name := l.Name
		if l.ID != "" && l.ID != l.Name {
			name = l.ID + " " + l.Name
		}

		fmt.Fprintln(a.Stdout, a.t("cli_plugin_diag_leftover", a.word("plugin_left_", l.Kind),
			sanitize(strings.TrimSpace(name))))
	}

	fmt.Fprintln(a.Stdout, a.t("cli_plugin_diag_plugins", len(d.Plugins)))

	return a.diagnoseExit(d)
}

// diagnoseExit gives the exit code of a check of every installed plugin: the same classes as
// checkExit, over the findings of every plugin and the general ones.
//
// Parameters:
//   - d: what the plugin layer found
func (a *app) diagnoseExit(d plugin.Diagnosis) int {
	all := append([]plugin.Finding(nil), d.Findings...)

	for _, res := range d.Plugins {
		all = append(all, res.Findings...)
	}

	return a.checkExit(d.Failed(), all)
}

// diagnoseJSON prints the result of a check of every installed plugin as JSON.
//
// Parameters:
//   - d: what the plugin layer found
func (a *app) diagnoseJSON(d plugin.Diagnosis) int {
	doc := diagnosisDoc{
		Schema: pluginSchema, OK: !d.Failed(), Strict: d.Strict,
		Problems: a.reasonDocs(d.Problems), Plugins: []checkDoc{}, Orphans: []orphanDoc{},
		Leftovers: []leftoverDoc{}, Findings: a.findingDocs(d.Findings),
	}

	for _, res := range d.Plugins {
		doc.Plugins = append(doc.Plugins, a.toCheckDoc(res))
	}

	for _, o := range d.Orphans {
		doc.Orphans = append(doc.Orphans, orphanDoc{Kind: o.Kind, Name: o.Name})
	}

	for _, l := range d.Leftovers {
		doc.Leftovers = append(doc.Leftovers, leftoverDoc{
			Kind: l.Kind, ID: l.ID, Name: l.Name,
			AgeSeconds: int64(l.Age / time.Second),
		})
	}

	if err := writeJSON(a.Stdout, doc); err != nil {
		return a.fail(err)
	}

	return a.diagnoseExit(d)
}

// hoistSharedDir reports an unsafe plugins directory once, as a problem of every plugin, instead
// of once inside each plugin. A directory that others can write turns every plugin off; the
// plugin layer says so for each of them, and says nothing when no plugin is installed.
//
// Parameters:
//   - d: what the plugin layer found
//
// Returns d, with the problem added and the same finding taken out of the plugins, when the
// plugins directory belongs to the user and others can write it.
func (a *app) hoistSharedDir(d plugin.Diagnosis) plugin.Diagnosis {
	paths, err := a.cfgPaths()
	if err != nil {
		return d
	}

	dir := sharedPluginsDir(paths, a.geteuid())
	if dir == "" {
		return d
	}

	d.Problems = append([]plugin.Reason{{Code: plugin.CodeStoreWritable, Args: []string{dir, dir}}},
		d.Problems...)

	plugins := make([]*plugin.CheckResult, len(d.Plugins))

	for i, res := range d.Plugins {
		cp := *res
		cp.Findings = nil

		for _, f := range res.Findings {
			if f.Code != plugin.CodeStoreWritable || arg(f.Args, 0) != dir {
				cp.Findings = append(cp.Findings, f)
			}
		}

		if cp.Findings == nil {
			cp.Findings = []plugin.Finding{}
		}

		plugins[i] = &cp
	}

	d.Plugins = plugins

	return d
}

// sharedPluginsDir finds out whether others can write the plugins directory.
//
// Parameters:
//   - paths: resolved tote directories
//   - uid: user id that must own the directory
//
// Returns the path of the directory, as the plugin layer spells it, when it belongs to uid and its
// mode lets the group or the others write, else an empty string. The mode is read on the real
// path, but the answer keeps the spelling of the configuration: a directory below a link (the
// temporary directory of macOS is one) is otherwise never matched with the finding of the plugin
// layer, which is then repeated inside every plugin.
func sharedPluginsDir(paths config.Paths, uid int) string {
	resolved, err := filepath.EvalSymlinks(paths.PluginsDir())
	if err != nil {
		return ""
	}

	info, err := os.Lstat(resolved)
	if err != nil || !info.IsDir() {
		return ""
	}

	if owner, known := ownerOf(info); !known || owner != uid || info.Mode().Perm()&0o022 == 0 {
		return ""
	}

	return paths.PluginsDir()
}
