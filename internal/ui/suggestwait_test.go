package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/totecli/tote/internal/plugin"
)

// release lets the lister of a slow value answer.
func (s *suggestRig) release() {
	s.t.Helper()

	if err := os.WriteFile(s.gate, nil, 0o600); err != nil {
		s.t.Fatal(err)
	}
}

// loading starts the suggestions of a slow value and checks that the loading box is over the form.
//
// Parameters:
//   - t: running test
//   - value: the repository, such as slowok
func loading(t *testing.T, value string) (*suggestRig, *fieldsForm, int) {
	t.Helper()

	s := newSuggestRig(t, value)
	f := s.form()
	before := len(s.r.overlays)

	s.press("ctrl+l")

	w, ok := s.top().(*suggestWait)
	if !ok || s.r.choices == nil || s.r.choices.wait != w {
		t.Fatalf("Ctrl+L did not open the loading box, the top is %T:\n%s", s.top(), s.text())
	}

	if len(s.r.overlays) != before+1 || s.r.overlays[before-1] != overlay(f) {
		t.Fatalf("the loading box is not over the form: %d overlays", len(s.r.overlays))
	}

	return s, f, s.pid()
}

// TestSuggestLoadingBox checks what a person sees while the program runs: the title of the list, a
// muted row with the spinner and a word, the argv on the line under it, Esc to cancel, and no list.
func TestSuggestLoadingBox(t *testing.T) {
	s, _, _ := loading(t, "slowok")

	for _, want := range []string{
		"Suggestions: Tag", "\u280b Loading", "lister --tags slowok", "Listing: lister --tags slowok",
	} {
		if !strings.Contains(s.text(), want) {
			t.Errorf("the loading box lacks %q:\n%s", want, s.text())
		}
	}

	if got := s.footerText(); !strings.Contains(got, "esc") || !strings.Contains(got, "cancel") {
		t.Errorf("the footer does not offer to cancel: %q", got)
	}

	if strings.Contains(s.text(), "v1.0") {
		t.Errorf("the box shows a list before the program answered:\n%s", s.text())
	}
}

// TestSuggestLoadingToReady checks that the list takes the place of the loading box and that the
// keys pressed while it loaded went nowhere.
func TestSuggestLoadingToReady(t *testing.T) {
	s, f, _ := loading(t, "slowok")
	depth := len(s.r.overlays)

	s.typeText("zz")
	s.press("enter", "tab", "ctrl+s", "ctrl+l", "down")

	if got := f.fields[1].text(); got != "" {
		t.Errorf("the keys typed while loading reached the field: %q", got)
	}

	if s.starts() != 1 {
		t.Errorf("a second Ctrl+L started a second run (%d starts)", s.starts())
	}

	if _, ok := s.top().(*suggestWait); !ok {
		t.Fatalf("a key closed the loading box: %T", s.top())
	}

	s.release()
	s.listed()

	pick, ok := s.top().(*pickOverlay)
	if !ok {
		t.Fatalf("the list did not open: %T:\n%s", s.top(), s.text())
	}

	if len(s.r.overlays) != depth || s.r.overlays[depth-2] != overlay(f) {
		t.Errorf("the list is not in the place of the box: %d overlays, want %d",
			len(s.r.overlays), depth)
	}

	if pick.cur != 0 || pick.off != 0 || len(pick.items) != 3 {
		t.Errorf("the list opened with cursor %d, offset %d, %d items",
			pick.cur, pick.off, len(pick.items))
	}

	for _, want := range []string{"Suggestions: Tag", "v1.0", "first", "V2.0", "v3.0", "recommended"} {
		if !strings.Contains(s.text(), want) {
			t.Errorf("the list lacks %q:\n%s", want, s.text())
		}
	}

	if strings.Contains(s.text(), "Loading") {
		t.Errorf("the list still says it loads:\n%s", s.text())
	}

	if f.sug.phase != sugReady || s.r.choices != nil {
		t.Errorf("phase %v, run %v", f.sug.phase, s.r.choices)
	}

	s.press("enter")

	if got := f.fields[1].text(); got != "v1.0" {
		t.Errorf("Enter on the first row gave %q", got)
	}
}

