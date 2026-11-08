package plugin

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// failWriter accepts a number of bytes and then fails.
type failWriter struct {
	left int
}

// Write implements io.Writer.
//
// Parameters:
//   - p: bytes to write
func (f *failWriter) Write(p []byte) (int, error) {
	if len(p) > f.left {
		return 0, errors.New("disk full")
	}

	f.left -= len(p)

	return len(p), nil
}

// closeHook wraps a transport so that a function runs when the body of a 200 answer is closed,
// which is after the download computed its digest and before anything was extracted.
type closeHook struct {
	rt http.RoundTripper
	fn func()
}

// RoundTrip implements http.RoundTripper.
//
// Parameters:
//   - req: request to send
func (c closeHook) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.rt.RoundTrip(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body = hookBody{resp.Body, c.fn}
	}

	return resp, err
}

// hookBody is a body whose Close runs a function first.
type hookBody struct {
	io.ReadCloser
	fn func()
}

// Close implements io.Closer.
func (b hookBody) Close() error {
	b.fn()

	return b.ReadCloser.Close()
}

// TestDownloadSteadySlowBody checks that a body which keeps arriving is never cut by the idle
// time, however long it takes in all, as long as it ends before the total time.
//
// Parameters:
//   - t: test to report to
func TestDownloadSteadySlowBody(t *testing.T) {
	fastTimeouts(t, netTimeouts{
		connect: time.Second, headers: time.Second,
		idle: 300 * time.Millisecond, total: 10 * time.Second,
	})

	h := newHub(t)
	h.handle(hostGH, demoPath, func(w http.ResponseWriter, _ *http.Request) {
		for range 8 {
			_, _ = w.Write([]byte("chunk"))
			w.(http.Flusher).Flush()
			time.Sleep(100 * time.Millisecond)
		}
	})

	sum, body, err := getAsset(h, demoRef, 1<<20)
	if err != nil {
		t.Fatalf("a steady body was cut: %v", err)
	}

	if want := strings.Repeat("chunk", 8); string(body) != want || sum != sumOf([]byte(want)) {
		t.Errorf("body = %q, sum = %s", body, sum)
	}
}

// TestDownloadHeaderTimeIsPerHop checks that the wait for the headers starts again after every
// redirect: three answers that each take most of the time are not cut as one slow answer.
//
// Parameters:
//   - t: test to report to
func TestDownloadHeaderTimeIsPerHop(t *testing.T) {
	fastTimeouts(t, netTimeouts{
		connect: time.Second, headers: 1500 * time.Millisecond,
		idle: 4 * time.Second, total: 20 * time.Second,
	})

	slow := func(to string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(800 * time.Millisecond)
			http.Redirect(w, r, to, http.StatusFound)
		}
	}

	h := newHub(t)
	h.handle(hostGH, demoPath, slow("https://release-assets.githubusercontent.com/a"))
	h.handle(hostAssets, "/a", slow("/b"))
	h.bytesAt(hostAssets, "/b", []byte("end"))

	if _, body, err := getAsset(h, demoRef, 1<<20); err != nil || string(body) != "end" {
		t.Fatalf("%q %v", body, err)
	}
}

