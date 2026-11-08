package plugin

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// TestFixtureGft checks the full gft example of section 10 of the specification.
//
// Parameters:
//   - t: test to report to
func TestFixtureGft(t *testing.T) {
	data := readFixture(t, "gft/plugin.toml")

	m, rep := Decode(data, DecodeOptions{Host: testHost(), Official: true})
	if m == nil {
		t.Fatalf("gft refused: %v", rep.Issues)
	}

	// The only finding is the accelerator "l", which the specification uses and also reserves.
	if len(rep.Issues) != 1 || rep.Issues[0].Code != CodeKeyReserved ||
		rep.Issues[0].Severity != SeverityWarning {
		t.Errorf("gft issues = %v", rep.Issues)
	}

	if m.ID != "gft" || m.Tier != TierDeclarative || m.Version == "" {
		t.Errorf("identity = %q %q %q", m.ID, m.Tier, m.Version)
	}

	if len(m.Permissions) != 10 || len(m.Commands) != 4 || len(m.ItemActions) != 7 ||
		len(m.Requires) == 0 {
		t.Errorf("counts: %d permissions, %d commands, %d item actions, %d requires",
			len(m.Permissions), len(m.Commands), len(m.ItemActions), len(m.Requires))
	}

	if got := len(Commands(m)); got != 11 {
		t.Errorf("Commands = %d, want 11", got)
	}

	wantCaps := []string{
		CapDerivedTTY, "env:GITHUB_TOKEN", "exec:gft", "fs:read:cwd", "fs:write:cwd", "fs:write:home",
		"host:open-url", "host:read:repos", "net:*.githubusercontent.com", "net:api.github.com",
		"net:github.com",
	}
	if got := EffectiveCaps(m); !slices.Equal(got, wantCaps) {
		t.Errorf("EffectiveCaps = %v", got)
	}

	for _, c := range EffectiveCaps(m) {
		if !ValidCap(c) {
			t.Errorf("capability %q is not valid", c)
		}
	}

	// Without the official flag the id is refused, with its reason.
	if _, rep := Decode(data, DecodeOptions{Host: testHost()}); !rep.Has(CodeIDInvalid) {
		t.Errorf("gft accepted as a third-party plugin: %v", rep.Issues)
	}

	// Another host: too old a tote is incompatible, not invalid.
	if m, rep := Decode(data, DecodeOptions{
		Host:     NewHost("0.9.0", "tote-test"),
		Official: true,
	}); m != nil ||
		rep.Class() != ClassIncompatible {
		t.Errorf("old host class = %v", rep.Class())
	}

	if h := SurfaceSHA256(m); h != SurfaceSHA256(m) || len(h) != 64 {
		t.Errorf("surface hash is unstable: %s", h)
	}

	if tok := GrantToken(EffectiveCaps(m), SurfaceSHA256(m)); !strings.HasSuffix(tok,
		"@"+SurfaceSHA256(m)[:12]) {
		t.Errorf("grant token = %q", tok)
	}

	if rep.Header.ID != "gft" || rep.Header.Tier != TierDeclarative || rep.Header.Title == "" {
		t.Errorf("header = %+v", rep.Header)
	}
}

// TestFixtureGhrel checks the full ghrel example of section 11 of the specification.
//
// Parameters:
//   - t: test to report to
func TestFixtureGhrel(t *testing.T) {
	data := readFixture(t, "ghrel/plugin.toml")

	m, rep := Decode(data, DecodeOptions{Host: testHost(), Official: true})
	if m == nil || len(rep.Issues) != 0 {
		t.Fatalf("ghrel: %v", rep.Issues)
	}

	if m.Tier != TierExec || m.Exec == nil || len(m.Lists) != 1 || len(m.Settings) != 2 ||
		len(m.Requires) != 2 {
		t.Errorf("shape: tier=%s exec=%v lists=%d settings=%d requires=%d",
			m.Tier, m.Exec, len(m.Lists), len(m.Settings), len(m.Requires))
	}

	wantCaps := []string{
		CapDerivedCode, "env:GH_TOKEN", "env:GITHUB_TOKEN", "exec:gh", "exec:jq", "host:open-url",
		"net:api.github.com",
	}
	if got := EffectiveCaps(m); !slices.Equal(got, wantCaps) {
		t.Errorf("EffectiveCaps = %v", got)
	}

	if got := ExecFiles(m); len(got) != 1 {
		t.Errorf("ExecFiles = %v", got)
	}

	if issues := CheckPackage(m, []string{"plugin.toml", m.Exec.Bin}, testHost()); len(issues) != 0 {
		t.Errorf("CheckPackage = %v", issues)
	}

	if len(TranslatableKeys(m)) != 14 {
		t.Errorf("TranslatableKeys = %v", TranslatableKeys(m))
	}

	// No choice, so no English file is needed, and no other language is shipped.
	for _, is := range CheckLocales(m, nil, true) {
		t.Errorf("a plugin without choices needs no locale: %v", is)
	}
}

