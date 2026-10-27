package store

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// strCase is one input of a table driven string validator test.
type strCase struct {
	name string
	in   string
	ok   bool
}

// checkVerdict fails the test when err does not match the expected verdict, and requires every
// rejection to wrap ErrInvalidField and to carry no raw control character.
//
// Parameters:
//   - t: test handle
//   - err: error returned by the validator under test
//   - ok: true when the input must be accepted
func checkVerdict(t *testing.T, err error, ok bool) {
	t.Helper()

	if ok {
		if err != nil {
			t.Errorf("want accepted, got error: %v", err)
		}

		return
	}

	if err == nil {
		t.Error("want rejected, got nil")

		return
	}

	if !errors.Is(err, ErrInvalidField) {
		t.Errorf("error does not wrap ErrInvalidField: %v", err)
	}

	for _, r := range err.Error() {
		if r < 0x20 || r == 0x7f {
			t.Errorf("error message carries a control character: %q", err.Error())

			break
		}
	}
}

// runStrCases runs a table of string cases against a validator.
//
// Parameters:
//   - t: test handle
//   - fn: validator under test
//   - cases: inputs with their expected verdict
func runStrCases(t *testing.T, fn func(string) error, cases []strCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkVerdict(t, fn(tc.in), tc.ok)
		})
	}
}

// TestValidConnectionName checks the connection name rule on accepted and hostile inputs.
//
// Parameters:
//   - t: test to report to
func TestValidConnectionName(t *testing.T) {
	cases := []strCase{
		{"simple", "prod", true},
		{"single letter", "a", true},
		{"leading underscore", "_x", true},
		{"leading digit", "9lives", true},
		{"dash dot underscore", "web-1.a_b", true},
		{"upper case", "ProdDB", true},
		{"64 bytes", strings.Repeat("a", 64), true},
		{"empty", "", false},
		{"65 bytes", strings.Repeat("a", 65), false},
		{"leading dash", "-x", false},
		{"leading dot", ".x", false},
		{"space inside", "my host", false},
		{"leading space", " a", false},
		{"trailing space", "a ", false},
		{"semicolon", "a;rm", false},
		{"command substitution", "a$(id)", false},
		{"backtick", "a`id`", false},
		{"newline", "a\nb", false},
		{"carriage return", "a\rb", false},
		{"tab", "a\tb", false},
		{"nul", "a\x00b", false},
		{"escape", "a\x1b[31m", false},
		{"slash", "a/b", false},
		{"colon", "a:b", false},
		{"at sign", "a@b", false},
		{"accented", "caf\u00e9", false},
		{"cyrillic confusable", "\u0430dmin", false},
		{"fullwidth", "\uff41", false},
		{"zero width space", "\u200bx", false},
		{"invalid utf-8", "a\xffb", false},
	}

	runStrCases(t, ValidConnectionName, cases)
}

