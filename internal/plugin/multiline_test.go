package plugin

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// mlField builds a multiline text field.
//
// Parameters:
//   - id: field id
//   - extra: more key lines of the field
func mlField(id, extra string) string {
	return "[[command.field]]\nid = \"" + id + "\"\ntype = \"text\"\nlabel = \"F\"\nmultiline = true\n" +
		extra
}

// TestRulesMultiline checks the reading of multiline and lines: the bounds of lines, the types
// that refuse the keys and the keys that a multiline field refuses.
//
// Parameters:
//   - t: test to report to
func TestRulesMultiline(t *testing.T) {
	in := func(f string) string { return decl("", tool, cmd(run(", \"{x}\""))+f) }
	at := "command[0].field[0]."

	tests := []ruleCase{
		{
			name: "multiline is not a boolean", src: in(field("type = \"text\"\nmultiline = 1\n")),
			code: CodeTypeInvalid, path: at + "multiline",
		},
		{
			name: "lines below the range",
			src:  in(field("type = \"text\"\nmultiline = true\nlines = 1\n")),
			code: CodeLimitExceeded, path: at + "lines",
		},
		{
			name: "lines above the range",
			src:  in(field("type = \"text\"\nmultiline = true\nlines = 9\n")),
			code: CodeLimitExceeded, path: at + "lines",
		},
		{
			name: "lines is not an integer",
			src:  in(field("type = \"text\"\nmultiline = true\nlines = \"4\"\n")),
			code: CodeTypeInvalid, path: at + "lines",
		},
		{
			name: "lines without multiline", src: in(field("type = \"text\"\nlines = 4\n")),
			code: CodeKeyMissing, path: at + "multiline",
		},
		{
			name: "lines with multiline false",
			src:  in(field("type = \"text\"\nmultiline = false\nlines = 4\n")),
			code: CodeKeyConflict, path: at + "lines",
		},
		{
			name: "multiline with a pattern",
			src:  in(field("type = \"text\"\nmultiline = true\npattern = \"^a$\"\n")),
			code: CodeKeyConflict, path: at + "pattern",
		},
		{
			name: "multiline with a default",
			src:  in(field("type = \"text\"\nmultiline = true\ndefault = \"a\"\n")),
			code: CodeKeyConflict, path: at + "default",
		},
		{
			name: "multiline with an empty default",
			src:  in(field("type = \"text\"\nmultiline = true\ndefault = \"\"\n")),
			code: CodeKeyConflict, path: at + "default",
		},
		{
			name: "multiline with choices_from",
			src:  in(field("type = \"text\"\nmultiline = true\nchoices_from = [\"tool\"]\n")),
			code: CodeKeyConflict, path: at + "choices_from",
		},
		{
			name: "multiline on a choice",
			src:  in(field("type = \"choice\"\nchoices = [\"a\"]\nmultiline = true\n")),
			code: CodeKeyConflict, path: at + "multiline",
		},
		{
			name: "multiline on a bool",
			src:  in(field("type = \"bool\"\nflag = \"-a\"\nmultiline = true\n")),
			code: CodeKeyConflict, path: at + "multiline",
		},
		{
			name: "lines on a choice",
			src:  in(field("type = \"choice\"\nchoices = [\"a\"]\nlines = 4\n")),
			code: CodeKeyConflict, path: at + "lines",
		},
		{
			name: "lines on a bool",
			src:  in(field("type = \"bool\"\nflag = \"-a\"\nlines = 4\n")),
			code: CodeKeyConflict, path: at + "lines",
		},
	}

	runRules(t, tests)

	valid := []struct {
		src    string
		multi  bool
		lines  int
		maxLen int
	}{
		{field("type = \"text\"\nmultiline = true\n"), true, DefaultMultilineRows, MaxMultilineBytes},
		{field("type = \"text\"\nmultiline = true\nlines = 2\n"), true, 2, MaxMultilineBytes},
		{field("type = \"text\"\nmultiline = true\nlines = 8\n"), true, 8, MaxMultilineBytes},
		{field("type = \"text\"\nmultiline = true\nmax_len = 300\n"), true, DefaultMultilineRows, 300},
		{
			field("type = \"text\"\nmultiline = true\nrequired = true\n"), true, DefaultMultilineRows,
			MaxMultilineBytes,
		},
		{field("type = \"text\"\nmultiline = false\n"), false, 0, defaultMaxLen},
		{field("type = \"text\"\nmultiline = false\npattern = \"^a$\"\n"), false, 0, defaultMaxLen},
	}

	for i, tt := range valid {
		m, rep := decode(in(tt.src))
		if m == nil {
			t.Errorf("valid case %d refused: %v", i, rep.Issues)

			continue
		}

		f := m.Commands[0].Fields[0]
		if f.Multiline != tt.multi || f.Lines != tt.lines || f.MaxLen != tt.maxLen {
			t.Errorf("case %d: multiline=%v lines=%d max_len=%d, want %v %d %d", i, f.Multiline,
				f.Lines, f.MaxLen, tt.multi, tt.lines, tt.maxLen)
		}
	}
}

