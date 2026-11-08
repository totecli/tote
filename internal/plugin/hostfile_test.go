package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestWriteHostFile checks the normal case: the directory is created 0700, the file 0600, the
// content is replaced on a second write, and a longer first content leaves no tail.
//
// Parameters:
//   - t: test to report to
func TestWriteHostFile(t *testing.T) {
	for _, mask := range []int{0, 0o077} {
		old := syscall.Umask(mask)

		l := newLab(t)
		must(t, WriteHostFile(l.paths, "demo", "last.log", []byte("a long first content"),
			os.Getuid()))
		must(t, WriteHostFile(l.paths, "demo", "last.log", []byte("short"), os.Getuid()))

		syscall.Umask(old)

		dir, _ := l.paths.PluginHostDir("demo")
		assertMode(t, dir, 0o700)
		assertMode(t, filepath.Join(dir, "last.log"), 0o600)
		assertMode(t, filepath.Dir(dir), 0o700)

		if data, _ := os.ReadFile(filepath.Join(dir, "last.log")); string(data) != "short" {
			t.Errorf("content = %q", data)
		}
	}
}

// TestWriteHostFileRefuses checks every way the destination can be hostile or wrong: a link in
// place of the file, of the directory, or of the directory above; a directory that is not private
// or not ours; a file that is not a regular private file; and a name or an id outside the grammar.
// Nothing is written through any of them.
//
// Parameters:
//   - t: test to report to
func TestWriteHostFileRefuses(t *testing.T) {
	uid := os.Getuid()

	tests := []struct {
		name   string
		setup  func(t *testing.T, l *lab, dir, victim string)
		id     string
		file   string
		uid    int
		unsafe bool
	}{
		{"file is a symlink", func(t *testing.T, l *lab, dir, victim string) {
			must(t, os.MkdirAll(dir, 0o700))
			must(t, os.Symlink(victim, filepath.Join(dir, "last.log")))
		}, "demo", "last.log", uid, true},
		{"file is a dangling symlink", func(t *testing.T, l *lab, dir, victim string) {
			must(t, os.MkdirAll(dir, 0o700))
			must(t, os.Symlink(victim+".new", filepath.Join(dir, "last.log")))
		}, "demo", "last.log", uid, true},
		{"directory is a symlink", func(t *testing.T, l *lab, dir, victim string) {
			must(t, os.MkdirAll(filepath.Dir(dir), 0o700))
			must(t, os.Mkdir(victim+".dir", 0o700))
			must(t, os.Symlink(victim+".dir", dir))
		}, "demo", "last.log", uid, true},
		{"directory is not private", func(t *testing.T, l *lab, dir, victim string) {
			must(t, os.MkdirAll(dir, 0o700))
			must(t, os.Chmod(dir, 0o755))
		}, "demo", "last.log", uid, true},
		{"directory is group writable", func(t *testing.T, l *lab, dir, victim string) {
			must(t, os.MkdirAll(dir, 0o700))
			must(t, os.Chmod(dir, 0o770))
		}, "demo", "last.log", uid, true},
		{"directory is a file", func(t *testing.T, l *lab, dir, victim string) {
			must(t, os.MkdirAll(filepath.Dir(dir), 0o700))
			must(t, os.WriteFile(dir, nil, 0o600))
		}, "demo", "last.log", uid, true},
		{
			"directory belongs to someone else", func(*testing.T, *lab, string, string) {},
			"demo", "last.log", uid + 1, true,
		},
		{"file is a directory", func(t *testing.T, l *lab, dir, victim string) {
			must(t, os.MkdirAll(filepath.Join(dir, "last.log"), 0o700))
		}, "demo", "last.log", uid, true},
		{"file is a pipe", func(t *testing.T, l *lab, dir, victim string) {
			must(t, os.MkdirAll(dir, 0o700))
			must(t, syscall.Mkfifo(filepath.Join(dir, "last.log"), 0o600))
		}, "demo", "last.log", uid, false},
		{"file is world readable", func(t *testing.T, l *lab, dir, victim string) {
			must(t, os.MkdirAll(dir, 0o700))
			must(t, os.WriteFile(filepath.Join(dir, "last.log"), nil, 0o644))
		}, "demo", "last.log", uid, true},
		{"name has a slash", func(*testing.T, *lab, string, string) {}, "demo", "../x", uid, true},
		{"name is empty", func(*testing.T, *lab, string, string) {}, "demo", "", uid, true},
		{"name has upper case", func(*testing.T, *lab, string, string) {}, "demo", "Last.log", uid, true},
		{"name starts with a dot", func(*testing.T, *lab, string, string) {}, "demo", ".x", uid, true},
		{
			"name is long", func(*testing.T, *lab, string, string) {}, "demo", "a" + times("b", 64),
			uid,
			true,
		},
		{"id is a path", func(*testing.T, *lab, string, string) {}, "../data", "last.log", uid, false},
		{"id is empty", func(*testing.T, *lab, string, string) {}, "", "last.log", uid, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			victim := filepath.Join(l.root, "victim")
			must(t, os.WriteFile(victim, []byte("precious"), 0o600))

			dir := filepath.Join(l.paths.State, "plugins-host", tt.id)
			if tt.id == "demo" {
				tt.setup(t, l, dir, victim)
			}

			err := WriteHostFile(l.paths, tt.id, tt.file, []byte("overwritten"), tt.uid)
			if err == nil {
				t.Fatal("the write went through")
			}

			if tt.unsafe && !errors.Is(err, ErrHostFile) {
				t.Errorf("error %v does not wrap ErrHostFile", err)
			}

			if data, _ := os.ReadFile(victim); string(data) != "precious" {
				t.Errorf("the victim was written: %q", data)
			}

			if _, err := os.Stat(filepath.Join(l.root, "data", "x")); err == nil {
				t.Error("a file escaped the host directory")
			}
		})
	}
}