// TestValidAliasName checks bash's alias rule, the dot aliases and the reserved words.
//
// Parameters:
//   - t: test to report to
func TestValidAliasName(t *testing.T) {
	cases := []strCase{
		{"simple", "ll", true},
		{"two dots", "..", true},
		{"three dots", "...", true},
		{"four dots", "....", true},
		{"five dots", ".....", true},
		{"six dots", "......", true},
		{"digits and dash", "g-2", true},
		{"underscore", "_g", true},
		{"percent", "a%b", true},
		{"colon", "a:b", true},
		{"at sign", "a@b", true},
		{"plus and comma", "a+b,c", true},
		{"printable non ascii", "\u00e9crire", true},
		{"reserved prefix", "tote2", true},
		{"reserved case differs", "Tote", true},
		{"mn prefix", "mn2", true},
		{"64 bytes", strings.Repeat("a", 64), true},
		{"empty", "", false},
		{"65 bytes", strings.Repeat("a", 65), false},
		{"reserved tote", "tote", false},
		{"reserved mn", "mn", false},
		{"leading dash", "-l", false},
		{"only dash", "-", false},
		{"six dots then space", "...... ", false},
		{"space inside", "a b", false},
		{"tab", "a\tb", false},
		{"newline", "a\nb", false},
		{"carriage return", "a\rb", false},
		{"nul", "a\x00b", false},
		{"escape", "a\x1bb", false},
		{"no break space", "a\u00a0b", false},
		{"line separator", "a\u2028b", false},
		{"zero width space", "a\u200bb", false},
		{"slash", "a/b", false},
		{"dollar", "a$b", false},
		{"command substitution", "$(id)", false},
		{"backtick", "a`b", false},
		{"equals", "a=b", false},
		{"single quote", "a'b", false},
		{"double quote", "a\"b", false},
		{"pipe", "a|b", false},
		{"ampersand", "a&b", false},
		{"semicolon", "a;b", false},
		{"open paren", "a(b", false},
		{"close paren", "a)b", false},
		{"less than", "a<b", false},
		{"greater than", "a>b", false},
		{"bang", "a!b", false},
		{"open brace", "a{b", false},
		{"close brace", "a}b", false},
		{"star", "a*b", false},
		{"question mark", "a?b", false},
		{"open bracket", "a[b", false},
		{"close bracket", "a]b", false},
		{"hash", "a#b", false},
		{"tilde", "a~b", false},
		{"caret", "a^b", false},
		{"backslash", "a\\b", false},
		{"invalid utf-8", "\xff", false},
	}

	runStrCases(t, ValidAliasName, cases)
}

// TestValidRepoName checks that the repository rule is the alias rule without reserved words.
//
// Parameters:
//   - t: test to report to
func TestValidRepoName(t *testing.T) {
	cases := []strCase{
		{"simple", "tote-site", true},
		{"dotted", "my.repo", true},
		{"dot alias", "..", true},
		{"tote is reserved", "tote", false},
		{"mn is allowed", "mn", true},
		{"empty", "", false},
		{"65 bytes", strings.Repeat("r", 65), false},
		{"leading dash", "-r", false},
		{"slash", "a/b", false},
		{"space", "a b", false},
		{"semicolon", "a;b", false},
		{"command substitution", "a$(id)", false},
		{"backtick", "a`id`", false},
		{"newline", "a\nb", false},
		{"nul", "a\x00b", false},
		{"equals", "a=b", false},
		{"quote", "a'b", false},
	}

	runStrCases(t, ValidRepoName, cases)
}

// TestValidFuncName checks the function name rule on accepted and hostile inputs.
//
// Parameters:
//   - t: test to report to
func TestValidFuncName(t *testing.T) {
	cases := []strCase{
		{"simple", "mkcd", true},
		{"leading underscore", "_x", true},
		{"dash and digits", "a-b_2", true},
		{"dash kept although bash posix mode refuses it", "fn-x", true},
		{"upper case", "MkCd", true},
		{"64 bytes", strings.Repeat("f", 64), true},
		{"empty", "", false},
		{"65 bytes", strings.Repeat("f", 65), false},
		{"leading digit", "1a", false},
		{"leading dash", "-a", false},
		{"dot", "a.b", false},
		{"space", "a b", false},
		{"semicolon", "a;b", false},
		{"command substitution", "a$(id)", false},
		{"backtick", "a`id`", false},
		{"newline", "a\nb", false},
		{"nul", "a\x00b", false},
		{"slash", "a/b", false},
		{"accented", "caf\u00e9", false},
		{"cyrillic confusable", "m\u043ecd", false},
		{"invalid utf-8", "a\xff", false},
	}

	runStrCases(t, ValidFuncName, cases)
}