// TestDownloadCanceledWhileReading checks that the cancellation of the caller during a body is
// returned as it is and not reported as a network failure.
//
// Parameters:
//   - t: test to report to
func TestDownloadCanceledWhileReading(t *testing.T) {
	h := newHub(t)
	h.handle(hostGH, demoPath, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
		w.(http.Flusher).Flush()

		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(150*time.Millisecond, cancel)

	r := Open(Options{Host: testHost(), Transport: h.transport()})

	_, _, err := r.fetcher().download(ctx, demoRef, 1<<20, &bytes.Buffer{})

	var pe *Error

	if !errors.Is(err, context.Canceled) || errors.As(err, &pe) {
		t.Errorf("err = %v (%T), want the cancellation itself", err, err)
	}
}

// TestDownloadRedirectHosts checks that the list of hosts a redirect may reach is closed: the
// other hosts of GitHub are refused and a listed host is accepted in any case.
//
// Parameters:
//   - t: test to report to
func TestDownloadRedirectHosts(t *testing.T) {
	data := []byte("ok")

	refused := []string{
		"raw.githubusercontent.com", "githubusercontent.com", "api.github.com",
		"codeload.github.com", "gist.github.com", "www.github.com", "avatars.githubusercontent.com",
	}

	for _, host := range refused {
		t.Run(host, func(t *testing.T) {
			h := newHub(t)
			h.redirectAt(hostGH, demoPath, "https://"+host+"/blob")
			h.bytesAt(host, "/blob", data)

			_, _, err := getAsset(h, demoRef, 1<<20)
			if pe := asError(t, err); pe.Code != CodeNetRedirect {
				t.Fatalf("code = %s", pe.Code)
			}

			if h.count(host) != 0 {
				t.Errorf("%s was contacted", host)
			}
		})
	}

	t.Run("upper case listed host", func(t *testing.T) {
		h := newHub(t)
		h.redirectAt(hostGH, demoPath, "https://OBJECTS.GITHUBUSERCONTENT.COM/blob")
		h.bytesAt("OBJECTS.GITHUBUSERCONTENT.COM", "/blob", data)

		if _, body, err := getAsset(h, demoRef, 1<<20); err != nil || !bytes.Equal(body, data) {
			t.Fatalf("%q %v", body, err)
		}
	})
}

// TestDownloadRedirectStatuses checks which statuses are redirects, and the Location values that
// cannot be followed.
//
// Parameters:
//   - t: test to report to
func TestDownloadRedirectStatuses(t *testing.T) {
	data := []byte("ok")

	tests := []struct {
		name string
		code int
		to   string
		want string
	}{
		{"301", 301, "https://release-assets.githubusercontent.com/blob", ""},
		{"302", 302, "https://release-assets.githubusercontent.com/blob", ""},
		{"303", 303, "https://release-assets.githubusercontent.com/blob", ""},
		{"307", 307, "https://release-assets.githubusercontent.com/blob", ""},
		{"308", 308, "https://release-assets.githubusercontent.com/blob", ""},
		{
			"300 is not a redirect", 300, "https://release-assets.githubusercontent.com/blob",
			CodeNetStatus,
		},
		{
			"304 is not a redirect", 304, "https://release-assets.githubusercontent.com/blob",
			CodeNetStatus,
		},
		{"unparsable location", 302, "http://[::1", CodeNetRedirect},
		{"empty port", 302, "https://objects.githubusercontent.com:/blob", CodeNetRedirect},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHub(t)
			h.handle(hostGH, demoPath, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", tt.to)
				w.WriteHeader(tt.code)
			})
			h.bytesAt(hostAssets, "/blob", data)

			_, body, err := getAsset(h, demoRef, 1<<20)

			if tt.want == "" {
				if err != nil || !bytes.Equal(body, data) {
					t.Fatalf("%q %v", body, err)
				}

				return
			}

			if pe := asError(t, err); pe.Code != tt.want {
				t.Fatalf("code = %s, want %s", pe.Code, tt.want)
			}
		})
	}
}

