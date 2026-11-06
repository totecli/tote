package plugin

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// reservedAccelerators lists the menu keys that the core uses. A plugin that claims one gets a
// warning: the core key always wins.
const reservedAccelerators = "qjkhlg"

// entryCtx carries what the checks of one contribution share: the scope of its placeholders, the
// names its run templates use, and where it first read the current repository.
type entryCtx struct {
	e        *Entry
	scope    Scope
	runNames map[string]bool
	repoAt   string
}

// scopeFor builds the scope of the templates of a contribution: its own fields, every setting,
// and the row of a list when the contribution is a list action.
//
// Parameters:
//   - m: manifest
//   - e: contribution
//   - item: true for a [[list.action]]
func scopeFor(m *Manifest, e *Entry, item bool) Scope {
	s := Scope{Fields: map[string]string{}, Settings: map[string]string{}, Item: item}

	for _, f := range e.Fields {
		s.Fields[f.ID] = f.Type
	}

	for _, st := range m.Settings {
		s.Settings[st.ID] = st.Type
	}

	return s
}

// contributions checks every contribution table.
func (v *validator) contributions() {
	m := v.m

	v.table(m.Commands, "command")
	v.table(m.ItemActions, "item_action")
	v.table(m.Actions, "action")
	v.keyClashes(m.ItemActions)

	ids := map[string]bool{}

	for i := range m.Lists {
		v.list(&m.Lists[i], ids)
	}
}

// table checks the contributions of one table and the uniqueness of their ids.
//
// Parameters:
//   - entries: contributions in file order
//   - table: name of the table
func (v *validator) table(entries []Entry, table string) {
	seen := map[string]bool{}

	for i := range entries {
		e := &entries[i]

		v.contributionID(e.Path+".id", e.ID, seen)
		v.entry(e, table)
	}
}

// contributionID checks the grammar and the uniqueness of a contribution id.
//
// Parameters:
//   - path: issue path of the id
//   - id: id as written, empty when it was not read
//   - seen: ids already used in the same table; updated here
func (v *validator) contributionID(path, id string, seen map[string]bool) {
	if id == "" {
		return
	}

	if !contributionIDRe.MatchString(id) {
		v.c.add(CodeIDInvalid, path, id, "contribution")

		return
	}

	if seen[id] {
		v.c.add(CodeIDDuplicate, path, id)
	}

	seen[id] = true
}

// keyClashes warns when two item actions claim the same accelerator: the first one wins.
//
// Parameters:
//   - entries: item actions in file order
func (v *validator) keyClashes(entries []Entry) {
	seen := map[string]bool{}

	for _, e := range entries {
		if e.Key == "" {
			continue
		}

		if seen[e.Key] {
			v.c.warn(CodeKeyClash, e.Path+".key", e.Key)
		}

		seen[e.Key] = true
	}
}

// list checks one [[list]] and its columns and actions.
//
// Parameters:
//   - l: list
//   - ids: ids of the lists already seen; updated here
func (v *validator) list(l *List, ids map[string]bool) {
	v.contributionID(l.Path+".id", l.ID, ids)

	cols := map[string]bool{}

	for i, col := range l.Columns {
		path := l.Path + ".column[" + itoa(i) + "]"

		if col.ID == "" {
			continue
		}

		if !snakeIDRe.MatchString(col.ID) {
			v.c.add(CodeIDInvalid, path+".id", col.ID, "snake")
		} else if cols[col.ID] {
			v.c.add(CodeIDDuplicate, path+".id", col.ID)
		}

		cols[col.ID] = true
	}

	actions := map[string]bool{}

	for i := range l.Actions {
		a := &l.Actions[i]

		v.contributionID(a.Path+".id", a.ID, actions)
		v.entry(a, "list.action")
	}
}

// entry checks one contribution.
//
// Parameters:
//   - e: contribution
//   - table: name of the table it came from
func (v *validator) entry(e *Entry, table string) {
	ctx := &entryCtx{e: e, runNames: map[string]bool{}}

	v.fields(ctx)

	ctx.scope = scopeFor(v.m, e, table == "list.action")

	v.shape(ctx, table)
	v.argv(ctx)
	v.open(ctx)
	v.envTable(ctx)
	v.scalarTemplate(ctx, e.Path+".cwd", e.Cwd)
	v.confirmText(ctx)
	v.allowDash(ctx)
	v.accelerator(e)
	v.repoPermission(ctx)
}

