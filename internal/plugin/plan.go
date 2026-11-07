package plugin

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// Tables whose contributions Prepare runs, and the output modes of a run.
const (
	TableCommand    = "command"
	TableItemAction = "item_action"
	TableListAction = "list.action"
	OutputPager     = "pager"
	OutputSilent    = "silent"
	OutputTerminal  = "terminal"
	PauseAlways     = "always"
	PauseError      = "error"
	PauseNever      = "never"
)

// Limits of a call.
const (
	maxValueBytes = 4096
	patternCache  = 512
)

// Call asks to run one contribution of one plugin.
//
// Plugin is the id; Table is TableCommand, TableItemAction or TableListAction and Entry the id of
// the contribution, List the id of the [[list]] of a list action. Values are the submitted form
// fields by id, as text (a bool is "true" or "false"); a field that is absent takes its default.
// Repo is the name of the saved repository the call is about, empty for the current repository.
// Item is the row of a list action. Lang is the language of the interface and Mono is true in the
// mono theme. Yes records that the user already confirmed, which satisfies a confirm key.
type Call struct {
	Plugin string
	Table  string
	Entry  string
	List   string
	Values map[string]string
	Repo   string
	Item   *Item
	Lang   string
	Mono   bool
	Yes    bool
}

// Confirmation is the confirm key of a contribution once translated and expanded. Text is empty
// for the generic question.
type Confirmation struct {
	Required bool
	Text     string
}

// Prepared is a run that has passed every check and has nothing left to decide: the host shows
// Display, Dir and Confirm, and then hands it to Run, or, for a terminal run, to its own exec path
// after a call to Guard.
//
// Argv starts with the absolute path of the program and holds the elements after substitution;
// Name is the program as the manifest wrote it. Env is the complete environment, KEY=value, and
// nothing else may be added. EnvTable is the env table of the contribution as sorted NAME=template
// lines, for the confirmation to show next to Display. Dir is the working directory. Display is
// the command as the confirmation shows it. Output is pager, silent or terminal, empty for an open
// contribution, whose URL is OpenURL. Pause applies to terminal runs. Timeout is zero for a
// terminal run. A Prepared is bound to the tree and the surface it was prepared from: Run and
// Guard refuse it when either changed.
type Prepared struct {
	PluginID  string
	Table     string
	EntryID   string
	ListID    string
	Output    string
	Pause     string
	Name      string
	Argv      []string
	Display   string
	Dir       string
	Env       []string
	EnvTable  []string
	Timeout   time.Duration
	OpenURL   string
	Confirm   Confirmation
	Confirmed bool
	Mono      bool
	tree      string
	surface   string
	program   statID
	opener    string
	secrets   []string
}

// IsOpen reports whether the run only asks the platform to open a URL.
func (p Prepared) IsOpen() bool { return p.OpenURL != "" }

// IsTerminal reports whether the program owns the terminal; the host runs it.
func (p Prepared) IsTerminal() bool { return p.Output == OutputTerminal }

// Prepare checks a call and builds everything a run needs. It reads the lock and config.toml
// again and computes the state of the plugin from them, so that a disable made in another terminal
// is seen here, and refuses unless the plugin is enabled: a lock record, a tree that matches its
// hash, a consent that covers the capabilities and the surface, no kill switch, a compatible
// manifest and no --no-plugins.
//
// It then validates the form fields (required, max_len, pattern, choices), substitutes the
// placeholders into the argv elements, one element at a time and never through a shell, applies
// the dash rule, resolves the program on a scrubbed PATH, finds the working directory, and builds
// the environment from nothing. It runs no plugin code; the one program it may start is git, to
// derive {repo.remote}, {repo.slug} and {repo.branch}.
//
// Parameters:
//   - call: contribution to run, with the values of its form
//
// Returns the prepared run, or an *Error.
func (r *Registry) Prepare(call Call) (Prepared, error) {
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

	pc, err := r.newPrep(p, ctx, call, e, list)
	if err != nil {
		return Prepared{}, err
	}

	return pc.build()
}

// notRunnable builds the refusal for a plugin that is not enabled.
//
// Parameters:
//   - p: plugin as Recheck returned it
func notRunnable(p Plugin) *Error {
	first := ""
	if len(p.Reasons) > 0 {
		first = p.Reasons[0].Code
	}

	return newError(CodeNotRunnable, p.ID, string(p.State), first)
}