// TestDownloadDiskFailure checks that a destination that fails is a disk error, and that the
// sizes of a refused body are written in the units of the limit.
//
// Parameters:
//   - t: test to report to
func TestDownloadDiskFailure(t *testing.T) {
	h := newHub(t)
	h.bytesAt(hostGH, demoPath, bytes.Repeat([]byte("x"), 100<<10))

	r := Open(Options{Host: testHost(), Transport: h.transport()})

	_, _, err := r.fetcher().download(context.Background(), demoRef, 1<<20, &failWriter{left: 10})
	if pe := asError(t, err); pe.Code != CodeDisk {
		t.Errorf("code = %s", pe.Code)
	}

	_, _, err = r.fetcher().download(context.Background(), demoRef, 1<<10, io.Discard)

	if pe := asError(t, err); pe.Code != CodeTooLarge || pe.Args[0] != "1 KiB" {
		t.Errorf("err = %v", err)
	}

	for n, want := range map[int64]string{
		1: "1 B", 1023: "1023 B", 1 << 10: "1 KiB",
		4096: "4 KiB", 1<<20 - 1: "1023 KiB", 3 << 20: "3 MiB", archiveLimit: "16 MiB",
	} {
		if got := sizeText(n); got != want {
			t.Errorf("sizeText(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestDownloadTransportFailures checks, through the transport that real downloads use, a server
// that never completes the handshake (a timeout) and a server that speaks no TLS 1.2 (a TLS
// failure).
//
// Parameters:
//   - t: test to report to
func TestDownloadTransportFailures(t *testing.T) {
	fastTimeouts(t, netTimeouts{
		connect: 200 * time.Millisecond, headers: 2 * time.Second,
		idle: 2 * time.Second, total: 10 * time.Second,
	})

	dialTo := func(tr *http.Transport, addr string) {
		tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}
	}

	t.Run("handshake that never ends", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}

		done := make(chan struct{})

		t.Cleanup(func() {
			close(done)
			_ = l.Close()
		})

		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}

				go func() {
					<-done
					_ = c.Close()
				}()
			}
		}()

		tr := newTransport()
		dialTo(tr, l.Addr().String())

		r := Open(Options{Host: testHost(), Transport: tr})

		_, _, err = r.fetcher().download(context.Background(), demoRef, 1<<20, io.Discard)
		if pe := asError(t, err); pe.Code != CodeNetTimeout {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("old TLS", func(t *testing.T) {
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter,
			_ *http.Request,
		) {
			_, _ = w.Write([]byte("x"))
		}))
		srv.Config.ErrorLog = log.New(io.Discard, "", 0)
		srv.TLS = &tls.Config{MaxVersion: tls.VersionTLS11}
		srv.StartTLS()
		t.Cleanup(srv.Close)

		tr := newTransport()
		dialTo(tr, srv.Listener.Addr().String())

		r := Open(Options{Host: testHost(), Transport: tr})

		_, _, err := r.fetcher().download(context.Background(), demoRef, 1<<20, io.Discard)
		if pe := asError(t, err); pe.Code != CodeNetTLS {
			t.Errorf("err = %v", err)
		}
	})
}

// TestTransportLimits checks the settings of the real transport that no test reaches through the
// local server: no connection is kept, and the block of response headers is bounded.
//
// Parameters:
//   - t: test to report to
func TestTransportLimits(t *testing.T) {
	tr := newTransport()

	if !tr.DisableKeepAlives {
		t.Error("connections are kept")
	}

	if tr.MaxResponseHeaderBytes != 64<<10 {
		t.Errorf("MaxResponseHeaderBytes = %d", tr.MaxResponseHeaderBytes)
	}

	if tr.DialContext == nil {
		t.Error("no dialer, so no connect timeout")
	}
}

// TestParseReleaseCharset checks that the owner, the repository and the tag of a release URL stay
// inside their character set.
//
// Parameters:
//   - t: test to report to
func TestParseReleaseCharset(t *testing.T) {
	bad := []string{" ", ";", "@", "'", `"`, "\u00e9", "\x00", "$", "&", "=", "!", "*", "(", "~"}

	templates := []string{
		"https://github.com/o%s/r/releases/download/1.0.0/p.tar.gz",
		"https://github.com/o/r%s/releases/download/1.0.0/p.tar.gz",
		"https://github.com/o/r/releases/download/1.0.0%s/p.tar.gz",
	}

	for _, tpl := range templates {
		for _, c := range bad {
			text := strings.Replace(tpl, "%s", c, 1)

			if ref, err := ParseRelease(text); err == nil {
				t.Errorf("%q accepted as %+v", text, ref)
			} else if pe := asError(t, err); pe.Code != CodeBadSource {
				t.Errorf("%q: code = %s", text, pe.Code)
			}
		}
	}

	for _, c := range bad {
		if _, err := ParseShorthand("o" + c + "/r@1.0.0"); err == nil {
			t.Errorf("shorthand with %q accepted", c)
		}
	}
}

