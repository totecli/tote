package plugin

import (
	"slices"
	"strings"
	"testing"
)

// cfField builds a text field with a choices_from argv.
//
// Parameters:
//   - id: field id
//   - argv: TOML array body of choices_from, such as "\"tool\", \"{a}\""
//   - extra: more key lines of the field
func cfField(id, argv, extra string) string {
	return "[[command.field]]\nid = \"" + id + "\"\ntype = \"text\"\nlabel = \"F\"\n" +
		"choices_from = [" + argv + "]\n" + extra
}

// plainField builds a text field with no suggestions.
//
// Parameters:
//   - id: field id
func plainField(id string) string {
	return "[[command.field]]\nid = \"" + id + "\"\ntype = \"text\"\nlabel = \"F\"\n"
}

// TestRulesChoicesFrom checks the validation of the choices_from argv of a text field: the scope
// of its placeholders, the program rules of a run and the lint rules.
//
// Parameters:
//   - t: test to report to
func TestRulesChoicesFrom(t *testing.T) {
	in := func(perms []string, body string) string { return decl("", perms, body) }
	setting := "[[setting]]\nid = \"s\"\ntype = \"string\"\nlabel = \"S\"\n"
	listSetting := "[[setting]]\nid = \"s\"\ntype = \"list\"\nlabel = \"S\"\n"
	at := "command[0].field[1].choices_from"

	tests := []ruleCase{
		{
			name: "own field is unknown",
			src:  in(tool, cmd(run(""))+cfField("x", "\"tool\", \"{x}\"", "")),
			code: CodePlaceholderUnknown, path: "command[0].field[0].choices_from[1]",
		},
		{
			name: "later field is unknown",
			src: in(tool, cmd(run(""))+cfField("a", "\"tool\", \"{b}\"", "")+
				plainField("b")),
			code: CodePlaceholderUnknown, path: "command[0].field[0].choices_from[1]",
		},
		{
			name: "unknown name",
			src:  in(tool, cmd(run(""))+cfField("a", "\"tool\", \"{nope?}\"", "")),
			code: CodePlaceholderUnknown, path: "command[0].field[0].choices_from[1]",
		},
		{
			name: "unknown setting",
			src:  in(tool, cmd(run(""))+cfField("a", "\"tool\", \"{setting.zz}\"", "")),
			code: CodePlaceholderUnknown,
		},
		{
			name: "item is out of scope",
			src:  in(tool, cmd(run(""))+cfField("a", "\"tool\", \"{item.id}\"", "")),
			code: CodePlaceholderScope, path: "command[0].field[0].choices_from[1]",
		},
		{
			name: "list setting inside a text",
			src: in(tool, listSetting+cmd(run(""))+
				cfField("a", "\"tool\", \"--x={setting.s}\"", "")),
			code: CodePlaceholderList,
		},
		{
			name: "syntax",
			src:  in(tool, cmd(run(""))+cfField("a", "\"tool\", \"{\"", "")),
			code: CodePlaceholderSyntax, path: "command[0].field[0].choices_from[1]",
		},
		{
			name: "program is a placeholder",
			src: in(tool, cmd(run(""))+plainField("a")+
				cfField("b", "\"{a}\"", "")),
			code: CodeArgv0Invalid, path: at + "[0]",
		},
		{
			name: "program not covered",
			src:  in(tool, cmd(run(""))+cfField("a", "\"other\"", "")),
			code: CodeExecUncovered, path: "command[0].field[0].choices_from[0]",
		},
		{
			name: "program is tote",
			src:  in(tool, cmd(run(""))+cfField("a", "\"tote\"", "")),
			code: CodeExecSelf,
		},
		{
			name: "program is a shell",
			src:  in([]string{"exec:tool", "exec:bash"}, cmd(run(""))+cfField("a", "\"bash\", \"-c\"", "")),
			code: CodeArgvShell, path: "command[0].field[0].choices_from[0]",
		},
		{
			name: "risky flag",
			src: in([]string{"exec:tool", "exec:git"},
				cmd(run(""))+cfField("a", "\"git\", \"-c\", \"a.b=c\", \"tag\"", "")),
			code: CodeArgvRiskyFlag, path: "command[0].field[0].choices_from[1]", warn: true,
		},
		{
			name: "ssh without dashdash",
			src: in([]string{"exec:tool", "exec:ssh"},
				cmd(run(""))+plainField("h")+cfField("a", "\"ssh\", \"{h}\"", "")),
			code: CodeArgvSSHDashDash, path: at + "[1]",
		},
		{
			name: "repo needs its permission",
			src:  in(tool, cmd(run(""))+cfField("a", "\"tool\", \"{repo.slug}\"", "")),
			code: CodePermMissing, path: "command[0].field[0].choices_from[1]",
		},
		{
			name: "empty argv",
			src:  in(tool, cmd(run(""))+cfField("a", "", "")),
			code: CodeValueInvalid, path: "command[0].field[0].choices_from",
		},
		{
			name: "confirm and the same word",
			src: in([]string{"exec:git"}, cmd("run = [\"git\", \"stash\", \"drop\"]\n"+
				"output = \"pager\"\nconfirm = true\n")+
				cfField("a", "\"git\", \"stash\", \"list\"", "")),
			code: CodeArgvChoicesConfirmed, path: "command[0].field[0].choices_from",
			warn: true,
		},
		{
			name: "the default is still checked",
			src: in(tool, setting+cmd(run(""))+
				cfField("a", "\"tool\", \"{setting.s}\"", "default = \"{zz}\"\n")),
			code: CodePlaceholderUnknown, path: "command[0].field[0].default",
		},
	}

	runRules(t, tests)

	valid := []string{
		in(tool, cmd(run(", \"{a}\""))+plainField("a")+
			cfField("b", "\"tool\", \"--list\", \"--\", \"{a}\"", "")),
		in(tool, setting+cmd(run(""))+plainField("a")+
			cfField("b", "\"tool\", \"--pre={setting.s}\", \"{a?}\", \"{cwd}\", \"{home}\"", "")),
		in(repoTool, cmd(run(""))+cfField("a", "\"tool\", \"--\", \"{repo.slug}\"", "")),
		in(tool, listSetting+cmd(run(""))+cfField("a", "\"tool\", \"{setting.s}\"", "")),
		in([]string{"exec:git"}, cmd("run = [\"git\", \"stash\", \"drop\"]\noutput = \"pager\"\n"+
			"confirm = true\n")+cfField("a", "\"git\", \"tag\", \"--list\"", "")),
		in([]string{"exec:gft"}, cmd("run = [\"gft\", \"--\", \"{x}\"]\noutput = \"pager\"\n"+
			"confirm = true\n")+plainField("x")+
			cfField("a", "\"gft\", \"--releases\", \"--plain\", \"--\", \"{x}\"", "")),
	}

	for i, src := range valid {
		if m, rep := decode(src); m == nil {
			t.Errorf("valid case %d refused: %v", i, rep.Issues)
		} else if is := find(rep, CodeArgvChoicesConfirmed, ""); is != nil {
			t.Errorf("valid case %d warns about a confirmed listing: %v", i, is)
		}
	}
}

