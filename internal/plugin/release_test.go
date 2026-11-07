package plugin

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// relEnv is a sandbox with a registry whose downloads reach a local server.
type relEnv struct {
	t    *testing.T
	l    *lab
	h    *hub
	r    *Registry
	blob int
}

// newRelEnv creates the sandbox, with the program "tool" that the test plugins run in its PATH.
//
// Parameters:
//   - t: test to report to
func newRelEnv(t *testing.T) *relEnv {
	t.Helper()

	l := newLab(t)
	l.fakeBin("tool")

	h := newHub(t)

	return &relEnv{t: t, l: l, h: h, r: l.reg(func(o *Options) { o.Transport = h.transport() })}
}

// publish serves an asset the way GitHub does, through a redirect to the asset host.
//
// Parameters:
//   - owner: owner of the repository
//   - repo: repository
//   - tag: tag of the release
//   - asset: name of the asset
//   - data: contents
//
// Returns the release URL.
func (e *relEnv) publish(owner, repo, tag, asset string, data []byte) string {
	e.t.Helper()

	e.blob++
	blob := fmt.Sprintf("/blob/%d", e.blob)

	e.h.redirectAt(hostGH, relPath(owner, repo, tag, asset),
		"https://release-assets.githubusercontent.com"+blob+"?sig=x")
	e.h.bytesAt(hostAssets, blob, data)

	return "https://github.com" + relPath(owner, repo, tag, asset)
}

// archiveOf builds a release archive from the manifest of a declarative plugin and extra entries.
//
// Parameters:
//   - t: test to report to
//   - manifest: contents of plugin.toml
//   - extra: more entries
func archiveOf(t *testing.T, manifest string, extra ...ent) []byte {
	t.Helper()

	return tgz(t, append([]ent{file("plugin.toml", manifest)}, extra...)...)
}

// demoArchive builds the archive of the plugin "demo" in a version.
//
// Parameters:
//   - t: test to report to
//   - version: plugin version
func demoArchive(t *testing.T, version string) []byte {
	t.Helper()

	return archiveOf(t, manifest("demo", version, []string{"exec:tool"}, cmdWith("go", "")))
}

// install plans and commits a release with the consent of the user.
//
// Parameters:
//   - url: release URL
//   - data: the asset, to compute the pin
func (e *relEnv) install(url string, data []byte) InstallResult {
	e.t.Helper()

	st := plan(e.t, e.r, InstallRequest{Source: url, SHA256: sumOf(data)})

	res, err := st.Commit(Grant{Prompt: true})
	if err != nil {
		e.t.Fatalf("Commit: %v", err)
	}

	return res
}

// idDirs lists the entries of the plugins directory that are plugins.
func (e *relEnv) idDirs() []string {
	e.t.Helper()

	entries, err := os.ReadDir(e.l.paths.PluginsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		e.t.Fatal(err)
	}

	var out []string

	for _, x := range entries {
		if !strings.HasPrefix(x.Name(), ".") {
			out = append(out, x.Name())
		}
	}

	return out
}

// assertOnly checks that the staging directory is empty and that exactly these plugins are
// installed, in the lock and on disk: no partial install.
//
// Parameters:
//   - ids: the installed plugins
func (e *relEnv) assertOnly(ids ...string) {
	e.t.Helper()

	if got := stagingNames(e.t, e.l); len(got) != 0 {
		e.t.Errorf("staging holds %v", got)
	}

	if got := e.idDirs(); !slices.Equal(got, nilIfEmpty(ids)) {
		e.t.Errorf("plugin directories = %v, want %v", got, ids)
	}

	var recs []string

	for _, rec := range e.l.lock().Records() {
		recs = append(recs, rec.ID)
	}

	if !slices.Equal(recs, nilIfEmpty(ids)) {
		e.t.Errorf("records = %v, want %v", recs, ids)
	}
}

// nilIfEmpty turns an empty list into nil, so that lists compare.
//
// Parameters:
//   - s: list
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}

	return s
}

// planCode plans a request and returns the code of the error, failing when it works.
//
// Parameters:
//   - req: request
func (e *relEnv) planCode(req InstallRequest) *Error {
	e.t.Helper()

	return planErr(e.t, e.r, req)
}

// TestInstallRelease installs a plugin from a release URL and from the shorthand, with the pin in
// either case, and checks the record, the diff and the tree.
//
// Parameters:
//   - t: test to report to
func TestInstallRelease(t *testing.T) {
	data := demoArchive(t, "1.0.0")

	tests := []struct {
		name   string
		source func(e *relEnv) string
		sha    func() string
	}{
		{"url", func(e *relEnv) string {
			return e.publish("Own", "Repo", "1.0.0", "demo.tar.gz", data)
		}, func() string { return sumOf(data) }},
		{"url, upper case pin", func(e *relEnv) string {
			return e.publish("o", "r", "1.0.0", "demo.tar.gz", data)
		}, func() string { return strings.ToUpper(sumOf(data)) }},
		{"shorthand", func(e *relEnv) string {
			e.publish("o", "demo", "1.0.0", "demo.tar.gz", data)

			return "o/demo@1.0.0"
		}, func() string { return sumOf(data) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRelEnv(t)
			src := tt.source(e)

			st := plan(t, e.r, InstallRequest{Source: src, SHA256: tt.sha()})
			d := st.Diff()

			if d.Kind != KindRelease || d.ArchiveSHA256 != sumOf(data) || d.Version != "1.0.0" ||
				d.Class != OriginCommunity || d.NeedsConsent != true || d.Update {
				t.Fatalf("diff = %+v", d)
			}

			if got := stagingNames(t, e.l); len(got) != 1 {
				t.Errorf("staging while the user decides = %v", got)
			}

			res, err := st.Commit(Grant{Prompt: true})
			if err != nil {
				t.Fatal(err)
			}

			if res.State != StateEnabled {
				t.Errorf("state = %s %v", res.State, res.Reasons)
			}

			rec, _ := e.l.lock().Get("demo")

			wantOrigin := "o/r"
			switch tt.name {
			case "url":
				wantOrigin = "own/repo"
			case "shorthand":
				wantOrigin = "o/demo"
			}

			if rec.Kind != KindRelease || rec.Origin != wantOrigin || rec.Source != d.Source ||
				!strings.HasPrefix(rec.Source, "https://github.com/") ||
				rec.ArchiveSHA256 != sumOf(data) || rec.ConsentedBy != ConsentPrompt {
				t.Errorf("record = %+v", rec)
			}

			e.assertOnly("demo")
			checkMode(t, e.l.treeDir(rec), 0o500)
			checkMode(t, filepath.Join(e.l.treeDir(rec), "plugin.toml"), 0o400)
		})
	}
}