// TestInstallReleaseWrongPinIsCheckedFirst serves every hostile archive of the fixtures with a
// wrong pin: the digest is compared before anything is read from the archive, so the answer is a
// hash mismatch that tells the expected and the actual digest, never a complaint about the
// content.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseWrongPinIsCheckedFirst(t *testing.T) {
	wrong := sumOf([]byte("not the archive"))

	for _, tt := range hostileArchives(t) {
		t.Run(tt.name, func(t *testing.T) {
			e := newRelEnv(t)
			url := e.publish("o", "r", "1.0.0", "demo.tar.gz", tt.data)

			pe := e.planCode(InstallRequest{Source: url, SHA256: wrong, ID: "demo"})

			if pe.Code != CodeHashMismatch || len(pe.Args) != 2 || pe.Args[0] != wrong ||
				pe.Args[1] != sumOf(tt.data) {
				t.Fatalf("code = %s %v, want hash_mismatch of %s", pe.Code, pe.Args, wrong)
			}

			if ExitClass(pe.Code) != ExitIntegrity {
				t.Errorf("exit = %d", ExitClass(pe.Code))
			}

			e.assertOnly()
		})
	}
}

// TestInstallReleaseSwappedDownload swaps the downloaded file for another archive after the
// digest was computed and before the extraction reads it: the install is refused, so the pin
// never stands for a tree that does not come from the archive it names.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseSwappedDownload(t *testing.T) {
	good := manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", ""))

	swap := func(e *relEnv, pattern string, with []byte) func() {
		return func() {
			matches, _ := filepath.Glob(filepath.Join(e.l.paths.PluginsDir(), ".staging",
				pattern))
			for _, m := range matches {
				_ = os.WriteFile(m, with, 0o600)
			}
		}
	}

	t.Run("package", func(t *testing.T) {
		e := newRelEnv(t)
		data := archiveOf(t, good)
		evil := archiveOf(t, good, file("README.md", "swapped"))
		url := e.publish("o", "r", "1.0.0", "demo.tar.gz", data)

		e.r = e.l.reg(func(o *Options) {
			o.Transport = closeHook{e.h.transport(), swap(e, "*.dl", evil)}
		})

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(data)})
		if pe.Code != CodeHashMismatch {
			t.Fatalf("code = %s %v", pe.Code, pe.Args)
		}

		e.assertOnly()
	})

	t.Run("platform", func(t *testing.T) {
		prog := ent{name: "bin/lin", typ: '0', body: "#!/bin/sh\necho hi\n", mode: 0o755}
		asset := tgz(t, prog)
		evil := tgz(t, ent{name: "bin/lin", typ: '0', body: "#!/bin/sh\nevil\n", mode: 0o755})

		e := newRelEnv(t)
		main := platformPackage(t, sumOf(asset))
		url := e.publish("o", "r", "1.0.0", "demo.tar.gz", main)
		e.publish("o", "r", "1.0.0", "lin.tgz", asset)

		e.r = e.l.reg(func(o *Options) {
			o.Transport = closeHook{e.h.transport(), swap(e, "*.pf", evil)}
		})

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(main)})
		if pe.Code != CodePlatformHash {
			t.Fatalf("code = %s %v", pe.Code, pe.Args)
		}

		e.assertOnly()
	})
}

