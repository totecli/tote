package store

import (
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// connFile is the shape of a connections file for the encoding tests.
type connFile struct {
	Schema     int          `toml:"schema"`
	Connection []Connection `toml:"connection"`
}

// aliasFile is the shape of an aliases file for the encoding tests.
type aliasFile struct {
	Schema int     `toml:"schema"`
	Alias  []Alias `toml:"alias"`
}

// repoFile is the shape of a repositories file for the encoding tests.
type repoFile struct {
	Schema int    `toml:"schema"`
	Repo   []Repo `toml:"repo"`
}

// funcFile is the shape of a functions file for the encoding tests.
type funcFile struct {
	Schema int    `toml:"schema"`
	Func   []Func `toml:"func"`
}

// roundTrip encodes a value to TOML, decodes it into a fresh value of the same type and fails
// the test when the two differ.
//
// Parameters:
//   - t: test handle
//   - in: pointer to the value to encode
//   - out: pointer to an empty value of the same type
//
// Returns the encoded text.
func roundTrip(t *testing.T, in, out any) string {
	t.Helper()

	data, err := toml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := toml.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal %q: %v", data, err)
	}

	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip changed the value:\n in: %+v\nout: %+v\ntext:\n%s", in, out, data)
	}

	return string(data)
}

// assertAbsent fails the test when the encoded text holds any of the given keys.
//
// Parameters:
//   - t: test handle
//   - text: encoded TOML
//   - keys: key names that must not appear at the start of a line
func assertAbsent(t *testing.T, text string, keys ...string) {
	t.Helper()

	for _, line := range strings.Split(text, "\n") {
		for _, k := range keys {
			if strings.HasPrefix(strings.TrimSpace(line), k+" ") {
				t.Errorf("key %q must be omitted, got line %q", k, line)
			}
		}
	}
}

// TestConnectionRoundTrip checks that connections survive an encode and decode cycle.
//
// Parameters:
//   - t: test to report to
func TestConnectionRoundTrip(t *testing.T) {
	in := &connFile{Schema: 1, Connection: []Connection{
		{
			Name: "prod", Via: "ssh -J jump {user@}{host}", Target: "10.0.0.5", User: "root",
			Aliases: []string{"p", "web1"}, IP: "2001:db8::1", Desc: "Serveur \"prod\" \\ d\u00e9",
		},
		{Name: "min", Target: "h"},
		{Name: "review", Target: "fe80::", NeedsReview: true},
		{Name: "tpl", Via: "tool {name}"},
	}}

	roundTrip(t, in, &connFile{})
}

// TestConnectionOmitEmpty checks that zero-valued optional fields do not reach the file.
//
// Parameters:
//   - t: test to report to
func TestConnectionOmitEmpty(t *testing.T) {
	in := &connFile{Schema: 1, Connection: []Connection{{Name: "min", Target: "h"}}}
	text := roundTrip(t, in, &connFile{})

	assertAbsent(t, text, "via", "user", "aliases", "ip", "desc", "needs_review")

	if !strings.Contains(text, "name = ") || !strings.Contains(text, "target = ") {
		t.Errorf("name and target must be written:\n%s", text)
	}

	empty := &connFile{Schema: 1, Connection: []Connection{{Name: "e"}}}
	assertAbsent(t, roundTrip(t, empty, &connFile{}), "target")
}

// TestConnectionNeedsReview checks that the review flag is written only when true.
//
// Parameters:
//   - t: test to report to
func TestConnectionNeedsReview(t *testing.T) {
	on := &connFile{Schema: 1, Connection: []Connection{{Name: "a", Target: "h", NeedsReview: true}}}
	if text := roundTrip(t, on, &connFile{}); !strings.Contains(text, "needs_review = true") {
		t.Errorf("needs_review = true missing:\n%s", text)
	}

	off := &connFile{Schema: 1, Connection: []Connection{{Name: "a", Target: "h"}}}
	assertAbsent(t, roundTrip(t, off, &connFile{}), "needs_review")
}

