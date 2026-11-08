package update

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// Repo is the GitHub repository that publishes the releases.
	Repo = "totecli/tote"

	// listURL is the GitHub API list of the releases of the repository, newest first.
	listURL = "https://api.github.com/repos/" + Repo + "/releases?per_page=50"

	// downloadBase is the root of the release files on GitHub: <root>/download/<tag>/<file>.
	downloadBase = "https://github.com/" + Repo + "/releases"

	// maxRedirects is the most redirects followed for one request.
	maxRedirects = 5

	// headerTimeout is how long the server may take to start answering.
	headerTimeout = 30 * time.Second

	// dialTimeout is how long a connection may take to open.
	dialTimeout = 15 * time.Second

	// idleTimeout is how long a download may go without receiving a byte.
	idleTimeout = 30 * time.Second

	// maxListSize is the largest release list read, in bytes.
	maxListSize = 4 << 20

	// maxSmallSize is the largest checksums or signature file read, in bytes.
	maxSmallSize = 1 << 20

	// maxArchiveSize is the largest release archive downloaded, in bytes.
	maxArchiveSize = 256 << 20
)

// Source says where the releases come from.
//
// An empty Base means GitHub: the list of releases comes from the API and the files from the
// release downloads, over https only. A Base starting with https:// or file:// is a mirror that
// serves the list as <Base>/releases.json, in the format of the GitHub API, and the files as
// <Base>/download/<tag>/<file>; the file:// form is for tests and offline mirrors. Plain http
// is never accepted, nor a redirect to it. Client, when set, only lends its transport, so that
// tests can trust a local certificate; its redirect and timeout rules are replaced. Agent is
// the User-Agent header, "tote" when empty. A download that receives no byte for Idle is
// abandoned as offline, 30 seconds when zero.
type Source struct {
	Base   string
	Client *http.Client
	Agent  string
	Idle   time.Duration
}

// Validate checks that the source uses an allowed scheme.
//
// Returns nil for GitHub, an https:// mirror or a file:// mirror, else an error wrapping
// ErrInsecure.
func (s Source) Validate() error {
	if s.Base == "" {
		return nil
	}

	bad := &InsecureError{URL: redact(s.Base)}

	u, err := url.Parse(s.Base)
	if err != nil {
		return bad
	}

	switch u.Scheme {
	case "https":
		if u.Host == "" {
			return bad
		}
	case "file":
		if u.Host != "" || !strings.HasPrefix(u.Path, "/") {
			return bad
		}
	default:
		return bad
	}

	return nil
}

// redact returns an address without the credentials it may carry, which must never be printed
// or recorded.
//
// Parameters:
//   - raw: address as typed; text that is not a URL is returned as is
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}

	u.User = nil

	return u.String()
}

// Local reports whether the source is a file:// mirror.
func (s Source) Local() bool {
	return strings.HasPrefix(s.Base, "file://")
}

// listLocation returns where the list of releases is read.
func (s Source) listLocation() string {
	if s.Base == "" {
		return listURL
	}

	return strings.TrimRight(s.Base, "/") + "/releases.json"
}

// assetLocation returns where a release file is read.
//
// Parameters:
//   - tag: release tag, a strict version
//   - name: file name built by this package
func (s Source) assetLocation(tag, name string) string {
	base := downloadBase
	if s.Base != "" {
		base = strings.TrimRight(s.Base, "/")
	}

	return base + "/download/" + tag + "/" + name
}

// agent returns the User-Agent header.
func (s Source) agent() string {
	if s.Agent != "" {
		return s.Agent
	}

	return "tote"
}

// httpClient builds the client used for one request.
//
// Returns a client that follows a few redirects, all of them to https, with a TLS 1.2 floor
// when the default transport is used.
func (s Source) httpClient() *http.Client {
	var rt http.RoundTripper

	if s.Client != nil {
		rt = s.Client.Transport
	}

	if rt == nil {
		tr, ok := http.DefaultTransport.(*http.Transport)
		if ok {
			tr = tr.Clone()
		} else {
			tr = &http.Transport{}
		}

		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		tr.ResponseHeaderTimeout = headerTimeout
		tr.DialContext = (&net.Dialer{Timeout: dialTimeout}).DialContext
		rt = tr
	}

	return &http.Client{Transport: rt, CheckRedirect: httpsRedirect}
}

// httpsRedirect is the redirect policy: a few hops, never to a scheme other than https.
//
// Parameters:
//   - req: the request about to be sent
//   - via: the requests already made
func httpsRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("%w: too many redirects", ErrServer)
	}

	if req.URL.Scheme != "https" {
		return &InsecureError{URL: redact(req.URL.String()), Redirect: true}
	}

	return nil
}