// findEntry looks the contribution of a call up in the manifest.
//
// Parameters:
//   - m: manifest of a verified tree
//   - call: the call
//
// Returns the entry and, for a list action, its list.
func findEntry(m *Manifest, call Call) (*Entry, *List, *Error) {
	var set []Entry

	switch call.Table {
	case TableCommand:
		set = m.Commands
	case TableItemAction:
		set = m.ItemActions
	case TableListAction:
		for i := range m.Lists {
			if m.Lists[i].ID == call.List {
				return pickEntry(m.Lists[i].Actions, call.Entry, &m.Lists[i])
			}
		}

		return nil, nil, newError(CodeNoEntry, call.Table, call.List+"/"+call.Entry)
	case "action", "list":
		return nil, nil, newError(CodeUnsupported, call.Table)
	default:
		return nil, nil, newError(CodeNoEntry, call.Table, call.Entry)
	}

	return pickEntry(set, call.Entry, nil)
}

// pickEntry finds a contribution by id.
//
// Parameters:
//   - set: contributions of one table
//   - id: contribution id
//   - list: owning list, nil outside a list
func pickEntry(set []Entry, id string, list *List) (*Entry, *List, *Error) {
	for i := range set {
		if set[i].ID == id {
			if set[i].Run == nil && set[i].Open == "" {
				return nil, nil, newError(CodeUnsupported, set[i].Table, id)
			}

			return &set[i], list, nil
		}
	}

	return nil, nil, newError(CodeNoEntry, "entry", id)
}

// prep is the state of one Prepare.
type prep struct {
	r     *Registry
	p     Plugin
	ctx   *loadContext
	call  Call
	e     *Entry
	list  *List
	vals  *Values
	cwd   string
	tmpl  map[string]Template
	allow map[string]bool
	avoid Avoid
	dir   string
}

// newPrep gathers the inputs of a call: the effective settings, the repository and the parsed
// templates.
//
// Parameters:
//   - p: enabled plugin
//   - ctx: inputs of the pass
//   - call: the call
//   - e: contribution
//   - list: its list, nil outside a list
func (r *Registry) newPrep(p Plugin, ctx *loadContext, call Call, e *Entry,
	list *List,
) (*prep, *Error) {
	pc := &prep{
		r: r, p: p, ctx: ctx, call: call, e: e, list: list, allow: map[string]bool{},
		tmpl: map[string]Template{},
	}

	for _, n := range e.AllowDash {
		pc.allow[n] = true
	}

	pc.cwd = r.opts.Cwd
	if pc.cwd == "" {
		pc.cwd, _ = os.Getwd()
	}

	pc.vals = &Values{
		Cwd: pc.cwd, Home: r.opts.Home, Fields: map[string]FieldValue{},
		Settings: map[string]SettingValue{}, Item: call.Item,
	}

	for _, sv := range r.Settings(p).Values {
		pc.vals.Settings[sv.ID] = sv
	}

	if err := pc.parseAll(); err != nil {
		return nil, err
	}

	if err := pc.chooseRepo(); err != nil {
		return nil, err
	}

	return pc, nil
}

// parseAll parses every template of the contribution once. The manifest was validated at load, so
// a failure here means the manifest is not the one that was checked.
func (pc *prep) parseAll() *Error {
	e := pc.e

	texts := []string{e.Open, e.Cwd, e.Confirm.Text}
	texts = append(texts, e.Run...)

	for _, v := range e.Env {
		texts = append(texts, v)
	}

	for _, f := range e.Fields {
		if f.Type != "bool" {
			texts = append(texts, f.Default)
		}
	}

	for _, s := range texts {
		if _, done := pc.tmpl[s]; done || s == "" {
			continue
		}

		t, perr := ParseTemplate(s)
		if perr != nil {
			return newError(CodePlaceholderSyntax, e.Path, perr.Reason)
		}

		pc.tmpl[s] = t
	}

	return nil
}

// usage tells which kinds of repository data the templates read.
func (pc *prep) usage() (repo, git bool) {
	for _, t := range pc.tmpl {
		for _, h := range t.Placeholders() {
			if h.Kind != KindRepo {
				continue
			}

			repo = true
			git = git || h.Member == "remote" || h.Member == "slug" || h.Member == "branch"
		}
	}

	return repo, git
}

