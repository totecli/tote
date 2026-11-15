package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// hanging opens the form, types the repository that makes the fake lister wait, starts the
// suggestions and returns the process id of the program that waits.
//
// Parameters:
//   - t: running test
func hanging(t *testing.T) (*suggestRig, int) {
	t.Helper()

	s := newSuggestRig(t, "hang")
	s.press("ctrl+l")

	if s.r.choices == nil || s.r.choices.finished == nil {
		t.Fatalf("no program runs:\n%s", s.text())
	}

	return s, s.pid()
}

// TestSuggestRunDiesOnEveryWayOut checks that every way out of the form or the program ends the
// process group of a suggestions run. The table says what to do and what the root holds afterwards.
func TestSuggestRunDiesOnEveryWayOut(t *testing.T) {
	tests := []struct {
		name string
		do   func(t *testing.T, s *suggestRig)
		// alive is true when the run must still go afterwards.
		alive bool
	}{
		{"esc", func(_ *testing.T, s *suggestRig) { s.press("esc") }, false},
		{"first ctrl+c stops the run and keeps the form", func(t *testing.T, s *suggestRig) {
			s.press("ctrl+c")

			if _, ok := s.top().(*fieldsForm); !ok {
				t.Errorf("Ctrl+C closed the form: %T", s.top())
			}
		}, false},
		{"submit", func(_ *testing.T, s *suggestRig) { s.press("ctrl+s") }, false},
		{"enter on the last field", func(_ *testing.T, s *suggestRig) { s.press("enter") }, false},
		{"close", func(_ *testing.T, s *suggestRig) { s.send(dismissMsg{}) }, false},
		{"esc twice", func(_ *testing.T, s *suggestRig) { s.press("esc", "esc") }, false},
		{"ctrl+c twice", func(_ *testing.T, s *suggestRig) { s.press("ctrl+c", "ctrl+c") }, false},
		{"form removed from the stack", func(_ *testing.T, s *suggestRig) {
			s.r.overlays = s.r.overlays[:len(s.r.overlays)-1]
			s.press("ctrl+k")
		}, false},
		{"disable from inside", func(t *testing.T, s *suggestRig) {
			p, ok := s.lab.reg.Recheck("taglist")
			if !ok {
				t.Fatal("no plugin")
			}

			s.r.toggle(p)
		}, false},
		{"disable from outside", func(t *testing.T, s *suggestRig) {
			if _, err := s.lab.reg.Disable("taglist"); err != nil {
				t.Fatal(err)
			}

			s.awaitEnded()
		}, false},
		{"remove", func(_ *testing.T, s *suggestRig) {
			s.r.removePlugin(pluginRemoveMsg{ID: "taglist"})
		}, false},
		{"end of the interface", func(_ *testing.T, s *suggestRig) { s.r.closeOverlays() }, false},
		{"ctrl+d then the end", func(t *testing.T, s *suggestRig) {
			s.press("ctrl+d")

			if !s.Quit {
				t.Error("Ctrl+D did not quit")
			}

			s.r.closeOverlays()
		}, false},
		{"ctrl+c while a program of the terminal runs", func(t *testing.T, s *suggestRig) {
			s.r.guard.busy.Store(1)
			s.press("ctrl+c")

			if !s.Quit {
				t.Error("Ctrl+C did not quit")
			}

			s.r.closeOverlays()
		}, false},
		{"signal of interruption", func(t *testing.T, s *suggestRig) {
			s.send(interruptMsg{})

			if !s.Quit || !s.r.interrupted {
				t.Error("the interruption did not quit")
			}

			s.r.closeOverlays()
		}, false},
		{"quit message", func(_ *testing.T, s *suggestRig) {
			s.send(quitMsg{})
			s.r.closeOverlays()
		}, false},
		{"ctrl+z", func(t *testing.T, s *suggestRig) {
			s.press("ctrl+z")

			if !suspended(s.Seen) {
				t.Error("Ctrl+Z did not suspend")
			}
		}, false},
		{"tiny terminal", func(t *testing.T, s *suggestRig) {
			s.resize(minWidth-1, minHeight+1)
			s.press("ctrl+c")

			if !s.Quit {
				t.Error("Ctrl+C did not quit a tiny terminal")
			}

			s.r.closeOverlays()
		}, false},
		{"another key keeps the run", func(_ *testing.T, s *suggestRig) {
			s.press("left", "right", "home", "end")
		}, true},
		{"help over the form keeps the run", func(_ *testing.T, s *suggestRig) {
			s.press("f1")
		}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, pid := hanging(t)
			t.Cleanup(func() { s.r.closeOverlays() })

			tc.do(t, s)

			if tc.alive {
				if s.r.choices == nil {
					t.Fatal("the run was stopped")
				}

				if s.alive(pid) == false {
					t.Fatal("the program was ended")
				}

				return
			}

			if s.r.choices != nil {
				t.Errorf("the root still holds the run: %+v", s.r.choices)
			}

			s.gone(pid)
		})
	}
}