// shape checks the keys of a contribution against each other: run or open, output, pause and
// timeout.
//
// Parameters:
//   - ctx: contribution under check
//   - table: name of the table
func (v *validator) shape(ctx *entryCtx, table string) {
	e := ctx.e
	hasRun, hasOpen := e.Run != nil, e.Open != ""

	switch {
	case table == "action":
		v.requireKey(e, e.Output == "", "output")
		v.conflict(e, e.Pause != "", "pause", table)
	case hasRun && hasOpen:
		v.c.add(CodeKeyConflict, e.Path+".open", "run", "open")
	case hasRun:
		v.requireKey(e, e.Output == "", "output")
		v.conflict(e, e.Pause != "" && e.Output != "terminal", "pause", e.Output)
		v.conflict(e, e.Timeout != "" && e.Output == "terminal", "timeout", e.Output)

		if len(e.Run) == 0 {
			v.c.add(CodeValueInvalid, e.Path+".run", "empty")
		}
	case hasOpen:
		v.openExclusive(e)
	default:
		v.requireKey(e, true, "run")
	}

	if table == "item_action" {
		v.requireKey(e, e.Target == "", "target")
	}
}

// requireKey reports a missing key, unless the contribution already has an issue about its
// reading, which would make the message noise.
//
// Parameters:
//   - e: contribution
//   - missing: true when the key is absent
//   - key: key name
func (v *validator) requireKey(e *Entry, missing bool, key string) {
	if missing && !e.broken {
		v.c.add(CodeKeyMissing, e.Path+"."+key, key)
	}
}

// conflict reports a key that cannot go with another.
//
// Parameters:
//   - e: contribution
//   - bad: true when the combination is wrong
//   - key: key that is out of place
//   - other: what it conflicts with
func (v *validator) conflict(e *Entry, bad bool, key, other string) {
	if bad {
		v.c.add(CodeKeyConflict, e.Path+"."+key, key, other)
	}
}

// openExclusive reports the keys that cannot go with open: no program runs, so nothing that
// configures a program is accepted.
//
// Parameters:
//   - e: contribution that has open
func (v *validator) openExclusive(e *Entry) {
	v.conflict(e, e.Cwd != "", "cwd", "open")
	v.conflict(e, len(e.Env) > 0, "env", "open")
	v.conflict(e, e.Output != "", "output", "open")
	v.conflict(e, e.Pause != "", "pause", "open")
	v.conflict(e, e.Timeout != "", "timeout", "open")
	v.conflict(e, len(e.AllowDash) > 0, "allow_dash", "open")
}

// argv checks the run template of a contribution.
//
// Parameters:
//   - ctx: contribution under check
func (v *validator) argv(ctx *entryCtx) {
	e := ctx.e
	if len(e.Run) == 0 {
		return
	}

	parsed := make([]Template, len(e.Run))
	ok := make([]bool, len(e.Run))

	for i, raw := range e.Run {
		path := e.Path + ".run[" + itoa(i) + "]"
		parsed[i], ok[i] = v.template(ctx, path, raw, true)
	}

	if !ok[0] {
		return
	}

	prog := v.program(ctx, parsed[0])
	if prog == "" {
		return
	}

	v.lintArgv(e, prog, parsed, ok)

	for _, t := range parsed {
		for _, p := range t.Placeholders() {
			ctx.runNames[p.Name] = true
		}
	}
}

// program checks the program at argv[0]: a literal word, never tote, never a shell, and covered
// by an exec: permission.
//
// Parameters:
//   - ctx: contribution under check
//   - t: parsed run[0]
//
// Returns the program name, or an empty string when it is refused.
func (v *validator) program(ctx *entryCtx, t Template) string {
	path := ctx.e.Path + ".run[0]"

	name, literal := t.Literal()
	if !literal || !execNameRe.MatchString(name) {
		v.c.add(CodeArgv0Invalid, path, name)

		return ""
	}

	switch {
	case v.isSelf(name):
		v.c.add(CodeExecSelf, path, name)

		return ""
	case IsShellName(name):
		v.c.add(CodeArgvShell, path, name)

		return ""
	case !v.perms[PermExec+":"+name]:
		v.c.add(CodeExecUncovered, path, name)

		return ""
	}

	v.execUsed[name] = true

	return name
}

