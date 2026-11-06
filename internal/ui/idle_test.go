package ui

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// counted is a model that counts the messages it handles and the frames it draws.
type counted struct {
	tea.Model
	updates *atomic.Int64
	views   *atomic.Int64
}

// Update implements tea.Model.
//
// Parameters:
//   - msg: message to handle
func (c *counted) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	c.updates.Add(1)

	m, cmd := c.Model.Update(msg)
	c.Model = m

	return c, cmd
}

// View implements tea.Model.
func (c *counted) View() tea.View {
	c.views.Add(1)

	return c.Model.View()
}

// TestIdleDoesNothing runs the real program loop on a pipe with the real timers and checks that an
// interface nobody touches does no work once it has settled: not one message handled, not one
// frame built and not one byte written during two seconds, on the welcome page, a list, the
// palette and the settings screen. A periodic timer, a polling command or a spinner that keeps
// running would show here. What remains while idle is the wake-up of the Bubble Tea renderer, at
// renderFPS per second, which draws nothing when the frame is the same.
//
// Parameters:
//   - t: test to report to
func TestIdleDoesNothing(t *testing.T) {
	saved := tick
	tick = tea.Tick

	t.Cleanup(func() { tick, wrapModel = saved, nil })

	tests := []struct {
		name  string
		start Start
		keys  []string
	}{
		{"home", StartHome, nil},
		{"list", StartConnections, nil},
		{"palette", StartConnections, []string{"\x0b"}},
		{"settings", StartConfig, nil},
		{"repositories", StartRepos, nil},
	}

	for _, tt := range tests {
		updates, views := &atomic.Int64{}, &atomic.Int64{}
		wrapModel = func(m tea.Model) tea.Model {
			return &counted{Model: m, updates: updates, views: views}
		}

		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}

		out := newLockedBuffer()
		s := newSandbox(t).with(func(o *Options) {
			o.Start = tt.start
			o.Input, o.Output = pr, out
		})

		done := make(chan struct{})

		go func() {
			_, _ = Run(s.Opts)

			close(done)
		}()

		for _, k := range tt.keys {
			time.Sleep(200 * time.Millisecond)
			_, _ = pw.Write([]byte(k))
		}

		// The first frames, the wait for the background color (300 ms) and the first branch
		// lookup are startup, not idleness. On a busy machine they take longer than the pause
		// that is enough on an idle one, so the count starts when a window of 700 ms passes with
		// nothing done.
		time.Sleep(1500 * time.Millisecond)

		for end := time.Now().Add(30 * time.Second); time.Now().Before(end); {
			u, v, b := updates.Load(), views.Load(), len(out.String())

			time.Sleep(700 * time.Millisecond)

			if u == updates.Load() && v == views.Load() && b == len(out.String()) {
				break
			}
		}

		u0, v0, b0 := updates.Load(), views.Load(), len(out.String())

		time.Sleep(2 * time.Second)

		u1, v1, b1 := updates.Load(), views.Load(), len(out.String())

		if u1 != u0 || v1 != v0 || b1 != b0 {
			t.Errorf("%s: idle for 2 s handled %d messages, built %d frames, wrote %d bytes",
				tt.name, u1-u0, v1-v0, b1-b0)
		}

		if u0 == 0 || b0 == 0 {
			t.Errorf("%s: the interface did not start: %d messages, %d bytes", tt.name, u0, b0)
		}

		_, _ = pw.Write([]byte("\x04"))

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: Run did not return", tt.name)
		}

		_ = pw.Close()
		_ = pr.Close()
	}
}
