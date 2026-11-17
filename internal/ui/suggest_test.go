package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/totecli/tote/internal/plugin"
)

// suggestManifest is the plugin of the tests of the suggestions: a palette command that asks for a
// repository and a tag, where the tag lists its suggestions with the program lister, and a second
// command whose second field has no suggestions.
const suggestManifest = `api = 1
id = "taglist"
version = "1.0.0"
tote = ">=1.0.0, <2"
requires_features = ["field.choices_from"]
title = "Tags"
description = "Lists the tags of a repository."
license = "MIT"
tier = "declarative"
permissions = ["exec:lister"]

[[command]]
id = "pick"
label = "Pick a tag"
run = ["lister", "--show", "--", "{name}", "{tag?}"]
output = "pager"

[[command.field]]
id = "name"
type = "text"
label = "Repository"
required = true
pattern = "^[a-z0-9/_-]+$"

[[command.field]]
id = "tag"
type = "text"
label = "Tag"
pattern = "^[A-Za-z0-9._-]{0,24}$"
choices_from = ["lister", "--tags", "{name}"]

[[command]]
id = "plain"
label = "Plain command"
run = ["lister", "--show", "{x}"]
output = "pager"

[[command.field]]
id = "x"
type = "text"
label = "X"
`

// fakeLister stands for the lister of the suggestions. It logs its argv and its process id, then
// answers by the value of the repository that comes after --tags. A value that starts with slow
// waits for the file GATE first, then answers as the rest of the value does.
const fakeLister = `echo "$@" >> "LOG"
case "$1" in
--tags) ;;
*) exit 0;;
esac
echo $$ >> "PIDS"
case "$2" in
slow*) while [ ! -e "GATE" ]; do sleep 0.02; done; set -- "$1" "${2#slow}";;
esac
case "$2" in
ok) printf 'v1.0\tfirst\nV2.0\nv3.0\trecommended\n';;
empty) exit 0;;
fail) printf '\033[31mUnknown option: --tags\033[0m\n'; exit 1;;
longfail) printf 'Unknown option: %s\n' "$(printf '%0120d' 7)"; exit 1;;
mix) printf 'good\nbad value\n-x\nok.1\n';;
hang) exec sleep 60;;
*) printf 'one\n';;
esac
exit 0
`

// suggestRig is a rig over a sandbox where the plugin taglist is installed and enabled, with the
// fake lister first on the PATH. The lister of a slow value waits until gate exists, see release.
type suggestRig struct {
	*rig
	lab  *plugLab
	log  string
	pids string
	gate string
}

// newSuggestRig builds the sandbox, the plugin and the rig, and opens the form of the command
// pick with the repository typed and the keys on the tag.
//
// Parameters:
//   - t: running test
//   - repo: value typed in the repository field, empty to leave it empty
func newSuggestRig(t *testing.T, repo string) *suggestRig {
	t.Helper()

	return newSuggestRigOf(t, repo, suggestManifest)
}

// newSuggestRigOf is newSuggestRig for another manifest of the plugin taglist, which must keep the
// command pick with the repository as its first field.
//
// Parameters:
//   - t: running test
//   - repo: value typed in the repository field, empty to leave it empty
//   - manifest: text of the manifest
func newSuggestRigOf(t *testing.T, repo, manifest string) *suggestRig {
	t.Helper()

	sb := newSandbox(t)
	l := newPlugLab(t, sb)

	bin := filepath.Join(sb.Dir, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}

	s := &suggestRig{
		lab: l, log: filepath.Join(sb.Dir, "lister.log"), pids: filepath.Join(sb.Dir, "lister.pids"),
		gate: filepath.Join(sb.Dir, "lister.gate"),
	}

	body := strings.NewReplacer("LOG", s.log, "PIDS", s.pids, "GATE", s.gate).Replace(fakeLister)
	script := filepath.Join(bin, "lister")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}

	l.env["PATH"] = bin + ":/usr/bin:/bin"
	l.install("taglist", manifest)

	sb.Opts.Start = StartHome
	s.rig = newRig(t, sb)

	// The runs that were stopped must have returned before the directories are removed.
	t.Cleanup(func() { s.r.closeOverlays() })

	s.press("ctrl+k")
	s.typeText("pick a tag")
	s.press("enter")

	if _, ok := s.top().(*fieldsForm); !ok {
		t.Fatalf("the palette led to %T:\n%s", s.top(), s.text())
	}

	s.typeText(repo)
	s.press("tab")

	return s
}

