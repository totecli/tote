package shell

import (
	"os"
	"path/filepath"
	"testing"
)

// checkCdIgnored makes the fake tote ask for a change of directory to a target that is not a
// directory and checks that nothing happens.
//
// Parameters:
//   - t: test to report to
//   - pre: option line such as "set -u", possibly empty
func checkCdIgnored(t *testing.T, pre string) {
	t.Helper()

	w := newWrapperEnv(t)

	file := filepath.Join(w.Dir, "plainfile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{file, filepath.Join(w.Dir, "missing"), "-x", "--"} {
		w.setenv("FAKE_ACTION", "cd")
		w.setenv("FAKE_PAYLOAD", target)

		got, stderr := w.call(t, pre, "tote go")

		if stderr != "" || got["rc"] != "0" || got["pwd"] != w.Dir {
			t.Errorf("%q: stderr %q, rc %q, pwd %q", target, stderr, got["rc"], got["pwd"])
		}
	}
}

// TestReviewWrapperCdNotADirectory checks that a hand-off asking to change to something that is
// not a directory is ignored silently: no error message, the same directory and the exit code of
// the program.
//
// Parameters:
//   - t: test to report to
func TestReviewWrapperCdNotADirectory(t *testing.T) {
	for _, pre := range preambles {
		t.Run(pre.name, func(t *testing.T) { checkCdIgnored(t, pre.text) })
	}
}

// TestReviewWrapperCdDashDirectory checks that a directory whose name starts with a dash is
// entered as a directory and not parsed as an option of cd.
//
// Parameters:
//   - t: test to report to
func TestReviewWrapperCdDashDirectory(t *testing.T) {
	w := newWrapperEnv(t)

	dir := filepath.Join(w.Dir, "-P")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	w.setenv("FAKE_ACTION", "cd")
	w.setenv("FAKE_PAYLOAD", "-P")

	got, stderr := w.call(t, "cd "+Quote(w.Dir)+"\n", "tote go")

	if stderr != "" || got["rc"] != "0" {
		t.Fatalf("stderr %q, rc %q", stderr, got["rc"])
	}

	if got["pwd"] != dir {
		t.Errorf("pwd = %q, want %q", got["pwd"], dir)
	}
}