// TestInstallReleaseNeedsThePin checks that a release is refused without a well formed pin before
// anything is requested, with the usage exit code.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseNeedsThePin(t *testing.T) {
	e := newRelEnv(t)
	data := demoArchive(t, "1.0.0")
	url := e.publish("o", "r", "1.0.0", "demo.tar.gz", data)

	tests := []struct {
		name string
		req  InstallRequest
		code string
	}{
		{"none", InstallRequest{Source: url}, CodeHashRequired},
		{"shorthand, none", InstallRequest{Source: "o/r@1.0.0"}, CodeHashRequired},
		{"short", InstallRequest{Source: url, SHA256: sumOf(data)[:63]}, CodeHashMalformed},
		{
			"not hexadecimal",
			InstallRequest{Source: url, SHA256: strings.Repeat("z", 64)},
			CodeHashMalformed,
		},
	}

	for _, tt := range tests {
		pe := e.planCode(tt.req)

		if pe.Code != tt.code || ExitClass(pe.Code) != ExitUsage {
			t.Errorf("%s: %s exit %d", tt.name, pe.Code, ExitClass(pe.Code))
		}
	}

	if n := len(e.h.seen()); n != 0 {
		t.Errorf("%d requests were made without a valid pin", n)
	}

	e.assertOnly()
}

// TestInstallReleaseRefusals runs the network and integrity failures through Plan and checks
// that each has its code and exit class, makes no partial install and leaves no staging.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseRefusals(t *testing.T) {
	data := demoArchive(t, "1.0.0")
	path := relPath("o", "r", "1.0.0", "demo.tar.gz")
	url := "https://github.com" + path

	other := archiveOf(t, manifest("other", "1.0.0", []string{"exec:tool"}, cmdWith("go", "")))
	html := []byte("<html>not found</html>")

	tests := []struct {
		name  string
		setup func(e *relEnv)
		sha   string
		id    string
		code  string
		exit  int
	}{
		{
			"wrong sha256", func(e *relEnv) { e.publish("o", "r", "1.0.0", "demo.tar.gz", data) },
			sumOf([]byte("other")), "", CodeHashMismatch, ExitIntegrity,
		},
		{"redirect to another host", func(e *relEnv) {
			e.h.redirectAt(hostGH, path, "https://evil.example.org/demo.tar.gz")
		}, sumOf(data), "", CodeNetRedirect, ExitFailure},
		{"redirect to http", func(e *relEnv) {
			e.h.redirectAt(hostGH, path, "http://release-assets.githubusercontent.com/x")
		}, sumOf(data), "", CodeNetRedirect, ExitFailure},
		{"redirect to another repository", func(e *relEnv) {
			e.h.redirectAt(hostGH, path,
				"https://github.com/x/y/releases/download/1.0.0/demo.tar.gz")
		}, sumOf(data), "", CodeOriginRedirect, ExitIntegrity},
		{
			"redirect loop", func(e *relEnv) { e.h.redirectAt(hostGH, path, url) }, sumOf(data), "",
			CodeNetRedirect, ExitFailure,
		},
		{"not found", func(e *relEnv) {}, sumOf(data), "", CodeNetStatus, ExitFailure},
		{"server error", func(e *relEnv) {
			e.h.handle(hostGH, path, func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			})
		}, sumOf(data), "", CodeNetStatus, ExitFailure},
		{"too large", func(e *relEnv) {
			e.publish("o", "r", "1.0.0", "demo.tar.gz", make([]byte, archiveLimit+1))
		}, sumOf(data), "", CodeTooLarge, ExitIntegrity},
		{"id differs from the one asked", func(e *relEnv) {
			e.publish("o", "r", "1.0.0", "demo.tar.gz", other)
		}, sumOf(other), "demo", CodeIDInvalid, ExitFailure},
		{"not a gzip archive", func(e *relEnv) {
			e.publish("o", "r", "1.0.0", "demo.tar.gz", html)
		}, sumOf(html), "", CodeArchiveFormat, ExitIntegrity},
		{"truncated archive", func(e *relEnv) {
			e.publish("o", "r", "1.0.0", "demo.tar.gz", data[:len(data)-12])
		}, sumOf(data[:len(data)-12]), "", CodeArchiveFormat, ExitIntegrity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRelEnv(t)
			tt.setup(e)

			pe := e.planCode(InstallRequest{Source: url, SHA256: tt.sha, ID: tt.id})

			if pe.Code != tt.code {
				t.Fatalf("code = %s %v, want %s", pe.Code, pe.Args, tt.code)
			}

			if got := ExitClass(pe.Code); got != tt.exit {
				t.Errorf("exit = %d, want %d", got, tt.exit)
			}

			e.assertOnly()
		})
	}
}

// TestInstallReleaseSlow checks that a body that stops, and one that never ends, are cut by the
// clocks and leave nothing behind.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseSlow(t *testing.T) {
	path := relPath("o", "r", "1.0.0", "demo.tar.gz")
	url := "https://github.com" + path

	t.Run("stops", func(t *testing.T) {
		fastTimeouts(t, netTimeouts{
			connect: time.Second, headers: time.Second,
			idle: 200 * time.Millisecond, total: 5 * time.Second,
		})

		e := newRelEnv(t)
		e.h.handle(hostGH, path, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("\x1f\x8b"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		})

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf([]byte("x"))})
		if pe.Code != CodeNetTimeout {
			t.Fatalf("code = %s", pe.Code)
		}

		e.assertOnly()
	})

	t.Run("never ends", func(t *testing.T) {
		fastTimeouts(t, netTimeouts{
			connect: time.Second, headers: time.Second,
			idle: time.Second, total: 400 * time.Millisecond,
		})

		e := newRelEnv(t)
		e.h.handle(hostGH, path, func(w http.ResponseWriter, r *http.Request) {
			for r.Context().Err() == nil {
				_, _ = w.Write([]byte("x"))
				w.(http.Flusher).Flush()
				time.Sleep(20 * time.Millisecond)
			}
		})

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf([]byte("x"))})
		if pe.Code != CodeNetTimeout {
			t.Fatalf("code = %s", pe.Code)
		}

		e.assertOnly()
	})

	t.Run("canceled by the user", func(t *testing.T) {
		e := newRelEnv(t)
		e.h.handle(hostGH, path, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		})

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		_, err := e.r.Plan(ctx, InstallRequest{Source: url, SHA256: sumOf([]byte("x"))})
		if err == nil {
			t.Fatal("Plan survived the cancellation")
		}

		if pe, ok := err.(*Error); ok && pe.Code == CodeNetOffline {
			t.Errorf("a cancellation is reported as %s", pe.Code)
		}

		e.assertOnly()
	})
}