// chooseRepo finds the repository the call is about: the named saved repository, else, for a
// call that reads {repo.*} outside a repository action, the current repository. It needs
// host:read:repos, which a validated manifest always declares and this check repeats.
func (pc *prep) chooseRepo() *Error {
	readsRepo, needGit := pc.usage()
	target := pc.e.Target == "repo"

	if !readsRepo && !target {
		return nil
	}

	if !slices.Contains(pc.p.Manifest.Permissions, PermReadRepos) {
		return newError(CodePermMissing, PermReadRepos)
	}

	repos, err := store.OpenRepos(pc.r.opts.Paths.ReposFile())
	if err != nil {
		return wrapError(CodeUnavailable, err, "repo", "unreadable")
	}

	if err := repos.Err(); err != nil {
		return wrapError(CodeUnavailable, err, "repo", "unreadable")
	}

	var saved store.Repo

	found := false

	switch {
	case pc.call.Repo != "":
		saved, found = repos.Get(pc.call.Repo)
	case target:
	default:
		saved, found = CurrentRepo(repos.List(), pc.cwd, pc.r.opts.Home)
	}

	if !found {
		if target {
			return newError(CodeUnavailable, "repo", "unknown")
		}

		return nil
	}

	info, ok := pc.r.repoInfo(pc.ctx, saved, RepoNeeds{Git: needGit})
	if !ok {
		if target {
			return newError(CodeUnavailable, "repo", "path")
		}

		return nil
	}

	pc.vals.Repo = &info

	return nil
}

// fields validates the submitted values and fills in the defaults, in the order of the form.
func (pc *prep) fields() *Error {
	known := map[string]bool{}
	for _, f := range pc.e.Fields {
		known[f.ID] = true
	}

	for _, k := range sortedKeys(pc.call.Values) {
		if !known[k] {
			return newError(CodeInputInvalid, k, RejectUnknown)
		}
	}

	for i := range pc.e.Fields {
		f := &pc.e.Fields[i]

		raw, given := pc.call.Values[f.ID]
		if !given {
			raw = pc.defaultOf(f)
		}

		fv, err := checkField(f, raw)
		if err != nil {
			return err
		}

		pc.vals.Fields[f.ID] = fv
	}

	return nil
}

// defaultOf gives the default of a field: the literal of a bool, or a template over the earlier
// fields, the settings, the repository, the working directory and the home. What does not resolve
// gives the empty text.
//
// Parameters:
//   - f: field
func (pc *prep) defaultOf(f *Field) string {
	if !f.HasDefault {
		return ""
	}

	if f.Type == "bool" {
		return f.Default
	}

	return CleanLine(ExpandLoose(pc.tmpl[f.Default], pc.vals), maxFieldBytes)
}

// Defaults gives the initial value of every field of a contribution, which is what a form shows
// before the user types: the default templates expanded over the fields before them, the settings,
// the repository, the working directory and the home. It starts nothing but git.
//
// Parameters:
//   - call: the call; its Values are ignored
//
// Returns the values by field id, and an *Error when the contribution cannot be found or the
// plugin cannot run.
func (r *Registry) Defaults(call Call) (map[string]string, error) {
	if err := config.ValidPluginID(call.Plugin); err != nil {
		return nil, wrapError(CodeBadID, err, call.Plugin)
	}

	p, ctx, ok := r.recheck(call.Plugin)
	if !ok {
		return nil, newError(CodeNotRunnable, call.Plugin, "absent")
	}

	if !p.State.Runnable() || p.Manifest == nil {
		return nil, notRunnable(p)
	}

	e, list, err := findEntry(p.Manifest, call)
	if err != nil {
		return nil, err
	}

	pc, err := r.newPrep(p, ctx, call, e, list)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}

	for i := range e.Fields {
		f := &e.Fields[i]
		v := pc.defaultOf(f)
		out[f.ID] = v

		if fv, ferr := checkField(f, v); ferr == nil {
			pc.vals.Fields[f.ID] = fv
		} else {
			pc.vals.Fields[f.ID] = FieldValue{Type: f.Type}
		}
	}

	return out, nil
}

