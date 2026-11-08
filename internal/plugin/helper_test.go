package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testHost is the host of every test that does not choose another: tote 1.0.3 on linux/amd64
// with the features of 1.0.
func testHost() Host {
	h := NewHost("1.0.3", "tote-test")
	h.OS, h.Arch = "linux", "amd64"

	return h
}

// header is the required top-level keys of a valid manifest, without tier.
const header = `api = 1
id = "demo"
version = "1.0.0"
tote = ">=1.0.0, <2"
description = "Demo plugin."
license = "MIT"
`

// decl builds a declarative manifest.
//
// Parameters:
//   - top: extra top-level keys, written before the permissions
//   - perms: permission entries
//   - body: tables, written after the top-level keys
func decl(top string, perms []string, body string) string {
	return build(TierDeclarative, top, perms, "", body)
}

// execM builds an exec manifest with a single-program [exec] table.
//
// Parameters:
//   - top: extra top-level keys, written before the permissions
//   - perms: permission entries
//   - body: tables, written after [exec]
func execM(top string, perms []string, body string) string {
	return build(TierExec, top, perms, "[exec]\nbin = \"bin/demo\"\n", body)
}

// build joins the parts of a manifest in an order that is valid TOML.
//
// Parameters:
//   - tier: tier key value
//   - top: extra top-level keys
//   - perms: permission entries
//   - pre: tables that come first, such as [exec]
//   - body: remaining tables
func build(tier, top string, perms []string, pre, body string) string {
	var b strings.Builder

	b.WriteString(header)
	b.WriteString("tier = \"" + tier + "\"\n")
	b.WriteString(top)

	if len(perms) > 0 {
		b.WriteString("permissions = [")

		for i, p := range perms {
			if i > 0 {
				b.WriteString(", ")
			}

			b.WriteString("\"" + p + "\"")
		}

		b.WriteString("]\n")
	}

	b.WriteString(pre)
	b.WriteString(body)

	return b.String()
}

// cmdBody is a valid command that runs "tool" and uses the field x.
const cmdBody = `
[[command]]
id = "go"
label = "Go"
run = ["tool", "{x}"]
output = "pager"
[[command.field]]
id = "x"
type = "text"
label = "X"
`

// decode reads a manifest with the test host.
//
// Parameters:
//   - src: manifest text
func decode(src string) (*Manifest, Report) {
	return Decode([]byte(src), DecodeOptions{Host: testHost()})
}

// mustDecode reads a manifest that must be valid and fails the test otherwise.
//
// Parameters:
//   - t: test to report to
//   - src: manifest text
func mustDecode(t *testing.T, src string) *Manifest {
	t.Helper()

	m, rep := decode(src)
	if m == nil {
		t.Fatalf("manifest refused: %v\n%s", rep.Issues, src)
	}

	return m
}

// find returns the first issue with a code and, when path is not empty, that path.
//
// Parameters:
//   - rep: report to search
//   - code: stable code
//   - path: issue path, empty to match any
func find(rep Report, code, path string) *Issue {
	for i := range rep.Issues {
		if rep.Issues[i].Code == code && (path == "" || rep.Issues[i].Path == path) {
			return &rep.Issues[i]
		}
	}

	return nil
}

// readFixture reads a file of testdata.
//
// Parameters:
//   - t: test to report to
//   - name: slash separated path below testdata
func readFixture(t testing.TB, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

// times returns the string repeated n times, for the limit tests.
//
// Parameters:
//   - s: text to repeat
//   - n: number of copies
func times(s string, n int) string { return strings.Repeat(s, n) }