// TestValidDesc checks the description rule, including the rune limit and control characters.
//
// Parameters:
//   - t: test to report to
func TestValidDesc(t *testing.T) {
	cases := []strCase{
		{"empty", "", true},
		{"plain", "Serveur prod", true},
		{"accented", "Serveur de d\u00e9veloppement", true},
		{"quotes and backslash", "say \"hi\" \\ 'x' $HOME `id`", true},
		{"200 runes", strings.Repeat("a", 200), true},
		{"200 multibyte runes", strings.Repeat("\u00e9", 200), true},
		{"201 runes", strings.Repeat("a", 201), false},
		{"201 multibyte runes", strings.Repeat("\u00e9", 201), false},
		{"tab", "a\tb", false},
		{"newline", "a\nb", false},
		{"carriage return", "a\rb", false},
		{"nul", "a\x00b", false},
		{"escape sequence", "\x1b[31mred", false},
		{"delete", "a\x7fb", false},
		{"c1 next line", "a\u0085b", false},
		{"c1 csi", "a\u009bb", false},
		{"line separator", "a\u2028b", false},
		{"paragraph separator", "a\u2029b", false},
		{"invalid utf-8", "a\xffb", false},
	}

	runStrCases(t, ValidDesc, cases)
}

// TestValidLine checks the single line rule used for commands, paths and scripts.
//
// Parameters:
//   - t: test to report to
func TestValidLine(t *testing.T) {
	cases := []strCase{
		{"command", "ls -l | head", true},
		{"tilde path", "~/Documents/code", true},
		{"dollar kept", "$HOME/x", true},
		{"quotes", "echo 'a' \"b\"", true},
		{"tab inside", "a\tb", true},
		{"4096 bytes", strings.Repeat("a", maxLineLen), true},
		{"4097 bytes", strings.Repeat("a", maxLineLen+1), false},
		{"empty", "", false},
		{"spaces only", "   ", false},
		{"tab only", "\t", false},
		{"newline", "a\nb", false},
		{"trailing newline", "ls\n", false},
		{"carriage return", "a\rb", false},
		{"nul", "a\x00b", false},
		{"invalid utf-8", "a\xff", false},
	}

	runStrCases(t, validLine, cases)
}

// TestValidUser checks the connection user rule.
//
// Parameters:
//   - t: test to report to
func TestValidUser(t *testing.T) {
	cases := []strCase{
		{"empty", "", true},
		{"root", "root", true},
		{"underscore", "_svc", true},
		{"dotted", "deploy.bot", true},
		{"dash", "a-b", true},
		{"32 bytes", strings.Repeat("u", 32), true},
		{"33 bytes", strings.Repeat("u", 33), false},
		{"leading digit", "1user", true},
		{"leading dash", "-u", false},
		{"space", "a b", false},
		{"semicolon", "a;b", false},
		{"command substitution", "a$(id)", false},
		{"at sign", "a@b", false},
		{"trailing newline", "root\n", false},
		{"nul", "a\x00", false},
		{"cyrillic confusable", "\u0440oot", false},
	}

	runStrCases(t, validUser, cases)
}

// TestValidIP checks the informational address rule.
//
// Parameters:
//   - t: test to report to
func TestValidIP(t *testing.T) {
	cases := []strCase{
		{"empty", "", true},
		{"ipv4", "10.0.0.5", true},
		{"loopback v6", "::1", true},
		{"link local prefix", "fe80::", true},
		{"link local", "fe80::1", true},
		{"documentation", "2001:db8::1", true},
		{"mapped", "::ffff:1.2.3.4", true},
		{"short ipv4", "10.0.0", false},
		{"out of range", "999.1.1.1", false},
		{"hostname", "example.com", false},
		{"brackets", "[::1]", false},
		{"zone", "fe80::1%eth0", false},
		{"trailing space", "10.0.0.5 ", false},
		{"prefix length", "10.0.0.5/24", false},
		{"bad hex", "::g", false},
		{"newline", "10.0.0.5\n", false},
		{"nul", "10.0.0.5\x00", false},
	}

	runStrCases(t, validIP, cases)
}