// patterns caches the compiled patterns of fields, which are few and come from verified
// manifests.
var patterns = struct {
	mu sync.Mutex
	m  map[string]*regexp.Regexp
}{m: map[string]*regexp.Regexp{}}

// compilePattern compiles a field pattern once.
//
// Parameters:
//   - src: RE2 source
func compilePattern(src string) (*regexp.Regexp, error) {
	patterns.mu.Lock()
	defer patterns.mu.Unlock()

	if re, ok := patterns.m[src]; ok {
		return re, nil
	}

	re, err := regexp.Compile(src)
	if err != nil {
		return nil, err
	}

	if len(patterns.m) >= patternCache {
		patterns.m = map[string]*regexp.Regexp{}
	}

	patterns.m[src] = re

	return re, nil
}

// checkField validates one submitted value against its field: first the required rule, then
// max_len, then the pattern, for a text; membership for a choice; a boolean for a bool. The dash
// rule is applied later, at substitution.
//
// Parameters:
//   - f: field
//   - raw: submitted text
func checkField(f *Field, raw string) (FieldValue, *Error) {
	bad := func(rule string) (FieldValue, *Error) {
		return FieldValue{}, newError(CodeInputInvalid, f.ID, rule)
	}

	if len(raw) > maxValueBytes {
		return bad(RejectMaxLen)
	}

	switch f.Type {
	case "bool":
		return checkBool(f, raw)
	case "choice":
		if raw == "" && f.Required {
			return bad(RejectRequired)
		}

		if !slices.Contains(f.Choices, raw) {
			return bad(RejectChoice)
		}

		return FieldValue{Text: raw, Type: f.Type}, nil
	}

	if rule := ProducedIssue(raw, maxValueBytes); rule != "" {
		return bad(rule)
	}

	switch {
	case raw == "" && f.Required:
		return bad(RejectRequired)
	case utf8.RuneCountInString(raw) > f.MaxLen:
		return bad(RejectMaxLen)
	case raw != "" && f.Pattern != "" && !matchPattern(f.Pattern, raw):
		return bad(RejectPattern)
	}

	return FieldValue{Text: raw, Type: f.Type}, nil
}

// matchPattern tests a value against a field pattern. A pattern that does not compile matches
// nothing: the manifest was validated, so this is a refusal and not a pass.
//
// Parameters:
//   - src: pattern
//   - raw: value
func matchPattern(src, raw string) bool {
	re, err := compilePattern(src)

	return err == nil && re.MatchString(raw)
}

// checkBool reads the value of a bool field: true, false or empty (false), and the usual words.
//
// Parameters:
//   - f: bool field
//   - raw: submitted text
func checkBool(f *Field, raw string) (FieldValue, *Error) {
	var on bool

	switch strings.ToLower(raw) {
	case "true", "1", "yes", "on":
		on = true
	case "false", "0", "no", "off", "":
	default:
		return FieldValue{}, newError(CodeInputInvalid, f.ID, RejectBool)
	}

	if f.Required && !on {
		return FieldValue{}, newError(CodeInputInvalid, f.ID, RejectRequired)
	}

	if !on {
		return FieldValue{Type: f.Type}, nil
	}

	return FieldValue{Text: f.Flag, Type: f.Type}, nil
}

// build runs the checks that follow the form and assembles the result.
func (pc *prep) build() (Prepared, error) {
	if err := pc.fields(); err != nil {
		return Prepared{}, err
	}

	e := pc.e

	out := Prepared{
		PluginID: pc.p.ID, Table: e.Table, EntryID: e.ID, Mono: pc.call.Mono,
		Confirm: pc.confirmation(), Confirmed: pc.call.Yes, tree: pc.p.Record.TreeSHA256,
		surface: pc.p.Surface,
	}

	if pc.list != nil {
		out.ListID = pc.list.ID
	}

	if e.Open != "" {
		url, err := pc.openURL()
		if err != nil {
			return Prepared{}, err
		}

		out.OpenURL = url

		if out.opener, err = pc.openerPath(); err != nil {
			return Prepared{}, err
		}

		return out, nil
	}

	if err := pc.program(&out); err != nil {
		return Prepared{}, err
	}

	if err := pc.environment(&out); err != nil {
		return Prepared{}, err
	}

	out.EnvTable = envTableLines(e.Env)

	return out, nil
}