// lintArgv applies the lint rules of 3.3 and 3.5 to the elements after argv[0]: risky options and
// the literal "--" that ssh-family tools need before any placeholder.
//
// Parameters:
//   - e: contribution
//   - prog: program at argv[0]
//   - parsed: parsed elements
//   - ok: for each element, whether it parsed
func (v *validator) lintArgv(e *Entry, prog string, parsed []Template, ok []bool) {
	dashdash := -1

	for i := 1; i < len(parsed); i++ {
		if !ok[i] {
			continue
		}

		path := e.Path + ".run[" + itoa(i) + "]"

		if lit, literal := parsed[i].Literal(); literal && lit == "--" && dashdash < 0 {
			dashdash = i
		}

		if len(parsed[i].Parts) > 0 && parsed[i].Parts[0].Holder == nil {
			if f := riskyFlag(prog, parsed[i].Parts[0].Text); f != "" {
				v.c.warn(CodeArgvRiskyFlag, path, prog, f)
			}
		}

		if inFamily(prog) && len(parsed[i].Placeholders()) > 0 && dashdash < 0 {
			v.c.add(CodeArgvSSHDashDash, path, prog)
		}
	}
}

// inFamily reports whether a program is ssh, scp, rsync or sftp.
//
// Parameters:
//   - prog: program name
func inFamily(prog string) bool {
	for _, p := range sshFamily {
		if p == prog {
			return true
		}
	}

	return false
}

// open checks the open template of a contribution: it must be an https URL, and the plugin needs
// host:open-url.
//
// Parameters:
//   - ctx: contribution under check
func (v *validator) open(ctx *entryCtx) {
	e := ctx.e
	if e.Open == "" {
		return
	}

	path := e.Path + ".open"

	if !v.perms[PermOpenURL] {
		v.c.add(CodePermMissing, path, PermOpenURL)
	}

	t, ok := v.template(ctx, path, e.Open, false)
	if !ok || len(t.Parts) == 0 || t.Parts[0].Holder != nil {
		return
	}

	lit := t.Parts[0].Text
	if !strings.HasPrefix(lit, "https://") || openHasUserinfo(lit) {
		v.c.add(CodeValueInvalid, path, "https")
	}
}

// openHasUserinfo reports whether the literal start of an https template carries user information
// before the host.
//
// Parameters:
//   - lit: literal text that starts with https://
func openHasUserinfo(lit string) bool {
	rest := strings.TrimPrefix(lit, "https://")
	end := strings.IndexAny(rest, "/?#")

	if end < 0 {
		return strings.Contains(rest, "@")
	}

	return strings.Contains(rest[:end], "@")
}

// envTable checks the env table of a contribution: names, permissions and templates.
//
// Parameters:
//   - ctx: contribution under check
func (v *validator) envTable(ctx *entryCtx) {
	e := ctx.e

	for _, name := range sortedKeys(e.Env) {
		path := e.Path + ".env." + name

		switch {
		case !envNameRe.MatchString(name):
			v.c.add(CodeValueInvalid, path, name)
		case BlockedEnvName(name):
			v.c.add(CodeEnvRefused, path, name)
		case !v.perms[PermEnv+":"+name]:
			v.c.add(CodeEnvUncovered, path, name)
		}

		v.scalarTemplate(ctx, path, e.Env[name])
	}
}

// scalarTemplate checks a template that must yield one value: cwd, an env value or a field
// default.
//
// Parameters:
//   - ctx: contribution under check
//   - path: issue path of the value
//   - raw: template text, empty when the key is absent
func (v *validator) scalarTemplate(ctx *entryCtx, path, raw string) {
	if raw != "" {
		v.template(ctx, path, raw, false)
	}
}

// confirmText checks the text of a confirmation, whose placeholders are only displayed.
//
// Parameters:
//   - ctx: contribution under check
func (v *validator) confirmText(ctx *entryCtx) {
	if ctx.e.Confirm.Text != "" {
		v.template(ctx, ctx.e.Path+".confirm", ctx.e.Confirm.Text, false)
	}
}