// TestViaPlaceholders checks the via command rule and the placeholders it reports.
//
// Parameters:
//   - t: test to report to
func TestViaPlaceholders(t *testing.T) {
	good := []struct {
		via  string
		want []string
	}{
		{"", nil},
		{"ssh", nil},
		{"jmp", nil},
		{"tools/jmp -x", nil},
		{"my_tool.v2 --fast", nil},
		{"ssh -J jump {user@}{host}", []string{"user@", "host"}},
		{"mosh {user}@{host}", []string{"user", "host"}},
		{"echo {ip} {name} {ip}", []string{"ip", "name"}},
	}

	for _, tc := range good {
		t.Run("good "+tc.via, func(t *testing.T) {
			checkVia(t, tc.via, tc.want)
		})
	}

	bad := []strCase{
		{"leading dash", "-ssh", false},
		{"absolute path", "/usr/bin/ssh", false},
		{"unknown placeholder", "ssh {foo}", false},
		{"empty placeholder", "ssh {}", false},
		{"upper case placeholder", "ssh {HOST}", false},
		{"glued placeholders", "ssh {user@host}", false},
		{"nested braces", "ssh {{host}}", false},
		{"unclosed brace", "ssh {host", false},
		{"stray closing brace", "ssh host}", false},
		{"placeholder in verb", "{host}", false},
		{"tab separator", "ssh\t-v", false},
		{"newline", "ssh\n-v", false},
		{"nul", "ssh\x00", false},
		{"escape in placeholder", "ssh {\x1b}", false},
		{"leading space", " ssh", false},
		{"trailing space", "ssh ", false},
		{"only spaces", "  ", false},
		{"cyrillic verb", "ss\u0455h", false},
		{"invalid utf-8", "ssh \xff", false},
		{"unknown after valid", "ssh {host} {port}", false},
		{"single quote", "ssh -o ProxyCommand='ssh -W %h:%p jump' {host}", false},
		{"double quote", "ssh -o \"User=x\" {host}", false},
		{"quote in the verb", "ss'h", false},
		{"4097 bytes", "ssh " + strings.Repeat("a", maxLineLen-3), false},
	}

	for _, tc := range bad {
		t.Run("bad "+tc.name, func(t *testing.T) {
			_, err := viaPlaceholders(tc.in)
			checkVerdict(t, err, false)
		})
	}
}

// TestViaQuoteReason checks that a quoted via is refused with a reason that tells why and what to
// do instead, and that the rule on the first word is stated without the misleading '/' claim.
//
// Parameters:
//   - t: test to report to
func TestViaQuoteReason(t *testing.T) {
	cases := []struct {
		name string
		via  string
		want string
	}{
		{"quote", "ssh -o ProxyCommand='x y' {host}", "quotes are not interpreted"},
		{"absolute path", "/usr/bin/ssh {host}", "first word must be a command name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := viaPlaceholders(tc.via)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}

			if tc.name == "quote" && !strings.Contains(err.Error(), "ssh_config") {
				t.Errorf("error does not point to ssh_config: %v", err)
			}

			if tc.name == "absolute path" && !strings.Contains(err.Error(), "starts with") {
				t.Errorf("error does not say how the word must start: %v", err)
			}
		})
	}
}

// checkVia validates a via command that must be accepted and compares its placeholders.
//
// Parameters:
//   - t: test handle
//   - via: via command under test
//   - want: placeholder names that must be reported, in any order
func checkVia(t *testing.T, via string, want []string) {
	t.Helper()

	got, err := viaPlaceholders(via)
	checkVerdict(t, err, true)

	if len(got) != len(want) {
		t.Fatalf("placeholders = %v, want %v", got, want)
	}

	for _, name := range want {
		if !got[name] {
			t.Errorf("placeholder %q not reported in %v", name, got)
		}
	}
}

