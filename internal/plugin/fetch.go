package plugin

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Codes of a download. They are the net_* rows of the failure matrix of the security draft, plus
// origin_redirect for a repository that moved and net_truncated for a body shorter than the
// server announced.
const (
	CodeNetOffline     = "net_offline"
	CodeNetTimeout     = "net_timeout"
	CodeNetStatus      = "net_status"
	CodeNetTLS         = "net_tls"
	CodeNetRedirect    = "net_redirect"
	CodeNetTruncated   = "net_truncated"
	CodeTooLarge       = "too_large"
	CodeOriginRedirect = "origin_redirect"
)

// RoundTripper is the transport of the downloads. Options.Transport injects one, so that tests
// run against httptest; nil gives the transport of section 7.2.
type RoundTripper = http.RoundTripper

// maxRedirects is the number of redirects a download follows.
const maxRedirects = 3

// redirectHosts are the hosts a redirect may land on. No variable of the environment changes the
// list.
var redirectHosts = []string{
	releaseHost, "objects.githubusercontent.com",
	"release-assets.githubusercontent.com",
}

// maxHeaderBytes is the largest block of response headers that is read, far above what GitHub
// sends.
const maxHeaderBytes = 64 << 10

// netTimeouts are the timeouts of a download: connect and TLS, response headers, idle read and
// the total. It is a variable so that tests can shorten them.
type netTimeouts struct {
	connect time.Duration
	headers time.Duration
	idle    time.Duration
	total   time.Duration
}

// timeouts is the value used by every download.
var timeouts = netTimeouts{
	connect: 10 * time.Second,
	headers: 20 * time.Second,
	idle:    30 * time.Second,
	total:   5 * time.Minute,
}

// errStalled and errTotal are the causes with which a download cancels its own context.
var (
	errStalled = errors.New("download stalled")
	errTotal   = errors.New("download took too long")
)

// newTransport builds the transport of a download: TLS 1.2 or later with the system roots, the
// proxy of the environment, no transparent decoding (the bytes that are hashed are the bytes of
// the asset) and the timeouts of section 7.2.
//
// Returns the transport, which the caller owns.
func newTransport() *http.Transport {
	d := &net.Dialer{Timeout: timeouts.connect}

	return &http.Transport{
		Proxy:                  http.ProxyFromEnvironment,
		DialContext:            d.DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    timeouts.connect,
		ResponseHeaderTimeout:  timeouts.headers,
		MaxResponseHeaderBytes: maxHeaderBytes,
		DisableCompression:     true,
		DisableKeepAlives:      true,
	}
}

// fetcher downloads release assets.
type fetcher struct {
	rt    http.RoundTripper
	agent string
	t     netTimeouts
}

// fetcher builds a fetcher over the transport of the registry.
//
// Returns the fetcher, over the transport of the options or the default one of newTransport.
func (r *Registry) fetcher() *fetcher {
	rt := r.opts.Transport
	if rt == nil {
		rt = newTransport()
	}

	return &fetcher{rt: rt, agent: userAgent(r.opts.Host.Version), t: timeouts}
}

// userAgent builds the User-Agent header: tote/<version>, where the version keeps only characters
// that cannot break a header.
//
// Parameters:
//   - version: version of the running tote
func userAgent(version string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r == '.', r == '+', r == '-', r == '_':
			return r
		}

		return -1
	}, version)

	if clean == "" {
		clean = "dev"
	}

	return "tote/" + clean
}

// sizeText writes a size limit for a message.
//
// Parameters:
//   - n: number of bytes
func sizeText(n int64) string {
	if n >= 1<<20 {
		return strconv.FormatInt(n>>20, 10) + " MiB"
	}

	if n >= 1<<10 {
		return strconv.FormatInt(n>>10, 10) + " KiB"
	}

	return strconv.FormatInt(n, 10) + " B"
}

