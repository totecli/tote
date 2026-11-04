package ui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// checkWith returns an Options setup that gives the interface a fixed update check.
//
// Parameters:
//   - res: answer of the check
//   - err: failure of the check
func checkWith(res UpdateCheck, err error) func(*Options) {
	return func(o *Options) {
		o.CheckUpdate = func(context.Context) (UpdateCheck, error) { return res, err }
	}
}

// TestUpdateNotice checks the key u on the welcome page and on the settings: it shows the notice
// that is known, or says that none is and how to ask.
//
// Parameters:
//   - t: test to report to
func TestUpdateNotice(t *testing.T) {
	for _, start := range []Start{StartHome, StartConfig} {
		g := newRig(t, newSandbox(t).with(func(o *Options) { o.Start = start }))
		g.press("u")

		if got := g.toastText(); !strings.Contains(got, "no update known") ||
			!strings.Contains(got, "U") {
			t.Errorf("start %d: toast = %q", start, got)
		}

		g.send(updateMsg{Version: "1.3.0"})
		g.press("u")

		if got := g.toastText(); !strings.Contains(got, "1.3.0") ||
			!strings.Contains(got, "tote update") {
			t.Errorf("start %d: toast with a notice = %q", start, got)
		}

		if g.r.toast.err {
			t.Errorf("start %d: a notice is not an error", start)
		}
	}
}

// TestUpdateNoticeNeverAsks checks that u does not call the check: only U does.
//
// Parameters:
//   - t: test to report to
func TestUpdateNoticeNeverAsks(t *testing.T) {
	asked := 0
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.CheckUpdate = func(context.Context) (UpdateCheck, error) {
			asked++

			return UpdateCheck{}, nil
		}
	}))

	g.press("u", "u")

	if asked != 0 {
		t.Errorf("u asked the server %d times", asked)
	}
}

// TestUpdateCheck checks the key U for every outcome of the check: the toast, its severity and
// the notice of the status line.
//
// Parameters:
//   - t: test to report to
func TestUpdateCheck(t *testing.T) {
	tests := []struct {
		name   string
		res    UpdateCheck
		err    error
		want   string
		isErr  bool
		notice string
	}{
		{
			"available",
			UpdateCheck{Status: "available", Current: "1.2.3", Latest: "1.3.0"},
			nil,
			"update 1.3.0 available", false, "1.3.0",
		},
		{
			"up to date",
			UpdateCheck{Status: "up-to-date", Current: "1.2.3", Latest: "1.2.3"},
			nil,
			"tote 1.2.3 is up to date", false, "",
		},
		{
			"newer",
			UpdateCheck{Status: "newer", Current: "1.4.0", Latest: "1.3.0"},
			nil,
			"1.4.0) is newer than the latest release (1.3.0)", false, "",
		},
		{
			"dev",
			UpdateCheck{Status: "dev", Current: "dev", Latest: "1.3.0"},
			nil,
			"development build, latest release: 1.3.0", false, "",
		},
		{
			"managed",
			UpdateCheck{Status: "managed", Current: "1.2.3", Command: "brew upgrade tote"},
			nil, "a package manager owns tote, update it with: brew upgrade tote", false, "",
		},
		{
			"offline",
			UpdateCheck{},
			errors.New("cannot reach github.com"),
			"update check failed: cannot reach github.com", true, "",
		},
		{
			"hostile error",
			UpdateCheck{},
			errors.New("bad \x1b]52;c;aGk=\x07 reply"),
			"update check failed: bad ?]52;c;aGk=? reply", true, "",
		},
		{
			"hostile version",
			UpdateCheck{Status: "available", Latest: "9\x1b[31m"},
			nil,
			"update 9?[31m available", false, "9?[31m",
		},
		{
			"no update in the build",
			UpdateCheck{},
			errNoUpdate,
			"this build cannot check for updates", true, "",
		},
	}

	for _, tt := range tests {
		for _, start := range []Start{StartHome, StartConfig} {
			g := newRig(t, newSandbox(t).with(func(o *Options) {
				o.Start = start
				checkWith(tt.res, tt.err)(o)
			}))

			g.press("U")
			g.await("the check", func() bool {
				return !g.r.checking && !strings.Contains(g.toastText(), "checking")
			})

			if !strings.Contains(g.toastText(), tt.want) || g.r.toast.err != tt.isErr {
				t.Errorf("%s start %d: toast %q (error %v), want %q", tt.name, start,
					g.toastText(), g.r.toast.err, tt.want)
			}

			if g.r.status.update != tt.notice {
				t.Errorf("%s start %d: status notice %q, want %q", tt.name, start,
					g.r.status.update, tt.notice)
			}

			if strings.ContainsAny(g.toastText(), "\x1b\x07") {
				t.Errorf("%s: the toast holds a control character: %q", tt.name, g.toastText())
			}
		}
	}
}

