package ui

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/store"
)

// playExec plays a command made by tea.Exec the way Bubble Tea does: it takes the command and the
// callback out of the unexported message, connects the command to the given input, runs it and
// returns what the callback makes of the end. It lets a test drive the real chain from a key
// press to the program that is started and back to the model, which a hand made execDoneMsg
// cannot check.
//
// Parameters:
//   - t: running test
//   - cmd: command returned by the model for a program to run
//   - in: text the program may read, such as the Enter that ends the pause
//
// Returns the message that the callback produced.
func playExec(t *testing.T, cmd tea.Cmd, in string) tea.Msg {
	t.Helper()

	return playExecWith(t, cmd, in, io.Discard)
}

// playExecWith is playExec with the output of the program given.
//
// Parameters:
//   - t: running test
//   - cmd: command returned by the model for a program to run
//   - in: text the program may read
//   - out: receives what the command writes to the terminal
func playExecWith(t *testing.T, cmd tea.Cmd, in string, out io.Writer) tea.Msg {
	t.Helper()

	if cmd == nil {
		t.Fatal("no command was returned")
	}

	msg := cmd()
	v := reflect.New(reflect.TypeOf(msg)).Elem()
	v.Set(reflect.ValueOf(msg))

	field := func(name string) reflect.Value {
		f := v.FieldByName(name)
		if !f.IsValid() {
			t.Fatalf("%T has no field %q", msg, name)
		}

		return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
	}

	c, ok := field("cmd").Interface().(tea.ExecCommand)
	fn, ok2 := field("fn").Interface().(tea.ExecCallback)

	if !ok || !ok2 {
		t.Fatalf("%T is not an exec message", msg)
	}

	c.SetStdin(strings.NewReader(in))
	c.SetStdout(out)
	c.SetStderr(out)

	return fn(c.Run())
}

// TestReviewEditorAfterOpen drives the editor of a repository through the real chain for each
// repos.after_open value. TestEditor builds the execDoneMsg by hand, so it never sees what the
// model puts in it: a model that forgets the directory of "cd" passed it.
//
// Parameters:
//   - t: test to report to
func TestReviewEditorAfterOpen(t *testing.T) {
	tests := []struct {
		after   string
		quit    bool
		action  shell.Action
		payload func(*sandbox) string
	}{
		{"return", false, "", func(*sandbox) string { return "" }},
		{"quit", true, "", func(*sandbox) string { return "" }},
		{"cd", true, shell.ChangeDir, func(s *sandbox) string { return s.Repo }},
	}

	for _, tt := range tests {
		s := newSandbox(t)
		run := &fakeRunner{}
		g := newRig(t, s.with(func(o *Options) {
			o.Start = StartRepos
			o.Runner = run
			o.Settings.Repos.AfterOpen = tt.after
			o.Environ = append(o.Environ, "EDITOR=myeditor --wait")
		}))

		g.r.delay = 0

		g.send(playExec(t, g.r.openInEditor("proj"), "\n"))

		calls := run.calls()
		if len(calls) != 1 || strings.Join(calls[0].Argv, " ") != "myeditor --wait "+s.Repo {
			t.Fatalf("%s: calls = %+v", tt.after, calls)
		}

		if g.Quit != tt.quit || g.r.result.Action != tt.action ||
			g.r.result.Payload != tt.payload(s) {
			t.Errorf("%s: quit %v, result %+v, want action %q payload %q", tt.after, g.Quit,
				g.r.result, tt.action, tt.payload(s))
		}
	}
}

