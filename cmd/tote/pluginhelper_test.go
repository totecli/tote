package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/totecli/tote/internal/plugin/bundled"
)

// Hosts of a release download: the repository host and the host the assets are served from.
const (
	hostRepo   = "github.com"
	hostAssets = "release-assets.githubusercontent.com"
)

// pluginEnv is a harness for the plugin verbs: a sandbox, a directory of plugin sources and a
// server that plays GitHub.
type pluginEnv struct {
	*harness
	src  string
	hub  *releaseHub
	blob int
}

// newPluginEnv builds the sandbox. The plugins shipped with tote are off by default, so that a
// test only sees what it installs; withBundled turns them on.
//
// Parameters:
//   - t: running test
func newPluginEnv(t *testing.T) *pluginEnv {
	t.Helper()

	h := newHarness(t)
	e := &pluginEnv{harness: h, src: filepath.Join(h.dir, "src"), hub: newReleaseHub(t)}

	if err := os.MkdirAll(e.src, 0o755); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(h.dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}

	h.vars["PATH"] = bin
	h.app.PluginTransport = e.hub.transport()

	// An installed tree is read-only: give the permissions back before the sandbox is removed.
	t.Cleanup(func() { makeWritable(h.dir) })

	return e
}

// withBundled turns the plugins shipped with tote on.
func (e *pluginEnv) withBundled() *pluginEnv {
	e.app.Bundled = bundled.Set{}

	return e
}

// manifestOf writes the plugin.toml of a neutral declarative plugin with one command.
//
// Parameters:
//   - id: plugin id
//   - version: plugin version
//   - perms: permissions, such as exec:echo
func manifestOf(id, version string, perms ...string) string {
	quoted := make([]string, len(perms))
	for i, p := range perms {
		quoted[i] = fmt.Sprintf("%q", p)
	}

	return fmt.Sprintf(`api = 1
id = %q
version = %q
tote = ">=1.0.0, <2"
title = "Demo %s"
description = "A neutral demo plugin."
license = "MIT"
tier = "declarative"
permissions = [%s]

[[command]]
id = "hello"
label = "Say hello"
run = ["echo", "hello"]
output = "pager"
`, id, version, id, strings.Join(quoted, ", "))
}

// plugin writes a plugin source directory and returns its path.
//
// Parameters:
//   - id: plugin id, also the name of the directory
//   - version: plugin version
//   - perms: permissions
func (e *pluginEnv) plugin(id, version string, perms ...string) string {
	e.t.Helper()

	return e.pluginText(id, manifestOf(id, version, perms...))
}

// pluginText writes a plugin source directory from the text of its manifest.
//
// Parameters:
//   - id: name of the directory
//   - text: contents of plugin.toml
func (e *pluginEnv) pluginText(id, text string) string {
	e.t.Helper()

	dir := filepath.Join(e.src, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(text), 0o644); err != nil {
		e.t.Fatal(err)
	}

	return dir
}

// archive builds the release archive of a manifest.
//
// Parameters:
//   - t: running test
//   - manifest: contents of plugin.toml
func archive(t *testing.T, manifest string) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)

	hdr := &tar.Header{
		Name: "plugin.toml", Mode: 0o644, Size: int64(len(manifest)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}

	if _, err := tw.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// sum gives the sha256 of data in hexadecimal.
//
// Parameters:
//   - data: bytes to hash
func sum(data []byte) string {
	s := sha256.Sum256(data)

	return hex.EncodeToString(s[:])
}

// release publishes an asset the way GitHub does, through a redirect to the asset host, and
// returns its URL.
//
// Parameters:
//   - owner: owner of the repository
//   - repo: repository
//   - tag: tag of the release
//   - asset: name of the asset
//   - data: contents
func (e *pluginEnv) release(owner, repo, tag, asset string, data []byte) string {
	e.t.Helper()

	e.blob++
	blob := fmt.Sprintf("/blob/%d", e.blob)
	path := "/" + owner + "/" + repo + "/releases/download/" + tag + "/" + asset

	e.hub.handle(hostRepo, path, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://"+hostAssets+blob+"?sig=x", http.StatusFound)
	})
	e.hub.handle(hostAssets, blob, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	})

	return "https://" + hostRepo + path
}

// releaseHub is a TLS server that answers for every host a download may name.
type releaseHub struct {
	srv   *httptest.Server
	mu    sync.Mutex
	route map[string]http.HandlerFunc
}