// fetch reads one location, a URL of the source, into memory.
//
// Parameters:
//   - ctx: cancels the transfer
//   - loc: location built by listLocation or assetLocation
//   - limit: most bytes accepted
//
// Returns the content, or an error wrapping errMissing for a file that does not exist, or
// one of the errors of this package.
func (s Source) fetch(ctx context.Context, loc string, limit int64) ([]byte, error) {
	var b bytes.Buffer

	if err := s.fetchTo(ctx, loc, &b, limit); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// fetchTo copies one location to a writer.
//
// Parameters:
//   - ctx: cancels the transfer
//   - loc: location built by listLocation or assetLocation
//   - w: destination
//   - limit: most bytes accepted, beyond which ErrTooLarge is returned
//
// Returns nil, an error wrapping errMissing for a missing file, or one of the errors of this
// package.
func (s Source) fetchTo(ctx context.Context, loc string, w io.Writer, limit int64) error {
	if err := s.Validate(); err != nil {
		return err
	}

	if strings.HasPrefix(loc, "file://") {
		return fetchFile(ctx, loc, w, limit)
	}

	return s.fetchHTTP(ctx, loc, w, limit)
}

// fetchHTTP copies one https location to a writer.
//
// Parameters:
//   - ctx: cancels the transfer
//   - loc: https URL
//   - w: destination
//   - limit: most bytes accepted
func (s Source) fetchHTTP(ctx context.Context, loc string, w io.Writer, limit int64) error {
	if !strings.HasPrefix(loc, "https://") {
		return &InsecureError{URL: redact(loc)}
	}

	// The request ends when the caller cancels or when the body stays silent for too long.
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, loc, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrServer, err)
	}

	req.Header.Set("User-Agent", s.agent())
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return classifyTransport(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.Request != nil && resp.Request.URL.Scheme != "https" {
		return &InsecureError{URL: redact(resp.Request.URL.String()), Redirect: true}
	}

	if err := statusError(resp); err != nil {
		return err
	}

	idle := s.Idle
	if idle <= 0 {
		idle = idleTimeout
	}

	timer := time.AfterFunc(idle, cancel)
	defer timer.Stop()

	return copyLimited(ctx, w, &idleReader{r: resp.Body, timer: timer, idle: idle}, limit)
}

// idleReader restarts a timer after each read, so that the timer only fires on a stalled body.
type idleReader struct {
	r     io.Reader
	timer *time.Timer
	idle  time.Duration
}

// Read implements io.Reader.
//
// Parameters:
//   - p: buffer to fill
func (i *idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if n > 0 {
		i.timer.Reset(i.idle)
	}

	return n, err
}

// classifyTransport turns a failed request into one of the errors of this package.
//
// Parameters:
//   - err: error returned by http.Client.Do
func classifyTransport(err error) error {
	var (
		netErr  net.Error
		insec   *InsecureError
		unknown x509.UnknownAuthorityError
		invalid x509.CertificateInvalidError
		host    x509.HostnameError
		verify  *tls.CertificateVerificationError
	)

	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.As(err, &insec):
		return insec
	case errors.Is(err, ErrInsecure), errors.Is(err, ErrServer):
		return err
	case errors.As(err, &unknown), errors.As(err, &invalid), errors.As(err, &host),
		errors.As(err, &verify):
		return fmt.Errorf("%w: %v", ErrCertificate, err)
	case errors.As(err, &netErr), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %v", ErrOffline, err)
	}

	return fmt.Errorf("%w: %v", ErrServer, err)
}

// statusError turns the status of a response into an error.
//
// Parameters:
//   - resp: response whose status is read
//
// Returns nil for 200, an error wrapping errMissing for 404 and 410, a *RateLimitError for a
// refusal by rate limit, and an error wrapping ErrServer for any other status.
func statusError(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusGone:
		return errMissing
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && limited(resp.Header):
		return &RateLimitError{Reset: resetTime(resp.Header)}
	}

	return fmt.Errorf("%w: HTTP %d", ErrServer, resp.StatusCode)
}

// limited reports whether the headers of a refusal say that the rate limit is used up.
//
// Parameters:
//   - h: response headers
func limited(h http.Header) bool {
	return h.Get("X-RateLimit-Remaining") == "0" || h.Get("Retry-After") != ""
}

// resetTime reads when a rate limit ends.
//
// Parameters:
//   - h: response headers, X-RateLimit-Reset being a Unix time and Retry-After a delay in
//     seconds
//
// Returns the time, or the zero time when the headers do not say.
func resetTime(h http.Header) time.Time {
	if n, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0)
	}

	if n, err := strconv.ParseInt(h.Get("Retry-After"), 10, 64); err == nil && n > 0 {
		return time.Now().Add(time.Duration(n) * time.Second)
	}

	return time.Time{}
}

// fetchFile copies a file:// location to a writer.
//
// Parameters:
//   - ctx: cancels the transfer
//   - loc: file:// URL with an absolute path
//   - w: destination
//   - limit: most bytes accepted
func fetchFile(ctx context.Context, loc string, w io.Writer, limit int64) error {
	u, err := url.Parse(loc)
	if err != nil || u.Host != "" || u.Path == "" {
		return &InsecureError{URL: redact(loc)}
	}

	f, err := os.Open(u.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errMissing
		}

		return fmt.Errorf("%w: %v", ErrServer, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrServer, u.Path)
	}

	return copyLimited(ctx, w, f, limit)
}

// copyLimited copies a stream, stopping on a cancellation or an excess.
//
// Parameters:
//   - ctx: cancels the copy between two reads
//   - w: destination
//   - r: source
//   - limit: most bytes accepted
//
// Returns nil, the error of the copy, the error of the context, or ErrTooLarge.
func copyLimited(ctx context.Context, w io.Writer, r io.Reader, limit int64) error {
	n, err := io.Copy(w, &ctxReader{ctx: ctx, r: io.LimitReader(r, limit+1)})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return context.Canceled
		}

		return classifyRead(err)
	}

	if n > limit {
		return fmt.Errorf("%w: more than %d bytes", ErrTooLarge, limit)
	}

	return nil
}

// classifyRead maps an error met while reading a body.
//
// Parameters:
//   - err: error of the read or of the write
func classifyRead(err error) error {
	var netErr net.Error

	if errors.As(err, &netErr) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %v", ErrOffline, err)
	}

	return err
}

// ctxReader fails a read once its context is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

// Read implements io.Reader.
//
// Parameters:
//   - p: buffer to fill
func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}

	return c.r.Read(p)
}
