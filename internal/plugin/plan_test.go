package plugin

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// toolBody is a plugin of the sandbox: commands that run "tool" with every kind of field.
const toolBody = `
[[command]]
id = "echo"
label = "Echo"
run = ["tool", "{text}", "{tag?}", "{view?}", "{flag?}"]
output = "pager"
[[command.field]]
id = "text"
type = "text"
label = "Text"
required = true
max_len = 10
pattern = "^[a-z0-9-]+$"
[[command.field]]
id = "tag"
type = "text"
label = "Tag"
pattern = "^[a-z0-9.-]{0,16}$"
[[command.field]]
id = "view"
type = "choice"
label = "View"
choices = ["", "--assets", "-x"]
[[command.field]]
id = "flag"
type = "bool"
label = "Flag"
flag = "--checksum"

[[command]]
id = "ask"
label = "Ask"
run = ["tool", "go"]
confirm = "Run {text}?"
output = "silent"
[[command.field]]
id = "text"
type = "text"
label = "Text"
default = "none"

[[command]]
id = "dashy"
label = "Dashy"
run = ["tool", "{text}", "{{text}}"]
allow_dash = ["text"]
output = "pager"
[[command.field]]
id = "text"
type = "text"
label = "Text"
required = true
`

// toolLab installs the tool plugin and its program and returns the sandbox and a registry.
//
// Parameters:
//   - t: test to report to
func toolLab(t *testing.T) (*lab, *Registry) {
	t.Helper()

	l := newLab(t)
	l.useSystemTools()
	l.script("tool", "exit 0\n")
	l.plug("demo", []string{"exec:tool"}, toolBody)

	return l, l.reg(nil)
}

// TestPrepareBasic checks the result of a normal call: the absolute program, the elements after
// substitution with the optional ones dropped, the display, the directory, the limits and the
// bindings to the tree.
//
// Parameters:
//   - t: test to report to
func TestPrepareBasic(t *testing.T) {
	l, r := toolLab(t)

	p := prepare(t, r, Call{
		Plugin: "demo", Table: TableCommand, Entry: "echo",
		Values: map[string]string{"text": "hello"},
	})

	if want := []string{filepath.Join(l.bin, "tool"), "hello"}; strings.Join(p.Argv, "|") !=
		strings.Join(want, "|") {
		t.Errorf("argv %q want %q", p.Argv, want)
	}

	checks := map[string][2]string{
		"name":    {p.Name, "tool"},
		"display": {p.Display, "tool hello"},
		"dir":     {p.Dir, l.cwd()},
		"output":  {p.Output, OutputPager},
		"plugin":  {p.PluginID, "demo"},
		"entry":   {p.EntryID, "echo"},
		"timeout": {p.Timeout.String(), (5 * time.Minute).String()},
		"pause":   {p.Pause, ""},
	}

	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %q want %q", name, c[0], c[1])
		}
	}

	if p.Confirm.Required || p.Confirmed || p.IsOpen() || p.IsTerminal() {
		t.Errorf("unexpected flags: %+v", p)
	}

	if len(p.Env) == 0 {
		t.Error("no environment")
	}
}

// TestPrepareFields checks the validation of the submitted values, in the order of the
// specification: required, max_len, pattern, then the type rules.
//
// Parameters:
//   - t: test to report to
func TestPrepareFields(t *testing.T) {
	_, r := toolLab(t)

	tests := []struct {
		name   string
		values map[string]string
		code   string
		arg    string
		rule   string
	}{
		{"missing required", map[string]string{}, CodeInputInvalid, "text", "required"},
		{"empty required", map[string]string{"text": ""}, CodeInputInvalid, "text", "required"},
		{"too long", map[string]string{"text": "abcdefghijk"}, CodeInputInvalid, "text", "max_len"},
		{"pattern", map[string]string{"text": "ABC"}, CodeInputInvalid, "text", "pattern"},
		{
			"pattern wins over nothing",
			map[string]string{"text": "a b"},
			CodeInputInvalid, "text",
			"pattern",
		},
		{"newline", map[string]string{"text": "a\nb"}, CodeInputInvalid, "text", "control"},
		{"nul", map[string]string{"text": "a\x00"}, CodeInputInvalid, "text", "control"},
		{"escape", map[string]string{"text": "\x1b[0m"}, CodeInputInvalid, "text", "control"},
		{
			"huge",
			map[string]string{"text": strings.Repeat("a", 10<<20)},
			CodeInputInvalid, "text",
			"max_len",
		},
		{
			"unknown field",
			map[string]string{"text": "a", "nope": "1"},
			CodeInputInvalid, "nope",
			"unknown",
		},
		{
			"tag pattern",
			map[string]string{"text": "a", "tag": "UPPER"},
			CodeInputInvalid, "tag",
			"pattern",
		},
		{
			"choice outside",
			map[string]string{"text": "a", "view": "--evil"},
			CodeInputInvalid, "view",
			"choice",
		},
		{
			"bool nonsense",
			map[string]string{"text": "a", "flag": "maybe"},
			CodeInputInvalid, "flag",
			"bool",
		},
		{"dash refused", map[string]string{"text": "-rf"}, CodeDashRefused, "text", ""},
		{"dash tag refused", map[string]string{"text": "a", "tag": "-rf"}, CodeDashRefused, "tag", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := refusal(t, r, Call{
				Plugin: "demo", Table: TableCommand, Entry: "echo",
				Values: tt.values,
			})

			if e.Code != tt.code || e.Args[0] != tt.arg || (tt.rule != "" && e.Args[1] != tt.rule) {
				t.Errorf("got %s %v want %s %s %s", e.Code, e.Args, tt.code, tt.arg, tt.rule)
			}

			if ExitOf(e) != ExitUsage {
				t.Errorf("exit %d want %d", ExitOf(e), ExitUsage)
			}
		})
	}
}