// allowDash checks that every name of allow_dash is a placeholder of the run template.
//
// Parameters:
//   - ctx: contribution under check
func (v *validator) allowDash(ctx *entryCtx) {
	e := ctx.e

	for i, name := range e.AllowDash {
		path := e.Path + ".allow_dash[" + itoa(i) + "]"

		switch {
		case !validDottedName(name):
			v.c.add(CodeValueInvalid, path, name)
		case !ctx.runNames[name]:
			v.c.add(CodePlaceholderUnknown, path, name)
		}
	}
}

// accelerator warns when a menu key is one of the core keys.
//
// Parameters:
//   - e: contribution
func (v *validator) accelerator(e *Entry) {
	if e.Key == "" {
		return
	}

	c := e.Key[0]
	if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
		v.c.add(CodeValueInvalid, e.Path+".key", e.Key)

		return
	}

	if strings.IndexByte(reservedAccelerators, c) >= 0 {
		v.c.warn(CodeKeyReserved, e.Path+".key", e.Key)
	}
}

// repoPermission checks that a contribution that reads the repository declares host:read:repos.
//
// Parameters:
//   - ctx: contribution under check
func (v *validator) repoPermission(ctx *entryCtx) {
	if v.perms[PermReadRepos] {
		return
	}

	if ctx.e.Target == "repo" {
		v.c.add(CodePermMissing, ctx.e.Path+".target", PermReadRepos)
	}

	if ctx.repoAt != "" {
		v.c.add(CodePermMissing, ctx.repoAt, PermReadRepos)
	}
}

// template parses a template, checks its placeholders against the scope and notes what it uses.
//
// Parameters:
//   - ctx: contribution under check
//   - path: issue path of the template
//   - raw: template text
//   - argv: true for an argv element, where a list setting may stand alone
//
// Returns the parsed template and true, or false after recording the issue.
func (v *validator) template(ctx *entryCtx, path, raw string, argv bool) (Template, bool) {
	t, terr := ParseTemplate(raw)
	if terr != nil {
		code := CodePlaceholderSyntax
		if terr.Reason == "unknown" {
			code = CodePlaceholderUnknown
		}

		v.c.add(code, path, terr.Reason)

		return Template{}, false
	}

	good := true

	for _, p := range t.Placeholders() {
		if code := ctx.scope.Check(p); code != "" {
			v.c.add(code, path, p.Name)

			good = false

			continue
		}

		if p.Kind == KindRepo && ctx.repoAt == "" {
			ctx.repoAt = path
		}

		if p.Kind == KindSetting && ctx.scope.Settings[p.Member] == "list" {
			if _, whole := t.Whole(); !argv || !whole {
				v.c.add(CodePlaceholderList, path, p.Name)

				good = false
			}
		}
	}

	return t, good
}

// fieldSet is the state of the checks over the fields of one contribution.
type fieldSet struct {
	seen  map[string]bool
	scope Scope
}

// fields checks the form fields of a contribution in file order, so that a default sees only the
// fields declared before it.
//
// Parameters:
//   - ctx: contribution under check
func (v *validator) fields(ctx *entryCtx) {
	set := &fieldSet{seen: map[string]bool{}, scope: Scope{
		Fields:   map[string]string{},
		Settings: v.settings,
	}}

	for i := range ctx.e.Fields {
		f := &ctx.e.Fields[i]

		v.fieldID(f, set)

		switch f.Type {
		case "text":
			v.textField(ctx, f, set)
		case "choice":
			v.choiceField(ctx, f, set)
		case "bool":
			v.boolField(f)
		}

		if f.ID != "" {
			set.scope.Fields[f.ID] = f.Type
		}
	}
}

// fieldID checks the grammar and the uniqueness of a field id.
//
// Parameters:
//   - f: field
//   - set: state of the fields of the contribution
func (v *validator) fieldID(f *Field, set *fieldSet) {
	if f.ID == "" {
		return
	}

	switch {
	case !snakeIDRe.MatchString(f.ID):
		v.c.add(CodeIDInvalid, f.Path+".id", f.ID, "snake")
	case containsString(reservedFieldIDs, f.ID):
		v.c.add(CodeIDInvalid, f.Path+".id", f.ID, "reserved")
	case set.seen[f.ID]:
		v.c.add(CodeIDDuplicate, f.Path+".id", f.ID)
	}

	set.seen[f.ID] = true
}