// multilineManifest is a manifest that needs a feature and uses a multiline field, written for
// tote 1.2.
//
// Parameters:
//   - feature: the feature it declares
func multilineManifest(feature string) string {
	src := decl("requires_features = [\""+feature+"\"]\n", tool,
		cmd(run(", \"--body={body}\""))+mlField("body", "lines = 6\n"))

	return strings.Replace(src, ">=1.0.0, <2", ">=1.2.0, <2", 1)
}

// TestMultilineFeature checks that field.multiline is announced, that tote 1.0 and tote 1.1 report a
// manifest that needs it as incompatible and never invalid, and that tote 1.2 reads it.
//
// Parameters:
//   - t: test to report to
func TestMultilineFeature(t *testing.T) {
	if !slices.Contains(AnnouncedFeatures, "field.multiline") ||
		slices.Contains(ReservedFeatures, "field.multiline") {
		t.Fatalf("field.multiline must be announced and not reserved: %v %v", AnnouncedFeatures,
			ReservedFeatures)
	}

	if slices.Contains(testHost11().Features, "field.multiline") ||
		!slices.Contains(testHost12().Features, "field.multiline") {
		t.Fatal("tote 1.1 must not announce field.multiline, tote 1.2 must")
	}

	src := multilineManifest("field.multiline")

	for _, host := range []Host{testHost(), testHost11()} {
		m, rep := Decode([]byte(src), DecodeOptions{Host: host})
		if m != nil || rep.Class() != ClassIncompatible {
			t.Fatalf("on tote %s: manifest=%v class=%v, want nil and incompatible", host.Version,
				m != nil, rep.Class())
		}

		for _, code := range []string{CodeFeatureMissing, CodeToteRange} {
			if !rep.Has(code) {
				t.Errorf("on tote %s: no %s in %v", host.Version, code, rep.Issues)
			}
		}

		if rep.Has(CodeReserved) || rep.Has(CodeUnknownKey) || rep.Has(CodeKeyConflict) {
			t.Errorf("tote %s read the key instead of stopping at the probe: %v", host.Version,
				rep.Issues)
		}
	}

	m, rep := Decode([]byte(src), DecodeOptions{Host: testHost12()})
	if m == nil {
		t.Fatalf("on tote 1.2: refused: %v", rep.Issues)
	}

	if f := m.Commands[0].Fields[0]; !f.Multiline || f.Lines != 6 {
		t.Errorf("on tote 1.2: field = %+v", f)
	}
}