// download gets an asset and writes it to w while hashing it.
//
// Only https is spoken. At most three redirects are followed, each to https on github.com,
// objects.githubusercontent.com or release-assets.githubusercontent.com; a redirect to another
// owner or repository of github.com is origin_redirect. No credential is ever sent: no
// Authorization header, no cookie, so there is none to leak across hosts. Only status 200 is
// accepted. A Content-Length above the limit stops the download before the body is read, and a
// counting reader stops one without it. The connection stalls after the idle time and the whole
// download after the total time.
//
// Parameters:
//   - ctx: cancels the download
//   - ref: the asset
//   - limit: largest body in bytes
//   - w: receives the body
//
// Returns the sha256 of the body as received and its size, or an *Error: bad_source, net_offline,
// net_timeout, net_status, net_tls, net_redirect, origin_redirect, net_truncated, too_large, disk.
func (f *fetcher) download(parent context.Context, ref ReleaseRef, limit int64,
	w io.Writer,
) (string, int64, error) {
	// The address is built from parts that a manifest can name (the asset of a platform), so it
	// goes through the grammar of a release URL again before anything is sent.
	if _, err := ParseRelease(ref.URL()); err != nil {
		return "", 0, err
	}

	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)

	total := time.AfterFunc(f.t.total, func() { cancel(errTotal) })
	defer total.Stop()

	stall := time.AfterFunc(f.t.headers, func() { cancel(errStalled) })
	defer stall.Stop()

	cur, err := url.Parse(ref.URL())
	if err != nil {
		return "", 0, wrapError(CodeBadSource, err, ref.URL())
	}

	for hop := 0; ; hop++ {
		resp, err := f.get(ctx, cur)
		if err != nil {
			return "", 0, failure(ctx, parent, cur, err)
		}

		if !isRedirect(resp.StatusCode) {
			return f.read(ctx, parent, cur, resp, stall, limit, w)
		}

		next, err := f.follow(cur, resp, ref, hop)
		_ = resp.Body.Close()

		if err != nil {
			return "", 0, err
		}

		cur = next

		stall.Reset(f.t.headers)
	}
}

// get sends one request straight through the transport, so that no redirect is followed or
// even parsed by the standard client: where a redirect leads is decided by follow alone. It
// carries the User-Agent and nothing that identifies the user.
//
// Parameters:
//   - ctx: cancels the request
//   - u: address to get
func (f *fetcher) get(ctx context.Context, u *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", f.agent)
	req.Header.Set("Accept", "application/octet-stream")

	for _, h := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
		req.Header.Del(h)
	}

	return f.rt.RoundTrip(req)
}

// isRedirect reports whether a status asks the client to go elsewhere.
//
// Parameters:
//   - code: HTTP status
func isRedirect(code int) bool {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}

	return false
}

// follow checks where a redirect leads.
//
// Parameters:
//   - cur: address that answered
//   - resp: the redirect
//   - ref: the asset that was asked
//   - hop: number of redirects already followed
func (f *fetcher) follow(cur *url.URL, resp *http.Response, ref ReleaseRef, hop int) (*url.URL,
	error,
) {
	loc := resp.Header.Get("Location")

	if hop >= maxRedirects || loc == "" {
		return nil, newError(CodeNetRedirect, cur.Host)
	}

	next, err := cur.Parse(loc)
	if err != nil {
		return nil, newError(CodeNetRedirect, cur.Host)
	}

	if err := allowed(next, ref); err != nil {
		return nil, err
	}

	return next, nil
}

// allowed applies the rules of a redirect target: https, no user information, the default port
// and a host of the list. On github.com the target must be the same asset of the same release;
// a repository that moved gives origin_redirect, and the user installs again from the new URL.
//
// Parameters:
//   - next: the target
//   - ref: the asset that was asked
func allowed(next *url.URL, ref ReleaseRef) error {
	refuse := newError(CodeNetRedirect, next.Host)

	host := strings.ToLower(next.Hostname())

	if next.Scheme != "https" || next.User != nil || host == "" ||
		(next.Port() != "" && next.Port() != "443") || strings.HasSuffix(next.Host, ":") {
		return refuse
	}

	listed := false

	for _, h := range redirectHosts {
		listed = listed || host == h
	}

	if !listed {
		return refuse
	}

	if host != releaseHost {
		return nil
	}

	landed, err := ParseRelease("https://" + releaseHost + next.EscapedPath())
	if err != nil || next.RawQuery != "" {
		return refuse
	}

	if landed.Origin() != ref.Origin() {
		return newError(CodeOriginRedirect, ref.Origin(), landed.Origin())
	}

	if landed.Tag != ref.Tag || landed.Asset != ref.Asset {
		return refuse
	}

	return nil
}

