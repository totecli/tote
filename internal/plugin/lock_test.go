package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// sampleRecord is a valid record of a release, with every field set.
func sampleRecord() LockRecord {
	return LockRecord{
		ID: "gft", Version: "1.0.0", Tier: TierDeclarative, Kind: KindRelease,
		Source: "https://github.com/totecli/gft/releases/download/1.0.0/plugin-gft.tar.gz",
		Origin: "totecli/gft", ArchiveSHA256: sum256("a"), TreeSHA256: sum256("t"),
		SurfaceSHA256: sum256("s"), InstalledAt: "2026-11-03T10:15:00Z",
		UpdatedAt: "2026-11-03T10:15:00Z", Caps: []string{"env:GITHUB_TOKEN", "exec:gft"},
		ConsentedAt: "2026-11-03T10:15:00Z", ConsentedBy: ConsentPrompt, Failures: 2,
		LastFailureAt: "2026-11-03T11:00:00Z", LastReason: FailCrash,
	}
}

// TestLockRecordValidate checks every rule of Validate with one broken field at a time.
//
// Parameters:
//   - t: test to report to
func TestLockRecordValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(r *LockRecord)
		ok     bool
	}{
		{"valid release", func(*LockRecord) {}, true},
		{"valid bundled", func(r *LockRecord) {
			r.Kind, r.Source, r.Origin, r.ArchiveSHA256 = KindBundled, "", "", ""
		}, true},
		{"valid local", func(r *LockRecord) {
			r.Kind, r.Source, r.Origin, r.ArchiveSHA256 = KindLocal, "/home/u/plugin", "", ""
		}, true},
		{"never consented", func(r *LockRecord) { r.ConsentedAt, r.ConsentedBy = "", "" }, true},
		{"consented by grant", func(r *LockRecord) { r.ConsentedBy = ConsentGrant }, true},
		{"no caps", func(r *LockRecord) { r.Caps = nil }, true},
		{"derived caps", func(r *LockRecord) { r.Caps = []string{"derived:code", "derived:tty"} }, true},
		{"prerelease version", func(r *LockRecord) { r.Version = "1.1.0-beta.1" }, true},
		{"id uppercase", func(r *LockRecord) { r.ID = "Gft" }, false},
		{"id too short", func(r *LockRecord) { r.ID = "g" }, false},
		{"id path", func(r *LockRecord) { r.ID = "../data" }, false},
		{"version with v", func(r *LockRecord) { r.Version = "v1.0.0" }, false},
		{"version partial", func(r *LockRecord) { r.Version = "1.0" }, false},
		{"tier unknown", func(r *LockRecord) { r.Tier = "native" }, false},
		{"kind unknown", func(r *LockRecord) { r.Kind = "registry" }, false},
		{"release without source", func(r *LockRecord) { r.Source = "" }, false},
		{"release without origin", func(r *LockRecord) { r.Origin = "" }, false},
		{"release origin uppercase", func(r *LockRecord) { r.Origin = "TOTECLI/gft" }, false},
		{"release without archive hash", func(r *LockRecord) { r.ArchiveSHA256 = "" }, false},
		{"bundled with source", func(r *LockRecord) { r.Kind = KindBundled }, false},
		{"local with origin", func(r *LockRecord) {
			r.Kind, r.ArchiveSHA256 = KindLocal, ""
		}, false},
		{"source control character", func(r *LockRecord) { r.Source += "\x1b[2J" }, false},
		{"source newline", func(r *LockRecord) { r.Source += "\n" }, false},
		{"source too long", func(r *LockRecord) { r.Source = "https://" + times("a", 2048) }, false},
		{"source invalid UTF-8", func(r *LockRecord) { r.Source += "\xff" }, false},
		{"tree hash short", func(r *LockRecord) { r.TreeSHA256 = "abc" }, false},
		{"tree hash upper case", func(r *LockRecord) {
			r.TreeSHA256 = strings.ToUpper(r.TreeSHA256)
		}, false},
		{"surface hash empty", func(r *LockRecord) { r.SurfaceSHA256 = "" }, false},
		{"installed_at empty", func(r *LockRecord) { r.InstalledAt = "" }, false},
		{"updated_at garbage", func(r *LockRecord) { r.UpdatedAt = "yesterday" }, false},
		{"consented_at garbage", func(r *LockRecord) { r.ConsentedAt = "2026-11-03" }, false},
		{"last_failure_at garbage", func(r *LockRecord) { r.LastFailureAt = "x" }, false},
		{"cap unknown", func(r *LockRecord) { r.Caps = []string{"root"} }, false},
		{"cap repeated", func(r *LockRecord) { r.Caps = []string{"exec:gft", "exec:gft"} }, false},
		{"cap refused permission", func(r *LockRecord) { r.Caps = []string{"hook:intercept"} }, false},
		{"consent by nobody", func(r *LockRecord) { r.ConsentedBy = "" }, false},
		{"consent without date", func(r *LockRecord) { r.ConsentedAt = "" }, false},
		{"consent by stranger", func(r *LockRecord) { r.ConsentedBy = "yes" }, false},
		{"negative failures", func(r *LockRecord) { r.Failures = -1 }, false},
		{"huge failures", func(r *LockRecord) { r.Failures = 1 << 30 }, false},
		{"last reason unknown", func(r *LockRecord) { r.LastReason = "explosion" }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := sampleRecord()
			tt.change(&r)

			err := r.Validate()
			if (err == nil) != tt.ok {
				t.Fatalf("Validate = %v, want ok = %v", err, tt.ok)
			}

			if err != nil && !errors.Is(err, ErrLockRecord) {
				t.Errorf("error %v does not wrap ErrLockRecord", err)
			}
		})
	}
}