// TestReviewConnectEnvironment checks the program a connection starts: the exact arguments, the
// environment of the options (not the one of the test process) and the pause after a failure.
//
// Parameters:
//   - t: test to report to
func TestReviewConnectEnvironment(t *testing.T) {
	s := newSandbox(t)
	run := &fakeRunner{Res: RunResult{ExitCode: 255}}
	g := newRig(t, s.with(func(o *Options) {
		o.Start = StartConnections
		o.Runner = run
		o.Environ = append(o.Environ, "TOTE_REVIEW_MARK=1")
	}))

	g.r.delay = 0

	g.send(playExec(t, settle(g, g.r.activate(activateMsg{Kind: kindConn, Key: "prod"})), "\n"))

	calls := run.calls()
	if len(calls) != 1 || strings.Join(calls[0].Argv, " ") != "ssh -l deploy -- prod.example.com" {
		t.Fatalf("calls = %+v", calls)
	}

	found := false

	for _, kv := range calls[0].Env {
		found = found || kv == "TOTE_REVIEW_MARK=1"
	}

	if !found {
		t.Errorf("the program does not get the environment of the options: %v", calls[0].Env)
	}

	if !strings.Contains(g.toastText(), "255") || !g.r.toast.err {
		t.Errorf("a failed ssh left no error toast: %q", g.toastText())
	}
}

// TestReviewRepositoryIsAFile checks that Enter on a repository whose path is a regular file is
// refused: the shell would be told to change into something that is not a directory.
//
// Parameters:
//   - t: test to report to
func TestReviewRepositoryIsAFile(t *testing.T) {
	s := newSandbox(t)
	file := filepath.Join(s.Dir, "afile")
	writeFile(t, file, "x")
	writeFile(t, s.Opts.Paths.ReposFile(),
		"schema = 1\n\n[[repo]]\nname = \"f\"\npath = \""+file+"\"\n")

	g := newRig(t, s.with(func(o *Options) { o.Start = StartRepos }))

	g.press("enter")

	if g.Quit || g.r.result.Action != "" || !g.r.toast.err ||
		!strings.Contains(g.toastText(), "not found") {
		t.Errorf("quit %v, result %+v, toast %q", g.Quit, g.r.result, g.toastText())
	}
}

// TestReviewNeedsReviewMessage checks that a connection flagged for review is refused with the
// message made for it and that nothing is started.
//
// Parameters:
//   - t: test to report to
func TestReviewNeedsReviewMessage(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))

	g.r.activate(activateMsg{Kind: kindConn, Key: "legacy"})

	want := g.r.e.t("cli_needs_review", "legacy")

	if g.r.toast.text != want || !g.r.toast.err || g.r.guard.busy.Load() != 0 {
		t.Errorf("toast %q (err %v), want %q and no program", g.r.toast.text, g.r.toast.err, want)
	}
}

// TestReviewFilterEnterManyMatches checks that Enter in the filter only runs a record when the
// filter left exactly one: with several matches Enter must leave the typing mode and run
// nothing, whatever record is selected.
//
// Parameters:
//   - t: test to report to
func TestReviewFilterEnterManyMatches(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = StartConnections }))
	l := g.r.scr.(*listScreen)

	g.press("/")
	g.typeText("e")

	if len(l.shown) < 2 {
		t.Fatalf("the filter must match several records, got %d", len(l.shown))
	}

	g.press("enter")

	if l.Typing() || g.Quit || g.r.guard.busy.Load() != 0 {
		t.Errorf("typing %v, quit %v, busy %d: Enter ran a record among %d matches", l.Typing(),
			g.Quit, g.r.guard.busy.Load(), len(l.shown))
	}
}

// TestReviewDeleteOnReadOnlyFile checks that d on a list whose file cannot be read opens no
// confirmation and says why, on a fresh model so that an older message cannot hide the result.
//
// Parameters:
//   - t: test to report to
func TestReviewDeleteOnReadOnlyFile(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.AliasesFile(), "schema = 1\n[[alias\n")

	g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases }))

	g.send(deleteMsg{Kind: kindAlias, Key: "ll"})

	if len(g.r.overlays) != 0 || !g.r.toast.err ||
		!strings.Contains(g.toastText(), "syntax error") {
		t.Errorf("overlays %d, toast %q (err %v)", len(g.r.overlays), g.toastText(), g.r.toast.err)
	}
}