// TestInstallReleasePlatformSelection checks that, among the builds of a manifest, the one for
// this operating system and this architecture is fetched and no other, whatever the order.
//
// Parameters:
//   - t: test to report to
func TestInstallReleasePlatformSelection(t *testing.T) {
	asset := tgz(t, ent{name: "bin/lin", typ: '0', body: "#!/bin/sh\necho hi\n", mode: 0o755})
	other := strings.Repeat("c", 64)

	table := func(goos, arch, bin, name, sum string) string {
		return "[[platform]]\nos = \"" + goos + "\"\narch = \"" + arch + "\"\nbin = \"" + bin +
			"\"\nasset = \"" + name + "\"\nsha256 = \"" + sum + "\"\n"
	}

	bld := table("linux", "arm64", "bin/larm", "larm.tgz", other) +
		table("darwin", "amd64", "bin/mac", "mac.tgz", other) +
		table("linux", "amd64", "bin/lin", "lin.tgz", sumOf(asset)) +
		table("darwin", "arm64", "bin/marm", "marm.tgz", other)

	e := newRelEnv(t)
	main := archiveOf(t, build(TierExec, "", nil, "[exec]\n", bld))
	url := e.publish("o", "r", "1.0.0", "demo.tar.gz", main)
	e.publish("o", "r", "1.0.0", "lin.tgz", asset)

	res := e.install(url, main)
	if res.State != StateEnabled {
		t.Fatalf("state = %s %v", res.State, res.Reasons)
	}

	var assets []string

	for _, x := range e.h.seen() {
		if x.host == hostGH {
			assets = append(assets, filepath.Base(x.path))
		}
	}

	if want := []string{"demo.tar.gz", "lin.tgz"}; !slices.Equal(assets, want) {
		t.Errorf("assets requested = %v, want %v", assets, want)
	}

	e.assertOnly("demo")
}

// TestInstallReleasePlatformTooLarge checks that the program of a platform is held to the same
// size limit as the package.
//
// Parameters:
//   - t: test to report to
func TestInstallReleasePlatformTooLarge(t *testing.T) {
	huge := make([]byte, archiveLimit+1)

	e := newRelEnv(t)
	main := platformPackage(t, sumOf(huge))
	url := e.publish("o", "r", "1.0.0", "demo.tar.gz", main)
	e.publish("o", "r", "1.0.0", "lin.tgz", huge)

	pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(main)})
	if pe.Code != CodeTooLarge {
		t.Fatalf("code = %s %v", pe.Code, pe.Args)
	}

	e.assertOnly()
}

// TestPlatformChangesPerBuild checks that a changed digest is reported for the build it belongs
// to, and not for the other builds of the same operating system.
//
// Parameters:
//   - t: test to report to
func TestPlatformChangesPerBuild(t *testing.T) {
	mk := func(sums ...string) *Manifest {
		return &Manifest{Version: "1.0.0", Builds: []Build{
			{OS: "linux", Arch: "amd64", SHA256: sums[0]},
			{OS: "linux", Arch: "arm64", SHA256: sums[1]},
		}}
	}

	got := platformChanges(mk("a", "b"), mk("a", "c"))

	if len(got) != 1 || got[0].ID != "linux/arm64" || got[0].Kind != EscPlatformChanged {
		t.Errorf("changes = %+v, want only linux/arm64", got)
	}

	if got := platformChanges(mk("a", "b"), mk("a", "b")); len(got) != 0 {
		t.Errorf("unchanged builds reported: %+v", got)
	}
}

// TestCopyLocalLimit checks the boundary of the size of an archive on this machine: exactly the
// limit is read, one byte more is refused.
//
// Parameters:
//   - t: test to report to
func TestCopyLocalLimit(t *testing.T) {
	for _, tt := range []struct {
		size int64
		code string
	}{{archiveLimit, ""}, {archiveLimit + 1, CodeTooLarge}} {
		path := filepath.Join(t.TempDir(), "big.tar.gz")

		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}

		must(t, f.Truncate(tt.size))
		must(t, f.Close())

		_, err = copyLocal(path, io.Discard)

		switch {
		case tt.code == "" && err != nil:
			t.Errorf("%d bytes: %v", tt.size, err)
		case tt.code != "" && (err == nil || asError(t, err).Code != tt.code):
			t.Errorf("%d bytes: err = %v, want %s", tt.size, err, tt.code)
		}
	}
}