// TestLockRoundTrip writes a record and reads it back through a second handle, with every field
// kept, the kill switch included, and the file written in the layout of section 2: every key
// present, zero values too.
//
// Parameters:
//   - t: test to report to
func TestLockRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "plugins.lock")

	lk, err := OpenLock(path)
	if err != nil {
		t.Fatal(err)
	}

	rec := sampleRecord()
	rec.Disabled, rec.Quarantined = true, true

	if err := lk.Put(rec); err != nil {
		t.Fatal(err)
	}

	again, _ := OpenLock(path)

	got, ok := again.Get("gft")
	if !ok || !reflect.DeepEqual(got, rec) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, rec)
	}

	data, _ := os.ReadFile(path)
	text := string(data)

	for _, want := range []string{
		"schema = 1", "[[plugin]]", `id = "gft"`, "disabled = true",
		"quarantined = true", "failures = 2", `last_reason = "crash"`, "caps = [",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the file lacks %q:\n%s", want, text)
		}
	}

	// A record with every zero value still writes its switches, so that a user sees them.
	bare := sampleRecord()
	bare.ID, bare.Caps, bare.ConsentedAt, bare.ConsentedBy = "bare", nil, "", ""
	bare.Failures, bare.LastFailureAt, bare.LastReason = 0, "", ""

	if err := lk.Put(bare); err != nil {
		t.Fatal(err)
	}

	data, _ = os.ReadFile(path)
	for _, want := range []string{
		"disabled = false", "quarantined = false", "failures = 0",
		"caps = []", `consented_at = ""`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the file lacks %q:\n%s", want, data)
		}
	}
}