// TestWriteHostFileNeverFollowsTheStateDirectory is the scenario of the specification: the plugin
// owns <State>/plugins/<id> and swaps last.log in it for a symlink to a file the user cares
// about. The host writes its log to plugins-host and so never meets the link.
//
// Parameters:
//   - t: test to report to
func TestWriteHostFileNeverFollowsTheStateDirectory(t *testing.T) {
	l := newLab(t)

	victim := filepath.Join(l.home, ".gitconfig")
	must(t, os.WriteFile(victim, []byte("[user]\n"), 0o600))

	stateDir, _ := l.paths.PluginStateDir("demo")
	must(t, os.MkdirAll(stateDir, 0o700))
	must(t, os.Symlink(victim, filepath.Join(stateDir, "last.log")))
	must(t, os.Symlink(victim, filepath.Join(stateDir, "last.log.lock")))

	for range 3 {
		must(t, WriteHostFile(l.paths, "demo", "last.log", []byte("log\n"), os.Getuid()))
	}

	if data, _ := os.ReadFile(victim); string(data) != "[user]\n" {
		t.Errorf("gitconfig = %q", data)
	}

	hostDir, _ := l.paths.PluginHostDir("demo")
	if strings.HasPrefix(hostDir, stateDir) || strings.HasPrefix(stateDir, hostDir) {
		t.Errorf("the host directory %s overlaps the plugin state %s", hostDir, stateDir)
	}
}

// TestWriteHostFileSiblingLink checks a link that stays inside the host directory, which a root
// confined to the directory would follow: last.log pointing to another host file must be refused
// as well, or one host file could be overwritten through the name of another.
//
// Parameters:
//   - t: test to report to
func TestWriteHostFileSiblingLink(t *testing.T) {
	l := newLab(t)

	dir, _ := l.paths.PluginHostDir("demo")
	must(t, os.MkdirAll(dir, 0o700))
	must(t, os.WriteFile(filepath.Join(dir, "other"), []byte("keep"), 0o600))
	must(t, os.Symlink("other", filepath.Join(dir, "last.log")))

	err := WriteHostFile(l.paths, "demo", "last.log", []byte("overwritten"), os.Getuid())
	if !errors.Is(err, ErrHostFile) {
		t.Errorf("error = %v, want ErrHostFile", err)
	}

	if data, _ := os.ReadFile(filepath.Join(dir, "other")); string(data) != "keep" {
		t.Errorf("the sibling was written through the link: %q", data)
	}
}