// TestValidTarget checks the target rule, with and without placeholders in the via command.
//
// Parameters:
//   - t: test to report to
func TestValidTarget(t *testing.T) {
	none := map[string]bool{}
	host := map[string]bool{"host": true}
	nameOnly := map[string]bool{"name": true}

	cases := []struct {
		name   string
		target string
		found  map[string]bool
		ok     bool
	}{
		{"ipv4", "10.0.0.5", none, true},
		{"hostname", "web1.example.com", none, true},
		{"user at host", "root@web1", none, true},
		{"ipv6 inner colons", "fe80::1", none, true},
		{"ipv6 documentation", "2001:db8::1", none, true},
		{"ipv6 brackets", "[::1]", none, true},
		{"ipv6 prefix in brackets", "[fe80::]", none, true},
		{"host with port", "web1:22", none, true},
		{"empty with name only template", "", nameOnly, true},
		{"empty with empty set", "", none, false},
		{"empty with host template", "", host, false},
		{"ipv6 trailing colons", "fe80::", none, false},
		{"ipv6 leading colons", "::1", none, false},
		{"trailing colon", "host:", none, false},
		{"leading colon", ":22", none, false},
		{"bracketed trailing colon", "[::1]:", none, false},
		{"single colon", ":", none, false},
		{"leading dash", "-oProxyCommand=x", none, false},
		{"lone dash", "-", none, false},
		{"space", "host name", none, false},
		{"tab", "host\tname", none, false},
		{"trailing newline", "host\n", none, false},
		{"nul", "host\x00", none, false},
		{"escape", "ho\x1bst", none, false},
		{"no break space", "\u00a0host", none, false},
		{"zero width space", "ho\u200bst", none, false},
		{"invalid utf-8", "\xff", none, false},
		{"4096 bytes", strings.Repeat("h", maxLineLen), none, true},
		{"4097 bytes", strings.Repeat("h", maxLineLen+1), none, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkVerdict(t, validTarget(tc.target, tc.found), tc.ok)
		})
	}
}

// connCase is one connection and its expected validation verdict.
type connCase struct {
	name string
	c    Connection
	ok   bool
}