// TestCheckFieldMultiline checks the values of a multiline field: line feeds only there, line
// breaks normalised, the other control characters refused, the caps in bytes, lines and runes.
//
// Parameters:
//   - t: test to report to
func TestCheckFieldMultiline(t *testing.T) {
	ml := &Field{ID: "m", Type: "text", Multiline: true, MaxLen: MaxMultilineBytes}
	plain := &Field{ID: "p", Type: "text", MaxLen: maxMaxLen}

	ok := []struct {
		name string
		f    *Field
		raw  string
		want string
	}{
		{"two lines", ml, "milk\neggs", "milk\neggs"},
		{"crlf", ml, "milk\r\neggs\r\n", "milk\neggs\n"},
		{"lone cr", ml, "milk\reggs", "milk\neggs"},
		{"mixed breaks", ml, "a\r\rb\n\r\nc", "a\n\nb\n\nc"},
		{"leading dash", ml, "- milk\n- eggs", "- milk\n- eggs"},
		{"sixteen lines", ml, strings.Repeat("a\n", 15) + "a", strings.Repeat("a\n", 15) + "a"},
		{"sixteen lines, last empty", ml, strings.Repeat("a\n", 15), strings.Repeat("a\n", 15)},
		{"1000 bytes", ml, strings.Repeat("a", MaxMultilineBytes), strings.Repeat("a", MaxMultilineBytes)},
		{"joiners", ml, "a\u200db", "a\u200db"},
		{"empty", ml, "", ""},
	}

	for _, tt := range ok {
		fv, err := checkField(tt.f, tt.raw)
		if err != nil {
			t.Errorf("%s: refused: %v", tt.name, err)

			continue
		}

		if fv.Text != tt.want || !fv.Multiline || fv.Type != "text" {
			t.Errorf("%s: value = %+v, want %q multiline", tt.name, fv, tt.want)
		}
	}

	bad := []struct {
		name string
		f    *Field
		raw  string
		rule string
	}{
		{"lf in a single-line field", plain, "a\nb", RejectControl},
		{"crlf in a single-line field", plain, "a\r\nb", RejectControl},
		{"lone cr in a single-line field", plain, "a\rb", RejectControl},
		{"tab", ml, "a\tb", RejectControl},
		{"nul", ml, "a\x00b", RejectControl},
		{"escape", ml, "a\x1b[31mb", RejectControl},
		{"c1", ml, "a\u0085b", RejectControl},
		{"bidi", ml, "a\u202eb", RejectControl},
		{"isolate", ml, "a\u2066b", RejectControl},
		{"zero width", ml, "a\u200bb", RejectControl},
		{"line separator", ml, "a\u2028b", RejectControl},
		{"bad utf8", ml, "a\xffb", RejectUTF8},
		{"seventeen lines", ml, strings.Repeat("a\n", 16) + "a", RejectLines},
		{"seventeen lines, last empty", ml, strings.Repeat("a\n", 16), RejectLines},
		{"seventeen crlf lines", ml, strings.Repeat("a\r\n", 16) + "a", RejectLines},
		{"1001 bytes", ml, strings.Repeat("a", MaxMultilineBytes+1), RejectLength},
		{"1001 bytes after normalisation", ml, strings.Repeat("a", 990) + strings.Repeat("\r\n", 11),
			RejectLength},
		{"over the call limit", ml, strings.Repeat("a", maxValueBytes+1), RejectLength},
		{"required", &Field{ID: "m", Type: "text", Multiline: true, MaxLen: 10, Required: true}, "",
			RejectRequired},
		{"max_len in runes", &Field{ID: "m", Type: "text", Multiline: true, MaxLen: 3},
			"\u00e9\n\u00e9\u00e9", RejectMaxLen},
	}

	for _, tt := range bad {
		_, err := checkField(tt.f, tt.raw)
		if err == nil || err.Code != CodeInputInvalid || err.Args[1] != tt.rule {
			t.Errorf("%s: got %v, want %s", tt.name, err, tt.rule)
		}

		if got := FieldRule(*tt.f, tt.raw); got != tt.rule {
			t.Errorf("%s: FieldRule = %q, want %q", tt.name, got, tt.rule)
		}
	}

	// A CRLF counts as one byte once normalised: 1010 bytes typed are 995 bytes of value.
	if _, err := checkField(ml, strings.Repeat("a", 980)+strings.Repeat("\r\n", 15)); err != nil {
		t.Errorf("value under the cap after normalisation refused: %v", err)
	}

	if fv, err := checkField(plain, "abc"); err != nil || fv.Multiline {
		t.Errorf("single-line value = %+v %v", fv, err)
	}
}

