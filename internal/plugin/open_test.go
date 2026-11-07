package plugin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// openLab installs gft with an opener that records how it was started: its argument count, its
// arguments and its environment.
//
// Parameters:
//   - t: test to report to
func openLab(t *testing.T) (*lab, *Registry, string) {
	t.Helper()

	l, repo := gftLab(t)
	rec := filepath.Join(l.root, "opener")

	l.script(OpenerName(runtime.GOOS), "printf '%s\\n' \"$#\" \"$1\" > "+rec+".args\n"+
		"env > "+rec+".env\n")

	// Variables that must never reach the opener.
	l.env["GITHUB_TOKEN"] = "ghp_review_secret_value_0123456789"

	return l, l.reg(func(o *Options) { o.Cwd = repo }), rec
}

// openCall is the "open" command of gft: it opens the releases page of the repository.
func openCall() Call {
	return Call{Plugin: "gft", Table: TableCommand, Entry: "open"}
}

// TestReviewRunOpenStartsTheOpener pins the one path where tote itself executes a program for an
// open contribution, which had no test: the opener gets the URL as its only argument and an
// environment made of the desktop variables only.
//
// Parameters:
//   - t: test to report to
func TestReviewRunOpenStartsTheOpener(t *testing.T) {
	_, r, rec := openLab(t)
	p := prepare(t, r, openCall())

	out := r.Run(context.Background(), p, nil)
	if out.Status != OutcomeOK {
		t.Fatalf("outcome %+v err %v", out, out.Err)
	}

	args, err := os.ReadFile(rec + ".args")
	if err != nil {
		t.Fatalf("the opener did not run: %v", err)
	}

	if want := "1\n" + p.OpenURL + "\n"; string(args) != want {
		t.Errorf("opener arguments %q want %q", args, want)
	}

	env, err := os.ReadFile(rec + ".env")
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(env)), "\n") {
		name, _, _ := strings.Cut(line, "=")

		// The shell that runs the fixture adds these itself.
		if slices.Contains([]string{"PWD", "SHLVL", "_", "OLDPWD"}, name) {
			continue
		}

		if !slices.Contains(desktopEnv, name) {
			t.Errorf("the opener received %q, which is not a desktop variable", name)
		}
	}
}

// TestReviewRunOpenGates checks that the open path honours the same gates as a program: the
// confirmation, the kill switch, and the text that Prepare produced. In each case the opener must
// not start.
//
// Parameters:
//   - t: test to report to
func TestReviewRunOpenGates(t *testing.T) {
	tests := []struct {
		name   string
		change func(l *lab, p *Prepared)
		code   string
	}{
		{
			"confirmation not given", func(_ *lab, p *Prepared) { p.Confirm.Required = true },
			CodeConfirmNeeded,
		},
		{"plugin disabled after Prepare", func(l *lab, _ *Prepared) {
			must(t, l.lock().SetDisabled("gft", true))
		}, CodeNotRunnable},
		{
			"url is not https", func(_ *lab, p *Prepared) { p.OpenURL = "http://github.com/x/y" },
			CodeOpenInvalid,
		},
		{
			"url has userinfo", func(_ *lab,
				p *Prepared,
			) {
				p.OpenURL = "https://a@evil.example/x"
			},
			CodeOpenInvalid,
		},
		{
			"url with a newline", func(_ *lab,
				p *Prepared,
			) {
				p.OpenURL = "https://github.com/x\n-y"
			},
			CodeOpenInvalid,
		},
		{"opener path emptied", func(_ *lab, p *Prepared) { p.opener = "" }, CodeOpenInvalid},
		{
			"tree changed since Prepare", func(_ *lab, p *Prepared) {
				p.tree = strings.Repeat("0",
					64)
			},
			CodeStale,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, r, rec := openLab(t)
			p := prepare(t, r, openCall())

			tt.change(l, &p)

			out := r.Run(context.Background(), p, nil)
			if out.Status != OutcomeRefused || out.Err == nil || out.Err.Code != tt.code {
				t.Fatalf("outcome %+v err %v want %s", out, out.Err, tt.code)
			}

			if exists(rec + ".args") {
				t.Error("the opener started although the run was refused")
			}
		})
	}
}

// TestReviewRunOpenCancelled checks that a context that is already cancelled starts nothing.
//
// Parameters:
//   - t: test to report to
func TestReviewRunOpenCancelled(t *testing.T) {
	_, r, rec := openLab(t)
	p := prepare(t, r, openCall())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if out := r.Run(ctx, p, nil); out.Status != OutcomeCancelled {
		t.Errorf("outcome %+v", out)
	}

	if exists(rec + ".args") {
		t.Error("the opener started on a cancelled context")
	}
}