// form returns the form on top of the stack.
func (s *suggestRig) form() *fieldsForm {
	s.t.Helper()

	f, ok := s.top().(*fieldsForm)
	if !ok {
		s.t.Fatalf("the top overlay is %T:\n%s", s.top(), s.text())
	}

	return f
}

// starts returns how many times the lister was started to list suggestions.
func (s *suggestRig) starts() int {
	n := 0

	for _, l := range readLog(s.t, s.log) {
		if strings.HasPrefix(l, "--tags") {
			n++
		}
	}

	return n
}

// listed waits until no suggestions run is going.
func (s *suggestRig) listed() {
	s.t.Helper()
	s.await("the suggestions to end", func() bool { return s.r.choices == nil })
}

// TestSuggestFlow plays the way of a user: Ctrl+L on the tag, the picker with the labels, a pick
// that fills the field, and the form that still takes any typed value.
func TestSuggestFlow(t *testing.T) {
	s := newSuggestRig(t, "ok")

	got := s.footerText()
	if !strings.Contains(got, "ctrl+l") || !strings.Contains(got, "suggestions") {
		t.Errorf("the footer does not offer the suggestions: %q", got)
	}

	if !strings.Contains(s.text(), "ctrl+l: suggestions") {
		t.Errorf("the field does not say how to ask:\n%s", s.text())
	}

	if s.starts() != 0 {
		t.Fatal("the lister started before the key")
	}

	s.press("ctrl+l")
	s.listed()

	if _, ok := s.top().(*pickOverlay); !ok {
		t.Fatalf("Ctrl+L led to %T:\n%s", s.top(), s.text())
	}

	for _, want := range []string{"v1.0", "first", "V2.0", "v3.0", "recommended", "Suggestions: Tag"} {
		if !strings.Contains(s.text(), want) {
			t.Errorf("the picker lacks %q:\n%s", want, s.text())
		}
	}

	if got := readLog(t, s.log); len(got) != 1 || !strings.HasSuffix(got[0], "--tags ok") {
		t.Fatalf("log %q", got)
	}

	s.press("esc")

	if !strings.Contains(s.text(), "Suggestions: 3") {
		t.Errorf("the status line does not say what was listed:\n%s", s.text())
	}

	s.press("ctrl+l")
	s.press("down", "enter")

	if s.starts() != 1 {
		t.Errorf("the second listing started the lister again (%d starts)", s.starts())
	}

	f := s.form()
	if got := f.fields[1].text(); got != "V2.0" {
		t.Errorf("the pick gave %q, want V2.0", got)
	}

	if !f.fields[1].Touched || f.fields[1].Err != "" {
		t.Errorf("the picked field %+v", f.fields[1])
	}

	s.typeText("x")

	if got := f.fields[1].text(); got != "V2.0x" {
		t.Errorf("the field is not free text after a pick: %q", got)
	}
}

// TestSuggestNeverStartsBeforeTheKey checks that opening the form, moving between the fields,
// typing, Enter and Tab never start the lister.
func TestSuggestNeverStartsBeforeTheKey(t *testing.T) {
	s := newSuggestRig(t, "ok")

	s.typeText("v1")
	s.press("shift+tab", "tab", "up", "down", "tab", "tab")

	if s.starts() != 0 || s.r.choices != nil {
		t.Fatalf("the lister started (%d starts, run %v)", s.starts(), s.r.choices)
	}

	if f := s.form(); f.sug.phase != sugIdle {
		t.Errorf("phase %v", f.sug.phase)
	}
}

// TestSuggestCtrlLOnAnotherField checks that Ctrl+L is dropped on a field without suggestions and
// on a form of a command whose fields have none.
func TestSuggestCtrlLOnAnotherField(t *testing.T) {
	s := newSuggestRig(t, "ok")

	s.press("shift+tab", "ctrl+l")

	if s.r.choices != nil || s.starts() != 0 || s.form().sug.phase != sugIdle {
		t.Fatal("Ctrl+L on a field without suggestions started something")
	}

	if strings.Contains(s.footerText(), "ctrl+l") {
		t.Errorf("the footer offers suggestions on a field that has none: %q", s.footerText())
	}

	s.press("esc")
	s.press("ctrl+k")
	s.typeText("plain command")
	s.press("enter")

	if f := s.form(); f.sug != nil {
		t.Error("a form without any choices_from holds a suggestions state")
	}

	s.press("ctrl+l")

	if s.starts() != 0 {
		t.Error("Ctrl+L started the lister on a form without suggestions")
	}
}