// TestProducedIssueLines checks the produced text rule of a multiline value against the strict
// one.
//
// Parameters:
//   - t: test to report to
func TestProducedIssueLines(t *testing.T) {
	tests := []struct {
		s           string
		strict, ml  string
		description string
	}{
		{"a b", "", "", "plain"},
		{"a\nb", RejectControl, "", "line feed"},
		{"a\rb", RejectControl, RejectControl, "carriage return"},
		{"a\tb", RejectControl, RejectControl, "tab"},
		{"a\x00b", RejectControl, RejectControl, "nul"},
		{"a\u202eb", RejectControl, RejectControl, "bidi"},
		{"a\xffb", RejectUTF8, RejectUTF8, "utf8"},
		{strings.Repeat("\n", 11), RejectLength, RejectLength, "length"},
	}

	for _, tt := range tests {
		if got := ProducedIssue(tt.s, 10); got != tt.strict {
			t.Errorf("%s: ProducedIssue = %q, want %q", tt.description, got, tt.strict)
		}

		if got := ProducedIssueLines(tt.s, 10); got != tt.ml {
			t.Errorf("%s: ProducedIssueLines = %q, want %q", tt.description, got, tt.ml)
		}
	}

	if NormalizeLines("a\r\nb\rc\n") != "a\nb\nc\n" || NormalizeLines("x") != "x" {
		t.Error("NormalizeLines")
	}

	for s, n := range map[string]int{"": 0, "a": 1, "a\n": 2, "\n": 2, "a\nb\nc": 3} {
		if LineCount(s) != n {
			t.Errorf("LineCount(%q) = %d, want %d", s, LineCount(s), n)
		}
	}
}

// mlValues is the values of a call with a multiline field m that holds two lines, a single-line
// field x and a single-line field that a test fills with a line feed by hand.
func mlValues() *Values {
	v := vals()
	v.Fields["m"] = FieldValue{Text: "milk\neggs", Type: "text", Multiline: true}
	v.Fields["d"] = FieldValue{Text: "- milk\n- eggs", Type: "text", Multiline: true}
	v.Fields["forged"] = FieldValue{Text: "a\nb", Type: "text"}

	return v
}

// TestExpandMultiline checks where the line feeds of a multiline value may go: an element of the
// run only, never another placeholder nor a literal, never a scalar, and the dash rule unchanged.
//
// Parameters:
//   - t: test to report to
func TestExpandMultiline(t *testing.T) {
	v := mlValues()

	ok := []struct {
		src, want string
	}{
		{"{m}", "milk\neggs"},
		{"--body={m}", "--body=milk\neggs"},
		{"--body={d}", "--body=- milk\n- eggs"},
		{"{x}:{m}", "hello:milk\neggs"},
	}

	for _, tt := range ok {
		got, err := ExpandArgv(tpl(t, tt.src), v, nil)
		if err != nil || len(got) != 1 || got[0] != tt.want {
			t.Errorf("%s: got %q %v, want %q", tt.src, got, err, tt.want)
		}
	}

	bad := []struct {
		src, code, rule string
	}{
		{"{d}", CodeDashRefused, ""},
		{"{forged}", CodeProducedInvalid, RejectControl},
		{"{m}{forged}", CodeProducedInvalid, RejectControl},
		{"--a={forged}", CodeProducedInvalid, RejectControl},
	}

	for _, tt := range bad {
		_, err := ExpandArgv(tpl(t, tt.src), v, nil)
		if err == nil || err.Code != tt.code || (tt.rule != "" && err.Args[1] != tt.rule) {
			t.Errorf("%s: got %v, want %s %s", tt.src, err, tt.code, tt.rule)
		}
	}

	if _, err := ExpandArgv(tpl(t, "{d}"), v, map[string]bool{"d": true}); err != nil {
		t.Errorf("allow_dash on a multiline field refused: %v", err)
	}

	for _, src := range []string{"{m}", "--body={m}"} {
		_, err := ExpandArgvStrict(tpl(t, src), v, nil)
		if err == nil || err.Code != CodeProducedInvalid || err.Args[1] != RejectControl {
			t.Errorf("strict %s: got %v, want produced_invalid control", src, err)
		}

		_, _, serr := ExpandScalar(tpl(t, src), v, MaxArgvElemBytes)
		if serr == nil || serr.Code != CodeProducedInvalid || serr.Args[1] != RejectControl {
			t.Errorf("scalar %s: got %v, want produced_invalid control", src, serr)
		}

		if got := ExpandLoose(tpl(t, src), v); strings.Contains(got, "\n") {
			t.Errorf("loose %s gives a line feed: %q", src, got)
		}
	}

	// A single-line multiline value is fine everywhere.
	one := mlValues()
	one.Fields["m"] = FieldValue{Text: "milk", Type: "text", Multiline: true}

	if got, err := ExpandArgvStrict(tpl(t, "{m}"), one, nil); err != nil || got[0] != "milk" {
		t.Errorf("strict one line: %q %v", got, err)
	}

	// The element limit stays 1024 bytes after substitution.
	long := mlValues()
	long.Fields["m"] = FieldValue{Text: strings.Repeat("a\n", 510), Type: "text", Multiline: true}

	if _, err := ExpandArgv(tpl(t, "--body={m}"), long, nil); err == nil ||
		err.Code != CodeProducedInvalid || err.Args[1] != RejectLength {
		t.Errorf("1027 bytes: %v", err)
	}
}