// TestInstallReleaseHostileArchives serves every hostile archive of the fixtures with the right
// pin, so that it is the extraction that refuses it, and checks the code, the exit class, and that
// nothing is left in staging or installed.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseHostileArchives(t *testing.T) {
	for _, tt := range hostileArchives(t) {
		t.Run(tt.name, func(t *testing.T) {
			e := newRelEnv(t)
			url := e.publish("o", "r", "1.0.0", "demo.tar.gz", tt.data)

			pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(tt.data), ID: "demo"})

			if pe.Code != tt.code {
				t.Fatalf("code = %s %v, want %s", pe.Code, pe.Args, tt.code)
			}

			if ExitClass(pe.Code) != ExitIntegrity {
				t.Errorf("exit = %d", ExitClass(pe.Code))
			}

			e.assertOnly()
		})
	}
}

// TestInstallReleaseHostilePackages serves archives that extract fine but are not a plugin.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseHostilePackages(t *testing.T) {
	good := manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", ""))

	tests := []struct {
		name string
		data []byte
		code string
	}{
		{"X19 wrapper directory", tgz(t, file("demo/plugin.toml", good)), CodeManifestInvalid},
		{"X19 no manifest", tgz(t, file("README.md", "x")), CodeManifestInvalid},
		{"X20 manifest of one mebibyte", tgz(t, file("plugin.toml", good+"#"+
			strings.Repeat("a", 1<<20)+"\n")), CodeManifestTooLarge},
		{
			"X20 nested tables", tgz(t, file("plugin.toml", strings.Repeat("a = [", 1000))),
			CodeManifestInvalid,
		},
		{
			"X21 extra file in a declarative package", archiveOf(t, good, file("run.sh",
				"#!/bin/sh")),
			CodeDeclarativeExtra,
		},
		{"X21 extra directory", archiveOf(t, good, file("bin/demo", "x")), CodeDeclarativeExtra},
		{"manifest syntax", tgz(t, file("plugin.toml", "id = ")), CodeManifestInvalid},
		{
			"id of a core verb", archiveOf(t, strings.Replace(good, `id = "demo"`, `id = "conn"`,
				1)),
			CodeIDInvalid,
		},
		{"official id from a community repository", archiveOf(t, strings.Replace(good,
			`id = "demo"`, `id = "gft"`, 1)), CodeIDInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRelEnv(t)
			url := e.publish("o", "r", "1.0.0", "demo.tar.gz", tt.data)

			pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(tt.data)})

			if pe.Code != tt.code {
				t.Fatalf("code = %s %v, want %s", pe.Code, pe.Args, tt.code)
			}

			e.assertOnly()
		})
	}
}

// TestInstallReleaseOfficial checks that an official id is for the release of totecli/<id> alone,
// and that this release is an official one.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseOfficial(t *testing.T) {
	data := archiveOf(t, strings.Replace(manifest("demo", "1.0.0", []string{"exec:tool"},
		cmdWith("go", "")), `id = "demo"`, `id = "gft"`, 1))

	e := newRelEnv(t)
	url := e.publish("totecli", "gft", "1.0.0", "plugin-gft.tar.gz", data)

	st := plan(t, e.r, InstallRequest{Source: url, SHA256: sumOf(data)})
	if d := st.Diff(); d.Class != OriginOfficial || d.Origin != "totecli/gft" {
		t.Errorf("diff = %+v", d)
	}

	if _, err := st.Commit(Grant{Prompt: true}); err != nil {
		t.Fatal(err)
	}

	p, _ := e.r.Recheck("gft")
	if p.State != StateEnabled {
		t.Errorf("state = %s %v", p.State, p.Reasons)
	}

	other := newRelEnv(t)
	url = other.publish("someone", "gft", "1.0.0", "plugin-gft.tar.gz", data)

	if pe := other.planCode(InstallRequest{Source: url, SHA256: sumOf(data)}); pe.Code !=
		CodeIDInvalid {
		t.Errorf("official id from someone else: %s", pe.Code)
	}
}

// TestInstallReleaseCleansStaleDownloads checks that the file of a download that a crash left in
// staging is swept by the next change, like a stale staging directory.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseCleansStaleDownloads(t *testing.T) {
	e := newRelEnv(t)
	data := demoArchive(t, "1.0.0")
	url := e.publish("o", "r", "1.0.0", "demo.tar.gz", data)

	staging := filepath.Join(e.l.paths.PluginsDir(), ".staging")
	must(t, os.MkdirAll(staging, 0o700))

	dead := filepath.Join(staging, "stage-deadbeefdeadbeef.dl")
	must(t, os.WriteFile(dead, []byte("partial"), 0o600))

	old := time.Now().Add(-3 * time.Hour)
	must(t, os.Chtimes(dead, old, old))

	e.install(url, data)

	if _, err := os.Lstat(dead); err == nil {
		t.Error("the stale download is still there")
	}

	e.assertOnly("demo")
}

// TestInstallReleaseTLSAndOffline checks the failures of the connection itself.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseTLSAndOffline(t *testing.T) {
	data := demoArchive(t, "1.0.0")
	url := "https://github.com" + relPath("o", "r", "1.0.0", "demo.tar.gz")

	for name, tr := range map[string]func(h *hub) http.RoundTripper{
		"untrusted": func(h *hub) http.RoundTripper { return h.transportFor("example.com", false) },
		"wrong name": func(h *hub) http.RoundTripper {
			return h.transportFor("wrong.example.net", true)
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newRelEnv(t)
			e.r = e.l.reg(func(o *Options) { o.Transport = tr(e.h) })
			e.h.bytesAt(hostGH, relPath("o", "r", "1.0.0", "demo.tar.gz"), data)

			pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(data)})
			if pe.Code != CodeNetTLS {
				t.Fatalf("code = %s", pe.Code)
			}

			e.assertOnly()
		})
	}
}

