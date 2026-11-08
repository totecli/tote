package update

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// write creates a file and its parent directories.
//
// Parameters:
//   - t: running test
//   - path: file to create
//   - text: content
func write(t *testing.T, path, text string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// exists reports whether a path exists, without following a final link.
//
// Parameters:
//   - path: path to test
func exists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

// TestPurgeDir checks that only the named entries go, that the directory goes when it is
// empty and stays otherwise.
//
// Parameters:
//   - t: test to report to
func TestPurgeDir(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "tote")

	write(t, filepath.Join(dir, "connections.toml"), "x")
	write(t, filepath.Join(dir, "docs", "a", "b.md"), "x")
	write(t, filepath.Join(dir, "mine.txt"), "keep")

	d := DataDir{Dir: dir, Entries: []string{"connections.toml", "docs", "absent.toml"}}

	out, err := PurgeDir(d, home)
	if err != nil {
		t.Fatal(err)
	}

	if !out.Kept || len(out.Removed) != 2 {
		t.Errorf("purged = %+v", out)
	}

	if exists(filepath.Join(dir, "connections.toml")) || exists(filepath.Join(dir, "docs")) {
		t.Error("an entry was not removed")
	}

	if content(t, filepath.Join(dir, "mine.txt")) != "keep" {
		t.Error("a file that is not tote's was removed")
	}

	if err := os.Remove(filepath.Join(dir, "mine.txt")); err != nil {
		t.Fatal(err)
	}

	out, err = PurgeDir(d, home)
	if err != nil || out.Kept || len(out.Removed) != 0 {
		t.Errorf("second purge = %+v, %v", out, err)
	}

	if exists(dir) {
		t.Error("the empty directory was kept")
	}

	if !exists(filepath.Join(home, ".config")) {
		t.Error("the parent directory was removed")
	}

	if out, err := PurgeDir(d, home); err != nil || out.Kept {
		t.Errorf("purge of a missing directory = %+v, %v", out, err)
	}
}

// TestPurgeDirRefusals checks the directories and entries that are refused.
//
// Parameters:
//   - t: test to report to
func TestPurgeDirRefusals(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "precious"), "x")

	link := filepath.Join(home, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		d    DataDir
	}{
		{"root", DataDir{Dir: "/", Entries: []string{"x"}}},
		{"home", DataDir{Dir: home, Entries: []string{"x"}}},
		{"parent of home", DataDir{Dir: filepath.Dir(home), Entries: []string{"x"}}},
		{"relative", DataDir{Dir: "relative/tote", Entries: []string{"x"}}},
		{"empty", DataDir{Entries: []string{"x"}}},
		{"unclean", DataDir{Dir: home + "/a/../b", Entries: []string{"x"}}},
		{"symbolic link", DataDir{Dir: link, Entries: []string{"precious"}}},
		{"dot dot entry", DataDir{Dir: filepath.Join(home, "d"), Entries: []string{".."}}},
		{"dot entry", DataDir{Dir: filepath.Join(home, "d"), Entries: []string{"."}}},
		{"empty entry", DataDir{Dir: filepath.Join(home, "d"), Entries: []string{""}}},
		{"path entry", DataDir{Dir: filepath.Join(home, "d"), Entries: []string{"a/b"}}},
		{
			"escaping entry",
			DataDir{Dir: filepath.Join(home, "d"), Entries: []string{"../" + filepath.Base(outside)}},
		},
	}

	for _, tt := range tests {
		if _, err := PurgeDir(tt.d, home); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("%s: error = %v, want ErrUnsafePath", tt.name, err)
		}
	}

	if !exists(filepath.Join(outside, "precious")) || !exists(home) {
		t.Error("something outside the purged directories was removed")
	}
}

// TestPurgeDirLinkEntries checks that a symbolic link inside a purged directory is unlinked,
// never followed.
//
// Parameters:
//   - t: test to report to
func TestPurgeDirLinkEntries(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "precious"), "x")

	dir := filepath.Join(home, "tote")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(dir, "docs")); err != nil {
		t.Fatal(err)
	}

	if _, err := PurgeDir(DataDir{Dir: dir, Entries: []string{"docs"}}, home); err != nil {
		t.Fatal(err)
	}

	if exists(filepath.Join(dir, "docs")) || !exists(filepath.Join(outside, "precious")) {
		t.Error("the link was followed or kept")
	}
}