// envTableLines lists the env table of a contribution as sorted NAME=template lines, the form the
// consent view and the confirmation show.
//
// Parameters:
//   - env: env table of the contribution
func envTableLines(env map[string]string) []string {
	var out []string

	for _, k := range sortedKeys(env) {
		out = append(out, k+"="+env[k])
	}

	return out
}

// confirmation renders the confirm key: the translated text with its placeholders shown, cleaned
// and cut.
func (pc *prep) confirmation() Confirmation {
	e := pc.e
	if !e.Confirm.Set {
		return Confirmation{}
	}

	if e.Confirm.Text == "" {
		return Confirmation{Required: true}
	}

	text := pc.p.Catalog.Text(pc.call.Lang, pc.keyBase()+".confirm", e.Confirm.Text)

	t, perr := ParseTemplate(text)
	if perr != nil {
		t = pc.tmpl[e.Confirm.Text]
	}

	return Confirmation{Required: true, Text: CleanLine(ExpandLoose(t, pc.vals), maxConfirmRunes)}
}

// keyBase gives the catalog key prefix of the contribution.
func (pc *prep) keyBase() string { return entryKeyBase(pc.e, pc.list) }

// entryKeyBase gives the catalog key prefix of a contribution: table.id, or
// list.<list>.action.<id> for the action of a list.
//
// Parameters:
//   - e: the contribution
//   - list: its list, nil outside a list
func entryKeyBase(e *Entry, list *List) string {
	if list != nil {
		return "list." + list.ID + ".action." + e.ID
	}

	return e.Table + "." + e.ID
}

// program builds the argv, resolves the program, the directory and the limits of a run.
//
// Parameters:
//   - out: prepared run being filled
func (pc *prep) program(out *Prepared) *Error {
	e := pc.e

	name, perr := pc.programName()
	if perr != nil {
		return perr
	}

	argv, err := pc.expand(name)
	if err != nil {
		return err
	}

	dir, err := pc.directory()
	if err != nil {
		return err
	}

	pc.dir = dir
	pc.avoid = pc.ctx.avoid
	pc.avoid.Exactly = append(append([]string(nil), pc.avoid.Exactly...), filepath.Clean(dir))

	abs, found := LookBin(pc.ctx.pathEnv, name, pc.avoid)
	if !found {
		return pc.missing(name)
	}

	info, serr := os.Stat(abs)
	if serr != nil {
		return pc.missing(name)
	}

	if pc.isSelfFile(info) {
		return newError(CodeExecSelf, name)
	}

	id, _ := statIDOf(info)

	out.Name, out.Dir, out.Display = name, dir, shell.PrettyCommand(argv)
	out.Argv = append([]string{abs}, argv[1:]...)
	out.program = id
	out.Output = e.Output
	out.Pause = pauseOf(e)
	out.Timeout = timeoutOf(e)

	return nil
}

// isSelfFile reports whether a resolved program is the tote binary under another name, a link or
// an alias: the name test of programName cannot see those.
//
// Parameters:
//   - info: file information of the resolved program
func (pc *prep) isSelfFile(info fs.FileInfo) bool {
	bin := pc.r.opts.Bin
	if bin == "" {
		return false
	}

	self, err := os.Stat(bin)

	return err == nil && os.SameFile(info, self)
}

// expand substitutes the elements of the run template after the program, one at a time, and
// applies the dash rule and the rule for ssh and its relatives.
//
// Parameters:
//   - name: program at element 0
//
// Returns the argv with the program name first.
func (pc *prep) expand(name string) ([]string, *Error) {
	argv := []string{name}

	for i, raw := range pc.e.Run[1:] {
		elems, err := ExpandArgv(pc.tmpl[raw], pc.vals, pc.allow)
		if err != nil {
			return nil, err
		}

		if err := pc.dashdash(name, i+1, raw); err != nil {
			return nil, err
		}

		argv = append(argv, elems...)
	}

	if len(argv) > MaxArgvElems {
		return nil, newError(CodeProducedInvalid, "argv", "count")
	}

	return argv, nil
}

