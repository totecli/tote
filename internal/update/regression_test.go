package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestReviewFixStalledBody checks that a server that sends its headers and then goes silent
// ends the download as offline instead of hanging for ever.
//
// Parameters:
//   - t: test to report to
func TestReviewFixStalledBody(t *testing.T) {
	release := make(chan struct{})

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte("["))
		w.(http.Flusher).Flush()

		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	src := Source{Base: srv.URL, Client: srv.Client(), Idle: 150 * time.Millisecond}
	done := make(chan error, 1)

	go func() {
		_, err := src.fetch(context.Background(), src.listLocation(), 1<<20)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrOffline) {
			t.Errorf("error %v, want ErrOffline", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the download is still blocked on a stalled body")
	}
}

// TestReviewFixSlowBodyKeepsGoing checks that a body that keeps arriving is never cut by the
// idle timer, however long it takes in total.
//
// Parameters:
//   - t: test to report to
func TestReviewFixSlowBodyKeepsGoing(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 6; i++ {
			_, _ = w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
	}))
	defer srv.Close()

	src := Source{Base: srv.URL, Client: srv.Client(), Idle: 250 * time.Millisecond}

	data, err := src.fetch(context.Background(), src.listLocation(), 1<<20)
	if err != nil || string(data) != "xxxxxx" {
		t.Errorf("fetch = %q, %v", data, err)
	}
}

// TestReviewFixCredentialsAreRedacted checks that a user name or password in the address of a
// mirror reaches neither an error message nor the install receipt.
//
// Parameters:
//   - t: test to report to
func TestReviewFixCredentialsAreRedacted(t *testing.T) {
	for _, base := range []string{"http://token@host/x", "ftp://user:secret@host/x"} {
		err := (Source{Base: base}).Validate()
		if !errors.Is(err, ErrInsecure) {
			t.Fatalf("%s: error %v, want ErrInsecure", base, err)
		}

		if strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: the error shows the credentials: %v", base, err)
		}
	}

	if got := redact("https://token@host/x"); strings.Contains(got, "token") {
		t.Errorf("redact kept the user name: %q", got)
	}

	if got := redact("file:///tmp/mirror"); got != "file:///tmp/mirror" {
		t.Errorf("redact changed a plain address: %q", got)
	}
}

// TestReviewFixPurgeSharedDirectory checks that in a directory not named tote, one that the
// user may share with other programs, a directory entry is removed only when it is empty.
//
// Parameters:
//   - t: test to report to
func TestReviewFixPurgeSharedDirectory(t *testing.T) {
	home := t.TempDir()

	tests := []struct {
		dirName string
		kept    bool
	}{
		{"shared", true},
		{ownDirName, false},
	}

	for _, tt := range tests {
		dir := filepath.Join(home, tt.dirName)
		foreign := filepath.Join(dir, "bin", "mytool")

		if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(foreign, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o755); err != nil {
			t.Fatal(err)
		}

		d := DataDir{Dir: dir, Entries: []string{"bin", "plugins"}}

		remove, leave := d.Existing()
		if tt.kept && (len(leave) != 1 || len(remove) != 1) {
			t.Errorf("%s: Existing = %v, %v; want one removed and one left", tt.dirName, remove, leave)
		}

		out, err := PurgeDir(d, filepath.Join(home, "home"))
		if err != nil {
			t.Fatal(err)
		}

		_, statErr := os.Stat(foreign)
		if tt.kept && (statErr != nil || len(out.Left) != 1 || !out.Kept) {
			t.Errorf("%s: foreign file lost or not reported: %v %+v", tt.dirName, statErr, out)
		}

		if !tt.kept && statErr == nil {
			t.Errorf("%s: the bin directory of an own directory is still there", tt.dirName)
		}

		if _, err := os.Stat(filepath.Join(dir, "plugins")); err == nil {
			t.Errorf("%s: an empty plugins directory was not removed", tt.dirName)
		}
	}
}

// TestReviewFixStaleStages checks that only old staging directories are cleaned, that nothing
// but the two known files is deleted in them, and that other names are left alone.
//
// Parameters:
//   - t: test to report to
func TestReviewFixStaleStages(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tote")
	old := time.Now().Add(-3 * time.Hour)

	make1 := func(name string, files ...string) string {
		p := filepath.Join(dir, name)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}

		for _, f := range files {
			if err := os.WriteFile(filepath.Join(p, f), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}

		return p
	}

	stale := make1(stagePrefix+"111", stageArchive, stageBinary)
	foreign := make1(stagePrefix+"222", "notes.txt")
	other := make1("other-dir", stageArchive)

	fresh := filepath.Join(dir, stagePrefix+"333")
	if err := os.Mkdir(fresh, 0o700); err != nil {
		t.Fatal(err)
	}

	cleanStale(target, time.Now())

	if _, err := os.Stat(stale); err == nil {
		t.Error("a stale stage was kept")
	}

	for _, p := range []string{filepath.Join(foreign, "notes.txt"), other, fresh} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}
}