// TestSuggestRunningShowsTheArgv checks what shows while the program runs: the exact argv, once, in
// the box, with no status line under it, and Esc, which stops it and keeps the form.
func TestSuggestRunningShowsTheArgv(t *testing.T) {
	s := newSuggestRig(t, "hang")

	s.press("ctrl+l")

	_, boxed := s.top().(*suggestWait)

	if !boxed || s.r.choices == nil || s.r.choices.form.sug.phase != sugRunning {
		t.Fatalf("no run goes:\n%s", s.text())
	}

	if !strings.Contains(s.text(), "lister --tags hang") || strings.Contains(s.text(), "Listing") {
		t.Errorf("the screen shows the argv once and no status line:\n%s", s.text())
	}

	if n := strings.Count(s.text(), "lister --tags hang"); n != 1 {
		t.Errorf("the argv shows %d times, want once, in the box:\n%s", n, s.text())
	}

	if got := s.footerText(); !strings.Contains(got, "esc") {
		t.Errorf("the footer does not offer Esc: %q", got)
	}

	s.press("ctrl+l")

	if s.starts() != 1 {
		t.Errorf("a second Ctrl+L started a second run (%d starts)", s.starts())
	}

	pid := s.pid()
	s.press("esc")

	if s.r.choices != nil {
		t.Fatal("Esc left the run")
	}

	if _, ok := s.top().(*fieldsForm); !ok {
		t.Fatalf("Esc closed the form: %T", s.top())
	}

	if !strings.Contains(s.text(), "Listing stopped.") {
		t.Errorf("nothing says the listing stopped:\n%s", s.text())
	}

	s.gone(pid)
}

// pid reads the process id that the lister recorded last.
func (s *suggestRig) pid() int {
	s.t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(s.pids)
		if f := strings.Fields(string(b)); len(f) > 0 {
			n, err := strconv.Atoi(f[len(f)-1])
			if err == nil {
				return n
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	s.t.Fatal("the lister recorded no pid")

	return 0
}

// processAlive reports whether a process still exists.
//
// Parameters:
//   - pid: process id
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) != syscall.ESRCH
}

// gone waits until a process no longer exists.
//
// Parameters:
//   - pid: process id
func (s *suggestRig) gone(pid int) {
	s.t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	s.t.Fatalf("the process %d is still alive", pid)
}

// TestSuggestWaitsForAnEarlierField checks that an earlier field the argv names, left empty, keeps
// the program from starting and says which field is waited for.
func TestSuggestWaitsForAnEarlierField(t *testing.T) {
	s := newSuggestRig(t, "")

	s.press("ctrl+l")
	s.await("the answer of the registry", func() bool { return s.form().sug.phase != sugPreparing })

	if s.starts() != 0 || s.r.choices != nil {
		t.Fatalf("a program started (%d starts)", s.starts())
	}

	if !strings.Contains(s.text(), "Suggestions wait for the field Repository.") {
		t.Errorf("the wait is not said:\n%s", s.text())
	}

	s.press("shift+tab")
	s.typeText("ok")
	s.press("tab", "ctrl+l")
	s.listed()

	if _, ok := s.top().(*pickOverlay); !ok {
		t.Fatalf("the filled field did not give the list: %T\n%s", s.top(), s.text())
	}
}

// TestSuggestFailureIsReadable checks that a program that fails shows why on the status line, with
// the first line of its standard output cleaned, and that the field stays free text.
func TestSuggestFailureIsReadable(t *testing.T) {
	s := newSuggestRig(t, "fail")

	s.press("ctrl+l")
	s.listed()

	for _, want := range []string{"! Unknown option: --tags (the program exited with status 1)"} {
		if !strings.Contains(s.text(), want) {
			t.Errorf("the failure lacks %q:\n%s", want, s.text())
		}
	}

	if strings.Contains(s.text(), "\x1b[31m") || strings.Contains(stripStyles(s.r.render()), "[31m") {
		t.Errorf("the failure shows an escape sequence:\n%q", s.text())
	}

	s.typeText("v9")

	if got := s.form().fields[1].text(); got != "v9" {
		t.Errorf("the field after a failure holds %q", got)
	}

	if strings.Contains(s.text(), "Unknown option") {
		t.Errorf("the failure stays after typing:\n%s", s.text())
	}
}

