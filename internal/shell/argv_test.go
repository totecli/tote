package shell

import (
	"errors"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// TestReviewConnectArgvBrackets checks which bracketed targets lose their brackets: only a whole
// IPv6 address does.
//
// Parameters:
//   - t: test to report to
func TestReviewConnectArgvBrackets(t *testing.T) {
	runArgvCases(t, []argvCase{
		{
			"bracketed ipv4 stays",
			store.Connection{Name: "n", Target: "[10.0.0.1]"},
			"",
			[]string{"ssh", "--", "[10.0.0.1]"},
		},
		{
			"unclosed bracket stays",
			store.Connection{Name: "n", Target: "[::1"},
			"",
			[]string{"ssh", "--", "[::1"},
		},
		{
			"closing bracket only stays",
			store.Connection{Name: "n", Target: "a::1]"},
			"",
			[]string{"ssh", "--", "a::1]"},
		},
		{
			"bracketed zone",
			store.Connection{Name: "n", Target: "[fe80::1%eth0]"},
			"",
			[]string{"ssh", "--", "fe80::1%eth0"},
		},
		{
			"empty brackets stay",
			store.Connection{Name: "n", Target: "[]"},
			"",
			[]string{"ssh", "--", "[]"},
		},
		{
			"ipv4 mapped in brackets",
			store.Connection{Name: "n", Target: "[::ffff:10.0.0.1]"},
			"",
			[]string{"ssh", "--", "::ffff:10.0.0.1"},
		},
	})
}

// TestReviewConnectArgvHostileFields checks that no stored or typed field can become an option
// of the program that is started.
//
// Parameters:
//   - t: test to report to
func TestReviewConnectArgvHostileFields(t *testing.T) {
	cases := []struct {
		name string
		c    store.Connection
		user string
	}{
		{"target is an option", store.Connection{Name: "n", Target: "-oProxyCommand=x"}, ""},
		{"user override is an option", store.Connection{Name: "n", Target: "h"}, "-oProxyCommand=x"},
		{"stored user is an option", store.Connection{Name: "n", Target: "h", User: "-x"}, ""},
		{"verb target is an option", store.Connection{Name: "n", Via: "mosh", Target: "-x"}, ""},
		{"verb user is an option", store.Connection{Name: "n", Via: "mosh", Target: "h"}, "-x"},
		{"target with newline", store.Connection{Name: "n", Target: "h\n-x"}, ""},
		{"target with space", store.Connection{Name: "n", Target: "h -x"}, ""},
		{"user with at sign", store.Connection{Name: "n", Target: "h"}, "a@b"},
		{"user with space", store.Connection{Name: "n", Target: "h"}, "a b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argv, err := ConnectArgv(tc.c, tc.user)
			if err == nil {
				t.Errorf("accepted, argv %q", argv)
			}

			if argv != nil {
				t.Errorf("argv %q returned with an error", argv)
			}
		})
	}
}

// TestReviewConnectArgvNeedsReviewWins checks that the review flag is refused before any other
// consideration, including a user override that would otherwise be accepted.
//
// Parameters:
//   - t: test to report to
func TestReviewConnectArgvNeedsReviewWins(t *testing.T) {
	c := store.Connection{Name: "n", Target: "h", NeedsReview: true}

	for _, user := range []string{"", "root"} {
		argv, err := ConnectArgv(c, user)
		if !errors.Is(err, ErrNeedsReview) || argv != nil {
			t.Errorf("user %q: argv %q, err %v, want ErrNeedsReview", user, argv, err)
		}
	}
}
