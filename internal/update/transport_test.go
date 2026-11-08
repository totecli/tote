package update

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestReviewTestsDefaultTransportVerifiesCertificates checks the production client, the one
// built when the source has no Client: a server whose certificate is not trusted is refused
// before any request is served, so that nobody can serve a release list or an archive over a
// forged TLS endpoint.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsDefaultTransportVerifiesCertificates(t *testing.T) {
	var hits atomic.Int32

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("[]"))
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var b bytes.Buffer

	err := Source{Base: srv.URL}.fetchTo(ctx, srv.URL+"/releases.json", &b, 1<<10)
	if err == nil {
		t.Fatalf("an untrusted certificate was accepted, body %q", b.String())
	}

	if hits.Load() != 0 || b.Len() != 0 {
		t.Errorf("the server was reached (%d request, %d bytes)", hits.Load(), b.Len())
	}
}

// TestReviewTestsDefaultClientShape checks the settings of the production client: a TLS 1.2
// floor, bounded waits, and a redirect rule that refuses any scheme other than https.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsDefaultClientShape(t *testing.T) {
	c := Source{}.httpClient()

	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T", c.Transport)
	}

	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion < 0x0303 {
		t.Errorf("TLS config = %+v, want a TLS 1.2 floor", tr.TLSClientConfig)
	}

	if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("certificate verification is off")
	}

	if tr.ResponseHeaderTimeout <= 0 || tr.DialContext == nil {
		t.Error("the transport has no header timeout or dial timeout")
	}

	if c.CheckRedirect == nil {
		t.Fatal("no redirect policy")
	}
}

// TestReviewTestsArchiveCutShort checks an archive download that dies half way: the failure is
// reported as an offline error, not as a bad checksum or a success, the old binary stays and the
// staging directory is removed.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsArchiveCutShort(t *testing.T) {
	const tag = "1.1.0"

	m := newMirror(t, newSigner(1))
	m.publish(fakeRelease{Tag: tag})

	archive := ArchiveName(tag, "linux", "amd64")

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(filepath.Join(m.dir, filepath.FromSlash(r.URL.Path)))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if strings.HasSuffix(r.URL.Path, archive) {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			data = data[:len(data)/2]
		}

		_, _ = w.Write(data)
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	u, target := m.updater("1.0.0")
	u.Source = Source{Base: srv.URL, Client: srv.Client()}

	_, err := u.Apply(context.Background())
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("error = %v, want ErrOffline", err)
	}

	if content(t, target) != fakeBinary("1.0.0") {
		t.Error("the binary changed")
	}

	if got := names(t, filepath.Dir(target)); !slices.Equal(got, []string{"tote"}) {
		t.Errorf("directory holds %v", got)
	}
}

// TestReviewTestsRedirectLoop checks that a server which redirects to itself for ever is cut off
// after a few hops with a server error, instead of keeping the update waiting.
//
// Parameters:
//   - t: test to report to
func TestReviewTestsRedirectLoop(t *testing.T) {
	var hops atomic.Int32

	src, _ := tlsMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops.Add(1)
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := src.fetch(ctx, src.listLocation(), 100)
	if !errors.Is(err, ErrServer) {
		t.Errorf("error = %v, want ErrServer", err)
	}

	if n := hops.Load(); n > 2*maxRedirects {
		t.Errorf("%d requests were made for a limit of %d redirects", n, maxRedirects)
	}
}