// TestReviewFormDismissWritesNothing checks that a dismissal of the form, which is how the root
// asks an overlay to go away, never writes the record, even when every field is valid.
//
// Parameters:
//   - t: test to report to
func TestReviewFormDismissWritesNothing(t *testing.T) {
	s := newSandbox(t)
	g := openNew(t, s, StartAliases)
	g.play("zz", "@tab", "echo hi")

	ov, cmd, done := g.top().Update(g.r.e, dismissMsg{})

	if !done || ov == nil || len(collect(cmd)) != 0 {
		t.Errorf("done %v, messages %v", done, collect(cmd))
	}

	c, err := store.OpenAliases(s.Opts.Paths.AliasesFile())
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := c.Get("zz"); ok {
		t.Error("dismissing the form wrote the alias")
	}
}

// TestReviewFormWriteOperation checks the operation that a form reports: an addition for a new
// record and an edition for an existing one.
//
// Parameters:
//   - t: test to report to
func TestReviewFormWriteOperation(t *testing.T) {
	g := openNew(t, newSandbox(t), StartAliases)
	g.play("zz", "@tab", "x", "@ctrl+s")

	if m := lastWrote(g); m == nil || m.Op != opAdd || m.Key != "zz" {
		t.Errorf("add: %+v", m)
	}

	g = openEdit(t, newSandbox(t), StartAliases)
	g.play("@tab,end", " -h", "@ctrl+s")

	if m := lastWrote(g); m == nil || m.Op != opEdit || m.Key != "ll" {
		t.Errorf("edit: %+v", m)
	}
}

// lastWrote returns the last write message the rig has seen.
//
// Parameters:
//   - g: rig to look in
func lastWrote(g *rig) *wroteMsg {
	for i := len(g.Seen) - 1; i >= 0; i-- {
		if m, ok := g.Seen[i].(wroteMsg); ok {
			return &m
		}
	}

	return nil
}

// TestReviewSaveRefusesBrokenFile checks the guard of the dataset itself: a record is not written
// through a collection whose file could not be read, and the file is left as it was.
//
// Parameters:
//   - t: test to report to
func TestReviewSaveRefusesBrokenFile(t *testing.T) {
	s := newSandbox(t)
	broken := "schema = 1\n[[alias\n"
	writeFile(t, s.Opts.Paths.AliasesFile(), broken)

	g := newRig(t, s)
	err := g.r.e.Data.save(kindAlias, "", store.Alias{Name: "zz", Cmd: "x"})

	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("save = %v, want the read-only explanation", err)
	}

	if got, _ := os.ReadFile(s.Opts.Paths.AliasesFile()); string(got) != broken {
		t.Errorf("the broken file was changed: %q", got)
	}
}

// TestReviewRankUsesTheBestName checks that an entry is ranked by its best name: a connection
// whose alias is exactly the query comes before one whose name only starts with it, although its
// own name merely contains the query.
//
// Parameters:
//   - t: test to report to
func TestReviewRankUsesTheBestName(t *testing.T) {
	entries := []entry{
		{Scope: scopeConn, Name: "pq", Names: []string{"pq"}, Text: "pq"},
		{Scope: scopeConn, Name: "apple", Names: []string{"apple", "p"}, Text: "apple p"},
	}

	got := rank(entries, scopeAll, "p")

	if len(got) != 2 || got[0] != 1 {
		t.Errorf("rank = %v, want the entry with the exact alias first", got)
	}
}

