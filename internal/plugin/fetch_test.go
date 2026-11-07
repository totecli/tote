package plugin

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Hosts of the local release server.
const (
	hostGH     = "github.com"
	hostAssets = "release-assets.githubusercontent.com"
	hostObject = "objects.githubusercontent.com"
	hostEvil   = "evil.example.org"
)

// hit is one request the local server received.
type hit struct {
	host   string
	path   string
	header http.Header
}

// hub is a local TLS server that plays github.com and its asset hosts: it routes by the Host
// header. Its transport sends every connection to it and trusts its certificate, so that the
// code under test runs unchanged and no byte leaves the machine.
type hub struct {
	t     *testing.T
	srv   *httptest.Server
	mu    sync.Mutex
	hits  []hit
	route map[string]http.HandlerFunc
}

// newHub starts the server; it is stopped with the test.
//
// Parameters:
//   - t: test to report to
func newHub(t *testing.T) *hub {
	t.Helper()

	h := &hub{t: t, route: map[string]http.HandlerFunc{}}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(h.serve))

	t.Cleanup(h.srv.Close)

	return h
}

// serve records a request and runs its route.
//
// Parameters:
//   - w: response writer
//   - r: request
func (h *hub) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.hits = append(h.hits, hit{host: r.Host, path: r.URL.Path, header: r.Header.Clone()})
	fn := h.route[r.Host+r.URL.Path]
	h.mu.Unlock()

	if fn == nil {
		http.NotFound(w, r)

		return
	}

	fn(w, r)
}

// handle sets the route of a host and a path.
//
// Parameters:
//   - host: host name of the request
//   - path: path of the request
//   - fn: handler
func (h *hub) handle(host, path string, fn http.HandlerFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.route[host+path] = fn
}

// bytesAt serves a body with status 200.
//
// Parameters:
//   - host: host name of the request
//   - path: path of the request
//   - data: body
func (h *hub) bytesAt(host, path string, data []byte) {
	h.handle(host, path, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) })
}

// redirectAt answers with a redirect.
//
// Parameters:
//   - host: host name of the request
//   - path: path of the request
//   - to: value of Location
func (h *hub) redirectAt(host, path, to string) {
	h.handle(host, path, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, to, http.StatusFound)
	})
}

// count returns how many requests a host received.
//
// Parameters:
//   - host: host name of the request
func (h *hub) count(host string) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	n := 0

	for _, x := range h.hits {
		if x.host == host {
			n++
		}
	}

	return n
}

// seen returns a copy of the requests received.
func (h *hub) seen() []hit {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]hit(nil), h.hits...)
}

// transportFor returns a transport that reaches the server for every host.
//
// Parameters:
//   - serverName: name the certificate is checked against, "example.com" for the test one
//   - trust: true to trust the certificate of the server
func (h *hub) transportFor(serverName string, trust bool) http.RoundTripper {
	cfg := &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}

	if trust {
		pool := x509.NewCertPool()
		pool.AddCert(h.srv.Certificate())
		cfg.RootCAs = pool
	}

	addr := h.srv.Listener.Addr().String()

	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig:    cfg,
		DisableCompression: true,
		DisableKeepAlives:  true,
	}
}

// transport returns the transport that trusts the server.
func (h *hub) transport() http.RoundTripper { return h.transportFor("example.com", true) }

// sumOf returns the sha256 of data in hexadecimal.
//
// Parameters:
//   - data: bytes to hash
func sumOf(data []byte) string {
	s := sha256.Sum256(data)

	return hex.EncodeToString(s[:])
}

// relPath builds the path of a release asset.
//
// Parameters:
//   - owner: owner of the repository
//   - repo: repository
//   - tag: tag of the release
//   - asset: name of the asset
func relPath(owner, repo, tag, asset string) string {
	return "/" + owner + "/" + repo + "/releases/download/" + tag + "/" + asset
}

// fastTimeouts shortens the timeouts of a test.
//
// Parameters:
//   - t: test to report to
//   - tm: timeouts to use
func fastTimeouts(t *testing.T, tm netTimeouts) {
	t.Helper()

	old := timeouts
	timeouts = tm

	t.Cleanup(func() { timeouts = old })
}

// getAsset downloads one asset through a registry that uses the hub.
//
// Parameters:
//   - h: the server
//   - ref: the asset
//   - limit: largest body
func getAsset(h *hub, ref ReleaseRef, limit int64) (string, []byte, error) {
	r := Open(Options{Host: testHost(), Transport: h.transport()})

	var buf bytes.Buffer

	sum, _, err := r.fetcher().download(context.Background(), ref, limit, &buf)

	return sum, buf.Bytes(), err
}