// TestInstallLocalArchive installs an archive file: the pin is optional but checked when given,
// the record is a local one, and update re-reads the path.
//
// Parameters:
//   - t: test to report to
func TestInstallLocalArchive(t *testing.T) {
	e := newRelEnv(t)
	data := demoArchive(t, "1.0.0")
	path := filepath.Join(e.l.root, "demo.tar.gz")
	must(t, os.WriteFile(path, data, 0o600))

	pe := e.planCode(InstallRequest{Source: path, SHA256: sumOf([]byte("other"))})
	if pe.Code != CodeHashMismatch {
		t.Fatalf("wrong pin: %s", pe.Code)
	}

	pe = e.planCode(InstallRequest{Source: path, SHA256: "xyz"})
	if pe.Code != CodeHashMalformed {
		t.Fatalf("malformed pin: %s", pe.Code)
	}

	e.assertOnly()

	st := plan(t, e.r, InstallRequest{Source: path})
	d := st.Diff()

	if d.Kind != KindLocal || d.Class != OriginLocal || d.ArchiveSHA256 != sumOf(data) ||
		d.Source != path {
		t.Fatalf("diff = %+v", d)
	}

	if _, err := st.Commit(Grant{Prompt: true}); err != nil {
		t.Fatal(err)
	}

	rec, _ := e.l.lock().Get("demo")
	if rec.Kind != KindLocal || rec.Source != path || rec.ArchiveSHA256 != "" || rec.Origin != "" {
		t.Errorf("record = %+v", rec)
	}

	// A new version at the same path, read again by update with no source.
	must(t, os.WriteFile(path, demoArchive(t, "1.1.0"), 0o600))

	st = plan(t, e.r, InstallRequest{ID: "demo", Update: true})
	if d := st.Diff(); !d.Update || d.OldVersion != "1.0.0" || d.Version != "1.1.0" {
		t.Fatalf("update diff = %+v", d)
	}

	if _, err := st.Commit(Grant{}); err != nil {
		t.Fatal(err)
	}

	if rec, _ := e.l.lock().Get("demo"); rec.Version != "1.1.0" {
		t.Errorf("version = %s", rec.Version)
	}

	e.assertOnly("demo")

	for _, bad := range []string{"not gzip", ""} {
		must(t, os.WriteFile(path, []byte(bad), 0o600))

		pe := e.planCode(InstallRequest{ID: "demo", Update: true})
		if pe.Code != CodeArchiveFormat {
			t.Errorf("%q: %s", bad, pe.Code)
		}
	}

	must(t, os.WriteFile(path, bytes.Repeat([]byte{0}, archiveLimit+1), 0o600))

	if pe := e.planCode(InstallRequest{ID: "demo", Update: true}); pe.Code != CodeTooLarge {
		t.Errorf("large archive: %s", pe.Code)
	}

	e.assertOnly("demo")
}

// platformPackage builds the main archive and the platform archive of an exec plugin with a
// build for linux/amd64 and one for another machine.
//
// Parameters:
//   - t: test to report to
//   - sum: sha256 the manifest pins for the platform archive
//   - main: more entries of the main archive
func platformPackage(t *testing.T, sum string, main ...ent) []byte {
	t.Helper()

	bld := "[[platform]]\nos = \"linux\"\narch = \"amd64\"\nbin = \"bin/lin\"\n" +
		"asset = \"lin.tgz\"\nsha256 = \"" + sum + "\"\n" +
		"[[platform]]\nos = \"darwin\"\narch = \"arm64\"\n" +
		"bin = \"bin/mac\"\nasset = \"mac.tgz\"\nsha256 = \"" + strings.Repeat("b", 64) + "\"\n"

	return archiveOf(t, build(TierExec, "", nil, "[exec]\n", bld), main...)
}

// TestInstallReleasePlatform installs an exec plugin whose program is a separate asset, which is
// checked against the sha256 of its [[platform]] table and extracted into the same tree.
//
// Parameters:
//   - t: test to report to
func TestInstallReleasePlatform(t *testing.T) {
	prog := ent{name: "bin/lin", typ: '0', body: "#!/bin/sh\necho hi\n", mode: 0o4777}
	asset := tgz(t, prog)
	good := sumOf(asset)

	t.Run("installs", func(t *testing.T) {
		e := newRelEnv(t)
		main := platformPackage(t, good)
		url := e.publish("o", "r", "1.0.0", "demo.tar.gz", main)
		e.publish("o", "r", "1.0.0", "lin.tgz", asset)

		res := e.install(url, main)
		if res.State != StateEnabled {
			t.Fatalf("state = %s %v", res.State, res.Reasons)
		}

		rec, _ := e.l.lock().Get("demo")
		checkMode(t, filepath.Join(e.l.treeDir(rec), "bin", "lin"), 0o500)

		if got := e.h.count(hostGH); got != 2 {
			t.Errorf("%d requests to github.com, want one per asset", got)
		}

		e.assertOnly("demo")
	})

	t.Run("platform asset differs from its pin", func(t *testing.T) {
		e := newRelEnv(t)
		main := platformPackage(t, good)
		url := e.publish("o", "r", "1.0.0", "demo.tar.gz", main)
		e.publish("o", "r", "1.0.0", "lin.tgz", tgz(t, ent{
			name: "bin/lin", typ: '0',
			body: "#!/bin/sh\nevil\n", mode: 0o755,
		}))

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(main)})
		if pe.Code != CodePlatformHash || ExitClass(pe.Code) != ExitIntegrity {
			t.Fatalf("code = %s", pe.Code)
		}

		e.assertOnly()
	})

	t.Run("platform asset missing", func(t *testing.T) {
		e := newRelEnv(t)
		main := platformPackage(t, good)
		url := e.publish("o", "r", "1.0.0", "demo.tar.gz", main)

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(main)})
		if pe.Code != CodeNetStatus {
			t.Fatalf("code = %s", pe.Code)
		}

		e.assertOnly()
	})

	t.Run("path in both archives", func(t *testing.T) {
		e := newRelEnv(t)
		main := platformPackage(t, good, ent{name: "bin/lin", typ: '0', body: "x", mode: 0o644})
		url := e.publish("o", "r", "1.0.0", "demo.tar.gz", main)
		e.publish("o", "r", "1.0.0", "lin.tgz", asset)

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(main)})
		if pe.Code != CodeArchiveUnsafe || pe.Args[1] != UnsafeDuplicate {
			t.Fatalf("code = %s %v", pe.Code, pe.Args)
		}

		e.assertOnly()
	})

	t.Run("hostile platform archive", func(t *testing.T) {
		e := newRelEnv(t)
		evil := tgz(t, ent{name: "../escape", typ: '0', body: "x", mode: 0o755})
		main := platformPackage(t, sumOf(evil))
		url := e.publish("o", "r", "1.0.0", "demo.tar.gz", main)
		e.publish("o", "r", "1.0.0", "lin.tgz", evil)

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(main)})
		if pe.Code != CodeArchiveUnsafe || pe.Args[1] != UnsafeParent {
			t.Fatalf("code = %s %v", pe.Code, pe.Args)
		}

		e.assertOnly()
	})

	t.Run("no build for this machine", func(t *testing.T) {
		e := newRelEnv(t)
		bld := "[[platform]]\nos = \"darwin\"\narch = \"arm64\"\nbin = \"bin/mac\"\n" +
			"asset = \"mac.tgz\"\nsha256 = \"" + strings.Repeat("b", 64) + "\"\n"
		main := archiveOf(t, build(TierExec, "", nil, "[exec]\n", bld))
		url := e.publish("o", "r", "1.0.0", "demo.tar.gz", main)

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(main)})
		if pe.Code != CodePlatformNone {
			t.Fatalf("code = %s", pe.Code)
		}

		if got := e.h.count(hostGH); got != 1 {
			t.Errorf("%d requests, a build for another machine was fetched", got)
		}

		e.assertOnly()
	})
}