// programName checks element 0 of the run template: a literal word, covered by an exec:
// permission, never tote and never a shell or launcher.
func (pc *prep) programName() (string, *Error) {
	t := pc.tmpl[pc.e.Run[0]]

	name, literal := t.Literal()
	if !literal || !execNameRe.MatchString(name) {
		return "", newError(CodeArgv0Invalid, name)
	}

	host := pc.r.opts.Host

	switch {
	case name == "tote" || slices.Contains(host.Self, name):
		return "", newError(CodeExecSelf, name)
	case IsShellName(name):
		return "", newError(CodeArgvShell, name)
	case !slices.Contains(pc.p.Manifest.Permissions, PermExec+":"+name):
		return "", newError(CodeExecUncovered, name)
	}

	return name, nil
}

// dashdash repeats the lint rule for ssh, scp, rsync and sftp: an element that holds a
// placeholder must come after a literal "--".
//
// Parameters:
//   - name: program
//   - index: position of the element in the run template
//   - raw: its template text
func (pc *prep) dashdash(name string, index int, raw string) *Error {
	if !slices.Contains(sshFamily, name) || len(pc.tmpl[raw].Placeholders()) == 0 {
		return nil
	}

	for _, prev := range pc.e.Run[1:index] {
		if lit, ok := pc.tmpl[prev].Literal(); ok && lit == "--" {
			return nil
		}
	}

	return newError(CodeArgvSSHDashDash, name)
}

// directory finds the working directory: the cwd key, else the repository for a repository
// action and tote's own directory otherwise. It must be an absolute, existing directory.
func (pc *prep) directory() (string, *Error) {
	src := pc.e.Cwd

	switch {
	case src != "":
	case pc.e.Target == "repo":
		src = "{repo.path}"
	default:
		src = "{cwd}"
	}

	t, ok := pc.tmpl[src]
	if !ok {
		t, _ = ParseTemplate(src)
	}

	dir, keep, err := ExpandScalar(t, pc.vals, maxCwdBytes)
	if err != nil {
		return "", err
	}

	if !keep || !filepath.IsAbs(dir) {
		return "", newError(CodeCwdInvalid, dir)
	}

	dir = filepath.Clean(dir)

	if info, serr := os.Stat(dir); serr != nil || !info.IsDir() {
		return "", newError(CodeCwdInvalid, dir)
	}

	return dir, nil
}

// missing builds the error for a program that is not installed, with the hint of the manifest
// when it declares the program in a [[requires]].
//
// Parameters:
//   - name: program name
func (pc *prep) missing(name string) *Error {
	hint := ""

	for _, rq := range pc.p.Manifest.Requires {
		if rq.Bin == name {
			hint = pc.p.Catalog.Text(pc.call.Lang, "requires."+name+".hint", rq.Hint)
		}
	}

	return newError(CodeDepMissing, name, hint)
}

// environment builds the environment of the run.
//
// Parameters:
//   - out: prepared run being filled
func (pc *prep) environment(out *Prepared) *Error {
	table := map[string]string{}

	for _, name := range sortedKeys(pc.e.Env) {
		v, keep, err := ExpandScalar(pc.tmpl[pc.e.Env[name]], pc.vals, MaxArgvElemBytes)
		if err != nil {
			return err
		}

		if keep {
			table[name] = v
		}
	}

	plan := envPlan{
		Table: table, Lang: pc.call.Lang, Captured: out.Output != OutputTerminal,
		Mono: pc.call.Mono,
	}

	out.Env, out.secrets = pc.r.buildEnv(pc.p, pc.ctx.pathEnv, pc.avoid, plan)

	return nil
}

// pauseOf gives the pause policy of a terminal run: always unless the manifest says otherwise.
//
// Parameters:
//   - e: contribution
func pauseOf(e *Entry) string {
	if e.Output != OutputTerminal {
		return ""
	}

	if e.Pause != "" {
		return e.Pause
	}

	return PauseAlways
}

// timeoutOf gives the time limit of a captured run: the timeout key or 5 minutes. A terminal run
// has none.
//
// Parameters:
//   - e: contribution
func timeoutOf(e *Entry) time.Duration {
	if e.Output == OutputTerminal {
		return 0
	}

	if d, ok := ParseTimeout(e.Timeout); ok {
		return d
	}

	return DefaultEntryTime
}