// TestPrepareTypes checks the values that pass: a choice that starts with a dash, the flag of a
// bool, an optional field left empty, and the usual words for a bool.
//
// Parameters:
//   - t: test to report to
func TestPrepareTypes(t *testing.T) {
	l, r := toolLab(t)
	tool := filepath.Join(l.bin, "tool")

	tests := []struct {
		name   string
		values map[string]string
		want   []string
	}{
		{
			"choice with a dash",
			map[string]string{"text": "a", "view": "--assets"},
			[]string{tool, "a", "--assets"},
		},
		{"choice dash x", map[string]string{"text": "a", "view": "-x"}, []string{tool, "a", "-x"}},
		{"bool on", map[string]string{"text": "a", "flag": "true"}, []string{tool, "a", "--checksum"}},
		{"bool yes", map[string]string{"text": "a", "flag": "yes"}, []string{tool, "a", "--checksum"}},
		{"bool off", map[string]string{"text": "a", "flag": "false"}, []string{tool, "a"}},
		{
			"everything",
			map[string]string{"text": "a", "tag": "v1.2", "view": "--assets", "flag": "1"},
			[]string{tool, "a", "v1.2", "--assets", "--checksum"},
		},
		{"max length", map[string]string{"text": "abcdefghij"}, []string{tool, "abcdefghij"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := prepare(t, r, Call{
				Plugin: "demo", Table: TableCommand, Entry: "echo",
				Values: tt.values,
			})

			if strings.Join(p.Argv, "|") != strings.Join(tt.want, "|") {
				t.Errorf("argv %q want %q", p.Argv, tt.want)
			}
		})
	}
}

// TestPrepareAllowDash checks that allow_dash lets a value through for the placeholder it names,
// and that doubled braces stay literal in the element after it.
//
// Parameters:
//   - t: test to report to
func TestPrepareAllowDash(t *testing.T) {
	_, r := toolLab(t)

	p := prepare(t, r, Call{
		Plugin: "demo", Table: TableCommand, Entry: "dashy",
		Values: map[string]string{"text": "-rf"},
	})

	if got := p.Argv[1:]; len(got) != 2 || got[0] != "-rf" || got[1] != "{text}" {
		t.Errorf("argv %q", p.Argv)
	}
}

// TestPrepareConfirm checks the confirmation: the text is expanded for display, the generic
// question has no text, and Run does not start before it was given.
//
// Parameters:
//   - t: test to report to
func TestPrepareConfirm(t *testing.T) {
	_, r := toolLab(t)

	p := prepare(t, r, Call{
		Plugin: "demo", Table: TableCommand, Entry: "ask",
		Values: map[string]string{"text": "now"},
	})

	if !p.Confirm.Required || p.Confirm.Text != "Run now?" || p.Confirmed {
		t.Errorf("confirm %+v confirmed %v", p.Confirm, p.Confirmed)
	}

	p = prepare(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "ask", Yes: true})

	if !p.Confirmed || p.Confirm.Text != "Run none?" {
		t.Errorf("confirm %+v confirmed %v", p.Confirm, p.Confirmed)
	}
}

// TestPrepareNoEntry checks the refusals of a call that names nothing real.
//
// Parameters:
//   - t: test to report to
func TestPrepareNoEntry(t *testing.T) {
	_, r := toolLab(t)

	tests := []struct {
		name string
		call Call
		code string
	}{
		{"unknown entry", Call{Plugin: "demo", Table: TableCommand, Entry: "nope"}, CodeNoEntry},
		{"unknown table", Call{Plugin: "demo", Table: "widget", Entry: "echo"}, CodeNoEntry},
		{"exec table", Call{Plugin: "demo", Table: "action", Entry: "echo"}, CodeUnsupported},
		{"list table", Call{Plugin: "demo", Table: "list", Entry: "echo"}, CodeUnsupported},
		{"list action without list", Call{
			Plugin: "demo", Table: TableListAction, List: "x",
			Entry: "y",
		}, CodeNoEntry},
		{
			"unknown plugin",
			Call{Plugin: "ghost", Table: TableCommand, Entry: "echo"},
			CodeNotRunnable,
		},
		{"bad id", Call{Plugin: "../data", Table: TableCommand, Entry: "echo"}, CodeBadID},
		{"empty id", Call{Table: TableCommand, Entry: "echo"}, CodeBadID},
		{"id with a slash", Call{Plugin: "a/b", Table: TableCommand, Entry: "echo"}, CodeBadID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if e := refusal(t, r, tt.call); e.Code != tt.code {
				t.Errorf("code %s (%v) want %s", e.Code, e.Args, tt.code)
			}
		})
	}
}