// TestAliasRoundTrip checks that aliases survive an encode and decode cycle, quoting included.
//
// Parameters:
//   - t: test to report to
func TestAliasRoundTrip(t *testing.T) {
	in := &aliasFile{Schema: 1, Alias: []Alias{
		{Name: "ll", Cmd: "ls -l"},
		{Name: "gs", Cmd: "git log --format='%h' | head -n \"3\" \\ $(id) `x`\ttab", Desc: "d"},
		{Name: "..", Cmd: "cd .."},
	}}

	text := roundTrip(t, in, &aliasFile{})
	if strings.Count(text, "desc = ") != 1 {
		t.Errorf("only one record has a description:\n%s", text)
	}
}

// TestRepoRoundTrip checks that repositories survive an encode and decode cycle verbatim.
//
// Parameters:
//   - t: test to report to
func TestRepoRoundTrip(t *testing.T) {
	in := &repoFile{Schema: 1, Repo: []Repo{
		{Name: "tote", Path: "~/Documents/GitHub/Repositories/tote", Desc: "CLI"},
		{Name: "x", Path: "$HOME/x"},
	}}

	out := &repoFile{}
	text := roundTrip(t, in, out)

	if out.Repo[0].Path[0] != '~' || out.Repo[1].Path != "$HOME/x" {
		t.Errorf("paths must be kept verbatim: %+v", out.Repo)
	}

	if strings.Count(text, "desc = ") != 1 {
		t.Errorf("only one record has a description:\n%s", text)
	}
}

// TestFuncRoundTrip checks that functions survive an encode and decode cycle.
//
// Parameters:
//   - t: test to report to
func TestFuncRoundTrip(t *testing.T) {
	in := &funcFile{Schema: 1, Func: []Func{
		{Name: "mkcd", Desc: "mkdir + cd", Kind: "source", Script: "/usr/local/bin/mkcd"},
		{Name: "up", Kind: "exec", Script: "~/.local/bin/up", Managed: true},
		{Name: "off", Kind: "exec", Script: "s", Managed: false},
	}}

	text := roundTrip(t, in, &funcFile{})

	if strings.Count(text, "managed = ") != 3 {
		t.Errorf("managed must always be written:\n%s", text)
	}

	if strings.Count(text, "desc = ") != 1 {
		t.Errorf("only one record has a description:\n%s", text)
	}
}

// TestDesignExamples checks that the hand written examples of the design decode and validate.
//
// Parameters:
//   - t: test to report to
func TestDesignExamples(t *testing.T) {
	const conns = `schema = 1

# --- clients ---
[[connection]]
name    = "prod"
via     = "ssh"
target  = "10.0.0.5"
user    = "root"
aliases = ["p", "web1"]
ip      = "2001:db8::1"
desc    = "Serveur prod"
`

	const funcs = `schema = 1
[[func]]
name    = "mkcd"
desc    = "mkdir + cd"
kind    = "source"
script  = "/usr/local/bin/mkcd"
managed = false
`

	var cf connFile
	if err := toml.Unmarshal([]byte(conns), &cf); err != nil {
		t.Fatalf("connections: %v", err)
	}

	want := Connection{
		Name: "prod", Via: "ssh", Target: "10.0.0.5", User: "root",
		Aliases: []string{"p", "web1"}, IP: "2001:db8::1", Desc: "Serveur prod",
	}
	if cf.Schema != 1 || len(cf.Connection) != 1 || !reflect.DeepEqual(cf.Connection[0], want) {
		t.Errorf("decoded connections = %+v", cf)
	}

	if err := cf.Connection[0].Validate(); err != nil {
		t.Errorf("design connection rejected: %v", err)
	}

	var ff funcFile
	if err := toml.Unmarshal([]byte(funcs), &ff); err != nil {
		t.Fatalf("funcs: %v", err)
	}

	if len(ff.Func) != 1 || ff.Func[0].Kind != "source" || ff.Func[0].Managed {
		t.Errorf("decoded funcs = %+v", ff)
	}

	if err := ff.Func[0].Validate(); err != nil {
		t.Errorf("design func rejected: %v", err)
	}
}

// TestHandWrittenNeverTrusted checks that a record decoded from a hostile file fails Validate.
//
// Parameters:
//   - t: test to report to
func TestHandWrittenNeverTrusted(t *testing.T) {
	const text = `schema = 1
[[connection]]
name = "evil; touch x"
target = "-oProxyCommand=id"
`

	var cf connFile
	if err := toml.Unmarshal([]byte(text), &cf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if err := cf.Connection[0].Validate(); err == nil {
		t.Error("hostile connection accepted")
	}
}