// TestSuggestLongFailureKeepsTheReason checks that a long line of the program is cut and the reason
// stays whole on the status line.
func TestSuggestLongFailureKeepsTheReason(t *testing.T) {
	s := newSuggestRig(t, "longfail")

	s.press("ctrl+l")
	s.listed()

	if !strings.Contains(s.text(), "(the program exited with status 1)") ||
		!strings.Contains(s.text(), "Unknown option: 000") {
		t.Errorf("the reason was cut with the line of the program:\n%s", s.text())
	}
}

// TestSuggestEmpty checks the empty list: said on the status line, no picker, the field free.
func TestSuggestEmpty(t *testing.T) {
	s := newSuggestRig(t, "empty")

	s.press("ctrl+l")
	s.listed()

	if _, ok := s.top().(*fieldsForm); !ok {
		t.Fatalf("an empty list opened %T", s.top())
	}

	if !strings.Contains(s.text(), "No suggestion, type a value.") {
		t.Errorf("the empty list is not said:\n%s", s.text())
	}

	// An empty answer may be a passing one, so it is not kept.
	if len(s.form().sug.memo) != 0 {
		t.Errorf("an empty list was kept: %v", s.form().sug.memo)
	}

	s.press("ctrl+l")
	s.listed()

	if s.starts() != 2 {
		t.Errorf("%d starts after the second key, want 2", s.starts())
	}
}

// TestSuggestPickerFiltersWhatIsTyped checks the case-insensitive pre-filter by the text typed in
// the field, the fallback to the whole list when nothing matches, and that the list is not lost.
func TestSuggestPickerFiltersWhatIsTyped(t *testing.T) {
	s := newSuggestRig(t, "ok")

	s.typeText("V")
	s.press("ctrl+l")
	s.listed()

	p, ok := s.top().(*pickOverlay)
	if !ok {
		t.Fatalf("no picker: %T", s.top())
	}

	if len(p.items) != 3 {
		t.Errorf("%d items for V, want all three (every value holds a v)", len(p.items))
	}

	s.press("esc")
	s.typeText("2")

	s.press("ctrl+l")
	s.listed()

	p, ok = s.top().(*pickOverlay)
	if !ok || len(p.items) != 1 || p.items[0].value != "V2.0" {
		t.Fatalf("filter V2: %T %+v", s.top(), p)
	}

	if s.starts() != 1 {
		t.Errorf("the second Ctrl+L started the lister again (%d starts)", s.starts())
	}

	s.press("esc")
	s.typeText("zzz")
	s.press("ctrl+l")

	p, ok = s.top().(*pickOverlay)
	if !ok || len(p.items) != 3 {
		t.Fatalf("no match: %T %+v", s.top(), p)
	}

	if !strings.Contains(s.text(), "no match, all shown") {
		t.Errorf("the fallback is not said in the picker:\n%s", s.text())
	}

	s.press("esc")

	if !strings.Contains(s.text(), "No suggestion holds what you typed") {
		t.Errorf("the fallback is not said on the status line:\n%s", s.text())
	}

	if got := s.form().fields[1].text(); got != "V2zzz" {
		t.Errorf("Esc in the picker changed the field: %q", got)
	}
}

// TestSuggestHiddenIsCounted checks that the values the field would refuse are dropped and that
// the picker and the status line say how many.
func TestSuggestHiddenIsCounted(t *testing.T) {
	s := newSuggestRig(t, "mix")

	s.press("ctrl+l")
	s.listed()

	p, ok := s.top().(*pickOverlay)
	if !ok {
		t.Fatalf("no picker: %T\n%s", s.top(), s.text())
	}

	if len(p.items) != 3 || p.items[0].value != "good" || p.items[1].value != "-x" ||
		p.items[2].value != "ok.1" {
		t.Errorf("items %+v", p.items)
	}

	if !strings.Contains(s.text(), "1 hidden") {
		t.Errorf("the picker does not count what was hidden:\n%s", s.text())
	}

	s.press("esc")

	if !strings.Contains(s.text(), "Suggestions: 3, 1 hidden") {
		t.Errorf("the status line does not count what was hidden:\n%s", s.text())
	}
}