// TestLockModesUnderUmask checks that the lock is a 0600 file in a 0700 directory whatever the
// umask of the process: a wide open umask must not leak it, a closed one must not lock the user
// out.
//
// Parameters:
//   - t: test to report to
func TestLockModesUnderUmask(t *testing.T) {
	for _, mask := range []int{0, 0o077, 0o022} {
		t.Run("umask "+os.FileMode(mask).String(), func(t *testing.T) {
			old := syscall.Umask(mask)
			defer syscall.Umask(old)

			dir := filepath.Join(t.TempDir(), "state")
			path := filepath.Join(dir, "plugins.lock")

			lk, _ := OpenLock(path)
			if err := lk.Put(sampleRecord()); err != nil {
				t.Fatal(err)
			}

			second := sampleRecord()
			second.ID = "second"

			if err := lk.Put(second); err != nil {
				t.Fatal(err)
			}

			if err := lk.SetDisabled("gft", true); err != nil {
				t.Fatal(err)
			}

			assertMode(t, path, 0o600)
			assertMode(t, dir, 0o700)

			again, _ := OpenLock(path)
			if g, ok := again.Get("gft"); !ok || !g.Disabled {
				t.Errorf("the record did not survive the round trip: %+v", g)
			}

			entries, _ := os.ReadDir(dir)
			if len(entries) != 2 {
				t.Errorf("directory holds %d entries, want the lock and its flock file", len(entries))
			}
		})
	}
}

// assertMode checks the permission bits of a path.
//
// Parameters:
//   - t: test to report to
//   - path: file or directory
//   - want: expected permission bits
func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %v, want %v", filepath.Base(path), got, want)
	}
}

// TestLockKeepsUnknownKeys checks that a key and a comment a newer tote wrote survive an update
// of the record, because the lock is a user-visible file other versions share.
//
// Parameters:
//   - t: test to report to
func TestLockKeepsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugins.lock")
	lk, _ := OpenLock(path)

	if err := lk.Put(sampleRecord()); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	edited := strings.Replace(string(data), "disabled = false",
		"disabled = false # my note\nfuture_key = \"kept\"", 1)

	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := lk.Reload(); err != nil {
		t.Fatal(err)
	}

	if err := lk.SetDisabled("gft", true); err != nil {
		t.Fatal(err)
	}

	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), `future_key = "kept"`) ||
		!strings.Contains(string(data), "disabled = true") {
		t.Errorf("update lost the unknown key or did not set the flag:\n%s", data)
	}
}

// TestLockFailsClosed checks that a lock that does not parse is read-only with a reason, shows no
// record and accepts no write, and that a record that parses but is invalid is listed apart and
// never returned as usable.
//
// Parameters:
//   - t: test to report to
func TestLockFailsClosed(t *testing.T) {
	t.Run("syntax error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "plugins.lock")
		must(t, os.WriteFile(path, []byte("schema = 1\n[[plugin]\nid = \"gft\"\n"), 0o600))

		lk, err := OpenLock(path)
		if err != nil {
			t.Fatal(err)
		}

		if lk.Err() == nil {
			t.Fatal("no error for a lock that does not parse")
		}

		if len(lk.Records()) != 0 || len(lk.IDs()) != 0 {
			t.Error("records of a broken lock")
		}

		if err := lk.Put(sampleRecord()); !errors.Is(err, store.ErrReadOnly) {
			t.Errorf("Put = %v, want ErrReadOnly", err)
		}

		if err := lk.SetDisabled("gft", true); err == nil {
			t.Error("SetDisabled wrote into a broken lock")
		}
	})

	t.Run("invalid record", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "plugins.lock")
		lk, _ := OpenLock(path)

		must(t, lk.Put(sampleRecord()))

		data, _ := os.ReadFile(path)
		must(t, os.WriteFile(path, []byte(strings.Replace(string(data), sum256("t"), "zz", 1)), 0o600))
		must(t, lk.Reload())

		if _, ok := lk.Get("gft"); ok {
			t.Error("an invalid record was returned as usable")
		}

		if lk.Invalid()["gft"] == nil || len(lk.Records()) != 0 {
			t.Errorf("invalid = %v, records = %v", lk.Invalid(), lk.Records())
		}

		if ids := lk.IDs(); len(ids) != 1 || ids[0] != "gft" {
			t.Errorf("IDs = %v, want the invalid record to be listed", ids)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		if _, err := OpenLock(""); err == nil {
			t.Error("OpenLock accepted an empty path")
		}
	})

	t.Run("hostile structure", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "plugins.lock")
		hostile := "[[plugin]]\nid = \"a\"\nx = " + times("[", 100000) + times("]", 100000) + "\n"
		must(t, os.WriteFile(path, []byte(hostile), 0o600))

		lk, _ := OpenLock(path)
		if lk.Err() == nil {
			t.Error("a nesting bomb was accepted")
		}
	})
}