// TestFixturesAreASCII keeps the fixtures verbatim and portable.
//
// Parameters:
//   - t: test to report to
func TestFixturesAreASCII(t *testing.T) {
	for _, name := range []string{
		"gft/plugin.toml", "ghrel/plugin.toml", "gft/locales/en.toml",
		"gft/locales/fr.toml",
	} {
		for i, b := range readFixture(t, name) {
			if b >= 0x80 || (b < 0x20 && b != '\n' && b != '\t') {
				t.Errorf("%s: byte %#x at %d", name, b, i)

				break
			}
		}
	}
}

// TestFixtureMutations deletes, one at a time, every line of the fixtures. Each mutant must give a
// verdict without panicking, and an accepted one must still produce a surface and caps.
//
// Parameters:
//   - t: test to report to
func TestFixtureMutations(t *testing.T) {
	for _, name := range []string{"gft/plugin.toml", "ghrel/plugin.toml"} {
		lines := bytes.Split(readFixture(t, name), []byte("\n"))

		for i := range lines {
			mutant := bytes.Join(append(append([][]byte{}, lines[:i]...), lines[i+1:]...), []byte("\n"))

			m, rep := Decode(mutant, DecodeOptions{Host: testHost(), Official: true})
			if m == nil && rep.Class() == ClassOK {
				t.Fatalf("%s without line %d: no manifest and no error", name, i+1)
			}

			if m != nil {
				_ = SurfaceSHA256(m)
				_ = EffectiveCaps(m)
				_ = TranslatableKeys(m)
			}
		}
	}
}

// FuzzDecode feeds arbitrary bytes to the manifest reader. The invariants: no panic, a manifest if
// and only if the report holds no error, every reported text is clean, every position is sane,
// and a manifest that is accepted can be hashed and consented to.
//
// Parameters:
//   - f: fuzz target
func FuzzDecode(f *testing.F) {
	for _, name := range []string{"gft/plugin.toml", "ghrel/plugin.toml"} {
		f.Add(readFixture(f, name))
	}

	for _, s := range []string{
		"", "api = 1", header + "tier = \"declarative\"\n", decl("", tool, cmdBody),
		execM("", nil, "[[action]]\nid = \"a\"\nlabel = \"A\"\noutput = \"pager\"\n"),
		"api = 2\nid = 1\n", "[[command]]\n[[command]]\n", "a = \"\\u001b[31m\"\n", "\x00",
		"\xef\xbb\xbfapi = 1",
		decl("", tool, cmdBody+"[i18n.fr]\n\"command.go.label\" = \"Aller\"\n"),
		strings.Repeat("[[command]]\n", 70),
		"a = " + times("[", 50) + times("]", 50), "a = " + times("{a=", 50) + "1" + times("}", 50),
		"[" + times("a.", 50) + "a]\n", times("a.", 50) + "a = 1\n", "a = [[[[[1]]]]]\n",
		"[a.b.c.d.e.f.g.h.i]\n", times("[[a.b]]\n", 5000), "a = \"\"\"\n[[[[\n\"\"\"\"\"\n",
	} {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		m, rep := Decode(data, DecodeOptions{Host: testHost()})

		if (m == nil) != (len(rep.Errors()) > 0) {
			t.Fatalf("manifest=%v with %d errors", m != nil, len(rep.Errors()))
		}

		lines := bytes.Count(data, []byte("\n")) + 1

		for _, is := range rep.Issues {
			if is.Code == "" || is.Line < 0 || is.Column < 0 || is.Line > lines+1 {
				t.Fatalf("bad issue %+v", is)
			}

			for _, a := range is.Args {
				if !plainText(a) {
					t.Fatalf("unclean argument %q in %+v", a, is)
				}
			}
		}

		for _, s := range []string{rep.Header.ID, rep.Header.Version, rep.Header.Tier, rep.Header.Title} {
			if !plainText(s) {
				t.Fatalf("unclean header text %q", s)
			}
		}

		if m == nil {
			return
		}

		if len(SurfaceSHA256(m)) != 64 {
			t.Fatal("surface hash")
		}

		for _, c := range EffectiveCaps(m) {
			if !ValidCap(c) {
				t.Fatalf("capability %q of an accepted manifest is not valid", c)
			}
		}

		for _, s := range append(TranslatableKeys(m), m.Title, m.Description, m.ID) {
			if !plainText(s) {
				t.Fatalf("unclean accepted text %q", s)
			}
		}
	})
}