// newReleaseHub starts the server; it stops with the test.
//
// Parameters:
//   - t: running test
func newReleaseHub(t *testing.T) *releaseHub {
	t.Helper()

	h := &releaseHub{route: map[string]http.HandlerFunc{}}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		fn := h.route[r.Host+r.URL.Path]
		h.mu.Unlock()

		if fn == nil {
			http.NotFound(w, r)

			return
		}

		fn(w, r)
	}))

	t.Cleanup(h.srv.Close)

	return h
}

// handle sets the answer for a host and a path.
//
// Parameters:
//   - host: host name of the request
//   - path: path of the request
//   - fn: handler
func (h *releaseHub) handle(host, path string, fn http.HandlerFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.route[host+path] = fn
}

// transport returns a transport that reaches the server for every host and trusts its
// certificate.
func (h *releaseHub) transport() http.RoundTripper {
	pool := x509.NewCertPool()
	pool.AddCert(h.srv.Certificate())

	addr := h.srv.Listener.Addr().String()

	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{
			ServerName: "example.com", RootCAs: pool,
			MinVersion: tls.VersionTLS12,
		},
		DisableCompression: true,
		DisableKeepAlives:  true,
	}
}

// norm replaces what changes from one run to the next in an output: the sandbox and the working
// directory.
//
// Parameters:
//   - s: output of a verb
func (e *pluginEnv) norm(s string) string {
	s = strings.ReplaceAll(s, e.dir, "<sandbox>")

	if wd, err := os.Getwd(); err == nil {
		s = strings.ReplaceAll(s, wd, "<cwd>")
	}

	return s
}

// do runs a command line and returns the exit code, standard output and standard error, both
// normalised.
//
// Parameters:
//   - args: command line without the program name
func (e *pluginEnv) do(args ...string) (int, string, string) {
	e.t.Helper()

	rc := e.run(args...)

	return rc, e.norm(e.out.String()), e.norm(e.errb.String())
}

// must runs a command line that has to succeed.
//
// Parameters:
//   - args: command line without the program name
func (e *pluginEnv) must(args ...string) string {
	e.t.Helper()

	rc, out, errs := e.do(args...)
	if rc != 0 {
		e.t.Fatalf("tote %v: exit %d, stdout %q, stderr %q", args, rc, out, errs)
	}

	return out
}

// grantOf reads the --grant token that --dry-run prints for a source.
//
// Parameters:
//   - source: what would be installed
//   - extra: more arguments for the command, such as --sha256 and its value
func (e *pluginEnv) grantOf(source string, extra ...string) string {
	e.t.Helper()

	out := e.must(append([]string{"plugin", "install", source, "--dry-run", "--json"}, extra...)...)

	i := strings.Index(out, `"grant": "`)
	if i < 0 {
		e.t.Fatalf("no grant in %s", out)
	}

	rest := out[i+len(`"grant": "`):]

	return rest[:strings.Index(rest, `"`)]
}

// install installs a local plugin with the grant it asks for.
//
// Parameters:
//   - source: directory of the plugin
func (e *pluginEnv) install(source string) {
	e.t.Helper()

	e.must("plugin", "install", source, "--grant", e.grantOf(source))
}

// constValues reads the string constants of the plugin package whose name starts with a prefix.
//
// Parameters:
//   - t: running test
//   - prefix: start of the name, such as Code or Reason
func constValues(t *testing.T, prefix string) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "..", "internal", "plugin", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("cannot list the plugin package: %v", err)
	}

	var out []string

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, perr := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if perr != nil {
			t.Fatal(perr)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}

			if len(vs.Values) == 0 {
				return true
			}

			for i, id := range vs.Names {
				lit, isLit := vs.Values[min(i, len(vs.Values)-1)].(*ast.BasicLit)
				if !strings.HasPrefix(id.Name, prefix) || !isLit || lit.Kind != token.STRING {
					continue
				}

				if v, uerr := strconv.Unquote(lit.Value); uerr == nil {
					out = append(out, v)
				}
			}

			return true
		})
	}

	return out
}

// allCodes lists the stable codes of errors, issues and findings of the plugin package.
//
// Parameters:
//   - t: running test
func allCodes(t *testing.T) []string { return constValues(t, "Code") }

// makeWritable gives the owner write permission on a whole tree, so that the sandbox of a test
// can be removed after it installed read-only plugin trees.
//
// Parameters:
//   - root: directory to open up
func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			_ = os.Chmod(path, 0o700)
		} else if !d.Type().IsRegular() {
			return nil
		} else {
			_ = os.Chmod(path, 0o600)
		}

		return nil
	})
}
