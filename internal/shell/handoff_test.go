package shell

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// newHandoffFile creates the empty file that the wrapper would have made.
//
// Parameters:
//   - t: test to report to
//   - mode: permission bits of the file, set exactly whatever the umask is
func newHandoffFile(t *testing.T, mode os.FileMode) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "tote.handoff")

	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}

	return path
}

// readFile returns the content of a file as a string.
//
// Parameters:
//   - t: test to report to
//   - path: file to read
func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// formatCase is one row of the table of hand-off messages and their file content.
type formatCase struct {
	name string
	h    Handoff
	want string
}

// checkFormat writes a message and checks the file content and the read-back.
//
// Parameters:
//   - t: test to report to
//   - tc: message and the exact content expected in the file
func checkFormat(t *testing.T, tc formatCase) {
	t.Helper()

	path := newHandoffFile(t, 0o600)

	if err := WriteHandoff(path, tc.h); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, path); got != tc.want {
		t.Errorf("file = %q, want %q", got, tc.want)
	}

	back, err := ReadHandoff(path)
	if err != nil || back != tc.h {
		t.Errorf("ReadHandoff = %+v, %v; want %+v", back, err, tc.h)
	}
}

// TestWriteHandoffFormat checks the bytes written for every action and the read-back.
//
// Parameters:
//   - t: test to report to
func TestWriteHandoffFormat(t *testing.T) {
	cases := []formatCase{
		{
			"insert",
			Handoff{Action: Insert, Payload: "ssh -l bob -- web"},
			"v1 insert\nssh -l bob -- web",
		},
		{"insert reload", Handoff{Insert, true, "x"}, "v1 insert,reload\nx"},
		{"cd", Handoff{Action: ChangeDir, Payload: "/home/u/a b"}, "v1 cd\n/home/u/a b"},
		{"cd reload", Handoff{ChangeDir, true, "/tmp"}, "v1 cd,reload\n/tmp"},
		{"exit", Handoff{Action: Exit}, "v1 exit\n"},
		{"none", Handoff{Action: None}, "v1 none\n"},
		{"none reload", Handoff{Action: None, Reload: true}, "v1 none,reload\n"},
		{"multiline", Handoff{Action: Insert, Payload: "a\nb\n"}, "v1 insert\na\nb\n"},
		{
			"unicode",
			Handoff{Action: Insert, Payload: "caf\u00e9 \U0001f600"},
			"v1 insert\ncaf\u00e9 \U0001f600",
		},
		{
			"quotes",
			Handoff{Action: Insert, Payload: "echo 'a' \"b\" `c` $d"},
			"v1 insert\necho 'a' \"b\" `c` $d",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkFormat(t, tc) })
	}
}

// TestWriteHandoffRefusesNonEmpty checks that a file that already has content, which the wrapper
// never makes, is left untouched even when it is private and owned by the user.
//
// Parameters:
//   - t: test to report to
func TestWriteHandoffRefusesNonEmpty(t *testing.T) {
	path := newHandoffFile(t, 0o600)
	old := strings.Repeat("old content ", 50)

	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteHandoff(path, Handoff{Action: Exit}); !errors.Is(err, ErrHandoff) {
		t.Errorf("err = %v, want ErrHandoff", err)
	}

	if got := readFile(t, path); got != old {
		t.Errorf("file = %q", got)
	}
}

// TestWriteHandoffRefusesPath checks the paths and files that must never be written.
//
// Parameters:
//   - t: test to report to
func TestWriteHandoffRefusesPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	secret := "do not touch"

	if err := os.WriteFile(target, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, path string }{
		{"empty", ""},
		{"missing", filepath.Join(dir, "missing")},
		{"symlink", link},
		{"directory", dir},
		{"fifo", fifo},
		{"device", "/dev/null"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := WriteHandoff(tc.path, Handoff{Action: Exit}); err == nil {
				t.Error("write accepted")
			}
		})
	}

	if got := readFile(t, target); got != secret {
		t.Errorf("symlink target was changed: %q", got)
	}

	if _, err := os.Lstat(filepath.Join(dir, "missing")); err == nil {
		t.Error("the file was created")
	}
}

// modeCase is one row of the table of file modes and their expected verdict.
type modeCase struct {
	mode os.FileMode
	ok   bool
}

// checkMode writes to a file of a given mode and checks the verdict and the file content.
//
// Parameters:
//   - t: test to report to
//   - tc: mode of the file and whether the write must be accepted
func checkMode(t *testing.T, tc modeCase) {
	t.Helper()

	path := newHandoffFile(t, tc.mode)
	if !tc.ok {
		if err := os.WriteFile(path, []byte("before"), tc.mode); err != nil {
			t.Fatal(err)
		}
	}

	err := WriteHandoff(path, Handoff{Action: None})
	if tc.ok != (err == nil) {
		t.Fatalf("err = %v, want ok=%v", err, tc.ok)
	}

	if tc.ok {
		return
	}

	if !errors.Is(err, ErrHandoff) {
		t.Errorf("error does not wrap ErrHandoff: %v", err)
	}

	if got := readFile(t, path); got != "before" {
		t.Errorf("refused file was changed: %q", got)
	}
}

// TestWriteHandoffRefusesMode checks that only a file of mode 0600 is written and that a file of
// any other mode is left untouched.
//
// Parameters:
//   - t: test to report to
func TestWriteHandoffRefusesMode(t *testing.T) {
	cases := []modeCase{
		{0o600, true},
		{0o640, false},
		{0o644, false},
		{0o700, false},
		{0o620, false},
		{0o602, false},
		{0o660, false},
		{0o666, false},
		{0o622, false},
		{0o700 | 0o020, false},
	}

	for _, tc := range cases {
		t.Run(tc.mode.String(), func(t *testing.T) { checkMode(t, tc) })
	}
}