// demoRef is the asset used by the download tests.
var demoRef = ReleaseRef{Owner: "o", Repo: "r", Tag: "1.0.0", Asset: "demo.tar.gz"}

// demoPath is the path of demoRef.
var demoPath = relPath("o", "r", "1.0.0", "demo.tar.gz")

// TestParseRelease checks the grammar of a release URL, vector by vector.
//
// Parameters:
//   - t: test to report to
func TestParseRelease(t *testing.T) {
	const base = "https://github.com/o/r/releases/download/1.0.0/demo.tar.gz"

	tests := []struct {
		name string
		text string
		want string
	}{
		{"plain", base, "o/r"},
		{
			"upper case host",
			"https://GitHub.COM/Own/Rep/releases/download/v1.2.3-rc.1+b/p.tar.gz",
			"own/rep",
		},
		{"upper case scheme", "HTTPS://github.com/o/r/releases/download/1/p", "o/r"},
		{
			"charset", "https://github.com/a.b_c-d+e/r-1/releases/download/1.0.0/a+b.c_d-e.tgz",
			"a.b_c-d+e/r-1",
		},
		{"http", "http://github.com/o/r/releases/download/1.0.0/p.tar.gz", ""},
		{"other host", "https://example.com/o/r/releases/download/1.0.0/p.tar.gz", ""},
		{"sub domain", "https://api.github.com/o/r/releases/download/1.0.0/p.tar.gz", ""},
		{"suffix host", "https://evilgithub.com/o/r/releases/download/1.0.0/p.tar.gz", ""},
		{"trailing dot", "https://github.com./o/r/releases/download/1.0.0/p.tar.gz", ""},
		{"port", "https://github.com:443/o/r/releases/download/1.0.0/p.tar.gz", ""},
		{"empty port", "https://github.com:/o/r/releases/download/1.0.0/p.tar.gz", ""},
		{"userinfo", "https://u@github.com/o/r/releases/download/1.0.0/p.tar.gz", ""},
		{"userinfo disguise", "https://github.com@evil.example.org/o/r/releases/download/1/p", ""},
		{"query", base + "?x=1", ""},
		{"empty query", base + "?", ""},
		{"fragment", base + "#x", ""},
		{"percent", "https://github.com/o/r/releases/download/1.0.0/p%2etar.gz", ""},
		{"encoded dots", "https://github.com/o/r/releases/download/%2e%2e/p.tar.gz", ""},
		{"parent", "https://github.com/o/r/releases/download/../p.tar.gz", ""},
		{"dot owner", "https://github.com/./r/releases/download/1.0.0/p.tar.gz", ""},
		{"dot dot repo", "https://github.com/o/../releases/download/1.0.0/p.tar.gz", ""},
		{"dot tag", "https://github.com/o/r/releases/download/./p.tar.gz", ""},
		{"double slash", "https://github.com//o/r/releases/download/1.0.0/p.tar.gz", ""},
		{"double slash inside", "https://github.com/o/r/releases//download/1.0.0/p.tar.gz", ""},
		{"trailing slash", base + "/", ""},
		{"six segments", "https://github.com/o/r/releases/download/p.tar.gz", ""},
		{"eight segments", "https://github.com/o/r/releases/download/1.0.0/x/p.tar.gz", ""},
		{"wrong word", "https://github.com/o/r/archive/download/1.0.0/p.tar.gz", ""},
		{"wrong second word", "https://github.com/o/r/releases/latest/1.0.0/p.tar.gz", ""},
		{"backslash", `https://github.com/o/r/releases/download/1.0.0/p\q.tar.gz`, ""},
		{"space", "https://github.com/o/r/releases/download/1.0.0/p q.tar.gz", ""},
		{"non ascii", "https://github.com/o/r/releases/download/1.0.0/p\u00e9.tar.gz", ""},
		{"empty owner", "https://github.com//r/releases/download/1.0.0/p.tar.gz", ""},
		{"long asset", "https://github.com/o/r/releases/download/1.0.0/" + strings.Repeat("a",
			256), ""},
		{"not a url", "github.com/o/r", ""},
		{"scheme only", "https://", ""},
		{"ftp", "ftp://github.com/o/r/releases/download/1.0.0/p.tar.gz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseRelease(tt.text)

			if tt.want == "" {
				if err == nil {
					t.Fatalf("accepted as %+v", ref)
				}

				if pe := asError(t, err); pe.Code != CodeBadSource {
					t.Errorf("code = %s", pe.Code)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if ref.Origin() != tt.want {
				t.Errorf("origin = %s, want %s", ref.Origin(), tt.want)
			}

			if again, err := ParseRelease(ref.URL()); err != nil || again != ref {
				t.Errorf("canonical URL %s does not parse back: %v", ref.URL(), err)
			}
		})
	}
}

// TestParseShorthand checks owner/repo@tag, which stands for <repo>.tar.gz of that release.
//
// Parameters:
//   - t: test to report to
func TestParseShorthand(t *testing.T) {
	ref, err := ParseShorthand("Owner/plugin-gft@v1.0.0-rc.1")
	if err != nil {
		t.Fatal(err)
	}

	want := "https://github.com/Owner/plugin-gft/releases/download/v1.0.0-rc.1/plugin-gft.tar.gz"
	if ref.URL() != want || ref.Origin() != "owner/plugin-gft" {
		t.Errorf("ref = %+v %s", ref, ref.URL())
	}

	for _, bad := range []string{
		"owner/repo", "owner/repo@", "owner@1", "/repo@1", "owner/@1",
		"a/b/c@1", "owner/repo@1@2", "owner/..@1", "../repo@1", "o/r@..", "o/r@a b", "o/r@1/2",
		"o/" + strings.Repeat("a", 250) + "@1",
	} {
		if _, err := ParseShorthand(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}

	if _, err := ParseReleaseSource("https://example.com/x"); err == nil {
		t.Error("a foreign URL was accepted as a release source")
	}
}

// TestNormalizeSum checks the pin: 64 hexadecimal digits in either case.
//
// Parameters:
//   - t: test to report to
func TestNormalizeSum(t *testing.T) {
	good := sumOf([]byte("x"))

	if got, err := NormalizeSum(strings.ToUpper(good)); err != nil || got != good {
		t.Errorf("upper case: %q %v", got, err)
	}

	for _, bad := range []string{
		"", good[:63], good + "0", strings.Repeat("g", 64), " " + good,
		good + "\n", "0x" + good[2:],
	} {
		_, err := NormalizeSum(bad)

		if pe := asError(t, err); pe.Code != CodeHashMalformed {
			t.Errorf("%q: %s", bad, pe.Code)
		}
	}
}

// TestSidecar checks the format of the <asset>.sha256 file and its parser.
//
// Parameters:
//   - t: test to report to
func TestSidecar(t *testing.T) {
	sum := sumOf([]byte("x"))

	if got := Sidecar(sum, "demo.tar.gz"); got != sum+"  demo.tar.gz\n" {
		t.Errorf("Sidecar = %q", got)
	}

	tests := []struct {
		name string
		text string
		ok   bool
	}{
		{"line of sha256sum", sum + "  demo.tar.gz\n", true},
		{"binary mode", sum + " *demo.tar.gz\n", true},
		{"digest alone", sum + "\n", true},
		{"upper case", strings.ToUpper(sum), true},
		{"another file", sum + "  other.tar.gz\n", false},
		{"two lines", sum + "  demo.tar.gz\n" + sum + "  demo.tar.gz\n", false},
		{"short", sum[:60], false},
		{"empty", "", false},
		{"three fields", sum + " demo.tar.gz x", false},
	}

	for _, tt := range tests {
		got, err := ParseSidecar([]byte(tt.text), "demo.tar.gz")

		if tt.ok && (err != nil || got != sum) {
			t.Errorf("%s: %q %v", tt.name, got, err)
		}

		if !tt.ok && err == nil {
			t.Errorf("%s: accepted", tt.name)
		}
	}
}

// TestChecksumFromTheSidecar downloads a sidecar and returns the digest it holds, and refuses a
// sidecar that is not about the asset.
//
// Parameters:
//   - t: test to report to
func TestChecksumFromTheSidecar(t *testing.T) {
	h := newHub(t)
	sum := sumOf([]byte("asset"))

	h.bytesAt(hostGH, demoPath+".sha256", []byte(Sidecar(sum, "demo.tar.gz")))
	h.bytesAt(hostGH, relPath("o", "r", "1.0.0", "r.tar.gz.sha256"),
		[]byte(Sidecar(sum, "other.tar.gz")))

	r := Open(Options{Host: testHost(), Transport: h.transport()})

	got, err := r.Checksum(context.Background(), "https://github.com"+demoPath)
	if err != nil || got != sum {
		t.Fatalf("Checksum = %q %v", got, err)
	}

	_, err = r.Checksum(context.Background(), "o/r@1.0.0")
	if pe := asError(t, err); pe.Code != CodeHashMalformed {
		t.Errorf("sidecar of another file: %v", err)
	}

	_, err = r.Checksum(context.Background(), "o/missing@1.0.0")
	if pe := asError(t, err); pe.Code != CodeNetStatus {
		t.Errorf("missing sidecar: %v", err)
	}

	_, err = r.Checksum(context.Background(), "http://github.com/x")
	if pe := asError(t, err); pe.Code != CodeBadSource {
		t.Errorf("bad source: %v", err)
	}

	h.bytesAt(hostGH, demoPath+".sha256", bytes.Repeat([]byte("a"), maxSidecar+1))

	_, err = r.Checksum(context.Background(), "https://github.com"+demoPath)
	if pe := asError(t, err); pe.Code != CodeTooLarge {
		t.Errorf("huge sidecar: %v", err)
	}
}

// TestDownloadRedirects follows the legitimate path of a GitHub download, a redirect to the asset
// host, and checks what is sent along the way.
//
// Parameters:
//   - t: test to report to
func TestDownloadRedirects(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("GH_TOKEN", "ghp_secret")

	data := []byte("the asset bytes")
	h := newHub(t)

	h.handle(hostGH, demoPath, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "x"})
		http.Redirect(w, r, "https://release-assets.githubusercontent.com/blob/1?sig=abc",
			http.StatusFound)
	})
	h.handle(hostAssets, "/blob/1", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/blob/2", http.StatusTemporaryRedirect)
	})
	h.redirectAt(hostAssets, "/blob/2", "https://objects.githubusercontent.com/blob/3")
	h.bytesAt(hostObject, "/blob/3", data)

	sum, body, err := getAsset(h, demoRef, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	if sum != sumOf(data) || !bytes.Equal(body, data) {
		t.Errorf("sum = %s body = %q", sum, body)
	}

	hits := h.seen()
	if len(hits) != 4 {
		t.Fatalf("%d requests: %+v", len(hits), hits)
	}

	for _, x := range hits {
		for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
			if v := x.header.Get(name); v != "" {
				t.Errorf("%s sent %s = %q", x.host, name, v)
			}
		}

		if ua := x.header.Get("User-Agent"); ua != "tote/1.0.3" {
			t.Errorf("%s User-Agent = %q", x.host, ua)
		}
	}
}