// TestInstallPinIsRefusedForADirectory checks that --sha256 is a usage error for a directory,
// whatever the value: a checksum that is not checked must not look like one that was.
//
// Parameters:
//   - t: test to report to
func TestInstallPinIsRefusedForADirectory(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")

	for _, sum := range []string{"zz", strings.Repeat("a", 64)} {
		err := planErr(t, l.reg(nil), InstallRequest{Source: src, SHA256: sum})
		if err.Code != CodeHashUnexpected || ExitOf(err) != ExitUsage {
			t.Errorf("sha256 %q: err = %v", sum, err)
		}
	}
}

// TestUnpackExplicitDirectoryTwice checks that a directory declared twice is refused even when
// the first time was only implied by a file below it.
//
// Parameters:
//   - t: test to report to
func TestUnpackExplicitDirectoryTwice(t *testing.T) {
	u := extractBytes(t, tgz(t, file("d/f", "x"), dirEnt("d"), dirEnt("d")))

	wantArchiveError(t, u.err, CodeArchiveUnsafe, UnsafeDuplicate)
}

// TestUnpackOneFileAtTheLimit checks that a single file of exactly 16 MiB is accepted.
//
// Parameters:
//   - t: test to report to
func TestUnpackOneFileAtTheLimit(t *testing.T) {
	if u := extractBytes(t, tgz(t, file("a", strings.Repeat("a", MaxTreeBytes)))); u.err != nil {
		t.Fatal(u.err)
	}
}

// TestUnpackCountsImpliedDirectories checks that the limit of entries bounds the directories that
// the paths of the files imply, and that the error names a directory.
//
// Parameters:
//   - t: test to report to
func TestUnpackCountsImpliedDirectories(t *testing.T) {
	var ents []ent

	for i := range 200 {
		ents = append(ents, file("a"+strconv.Itoa(i)+"/b/c", ""))
	}

	u := extractBytes(t, tgz(t, ents...))

	wantArchiveError(t, u.err, CodeArchiveLimits, LimitEntries)

	// A tree that has as many nodes as the limit allows is accepted.
	ents = ents[:0]

	for i := range 170 {
		ents = append(ents, file("a"+strconv.Itoa(i)+"/b", ""))
	}

	if u := extractBytes(t, tgz(t, ents...)); u.err != nil {
		t.Errorf("340 nodes: %v", u.err)
	}
}

// TestUnpackDiskFailure checks that a staging directory that cannot be written to is a disk
// error and not a fault of the archive.
//
// Parameters:
//   - t: test to report to
func TestUnpackDiskFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to a directory of mode 500")
	}

	stage := filepath.Join(t.TempDir(), "stage")
	must(t, os.Mkdir(stage, 0o500))

	root, err := os.OpenRoot(stage)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = root.Close() }()

	u := newUnpacker(context.Background(), root)

	err = u.run(bytes.NewReader(tgz(t, file("a", "x"))))
	if pe := asError(t, err); pe.Code != CodeDisk {
		t.Errorf("err = %v", err)
	}
}

// TestUpdateRefusesAnInvalidRecord checks that the update of a plugin whose record does not
// validate answers that the store is corrupt, and not that the plugin is not installed.
//
// Parameters:
//   - t: test to report to
func TestUpdateRefusesAnInvalidRecord(t *testing.T) {
	l := newLab(t)
	src := demoSource(l, "demo", "1.0.0")

	installDir(t, l.reg(nil), src)

	data, err := os.ReadFile(l.paths.LockFile())
	if err != nil {
		t.Fatal(err)
	}

	hex := regexp.MustCompile(`"[0-9a-f]{64}"`)
	must(t, os.WriteFile(l.paths.LockFile(), hex.ReplaceAll(data, []byte(`"zz"`)), 0o600))

	if l.lock().Invalid()["demo"] == nil {
		t.Fatal("the record is still valid")
	}

	pe := planErr(t, l.reg(nil), InstallRequest{Source: src, ID: "demo", Update: true})
	if pe.Code != CodeStoreCorrupt {
		t.Errorf("code = %s", pe.Code)
	}
}