// TestDisplayArgv checks that the line feeds of an argv are shown as \n on one line and that an
// argv without them is shown as before.
//
// Parameters:
//   - t: test to report to
func TestDisplayArgv(t *testing.T) {
	plain := []string{"tool", "--x", "a b"}
	if DisplayArgv(plain) != "tool --x 'a b'" {
		t.Errorf("plain = %q", DisplayArgv(plain))
	}

	argv := []string{"tool", "--body=milk\neggs", `a\nb`, `c\d` + "\n"}
	got := DisplayArgv(argv)

	if strings.Contains(got, "\n") {
		t.Errorf("display holds a line feed: %q", got)
	}

	want := `tool '--body=milk\neggs' 'a\nb' 'c\\d\n'`
	if got != want {
		t.Errorf("display = %s, want %s", got, want)
	}

	if argv[1] != "--body=milk\neggs" {
		t.Error("DisplayArgv changed its argument")
	}
}

// mlBody is a plugin of the sandbox with a multiline field used by the run, and by env, cwd,
// confirm, open and choices_from, where its line feeds must be refused.
const mlBody = `
[[command]]
id = "add"
label = "Add"
run = ["tool", "add", "--body={body}"]
output = "silent"
confirm = "Send {body}?"
[[command.field]]
id = "body"
type = "text"
label = "Body"
multiline = true
lines = 5
required = true

[[command]]
id = "plain"
label = "Plain"
run = ["tool", "{body}"]
output = "silent"
[[command.field]]
id = "body"
type = "text"
label = "Body"
multiline = true

[[command]]
id = "env"
label = "Env"
run = ["tool"]
output = "silent"
[command.env]
BODY = "{body}"
[[command.field]]
id = "body"
type = "text"
label = "Body"
multiline = true

[[command]]
id = "cwd"
label = "Cwd"
run = ["tool"]
cwd = "{body}"
output = "silent"
[[command.field]]
id = "body"
type = "text"
label = "Body"
multiline = true

[[command]]
id = "web"
label = "Web"
open = "https://example.org/?q={body}"
[[command.field]]
id = "body"
type = "text"
label = "Body"
multiline = true

[[command]]
id = "sugg"
label = "Sugg"
run = ["tool", "--", "{body}", "{tag?}"]
output = "silent"
[[command.field]]
id = "body"
type = "text"
label = "Body"
multiline = true
[[command.field]]
id = "tag"
type = "text"
label = "Tag"
choices_from = ["tool", "--list", "--", "{body}"]
`