// alive reports whether a process still exists.
//
// Parameters:
//   - pid: process id
func (s *suggestRig) alive(pid int) bool {
	return processAlive(pid)
}

// awaitEnded plays the messages of the slow commands until the answer of the run was handled.
func (s *suggestRig) awaitEnded() {
	s.t.Helper()
	s.await("the end of the run", func() bool { return s.r.choices == nil })
}

// suspended reports whether the messages the model was given hold the one that suspends it.
//
// Parameters:
//   - seen: messages in order
func suspended(seen []tea.Msg) bool {
	for _, m := range seen {
		if _, ok := m.(tea.SuspendMsg); ok {
			return true
		}
	}

	return false
}

// TestSuggestEndWaitsForTheProgram checks that the end of the interface returns only after the
// program is gone, so that a plugin program never outlives tote, and does not wait longer than the
// registry needs.
func TestSuggestEndWaitsForTheProgram(t *testing.T) {
	s, pid := hanging(t)

	began := time.Now()

	s.r.closeOverlays()

	if processAlive(pid) {
		t.Fatal("the program is still alive when closeOverlays returned")
	}

	if d := time.Since(began); d > stopWait {
		t.Errorf("the end waited %v", d)
	}

	s.r.closeOverlays()

	if s.r.choices != nil {
		t.Error("the run is still recorded")
	}
}

// TestSuggestSuspendEndsTheProgramFirst checks that Ctrl+Z ends the program before the
// interface stops: the program has a session of its own and would otherwise go on while tote is
// suspended.
func TestSuggestSuspendEndsTheProgramFirst(t *testing.T) {
	s, pid := hanging(t)
	t.Cleanup(func() { s.r.closeOverlays() })

	_, cmd := s.r.Update(keyMsg("ctrl+z"))

	if processAlive(pid) {
		t.Fatal("the program was alive when the suspension was asked for")
	}

	if cmd == nil {
		t.Fatal("Ctrl+Z gave no command")
	}

	if _, ok := cmd().(tea.SuspendMsg); !ok {
		t.Error("the command is not the suspension")
	}

	if !strings.Contains(s.text(), "ctrl+l") {
		t.Errorf("the form lost its keys:\n%s", s.text())
	}
}

// TestSuggestCtrlLInsideOverlays checks that Ctrl+L does nothing in the picker, which stays, and in
// the help.
func TestSuggestCtrlLInsideOverlays(t *testing.T) {
	s := newSuggestRig(t, "ok")

	s.press("ctrl+l")
	s.listed()

	if _, ok := s.top().(*pickOverlay); !ok {
		t.Fatalf("no picker: %T", s.top())
	}

	s.press("ctrl+l")

	if _, ok := s.top().(*pickOverlay); !ok || s.starts() != 1 {
		t.Errorf("Ctrl+L in the picker did something: %T, %d starts", s.top(), s.starts())
	}

	s.press("esc", "f1", "ctrl+l")

	if _, ok := s.top().(*helpOverlay); !ok {
		t.Errorf("Ctrl+L closed the help: %T", s.top())
	}

	s.press("esc")

	if _, ok := s.top().(*fieldsForm); !ok {
		t.Fatalf("the form is gone: %T", s.top())
	}
}

// TestSuggestAnswerAfterTheFormClosed checks that the answer of a run whose form was closed is
// dropped: no picker, no memo, nothing on the screen.
func TestSuggestAnswerAfterTheFormClosed(t *testing.T) {
	s := newSuggestRig(t, "ok")
	f := s.form()

	s.press("ctrl+l")
	s.press("esc", "esc")
	s.listed()

	s.send(suggestDoneMsg{Form: f, Gen: s.r.choicesSeq, Field: "tag", Key: "k"})

	if s.top() != nil {
		t.Fatalf("a stale answer opened %T", s.top())
	}

	if len(f.sug.memo) > 1 {
		t.Errorf("a stale answer reached the memory: %v", f.sug.memo)
	}
}