// updateEnv installs version 1.0.0 of demo from o/r and returns the sandbox.
//
// Parameters:
//   - t: test to report to
//   - first: manifest of the first version
func updateEnv(t *testing.T, first string) *relEnv {
	t.Helper()

	e := newRelEnv(t)
	data := archiveOf(t, first)
	e.install(e.publish("o", "r", "1.0.0", "demo.tar.gz", data), data)

	return e
}

// TestUpdateRelease updates a release plugin and checks the dry run, the rules, what survives and
// that the old tree goes.
//
// Parameters:
//   - t: test to report to
func TestUpdateRelease(t *testing.T) {
	e := updateEnv(t, manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", "")))

	state, err := e.l.paths.PluginStateDir("demo")
	if err != nil {
		t.Fatal(err)
	}

	must(t, os.WriteFile(filepath.Join(state, "keep"), []byte("data"), 0o600))

	before, _ := e.l.lock().Get("demo")
	trees := treeNames(t, e, "demo")

	v2 := demoArchive(t, "1.1.0")
	url2 := e.publish("o", "r", "1.1.0", "demo.tar.gz", v2)
	req := InstallRequest{Source: url2, SHA256: sumOf(v2), ID: "demo", Update: true}

	// A dry run: the diff is computed, and nothing else happens.
	st := plan(t, e.r, req)
	d := st.Diff()

	if !d.Update || d.OldVersion != "1.0.0" || d.Version != "1.1.0" || d.NeedsConsent ||
		d.ArchiveSHA256 != sumOf(v2) || d.Origin != "o/r" {
		t.Fatalf("diff = %+v", d)
	}

	if err := st.Abort(); err != nil {
		t.Fatal(err)
	}

	after, _ := e.l.lock().Get("demo")
	if !equalRecords(before, after) || !slices.Equal(trees, treeNames(t, e, "demo")) {
		t.Error("a dry run changed the installed plugin")
	}

	e.assertOnly("demo")

	// The real update.
	st = plan(t, e.r, req)

	res, err := st.Commit(Grant{})
	if err != nil {
		t.Fatal(err)
	}

	if !res.Updated || res.OldVersion != "1.0.0" || res.Version != "1.1.0" ||
		res.State != StateEnabled || res.ConsentedBy != "" {
		t.Errorf("result = %+v", res)
	}

	now, _ := e.l.lock().Get("demo")
	if now.Version != "1.1.0" || now.ArchiveSHA256 != sumOf(v2) || now.Source != url2 ||
		now.InstalledAt != before.InstalledAt || now.ConsentedAt != before.ConsentedAt ||
		now.Origin != "o/r" {
		t.Errorf("record = %+v", now)
	}

	if got := treeNames(t, e, "demo"); len(got) != 1 || got[0] == trees[0] {
		t.Errorf("trees after = %v, before %v", got, trees)
	}

	body, err := os.ReadFile(filepath.Join(state, "keep"))
	if err != nil || string(body) != "data" {
		t.Errorf("state did not survive: %q %v", body, err)
	}

	e.assertOnly("demo")
}

// treeNames lists the tree directories of a plugin.
//
// Parameters:
//   - t: test to report to
//   - e: sandbox
//   - id: plugin id
func treeNames(t *testing.T, e *relEnv, id string) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(e.l.paths.PluginsDir(), id))
	if err != nil {
		t.Fatal(err)
	}

	var out []string
	for _, x := range entries {
		out = append(out, x.Name())
	}

	return out
}

// equalRecords reports whether two lock records are the same.
//
// Parameters:
//   - a: first record
//   - b: second record
func equalRecords(a, b LockRecord) bool {
	return fmt.Sprintf("%+v", a) == fmt.Sprintf("%+v", b)
}

// TestUpdateReleaseRules checks the rules of an update from a release: the same origin, a newer
// version that is not a pre-release, and the pin of the new asset.
//
// Parameters:
//   - t: test to report to
func TestUpdateReleaseRules(t *testing.T) {
	tests := []struct {
		name   string
		owner  string
		repo   string
		tag    string
		ver    string
		code   string
		force  bool
		warned string
	}{
		{"newer", "o", "r", "1.1.0", "1.1.0", "", false, ""},
		{"same version", "o", "r", "1.0.0", "1.0.0", CodeDowngrade, false, ""},
		{"older", "o", "r", "0.9.0", "0.9.0", CodeDowngrade, false, ""},
		{"pre-release", "o", "r", "2.0.0-rc.1", "2.0.0-rc.1", CodePrerelease, false, ""},
		{"other repository", "o", "other", "1.1.0", "1.1.0", CodeSourceChanged, false, ""},
		{"other owner", "x", "r", "1.1.0", "1.1.0", CodeSourceChanged, false, ""},
		{"same repository, other case", "O", "R", "1.1.0", "1.1.0", "", false, ""},
		{"other repository, forced", "o", "other", "1.1.0", "1.1.0", "", true, WarnSourceChanged},
		{"older, forced", "o", "r", "0.9.0", "0.9.0", "", true, WarnDowngrade},
		{"pre-release, forced", "o", "r", "2.0.0-rc.1", "2.0.0-rc.1", "", true, WarnPrerelease},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := updateEnv(t, manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", "")))
			before, _ := e.l.lock().Get("demo")

			data := demoArchive(t, tt.ver)
			url := e.publish(tt.owner, tt.repo, tt.tag, "demo.tar.gz", data)
			req := InstallRequest{
				Source: url, SHA256: sumOf(data), ID: "demo", Update: true,
				Force: tt.force,
			}

			if tt.code != "" {
				if pe := e.planCode(req); pe.Code != tt.code {
					t.Fatalf("code = %s, want %s", pe.Code, tt.code)
				}

				if now, _ := e.l.lock().Get("demo"); !equalRecords(before, now) {
					t.Error("a refused update changed the record")
				}

				e.assertOnly("demo")

				return
			}

			st := plan(t, e.r, req)

			if tt.warned != "" {
				found := false
				for _, w := range st.Diff().Warnings {
					found = found || w.Code == tt.warned
				}

				if !found {
					t.Errorf("warnings = %v, want %s", st.Diff().Warnings, tt.warned)
				}
			}

			if _, err := st.Commit(Grant{Prompt: true}); err != nil {
				t.Fatal(err)
			}

			if now, _ := e.l.lock().Get("demo"); now.Version != tt.ver {
				t.Errorf("version = %s", now.Version)
			}
		})
	}
}