// TestShellLines checks which lines of a startup file count as the tote block.
//
// Parameters:
//   - t: test to report to
func TestShellLines(t *testing.T) {
	text := "export A=1\n" +
		"eval \"$(tote init bash)\"\n" +
		"  eval \"$(tote init zsh)\"  \n" +
		"eval \"$(command tote init bash)\"\n" +
		"# eval \"$(tote init bash)\"\n" +
		"eval \"$(tote init fish)\"\n" +
		"eval \"$(tote init bash --defs)\"\n" +
		"eval \"$(tote init bash)\" && echo hi\n" +
		"eval $(tote init bash)\n" +
		"alias tote=x\n" +
		"echo \"run tote init bash to load it\"\n"

	path := filepath.Join(t.TempDir(), ".bashrc")
	write(t, path, text)

	got, err := ShellLines(path)
	if err != nil || !slices.Equal(got, []int{2, 3, 4, 7, 9}) {
		t.Errorf("ShellLines = %v, %v", got, err)
	}

	got, err = ShellMentions(path)
	if err != nil || !slices.Equal(got, []int{8, 11}) {
		t.Errorf("ShellMentions = %v, %v, want the chained load and the echo", got, err)
	}

	if got, err := ShellLines(filepath.Join(t.TempDir(), "none")); err != nil || len(got) != 0 {
		t.Errorf("missing file = %v, %v", got, err)
	}

	if _, err := ShellLines(filepath.Dir(path)); err == nil {
		t.Error("a directory was read")
	}
}

// loadsToteCases is the table of lines for LoadsTote. Every spelling that "tote doctor" reports
// as loading tote and that is safe to delete as a whole line is true, and a line that does more
// or only mentions tote is false.
var loadsToteCases = []struct {
	line string
	want bool
}{
	{`eval "$(tote init bash)"`, true},
	{`eval "$(tote init zsh)"`, true},
	{`eval "$(command tote init bash)"`, true},
	{`eval $(tote init bash)`, true},
	{"eval \"`tote init bash`\"", true},
	{"eval `tote init zsh`", true},
	{`  eval "$(tote init bash)"  `, true},
	{"\teval \"$(tote init bash)\"\r\n", true},
	{`eval "$(tote init bash)" # tote`, true},
	{`eval "$(tote init bash)"  # x`, true},
	{`eval "$(tote init bash)";`, true},
	{`eval "$(tote init bash)";# x`, true},
	{`eval "$(tote init bash --defs)"`, true},
	{`source <(tote init bash)`, true},
	{`. <(tote init zsh)`, true},
	{`source <(tote init bash) # tote`, true},
	{`command -v tote >/dev/null && eval "$(tote init bash)"`, true},
	{`command -v tote >/dev/null 2>&1 && eval "$(tote init bash)"`, true},
	{`command -v tote &>/dev/null && source <(tote init bash)`, true},
	{`type tote >/dev/null 2>&1 && eval "$(tote init zsh)"`, true},
	{`hash tote 2>/dev/null && eval "$(tote init bash)"`, true},
	{`if command -v tote >/dev/null 2>&1; then eval "$(tote init bash)"; fi`, true},
	{`if command -v tote; then source <(tote init bash) fi`, true},
	{`if command -v tote >/dev/null; then eval "$(tote init zsh)"; fi # tote`, true},
	{``, false},
	{`# eval "$(tote init bash)"`, false},
	{`eval "$(tote init fish)"`, false},
	{`eval "$(tote init)"`, false},
	{`eval "$(tote init bash)" && echo hi`, false},
	{`eval "$(tote init bash)"; echo hi`, false},
	{`echo hi; eval "$(tote init bash)"`, false},
	{`eval "$(tote init bash)" | tee x`, false},
	{`eval "$(tote init bash)"# x`, false},
	{`eval '$(tote init bash)'`, false},
	{`eval "$(tote init bash) $(rm -rf x)"`, false},
	{`echo "run tote init bash"`, false},
	{`alias tote='eval "$(tote init bash)"'`, false},
	{`export PATH=$HOME/bin:$PATH # tote`, false},
	{`command -v tote >/dev/null && echo yes`, false},
	{`command -v tote && eval "$(tote init bash)" && echo hi`, false},
	{`if command -v tote; then`, false},
	{`if command -v tote; then eval "$(tote init bash)"; echo hi; fi`, false},
	{`eval "$(mytote init bash)"`, false},
}