// TestDownloadRedirectRules is the table of redirects that must be refused, and the ones that
// must not. Nothing is requested from a host that is not on the list.
//
// Parameters:
//   - t: test to report to
func TestDownloadRedirectRules(t *testing.T) {
	data := []byte("ok")
	target := "https://github.com/o/r/releases/download/1.0.0/demo.tar.gz"

	tests := []struct {
		name  string
		to    string
		code  string
		host  string
		noLoc bool
	}{
		{"to another host", "https://evil.example.org/x", CodeNetRedirect, hostEvil, false},
		{"to a scheme relative host", "//evil.example.org/x", CodeNetRedirect, hostEvil, false},
		{
			"to a host in upper case", "HTTPS://EVIL.EXAMPLE.ORG/x", CodeNetRedirect, hostEvil,
			false,
		},
		{
			"to a lookalike host", "https://github.com.evil.example.org/x", CodeNetRedirect,
			"github.com.evil.example.org", false,
		},
		{"to an address", "https://10.0.0.5/x", CodeNetRedirect, "10.0.0.5", false},
		{
			"to the user information trick", "https://github.com@evil.example.org/x",
			CodeNetRedirect, hostEvil, false,
		},
		{"to http", "http://github.com" + demoPath, CodeNetRedirect, "", false},
		{
			"to http on the asset host", "http://release-assets.githubusercontent.com/x",
			CodeNetRedirect, "", false,
		},
		{
			"to a suffix host", "https://objects.githubusercontent.com.evil.example.org/x",
			CodeNetRedirect, "objects.githubusercontent.com.evil.example.org", false,
		},
		{"to a prefix host", "https://evilgithub.com/x", CodeNetRedirect, "evilgithub.com", false},
		{
			"to a trailing dot host", "https://github.com./o/r/releases/download/1.0.0/demo.tar.gz",
			CodeNetRedirect, "", false,
		},
		{"to a port", "https://github.com:8443" + demoPath, CodeNetRedirect, "", false},
		{"to user information", "https://u:p@github.com" + demoPath, CodeNetRedirect, "", false},
		{
			"to a scheme that is not http", "ftp://github.com" + demoPath, CodeNetRedirect, "",
			false,
		},
		{
			"to another owner", "https://github.com/other/r/releases/download/1.0.0/demo.tar.gz",
			CodeOriginRedirect, "", false,
		},
		{
			"to another repository",
			"https://github.com/o/other/releases/download/1.0.0/demo.tar.gz",
			CodeOriginRedirect, "", false,
		},
		{
			"to another tag", "https://github.com/o/r/releases/download/2.0.0/demo.tar.gz",
			CodeNetRedirect, "", false,
		},
		{
			"to another asset", "https://github.com/o/r/releases/download/1.0.0/other.tar.gz",
			CodeNetRedirect, "", false,
		},
		{"to another path of github", "https://github.com/login", CodeNetRedirect, "", false},
		{"to a query on github", target + "?x=1", CodeNetRedirect, "", false},
		{"to nothing", "", CodeNetRedirect, "", true},
		{
			"to the same repository in other case",
			"https://github.com/O/R/releases/download/1.0.0/demo.tar.gz", "", "", false,
		},
		{"to the asset host", "https://release-assets.githubusercontent.com/blob", "", "", false},
		{"with a port 443", "https://objects.githubusercontent.com:443/blob", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHub(t)

			h.redirectAt(hostGH, demoPath, tt.to)
			h.bytesAt(hostGH, "/O/R/releases/download/1.0.0/demo.tar.gz", data)
			h.bytesAt(hostGH, "/o/r/releases/download/2.0.0/demo.tar.gz", data)
			h.bytesAt(hostAssets, "/blob", data)
			h.bytesAt(hostObject, "/blob", data)
			h.bytesAt(hostObject+":443", "/blob", data)

			if tt.noLoc {
				h.handle(hostGH, demoPath, func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusFound)
				})
			}

			_, _, err := getAsset(h, demoRef, 1<<20)

			if tt.code == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}

				return
			}

			if pe := asError(t, err); pe.Code != tt.code {
				t.Fatalf("code = %s %v, want %s", pe.Code, pe.Args, tt.code)
			}

			for _, host := range []string{hostEvil, tt.host} {
				if host != "" && h.count(host) != 0 {
					t.Errorf("%s was contacted", host)
				}
			}

			if n := len(h.seen()); n != 1 {
				t.Errorf("%d requests, want only the first", n)
			}
		})
	}
}

