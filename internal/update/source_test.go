package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestSourceValidate checks which bases are accepted.
//
// Parameters:
//   - t: test to report to
func TestSourceValidate(t *testing.T) {
	tests := []struct {
		base string
		ok   bool
	}{
		{"", true},
		{"https://example.org/tote", true},
		{"https://example.org/tote/", true},
		{"file:///srv/mirror", true},
		{"http://example.org/tote", false},
		{"HTTP://example.org", false},
		{"ftp://example.org", false},
		{"file://host/srv", false},
		{"file://relative", false},
		{"https://", false},
		{"/srv/mirror", false},
		{"example.org", false},
		{"://", false},
	}

	for _, tt := range tests {
		err := Source{Base: tt.base}.Validate()
		if (err == nil) != tt.ok {
			t.Errorf("Validate(%q) = %v, want ok = %v", tt.base, err, tt.ok)
		}

		if err != nil && !errors.Is(err, ErrInsecure) {
			t.Errorf("Validate(%q) error %v does not wrap ErrInsecure", tt.base, err)
		}
	}
}

// TestSourceLocations checks the URLs built for GitHub and for a mirror.
//
// Parameters:
//   - t: test to report to
func TestSourceLocations(t *testing.T) {
	gh := Source{}

	wantList := "https://api.github.com/repos/totecli/tote/releases?per_page=50"
	if got := gh.listLocation(); got != wantList {
		t.Errorf("GitHub list = %q", got)
	}

	if got := gh.assetLocation("1.0.0", "a.tar.gz"); got !=
		"https://github.com/totecli/tote/releases/download/1.0.0/a.tar.gz" {
		t.Errorf("GitHub asset = %q", got)
	}

	m := Source{Base: "https://mirror.example/tote/"}

	if got := m.listLocation(); got != "https://mirror.example/tote/releases.json" {
		t.Errorf("mirror list = %q", got)
	}

	if got := m.assetLocation("1.0.0", "x"); got != "https://mirror.example/tote/download/1.0.0/x" {
		t.Errorf("mirror asset = %q", got)
	}

	if !(Source{Base: "file:///x"}).Local() || gh.Local() || m.Local() {
		t.Error("Local is wrong")
	}
}

// tlsMirror serves a handler over TLS and returns a source that trusts it.
//
// Parameters:
//   - t: running test
//   - h: handler of the fake release server
func tlsMirror(t *testing.T, h http.Handler) (Source, *httptest.Server) {
	t.Helper()

	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)

	return Source{Base: srv.URL, Client: srv.Client()}, srv
}

// TestFetchHTTPErrors checks how HTTP statuses become errors.
//
// Parameters:
//   - t: test to report to
func TestFetchHTTPErrors(t *testing.T) {
	reset := time.Now().Add(time.Hour).Truncate(time.Second)

	tests := []struct {
		name   string
		status int
		header map[string]string
		want   error
		rate   bool
	}{
		{"not found", 404, nil, errMissing, false},
		{"gone", 410, nil, errMissing, false},
		{"server error", 500, nil, ErrServer, false},
		{"bad gateway", 502, nil, ErrServer, false},
		{"too many requests", 429, nil, ErrRateLimited, true},
		{"forbidden by rate limit", 403, map[string]string{
			"X-RateLimit-Remaining": "0",
			"X-RateLimit-Reset":     strconv.FormatInt(reset.Unix(), 10),
		}, ErrRateLimited, true},
		{"forbidden with retry-after", 403, map[string]string{"Retry-After": "30"}, ErrRateLimited, true},
		{"plain forbidden", 403, nil, ErrServer, false},
		{"unauthorized", 401, nil, ErrServer, false},
	}

	for _, tt := range tests {
		src, _ := tlsMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range tt.header {
				w.Header().Set(k, v)
			}

			w.WriteHeader(tt.status)
		}))

		_, err := src.fetch(context.Background(), src.listLocation(), 1024)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}

		var rl *RateLimitError
		if errors.As(err, &rl) != tt.rate {
			t.Errorf("%s: RateLimitError = %v, want %v", tt.name, rl, tt.rate)
		}

		if rl != nil && tt.header["X-RateLimit-Reset"] != "" && !rl.Reset.Equal(reset) {
			t.Errorf("%s: reset = %v, want %v", tt.name, rl.Reset, reset)
		}
	}
}

// TestFetchHTTPRequest checks the headers sent and a successful read over TLS.
//
// Parameters:
//   - t: test to report to
func TestFetchHTTPRequest(t *testing.T) {
	var agent, path string

	src, _ := tlsMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent, path = r.Header.Get("User-Agent"), r.URL.Path
		_, _ = w.Write([]byte("hello"))
	}))
	src.Agent = "tote/1.2.3"

	got, err := src.fetch(context.Background(), src.assetLocation("1.0.0", "f"), 100)
	if err != nil || string(got) != "hello" {
		t.Fatalf("fetch = %q, %v", got, err)
	}

	if agent != "tote/1.2.3" || path != "/download/1.0.0/f" {
		t.Errorf("agent %q, path %q", agent, path)
	}
}