// TestSuggestMemoAndLimit checks that a list is kept in the form by its exact argv, that a changed
// field gives a new argv and a new run, and that a field starts at most five listings.
func TestSuggestMemoAndLimit(t *testing.T) {
	s := newSuggestRig(t, "ok")

	for i := 0; i < 3; i++ {
		s.press("ctrl+l")
		s.listed()
		s.press("esc")
	}

	if s.starts() != 1 {
		t.Fatalf("the memo did not hold: %d starts", s.starts())
	}

	f := s.form()
	if len(f.sug.memo) != 1 {
		t.Errorf("memo %v", f.sug.memo)
	}

	for i := 0; i < suggestLimit-1; i++ {
		s.press("shift+tab", "end")
		s.typeText("x")
		s.press("tab", "ctrl+l")
		s.listed()
		s.press("esc")
	}

	if s.starts() != suggestLimit {
		t.Fatalf("%d starts, want %d", s.starts(), suggestLimit)
	}

	s.press("shift+tab", "end")
	s.typeText("y")
	s.press("tab", "ctrl+l")
	s.await("the answer of the registry", func() bool { return s.form().sug.phase != sugPreparing })

	if s.starts() != suggestLimit || s.r.choices != nil {
		t.Errorf("a sixth run started (%d starts)", s.starts())
	}

	if !strings.Contains(s.text(), "No more than 5 listings") {
		t.Errorf("the limit is not said:\n%s", s.text())
	}

	s.press("shift+tab", "end", "backspace", "backspace", "backspace", "backspace", "backspace")
	s.press("tab", "ctrl+l")

	if _, ok := s.top().(*pickOverlay); !ok {
		t.Errorf("the memo no longer answers after the limit: %T", s.top())
	}
}

// TestSuggestMemoIsInMemory checks that nothing of a list is written under the directories of the
// sandbox other than the log of the fake program.
func TestSuggestMemoIsInMemory(t *testing.T) {
	s := newSuggestRig(t, "ok")

	s.press("ctrl+l")
	s.listed()
	s.press("esc")

	var found []string

	_ = filepath.WalkDir(s.lab.sb.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		if b, e := os.ReadFile(path); e == nil && strings.Contains(string(b), "recommended") &&
			path != s.log && !strings.Contains(path, "/bin/") {
			found = append(found, path)
		}

		return nil
	})

	if len(found) != 0 {
		t.Errorf("a list was written to disk: %v", found)
	}
}

// TestSuggestEnterStillMovesOn checks that Enter on the field with suggestions goes to the next
// field or submits, and starts no program.
func TestSuggestEnterStillMovesOn(t *testing.T) {
	s := newSuggestRig(t, "ok")

	s.press("enter")

	if s.starts() != 0 || s.r.choices != nil {
		t.Errorf("Enter on the last field started the lister (%d starts)", s.starts())
	}
}

// TestSuggestStaleAnswerIsDropped checks that the answer of a run that was stopped changes
// nothing, wherever it comes from.
func TestSuggestStaleAnswerIsDropped(t *testing.T) {
	s := newSuggestRig(t, "ok")
	f := s.form()

	s.send(suggestDoneMsg{Form: f, Gen: 99, Field: "tag", Key: "k", Res: plugin.ChoicesResult{
		State: plugin.ChoicesList, Items: []plugin.Choice{{Value: "late"}},
	}})
	s.send(suggestPreparedMsg{Form: f, Gen: 99, Field: "tag"})

	if _, ok := s.top().(*fieldsForm); !ok || len(f.sug.memo) != 0 {
		t.Fatalf("a stale answer was taken: %T %v", s.top(), f.sug.memo)
	}
}

// TestSuggestDashValueIsRefusedAtSubmit checks that a suggested value that starts with a dash can
// be picked (the field decides what it accepts) but that the submit refuses it: the form stays
// open and nothing starts.
func TestSuggestDashValueIsRefusedAtSubmit(t *testing.T) {
	s := newSuggestRig(t, "mix")

	s.press("ctrl+l")
	s.listed()
	s.press("down", "enter")

	if got := s.form().fields[1].text(); got != "-x" {
		t.Fatalf("the pick gave %q", got)
	}

	s.press("enter")

	if _, ok := s.top().(*fieldsForm); !ok {
		t.Fatalf("the submit of a value that starts with a dash led to %T:\n%s", s.top(), s.text())
	}

	if len(readLog(t, s.log)) != 1 {
		t.Errorf("the command started: %q", readLog(t, s.log))
	}
}

