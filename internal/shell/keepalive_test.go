package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// recorder is a lookup that plays "ssh -G": it answers with a fixed text or error and keeps the
// commands it was asked about.
type recorder struct {
	out  string
	err  error
	seen [][]string
}

// probe implements Prober.
//
// Parameters:
//   - ctx: unused
//   - argv: command that was asked about
//   - environ: unused
func (r *recorder) probe(_ context.Context, argv, _ []string) (string, error) {
	r.seen = append(r.seen, append([]string(nil), argv...))

	return r.out, r.err
}

// TestKeepaliveFrom checks which options follow from the output of "ssh -G".
//
// Parameters:
//   - t: test to report to
func TestKeepaliveFrom(t *testing.T) {
	both := []string{"-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3"}

	tests := []struct {
		name string
		out  string
		want []string
	}{
		{"nothing configured", "serveraliveinterval 0\nserveralivecountmax 3\n", both},
		{"interval only", "user x\nserveraliveinterval 0\n", both},
		{"interval set", "serveraliveinterval 15\nserveralivecountmax 3\n", nil},
		{"interval of one", "serveraliveinterval 1\n", nil},
		{
			"count chosen", "serveraliveinterval 0\nserveralivecountmax 6\n",
			[]string{"-o", "ServerAliveInterval=30"},
		},
		{
			"count first", "serveralivecountmax 6\nserveraliveinterval 0\n",
			[]string{"-o", "ServerAliveInterval=30"},
		},
		{"upper case", "ServerAliveInterval 0\n", both},
		{"windows line ends", "serveraliveinterval 0\r\nserveralivecountmax 3\r\n", both},
		{"missing", "hostname x\nport 22\n", nil},
		{"empty", "", nil},
		{"not a number", "serveraliveinterval soon\n", nil},
		{"negative", "serveraliveinterval -1\n", nil},
	}

	for _, tt := range tests {
		if got := keepaliveFrom(tt.out); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: keepaliveFrom = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestKeepEligible checks which commands may receive the options.
//
// Parameters:
//   - t: test to report to
func TestKeepEligible(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want bool
	}{
		{"plain ssh", []string{"ssh", "-l", "deploy", "--", "h"}, true},
		{"path of ssh", []string{"/usr/bin/ssh", "--", "h"}, true},
		{"mosh", []string{"mosh", "--", "h"}, false},
		{"custom via", []string{"my-ssh", "--", "h"}, false},
		{"already set", []string{"ssh", "-o", "ServerAliveInterval=5", "--", "h"}, false},
		{"already set, other case", []string{"ssh", "-oServerAliveCountMax=1", "--", "h"}, false},
		{"program alone", []string{"ssh"}, false},
		{"empty", nil, false},
	}

	for _, tt := range tests {
		if got := KeepEligible(tt.argv); got != tt.want {
			t.Errorf("%s: KeepEligible = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestKeepalive checks the command that comes out of the lookup for several answers, that the
// lookup is "ssh -G" with the arguments of the connection, and that nothing is asked when the
// setting is off or the command is not plain ssh.
//
// Parameters:
//   - t: test to report to
func TestKeepalive(t *testing.T) {
	plain := "ssh -l deploy -- prod.example.com"
	both := "ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -l deploy -- prod.example.com"

	tests := []struct {
		name string
		on   bool
		argv string
		out  string
		err  error
		want string
		asks bool
	}{
		{
			"none configured", true, plain, "serveraliveinterval 0\nserveralivecountmax 3\n", nil,
			both, true,
		},
		{
			"configured", true, plain, "serveraliveinterval 60\nserveralivecountmax 3\n", nil,
			plain, true,
		},
		{
			"count chosen", true, plain, "serveraliveinterval 0\nserveralivecountmax 9\n", nil,
			"ssh -o ServerAliveInterval=30 -l deploy -- prod.example.com", true,
		},
		{
			"lookup failed", true, plain, "serveraliveinterval 0\n", errors.New("exit status 255"),
			plain, true,
		},
		{"no answer", true, plain, "", nil, plain, true},
		{"setting off", false, plain, "serveraliveinterval 0\n", nil, plain, false},
		{"not ssh", true, "mosh -- prod", "serveraliveinterval 0\n", nil, "mosh -- prod", false},
		{
			"own option", true, "ssh -o ServerAliveInterval=5 -- h", "serveraliveinterval 0\n", nil,
			"ssh -o ServerAliveInterval=5 -- h", false,
		},
	}

	for _, tt := range tests {
		rec := &recorder{out: tt.out, err: tt.err}
		argv := strings.Fields(tt.argv)

		got := Keepalive(tt.on, rec.probe, []string{"A=b"}, argv)

		if strings.Join(got, " ") != tt.want {
			t.Errorf("%s: Keepalive = %q, want %q", tt.name, strings.Join(got, " "), tt.want)
		}

		if strings.Join(argv, " ") != tt.argv {
			t.Errorf("%s: the argument slice was changed: %q", tt.name, argv)
		}

		if (len(rec.seen) == 1) != tt.asks || len(rec.seen) > 1 {
			t.Errorf("%s: asked %v, want a lookup: %v", tt.name, rec.seen, tt.asks)
		}

		if tt.asks && strings.Join(rec.seen[0], " ") != "ssh -G -l deploy -- prod.example.com" {
			t.Errorf("%s: asked %v", tt.name, rec.seen)
		}
	}
}

// TestSSHProbe runs the real lookup against a script: it gets the environment it is given, no
// input, a silent error output, and a limit of time.
//
// Parameters:
//   - t: test to report to
func TestSSHProbe(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake")

	body := "#!/bin/sh\necho \"seen $MARK $*\"\necho noise >&2\n" +
		"[ \"$1\" = fail ] && exit 3\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}

	out, err := SSHProbe(context.Background(), []string{script, "-G", "h"}, []string{"MARK=m"})
	if err != nil || out != "seen m -G h\n" {
		t.Errorf("SSHProbe = %q, %v", out, err)
	}

	out, err = SSHProbe(context.Background(), []string{script, "fail"}, nil)
	if err == nil || out != "seen  fail\n" {
		t.Errorf("a failing command gave %q, %v", out, err)
	}

	missing := []string{filepath.Join(dir, "missing")}
	if _, err = SSHProbe(context.Background(), missing, nil); err == nil {
		t.Error("a missing program gave no error")
	}
}

// TestSSHProbeLimits runs the real lookup against scripts that misbehave: one that never ends
// must be given up after probeTimeout, and one that ends at once but leaves a child holding its
// output must not hold the caller either (probeGrace). The slow script lasts five seconds at
// most, so a mutant fails the test instead of hanging it.
//
// Parameters:
//   - t: test to report to
func TestSSHProbeLimits(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name  string
		body  string
		limit time.Duration
	}{
		{"never ends", "#!/bin/sh\nexec sleep 5\n", probeTimeout + time.Second},
		{"child keeps the pipe", "#!/bin/sh\necho ok\nsleep 5 &\nexit 0\n", time.Second},
	}

	for _, tt := range tests {
		script := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "-"))
		if err := os.WriteFile(script, []byte(tt.body), 0o700); err != nil {
			t.Fatal(err)
		}

		start := time.Now()
		_, _ = SSHProbe(context.Background(), []string{script, "-G", "h"}, nil)

		if took := time.Since(start); took > tt.limit {
			t.Errorf("%s: the lookup took %v, want at most %v", tt.name, took, tt.limit)
		}
	}
}