// TestConnectionValidate checks the whole connection record, field by field.
//
// Parameters:
//   - t: test to report to
func TestConnectionValidate(t *testing.T) {
	long := strings.Repeat("n", 64)
	cases := []connCase{
		{"minimal", Connection{Name: "prod", Target: "10.0.0.5"}, true},
		{"full", Connection{
			Name: "prod", Via: "ssh", Target: "10.0.0.5", User: "root",
			Aliases: []string{"p", "web1"}, IP: "2001:db8::1", Desc: "Serveur prod",
		}, true},
		{"needs review carried", Connection{Name: "a", Target: "h", NeedsReview: true}, true},
		{"verb via", Connection{Name: "a", Via: "jmp", Target: "h"}, true},
		{"template via", Connection{Name: "a", Via: "ssh -J j {user@}{host}", Target: "h"}, true},
		{"template without target", Connection{Name: "a", Via: "tool {name}"}, true},
		{"template with ip only", Connection{Name: "a", Via: "tool {ip}", IP: "10.0.0.1"}, true},
		{"template without host but target", Connection{Name: "a", Via: "t {name}", Target: "h"}, true},
		{"ipv6 target", Connection{Name: "a", Target: "fe80::1"}, true},
		{"bracketed ipv6 target", Connection{Name: "a", Target: "[::1]"}, true},
		{"long name", Connection{Name: long, Target: "h"}, true},
		{"empty name", Connection{Target: "h"}, false},
		{"65 byte name", Connection{Name: long + "n", Target: "h"}, false},
		{"name with space", Connection{Name: "a b", Target: "h"}, false},
		{"name with semicolon", Connection{Name: "a;b", Target: "h"}, false},
		{"name with substitution", Connection{Name: "a$(id)", Target: "h"}, false},
		{"name with backtick", Connection{Name: "a`id`", Target: "h"}, false},
		{"name with newline", Connection{Name: "a\nb", Target: "h"}, false},
		{"name with nul", Connection{Name: "a\x00", Target: "h"}, false},
		{"name leading dash", Connection{Name: "-a", Target: "h"}, false},
		{"name cyrillic", Connection{Name: "\u0430", Target: "h"}, false},
		{"via leading dash", Connection{Name: "a", Via: "-x", Target: "h"}, false},
		{"via unknown placeholder", Connection{Name: "a", Via: "ssh {port}", Target: "h"}, false},
		{"via newline", Connection{Name: "a", Via: "ssh\nrm", Target: "h"}, false},
		{"empty target default via", Connection{Name: "a"}, false},
		{"empty target verb via", Connection{Name: "a", Via: "jmp"}, false},
		{"empty target host template", Connection{Name: "a", Via: "t {host}"}, false},
		{"target trailing colon", Connection{Name: "a", Target: "fe80::"}, false},
		{"target leading dash", Connection{Name: "a", Target: "-oX=y"}, false},
		{"target space", Connection{Name: "a", Target: "h h"}, false},
		{"user leading digit", Connection{Name: "a", Target: "h", User: "1u"}, true},
		{"user leading dash", Connection{Name: "a", Target: "h", User: "-u"}, false},
		{"user too long", Connection{Name: "a", Target: "h", User: strings.Repeat("u", 33)}, false},
		{"user semicolon", Connection{Name: "a", Target: "h", User: "u;x"}, false},
		{"alias duplicate", Connection{Name: "a", Target: "h", Aliases: []string{"p", "p"}}, false},
		{"alias duplicate apart", Connection{
			Name: "a", Target: "h",
			Aliases: []string{"p", "q", "p"},
		}, false},
		{"alias equals name", Connection{Name: "a", Target: "h", Aliases: []string{"a"}}, false},
		{"alias with space", Connection{Name: "a", Target: "h", Aliases: []string{"p q"}}, false},
		{"alias empty", Connection{Name: "a", Target: "h", Aliases: []string{""}}, false},
		{"alias leading dash", Connection{Name: "a", Target: "h", Aliases: []string{"-p"}}, false},
		{"alias with comma", Connection{Name: "a", Target: "h", Aliases: []string{"p,q"}}, false},
		{"alias too long", Connection{
			Name: "a", Target: "h",
			Aliases: []string{long + "n"},
		}, false},
		{"ip invalid", Connection{Name: "a", Target: "h", IP: "10.0.0"}, false},
		{"ip with zone", Connection{Name: "a", Target: "h", IP: "fe80::1%eth0"}, false},
		{"ip hostname", Connection{Name: "a", Target: "h", IP: "example.com"}, false},
		{"desc newline", Connection{Name: "a", Target: "h", Desc: "a\nb"}, false},
		{"desc escape", Connection{Name: "a", Target: "h", Desc: "\x1b[2J"}, false},
		{"desc too long", Connection{Name: "a", Target: "h", Desc: strings.Repeat("d", 201)}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkVerdict(t, tc.c.Validate(), tc.ok)
		})
	}
}

// TestConnectionValidateField checks that the error names the field that was rejected.
//
// Parameters:
//   - t: test to report to
func TestConnectionValidateField(t *testing.T) {
	cases := []struct {
		field string
		c     Connection
	}{
		{"connection name", Connection{Name: "a b", Target: "h"}},
		{"connection via", Connection{Name: "a", Via: "x {y}", Target: "h"}},
		{"connection target", Connection{Name: "a", Target: ""}},
		{"connection user", Connection{Name: "a", Target: "h", User: "-"}},
		{"connection aliases", Connection{Name: "a", Target: "h", Aliases: []string{"a"}}},
		{"connection ip", Connection{Name: "a", Target: "h", IP: "x"}},
		{"connection desc", Connection{Name: "a", Target: "h", Desc: "\n"}},
	}

	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			err := tc.c.Validate()
			if err == nil || !strings.HasPrefix(err.Error(), tc.field+": ") {
				t.Errorf("error = %v, want prefix %q", err, tc.field)
			}
		})
	}
}