// TestSuggestArgvIsShownClean checks the argv of the status line: the secrets of the known shapes
// are hidden, control characters are cleaned and a long argv is cut in the middle, so that its
// start and its end stay.
func TestSuggestArgvIsShownClean(t *testing.T) {
	p := plugin.Prepared{
		Display: "lister --token=ghp_abcdefghijklmnopqrstuvwxyz0123456789 \x1b[31m--x\u202e -- a/b",
	}

	got := shownArgv(p)
	if strings.Contains(got, "ghp_abcdef") || strings.ContainsAny(got, "\x1b\u202e") {
		t.Errorf("the argv shown is not clean: %q", got)
	}

	s := newSuggestRig(t, "ok")
	e := s.r.e
	long := "gft --releases --plain --dir=/home/tester/Downloads/with/many/parts -- demo/proj"

	for _, w := range []int{1, 2, 3, 4, 5, 8, 20, 40, 200} {
		cut := e.midFit(long, w)
		if e.Metric.width(cut) > w {
			t.Errorf("width %d: %q is %d cells", w, cut, e.Metric.width(cut))
		}
	}

	cut := e.midFit(long, 40)
	if !strings.HasPrefix(cut, "gft --") || !strings.HasSuffix(cut, "demo/proj") ||
		!strings.Contains(cut, e.Glyphs.ellipsis) {
		t.Errorf("the middle was not cut: %q", cut)
	}

	if e.midFit(long, 200) != long {
		t.Error("an argv that fits was cut")
	}

	wide := "gft --releases --dir=/home/\u6d4b\u8bd5\u6587\u4ef6/\u4e0b\u8f7d\u76ee\u5f55 " +
		"-- \u4ed3\u5e93/\u9879\u76ee"

	for w := 5; w < e.Metric.width(wide); w++ {
		if cut := e.midFit(wide, w); e.Metric.width(cut) > w {
			t.Errorf("width %d: %q is %d cells", w, cut, e.Metric.width(cut))
		}
	}
}

// TestSuggestPickerIsClean checks that what a program says reaches the picker cleaned: a value or
// a label with escape sequences or a reversal of the text direction never gets there as it is.
func TestSuggestPickerIsClean(t *testing.T) {
	s := newSuggestRig(t, "ok")
	f := s.form()

	pick, _ := f.pickList(s.r.e, "tag", plugin.ChoicesResult{Items: []plugin.Choice{
		{Value: "v\x1b[31m1", Label: "\x1b]0;owned\x07label \u202e text"},
	}})

	s.r.push(pick)

	text := s.text()
	if strings.ContainsAny(s.r.render(), "\u202e\x07") || strings.Contains(s.r.render(), "\x1b]") {
		t.Errorf("the picker shows a control sequence:\n%q", s.r.render())
	}

	if !strings.Contains(text, "label") {
		t.Errorf("the label is gone:\n%s", text)
	}
}

// twoTagsManifest is suggestManifest with a second text field that lists the same program with the
// same argv as the tag but has no pattern: the two fields do not filter a list alike.
var twoTagsManifest = strings.Replace(strings.Replace(suggestManifest,
	`pattern = "^[A-Za-z0-9._-]{0,24}$"`, `pattern = "^[a-z0-9][A-Za-z0-9._-]{0,24}$"`, 1),
	`[[command]]
id = "plain"`, `[[command.field]]
id = "tag2"
type = "text"
label = "Tag two"
choices_from = ["lister", "--tags", "{name}"]

[[command]]
id = "plain"`, 1)

// TestSuggestMemoIsPerField checks that the list of a field is not given to another field that has
// the same argv: the first list was filtered by the pattern of its own field.
func TestSuggestMemoIsPerField(t *testing.T) {
	s := newSuggestRigOf(t, "mix", twoTagsManifest)

	// The second tag has no pattern: it keeps -x. The first one keeps what starts with a letter or
	// a digit.
	s.press("tab", "ctrl+l")
	s.listed()

	p, ok := s.top().(*pickOverlay)
	if !ok || len(p.items) != 3 || p.items[1].value != "-x" {
		t.Fatalf("the lax field: %T %+v", s.top(), s.top())
	}

	s.press("esc", "shift+tab", "ctrl+l")
	s.listed()

	p, ok = s.top().(*pickOverlay)
	if !ok || len(p.items) != 2 || p.items[0].value != "good" || p.items[1].value != "ok.1" {
		t.Fatalf("the strict field got the list of the other: %T", s.top())
	}

	if s.starts() != 2 {
		t.Errorf("%d starts, want one per field", s.starts())
	}
}