// TestReviewDetectProfileInTmux checks the rule that raises a 256 color detection to true color
// when the tmux client reports RGB. The rows of TestDetectProfile that name tmux also set
// TOTE_COLOR, which answers before the rule is reached, so they pass without it.
//
// Parameters:
//   - t: test to report to
func TestReviewDetectProfileInTmux(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	base := []string{"TERM=xterm-256color", "TTY_FORCE=1"}
	withTmux := append([]string{"TMUX=/tmp/t,1,0"}, base...)

	tests := []struct {
		name  string
		env   []string
		probe string
		want  colorprofile.Profile
		asked bool
	}{
		{"rgb in tmux", withTmux, "256,RGB,mouse", colorprofile.TrueColor, true},
		{"no rgb in tmux", withTmux, "256,mouse", colorprofile.ANSI256, true},
		{"rgb outside tmux is not asked", base, "256,RGB", colorprofile.ANSI256, false},
	}

	for _, tt := range tests {
		asked := false
		probe := func() string {
			asked = true

			return tt.probe
		}

		if got := detectProfile(tt.env, &bytes.Buffer{}, probe); got != tt.want ||
			asked != tt.asked {
			t.Errorf("%s: profile %v asked %v, want %v and %v", tt.name, got, asked, tt.want,
				tt.asked)
		}
	}
}

// TestReviewEditorKeyOnlyForRepositories checks that the editor key does nothing in the other
// lists instead of reporting a missing repository.
//
// Parameters:
//   - t: test to report to
func TestReviewEditorKeyOnlyForRepositories(t *testing.T) {
	for _, start := range []Start{StartConnections, StartAliases, StartFuncs} {
		g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = start }))

		g.press("o")

		if g.toastText() != "" || g.r.guard.busy.Load() != 0 || len(g.r.overlays) != 0 {
			t.Errorf("start %d: toast %q busy %d overlays %d", start, g.toastText(),
				g.r.guard.busy.Load(), len(g.r.overlays))
		}
	}
}

// TestReviewReloadKeepsTheSelectedRecord checks that a reload after the file changed keeps the
// cursor on the same record, not on the same row.
//
// Parameters:
//   - t: test to report to
func TestReviewReloadKeepsTheSelectedRecord(t *testing.T) {
	s := newSandbox(t)
	g := newRig(t, s.with(func(o *Options) { o.Start = StartAliases }))

	g.press("down")

	if g.r.scr.(*listScreen).selected().Key != "gs" {
		t.Fatal("the second alias is not selected")
	}

	writeFile(t, s.Opts.Paths.AliasesFile(), "schema = 1\n\n[[alias]]\nname = \"aa\"\n"+
		"cmd = \"x\"\n\n[[alias]]\nname = \"ll\"\ncmd = \"ls -la\"\n\n[[alias]]\nname = \"gs\"\n"+
		"cmd = \"git status\"\n")

	g.press("ctrl+r")

	if got := g.r.scr.(*listScreen).selected().Key; got != "gs" {
		t.Errorf("selected %q after the reload, want gs", got)
	}
}

// TestReviewExecDelay checks that the program starts only after the pause that lets the renderer
// stop its ticker, the first of the mitigations for the frozen screen after a quick command.
//
// Parameters:
//   - t: test to report to
func TestReviewExecDelay(t *testing.T) {
	const delay = 40 * time.Millisecond

	var started time.Time

	run := runnerFunc(func(RunSpec) RunResult {
		started = time.Now()

		return RunResult{}
	})

	c := newExecCommand(run, RunSpec{Argv: []string{"x"}}, "", "x", newPrinter("en"),
		time.Now, delay)
	c.SetStdin(strings.NewReader(""))
	c.SetStdout(io.Discard)
	c.SetStderr(io.Discard)

	begin := time.Now()

	if err := c.Run(); err != nil {
		t.Fatal(err)
	}

	if started.Sub(begin) < delay {
		t.Errorf("the program started %v after Run, want at least %v", started.Sub(begin), delay)
	}
}