// TestFetchLimit checks that a download beyond the limit is refused, over HTTP and from a file.
//
// Parameters:
//   - t: test to report to
func TestFetchLimit(t *testing.T) {
	src, _ := tlsMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))

	ctx := context.Background()

	if _, err := src.fetch(ctx, src.listLocation(), 99); !errors.Is(err, ErrTooLarge) {
		t.Errorf("HTTP: error %v, want ErrTooLarge", err)
	}

	if _, err := src.fetch(context.Background(), src.listLocation(), 100); err != nil {
		t.Errorf("HTTP at the limit: %v", err)
	}

	dir := t.TempDir()
	body := []byte(strings.Repeat("y", 50))
	if err := os.WriteFile(filepath.Join(dir, "releases.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	file := Source{Base: "file://" + dir}

	if _, err := file.fetch(ctx, file.listLocation(), 49); !errors.Is(err, ErrTooLarge) {
		t.Errorf("file: error %v, want ErrTooLarge", err)
	}
}

// TestFetchRedirects checks that redirects stay on https and are bounded.
//
// Parameters:
//   - t: test to report to
func TestFetchRedirects(t *testing.T) {
	plainHits := 0
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHits++

		_, _ = w.Write([]byte("secret"))
	}))
	t.Cleanup(plain.Close)

	var tlsURL string

	src, srv := tlsMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-http":
			http.Redirect(w, r, plain.URL, http.StatusFound)
		case "/loop":
			http.Redirect(w, r, tlsURL+"/loop", http.StatusFound)
		case "/ok":
			http.Redirect(w, r, tlsURL+"/final", http.StatusFound)
		default:
			_, _ = w.Write([]byte("final"))
		}
	}))
	tlsURL = srv.URL

	ctx := context.Background()

	if _, err := src.fetch(ctx, srv.URL+"/to-http", 100); !errors.Is(err, ErrInsecure) {
		t.Errorf("redirect to http: error %v, want ErrInsecure", err)
	}

	if plainHits != 0 {
		t.Error("the redirect to http was followed")
	}

	if _, err := src.fetch(ctx, srv.URL+"/loop", 100); !errors.Is(err, ErrServer) {
		t.Errorf("redirect loop: error %v, want ErrServer", err)
	}

	if got, err := src.fetch(ctx, srv.URL+"/ok", 100); err != nil || string(got) != "final" {
		t.Errorf("redirect to https: %q, %v", got, err)
	}
}

// TestFetchRefusesPlainHTTP checks that nothing is ever requested over http.
//
// Parameters:
//   - t: test to report to
func TestFetchRefusesPlainHTTP(t *testing.T) {
	hit := false
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	t.Cleanup(plain.Close)

	src := Source{Base: plain.URL}

	ctx := context.Background()

	if _, err := src.fetch(ctx, src.listLocation(), 100); !errors.Is(err, ErrInsecure) {
		t.Errorf("http base: error %v, want ErrInsecure", err)
	}

	if hit {
		t.Error("the plain http server was contacted")
	}

	if _, err := (Source{}).fetch(ctx, "http://example.org/x", 10); !errors.Is(err, ErrInsecure) {
		t.Errorf("http location: error %v, want ErrInsecure", err)
	}
}

// TestFetchOffline checks that an unreachable server is reported as offline.
//
// Parameters:
//   - t: test to report to
func TestFetchOffline(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	src := Source{Base: srv.URL, Client: srv.Client()}
	srv.Close()

	_, err := src.fetch(context.Background(), src.listLocation(), 100)
	if !errors.Is(err, ErrOffline) {
		t.Errorf("closed server: error %v, want ErrOffline", err)
	}
}

// TestFetchTimeoutAndCancel checks that a slow server is reported as offline and that a
// cancellation is reported as such.
//
// Parameters:
//   - t: test to report to
func TestFetchTimeoutAndCancel(t *testing.T) {
	release := make(chan struct{})
	src, _ := tlsMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := src.fetch(ctx, src.listLocation(), 100); !errors.Is(err, ErrOffline) {
		t.Errorf("timeout: error %v, want ErrOffline", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()

	if _, err := src.fetch(ctx2, src.listLocation(), 100); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: error %v, want context.Canceled", err)
	}
}

// TestFetchFile checks the file:// mirror: a file, a missing file, a directory and a bad URL.
//
// Parameters:
//   - t: test to report to
func TestFetchFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "releases.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}

	src := Source{Base: "file://" + dir}
	ctx := context.Background()

	if got, err := src.fetch(ctx, src.listLocation(), 10); err != nil || string(got) != "[]" {
		t.Errorf("read = %q, %v", got, err)
	}

	if _, err := src.fetch(ctx, src.assetLocation("1.0.0", "x"), 10); !errors.Is(err, errMissing) {
		t.Errorf("missing: error %v, want errMissing", err)
	}

	if _, err := src.fetch(ctx, "file://"+dir, 10); !errors.Is(err, ErrServer) {
		t.Errorf("directory: error %v, want ErrServer", err)
	}

	if _, err := src.fetch(ctx, "file://other-host/x", 10); !errors.Is(err, ErrInsecure) {
		t.Errorf("remote file URL: error %v, want ErrInsecure", err)
	}
}

// TestCodes checks the machine names of errors.
//
// Parameters:
//   - t: test to report to
func TestCodes(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrOffline, "offline"},
		{&RateLimitError{}, "rate_limited"},
		{ErrBadSignature, "bad_signature"},
		{ErrUnknownKey, "unknown_key"},
		{fmt.Errorf("wrapped: %w", ErrUnknownKey), "unknown_key"},
		{ErrLegacySignature, "bad_signature"},
		{ErrBadChecksum, "bad_checksum"},
		{ErrNoKey, "no_key"},
		{&NotWritableError{Dir: "/x"}, "not_writable"},
		{context.Canceled, "interrupted"},
		{errors.New("other"), "failed"},
	}

	for _, tt := range tests {
		if got := Code(tt.err); got != tt.want {
			t.Errorf("Code(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}

	if (&RateLimitError{}).Error() == "" || (&NotWritableError{Dir: "/x"}).Error() == "" {
		t.Error("empty error text")
	}
}
