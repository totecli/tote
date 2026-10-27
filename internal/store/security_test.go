package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// reviewDoc is the decoded shape of a whole test file.
//
// Schema is the schema number and Rec the records of the rec table.
type reviewDoc struct {
	Schema int      `toml:"schema"`
	Rec    []rawRec `toml:"rec"`
}

// rawRec is a record without any validation, to attack the encoder and the verify step.
//
// Name is the key and Val an arbitrary string.
type rawRec struct {
	Name string   `toml:"name"`
	Val  string   `toml:"val,omitempty"`
	Tags []string `toml:"tags,omitempty"`
}

// Key implements Record.
func (r rawRec) Key() string { return r.Name }

// Validate implements Record and accepts everything.
func (r rawRec) Validate() error { return nil }

// reviewMode returns the permission bits of a path.
//
// Parameters:
//   - t: test to report to
//   - path: file or directory to stat
func reviewMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	return info.Mode().Perm()
}

// TestReviewSecLockSymlinkNotTouched checks that a lock file that is a symlink is not truncated.
//
// Parameters:
//   - t: test to report to
func TestReviewSecLockSymlinkNotTouched(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(victim, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "c.toml")
	if err := os.Symlink(victim, path+".lock"); err != nil {
		t.Fatal(err)
	}
	c, err := OpenConnections(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Add(Connection{Name: "a", Target: "h"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, _ := os.ReadFile(victim)
	if string(got) != "precious" {
		t.Fatalf("victim modified: %q", got)
	}
	if m := reviewMode(t, victim); m != 0o644 {
		t.Fatalf("victim mode changed to %v", m)
	}
}

// TestReviewSecLockSymlinkDangling documents that a dangling lock symlink creates its target.
//
// Parameters:
//   - t: test to report to
func TestReviewSecLockSymlinkDangling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	created := filepath.Join(dir, "created-by-lock")
	if err := os.Symlink(created, path+".lock"); err != nil {
		t.Fatal(err)
	}
	c, _ := OpenConnections(path)
	if err := c.Add(Connection{Name: "a", Target: "h"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := os.Lstat(created); err == nil {
		t.Logf("dangling lock symlink was followed: %s created, mode %v", created,
			reviewMode(t, created))
	}
}

// TestReviewSecDataSymlink checks that a symlinked data file stays a symlink and no temp is left.
//
// Parameters:
//   - t: test to report to
func TestReviewSecDataSymlink(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(realDir, "c.toml")
	link := filepath.Join(dir, "c.toml")
	if err := os.WriteFile(real, []byte("schema = 1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(real, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	c, _ := OpenConnections(link)
	if err := c.Add(Connection{Name: "a", Target: "h"}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v %v", info, err)
	}
	if m := reviewMode(t, real); m != 0o640 {
		t.Fatalf("mode not preserved: %v", m)
	}
	if _, err := os.Stat(real + ".lock"); err != nil {
		t.Fatalf("lock not next to the real file: %v", err)
	}
	if _, err := os.Lstat(link + ".lock"); err == nil {
		t.Fatalf("lock created next to the link")
	}
	entries, _ := os.ReadDir(realDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left: %s", e.Name())
		}
	}
}

// TestReviewSecUmask checks that modes do not depend on a permissive umask.
//
// Parameters:
//   - t: test to report to
func TestReviewSecUmask(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c.toml")
	c, _ := OpenConnections(path)
	if err := c.Add(Connection{Name: "a", Target: "h"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(dir, "a"), filepath.Join(dir, "a", "b")} {
		if m := reviewMode(t, p); m != 0o700 {
			t.Errorf("dir %s mode %v", p, m)
		}
	}
	for _, p := range []string{path, path + ".lock"} {
		if m := reviewMode(t, p); m != 0o600 {
			t.Errorf("file %s mode %v", p, m)
		}
	}
	// A second write goes through the temp file; the mode must stay 0600.
	if err := c.Add(Connection{Name: "b", Target: "h"}); err != nil {
		t.Fatal(err)
	}
	if m := reviewMode(t, path); m != 0o600 {
		t.Errorf("after second write mode %v", m)
	}
}

// TestReviewSecExistingDirUntouched checks that an existing world-writable parent is not chmod-ed.
//
// Parameters:
//   - t: test to report to
func TestReviewSecExistingDirUntouched(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	c, _ := OpenConnections(filepath.Join(dir, "c.toml"))
	if err := c.Add(Connection{Name: "a", Target: "h"}); err != nil {
		t.Fatal(err)
	}
	if m := reviewMode(t, dir); m != 0o777 {
		t.Fatalf("dir mode changed to %v", m)
	}
	t.Log("a world-writable data directory is accepted silently (no warning, no refusal)")
}

// TestReviewSecReadOnlyFileReplaced checks that a 0400 data file is refused, not replaced.
//
// Parameters:
//   - t: test to report to
func TestReviewSecReadOnlyFileReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	if err := os.WriteFile(path, []byte("schema = 1\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	c, _ := OpenConnections(path)
	if err := c.Add(Connection{Name: "a", Target: "h"}); err == nil {
		t.Error("add on a 0400 file succeeded")
	}
	if got, _ := os.ReadFile(path); string(got) != "schema = 1\n" {
		t.Errorf("read-only file changed: %q", got)
	}
	if m := reviewMode(t, path); m != 0o400 {
		t.Errorf("mode changed to %v", m)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

// TestReviewSecHardLink checks that a hard link keeps the old content after a write.
//
// Parameters:
//   - t: test to report to
func TestReviewSecHardLink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	other := filepath.Join(dir, "other.toml")
	if err := os.WriteFile(path, []byte("schema = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, other); err != nil {
		t.Fatal(err)
	}
	c, _ := OpenConnections(path)
	if err := c.Add(Connection{Name: "a", Target: "h"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(other)
	if string(got) != "schema = 1\n" {
		t.Fatalf("hard linked file written through: %q", got)
	}
}

// TestReviewSecDirectoryAtPath checks that a directory in place of the file errors cleanly.
//
// Parameters:
//   - t: test to report to
func TestReviewSecDirectoryAtPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	c, err := OpenConnections(path)
	if err == nil || c == nil {
		t.Fatalf("expected error and non-nil collection, got %v %v", c, err)
	}
	if err := c.Add(Connection{Name: "a", Target: "h"}); err == nil {
		t.Fatal("add on a directory succeeded")
	}
	if _, err := os.Stat(filepath.Join(path, "x")); err == nil {
		t.Fatal("unexpected file")
	}
}

// TestReviewSecFIFOAtPath checks that a FIFO in place of the data file is refused, not waited on.
//
// Parameters:
//   - t: test to report to
func TestReviewSecFIFOAtPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := OpenConnections(path)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as a data file")
		}
	case <-time.After(2 * time.Second):
		// Unblock the reader so the goroutine ends.
		f, _ := os.OpenFile(path, os.O_WRONLY, 0)
		if f != nil {
			_ = f.Close()
		}
		t.Fatal("Open hangs forever on a FIFO (no regular-file check)")
	}
}

// TestReviewSecDeepNesting checks that a hostile file of deeply nested brackets is refused.
//
// Parameters:
//   - t: test to report to
func TestReviewSecDeepNesting(t *testing.T) {
	if os.Getenv("TOTE_REVIEW_CHILD") != "" {
		p := os.Getenv("TOTE_REVIEW_CHILD")
		c, err := OpenConnections(p)
		if err != nil || c.Err() == nil {
			t.Logf("child survived: %v %v", err, c.Err())
		}
		return
	}
	p := filepath.Join(t.TempDir(), "c.toml")
	n := 3000000
	body := "schema = 1\nx = " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestReviewSecDeepNesting$")
	cmd.Env = append(os.Environ(), "TOTE_REVIEW_CHILD="+p)
	out, err := cmd.CombinedOutput()
	if err != nil {
		head := string(out)
		if len(head) > 200 {
			head = head[:200]
		}
		t.Fatalf("a %d byte file kills the process: %v: %s", len(body), err, head)
	}
}

// TestReviewSecHostileValues checks that hostile values are rejected by Validate.
//
// Parameters:
//   - t: test to report to
func TestReviewSecHostileValues(t *testing.T) {
	inj := "x\"\n[[connection]]\nname = \"evil\"\ntarget = \"z"
	bad := []interface{ Validate() error }{
		Connection{Name: "a\n[[connection]]", Target: "h"},
		Connection{Name: "a", Target: inj},
		Connection{Name: "a", Target: "h", Via: "ssh\n[[connection]]"},
		Connection{Name: "a", Target: "h", User: inj},
		Connection{Name: "a", Target: "h", Aliases: []string{inj}},
		Connection{Name: "a", Target: "h", IP: inj},
		Connection{Name: "a", Target: "h", Desc: inj},
		Connection{Name: "a", Target: "h\x00"},
		Connection{Name: "a", Target: "h", Desc: "x\x00y"},
		Connection{Name: "a", Target: "h\u00a0x"},
		Connection{Name: "a", Target: "h\u200bx"},
		Alias{Name: "a", Cmd: "x\x00y"},
		Alias{Name: "a", Cmd: inj},
		Alias{Name: inj, Cmd: "x"},
		Repo{Name: "r", Path: "/x\x00"},
		Repo{Name: "r", Path: inj},
		Func{Name: "f", Kind: KindExec, Script: inj},
		Func{Name: "f\n", Kind: KindExec, Script: "x"},
	}
	for i, r := range bad {
		if r.Validate() == nil {
			t.Errorf("case %d accepted: %#v", i, r)
		}
	}
}

// TestReviewSecBidiDesc checks that invisible format characters are refused by ValidDesc.
//
// Parameters:
//   - t: test to report to
func TestReviewSecBidiDesc(t *testing.T) {
	for _, r := range []rune{'\u202e', '\u2066', '\u200b', '\ufeff', '\u061c'} {
		if ValidDesc("prod "+string(r)+"db") == nil {
			t.Errorf("U+%04X accepted in desc", r)
		}
	}
}

// TestReviewSecEncoderRoundTrip checks hostile strings through the encoder with no validation.
//
// Parameters:
//   - t: test to report to
func TestReviewSecEncoderRoundTrip(t *testing.T) {
	vals := []string{
		"a\n[[rec]]\nname = \"evil\"",
		"\"\"\"\n[[rec]]\n\"\"\"",
		"'''\n[[rec]]\n'''",
		"x\r\n[[rec]]\r\n",
		"\x00\x01\x1b[2J",
		"\u2028[[rec]]",
		"a\\",
		"]]\n[[rec]]",
		"name = \"x\"",
		strings.Repeat("[", 5000),
	}
	path := filepath.Join(t.TempDir(), "r.toml")
	c, err := Open[rawRec](path, "rec")
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range vals {
		r := rawRec{Name: "n" + string(rune('a'+i)), Val: v, Tags: []string{v, "[[rec]]"}}
		if err := c.Add(r); err != nil {
			t.Fatalf("add %q: %v", v, err)
		}
	}
	// Hostile keys too.
	for _, k := range []string{"k\n[[rec]]", "k\"", "k\x00", "k]]", "\u202e"} {
		if err := c.Add(rawRec{Name: k, Val: "v"}); err != nil {
			t.Fatalf("add key %q: %v", k, err)
		}
	}
	var top reviewDoc
	data, _ := os.ReadFile(path)
	if _, err := toml.Decode(string(data), &top); err != nil {
		t.Fatalf("file does not parse: %v", err)
	}
	if got, want := len(top.Rec), len(vals)+5; got != want {
		t.Fatalf("decoded %d records, want %d", got, want)
	}
	if n := len(scanBlocks(data, "rec").Blocks); n != len(vals)+5 {
		t.Fatalf("scanner sees %d blocks", n)
	}
	c2, _ := Open[rawRec](path, "rec")
	if c2.Err() != nil || len(c2.List()) != len(vals)+5 {
		t.Fatalf("reload: %v %d", c2.Err(), len(c2.List()))
	}
	// Edit and delete every record: the other bytes must survive.
	for _, r := range c2.List() {
		if err := c2.Replace(r.Name, rawRec{Name: r.Name, Val: "changed"}); err != nil {
			t.Fatalf("replace %q: %v", r.Name, err)
		}
	}
	for _, r := range c2.List() {
		if err := c2.Delete(r.Name); err != nil {
			t.Fatalf("delete %q: %v", r.Name, err)
		}
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "[[rec]]") {
		t.Fatalf("blocks left:\n%s", data)
	}
}

// reviewHostileFile checks that edits of a hand-edited file leave a file that parses.
//
// Parameters:
//   - t: test to report to
//   - content: file content
func reviewHostileFile(t *testing.T, content string) {
	path := filepath.Join(t.TempDir(), "r.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Open[rawRec](path, "rec")
	if err != nil || c == nil {
		t.Fatalf("open: %v", err)
	}
	if c.Err() != nil {
		if werr := c.Add(rawRec{Name: "zz"}); werr == nil {
			t.Fatal("write on read-only collection succeeded")
		}
		got, _ := os.ReadFile(path)
		if string(got) != content {
			t.Fatal("read-only collection modified the file")
		}
		return
	}
	if err := c.Add(rawRec{Name: "zz", Val: "v"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := c.Delete("zz"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var top reviewDoc
	data, _ := os.ReadFile(path)
	if _, err := toml.Decode(string(data), &top); err != nil {
		t.Fatalf("result does not parse: %v", err)
	}
	if len(top.Rec) != len(c.List()) {
		t.Fatalf("decoded %d, collection %d", len(top.Rec), len(c.List()))
	}
}

// TestReviewSecHostileFiles feeds tricky hand-edited files and checks edits stay consistent.
//
// Parameters:
//   - t: test to report to
func TestReviewSecHostileFiles(t *testing.T) {
	files := map[string]string{
		"ml-basic": "schema = 1\n[[rec]]\nname = \"a\"\nval = \"\"\"\n[[rec]]\nname = \"fake\"\n\"\"\"\n",
		"ml-lit":   "schema = 1\n[[rec]]\nname = \"a\"\nval = '''\n[[rec]]\n'''\n",
		"array":    "schema = 1\n[[rec]]\nname = \"a\"\ntags = [\n  \"x\",\n# ]\n[[rec]]\n  \"y\",\n]\n",
		"inline":   "schema = 1\n[[rec]]\nname = \"a\"\nval = \"q\" # [[rec]]\n",
		"bom":      "\ufeffschema = 1\n[[rec]]\nname = \"a\"\n",
		"cr":       "schema = 1\r[[rec]]\rname = \"a\"\r",
		"nul":      "schema = 1\n[[rec]]\nname = \"a\x00\"\n",
		"quoted":   "schema = 1\n[[rec]]\nname = \"a\"\n\"[[rec]]\" = 1\n",
		"dotted":   "schema = 1\nrec = [{name = \"a\"}]\n[[rec]]\nname = \"b\"\n",
		"subtable": "schema = 1\n[[rec]]\nname = \"a\"\n[rec.sub]\nx = 1\n[[rec]]\nname = \"b\"\n",
		"two-last": "schema = 1\n[[rec]]\nname = \"a\"\nval = \"\"\"x\n[[rec]]\"\"\"\n" +
			"[[rec]]\nname = \"b\"\n",
		"escaped": "schema = 1\n[[rec]]\nname = \"a\"\nval = \"\\\n[[rec]]\"\n",
	}
	for name, content := range files {
		t.Run(name, func(t *testing.T) { reviewHostileFile(t, content) })
	}
}

// TestReviewSecLargeFile checks that a file above the size cap is refused.
//
// Parameters:
//   - t: test to report to
func TestReviewSecLargeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("# padding padding padding padding padding padding\n", 1000)
	for range 400 {
		_, _ = f.WriteString(line)
	}
	_ = f.Close()
	info, _ := os.Stat(path)
	start := time.Now()
	c, err := OpenConnections(path)
	t.Logf("%d byte file: err=%v readonly=%v in %v", info.Size(), err, c.Err() != nil,
		time.Since(start))
	if err == nil && c.Err() == nil {
		t.Fatal("no size cap: a file of that size is read fully into memory")
	}
}