// TestUpdateSourceIdentity checks which sources may replace which installed plugins: a bundled
// plugin by the official release of its id and by nothing else, and a local plugin by no release
// at all. The id demo is not an official id, so no repository is official for it, not even
// totecli/demo; TestUpdateBundledToRelease covers the accepted case with the id gft.
//
// Parameters:
//   - t: test to report to
func TestUpdateSourceIdentity(t *testing.T) {
	mk := func(version string) string {
		return manifest("demo", version, []string{"exec:tool"}, cmdWith("go", ""))
	}

	t.Run("bundled", func(t *testing.T) {
		l := newLab(t)
		l.fakeBin("tool")

		h := newHub(t)
		b := bundle(t, l, map[string]string{"plugin.toml": mk("1.0.0")})
		e := &relEnv{t: t, l: l, h: h, r: l.reg(func(o *Options) {
			o.Bundled = b
			o.Transport = h.transport()
		})}

		st := plan(t, e.r, InstallRequest{Source: "demo"})
		if _, err := st.Commit(Grant{Prompt: true}); err != nil {
			t.Fatal(err)
		}

		data := archiveOf(t, mk("1.1.0"))

		for _, owner := range []string{"other", "someone", "totecli"} {
			url := e.publish(owner, "demo", "1.1.0", "demo.tar.gz", data)

			pe := e.planCode(InstallRequest{
				Source: url, SHA256: sumOf(data), ID: "demo",
				Update: true,
			})
			if pe.Code != WarnSourceChanged {
				t.Errorf("release of %s: %s", owner, pe.Code)
			}
		}
	})

	t.Run("local", func(t *testing.T) {
		e := newRelEnv(t)
		installDir(t, e.r, demoSource(e.l, "demo", "1.0.0"))

		data := archiveOf(t, mk("1.1.0"))
		url := e.publish("totecli", "demo", "1.1.0", "demo.tar.gz", data)

		pe := e.planCode(InstallRequest{
			Source: url, SHA256: sumOf(data), ID: "demo",
			Update: true,
		})
		if pe.Code != WarnSourceChanged {
			t.Errorf("code = %s", pe.Code)
		}
	})
}

// TestForcedUpdateAsksForConsent checks that a refusal turned into a warning by force is never
// silent: the consent view is shown, a script must pass the grant, and nothing is replaced
// without it.
//
// Parameters:
//   - t: test to report to
func TestForcedUpdateAsksForConsent(t *testing.T) {
	first := manifest("demo", "1.1.0", []string{"exec:tool"}, cmdWith("go", ""))
	e := updateEnv(t, strings.Replace(first, `1.1.0`, `1.0.0`, 1))

	tests := []struct {
		name  string
		owner string
		data  []byte
		code  string
	}{
		{"other origin", "evil", archiveOf(t, first), WarnSourceChanged},
		{
			"downgrade", "o", archiveOf(t, strings.Replace(first, `1.1.0`, `0.9.0`, 1)),
			WarnDowngrade,
		},
		{
			"pre-release", "o", archiveOf(t, strings.Replace(first, `1.1.0`, `1.2.0-rc.1`, 1)),
			WarnPrerelease,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag := "1.1.0"
			if tt.code == WarnDowngrade {
				tag = "0.9.0"
			}

			if tt.code == WarnPrerelease {
				tag = "1.2.0-rc.1"
			}

			url := e.publish(tt.owner, "r", tag, "demo.tar.gz", tt.data)
			req := InstallRequest{Source: url, SHA256: sumOf(tt.data), ID: "demo", Update: true}

			if pe := e.planCode(req); pe.Code != tt.code {
				t.Fatalf("without force: %s", pe.Code)
			}

			req.Force = true
			st := plan(t, e.r, req)
			d := st.Diff()

			if !d.NeedsConsent || !slices.Contains(d.Why, WhyForced) {
				t.Fatalf("a forced update is silent: %+v", d)
			}

			if !slices.ContainsFunc(d.Warnings, func(r Reason) bool { return r.Code == tt.code }) {
				t.Errorf("warnings = %+v, want %s", d.Warnings, tt.code)
			}

			before, _ := e.l.lock().Get("demo")

			if _, err := st.Commit(Grant{}); asError(t, err).Code != CodeConsentNeeded {
				t.Fatalf("Commit without a grant = %v", err)
			}

			after, _ := e.l.lock().Get("demo")
			if !equalRecords(before, after) {
				t.Errorf("record changed without consent: %+v", after)
			}

			_ = st.Abort()
		})
	}
}

