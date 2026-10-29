package shell

import (
	"errors"
	"reflect"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// argvCase is one row of the ConnectArgv tables.
type argvCase struct {
	name string
	c    store.Connection
	user string
	want []string
}

// runArgvCases runs a table of ConnectArgv calls that must all succeed.
//
// Parameters:
//   - t: test to report to
//   - cases: rows with the expected argument vector
func runArgvCases(t *testing.T, cases []argvCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConnectArgv(tc.c, tc.user)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("argv = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConnectArgvSSH checks the plain ssh form.
//
// Parameters:
//   - t: test to report to
func TestConnectArgvSSH(t *testing.T) {
	runArgvCases(t, []argvCase{
		{
			"empty via",
			store.Connection{Name: "web", Target: "web.example.com"},
			"",
			[]string{"ssh", "--", "web.example.com"},
		},
		{
			"explicit ssh",
			store.Connection{Name: "web", Via: "ssh", Target: "h"},
			"",
			[]string{"ssh", "--", "h"},
		},
		{
			"stored user",
			store.Connection{Name: "web", Target: "h", User: "bob"},
			"",
			[]string{"ssh", "-l", "bob", "--", "h"},
		},
		{
			"override wins",
			store.Connection{Name: "web", Target: "h", User: "bob"},
			"alice",
			[]string{"ssh", "-l", "alice", "--", "h"},
		},
		{
			"override without stored user",
			store.Connection{Name: "web", Target: "h"},
			"root",
			[]string{"ssh", "-l", "root", "--", "h"},
		},
		{
			"empty target uses name",
			store.Connection{Name: "web-1"},
			"",
			[]string{"ssh", "--", "web-1"},
		},
		{
			"ipv4 target",
			store.Connection{Name: "n", Target: "10.0.0.1"},
			"",
			[]string{"ssh", "--", "10.0.0.1"},
		},
		{
			"ipv6 target",
			store.Connection{Name: "n", Target: "2001:db8::1"},
			"",
			[]string{"ssh", "--", "2001:db8::1"},
		},
		{
			"ipv6 link local",
			store.Connection{Name: "n", Target: "fe80::1"},
			"",
			[]string{"ssh", "--", "fe80::1"},
		},
		{
			"ipv6 in brackets",
			store.Connection{Name: "n", Target: "[2001:db8::1]"},
			"",
			[]string{"ssh", "--", "2001:db8::1"},
		},
		{
			"brackets around a name stay",
			store.Connection{Name: "n", Target: "[web]"},
			"",
			[]string{"ssh", "--", "[web]"},
		},
		{
			"bracketed address with a port stays",
			store.Connection{Name: "n", Target: "[::1]:22"},
			"",
			[]string{"ssh", "--", "[::1]:22"},
		},
		{
			"target with user part",
			store.Connection{Name: "n", Target: "bob@h"},
			"",
			[]string{"ssh", "--", "bob@h"},
		},
		{
			"ip is never used",
			store.Connection{Name: "n", Target: "h", IP: "192.0.2.9"},
			"",
			[]string{"ssh", "--", "h"},
		},
	})
}

// TestConnectArgvVerb checks a via made of words without placeholders.
//
// Parameters:
//   - t: test to report to
func TestConnectArgvVerb(t *testing.T) {
	runArgvCases(t, []argvCase{
		{
			"bare verb",
			store.Connection{Name: "n", Via: "mosh", Target: "h"},
			"",
			[]string{"mosh", "h"},
		},
		{
			"verb with user",
			store.Connection{Name: "n", Via: "mosh", Target: "h", User: "bob"},
			"",
			[]string{"mosh", "bob@h"},
		},
		{
			"verb with override",
			store.Connection{Name: "n", Via: "mosh", Target: "h"},
			"amy",
			[]string{"mosh", "amy@h"},
		},
		{
			"verb with words",
			store.Connection{Name: "n", Via: "mosh --ssh=ssh", Target: "h"},
			"",
			[]string{"mosh", "--ssh=ssh", "h"},
		},
		{
			"verb with words and stored user",
			store.Connection{Name: "n", Via: "ssh -p 2222", Target: "h", User: "u"},
			"",
			[]string{"ssh", "-p", "2222", "u@h"},
		},
		{
			"path verb",
			store.Connection{Name: "n", Via: "tools/go", Target: "h"},
			"",
			[]string{"tools/go", "h"},
		},
		{
			"empty target uses name",
			store.Connection{Name: "n", Via: "telnet"},
			"",
			[]string{"telnet", "n"},
		},
		{
			"ipv6",
			store.Connection{Name: "n", Via: "telnet", Target: "fe80::2"},
			"",
			[]string{"telnet", "fe80::2"},
		},
		{
			"brackets stay for a verb",
			store.Connection{Name: "n", Via: "telnet", Target: "[::1]"},
			"",
			[]string{"telnet", "[::1]"},
		},
	})
}

// TestConnectArgvTemplate checks the placeholders of a template.
//
// Parameters:
//   - t: test to report to
func TestConnectArgvTemplate(t *testing.T) {
	full := store.Connection{
		Name: "db", Via: "x {user@} {user} {host} {ip} {name}", Target: "db.example.com",
		User: "bob", IP: "192.0.2.7",
	}

	runArgvCases(t, []argvCase{
		{
			"every placeholder", full, "",
			[]string{"x", "bob@", "bob", "db.example.com", "192.0.2.7", "db"},
		},
		{
			"override replaces user", full, "amy",
			[]string{"x", "amy@", "amy", "db.example.com", "192.0.2.7", "db"},
		},
		{
			"nothing appended",
			store.Connection{Name: "n", Via: "echo hi {name}", Target: "h"},
			"",
			[]string{"echo", "hi", "n"},
		},
		{"user at host", store.Connection{
			Name: "n", Via: "ssh {user@}{host}", Target: "h",
			User: "u",
		}, "", []string{"ssh", "u@h"}},
		{
			"no user",
			store.Connection{Name: "n", Via: "ssh {user@}{host}", Target: "h"},
			"",
			[]string{"ssh", "h"},
		},
		{"standalone user at dropped", store.Connection{
			Name: "n", Via: "tool {user@} {host}",
			Target: "h",
		}, "", []string{"tool", "h"}},
		{"login flag", store.Connection{
			Name: "n", Via: "ssh -l {user} {host}", Target: "h",
			User: "u",
		}, "", []string{"ssh", "-l", "u", "h"}},
		{"embedded in an option", store.Connection{
			Name: "n", Via: "ssh -o User={user} {host}",
			Target: "h", User: "u",
		}, "", []string{"ssh", "-o", "User=u", "h"}},
		{"host empty falls back to name", store.Connection{
			Name: "n1",
			Via:  "tool {host} {name}",
		}, "", []string{"tool", "n1", "n1"}},
		{
			"template without host and target",
			store.Connection{Name: "n1", Via: "tool {name}"},
			"",
			[]string{"tool", "n1"},
		},
		{
			"ip alone",
			store.Connection{Name: "n", Via: "ping {ip}", IP: "2001:db8::5"},
			"",
			[]string{"ping", "2001:db8::5"},
		},
		{
			"value with braces is not expanded again",
			store.Connection{Name: "n", Via: "tool {host}", Target: "{name}"},
			"",
			[]string{"tool", "{name}"},
		},
		{"repeated placeholder", store.Connection{
			Name: "n", Via: "tool {host}:{host}",
			Target: "h",
		}, "", []string{"tool", "h:h"}},
		{"stored user ignored without placeholder", store.Connection{
			Name: "n", Via: "tool {host}",
			Target: "h", User: "u",
		}, "", []string{"tool", "h"}},
		{"ipv6 host kept verbatim", store.Connection{
			Name: "n", Via: "tool {host}",
			Target: "[::1]",
		}, "", []string{"tool", "[::1]"}},
	})
}

// refusedCase is one row of the table of connections that ConnectArgv must refuse.
type refusedCase struct {
	name string
	c    store.Connection
	user string
	is   error
}

// checkRefused runs one refused case.
//
// Parameters:
//   - t: test to report to
//   - tc: connection, user override and the error it must wrap
func checkRefused(t *testing.T, tc refusedCase) {
	t.Helper()

	got, err := ConnectArgv(tc.c, tc.user)
	if err == nil {
		t.Fatalf("accepted, argv = %q", got)
	}

	if got != nil {
		t.Errorf("argv = %q alongside an error", got)
	}

	if !errors.Is(err, tc.is) {
		t.Errorf("err = %v, want it to wrap %v", err, tc.is)
	}
}

// TestConnectArgvRefused checks every connection that must not produce a command line.
//
// Parameters:
//   - t: test to report to
func TestConnectArgvRefused(t *testing.T) {
	cases := []refusedCase{
		{
			"needs review",
			store.Connection{Name: "n", Target: "h", NeedsReview: true},
			"",
			ErrNeedsReview,
		},
		{
			"needs review with override",
			store.Connection{Name: "n", NeedsReview: true},
			"u",
			ErrNeedsReview,
		},
		{
			"target option",
			store.Connection{Name: "n", Target: "-oProxyCommand=id"},
			"",
			store.ErrInvalidField,
		},
		{"target dash only", store.Connection{Name: "n", Target: "-"}, "", store.ErrInvalidField},
		{
			"target option for a verb",
			store.Connection{Name: "n", Via: "mosh", Target: "-x"},
			"",
			store.ErrInvalidField,
		},
		{"target option in a template", store.Connection{
			Name: "n", Via: "t {host}",
			Target: "-x",
		}, "", store.ErrInvalidField},
		{
			"target with space",
			store.Connection{Name: "n", Target: "a b"},
			"",
			store.ErrInvalidField,
		},
		{
			"target with newline",
			store.Connection{Name: "n", Target: "a\nb"},
			"",
			store.ErrInvalidField,
		},
		{
			"target ending with colon",
			store.Connection{Name: "n", Target: "fe80::"},
			"",
			store.ErrInvalidField,
		},
		{
			"stored user option",
			store.Connection{Name: "n", Target: "h", User: "-x"},
			"",
			store.ErrInvalidField,
		},
		{
			"override option",
			store.Connection{Name: "n", Target: "h"},
			"-oProxyCommand=id",
			store.ErrInvalidField,
		},
		{
			"override option for a verb",
			store.Connection{Name: "n", Via: "mosh", Target: "h"},
			"-x", store.ErrInvalidField,
		},
		{
			"override with at sign",
			store.Connection{Name: "n", Target: "h"},
			"a@b",
			store.ErrInvalidField,
		},
		{
			"override with space",
			store.Connection{Name: "n", Target: "h"},
			"a b",
			store.ErrInvalidField,
		},
		{"override for a multi word verb", store.Connection{
			Name: "n", Via: "ssh -p 22",
			Target: "h",
		}, "u", ErrUserOverride},
		{"override for a template without user", store.Connection{
			Name: "n", Via: "t {host}",
			Target: "h",
		}, "u", ErrUserOverride},
		{
			"override for a template of words",
			store.Connection{Name: "n", Via: "echo hi"},
			"u",
			ErrUserOverride,
		},
		{
			"unknown placeholder",
			store.Connection{Name: "n", Via: "t {nope}", Target: "h"},
			"",
			store.ErrInvalidField,
		},
		{
			"unmatched brace",
			store.Connection{Name: "n", Via: "t {host", Target: "h"},
			"",
			store.ErrInvalidField,
		},
		{
			"absolute path verb",
			store.Connection{Name: "n", Via: "/opt/x/go", Target: "h"},
			"",
			store.ErrInvalidField,
		},
		{
			"verb with option look",
			store.Connection{Name: "n", Via: "-x", Target: "h"},
			"",
			store.ErrInvalidField,
		},
		{
			"verb with control character",
			store.Connection{Name: "n", Via: "a\tb", Target: "h"},
			"",
			store.ErrInvalidField,
		},
		{"bad name", store.Connection{Name: "-n", Target: "h"}, "", store.ErrInvalidField},
		{"empty name and target", store.Connection{}, "", store.ErrInvalidField},
		{"user placeholder without user", store.Connection{
			Name: "n", Via: "t -l {user} {host}",
			Target: "h",
		}, "", ErrMissing},
		{"ip placeholder without ip", store.Connection{Name: "n", Via: "t {ip}"}, "", ErrMissing},
		{"ip placeholder with a bad ip", store.Connection{
			Name: "n", Via: "t {ip}",
			IP: "not-an-ip",
		}, "", store.ErrInvalidField},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkRefused(t, tc) })
	}
}

// TestConnectArgvIgnoresInformationalFields checks that a bad ip or description does not stop
// a connection whose command does not use them.
//
// Parameters:
//   - t: test to report to
func TestConnectArgvIgnoresInformationalFields(t *testing.T) {
	c := store.Connection{Name: "n", Target: "h", IP: "garbage", Desc: "bad\x01desc"}

	got, err := ConnectArgv(c, "")
	if err != nil || !reflect.DeepEqual(got, []string{"ssh", "--", "h"}) {
		t.Errorf("got %q, %v", got, err)
	}
}

// TestConnectArgvNoAliasing checks that the result does not share memory with the record.
//
// Parameters:
//   - t: test to report to
func TestConnectArgvNoAliasing(t *testing.T) {
	c := store.Connection{Name: "n", Target: "h", Aliases: []string{"a"}}

	first, err := ConnectArgv(c, "")
	if err != nil {
		t.Fatal(err)
	}

	first[0] = "changed"

	second, err := ConnectArgv(c, "")
	if err != nil || second[0] != "ssh" {
		t.Errorf("second = %q, %v", second, err)
	}
}

// TestPrettyCommand checks the display form of an argument vector.
//
// Parameters:
//   - t: test to report to
func TestPrettyCommand(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{nil, ""},
		{[]string{"ssh", "--", "web"}, "ssh -- web"},
		{[]string{"ssh", "-l", "bob", "--", "10.0.0.1"}, "ssh -l bob -- 10.0.0.1"},
		{[]string{"mosh", "bob@h.example.com"}, "mosh bob@h.example.com"},
		{[]string{"ssh", "--", "[::1]"}, "ssh -- '[::1]'"},
		{[]string{"echo", "a b", "", "it's"}, "echo 'a b' '' 'it'\\''s'"},
		{[]string{"x", "$(id)", "a;b"}, "x '$(id)' 'a;b'"},
		{[]string{"ssh", "-o", "User=u"}, "ssh -o User=u"},
		{[]string{"x", "=y"}, "x '=y'"},
	}

	for _, tc := range cases {
		if got := PrettyCommand(tc.argv); got != tc.want {
			t.Errorf("PrettyCommand(%q) = %q, want %q", tc.argv, got, tc.want)
		}
	}
}