// read checks the answer and copies its body.
//
// Parameters:
//   - ctx: context of the download, canceled by its timers
//   - parent: context of the caller
//   - cur: address that answered
//   - resp: the answer, closed here
//   - stall: timer that cancels a download that stops
//   - limit: largest body in bytes
//   - w: receives the body
func (f *fetcher) read(ctx, parent context.Context, cur *url.URL, resp *http.Response,
	stall *time.Timer, limit int64, w io.Writer,
) (string, int64, error) {
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", 0, newError(CodeNetStatus, cur.Host, strconv.Itoa(resp.StatusCode))
	}

	tooLarge := newError(CodeTooLarge, sizeText(limit))

	if resp.ContentLength > limit {
		return "", 0, tooLarge
	}

	h := sha256.New()
	buf := make([]byte, 32<<10)

	// The wait for the headers is over: from here on the connection may be silent for the idle
	// time between two reads, whatever remains of the time for the headers.
	stall.Reset(f.t.idle)

	var n int64

	for {
		nr, rerr := resp.Body.Read(buf)

		if nr > 0 {
			stall.Reset(f.t.idle)

			n += int64(nr)
			if n > limit {
				return "", 0, tooLarge
			}

			_, _ = h.Write(buf[:nr])

			if _, werr := w.Write(buf[:nr]); werr != nil {
				return "", 0, wrapError(CodeDisk, werr, cur.Host)
			}
		}

		if errors.Is(rerr, io.EOF) {
			// A timer that fired cuts the connection, and the cut can read as a clean end of the
			// body: that is a timeout, not a complete download.
			if context.Cause(ctx) != nil {
				return "", 0, failure(ctx, parent, cur, rerr)
			}

			break
		}

		if rerr != nil {
			if errors.Is(rerr, io.ErrUnexpectedEOF) && context.Cause(ctx) == nil {
				return "", 0, newError(CodeNetTruncated, cur.Host)
			}

			return "", 0, failure(ctx, parent, cur, rerr)
		}
	}

	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return "", 0, newError(CodeNetTruncated, cur.Host)
	}

	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// failure turns a transport error into a code.
//
// Parameters:
//   - ctx: context of the download
//   - parent: context of the caller, whose cancellation is returned as it is
//   - cur: address that was asked
//   - err: the error
func failure(ctx, parent context.Context, cur *url.URL, err error) error {
	cause := context.Cause(ctx)

	switch {
	case errors.Is(cause, errStalled), errors.Is(cause, errTotal):
		return wrapError(CodeNetTimeout, cause, cur.Host)
	case parent.Err() != nil:
		return parent.Err()
	}

	var ne net.Error

	switch {
	case isTLSFailure(err):
		return wrapError(CodeNetTLS, err, cur.Host)
	case errors.As(err, &ne) && ne.Timeout():
		return wrapError(CodeNetTimeout, err, cur.Host)
	}

	return wrapError(CodeNetOffline, err, cur.Host)
}

// isTLSFailure reports whether an error comes from the TLS handshake or the certificate.
//
// Parameters:
//   - err: the error
func isTLSFailure(err error) bool {
	var (
		verify *tls.CertificateVerificationError
		record tls.RecordHeaderError
		host   x509.HostnameError
		auth   x509.UnknownAuthorityError
		inval  x509.CertificateInvalidError
	)

	if errors.As(err, &verify) || errors.As(err, &record) || errors.As(err, &host) ||
		errors.As(err, &auth) || errors.As(err, &inval) {
		return true
	}

	text := err.Error()

	return strings.Contains(text, "tls:") || strings.Contains(text, "x509:")
}