// TestAliasValidate checks the whole alias record.
//
// Parameters:
//   - t: test to report to
func TestAliasValidate(t *testing.T) {
	cases := []struct {
		name string
		a    Alias
		ok   bool
	}{
		{"minimal", Alias{Name: "ll", Cmd: "ls -l"}, true},
		{"with desc", Alias{Name: "gs", Cmd: "git status", Desc: "status"}, true},
		{"pipe and quotes", Alias{Name: "x", Cmd: "ps aux | grep 'a' \"b\" $1"}, true},
		{"dot alias", Alias{Name: "..", Cmd: "cd .."}, true},
		{"unicode cmd", Alias{Name: "x", Cmd: "echo \u00e9"}, true},
		{"reserved tote", Alias{Name: "tote", Cmd: "ls"}, false},
		{"reserved mn", Alias{Name: "mn", Cmd: "ls"}, false},
		{"empty name", Alias{Cmd: "ls"}, false},
		{"name with space", Alias{Name: "a b", Cmd: "ls"}, false},
		{"name with equals", Alias{Name: "a=b", Cmd: "ls"}, false},
		{"empty cmd", Alias{Name: "ll"}, false},
		{"blank cmd", Alias{Name: "ll", Cmd: "  "}, false},
		{"cmd newline", Alias{Name: "ll", Cmd: "ls\nrm x"}, false},
		{"cmd carriage return", Alias{Name: "ll", Cmd: "ls\rrm x"}, false},
		{"cmd nul", Alias{Name: "ll", Cmd: "ls\x00"}, false},
		{"desc newline", Alias{Name: "ll", Cmd: "ls", Desc: "a\nb"}, false},
		{"desc too long", Alias{Name: "ll", Cmd: "ls", Desc: strings.Repeat("d", 201)}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkVerdict(t, tc.a.Validate(), tc.ok)
		})
	}
}

// TestRepoValidate checks the whole repository record.
//
// Parameters:
//   - t: test to report to
func TestRepoValidate(t *testing.T) {
	cases := []struct {
		name string
		r    Repo
		ok   bool
	}{
		{"tilde path", Repo{Name: "proj", Path: "~/code/tote"}, true},
		{"home variable kept", Repo{Name: "x", Path: "$HOME/code/x"}, true},
		{"relative path", Repo{Name: "x", Path: "code/x"}, true},
		{"mn name allowed", Repo{Name: "mn", Path: "~/mn", Desc: "old tool"}, true},
		{"empty name", Repo{Path: "~/x"}, false},
		{"name with slash", Repo{Name: "a/b", Path: "~/x"}, false},
		{"name with space", Repo{Name: "a b", Path: "~/x"}, false},
		{"name leading dash", Repo{Name: "-a", Path: "~/x"}, false},
		{"empty path", Repo{Name: "x"}, false},
		{"path newline", Repo{Name: "x", Path: "~/a\nb"}, false},
		{"path nul", Repo{Name: "x", Path: "~/a\x00"}, false},
		{"desc escape", Repo{Name: "x", Path: "~/x", Desc: "\x1b"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkVerdict(t, tc.r.Validate(), tc.ok)
		})
	}
}