// checkRefusedContent writes a message that must be refused and checks the file is untouched.
//
// Parameters:
//   - t: test to report to
//   - h: message with an unknown action or a NUL in the payload
func checkRefusedContent(t *testing.T, h Handoff) {
	t.Helper()

	path := newHandoffFile(t, 0o600)

	err := WriteHandoff(path, h)
	if !errors.Is(err, ErrHandoff) {
		t.Errorf("err = %v, want ErrHandoff", err)
	}

	if got := readFile(t, path); got != "" {
		t.Errorf("file was changed: %q", got)
	}
}

// TestWriteHandoffRefusesContent checks the messages that are rejected before any file access.
//
// Parameters:
//   - t: test to report to
func TestWriteHandoffRefusesContent(t *testing.T) {
	cases := []struct {
		name string
		h    Handoff
	}{
		{"NUL in payload", Handoff{Action: Insert, Payload: "a\x00b"}},
		{"NUL alone", Handoff{Action: Insert, Payload: "\x00"}},
		{"zero action", Handoff{}},
		{"unknown action", Handoff{Action: "run"}},
		{"action with comma", Handoff{Action: "cd,reload"}},
		{"action with newline", Handoff{Action: "cd\nexit"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkRefusedContent(t, tc.h) })
	}
}

// TestReadHandoffRefuses checks the files and contents that ReadHandoff rejects.
//
// Parameters:
//   - t: test to report to
func TestReadHandoffRefuses(t *testing.T) {
	cases := []struct{ name, content string }{
		{"empty", ""},
		{"no version", "insert\nx"},
		{"other version", "v2 insert\nx"},
		{"unknown action", "v1 run\nx"},
		{"unknown flag", "v1 cd,later\nx"},
		{"two flags", "v1 cd,reload,reload\nx"},
		{"NUL", "v1 insert\na\x00b"},
		{"space before action", "v1  cd\nx"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := newHandoffFile(t, 0o600)
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := ReadHandoff(path); !errors.Is(err, ErrHandoff) {
				t.Errorf("err = %v, want ErrHandoff", err)
			}
		})
	}

	if _, err := ReadHandoff(""); !errors.Is(err, ErrHandoff) {
		t.Errorf("empty path: %v", err)
	}

	dir := t.TempDir()
	link := filepath.Join(dir, "link")

	if err := os.Symlink(newHandoffFile(t, 0o600), link); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadHandoff(link); err == nil {
		t.Error("symlink accepted")
	}

	if _, err := ReadHandoff(dir); err == nil {
		t.Error("directory accepted")
	}

	if _, err := ReadHandoff(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing file accepted")
	}
}

// TestReadHandoffLenient checks the forms that the wrapper also understands.
//
// Parameters:
//   - t: test to report to
func TestReadHandoffLenient(t *testing.T) {
	cases := []struct {
		content string
		want    Handoff
	}{
		{"v1 none", Handoff{Action: None}},
		{"v1 exit,reload", Handoff{Action: Exit, Reload: true}},
		{"v1 cd\n", Handoff{Action: ChangeDir}},
		{"v1 insert\n\n", Handoff{Action: Insert, Payload: "\n"}},
	}

	for _, tc := range cases {
		path := newHandoffFile(t, 0o600)
		if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
			t.Fatal(err)
		}

		got, err := ReadHandoff(path)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %+v, %v; want %+v", tc.content, got, err, tc.want)
		}
	}
}

// TestHandoffRefusesOtherOwner plays another user to check that a file owned by someone else is
// refused for writing and for reading, and left untouched.
//
// Parameters:
//   - t: test to report to
func TestHandoffRefusesOtherOwner(t *testing.T) {
	path := newHandoffFile(t, 0o600)

	if err := os.WriteFile(path, []byte("v1 none\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	old := getuid
	getuid = func() int { return os.Getuid() + 1 }

	defer func() { getuid = old }()

	if err := WriteHandoff(path, Handoff{Action: Exit}); !errors.Is(err, ErrHandoff) {
		t.Errorf("write: err = %v, want ErrHandoff", err)
	}

	if _, err := ReadHandoff(path); !errors.Is(err, ErrHandoff) {
		t.Errorf("read: err = %v, want ErrHandoff", err)
	}

	if got := readFile(t, path); got != "v1 none\n" {
		t.Errorf("file was changed: %q", got)
	}
}

// TestRemoveHandoff checks that the empty private file of the wrapper is deleted and that
// anything else is left alone.
//
// Parameters:
//   - t: test to report to
func TestRemoveHandoff(t *testing.T) {
	path := newHandoffFile(t, 0o600)

	if err := RemoveHandoff(path); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(path); err == nil {
		t.Error("the file is still there")
	}

	if err := RemoveHandoff(""); err != nil {
		t.Errorf("empty path: %v", err)
	}

	full := newHandoffFile(t, 0o600)
	if err := os.WriteFile(full, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	loose := newHandoffFile(t, 0o644)
	link := filepath.Join(t.TempDir(), "link")

	if err := os.Symlink(loose, link); err != nil {
		t.Fatal(err)
	}

	for name, p := range map[string]string{"content": full, "mode": loose, "symlink": link} {
		if err := RemoveHandoff(p); !errors.Is(err, ErrHandoff) && err == nil {
			t.Errorf("%s: removal accepted", name)
		}
	}

	for _, p := range []string{full, loose} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}
}