// TestSuggestLoadingToFailed checks that a run that gives no list closes the box and says why on
// the status line of the form: no empty box stays open.
func TestSuggestLoadingToFailed(t *testing.T) {
	tests := []struct {
		name  string
		value string
		phase suggestPhase
		want  string
	}{
		{"failure", "slowfail", sugFailed, "Unknown option: --tags"},
		{"empty", "slowempty", sugEmpty, "No suggestion, type a value."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, f, _ := loading(t, tt.value)
			depth := len(s.r.overlays) - 1

			s.release()
			s.listed()

			if _, ok := s.top().(*fieldsForm); !ok || len(s.r.overlays) != depth {
				t.Fatalf("the box was not closed: %T, %d overlays:\n%s", s.top(), len(s.r.overlays), s.text())
			}

			if f.sug.phase != tt.phase || !strings.Contains(s.text(), tt.want) {
				t.Errorf("phase %v, status line without %q:\n%s", f.sug.phase, tt.want, s.text())
			}
		})
	}

	t.Run("timeout", func(t *testing.T) {
		s, f, _ := loading(t, "slowok")

		s.send(suggestDoneMsg{
			Form: f, Gen: s.r.choices.gen, Field: "tag",
			Res: plugin.ChoicesResult{State: plugin.ChoicesTimedOut, Reason: plugin.ChoicesTimedOut},
		})

		if _, ok := s.top().(*fieldsForm); !ok || f.sug.phase != sugFailed || s.r.choices != nil {
			t.Fatalf("the timeout left %T, phase %v:\n%s", s.top(), f.sug.phase, s.text())
		}

		if !strings.Contains(s.text(), "10 seconds") {
			t.Errorf("the status line does not say the program took too long:\n%s", s.text())
		}
	})
}

// TestSuggestLoadingCancelled checks Esc and a click away: the program group is ended, the box
// closes, the form says the listing stopped, and the answer that comes late changes nothing.
func TestSuggestLoadingCancelled(t *testing.T) {
	for _, how := range []string{"esc", "ctrl+c", "dismiss"} {
		t.Run(how, func(t *testing.T) {
			s, f, pid := loading(t, "slowok")
			gen, depth := s.r.choices.gen, len(s.r.overlays)-1

			if how == "dismiss" {
				s.send(dismissMsg{})
			} else {
				s.press(how)
			}

			if _, ok := s.top().(*fieldsForm); !ok || len(s.r.overlays) != depth {
				t.Fatalf("the form is not back: %T, %d overlays, want %d", s.top(), len(s.r.overlays), depth)
			}

			if s.r.choices != nil || f.sug.phase != sugStopped {
				t.Errorf("run %v, phase %v", s.r.choices, f.sug.phase)
			}

			if !strings.Contains(s.text(), "Listing stopped.") {
				t.Errorf("nothing says the listing stopped:\n%s", s.text())
			}

			s.gone(pid)

			s.send(suggestDoneMsg{
				Form: f, Gen: gen, Field: "tag", Key: "k",
				Res: plugin.ChoicesResult{State: plugin.ChoicesList, Items: []plugin.Choice{{Value: "late"}}},
			})

			if _, ok := s.top().(*fieldsForm); !ok || len(f.sug.memo) != 0 {
				t.Errorf("a late answer was taken: %T, memo %v", s.top(), f.sug.memo)
			}
		})
	}
}

// unplayed is the message of a timer that a test does not let run.
type unplayed struct{}

// ticking replaces the timers of the interface by a recorder, so that a test sees which ticks are
// asked for and plays them itself.
//
// Parameters:
//   - t: running test
//
// Returns the delays of the ticks asked for since, in order.
func ticking(t *testing.T) *[]time.Duration {
	t.Helper()

	var asked []time.Duration

	saved := tick
	tick = func(d time.Duration, _ func(time.Time) tea.Msg) tea.Cmd {
		asked = append(asked, d)

		return func() tea.Msg { return unplayed{} }
	}

	t.Cleanup(func() { tick = saved })

	return &asked
}

// TestSuggestSpinnerTicksWhileTheRunGoes checks the spinner: one tick is asked for at the start and
// one more after each tick, and the box shows the frame and the seconds.
func TestSuggestSpinnerTicksWhileTheRunGoes(t *testing.T) {
	asked := ticking(t)
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)

	s := newSuggestRig(t, "slowok")
	s.r.e.Now = func() time.Time { return now }
	s.press("ctrl+l")

	if len(*asked) != 1 || (*asked)[0] != spinDelay {
		t.Fatalf("the start asked for the ticks %v, want one of %v", *asked, spinDelay)
	}

	gen := s.r.choices.gen
	sug := s.r.choices.form.sug

	for i := 1; i <= 3; i++ {
		now = now.Add(spinDelay)
		s.send(suggestTickMsg{Gen: gen})

		if sug.spin != i || sug.secs != 0 {
			t.Fatalf("tick %d: frame %d, %d seconds", i, sug.spin, sug.secs)
		}
	}

	if len(*asked) != 4 {
		t.Errorf("the ticks asked for are %v, want one more after each tick", *asked)
	}

	now = now.Add(4 * time.Second)
	s.send(suggestTickMsg{Gen: gen})

	frame := s.r.e.spinner(sug.spin)

	if sug.secs != 4 || !strings.Contains(s.text(), "4 s") ||
		!strings.Contains(s.text(), frame+" Loading") {
		t.Errorf("frame %d, %d seconds:\n%s", sug.spin, sug.secs, s.text())
	}

	if !strings.Contains(s.text(), frame+" Listing:") {
		t.Errorf("the status line has no spinner:\n%s", s.text())
	}
}