// TestReviewFixNoBlockOnPipes checks that a named pipe in place of a receipt, a cache or a
// startup file is refused at once instead of blocking the open for ever.
//
// Parameters:
//   - t: test to report to
func TestReviewFixNoBlockOnPipes(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")

	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("no named pipe here: %v", err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		if _, err := ReadReceipt(fifo); err == nil {
			t.Error("ReadReceipt accepted a pipe")
		}

		if _, _, ok := readCache(fifo); ok {
			t.Error("readCache accepted a pipe")
		}

		if _, err := ShellLines(fifo); err == nil {
			t.Error("ShellLines accepted a pipe")
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a read blocked on a named pipe")
	}
}

// TestReviewFixReceiptThroughLink checks that a receipt kept elsewhere by a dotfiles manager is
// written through its symbolic link, which stays a link.
//
// Parameters:
//   - t: test to report to
func TestReviewFixReceiptThroughLink(t *testing.T) {
	dir := t.TempDir()
	real1 := filepath.Join(dir, "dotfiles", "install.json")
	link := filepath.Join(dir, "state", "install.json")

	for _, d := range []string{filepath.Dir(real1), filepath.Dir(link)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(real1, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(real1, link); err != nil {
		t.Fatal(err)
	}

	if err := WriteReceipt(link, Receipt{InstallMethod: "script", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the receipt link was replaced: %v %v", info, err)
	}

	if r, err := ReadReceipt(real1); err != nil || r.Version != "1.0.0" {
		t.Errorf("the target holds %+v, %v", r, err)
	}
}

// TestReviewFixReceiptCannotClaimPackagePath checks that a receipt saying script or manual does
// not turn a binary in a package manager location into a self-managed one.
//
// Parameters:
//   - t: test to report to
func TestReviewFixReceiptCannotClaimPackagePath(t *testing.T) {
	tests := []struct {
		exe     string
		method  string
		managed bool
	}{
		{"/usr/bin/tote", "script", true},
		{"/usr/bin/tote", "manual", true},
		{"/opt/homebrew/bin/tote", "script", true},
		{"/usr/local/bin/tote", "script", false},
		{"/home/u/.local/bin/tote", "script", false},
	}

	for _, tt := range tests {
		got := Detect(tt.exe, &Receipt{InstallMethod: tt.method}, "/nonexistent")
		if got.Managed != tt.managed {
			t.Errorf("%s with a %s receipt: managed = %v, want %v", tt.exe, tt.method,
				got.Managed, tt.managed)
		}
	}
}

// TestReviewFixCanceledRefreshWritesNothing checks that closing the interface while the update
// check is running does not record a failed check, so the next start asks again.
//
// Parameters:
//   - t: test to report to
func TestReviewFixCanceledRefreshWritesNothing(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	u := &Updater{Version: "1.0.0", Source: Source{Base: srv.URL, Client: srv.Client()}}
	cache := filepath.Join(t.TempDir(), "cache", CacheFile)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	if _, err := u.RefreshNotice(ctx, cache, time.Now()); !errors.Is(err, context.Canceled) {
		t.Errorf("error %v, want context.Canceled", err)
	}

	if _, err := os.Stat(cache); err == nil {
		t.Error("a canceled check was recorded in the cache")
	}
}

// TestReviewFixNewBinaryKeepsOwnerRules checks that the replacement keeps the permissions of the
// old binary, at least for its owner to run it.
//
// Parameters:
//   - t: test to report to
func TestReviewFixNewBinaryKeepsOwnerRules(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tote")
	staged := filepath.Join(dir, "tote.new")

	for p, mode := range map[string]os.FileMode{target: 0o500, staged: 0o755} {
		if err := os.WriteFile(p, []byte("x"), mode); err != nil {
			t.Fatal(err)
		}
	}

	if err := keepModeAndOwner(staged, target); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(staged)
	if err != nil || info.Mode().Perm() != 0o500 {
		t.Errorf("mode %v, %v; want 0500", info.Mode().Perm(), err)
	}

	// A missing target leaves the staged file as it is.
	if err := keepModeAndOwner(staged, filepath.Join(dir, "absent")); err != nil {
		t.Errorf("missing target: %v", err)
	}
}