// TestUpdateReleaseNeedsASource checks the errors of an update that cannot find what to fetch.
//
// Parameters:
//   - t: test to report to
func TestUpdateReleaseNeedsASource(t *testing.T) {
	e := updateEnv(t, manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", "")))

	rec, _ := e.l.lock().Get("demo")
	asked := len(e.h.seen())

	pe := e.planCode(InstallRequest{ID: "demo", Update: true})
	if pe.Code != CodeSourceRequired || ExitClass(pe.Code) != ExitUsage || len(pe.Args) < 2 ||
		pe.Args[1] != rec.Source {
		t.Errorf("no source: %s %v", pe.Code, pe.Args)
	}

	pe = e.planCode(InstallRequest{ID: "ghost", Update: true})
	if pe.Code != CodeNotInstalled {
		t.Errorf("unknown id: %s", pe.Code)
	}

	data := demoArchive(t, "1.1.0")
	url := e.publish("o", "r", "1.1.0", "demo.tar.gz", data)

	pe = e.planCode(InstallRequest{Source: url, ID: "demo", Update: true})
	if pe.Code != CodeHashRequired {
		t.Errorf("update without a pin: %s", pe.Code)
	}

	pe = e.planCode(InstallRequest{Source: url, SHA256: sumOf(data), ID: "ghost", Update: true})
	if pe.Code != CodeNotInstalled {
		t.Errorf("update of an id that is not installed: %s", pe.Code)
	}

	pe = e.planCode(InstallRequest{Source: url, SHA256: sumOf(data), ID: "other", Update: true})
	if pe.Code != CodeNotInstalled {
		t.Errorf("id of another plugin: %s", pe.Code)
	}

	if n := len(e.h.seen()); n != asked {
		t.Errorf("%d requests for updates that cannot work", n-asked)
	}

	e.assertOnly("demo")
}

// TestUpdateReleaseFailureKeepsTheOldVersion checks P8 for a fetched update: whatever goes wrong
// with the new version, the old one is still installed, consented and enabled.
//
// Parameters:
//   - t: test to report to
func TestUpdateReleaseFailureKeepsTheOldVersion(t *testing.T) {
	e := updateEnv(t, manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", "")))
	before, _ := e.l.lock().Get("demo")

	v2 := demoArchive(t, "1.1.0")
	url := e.publish("o", "r", "1.1.0", "demo.tar.gz", v2)

	bad := []InstallRequest{
		{Source: url, SHA256: sumOf([]byte("other")), ID: "demo", Update: true},
		{Source: url + "x", SHA256: sumOf(v2), ID: "demo", Update: true},
	}

	for _, req := range bad {
		_ = e.planCode(req)
	}

	// An update that fails to commit: the copy is spent and the record is the old one.
	st := plan(t, e.r, InstallRequest{Source: url, SHA256: sumOf(v2), ID: "demo", Update: true})

	old := installHook
	installHook = func(point string) error {
		if point == PointC2Before {
			return fmt.Errorf("disk full")
		}

		return nil
	}

	_, err := st.Commit(Grant{})

	installHook = old

	if err == nil {
		t.Fatal("the commit worked")
	}

	now, _ := e.l.lock().Get("demo")
	if !equalRecords(before, now) {
		t.Errorf("record changed: %+v", now)
	}

	p, ok := e.r.Recheck("demo")
	if !ok || p.State != StateEnabled || p.Record.Version != "1.0.0" {
		t.Errorf("state = %s %v", p.State, p.Reasons)
	}

	e.assertOnly("demo")
}

// TestUpdateReleaseEscalation checks decision (f) and the capability rule through a fetched
// update: a removed confirm, a new allow_dash, a new env name and a new capability ask for a
// new consent, declining leaves the old version running and nothing else asks.
//
// Parameters:
//   - t: test to report to
func TestUpdateReleaseEscalation(t *testing.T) {
	plain := surfaceBase("", "", "")
	confirmed := surfaceBase("confirm = true\n", "", "")
	dash := surfaceBase("", "allow_dash = [\"x\"]\n", "")
	env := surfaceBase("", "", "[command.env]\nA = \"1\"\n")
	changed := strings.Replace(plain, `run = ["tool", "{x}"]`,
		`run = ["tool", "--other", "{x}"]`, 1)
	moreCaps := strings.Replace(plain, `"env:B"]`, `"env:B", "net:example.com"]`, 1)

	tests := []struct {
		name string
		old  string
		next string
		asks bool
		why  string
	}{
		{"confirm removed", confirmed, plain, true, WhySurface},
		{"allow_dash added", plain, dash, true, WhySurface},
		{"env name added", plain, env, true, WhySurface},
		{"capability added", plain, moreCaps, true, WhyCaps},
		{"command changed", plain, changed, false, ""},
		{"nothing changed", plain, plain, false, ""},
		{"confirm added", plain, confirmed, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := updateEnv(t, tt.old)
			before, _ := e.l.lock().Get("demo")

			data := archiveOf(t, withVersion(tt.next, "1.1.0"))
			url := e.publish("o", "r", "1.1.0", "demo.tar.gz", data)

			st := plan(t, e.r, InstallRequest{
				Source: url, SHA256: sumOf(data), ID: "demo",
				Update: true,
			})
			d := st.Diff()

			if d.NeedsConsent != tt.asks || (tt.asks && !contains(d.Why, tt.why)) {
				t.Fatalf("NeedsConsent = %v why %v, want %v %s", d.NeedsConsent, d.Why, tt.asks,
					tt.why)
			}

			if tt.asks {
				if _, err := st.Commit(Grant{}); asError(t, err).Code != CodeConsentNeeded {
					t.Fatalf("Commit without consent: %v", err)
				}

				if _, err := st.Commit(Grant{Token: "other@000000000000"}); asError(t, err).Code !=
					CodeGrantMismatch {
					t.Fatalf("Commit with another token: %v", err)
				}

				// Declining: the user cancels, and the old version runs.
				if err := st.Abort(); err != nil {
					t.Fatal(err)
				}

				now, _ := e.l.lock().Get("demo")
				if !equalRecords(before, now) {
					t.Error("declining changed the record")
				}

				if p, _ := e.r.Recheck("demo"); p.State != StateEnabled {
					t.Errorf("state after declining = %s", p.State)
				}

				e.assertOnly("demo")

				st = plan(t, e.r, InstallRequest{
					Source: url, SHA256: sumOf(data), ID: "demo",
					Update: true,
				})
			}

			res, err := st.Commit(Grant{Token: st.Diff().Grant})
			if err != nil {
				t.Fatal(err)
			}

			if res.State != StateEnabled || (tt.asks && res.ConsentedBy != ConsentGrant) ||
				(!tt.asks && res.ConsentedBy != "") {
				t.Errorf("result = %+v", res)
			}

			e.assertOnly("demo")
		})
	}
}

// TestRemoveRelease removes plugins installed from a release: the entry, the trees of an updated
// plugin and the record go, the state stays unless purged, a second remove finds nothing, and
// nothing is left in staging.
//
// Parameters:
//   - t: test to report to
func TestRemoveRelease(t *testing.T) {
	e := updateEnv(t, manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", "")))

	v2 := demoArchive(t, "1.1.0")
	url := e.publish("o", "r", "1.1.0", "demo.tar.gz", v2)

	st := plan(t, e.r, InstallRequest{Source: url, SHA256: sumOf(v2), ID: "demo", Update: true})
	if _, err := st.Commit(Grant{}); err != nil {
		t.Fatal(err)
	}

	res, err := e.r.Remove("demo", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Leftovers) != 0 || res.Link {
		t.Errorf("result = %+v", res)
	}

	e.assertOnly()

	if len(res.Kept) == 0 {
		t.Errorf("a remove without purge kept nothing: %+v", res)
	}

	if pe := asError(t, removeErr(e.r, "demo")); pe.Code != CodeNotInstalled {
		t.Errorf("second remove = %s", pe.Code)
	}

	// The record is gone but the directory stayed (a crash, or a failed deletion): remove
	// finishes the job once, then reports the plugin as not installed.
	e.install(e.publish("o", "r", "1.0.0", "demo.tar.gz", demoArchive(t, "1.0.0")),
		demoArchive(t, "1.0.0"))

	must(t, e.l.lock().Delete("demo"))

	if _, err := e.r.Remove("demo", RemoveOptions{Purge: true}); err != nil {
		t.Fatalf("remove after the record was deleted: %v", err)
	}

	e.assertOnly()

	if pe := asError(t, removeErr(e.r, "demo")); pe.Code != CodeNotInstalled {
		t.Errorf("remove again = %s", pe.Code)
	}
}

// removeErr runs Remove and returns its error.
//
// Parameters:
//   - r: registry
//   - id: plugin id
func removeErr(r *Registry, id string) error {
	_, err := r.Remove(id, RemoveOptions{})

	return err
}

// TestRemoveReleaseNeverFollowsLinks plants links in the places a hostile layout would use and
// checks that remove deletes the links and never what they point to.
//
// Parameters:
//   - t: test to report to
func TestRemoveReleaseNeverFollowsLinks(t *testing.T) {
	e := updateEnv(t, manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", "")))

	outside := filepath.Join(e.l.root, "outside")
	must(t, os.MkdirAll(outside, 0o700))
	must(t, os.WriteFile(filepath.Join(outside, "precious"), []byte("x"), 0o600))

	// A link in the tree directory of the plugin, and the staging directory holding a link.
	rec, _ := e.l.lock().Get("demo")
	idDir := filepath.Join(e.l.paths.PluginsDir(), "demo")
	tree := e.l.treeDir(rec)

	must(t, os.Chmod(tree, 0o700))
	must(t, os.Symlink(outside, filepath.Join(tree, "link")))
	must(t, os.Chmod(tree, 0o500))
	must(t, os.Symlink(outside, filepath.Join(idDir, "ffffffffffffffffffffffffffffffff")))

	if _, err := e.r.Remove("demo", RemoveOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(outside, "precious")); err != nil {
		t.Fatalf("remove followed a link: %v", err)
	}

	e.assertOnly()
}

// TestUpdateBundledToRelease checks that the official release of a bundled plugin replaces the
// embedded copy, that a release from another repository does not, and that --force does.
//
// Parameters:
//   - t: test to report to
func TestUpdateBundledToRelease(t *testing.T) {
	gft := func(version string) string {
		return strings.Replace(manifest("demo", version, []string{"exec:tool"}, cmdWith("go", "")),
			`id = "demo"`, `id = "gft"`, 1)
	}

	l := newLab(t)
	l.fakeBin("tool")

	h := newHub(t)
	b := bundle(t, l, map[string]string{"plugin.toml": gft("1.0.0")})
	e := &relEnv{t: t, l: l, h: h, r: l.reg(func(o *Options) {
		o.Bundled = b
		o.Transport = h.transport()
	})}

	st := plan(t, e.r, InstallRequest{Source: "gft"})
	if _, err := st.Commit(Grant{Prompt: true}); err != nil {
		t.Fatal(err)
	}

	data := archiveOf(t, gft("1.1.0"))
	official := e.publish("totecli", "gft", "1.1.0", "plugin-gft.tar.gz", data)
	req := InstallRequest{Source: official, SHA256: sumOf(data), ID: "gft", Update: true}

	// The id gft is official: only totecli/gft may ship it, so a release of another repository
	// is refused as an id before the question of the origin comes up.
	foreign := e.publish("someone", "gft", "1.1.0", "plugin-gft.tar.gz", data)

	pe := e.planCode(InstallRequest{Source: foreign, SHA256: sumOf(data), ID: "gft", Update: true})
	if pe.Code != CodeIDInvalid {
		t.Errorf("foreign release of an official id: %s", pe.Code)
	}

	st = plan(t, e.r, req)
	d := st.Diff()
	if d.Class != OriginOfficial || d.OldVersion != "1.0.0" || len(d.Warnings) != 0 {
		t.Fatalf("diff = %+v", d)
	}

	if _, err := st.Commit(Grant{}); err != nil {
		t.Fatal(err)
	}

	rec, _ := e.l.lock().Get("gft")
	if rec.Kind != KindRelease || rec.Origin != "totecli/gft" || rec.Version != "1.1.0" {
		t.Errorf("record = %+v", rec)
	}

	e.assertOnly("gft")
}

// TestInstallLocalArchiveNotRegular checks that an archive path that turns into a FIFO after it
// was classified cannot stall the install: it is refused.
//
// Parameters:
//   - t: test to report to
func TestInstallLocalArchiveNotRegular(t *testing.T) {
	e := newRelEnv(t)

	// copyLocal is what runs after the classification; give it the FIFO directly.
	fifo := filepath.Join(e.l.root, "fifo")
	must(t, syscall.Mkfifo(fifo, 0o600))

	done := make(chan error, 1)

	go func() {
		_, err := copyLocal(fifo, &bytes.Buffer{})
		done <- err
	}()

	select {
	case err := <-done:
		if pe := asError(t, err); pe.Code != CodeBadSource {
			t.Errorf("code = %s", pe.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a FIFO stalled the install")
	}
}

// TestInstallReleaseTagMismatch checks that the version of the manifest must be the tag of the
// release it was published under: a plugin cannot present itself as another version than the one
// the pinned URL names.
//
// Parameters:
//   - t: test to report to
func TestInstallReleaseTagMismatch(t *testing.T) {
	data := demoArchive(t, "1.0.0")

	tests := []struct {
		name   string
		source func(e *relEnv) string
	}{
		{"url", func(e *relEnv) string {
			return e.publish("o", "r", "1.0.1", "demo.tar.gz", data)
		}},
		{"url with a v", func(e *relEnv) string {
			return e.publish("o", "r", "v1.0.0", "demo.tar.gz", data)
		}},
		{"shorthand", func(e *relEnv) string {
			e.publish("o", "demo", "2.0.0", "demo.tar.gz", data)

			return "o/demo@2.0.0"
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRelEnv(t)

			pe := e.planCode(InstallRequest{Source: tt.source(e), SHA256: sumOf(data)})
			if pe.Code != CodeTagMismatch || len(pe.Args) < 2 || pe.Args[1] != "1.0.0" {
				t.Fatalf("code = %s %v", pe.Code, pe.Args)
			}

			e.assertOnly()
		})
	}

	// An update is held to the same rule, and the old version stays.
	e := updateEnv(t, manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", "")))
	v2 := demoArchive(t, "1.1.0")
	url := e.publish("o", "r", "9.9.9", "demo.tar.gz", v2)

	pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(v2), ID: "demo", Update: true})
	if pe.Code != CodeTagMismatch {
		t.Errorf("update: %s", pe.Code)
	}

	e.assertOnly("demo")
}

// TestInstallReleasePlatformAddress checks that a manifest cannot steer the download of its
// platform asset to another path: the name of the asset is a plain file name.
//
// Parameters:
//   - t: test to report to
func TestInstallReleasePlatformAddress(t *testing.T) {
	for _, asset := range []string{"..", ".", "a/b", "../../x/y/releases/download/1.0.0/z"} {
		e := newRelEnv(t)
		bld := "[[platform]]\nos = \"linux\"\narch = \"amd64\"\nbin = \"bin/lin\"\n" +
			"asset = \"" + asset + "\"\nsha256 = \"" + strings.Repeat("a", 64) + "\"\n"
		main := archiveOf(t, build(TierExec, "", nil, "[exec]\n", bld))
		url := e.publish("o", "r", "1.0.0", "demo.tar.gz", main)

		pe := e.planCode(InstallRequest{Source: url, SHA256: sumOf(main)})
		if pe.Code != CodeValueInvalid && pe.Code != CodeManifestInvalid {
			t.Errorf("asset %q: %s %v", asset, pe.Code, pe.Args)
		}

		if got := e.h.count(hostGH); got != 1 {
			t.Errorf("asset %q: %d requests to github.com", asset, got)
		}

		e.assertOnly()
	}
}

// TestUpdateReleaseHostile serves hostile archives as the new version of an installed plugin:
// each is refused with its own code, the old version stays installed, consented and enabled, and
// nothing is left in staging.
//
// Parameters:
//   - t: test to report to
func TestUpdateReleaseHostile(t *testing.T) {
	for _, tt := range hostileArchives(t) {
		t.Run(tt.name, func(t *testing.T) {
			e := updateEnv(t, manifest("demo", "1.0.0", []string{"exec:tool"}, cmdWith("go", "")))
			before, _ := e.l.lock().Get("demo")
			trees := treeNames(t, e, "demo")

			url := e.publish("o", "r", "1.1.0", "demo.tar.gz", tt.data)

			pe := e.planCode(InstallRequest{
				Source: url, SHA256: sumOf(tt.data), ID: "demo",
				Update: true,
			})
			if pe.Code != tt.code {
				t.Fatalf("code = %s %v, want %s", pe.Code, pe.Args, tt.code)
			}

			now, _ := e.l.lock().Get("demo")
			if !equalRecords(before, now) || !slices.Equal(trees, treeNames(t, e, "demo")) {
				t.Error("a refused update changed the installed plugin")
			}

			if p, _ := e.r.Recheck("demo"); p.State != StateEnabled {
				t.Errorf("state = %s", p.State)
			}

			e.assertOnly("demo")
		})
	}
}

// TestReleaseFailureAtEveryPoint injects an I/O failure at each point of the install of a
// release and of the update of one, and checks that the lock parses, every record has its tree,
// the previous version still runs when the switch did not happen, and that neither staging, a
// downloaded file nor a spare tree is left behind.
//
// Parameters:
//   - t: test to report to
func TestReleaseFailureAtEveryPoint(t *testing.T) {
	for _, update := range []bool{false, true} {
		for _, point := range points {
			name := point + " new"
			if update {
				name = point + " update"
			}

			t.Run(name, func(t *testing.T) {
				var e *relEnv

				if update {
					e = updateEnv(t, manifest("demo", "1.0.0", []string{"exec:tool"},
						cmdWith("go", "")))
				} else {
					e = newRelEnv(t)
				}

				data := demoArchive(t, "1.1.0")
				url := e.publish("o", "r", "1.1.0", "demo.tar.gz", data)
				req := InstallRequest{Source: url, SHA256: sumOf(data), Update: update}

				if update {
					req.ID = "demo"
				}

				withHook(t, failAt(point))

				st, err := e.r.Plan(t.Context(), req)

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
				}

				checkInvariants(t, e.l, "demo", want)

				if got := stagingNames(t, e.l); len(got) != 0 {
					t.Errorf("staging holds %v", got)
				}

				want1 := 0
				if want != "" {
					want1 = 1
				}

				if recs := e.l.lock().Records(); len(recs) != want1 {
					t.Errorf("records = %v", recs)
				}

				idDir, _ := e.l.paths.PluginDir("demo")
				if entries, _ := os.ReadDir(idDir); len(entries) != want1 {
					t.Errorf("trees = %v, want %d", entries, want1)
				}
			})
		}
	}
}