// TestDownloadRedirectLimit checks that three redirects are followed and the fourth is not, and
// that a loop ends.
//
// Parameters:
//   - t: test to report to
func TestDownloadRedirectLimit(t *testing.T) {
	data := []byte("end")

	t.Run("three redirects", func(t *testing.T) {
		h := newHub(t)
		h.redirectAt(hostGH, demoPath, "https://release-assets.githubusercontent.com/hop/b")
		h.redirectAt(hostAssets, "/hop/b", "/hop/c")
		h.redirectAt(hostAssets, "/hop/c", "/hop/d")
		h.bytesAt(hostAssets, "/hop/d", data)

		if _, body, err := getAsset(h, demoRef, 1<<20); err != nil || !bytes.Equal(body, data) {
			t.Fatalf("%q %v", body, err)
		}
	})

	t.Run("four redirects", func(t *testing.T) {
		h := newHub(t)
		h.redirectAt(hostGH, demoPath, "https://release-assets.githubusercontent.com/hop/b")
		h.redirectAt(hostAssets, "/hop/b", "/hop/c")
		h.redirectAt(hostAssets, "/hop/c", "/hop/d")
		h.redirectAt(hostAssets, "/hop/d", "/hop/e")
		h.bytesAt(hostAssets, "/hop/e", data)

		_, _, err := getAsset(h, demoRef, 1<<20)
		if pe := asError(t, err); pe.Code != CodeNetRedirect {
			t.Fatalf("err = %v", err)
		}

		if n := len(h.seen()); n != 4 {
			t.Errorf("%d requests, want 4", n)
		}
	})

	t.Run("loop", func(t *testing.T) {
		h := newHub(t)
		h.redirectAt(hostGH, demoPath, "https://github.com"+demoPath)

		_, _, err := getAsset(h, demoRef, 1<<20)
		if pe := asError(t, err); pe.Code != CodeNetRedirect {
			t.Fatalf("err = %v", err)
		}

		if n := len(h.seen()); n != 4 {
			t.Errorf("%d requests in a loop", n)
		}
	})

	t.Run("loop between two hosts", func(t *testing.T) {
		h := newHub(t)
		h.redirectAt(hostGH, demoPath, "https://release-assets.githubusercontent.com/a")
		h.redirectAt(hostAssets, "/a", "https://objects.githubusercontent.com/b")
		h.redirectAt(hostObject, "/b", "https://release-assets.githubusercontent.com/a")

		_, _, err := getAsset(h, demoRef, 1<<20)
		if pe := asError(t, err); pe.Code != CodeNetRedirect {
			t.Fatalf("err = %v", err)
		}
	})
}