// TestPrepareMultiline checks a multiline field end to end through Prepare: the line feed reaches
// the argv of the run, the display shows it as \n, the confirmation drops it, and env, cwd, open
// and choices_from refuse it.
//
// Parameters:
//   - t: test to report to
func TestPrepareMultiline(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()
	l.script("tool", "exit 0\n")
	l.plug("demo", []string{"exec:tool", "env:BODY", "host:open-url"}, mlBody)

	r := l.reg(nil)
	call := func(entry, body string) Call {
		return Call{
			Plugin: "demo", Table: TableCommand, Entry: entry,
			Values: map[string]string{"body": body},
		}
	}

	p := prepare(t, r, call("add", "- milk\r\n- eggs"))

	want := []string{filepath.Join(l.bin, "tool"), "add", "--body=- milk\n- eggs"}
	if !slices.Equal(p.Argv, want) {
		t.Errorf("argv = %q, want %q", p.Argv, want)
	}

	if p.Display != `tool add '--body=- milk\n- eggs'` {
		t.Errorf("display = %s", p.Display)
	}

	if !p.Confirm.Required || strings.ContainsAny(p.Confirm.Text, "\n\r") ||
		strings.Contains(p.Confirm.Text, "milk") {
		t.Errorf("confirm = %+v", p.Confirm)
	}

	if p := prepare(t, r, call("add", "one line")); p.Confirm.Text != "Send one line?" {
		t.Errorf("confirm of one line = %q", p.Confirm.Text)
	}

	if e := refusal(t, r, call("plain", "- milk\n- eggs")); e.Code != CodeDashRefused {
		t.Errorf("plain dash: %v", e)
	}

	if p := prepare(t, r, call("plain", "milk\neggs")); p.Argv[1] != "milk\neggs" {
		t.Errorf("plain: %q", p.Argv)
	}

	if e := refusal(t, r, call("add", strings.Repeat("a\n", 16)+"a")); e.Code != CodeInputInvalid ||
		e.Args[1] != RejectLines {
		t.Errorf("17 lines: %v", e)
	}

	if e := refusal(t, r, call("add", "a\tb")); e.Code != CodeInputInvalid ||
		e.Args[1] != RejectControl {
		t.Errorf("tab: %v", e)
	}

	for _, entry := range []string{"env", "cwd", "web"} {
		e := refusal(t, r, call(entry, "a\nb"))
		if e.Code != CodeProducedInvalid || e.Args[1] != RejectControl {
			t.Errorf("%s: got %v, want produced_invalid control", entry, e)
		}
	}

	_, err := r.Choices(Call{
		Plugin: "demo", Table: TableCommand, Entry: "sugg",
		Values: map[string]string{"body": "a\nb"},
	}, "tag")

	if e, ok := err.(*Error); !ok || e.Code != CodeProducedInvalid || e.Args[1] != RejectControl {
		t.Errorf("choices_from: got %v, want produced_invalid control", err)
	}
}

// surfaceMultiline is a manifest with a multiline field, one with lines set and a plain text
// field, for the pinned hash.
const surfaceMultiline = `[[command]]
id = "add"
label = "Add"
run = ["tool", "add", "--body={body}", "--", "{title}"]
output = "silent"
[[command.field]]
id = "title"
type = "text"
label = "Title"
required = true
[[command.field]]
id = "body"
type = "text"
label = "Body"
multiline = true
lines = 6
required = true
max_len = 900
`

// surfaceMultilineHash freezes the canonical encoding of multiline. It must never change: a
// consented plugin would ask again.
const surfaceMultilineHash = "4d245ee8ea4630e1ad239ca33ec1c3080ef7fdf9b67d0bc7bce4a8d1d408cd60"

// TestSurfaceMultiline checks that multiline is counted only when true, that lines is not counted,
// and pins the hash.
//
// Parameters:
//   - t: test to report to
func TestSurfaceMultiline(t *testing.T) {
	for _, f := range []Field{
		{ID: "x", Type: "text"},
		{ID: "x", Type: "text", Lines: 6},
	} {
		if _, has := fieldObject(f)["multiline"]; has {
			t.Errorf("field %+v writes multiline", f)
		}
	}

	m := mustDecode(t, decl("", tool, surfaceMultiline))
	raw := string(SurfaceJSON(m))

	want := `{"id":"body","multiline":true,"required":true,"type":"text"}`
	if !strings.Contains(raw, want) || strings.Contains(raw, "lines") {
		t.Errorf("surface lacks %s or holds lines:\n%s", want, raw)
	}

	if got := SurfaceSHA256(m); got != surfaceMultilineHash {
		t.Errorf("surface hash of the multiline manifest = %s, want %s\n%s", got,
			surfaceMultilineHash, raw)
	}

	rows := mustDecode(t, decl("", tool, strings.Replace(surfaceMultiline, "lines = 6", "lines = 3", 1)))
	if SurfaceSHA256(rows) != SurfaceSHA256(m) {
		t.Error("lines changes the surface hash")
	}

	single := mustDecode(t, decl("", tool, strings.Replace(surfaceMultiline,
		"multiline = true\nlines = 6\n", "", 1)))
	if SurfaceSHA256(single) == SurfaceSHA256(m) {
		t.Error("multiline does not change the surface hash")
	}

	off := mustDecode(t, decl("", tool, strings.Replace(surfaceMultiline,
		"multiline = true\nlines = 6\n", "multiline = false\n", 1)))
	if SurfaceSHA256(off) != SurfaceSHA256(single) {
		t.Error("multiline = false changes the surface hash")
	}
}