// TestChoicesFromCountsTheProgram checks that a program used only by a choices_from is not
// reported as an exec: permission that nothing uses, and that an unused one still is.
//
// Parameters:
//   - t: test to report to
func TestChoicesFromCountsTheProgram(t *testing.T) {
	body := cmd(run("")) + cfField("a", "\"other\", \"--list\"", "")

	m, rep := decode(decl("", []string{"exec:tool", "exec:other"}, body))
	if m == nil {
		t.Fatalf("refused: %v", rep.Issues)
	}

	if is := find(rep, CodeExecUnused, ""); is != nil {
		t.Errorf("exec_unused although the suggestions run the program: %v", is)
	}

	_, rep = decode(decl("", []string{"exec:tool", "exec:other", "exec:third"}, body))
	if find(rep, CodeExecUnused, "permissions[2]") == nil {
		t.Errorf("exec:third is unused and was not reported: %v", rep.Issues)
	}
}

// TestChoicesFromIsBoundToTheType checks that the key is only read on a text field and that its
// value is kept as written.
//
// Parameters:
//   - t: test to report to
func TestChoicesFromIsBoundToTheType(t *testing.T) {
	m := mustDecode(t, decl("", tool, cmd(run(""))+plainField("a")+
		cfField("b", "\"tool\", \"--list\", \"--\", \"{a?}\"", "")))

	f := m.Commands[0].Fields
	if f[0].ChoicesFrom != nil {
		t.Errorf("a field without the key has %v", f[0].ChoicesFrom)
	}

	want := []string{"tool", "--list", "--", "{a?}"}
	if len(f[1].ChoicesFrom) != len(want) {
		t.Fatalf("ChoicesFrom = %v, want %v", f[1].ChoicesFrom, want)
	}

	for i := range want {
		if f[1].ChoicesFrom[i] != want[i] {
			t.Errorf("ChoicesFrom[%d] = %q, want %q", i, f[1].ChoicesFrom[i], want[i])
		}
	}
}