// TestDefaults checks the initial values of a form: the default templates expanded in order.
//
// Parameters:
//   - t: test to report to
func TestDefaults(t *testing.T) {
	_, r := toolLab(t)

	got, err := r.Defaults(Call{Plugin: "demo", Table: TableCommand, Entry: "ask"})
	if err != nil || got["text"] != "none" || len(got) != 1 {
		t.Errorf("got %v, %v", got, err)
	}

	if _, err := r.Defaults(Call{Plugin: "ghost", Table: TableCommand, Entry: "ask"}); err == nil {
		t.Error("Defaults accepted a plugin that does not exist")
	}
}

// TestProgramChecks checks the defences of Prepare that a validated manifest never reaches:
// element 0 must be a literal covered by an exec: permission, never tote, never a shell, and an
// ssh-family program needs a literal "--" before any placeholder.
//
// Parameters:
//   - t: test to report to
func TestProgramChecks(t *testing.T) {
	m := &Manifest{Permissions: []string{"exec:git", "exec:ssh", "exec:sh", "exec:tote", "exec:mine"}}
	host := testHost()

	tests := []struct {
		name string
		run  []string
		code string
	}{
		{"covered", []string{"git", "status"}, ""},
		{"uncovered", []string{"curl"}, CodeExecUncovered},
		{"tote", []string{"tote"}, CodeExecSelf},
		{"self by TOTE_BIN", []string{"tote-test"}, CodeExecSelf},
		{"shell", []string{"sh", "-c", "x"}, CodeArgvShell},
		{"placeholder program", []string{"{x}"}, CodeArgv0Invalid},
		{"path program", []string{"/bin/git"}, CodeArgv0Invalid},
		{"empty program", []string{""}, CodeArgv0Invalid},
		{"ssh without dashdash", []string{"ssh", "{x}"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &prep{
				r: Open(Options{Host: host}), p: Plugin{Manifest: m},
				e: &Entry{Run: tt.run}, tmpl: map[string]Template{},
			}

			for _, s := range tt.run {
				if tm, perr := ParseTemplate(s); perr == nil {
					pc.tmpl[s] = tm
				}
			}

			_, err := pc.programName()

			switch {
			case tt.code == "" && err != nil:
				t.Errorf("refused: %v", err)
			case tt.code != "" && (err == nil || err.Code != tt.code):
				t.Errorf("got %v want %s", err, tt.code)
			}
		})
	}

	t.Run("dashdash", func(t *testing.T) {
		for _, tc := range []struct {
			run  []string
			code string
		}{
			{[]string{"ssh", "-p", "22", "--", "{host}"}, ""},
			{[]string{"ssh", "{host}"}, CodeArgvSSHDashDash},
			{[]string{"ssh", "{host}", "--"}, CodeArgvSSHDashDash},
			{[]string{"scp", "-r", "--", "{a}", "{b}"}, ""},
			{[]string{"git", "{host}"}, ""},
		} {
			pc := &prep{e: &Entry{Run: tc.run}, tmpl: map[string]Template{}}

			for _, s := range tc.run {
				pc.tmpl[s], _ = ParseTemplate(s)
			}

			var err *Error

			for i := 1; i < len(tc.run) && err == nil; i++ {
				err = pc.dashdash(tc.run[0], i, tc.run[i])
			}

			if (err == nil) != (tc.code == "") || (err != nil && err.Code != tc.code) {
				t.Errorf("%v: got %v want %q", tc.run, err, tc.code)
			}
		}
	})
}

// TestCheckField checks the field rules on their own, including the case where a pattern does
// not compile: it matches nothing.
//
// Parameters:
//   - t: test to report to
func TestCheckField(t *testing.T) {
	f := &Field{ID: "n", Type: "text", MaxLen: 4, Pattern: "^[0-9]+$"}

	if _, err := checkField(f, "123"); err != nil {
		t.Errorf("valid refused: %v", err)
	}

	if _, err := checkField(f, ""); err != nil {
		t.Errorf("an empty optional field must pass: %v", err)
	}

	if _, err := checkField(f, "12345"); err == nil || err.Args[1] != "max_len" {
		t.Errorf("long: %v", err)
	}

	// The length is counted in runes, not bytes.
	g := &Field{ID: "n", Type: "text", MaxLen: 3}
	if _, err := checkField(g, "\u00e9\u00e9\u00e9"); err != nil {
		t.Errorf("three runes refused: %v", err)
	}

	bad := &Field{ID: "n", Type: "text", MaxLen: 10, Pattern: "(["}
	if _, err := checkField(bad, "x"); err == nil {
		t.Error("a pattern that does not compile matched")
	}

	req := &Field{ID: "n", Type: "choice", Choices: []string{"a", ""}, Required: true}
	if _, err := checkField(req, ""); err == nil {
		t.Error("an empty required choice passed")
	}
}