// TestDownloadStatus checks that only 200 is an answer.
//
// Parameters:
//   - t: test to report to
func TestDownloadStatus(t *testing.T) {
	for _, code := range []int{
		http.StatusNotFound, http.StatusInternalServerError,
		http.StatusPartialContent, http.StatusNotModified, http.StatusForbidden,
		http.StatusNoContent, http.StatusAccepted, http.StatusTeapot,
	} {
		h := newHub(t)
		h.handle(hostGH, demoPath, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte("body"))
		})

		_, _, err := getAsset(h, demoRef, 1<<20)

		pe := asError(t, err)
		if pe.Code != CodeNetStatus || len(pe.Args) < 2 || pe.Args[1] != itoa(code) {
			t.Errorf("status %d: %v", code, err)
		}
	}
}

// TestDownloadSizeCap checks the 16 MiB cap, from the header and from the bytes that arrive.
//
// Parameters:
//   - t: test to report to
func TestDownloadSizeCap(t *testing.T) {
	const limit = 1024

	t.Run("announced", func(t *testing.T) {
		h := newHub(t)

		// The server closes the connection after 100 bytes: a client that read the body would
		// report a truncated download, not a body that is too large.
		h.handle(hostGH, demoPath, rawResponse(
			"HTTP/1.1 200 OK\r\nContent-Length: 1073741824\r\n\r\n", make([]byte, 100)))

		_, _, err := getAsset(h, demoRef, limit)

		if pe := asError(t, err); pe.Code != CodeTooLarge {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("without a length", func(t *testing.T) {
		h := newHub(t)
		h.handle(hostGH, demoPath, func(w http.ResponseWriter, _ *http.Request) {
			for range 10 {
				_, _ = w.Write(make([]byte, 512))
				w.(http.Flusher).Flush()
			}
		})

		_, _, err := getAsset(h, demoRef, limit)
		if pe := asError(t, err); pe.Code != CodeTooLarge {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("exactly the cap", func(t *testing.T) {
		h := newHub(t)
		h.bytesAt(hostGH, demoPath, make([]byte, limit))

		if _, body, err := getAsset(h, demoRef, limit); err != nil || len(body) != limit {
			t.Fatalf("%d %v", len(body), err)
		}
	})

	t.Run("one byte over", func(t *testing.T) {
		h := newHub(t)
		h.bytesAt(hostGH, demoPath, make([]byte, limit+1))

		_, _, err := getAsset(h, demoRef, limit)
		if pe := asError(t, err); pe.Code != CodeTooLarge {
			t.Fatalf("err = %v", err)
		}
	})
}

// rawResponse answers a request with bytes written straight to the connection, which lets a
// response lie about its length.
//
// Parameters:
//   - head: status line and headers, ended by a blank line
//   - body: bytes after the headers
func rawResponse(head string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		_, _ = buf.WriteString(head)
		_, _ = buf.Write(body)
		_ = buf.Flush()
	}
}

// TestDownloadLengthLies checks a body shorter than announced and one longer.
//
// Parameters:
//   - t: test to report to
func TestDownloadLengthLies(t *testing.T) {
	t.Run("shorter", func(t *testing.T) {
		h := newHub(t)
		h.handle(hostGH, demoPath, rawResponse("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\n",
			[]byte("0123456789")))

		_, _, err := getAsset(h, demoRef, 1<<20)
		if pe := asError(t, err); pe.Code != CodeNetTruncated {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("chunked and cut", func(t *testing.T) {
		h := newHub(t)
		h.handle(hostGH, demoPath, rawResponse(
			"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n", []byte("a\r\n0123456789\r\n")))

		_, _, err := getAsset(h, demoRef, 1<<20)
		if pe := asError(t, err); pe.Code != CodeNetTruncated {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("longer", func(t *testing.T) {
		h := newHub(t)
		h.handle(hostGH, demoPath, rawResponse("HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\n",
			[]byte("0123456789"+strings.Repeat("X", 1000))))

		sum, body, err := getAsset(h, demoRef, 1<<20)
		if err != nil {
			t.Fatal(err)
		}

		if string(body) != "0123456789" || sum != sumOf(body) {
			t.Errorf("only the announced bytes count: %q %s", body, sum)
		}
	})
}

// TestDownloadWireBytes checks that the hash covers the bytes of the asset as sent, even when the
// server labels them as gzip: no transparent decoding.
//
// Parameters:
//   - t: test to report to
func TestDownloadWireBytes(t *testing.T) {
	wire := gzipBytes([]byte("decoded content that must never be seen"))
	h := newHub(t)

	h.handle(hostGH, demoPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "" {
			t.Errorf("Accept-Encoding = %q", r.Header.Get("Accept-Encoding"))
		}

		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(wire)
	})

	sum, body, err := getAsset(h, demoRef, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(body, wire) || sum != sumOf(wire) {
		t.Errorf("hash is not over the wire bytes: %d bytes", len(body))
	}
}

// TestDownloadTimeouts checks the three clocks: the headers, a body that stops and a body that
// never ends.
//
// Parameters:
//   - t: test to report to
func TestDownloadTimeouts(t *testing.T) {
	t.Run("headers", func(t *testing.T) {
		fastTimeouts(t, netTimeouts{
			connect: time.Second, headers: 150 * time.Millisecond,
			idle: time.Second, total: 5 * time.Second,
		})

		h := newHub(t)
		h.handle(hostGH, demoPath, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
		})

		start := time.Now()
		_, _, err := getAsset(h, demoRef, 1<<20)

		if pe := asError(t, err); pe.Code != CodeNetTimeout {
			t.Fatalf("err = %v", err)
		}

		if time.Since(start) > 2*time.Second {
			t.Errorf("took %v", time.Since(start))
		}
	})

	t.Run("a body that stops", func(t *testing.T) {
		fastTimeouts(t, netTimeouts{
			connect: time.Second, headers: time.Second,
			idle: 200 * time.Millisecond, total: 5 * time.Second,
		})

		h := newHub(t)
		h.handle(hostGH, demoPath, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("x"))
			w.(http.Flusher).Flush()

			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		})

		start := time.Now()
		_, _, err := getAsset(h, demoRef, 1<<20)

		if pe := asError(t, err); pe.Code != CodeNetTimeout {
			t.Fatalf("err = %v", err)
		}

		if time.Since(start) > 2*time.Second {
			t.Errorf("took %v", time.Since(start))
		}
	})

	t.Run("a body that never ends", func(t *testing.T) {
		fastTimeouts(t, netTimeouts{
			connect: time.Second, headers: time.Second,
			idle: time.Second, total: 400 * time.Millisecond,
		})

		h := newHub(t)
		h.handle(hostGH, demoPath, func(w http.ResponseWriter, r *http.Request) {
			for {
				if _, err := w.Write([]byte("x")); err != nil {
					return
				}

				w.(http.Flusher).Flush()

				select {
				case <-r.Context().Done():
					return
				case <-time.After(30 * time.Millisecond):
				}
			}
		})

		start := time.Now()
		_, _, err := getAsset(h, demoRef, 1<<20)

		if pe := asError(t, err); pe.Code != CodeNetTimeout {
			t.Fatalf("err = %v", err)
		}

		if time.Since(start) > 3*time.Second {
			t.Errorf("took %v", time.Since(start))
		}
	})
}

// TestDownloadTransportErrors checks the codes of a connection that fails: an untrusted
// certificate, a certificate for another name, a server that is not there and a canceled call.
//
// Parameters:
//   - t: test to report to
func TestDownloadTransportErrors(t *testing.T) {
	h := newHub(t)
	h.bytesAt(hostGH, demoPath, []byte("x"))

	run := func(rt http.RoundTripper) error {
		r := Open(Options{Host: testHost(), Transport: rt})

		_, _, err := r.fetcher().download(context.Background(), demoRef, 1<<20,
			&bytes.Buffer{})

		return err
	}

	if pe := asError(t, run(h.transportFor("example.com", false))); pe.Code != CodeNetTLS {
		t.Errorf("untrusted certificate: %s", pe.Code)
	}

	if pe := asError(t, run(h.transportFor("wrong.example.net", true))); pe.Code != CodeNetTLS {
		t.Errorf("wrong name: %s", pe.Code)
	}

	refused := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn,
		error,
	) {
		return nil, errors.New("connection refused")
	}}

	if pe := asError(t, run(refused)); pe.Code != CodeNetOffline {
		t.Errorf("refused: %s", pe.Code)
	}

	r := Open(Options{Host: testHost(), Transport: h.transport()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := r.fetcher().download(ctx, demoRef, 1<<20, &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("canceled call: %v", err)
	}
}

// TestDefaultTransport checks the transport that real downloads use: the proxy of the
// environment, TLS 1.2 or later with the system roots, no transparent decoding, and the timeouts.
//
// Parameters:
//   - t: test to report to
func TestDefaultTransport(t *testing.T) {
	tr := newTransport()

	if tr.Proxy == nil || reflect.ValueOf(tr.Proxy).Pointer() !=
		reflect.ValueOf(http.ProxyFromEnvironment).Pointer() {
		t.Error("the proxy of the environment is not used")
	}

	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion < tls.VersionTLS12 ||
		tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.RootCAs != nil {
		t.Errorf("TLS config = %+v", tr.TLSClientConfig)
	}

	if !tr.DisableCompression {
		t.Error("compression is not disabled")
	}

	if tr.TLSHandshakeTimeout != 10*time.Second || tr.ResponseHeaderTimeout != 20*time.Second {
		t.Errorf("timeouts = %v %v", tr.TLSHandshakeTimeout, tr.ResponseHeaderTimeout)
	}

	if timeouts.idle != 30*time.Second || timeouts.total != 5*time.Minute ||
		timeouts.connect != 10*time.Second || timeouts.headers != 20*time.Second {
		t.Errorf("timeouts = %+v", timeouts)
	}

	if maxRedirects != 3 || archiveLimit != 16<<20 {
		t.Errorf("redirects %d, limit %d", maxRedirects, archiveLimit)
	}

	if got := Open(Options{Host: testHost()}).fetcher().rt; got == nil {
		t.Error("no transport by default")
	}
}

// TestUserAgent checks that the version that reaches a header is cleaned.
//
// Parameters:
//   - t: test to report to
func TestUserAgent(t *testing.T) {
	tests := map[string]string{
		"1.0.3":            "tote/1.0.3",
		"1.0.3-rc.1+b5":    "tote/1.0.3-rc.1+b5",
		"":                 "tote/dev",
		"1.0\r\nX-Evil: 1": "tote/1.0X-Evil1",
		"\u00e9\u00e8":     "tote/dev",
	}

	for in, want := range tests {
		if got := userAgent(in); got != want {
			t.Errorf("userAgent(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNoInsecureTLS reads the sources of the package: nothing may turn certificate checks off or
// read a variable of the environment that changes the host list.
//
// Parameters:
//   - t: test to report to
func TestNoInsecureTLS(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(string(src), "InsecureSkipVerify") {
			t.Errorf("%s mentions InsecureSkipVerify", name)
		}
	}

	src, _ := os.ReadFile("fetch.go")
	if strings.Contains(string(src), "Getenv") || strings.Contains(string(src), "LookupEnv") {
		t.Error("fetch.go reads the environment")
	}
}

// TestDownloadRefusesAnUnsafeAddress checks that an address that does not follow the grammar of a
// release URL, as a manifest could make by naming its platform asset, is refused before any
// request is sent.
//
// Parameters:
//   - t: test to report to
func TestDownloadRefusesAnUnsafeAddress(t *testing.T) {
	for _, asset := range []string{"..", ".", "a/b", "a?b", "a%2fb", "", "a b"} {
		h := newHub(t)
		h.bytesAt(hostGH, "/o/r/releases/download/1.0.0/other", []byte("x"))

		ref := ReleaseRef{Owner: "o", Repo: "r", Tag: "1.0.0", Asset: asset}

		_, _, err := getAsset(h, ref, 1<<20)

		if pe := asError(t, err); pe.Code != CodeBadSource {
			t.Errorf("asset %q: %v", asset, err)
		}

		if n := len(h.seen()); n != 0 {
			t.Errorf("asset %q: %d requests were sent", asset, n)
		}
	}
}

// TestDownloadSlowFirstByte checks that the time allowed for the headers is spent once the
// headers are there: a server that answers at once and sends its first byte later than that is
// held only by the idle time.
//
// Parameters:
//   - t: test to report to
func TestDownloadSlowFirstByte(t *testing.T) {
	fastTimeouts(t, netTimeouts{
		connect: time.Second, headers: 150 * time.Millisecond,
		idle: 3 * time.Second, total: 5 * time.Second,
	})

	h := newHub(t)
	h.handle(hostGH, demoPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	})

	_, body, err := getAsset(h, demoRef, 1<<20)
	if err != nil || string(body) != "late" {
		t.Fatalf("body %q, err %v", body, err)
	}
}

// TestDownloadExpiredCertificate checks that a certificate that is out of date is refused as a
// TLS failure, even when it is trusted and made for the name that is asked (SEC-N07).
//
// Parameters:
//   - t: test to report to
func TestDownloadExpiredCertificate(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "example.com"},
		DNSNames:              []string{"example.com"},
		NotBefore:             time.Now().Add(-48 * time.Hour),
		NotAfter:              time.Now().Add(-24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	served := 0
	answer := func(w http.ResponseWriter, _ *http.Request) {
		served++
		_, _ = w.Write([]byte("body"))
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(answer))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}}}
	srv.StartTLS()

	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	rt := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
		TLSClientConfig:    &tls.Config{ServerName: "example.com", RootCAs: pool},
		DisableKeepAlives:  true,
		DisableCompression: true,
	}

	r := Open(Options{Host: testHost(), Transport: rt})

	_, _, err = r.fetcher().download(context.Background(), demoRef, 1<<20, &bytes.Buffer{})
	if pe := asError(t, err); pe.Code != CodeNetTLS {
		t.Fatalf("err = %v", err)
	}

	if served != 0 {
		t.Errorf("the server answered %d requests over an expired certificate", served)
	}
}