// TestUpdateCheckSecondComesAfter checks that a second U during a check does not start another
// one, says so, and that the first answer is still shown.
//
// Parameters:
//   - t: test to report to
func TestUpdateCheckSecondComesAfter(t *testing.T) {
	release := make(chan struct{})

	var calls atomic.Int32

	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.CheckUpdate = func(ctx context.Context) (UpdateCheck, error) {
			calls.Add(1)
			<-release

			return UpdateCheck{Status: "available", Latest: "2.0.0"}, nil
		}
	}))

	g.press("U")

	if !g.r.checking || !strings.Contains(g.toastText(), "checking") {
		t.Fatalf("checking = %v, toast %q", g.r.checking, g.toastText())
	}

	g.press("U")

	if !strings.Contains(g.toastText(), "already running") {
		t.Errorf("toast = %q", g.toastText())
	}

	close(release)
	g.await("the answer", func() bool { return !g.r.checking })

	if calls.Load() != 1 || !strings.Contains(g.toastText(), "2.0.0") {
		t.Errorf("%d calls, toast %q", calls.Load(), g.toastText())
	}

	// Another check can start after the first one ended.
	g.press("U")
	g.await("the second answer", func() bool { return !g.r.checking && calls.Load() == 2 })
}

// TestUpdateCheckWithoutCallback checks that U says so when the build has no check.
//
// Parameters:
//   - t: test to report to
func TestUpdateCheckWithoutCallback(t *testing.T) {
	g := newRig(t, newSandbox(t))
	g.press("U")

	if !g.r.toast.err || !strings.Contains(g.toastText(), "cannot check for updates") ||
		g.r.checking {
		t.Errorf("toast %q error %v checking %v", g.toastText(), g.r.toast.err, g.r.checking)
	}
}

// TestUpdateCheckFromSettingsRow checks the row of the settings screen: Enter on it checks for
// an update, and the footer says so.
//
// Parameters:
//   - t: test to report to
func TestUpdateCheckFromSettingsRow(t *testing.T) {
	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.Start = StartConfig
		checkWith(UpdateCheck{Status: "up-to-date", Current: "1.2.3"}, nil)(o)
	}))

	selectRow(g, "update")

	if !strings.Contains(g.text(), "check for update") {
		t.Errorf("the footer does not offer the check:\n%s", g.text())
	}

	g.press("enter")
	g.await("the check", func() bool {
		return !g.r.checking && strings.Contains(g.toastText(), "up to date")
	})
}

// TestUpdateCheckContext checks that the context given to the check ends when the
// interface closes, so that a hung request cannot keep the program alive.
//
// Parameters:
//   - t: test to report to
func TestUpdateCheckContext(t *testing.T) {
	var seen atomic.Pointer[context.Context]

	g := newRig(t, newSandbox(t).with(func(o *Options) {
		o.CheckUpdate = func(ctx context.Context) (UpdateCheck, error) {
			seen.Store(&ctx)

			return UpdateCheck{Status: "up-to-date"}, nil
		}
	}))

	ctx, stop := context.WithCancel(context.Background())
	g.r.ctx = ctx

	g.press("U")
	g.await("the check", func() bool { return seen.Load() != nil })

	stop()

	got := *seen.Load()

	if got.Err() == nil {
		t.Error("the context of the check did not follow the interface")
	}

	if dl, ok := got.Deadline(); !ok || dl.IsZero() {
		t.Error("the check has no deadline")
	}
}