// TestLoadsTote checks which lines count as the line that loads tote: the spellings that are
// safe to delete as a whole, and none that does something else or only mentions the command.
//
// Parameters:
//   - t: test to report to
func TestLoadsTote(t *testing.T) {
	for _, tt := range loadsToteCases {
		if got := LoadsTote(tt.line); got != tt.want {
			t.Errorf("LoadsTote(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

// TestRemoveShellLinesVariants checks that every spelling of the table goes from a startup file
// and that every line that must stay is kept byte for byte.
//
// Parameters:
//   - t: test to report to
func TestRemoveShellLinesVariants(t *testing.T) {
	var in, keep strings.Builder

	removed := 0

	for _, tt := range loadsToteCases {
		if tt.line == "" {
			continue
		}

		line := strings.TrimRight(tt.line, "\r\n") + "\n"
		in.WriteString(line)

		if tt.want {
			removed++
		} else {
			keep.WriteString(line)
		}
	}

	path := filepath.Join(t.TempDir(), ".bashrc")
	write(t, path, in.String())

	lines, err := ShellLines(path)
	if err != nil || len(lines) != removed {
		t.Fatalf("ShellLines = %v, %v, want %d lines", lines, err, removed)
	}

	n, err := RemoveShellLines(path)
	if err != nil || n != removed {
		t.Fatalf("RemoveShellLines = %d, %v, want %d", n, err, removed)
	}

	if got := content(t, path); got != keep.String() {
		t.Errorf("file after removal:\n%s\nwant:\n%s", got, keep.String())
	}

	if lines, err := ShellLines(path); err != nil || len(lines) != 0 {
		t.Errorf("lines left that load tote: %v, %v", lines, err)
	}
}

// TestRemoveShellLinesBlocks checks that a load which is the only command of a block leaves a
// no-op in its place, so that the file stays valid for the shell, and that a block with another
// command is not touched.
//
// Parameters:
//   - t: test to report to
func TestRemoveShellLinesBlocks(t *testing.T) {
	in := "if command -v tote >/dev/null; then\n" +
		"    eval \"$(tote init bash)\"\n" +
		"fi\n" +
		"if [ -n \"$X\" ]; then # c\n" +
		"\t# note\n" +
		"\teval \"$(tote init bash)\"\n" +
		"\n" +
		"else\n" +
		"    echo hi\n" +
		"    eval \"$(tote init zsh)\"\n" +
		"fi\n" +
		"f() {\n" +
		"    eval \"$(tote init bash)\"\n" +
		"}\n"
	want := "if command -v tote >/dev/null; then\n" +
		"    :\n" +
		"fi\n" +
		"if [ -n \"$X\" ]; then # c\n" +
		"\t# note\n" +
		"\n" +
		"\t:\n" +
		"else\n" +
		"    echo hi\n" +
		"fi\n" +
		"f() {\n" +
		"    :\n" +
		"}\n"

	path := filepath.Join(t.TempDir(), ".bashrc")
	write(t, path, in)

	if n, err := RemoveShellLines(path); err != nil || n != 4 {
		t.Fatalf("RemoveShellLines = %d, %v", n, err)
	}

	if got := content(t, path); got != want {
		t.Errorf("file after removal:\n%s\nwant:\n%s", got, want)
	}

	if bash, err := exec.LookPath("bash"); err == nil {
		if out, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
			t.Errorf("bash -n: %v: %s", err, out)
		}
	}
}

// TestRemoveShellLines checks the rewrite of a startup file: only the block goes, the mode and
// the other bytes stay, and a link is followed.
//
// Parameters:
//   - t: test to report to
func TestRemoveShellLines(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "bashrc")
	link := filepath.Join(dir, ".bashrc")

	write(t, real, "export A=1\neval \"$(tote init bash)\"\r\nalias x=y\nno newline at end")

	if err := os.Chmod(real, 0o640); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	n, err := RemoveShellLines(link)
	if err != nil || n != 1 {
		t.Fatalf("RemoveShellLines = %d, %v", n, err)
	}

	if got := content(t, real); got != "export A=1\nalias x=y\nno newline at end" {
		t.Errorf("file = %q", got)
	}

	info, _ := os.Lstat(link)
	rinfo, _ := os.Stat(real)

	if info.Mode()&os.ModeSymlink == 0 || rinfo.Mode().Perm() != 0o640 {
		t.Errorf("link %v, mode %v", info.Mode(), rinfo.Mode().Perm())
	}

	if n, err := RemoveShellLines(link); err != nil || n != 0 {
		t.Errorf("second run = %d, %v", n, err)
	}

	if n, err := RemoveShellLines(filepath.Join(dir, "none")); err != nil || n != 0 {
		t.Errorf("missing file = %d, %v", n, err)
	}

	if got := names(t, filepath.Dir(real)); !slices.Equal(got, []string{"bashrc"}) {
		t.Errorf("directory holds %v", got)
	}
}

// TestCanRemove checks the permission check made before a binary is removed.
//
// Parameters:
//   - t: test to report to
func TestCanRemove(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tote")

	if err := CanRemove(file); err != nil {
		t.Errorf("writable directory: %v", err)
	}

	if os.Geteuid() == 0 {
		return
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	var nw *NotWritableError
	err := CanRemove(file)
	if !errors.Is(err, ErrNotWritable) || !errors.As(err, &nw) || nw.Dir != dir {
		t.Errorf("read-only directory: %v", err)
	}
}