// TestRemoveDanglingLinkWithoutRecord checks that a link to nothing, with no record behind it, is
// an entry to unlink and not a plugin that is not installed.
//
// Parameters:
//   - t: test to report to
func TestRemoveDanglingLinkWithoutRecord(t *testing.T) {
	l := newLab(t)
	r := l.reg(nil)

	must(t, os.MkdirAll(l.paths.PluginsDir(), 0o700))

	link := filepath.Join(l.paths.PluginsDir(), "ghost")
	must(t, os.Symlink(filepath.Join(l.root, "nowhere"), link))

	res, err := r.Remove("ghost", RemoveOptions{})
	if err != nil || !res.Link {
		t.Fatalf("Remove = %+v %v", res, err)
	}

	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("the link is still there: %v", err)
	}
}

// TestRemoveDistrustsALinkedStaging checks that a staging directory which is a link to somewhere
// else is never used: the removal is refused, the plugin stays where it is, and the target of the
// link is not touched.
//
// Parameters:
//   - t: test to report to
func TestRemoveDistrustsALinkedStaging(t *testing.T) {
	l, r := removeLab(t)

	staging := filepath.Join(l.paths.PluginsDir(), ".staging")
	moved := filepath.Join(l.root, "elsewhere", "staging-real")

	must(t, os.MkdirAll(filepath.Dir(moved), 0o700))

	if _, err := os.Lstat(staging); err == nil {
		must(t, os.Rename(staging, moved))
	} else {
		must(t, os.Mkdir(moved, 0o700))
	}

	must(t, os.Symlink(moved, staging))

	before := snapshot(t, moved)

	_, err := r.Remove("demo", RemoveOptions{})
	if err == nil || asError(t, err).Code != CodeStoreCorrupt {
		t.Errorf("Remove = %v, want %s", err, CodeStoreCorrupt)
	}

	if !entryExists(filepath.Join(l.paths.PluginsDir(), "demo")) {
		t.Error("the plugin was removed through a staging directory that cannot be trusted")
	}

	sameSnapshot(t, before, snapshot(t, moved))
}

// TestRemoveReportsALockThatCannotBeWritten checks that a record which cannot be deleted stops the
// removal with an error and leaves the tree where it is, instead of deleting the files of a
// plugin that the lock still lists.
//
// Parameters:
//   - t: test to report to
func TestRemoveReportsALockThatCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a directory of mode 500")
	}

	l, r := removeLab(t)
	dir := filepath.Dir(l.paths.LockFile())

	withHook(t, func(point string) error {
		if point == PointRemoveStop {
			must(t, os.Chmod(dir, 0o500))
		}

		return nil
	})

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, err := r.Remove("demo", RemoveOptions{}); err == nil {
		t.Error("Remove reported success although the record could not be deleted")
	}

	must(t, os.Chmod(dir, 0o700))

	if !entryExists(filepath.Join(l.paths.PluginsDir(), "demo")) {
		t.Error("the tree of a plugin that the lock still lists was deleted")
	}
}