// TestLockUpdate checks Update and Delete: a missing id, a function that makes the record
// invalid, and the removal of one record among several.
//
// Parameters:
//   - t: test to report to
func TestLockUpdate(t *testing.T) {
	lk, _ := OpenLock(filepath.Join(t.TempDir(), "plugins.lock"))

	a, b := sampleRecord(), sampleRecord()
	a.ID, b.ID = "aaa", "bbb"

	must(t, lk.Put(a))
	must(t, lk.Put(b))

	if err := lk.Update("nope", func(*LockRecord) {}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Update of a missing record = %v", err)
	}

	if err := lk.Update("aaa", func(r *LockRecord) { r.TreeSHA256 = "bad" }); err == nil {
		t.Error("Update wrote an invalid record")
	}

	if g, _ := lk.Get("aaa"); g.TreeSHA256 != a.TreeSHA256 {
		t.Error("a refused update changed the record")
	}

	must(t, lk.Update("aaa", func(r *LockRecord) { r.Failures, r.Quarantined = 3, true }))

	if g, _ := lk.Get("aaa"); g.Failures != 3 || !g.Quarantined {
		t.Errorf("record after update: %+v", g)
	}

	must(t, lk.Delete("aaa"))

	if _, ok := lk.Get("aaa"); ok {
		t.Error("deleted record still there")
	}

	if _, ok := lk.Get("bbb"); !ok {
		t.Error("the other record went away with it")
	}

	if err := lk.Delete("aaa"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second Delete = %v", err)
	}
}

// TestLockClone checks that a record returned by the lock shares no slice with the collection.
//
// Parameters:
//   - t: test to report to
func TestLockClone(t *testing.T) {
	lk, _ := OpenLock(filepath.Join(t.TempDir(), "plugins.lock"))
	must(t, lk.Put(sampleRecord()))

	got, _ := lk.Get("gft")
	got.Caps[0] = "changed"

	again, _ := lk.Get("gft")
	if again.Caps[0] == "changed" {
		t.Error("the record shares its caps with the collection")
	}
}

// TestLockConcurrentWriters starts several writers on separate handles, in one process, each
// toggling a different flag of a different record and bumping a shared counter, and checks that
// no update is lost and the file stays valid. The handles stand for several tote processes: the
// flock and the conflict detection of the store are what serialise them.
//
// Parameters:
//   - t: test to report to
func TestLockConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugins.lock")
	setup, _ := OpenLock(path)

	ids := []string{"aaa", "bbb", "ccc", "ddd"}

	for _, id := range ids {
		r := sampleRecord()
		r.ID, r.Failures = id, 0

		must(t, setup.Put(r))
	}

	const rounds = 8

	var wg sync.WaitGroup

	errs := make(chan error, len(ids)*rounds*2)

	for _, id := range ids {
		wg.Add(1)

		go func() {
			defer wg.Done()

			lk, err := OpenLock(path)
			if err != nil {
				errs <- err

				return
			}

			for range rounds {
				errs <- lk.Update(id, func(r *LockRecord) { r.Failures++ })
				errs <- lk.Update("aaa", func(r *LockRecord) { r.Quarantined = !r.Quarantined })
			}
		}()
	}

	wg.Wait()
	close(errs)

	failed := 0

	for err := range errs {
		if err != nil {
			failed++

			t.Logf("write failed: %v", err)
		}
	}

	if failed > 0 {
		t.Fatalf("%d writes failed", failed)
	}

	final, _ := OpenLock(path)
	if final.Err() != nil || len(final.Invalid()) != 0 {
		t.Fatalf("the lock is damaged: %v %v", final.Err(), final.Invalid())
	}

	for _, id := range ids {
		if r, _ := final.Get(id); r.Failures != rounds {
			t.Errorf("%s: failures = %d, want %d: updates were lost", id, r.Failures, rounds)
		}
	}
}