// suggestionsManifest is a manifest that uses choices_from the way the specification asks: it
// declares the feature and the range of the tote that has it.
//
// Parameters:
//   - feature: name of the feature to require
func suggestionsManifest(feature string) string {
	src := decl("requires_features = [\""+feature+"\"]\n", tool,
		cmd(run(", \"{a}\""))+plainField("a")+cfField("b", "\"tool\", \"--\", \"{a?}\"", ""))

	return strings.Replace(src, ">=1.0.0, <2", ">=1.1.0, <2", 1)
}

// TestChoicesFromFeature checks that tote 1.0 reports the feature as incompatible, never invalid,
// and that field.choices_closed stays reserved.
//
// Parameters:
//   - t: test to report to
func TestChoicesFromFeature(t *testing.T) {
	if !slices.Contains(AnnouncedFeatures, "field.choices_from") ||
		slices.Contains(ReservedFeatures, "field.choices_from") {
		t.Fatalf("field.choices_from must be announced and not reserved: %v %v", AnnouncedFeatures,
			ReservedFeatures)
	}

	if !slices.Contains(ReservedFeatures, "field.choices_closed") ||
		slices.Contains(AnnouncedFeatures, "field.choices_closed") {
		t.Errorf("field.choices_closed must be reserved and not announced")
	}

	src := suggestionsManifest("field.choices_from")

	m, rep := Decode([]byte(src), DecodeOptions{Host: testHost()})
	if m != nil || rep.Class() != ClassIncompatible {
		t.Fatalf("on tote 1.0: manifest=%v class=%v, want nil and incompatible", m != nil,
			rep.Class())
	}

	for _, code := range []string{CodeFeatureMissing, CodeToteRange} {
		if !rep.Has(code) {
			t.Errorf("on tote 1.0: no %s in %v", code, rep.Issues)
		}
	}

	if rep.Has(CodeReserved) || rep.Has(CodeKeyConflict) {
		t.Errorf("tote 1.0 read the key instead of stopping at the probe: %v", rep.Issues)
	}

	if m, rep := Decode([]byte(src), DecodeOptions{Host: testHost11()}); m == nil {
		t.Errorf("on tote 1.1: refused: %v", rep.Issues)
	}

	for _, host := range []Host{testHost(), testHost11()} {
		m, rep := Decode([]byte(suggestionsManifest("field.choices_closed")), DecodeOptions{Host: host})
		if m != nil || rep.Class() != ClassIncompatible || !rep.Has(CodeFeatureMissing) {
			t.Errorf("field.choices_closed on %s: manifest=%v class=%v: %v", host.Version, m != nil,
				rep.Class(), rep.Issues)
		}
	}
}

// TestTestHostModelsTote10 checks that the host of the tests keeps the features of tote 1.0
// whatever the list of the running tote is.
//
// Parameters:
//   - t: test to report to
func TestTestHostModelsTote10(t *testing.T) {
	if got := testHost().Features; !slices.Equal(got, features10) {
		t.Errorf("testHost features = %v, want %v", got, features10)
	}

	if slices.Contains(testHost().Features, "field.choices_from") {
		t.Error("the host of tote 1.0 announces field.choices_from")
	}

	if !slices.Contains(testHost11().Features, "field.choices_from") {
		t.Error("the host of tote 1.1 does not announce field.choices_from")
	}
}