// TestSurfaceMultilineEscalation checks the escalation of multiline: a field that becomes
// multiline, or a new command with one, asks again; a kept or removed multiline and a change of
// lines do not.
//
// Parameters:
//   - t: test to report to
func TestSurfaceMultilineEscalation(t *testing.T) {
	plain := surfaceBase("", "", "")
	multi := strings.Replace(plain, "label = \"X\"\n", "label = \"X\"\nmultiline = true\n", 1)
	rows := strings.Replace(multi, "multiline = true\n", "multiline = true\nlines = 7\n", 1)
	esc := []SurfaceChange{{EscMultiline, "command", "go"}}

	tests := []struct {
		name      string
		old, next string
		want      []SurfaceChange
	}{
		{"added", plain, multi, esc},
		{"kept", multi, multi, nil},
		{"lines changed", multi, rows, nil},
		{"removed", multi, plain, nil},
		{"new command", plain, plain + strings.Replace(multi[strings.Index(multi, "[[command]]"):],
			"id = \"go\"", "id = \"fresh\"", 1), []SurfaceChange{{EscMultiline, "command", "fresh"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SurfaceEscalations(mustDecode(t, tt.old), mustDecode(t, tt.next))
			if !slices.Equal(got, tt.want) {
				t.Errorf("changes = %v, want %v", got, tt.want)
			}
		})
	}
}

// FuzzExpandMultiline checks that a multiline value can put a line feed in an element of the run
// only through its own placeholder, never another control character, never more than the limit,
// and never a line feed through the strict expansion.
//
// Parameters:
//   - f: fuzz target
func FuzzExpandMultiline(f *testing.F) {
	for _, s := range []string{"{m}", "--body={m}", "{x}{m}", "{m}{x}", "{m?}", "-{m}", "{x}", "{view}", "{flag}{m}"} {
		f.Add(s, "milk\neggs")
		f.Add(s, "- milk\n- eggs")
		f.Add(s, "a\r\nb")
		f.Add(s, "a\tb\n")
		f.Add(s, "\u202e\n")
	}

	f.Fuzz(func(t *testing.T, src, value string) {
		if len(src) > 512 || len(value) > 8192 {
			return
		}

		tm, err := ParseTemplate(src)
		if err != nil {
			return
		}

		v := vals()
		v.Fields["m"] = FieldValue{Text: value, Type: "text", Multiline: true}

		usesM := false

		for _, h := range tm.Placeholders() {
			usesM = usesM || h.Name == "m"
		}

		got, perr := ExpandArgv(tm, v, nil)
		if perr == nil {
			for _, e := range got {
				if !utf8.ValidString(e) || ProducedIssueLines(e, MaxArgvElemBytes) != "" {
					t.Fatalf("element %q from %q with %q", e, src, value)
				}

				if strings.Contains(e, "\n") && !usesM {
					t.Fatalf("line feed without {m}: %q from %q", e, src)
				}

				if strings.HasPrefix(e, "-") && len(tm.Parts) > 0 && dashChecked(tm.Parts[0].Holder, v) {
					t.Fatalf("dash element %q from %q with %q", e, src, value)
				}
			}
		}

		strict, serr := ExpandArgvStrict(tm, v, nil)
		if serr == nil {
			for _, e := range strict {
				if ProducedIssue(e, MaxArgvElemBytes) != "" {
					t.Fatalf("strict element %q from %q with %q", e, src, value)
				}
			}
		}
	})
}

// dashChecked reports whether the dash rule applies to an element that starts with this
// placeholder: it does not for a choice with static choices or the flag of a bool field, which
// dashCheck lets through on purpose.
//
// Parameters:
//   - h: first placeholder of the template, nil for a literal
//   - v: values of the fuzz run
func dashChecked(h *Placeholder, v *Values) bool {
	if h == nil {
		return false
	}

	if h.Kind == KindField {
		if typ := v.Fields[h.Name].Type; typ == "choice" || typ == "bool" {
			return false
		}
	}

	return true
}