// TestReviewExecKeepsItsOwnStreams checks that streams given in the spec are not replaced by
// the ones Bubble Tea offers.
//
// Parameters:
//   - t: test to report to
func TestReviewExecKeepsItsOwnStreams(t *testing.T) {
	mine := strings.NewReader("mine")
	out, errw := &strings.Builder{}, &strings.Builder{}

	var got RunSpec

	run := runnerFunc(func(spec RunSpec) RunResult {
		got = spec

		return RunResult{}
	})

	c := newExecCommand(run, RunSpec{Argv: []string{"x"}, Stdin: mine, Stdout: out, Stderr: errw},
		"", "x", newPrinter("en"), time.Now, 0)
	c.SetStdin(strings.NewReader("theirs"))
	c.SetStdout(io.Discard)
	c.SetStderr(io.Discard)

	if err := c.Run(); err != nil {
		t.Fatal(err)
	}

	if got.Stdin != io.Reader(mine) || got.Stdout != io.Writer(out) ||
		got.Stderr != io.Writer(errw) {
		t.Errorf("streams were replaced: %+v", got)
	}
}

// TestReviewPanicResetsTheTerminal checks that a crash writes the reset sequences of the
// program itself. Bubble Tea also leaves the alternate screen and shows the cursor, so those
// two alone cannot tell that ResetTerminal ran.
//
// Parameters:
//   - t: test to report to
func TestReviewPanicResetsTheTerminal(t *testing.T) {
	s := newSandbox(t).with(func(o *Options) {
		o.Environ = append(o.Environ, "TOTE_DEBUG_PANIC=1")
	})

	pr, pw := io.Pipe()
	out := newLockedBuffer()
	o := s.Opts
	o.Input, o.Output = pr, out

	go func() { _, _ = pw.Write([]byte("j")) }()

	_, err := Run(o)

	_ = pw.Close()

	if err == nil {
		t.Fatal("a crash was not reported")
	}

	if !strings.Contains(out.String(), panicReset) {
		t.Errorf("the reset sequence was not written: %q", out.String())
	}
}

// TestReviewRunIgnoresSigintAroundAProgram runs the whole program loop and delivers real SIGINT
// signals to the process while a program runs and just after it returned, as the terminal does
// for a Ctrl-C meant for the child. The interface must stay open and come back to the list; it
// then quits with q. A second signal handler, such as the one Bubble Tea installs by default,
// would interrupt it.
//
// Parameters:
//   - t: test to report to
func TestReviewRunIgnoresSigintAroundAProgram(t *testing.T) {
	self := os.Getpid()

	run := runnerFunc(func(RunSpec) RunResult {
		_ = syscall.Kill(self, syscall.SIGINT)

		time.Sleep(50 * time.Millisecond)

		go func() {
			time.Sleep(30 * time.Millisecond)
			_ = syscall.Kill(self, syscall.SIGINT)
		}()

		return RunResult{}
	})

	s := newSandbox(t).with(func(o *Options) {
		o.Start = StartConnections
		o.Runner = run
		o.Now = time.Now
	})

	res, err, _ := runPaced(t, s, 200*time.Millisecond, "\r", "q")

	if err != nil || !res.Empty() {
		t.Errorf("Run = %+v, %v, want a clean quit after the program", res, err)
	}
}

// TestReviewResetSequences pins the sequences that restore a terminal, one by one, as the
// mitigations list them: the mode reset written after a program must never leave the alternate
// screen, and the one written after a crash must end with the exit of the alternate screen and a
// cleared main screen.
//
// Parameters:
//   - t: test to report to
func TestReviewResetSequences(t *testing.T) {
	for _, seq := range []string{
		"\x1b[?1000l", "\x1b[?1002l", "\x1b[?1003l", "\x1b[?1005l", "\x1b[?1006l", "\x1b[?1015l",
		"\x1b[?1004l", "\x1b[?2004l", "\x1b[?1l", "\x1b>", "\x1b[?25h", "\x1b[0m", "\x1b[<u",
		"\x1b[>4m",
	} {
		if !strings.Contains(modeReset, seq) {
			t.Errorf("modeReset lacks %q", seq)
		}
	}

	if strings.Contains(modeReset, "\x1b[?1049l") {
		t.Error("modeReset leaves the alternate screen, which restores an old cursor on tmux 3.4")
	}

	if want := modeReset + "\x1b[?1049l\x1b[2J\x1b[H"; panicReset != want {
		t.Errorf("panicReset = %q, want %q", panicReset, want)
	}
}