// TestSuggestTickStopsWithTheRun checks that no tick outlives the run: a tick that comes late asks
// for no other one and moves nothing, so that nothing redraws while the form is idle.
func TestSuggestTickStopsWithTheRun(t *testing.T) {
	asked := ticking(t)

	tests := []struct {
		name string
		end  func(s *suggestRig)
	}{
		{"answer", func(s *suggestRig) {
			s.release()
			s.listed()
		}},
		{"esc", func(s *suggestRig) { s.press("esc") }},
		{"form closed", func(s *suggestRig) { s.press("esc", "esc") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, f, _ := loading(t, "slowok")
			gen := s.r.choices.gen

			tt.end(s)

			before, spin := len(*asked), f.sug.spin

			if cmd := s.r.suggestTick(suggestTickMsg{Gen: gen}); cmd != nil {
				t.Error("a tick after the end of the run asked for another")
			}

			s.send(suggestTickMsg{Gen: gen})

			if len(*asked) != before || f.sug.spin != spin {
				t.Errorf("a tick after the end moved the spinner: %d ticks asked, frame %d",
					len(*asked)-before, f.sug.spin)
			}
		})
	}

	t.Run("other run", func(t *testing.T) {
		s, _, _ := loading(t, "slowok")
		before := len(*asked)

		cmd := s.r.suggestTick(suggestTickMsg{Gen: s.r.choices.gen + 1})

		if cmd != nil || len(*asked) != before {
			t.Error("the tick of another run was taken")
		}
	})
}

// TestSuggestStartsNothingBeforeTheKeyNorTicks checks that opening the form and moving in it asks
// for no tick: the spinner starts with the program, which starts with Ctrl+L.
func TestSuggestStartsNothingBeforeTheKeyNorTicks(t *testing.T) {
	asked := ticking(t)
	s := newSuggestRig(t, "slowok")

	s.typeText("v")
	s.press("shift+tab", "tab", "up", "down")

	if len(*asked) != 0 || s.starts() != 0 || s.r.choices != nil {
		t.Errorf("ticks %v, %d starts, run %v", *asked, s.starts(), s.r.choices)
	}
}

// TestSuggestNoBoxOverHelp checks that a run that starts while the help is over the form opens no
// loading box: the status line of the form says what runs and the list opens once the form is back.
func TestSuggestNoBoxOverHelp(t *testing.T) {
	asked := ticking(t)
	s := newSuggestRig(t, "slowok")
	f := s.form()
	v := f.values()

	cmd := s.r.suggestAsk(suggestAskMsg{
		Form: f, Field: "tag", Call: s.r.e.callOf(f.sug.start, f.sug.fields, &v),
	})
	f.sug.begin("tag")

	s.press("f1")

	for _, m := range collect(cmd) {
		s.send(m)
	}

	if s.r.choices == nil || s.r.choices.wait != nil || len(*asked) != 1 {
		t.Fatalf("run %+v, ticks %v", s.r.choices, *asked)
	}

	if _, ok := s.top().(*helpOverlay); !ok {
		t.Fatalf("the help is not on top: %T", s.top())
	}

	s.press("esc")

	_, onForm := s.top().(*fieldsForm)

	if !onForm || !strings.Contains(s.text(), "\u280b Listing: lister --tags slowok") {
		t.Fatalf("the form does not say what runs, the top is %T:\n%s", s.top(), s.text())
	}

	s.release()
	s.listed()

	if _, ok := s.top().(*pickOverlay); !ok {
		t.Errorf("the list did not open: %T", s.top())
	}
}

// TestSuggestLoadingHoldsHelp checks that the help does not open over the loading box: the form
// would not be on top when the list arrives, and the list would be lost.
//
// Parameters:
//   - t: test to report to
func TestSuggestLoadingHoldsHelp(t *testing.T) {
	s, _, _ := loading(t, "slowok")
	n := len(s.r.overlays)

	for _, k := range []tea.KeyPressMsg{keyMsg("?"), {Code: tea.KeyF1}} {
		s.send(k)

		if _, ok := s.top().(*suggestWait); !ok || len(s.r.overlays) != n {
			t.Fatalf("%q opened the help over the loading box, the top is %T", k.String(), s.top())
		}
	}

	s.release()
	s.listed()

	if _, ok := s.top().(*pickOverlay); !ok {
		t.Errorf("the list did not open: %T", s.top())
	}
}
