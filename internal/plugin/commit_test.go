package plugin

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/totecli/tote/internal/config"
)

// points lists the points of an install in order, and the first one after the switch: from
// PointC2After on, the lock record already names the new version.
var points = []string{
	PointStaged, PointC1Before, PointC1Renamed, PointC1After, PointC2Before,
	PointC2After, PointC3Before, PointC3After, PointC4Before, PointC4After,
}

// switched reports whether the lock record names the new version once a point is reached.
//
// Parameters:
//   - point: one of the Point constants
func switched(point string) bool {
	for i, p := range points {
		if p == point {
			return i >= 5
		}
	}

	return false
}

// withHook runs a function with the install hook replaced and puts the old one back.
//
// Parameters:
//   - t: test to report to
//   - hook: the new hook
func withHook(t *testing.T, hook func(string) error) {
	t.Helper()

	old := installHook
	installHook = hook

	t.Cleanup(func() { installHook = old })
}

// failAt makes the hook return an error at one point, the way a full disk would.
//
// Parameters:
//   - point: point that fails
func failAt(point string) func(string) error {
	return func(p string) error {
		if p == point {
			return errors.New("injected failure")
		}

		return nil
	}
}

// checkInvariants asserts what must hold after any interruption of an install, whether an error
// or a crash: the lock parses; every record has its tree, which hashes to the record; no plugin
// is enabled without a consent; and the plugin runs the version that is expected, none when the
// expectation is empty.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
//   - id: plugin id
//   - version: version that must run, empty for none
func checkInvariants(t *testing.T, l *lab, id, version string) {
	t.Helper()

	lk, err := OpenLock(l.paths.LockFile())
	if err != nil || lk.Err() != nil {
		t.Fatalf("the lock does not parse: %v %v", err, lk)
	}

	r := l.reg(nil)

	for _, rec := range lk.Records() {
		dir, err := l.paths.PluginTree(rec.ID, TreeID(rec.TreeSHA256))
		if err != nil {
			t.Fatal(err)
		}

		tree, err := HashTree(dir)
		if err != nil || tree.SHA256 != rec.TreeSHA256 {
			t.Errorf("record %s has no tree that matches: %v", rec.ID, err)
		}
	}

	for _, p := range r.Load().Plugins() {
		if p.State == StateEnabled && !p.Record.Consented() {
			t.Errorf("%s runs without consent", p.ID)
		}
	}

	p, ok := r.Recheck(id)

	switch {
	case version == "" && ok && p.Installed:
		t.Errorf("%s is installed: %+v", id, p.Record)
	case version != "" && (!ok || p.State != StateEnabled || p.Record.Version != version):
		t.Errorf("%s = %v %s %v, want enabled %s", id, ok, p.State, reasonCodes(p.Reasons), version)
	}
}