// containsString reports whether a list holds a string.
//
// Parameters:
//   - list: strings to search
//   - s: string to find
func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}

	return false
}

// textField checks a text field: its pattern and its default.
//
// Parameters:
//   - ctx: contribution under check
//   - f: field
//   - set: state of the fields of the contribution
func (v *validator) textField(ctx *entryCtx, f *Field, set *fieldSet) {
	re := v.pattern(f.Path+".pattern", f.Pattern)

	if !f.HasDefault {
		return
	}

	t, ok := v.defaultTemplate(ctx, f, set)
	if !ok {
		return
	}

	lit, literal := t.Literal()
	if !literal {
		return
	}

	if utf8.RuneCountInString(lit) > f.MaxLen || (re != nil && lit != "" && !re.MatchString(lit)) {
		v.c.add(CodeValueInvalid, f.Path+".default", lit)
	}
}

// defaultTemplate checks the default of a text or choice field against the scope of the fields
// declared before it.
//
// Parameters:
//   - ctx: contribution under check
//   - f: field
//   - set: state of the fields of the contribution
func (v *validator) defaultTemplate(ctx *entryCtx, f *Field, set *fieldSet) (Template, bool) {
	saved := ctx.scope
	ctx.scope = set.scope

	defer func() { ctx.scope = saved }()

	return v.template(ctx, f.Path+".default", f.Default, false)
}

// choiceField checks a choice field: its values and its default.
//
// Parameters:
//   - ctx: contribution under check
//   - f: field
//   - set: state of the fields of the contribution
func (v *validator) choiceField(ctx *entryCtx, f *Field, set *fieldSet) {
	switch {
	case f.Choices == nil:
		v.fieldKey(f, "choices")
	case len(f.Choices) == 0:
		v.c.add(CodeValueInvalid, f.Path+".choices", "empty")
	}

	seen := map[string]bool{}

	for i, ch := range f.Choices {
		if seen[ch] {
			v.c.add(CodeValueInvalid, f.Path+".choices["+itoa(i)+"]", ch, "duplicate")
		}

		seen[ch] = true
	}

	if !f.HasDefault {
		return
	}

	t, ok := v.defaultTemplate(ctx, f, set)
	if !ok {
		return
	}

	if lit, literal := t.Literal(); literal && !containsString(f.Choices, lit) {
		v.c.add(CodeValueInvalid, f.Path+".default", lit)
	}
}

// fieldKey reports a key that a field type requires and that is missing, unless the field already
// has an issue about its reading.
//
// Parameters:
//   - f: field
//   - key: key name
func (v *validator) fieldKey(f *Field, key string) {
	if !f.broken {
		v.c.add(CodeKeyMissing, f.Path+"."+key, key)
	}
}

// boolField checks a bool field: its flag.
//
// Parameters:
//   - f: field
func (v *validator) boolField(f *Field) {
	if f.Flag != "" && (!strings.HasPrefix(f.Flag, "-") || strings.ContainsAny(f.Flag, " \t")) {
		v.c.add(CodeValueInvalid, f.Path+".flag", f.Flag)
	}
}

// compiledPattern is the verdict on one pattern: the compiled expression, nil when it does not
// compile, and whether every alternative is anchored.
type compiledPattern struct {
	re       *regexp.Regexp
	anchored bool
}

// pattern compiles a pattern and checks that it is anchored. A manifest repeats the same few
// patterns, so each distinct one is compiled once per manifest.
//
// Parameters:
//   - path: issue path of the pattern
//   - pat: expression, empty when the key is absent
//
// Returns the compiled expression, or nil when it is absent or does not compile.
func (v *validator) pattern(path, pat string) *regexp.Regexp {
	if pat == "" {
		return nil
	}

	cp, known := v.patterns[pat]
	if !known {
		cp.re, _ = regexp.Compile(pat)
		cp.anchored = cp.re != nil && anchoredPattern(pat)

		if v.patterns == nil {
			v.patterns = map[string]compiledPattern{}
		}

		v.patterns[pat] = cp
	}

	if cp.re == nil {
		v.c.add(CodeValueInvalid, path, "regexp")

		return nil
	}

	if !cp.anchored {
		v.c.add(CodePatternUnanchored, path, pat)
	}

	return cp.re
}