// TestFuncValidate checks the whole function record.
//
// Parameters:
//   - t: test to report to
func TestFuncValidate(t *testing.T) {
	cases := []struct {
		name string
		f    Func
		ok   bool
	}{
		{"exec", Func{Name: "mkcd", Kind: "exec", Script: "/usr/local/bin/mkcd"}, true},
		{"source managed", Func{Name: "up", Kind: "source", Script: "~/f/up.sh", Managed: true}, true},
		{"with desc", Func{Name: "x", Kind: "exec", Script: "s", Desc: "mkdir + cd"}, true},
		{"empty kind", Func{Name: "x", Script: "s"}, false},
		{"upper case kind", Func{Name: "x", Kind: "EXEC", Script: "s"}, false},
		{"unknown kind", Func{Name: "x", Kind: "shell", Script: "s"}, false},
		{"kind with space", Func{Name: "x", Kind: "exec ", Script: "s"}, false},
		{"empty script", Func{Name: "x", Kind: "exec"}, false},
		{"script newline", Func{Name: "x", Kind: "exec", Script: "a\nb"}, false},
		{"script nul", Func{Name: "x", Kind: "exec", Script: "a\x00"}, false},
		{"leading digit name", Func{Name: "1x", Kind: "exec", Script: "s"}, false},
		{"empty name", Func{Kind: "exec", Script: "s"}, false},
		{"long name", Func{Name: strings.Repeat("f", 65), Kind: "exec", Script: "s"}, false},
		{"desc newline", Func{Name: "x", Kind: "exec", Script: "s", Desc: "a\nb"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkVerdict(t, tc.f.Validate(), tc.ok)
		})
	}
}

// TestKeys checks that every record kind uses its name as key and that each implements Record.
//
// Parameters:
//   - t: test to report to
func TestKeys(t *testing.T) {
	var _ Record = Connection{}

	var _ Record = Alias{}

	var _ Record = Repo{}

	var _ Record = Func{}

	if k := (Connection{Name: "c"}).Key(); k != "c" {
		t.Errorf("Connection key = %q", k)
	}

	if k := (Alias{Name: "a"}).Key(); k != "a" {
		t.Errorf("Alias key = %q", k)
	}

	if k := (Repo{Name: "r"}).Key(); k != "r" {
		t.Errorf("Repo key = %q", k)
	}

	if k := (Func{Name: "f"}).Key(); k != "f" {
		t.Errorf("Func key = %q", k)
	}
}

// TestTableNames checks the exported table name constants.
//
// Parameters:
//   - t: test to report to
func TestTableNames(t *testing.T) {
	got := []string{TableConnection, TableAlias, TableRepo, TableFunc}
	want := []string{"connection", "alias", "repo", "func"}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("table %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// FuzzValidators checks that no validator panics and that what they accept respects their rules.
//
// Parameters:
//   - f: fuzz target that receives the seed corpus and the fuzz function
func FuzzValidators(f *testing.F) {
	seeds := []string{
		"", "prod", "..", "a b", "a;b", "$(id)", "fe80::", "ssh {host}", "\x00",
		"\u0430", "tote", "a\nb", "[::1]", "-x",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(fuzzCheck)
}

// fuzzCheck is the body of FuzzValidators for one input.
//
// Parameters:
//   - t: test handle
//   - s: fuzzed input, used as every string field in turn
func fuzzCheck(t *testing.T, s string) {
	t.Helper()

	if ValidConnectionName(s) == nil && !connNameRe.MatchString(s) {
		t.Errorf("connection name %q accepted", s)
	}

	if ValidFuncName(s) == nil && !funcNameRe.MatchString(s) {
		t.Errorf("func name %q accepted", s)
	}

	if ValidAliasName(s) == nil {
		checkShellSafe(t, s)
	}

	if ValidDesc(s) == nil && !utf8.ValidString(s) {
		t.Errorf("desc %q accepted", s)
	}

	_ = Connection{Name: s, Via: s, Target: s, User: s, Aliases: []string{s}, IP: s}.Validate()
}

// checkShellSafe fails the test when an accepted alias name holds a character that bash would
// treat specially.
//
// Parameters:
//   - t: test handle
//   - s: alias name that ValidAliasName accepted
func checkShellSafe(t *testing.T, s string) {
	t.Helper()

	if s == "" || s[0] == '-' || strings.ContainsAny(s, shellForbidden+" \t\n\r\x00") {
		t.Errorf("alias name %q accepted", s)
	}
}