// TestInstallFailureAtEveryPoint injects an I/O failure at each point of the install of a new
// plugin and of an update, and checks the invariants: a failure before the switch leaves the
// previous version running (or nothing) and no orphan tree, staging or mutex behind; a failure
// after the switch is a warning and the new version stands.
//
// Parameters:
//   - t: test to report to
func TestInstallFailureAtEveryPoint(t *testing.T) {
	for _, update := range []bool{false, true} {
		for _, point := range points {
			name := point + " new"
			if update {
				name = point + " update"
			}

			t.Run(name, func(t *testing.T) {
				l := newLab(t)
				plain := surfaceBase("", "", "")

				var (
					r   *Registry
					dir string
				)

				if update {
					r, dir = seeded(t, l, plain)
					rewrite(t, dir, withVersion(plain, "1.1.0"))
				} else {
					l.fakeBin("tool")
					dir = l.srcDir("demo", map[string]string{"plugin.toml": plain})
					r = l.reg(nil)
				}

				withHook(t, failAt(point))

				st, err := r.Plan(t.Context(), InstallRequest{Source: dir})

				switch {
				case point == PointStaged:
					if err == nil {
						t.Fatal("Plan ignored the failure")
					}
				case err != nil:
					t.Fatal(err)
				default:
					res, cerr := st.Commit(Grant{Prompt: true})

					if switched(point) {
						if cerr != nil || len(res.Warnings) == 0 ||
							res.Warnings[0].Code != ReasonStepFailed {
							t.Fatalf("a failure after the switch: %+v %v", res, cerr)
						}
					} else if cerr == nil {
						t.Fatal("Commit ignored the failure")
					}
				}

				want := ""
				if update {
					want = "1.0.0"
				}

				if switched(point) {
					want = "1.1.0"
					if !update {
						want = "1.0.0"
					}
				}

				checkInvariants(t, l, "demo", want)

				if names := stagingNames(t, l); len(names) != 0 {
					t.Errorf("staging = %v", names)
				}

				idDir, _ := l.paths.PluginDir("demo")
				entries, _ := os.ReadDir(idDir)

				trees := 1
				if want == "" {
					trees = 0
				}

				if len(entries) != trees {
					t.Errorf("trees = %v, want %d", entries, trees)
				}

				// The mutex was released: another change goes through at once.
				installHook = func(string) error { return nil }

				if _, err := r.Cleanup(); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

// TestCrashHelper is not a test: it is the process that TestInstallCrashAtEveryPoint starts and
// kills at a point of an install, as a crash would. It does nothing unless the parent set its
// environment.
//
// Parameters:
//   - t: test to report to
func TestCrashHelper(t *testing.T) {
	root := os.Getenv("TOTE_CRASH_ROOT")
	if root == "" {
		t.Skip("helper of TestInstallCrashAtEveryPoint")
	}

	at := os.Getenv("TOTE_CRASH_AT")

	installHook = func(p string) error {
		if p == at {
			os.Exit(97)
		}

		return nil
	}

	r := crashRegistry(root)

	st, err := r.Plan(t.Context(), InstallRequest{Source: os.Getenv("TOTE_CRASH_SRC")})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.Commit(Grant{Prompt: true}); err != nil {
		t.Fatal(err)
	}
}

// crashRegistry opens the registry of a sandbox from its root alone, as the helper process must.
//
// Parameters:
//   - root: root of the sandbox
func crashRegistry(root string) *Registry {
	env := map[string]string{
		"PATH":            filepath.Join(root, "bin"),
		"TOTE_CONFIG_DIR": filepath.Join(root, "config"),
		"TOTE_DATA_DIR":   filepath.Join(root, "data"),
		"TOTE_STATE_DIR":  filepath.Join(root, "state"),
		"TOTE_CACHE_DIR":  filepath.Join(root, "cache"),
	}

	home := filepath.Join(root, "home")
	paths, _ := config.Resolve(func(k string) string { return env[k] }, home)

	return Open(Options{
		Paths: paths, Host: testHost(), Home: home,
		Cwd: filepath.Join(root, "cwd"), Bin: "/usr/local/bin/tote",
		Lookup: func(k string) (string, bool) { v, ok := env[k]; return v, ok },
	})
}

// TestInstallCrashAtEveryPoint kills a real process at each point of the install of a new plugin
// and of an update, then checks the invariants from another registry, that the mutex is free, that
// a retry of the same install completes and reuses an orphan tree, and that the cleanup leaves
// nothing but the installed version.
//
// Parameters:
//   - t: test to report to
func TestInstallCrashAtEveryPoint(t *testing.T) {
	for _, update := range []bool{false, true} {
		for _, point := range points {
			name := point + " new"
			if update {
				name = point + " update"
			}

			t.Run(name, func(t *testing.T) {
				l := newLab(t)
				plain := surfaceBase("", "", "")

				var dir string

				if update {
					_, dir = seeded(t, l, plain)
					rewrite(t, dir, withVersion(plain, "1.1.0"))
				} else {
					l.fakeBin("tool")
					dir = l.srcDir("demo", map[string]string{"plugin.toml": plain})
				}

				cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
				cmd.Env = append(os.Environ(), "TOTE_CRASH_ROOT="+l.root, "TOTE_CRASH_AT="+point,
					"TOTE_CRASH_SRC="+dir)

				out, err := cmd.CombinedOutput()

				var ee *exec.ExitError
				if !errors.As(err, &ee) || ee.ExitCode() != 97 {
					t.Fatalf("the helper did not crash at %s: %v\n%s", point, err, out)
				}

				want := ""
				if update {
					want = "1.0.0"
				}

				if switched(point) {
					want = "1.1.0"
					if !update {
						want = "1.0.0"
					}
				}

				checkInvariants(t, l, "demo", want)

				r := l.reg(nil)

				if _, err := r.Cleanup(); err != nil {
					t.Fatal(err)
				}

				// A fresh stage may belong to a live install and survives the cleanup; an old one is
				// abandoned and goes at the next.
				if n := len(stagingNames(t, l)); (point == PointStaged || point == PointC1Before) !=
					(n == 1) {
					t.Errorf("staging after a fresh cleanup = %v", stagingNames(t, l))
				}

				ageStages(t, l)

				if _, err := r.Cleanup(); err != nil {
					t.Fatal(err)
				}

				// Cleanup leaves the tree of an id with no record: a retry reuses it.
				retry := plan(t, r, InstallRequest{Source: dir, Force: true})

				res, err := retry.Commit(Grant{Prompt: true})
				if err != nil {
					t.Fatalf("retry: %v", err)
				}

				if res.State != StateEnabled || res.Version != "1.0.0" && !update {
					t.Errorf("retry result = %+v", res)
				}

				rep, _ := r.Cleanup()
				if len(rep.Failed) != 0 {
					t.Errorf("cleanup failed on %v", rep.Failed)
				}

				if names := stagingNames(t, l); len(names) != 0 {
					t.Errorf("staging after the retry = %v", names)
				}

				rec, _ := l.lock().Get("demo")
				tree := l.treeDir(rec)

				checkMode(t, tree, 0o500)
				checkMode(t, filepath.Join(tree, "plugin.toml"), 0o400)

				idDir, _ := l.paths.PluginDir("demo")
				if entries, _ := os.ReadDir(idDir); len(entries) != 1 {
					t.Errorf("trees after the retry = %v", entries)
				}
			})
		}
	}
}

// TestCommitTreeCollision checks that a directory that has the name of the new tree but other
// content is never accepted: identity is the full hash, not the name, and the existing directory
// is left alone.
//
// Parameters:
//   - t: test to report to
func TestCommitTreeCollision(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, dest string){
		"other content": func(t *testing.T, dest string) {
			must(t, os.MkdirAll(dest, 0o700))
			must(t, os.WriteFile(filepath.Join(dest, "plugin.toml"), []byte("other"), 0o600))
		},
		"a file": func(t *testing.T, dest string) {
			must(t, os.WriteFile(dest, []byte("x"), 0o600))
		},
		"a link": func(t *testing.T, dest string) {
			must(t, os.Symlink(t.TempDir(), dest))
		},
		"an empty directory": func(t *testing.T, dest string) {
			must(t, os.MkdirAll(dest, 0o700))
		},
	} {
		t.Run(name, func(t *testing.T) {
			l := newLab(t)
			src := demoSource(l, "demo", "1.0.0")
			r := l.reg(nil)
			st := plan(t, r, InstallRequest{Source: src})

			dest, _ := l.paths.PluginTree("demo", TreeID(st.Diff().TreeSHA256))
			must(t, os.MkdirAll(filepath.Dir(dest), 0o700))
			setup(t, dest)

			before, _ := os.Lstat(dest)

			_, err := st.Commit(Grant{Prompt: true})
			if pe := asError(t, err); pe.Code != CodeTreeCollision || ExitOf(err) != ExitIntegrity {
				t.Fatalf("Commit = %v", err)
			}

			after, lerr := os.Lstat(dest)
			if lerr != nil || after.Mode() != before.Mode() {
				t.Errorf("the existing entry was touched: %v", lerr)
			}

			if _, ok := l.lock().Get("demo"); ok {
				t.Error("a collision installed the plugin")
			}

			if names := stagingNames(t, l); len(names) != 0 {
				t.Errorf("staging = %v", names)
			}
		})
	}
}

// TestConcurrentInstalls runs many installs at once, in one registry and in several, and checks
// that exactly one install of the same new id wins while the others lose with stale, that
// different ids all go through, and that no staging, orphan or broken record is left. Run it with
// -race.
//
// Parameters:
//   - t: test to report to
func TestConcurrentInstalls(t *testing.T) {
	t.Run("same id", func(t *testing.T) {
		l := newLab(t)
		src := demoSource(l, "demo", "1.0.0")

		const n = 8

		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			wins int
			lost []string
		)

		// Every install plans before any of them commits, whatever the number of CPUs, so that
		// each one sees the same empty state.
		var planned sync.WaitGroup

		start := make(chan struct{})

		planned.Add(n)

		for i := range n {
			wg.Add(1)

			go func() {
				defer wg.Done()

				r := l.reg(nil)
				if i%2 == 0 {
					r = l.reg(func(o *Options) {})
				}

				st, err := r.Plan(t.Context(), InstallRequest{Source: src})

				planned.Done()

				if err != nil {
					t.Error(err)

					return
				}

				<-start

				_, err = st.Commit(Grant{Prompt: true})

				mu.Lock()
				defer mu.Unlock()

				if err == nil {
					wins++

					return
				}

				lost = append(lost, asError(t, err).Code)
			}()
		}

		planned.Wait()
		close(start)
		wg.Wait()

		if wins != 1 {
			t.Errorf("%d installs won, lost with %v", wins, lost)
		}

		for _, code := range lost {
			if code != CodeStale {
				t.Errorf("a loser failed with %s", code)
			}
		}

		checkInvariants(t, l, "demo", "1.0.0")

		if names := stagingNames(t, l); len(names) != 0 {
			t.Errorf("staging = %v", names)
		}

		idDir, _ := l.paths.PluginDir("demo")
		if entries, _ := os.ReadDir(idDir); len(entries) != 1 {
			t.Errorf("trees = %v", entries)
		}
	})

	t.Run("different ids", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")

		ids := []string{"aa", "bb", "cc", "dd", "ee", "ff"}

		var wg sync.WaitGroup

		for _, id := range ids {
			dir := l.srcDir(id, map[string]string{"plugin.toml": manifest(id, "1.0.0",
				[]string{"exec:tool"}, cmdWith("go", ""))})

			wg.Add(1)

			go func() {
				defer wg.Done()

				r := l.reg(nil)

				st, err := r.Plan(t.Context(), InstallRequest{Source: dir})
				if err != nil {
					t.Error(err)

					return
				}

				if _, err := st.Commit(Grant{Prompt: true}); err != nil {
					t.Error(err)
				}
			}()
		}

		wg.Wait()

		for _, id := range ids {
			checkInvariants(t, l, id, "1.0.0")
		}

		if names := stagingNames(t, l); len(names) != 0 {
			t.Errorf("staging = %v", names)
		}
	})
}

// TestMutex checks that a change waits for the mutex, then gives up with busy and keeps the
// staged package, that cleanup does not wait, and that disable is never held up by it.
//
// Parameters:
//   - t: test to report to
func TestMutex(t *testing.T) {
	l := newLab(t)
	plain := surfaceBase("", "", "")
	r, dir := seeded(t, l, plain)

	rewrite(t, dir, withVersion(plain, "1.1.0"))

	st := plan(t, r, InstallRequest{Source: dir})

	old := mutexWait
	mutexWait = 50 * time.Millisecond

	t.Cleanup(func() { mutexWait = old })

	pd, err := r.resolvePlugins(true)
	must(t, err)

	held, err := r.lockChanges(pd, time.Second)
	must(t, err)

	start := time.Now()

	if _, err := st.Commit(Grant{Prompt: true}); asError(t, err).Code != CodeBusy ||
		ExitOf(err) != ExitPolicy {
		t.Errorf("Commit = %v", err)
	}

	if time.Since(start) < 40*time.Millisecond {
		t.Error("Commit did not wait for the mutex")
	}

	if rep, err := r.Cleanup(); err != nil || !rep.Busy {
		t.Errorf("Cleanup = %+v %v", rep, err)
	}

	if _, err := r.Remove("demo", RemoveOptions{}); asError(t, err).Code != CodeBusy {
		t.Errorf("Remove = %v", err)
	}

	if res, err := r.Disable("demo"); err != nil || !res.LockFlag {
		t.Errorf("Disable under the mutex = %+v %v", res, err)
	}

	held.release()

	if _, err := st.Commit(Grant{Prompt: true}); err != nil {
		t.Errorf("Commit after the release = %v", err)
	}

	if rec, _ := l.lock().Get("demo"); rec.Version != "1.1.0" || !rec.Disabled {
		t.Errorf("record = %+v", rec)
	}
}

// TestMutexFile checks that a .mutex that is a link or not private is refused rather than locked.
//
// Parameters:
//   - t: test to report to
func TestMutexFile(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")
	r := l.reg(nil)
	st := plan(t, r, InstallRequest{Source: src})

	victim := filepath.Join(l.root, "victim")
	must(t, os.WriteFile(victim, []byte("keep"), 0o600))
	mutex := filepath.Join(l.paths.PluginsDir(), ".mutex")

	if _, err := os.Lstat(mutex); err == nil {
		must(t, os.Rename(mutex, filepath.Join(l.root, "mutex.moved")))
	}

	must(t, os.Symlink(victim, mutex))

	if _, err := st.Commit(Grant{Prompt: true}); asError(t, err).Code != CodeStoreCorrupt {
		t.Errorf("Commit = %v", err)
	}

	if data, _ := os.ReadFile(victim); string(data) != "keep" {
		t.Error("the file behind the link was touched")
	}
}

// TestCommitWithBrokenLockLater checks that a lock that stops parsing between Plan and Commit
// stops the commit before anything is placed.
//
// Parameters:
//   - t: test to report to
func TestCommitWithBrokenLockLater(t *testing.T) {
	l := newLab(t)
	r := l.reg(nil)
	st := plan(t, r, InstallRequest{Source: demoSource(l, "demo", "1.0.0")})

	l.writeFile(l.paths.LockFile(), "[[plugin]\n")

	if _, err := st.Commit(Grant{Prompt: true}); asError(t, err).Code != CodeStoreCorrupt {
		t.Errorf("Commit = %v", err)
	}

	idDir, _ := l.paths.PluginDir("demo")
	if _, err := os.Lstat(idDir); !os.IsNotExist(err) {
		t.Errorf("a tree was placed: %v", err)
	}

	if !strings.Contains(strings.Join(stagingNames(t, l), ""), "") || len(stagingNames(t, l)) != 0 {
		t.Errorf("staging = %v", stagingNames(t, l))
	}
}

// ageStages makes every install in staging look abandoned, by moving its change time back.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox
func ageStages(t *testing.T, l *lab) {
	t.Helper()

	old := time.Now().Add(-2 * staleStage)

	for _, name := range stagingNames(t, l) {
		if strings.HasPrefix(name, stagePrefix) {
			must(t, os.Chtimes(filepath.Join(l.paths.PluginsDir(), ".staging", name), old, old))
		}
	}
}
