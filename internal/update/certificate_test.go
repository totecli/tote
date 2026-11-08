package update

import (
	"crypto/x509"
	"errors"
	"net/url"
	"testing"
)

// TestReviewCertificateFailureIsNotOffline pins that a failed TLS certificate check is not
// reported as "are you offline?". Every *url.Error is a net.Error, so classifyTransport
// currently maps an untrusted or expired certificate, which may be an attack, to ErrOffline.
//
// Parameters:
//   - t: test to report to
func TestReviewCertificateFailureIsNotOffline(t *testing.T) {
	for _, inner := range []error{x509.UnknownAuthorityError{}, x509.CertificateInvalidError{
		Reason: x509.Expired,
	}} {
		err := &url.Error{Op: "Get", URL: "https://api.github.com/", Err: inner}

		if got := classifyTransport(err); errors.Is(got, ErrOffline) {
			t.Errorf("%T classed as offline: %v", inner, got)
		}
	}
}
