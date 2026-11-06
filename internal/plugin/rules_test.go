package plugin

import (
	"fmt"
	"strings"
	"testing"
)

// ruleCase is one manifest and the issue it must produce: its stable code, its path (empty to
// accept any), whether it is a warning that leaves the manifest valid, and the class of the
// verdict for an error.
type ruleCase struct {
	name  string
	src   string
	code  string
	path  string
	warn  bool
	class Class
}

// runRules decodes each case and checks the verdict, the code and the path.
//
// Parameters:
//   - t: test to report to
//   - tests: cases to run
func runRules(t *testing.T, tests []ruleCase) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, rep := decode(tt.src)

			is := find(rep, tt.code, tt.path)
			if is == nil {
				t.Fatalf("no %s at %q in %v", tt.code, tt.path, rep.Issues)
			}

			if tt.warn {
				if m == nil || is.Severity != SeverityWarning {
					t.Errorf("want a warning on a valid manifest, got manifest=%v issue=%v", m != nil, is)
				}

				return
			}

			want := tt.class
			if want == ClassOK {
				want = ClassInvalid
			}

			if m != nil || rep.Class() != want {
				t.Errorf("manifest=%v class=%v, want nil and %v", m != nil, rep.Class(), want)
			}
		})
	}
}

// TestRulesDocument checks the document-level rules: size, encoding, syntax, the probe and the
// unknown keys.
//
// Parameters:
//   - t: test to report to
func TestRulesDocument(t *testing.T) {
	good := decl("", nil, "")
	inc := ClassIncompatible

	tests := []ruleCase{
		{name: "syntax", src: "api = ", code: CodeManifestSyntax},
		{name: "utf8", src: "api = 1\n# \xff\n", code: CodeManifestSyntax},
		{name: "nul byte", src: "api = 1\nid = \"a\x00\"\n", code: CodeManifestSyntax},
		{name: "too large", src: good + "# " + times("x", MaxManifestSize), code: CodeManifestTooLarge},
		{
			name: "api missing", src: strings.Replace(good, "api = 1\n", "", 1),
			code: CodeKeyMissing,
			path: "api",
		},
		{
			name: "api type", src: strings.Replace(good, "api = 1", "api = \"1\"", 1),
			code: CodeTypeInvalid, path: "api",
		},
		{
			name: "api newer", src: strings.Replace(good, "api = 1", "api = 2", 1),
			code: CodeAPIUnsupported, path: "api", class: inc,
		},
		{
			name: "api older", src: strings.Replace(good, "api = 1", "api = 0", 1),
			code: CodeAPIUnsupported, path: "api", class: inc,
		},
		{
			name: "tote missing", src: strings.Replace(good, "tote = \">=1.0.0, <2\"\n", "", 1),
			code: CodeKeyMissing, path: "tote",
		},
		{
			name: "tote type", src: strings.Replace(good, "\">=1.0.0, <2\"", "2", 1),
			code: CodeTypeInvalid, path: "tote",
		},
		{
			name: "tote caret", src: strings.Replace(good, ">=1.0.0, <2", "^1.0", 1),
			code: CodeRangeInvalid, path: "tote",
		},
		{
			name: "tote excludes host", src: strings.Replace(good, ">=1.0.0, <2", ">=1.2.0", 1),
			code: CodeToteRange, path: "tote", class: inc,
		},
		{
			name: "tier missing", src: strings.Replace(good, "tier = \"declarative\"\n", "", 1),
			code: CodeKeyMissing, path: "tier",
		},
		{
			name: "tier unknown", src: strings.Replace(good, "declarative", "magic", 1),
			code: CodeValueInvalid, path: "tier",
		},
		{
			name: "feature unknown", src: decl("requires_features = [\"nope\"]\n", nil, ""),
			code: CodeFeatureMissing, class: inc,
		},
		{
			name: "feature reserved", src: decl("requires_features = [\"exec.health\"]\n", nil, ""),
			code: CodeFeatureMissing, class: inc,
		},
		{
			name: "feature grammar", src: decl("requires_features = [\"Bad Name\"]\n", nil, ""),
			code: CodeValueInvalid, path: "requires_features[0]",
		},
		{name: "features count", src: decl("requires_features = ["+times("\"command\",", 33)+"]\n",
			nil, ""), code: CodeLimitExceeded, path: "requires_features"},
		{
			name: "platforms windows", src: decl("platforms = [\"windows\"]\n", nil, ""),
			code: CodeValueInvalid, path: "platforms[0]",
		},
		{
			name: "platforms empty", src: decl("platforms = []\n", nil, ""), code: CodeValueInvalid,
			path: "platforms",
		},
		{
			name: "platforms excludes host", src: decl("platforms = [\"darwin\"]\n", nil, ""),
			code: CodePlatformUnsupported, class: inc,
		},
		{
			name: "no build for host", src: execM("", nil,
				"[[platform]]\nos = \"linux\"\narch = \"arm64\"\n"),
			code: CodePlatformNone, class: inc,
		},
		{name: "unknown key", src: decl("bogus = 1\n", nil, ""), code: CodeUnknownKey, path: "bogus"},
		{
			name: "shell key does not exist", src: decl("shell = true\n", nil, ""),
			code: CodeUnknownKey, path: "shell",
		},
		{
			name: "unknown key in table", src: decl("", nil, cmdBody+"confrim = true\n"),
			code: CodeUnknownKey, path: "command[0].field[0].confrim",
		},
		{
			name: "run key on an action", src: execM("", nil,
				"[[action]]\nid = \"a\"\nlabel = \"A\"\noutput = \"pager\"\nrun = [\"x\"]\n"),
			code: CodeUnknownKey, path: "action[0].run",
		},
		{
			name: "target on a command", src: decl("", []string{"exec:tool"},
				cmdBody+"target = \"repo\"\n"),
			code: CodeUnknownKey,
		},
		{name: "keywords on an item action", src: decl("", []string{"exec:tool", "host:read:repos"},
			"[[item_action]]\nid = \"a\"\ntarget = \"repo\"\nlabel = \"A\"\nrun = [\"tool\"]\n"+
				"output = \"pager\"\nkeywords = [\"k\"]\n"), code: CodeUnknownKey},
		{
			name: "type error", src: decl("", nil, "[[command]]\nid = 3\n"), code: CodeTypeInvalid,
			path: "command[0].id",
		},
		{name: "table as scalar", src: decl("", nil, "command = 3\n"), code: CodeTypeInvalid},
		{
			name: "locale table unknown language", src: decl("", nil, "[i18n.de]\ntitle = \"x\"\n"),
			code: CodeUnknownKey, path: "i18n.de",
		},
		{
			name: "locale value type", src: decl("", nil, "[i18n.fr]\ntitle = 3\n"),
			code: CodeTypeInvalid, path: "i18n.fr.title",
		},
	}

	for _, r := range reservedTables {
		tests = append(tests, ruleCase{
			name: "reserved table " + r,
			src:  decl("", nil, "[["+r+"]]\nid = \"x\"\n"), code: CodeReserved, path: r,
		})
	}

	runRules(t, tests)
}

// TestRulesIncompatibleStopsAtTheProbe checks that a manifest that fails the probe is reported
// without the strict step: a key from a newer tote is not an unknown key to an older one.
//
// Parameters:
//   - t: test to report to
func TestRulesIncompatibleStopsAtTheProbe(t *testing.T) {
	src := strings.Replace(decl("future_key = 1\n", nil, "[[future_table]]\nx = 1\n"),
		">=1.0.0, <2", ">=9.0.0", 1)

	m, rep := decode(src)
	if m != nil || rep.Class() != ClassIncompatible {
		t.Fatalf("manifest=%v class=%v, want nil and incompatible", m != nil, rep.Class())
	}

	if rep.Has(CodeUnknownKey) {
		t.Errorf("the strict step ran after a failed probe: %v", rep.Issues)
	}

	if rep.Header.ID != "demo" || rep.Header.Version != "1.0.0" {
		t.Errorf("header = %+v, want the id and the version of the manifest", rep.Header)
	}
}

// TestRulesAllUnknownKeysListed checks that every unknown key is reported, in document order, with
// the line where it sits.
//
// Parameters:
//   - t: test to report to
func TestRulesAllUnknownKeysListed(t *testing.T) {
	src := decl("zeta = 1\nalpha = 2\n", nil, cmdBody+"typo = 1\n")

	_, rep := decode(src)

	var got []string

	for _, i := range rep.Issues {
		if i.Code == CodeUnknownKey {
			got = append(got, fmt.Sprintf("%s@%d", i.Path, i.Line))
		}
	}

	want := "zeta@8 alpha@9 command[0].field[0].typo@20"
	if strings.Join(got, " ") != want {
		t.Errorf("unknown keys = %q, want %q", strings.Join(got, " "), want)
	}
}

// TestRulesIdentity checks the identity keys: id grammar and reserved names, version, license,
// homepage, title and description.
//
// Parameters:
//   - t: test to report to
func TestRulesIdentity(t *testing.T) {
	good := decl("", nil, "")
	swap := func(old, repl string) string { return strings.Replace(good, old, repl, 1) }

	tests := []ruleCase{
		{name: "id upper case", src: swap("\"demo\"", "\"Demo\""), code: CodeIDInvalid, path: "id"},
		{name: "id too short", src: swap("\"demo\"", "\"d\""), code: CodeIDInvalid, path: "id"},
		{
			name: "id too long", src: swap("\"demo\"", "\""+times("a", 33)+"\""),
			code: CodeIDInvalid,
			path: "id",
		},
		{name: "id leading hyphen", src: swap("\"demo\"", "\"-ab\""), code: CodeIDInvalid, path: "id"},
		{name: "id with slash", src: swap("\"demo\"", "\"a/b\""), code: CodeIDInvalid, path: "id"},
		{name: "id dotdot", src: swap("\"demo\"", "\"..\""), code: CodeIDInvalid, path: "id"},
		{name: "id verb", src: swap("\"demo\"", "\"plugin\""), code: CodeIDInvalid, path: "id"},
		{name: "id core verb", src: swap("\"demo\"", "\"conn\""), code: CodeIDInvalid, path: "id"},
		{name: "id prefix", src: swap("\"demo\"", "\"tote-x\""), code: CodeIDInvalid, path: "id"},
		{name: "id word", src: swap("\"demo\"", "\"registry\""), code: CodeIDInvalid, path: "id"},
		{
			name: "id record field", src: swap("\"demo\"", "\"needs_review\""), code: CodeIDInvalid,
			path: "id",
		},
		{
			name: "id record field plain", src: swap("\"demo\"", "\"jump\""), code: CodeIDInvalid,
			path: "id",
		},
		{name: "id official", src: swap("\"demo\"", "\"gft\""), code: CodeIDInvalid, path: "id"},
		{
			name: "version v prefix", src: swap("\"1.0.0\"", "\"v1.0.0\""),
			code: CodeVersionInvalid,
			path: "version",
		},
		{
			name: "version two parts", src: swap("\"1.0.0\"", "\"1.0\""), code: CodeVersionInvalid,
			path: "version",
		},
		{
			name: "version leading zero", src: swap("\"1.0.0\"", "\"01.0.0\""),
			code: CodeVersionInvalid,
			path: "version",
		},
		{name: "version type", src: swap("\"1.0.0\"", "1"), code: CodeTypeInvalid, path: "version"},
		{
			name: "license shape", src: swap("\"MIT\"", "\"no good!\""), code: CodeValueInvalid,
			path: "license",
		},
		{name: "license empty", src: swap("\"MIT\"", "\"\""), code: CodeValueInvalid, path: "license"},
		{
			name: "homepage http", src: decl("homepage = \"http://x.example\"\n", nil, ""),
			code: CodeValueInvalid, path: "homepage",
		},
		{
			name: "homepage userinfo", src: decl("homepage = \"https://u:p@x.example/\"\n", nil,
				""),
			code: CodeValueInvalid, path: "homepage",
		},
		{
			name: "homepage long", src: decl("homepage = \"https://x.example/"+times("a",
				250)+"\"\n", nil,
				""),
			code: CodeLimitExceeded, path: "homepage",
		},
		{
			name: "title long", src: decl("title = \""+times("a", 41)+"\"\n", nil, ""),
			code: CodeLimitExceeded, path: "title",
		},
		{
			name: "title control", src: decl("title = \"a\\u0007b\"\n", nil, ""),
			code: CodeTextInvalid,
			path: "title",
		},
		{
			name: "description missing", src: swap("description = \"Demo plugin.\"\n", ""),
			code: CodeKeyMissing, path: "description",
		},
		{
			name: "description long", src: swap("Demo plugin.", times("a", 201)),
			code: CodeLimitExceeded, path: "description",
		},
		{
			name: "description newline", src: swap("Demo plugin.", "a\\nb"), code: CodeTextInvalid,
			path: "description",
		},
	}

	runRules(t, tests)

	_, rep := Decode([]byte(swap("\"demo\"", "\"other\"")),
		DecodeOptions{Host: testHost(), WantID: "demo"})
	if find(rep, CodeIDInvalid, "id") == nil {
		t.Errorf("an id that differs from the requested one was accepted: %v", rep.Issues)
	}

	m, rep := Decode([]byte(swap("\"demo\"", "\"gft\"")),
		DecodeOptions{Host: testHost(), Official: true})
	if m == nil {
		t.Errorf("an official id was refused to an official source: %v", rep.Issues)
	}
}

// TestRulesPermissions checks the permission grammar through the manifest.
//
// Parameters:
//   - t: test to report to
func TestRulesPermissions(t *testing.T) {
	with := func(p ...string) string { return decl("", p, "") }

	tests := []ruleCase{
		{name: "unknown entry", src: with("frob:x"), code: CodePermUnknown, path: "permissions[0]"},
		{name: "exec path", src: with("exec:/bin/tool"), code: CodePermInvalid, path: "permissions[0]"},
		{name: "exec empty", src: with("exec:"), code: CodePermInvalid},
		{name: "exec tote", src: with("exec:tote"), code: CodeExecSelf, path: "permissions[0]"},
		{name: "exec tote case", src: with("exec:Tote"), code: CodeExecSelf},
		{name: "exec host binary", src: with("exec:tote-test"), code: CodeExecSelf},
		{name: "exec shell", src: with("exec:bash"), code: CodeArgvShell, path: "permissions[0]"},
		{name: "exec launcher", src: with("exec:xargs"), code: CodeArgvShell},
		{name: "net star", src: with("net:*"), code: CodePermInvalid},
		{name: "net star com", src: with("net:*.com"), code: CodePermInvalid},
		{name: "net port", src: with("net:example.com:80"), code: CodePermInvalid},
		{name: "net ip", src: with("net:10.0.0.1"), code: CodePermInvalid},
		{name: "net upper", src: with("net:Example.com"), code: CodePermInvalid},
		{name: "net userinfo", src: with("net:a@example.com"), code: CodePermInvalid},
		{name: "net one label", src: with("net:localhost"), code: CodePermInvalid},
		{name: "fs zone", src: with("fs:read:moon"), code: CodePermInvalid},
		{name: "fs verb", src: with("fs:exec:home"), code: CodePermUnknown},
		{name: "env lower", src: with("env:token"), code: CodePermInvalid},
		{name: "env tote", src: with("env:TOTE_HANDOFF"), code: CodePermRefused},
		{name: "env tote token", src: with("env:TOTE_TOKEN"), code: CodeReserved},
		{name: "env loader", src: with("env:LD_PRELOAD"), code: CodePermRefused},
		{name: "env path", src: with("env:PATH"), code: CodePermRefused},
		{name: "env git ssh", src: with("env:GIT_SSH_COMMAND"), code: CodePermRefused},
		{name: "duplicate", src: with("env:A", "env:A"), code: CodePermInvalid, path: "permissions[1]"},
		{name: "intercept", src: with("hook:intercept"), code: CodePermRefused},
		{name: "write settings", src: with("host:write:settings"), code: CodePermRefused},
		{name: "reserved connections", src: with("host:read:connections"), code: CodeReserved},
		{name: "reserved handoff", src: with("host:handoff"), code: CodeReserved},
		{name: "reserved clipboard", src: with("host:clipboard"), code: CodeReserved},
		{name: "reserved notify", src: with("host:notify"), code: CodeReserved},
		{name: "reserved editor", src: with("host:editor"), code: CodeReserved},
		{
			name: "too many", src: decl("permissions = ["+times("\"env:A\",", 65)+"]\n", nil, ""),
			code: CodeLimitExceeded, path: "permissions",
		},
		{name: "not a list", src: decl("permissions = \"env:A\"\n", nil, ""), code: CodeTypeInvalid},
	}

	runRules(t, tests)

	m := mustDecode(t, decl("", []string{
		"net:b.example.com", "exec:tool", "env:A",
		"net:*.example.org",
	}, ""))
	want := []string{"env:A", "exec:tool", "net:*.example.org", "net:b.example.com"}

	if strings.Join(m.Permissions, " ") != strings.Join(want, " ") {
		t.Errorf("Permissions = %v, want the sorted list %v", m.Permissions, want)
	}
}

// TestRulesTier checks the rules that tie the tables to the tier.
//
// Parameters:
//   - t: test to report to
func TestRulesTier(t *testing.T) {
	act := "[[action]]\nid = \"a\"\nlabel = \"A\"\noutput = \"pager\"\n"
	lst := "[[list]]\nid = \"l\"\nlabel = \"L\"\n[[list.column]]\nid = \"c\"\nlabel = \"C\"\n"
	bld := "[[platform]]\nos = \"linux\"\narch = \"amd64\"\nbin = \"bin/x\"\nasset = \"x.tar.gz\"\n" +
		"sha256 = \"" +
		times("a", 64) + "\"\n"

	tests := []ruleCase{
		{
			name: "declarative exec", src: decl("", nil, "[exec]\nbin = \"bin/x\"\n"),
			code: CodeTierMismatch,
		},
		{name: "declarative platform", src: decl("", nil, bld), code: CodeTierMismatch},
		{name: "declarative action", src: decl("", nil, act), code: CodeTierMismatch},
		{name: "declarative list", src: decl("", nil, lst), code: CodeTierMismatch},
		{
			name: "exec without program", src: build(TierExec, "", nil, "", ""),
			code: CodeKeyMissing,
			path: "exec.bin",
		},
		{name: "exec bin and platform", src: execM("", nil, bld), code: CodeKeyConflict},
		{
			name: "exec bin parent", src: build(TierExec, "", nil, "[exec]\nbin = \"../x\"\n", ""),
			code: CodeValueInvalid, path: "exec.bin",
		},
		{
			name: "exec bin absolute", src: build(TierExec, "", nil, "[exec]\nbin = \"/bin/x\"\n",
				""),
			code: CodeValueInvalid,
		},
		{
			name: "exec bin backslash", src: build(TierExec, "", nil, "[exec]\nbin = \"a\\\\b\"\n",
				""),
			code: CodeValueInvalid,
		},
		{
			name: "exec bin dash", src: build(TierExec, "", nil, "[exec]\nbin = \"-x\"\n", ""),
			code: CodeValueInvalid,
		},
		{
			name: "exec bin deep", src: build(TierExec, "", nil,
				"[exec]\nbin = \"a/b/c/d/e/f/g\"\n", ""),
			code: CodeValueInvalid,
		},
		{
			name: "exec mode health", src: build(TierExec, "", nil,
				"[exec]\nbin = \"b\"\nmodes = [\"health\"]\n", ""),
			code: CodeReserved, path: "exec.modes[0]",
		},
		{
			name: "exec mode search", src: build(TierExec, "", nil,
				"[exec]\nbin = \"b\"\nmodes = [\"search\"]\n", ""),
			code: CodeReserved,
		},
		{
			name: "exec mode unknown", src: build(TierExec, "", nil,
				"[exec]\nbin = \"b\"\nmodes = [\"x\"]\n", ""),
			code: CodeValueInvalid,
		},
		{
			name: "exec session long", src: build(TierExec, "", nil,
				"[exec]\nbin = \"b\"\nsession = \"long\"\n", ""),
			code: CodeReserved, path: "exec.session",
		},
		{
			name: "exec session unknown", src: build(TierExec, "", nil,
				"[exec]\nbin = \"b\"\nsession = \"x\"\n", ""),
			code: CodeValueInvalid,
		},
		{name: "exec modes lack list", src: build(TierExec, "", nil,
			"[exec]\nbin = \"b\"\nmodes = [\"run\"]\n", lst), code: CodeValueInvalid, path: "exec.modes"},
		{name: "exec modes lack run", src: build(TierExec, "", nil,
			"[exec]\nbin = \"b\"\nmodes = [\"list\"]\n", act), code: CodeValueInvalid},
		{
			name: "platform twice", src: build(TierExec, "", nil, "[exec]\n", bld+bld),
			code: CodeIDDuplicate,
		},
		{name: "platform hash", src: build(TierExec, "", nil, "[exec]\n",
			strings.Replace(bld, times("a", 64), "xyz", 1)), code: CodeValueInvalid},
		{
			name: "platform os", src: build(TierExec, "", nil, "[exec]\n",
				bld+strings.Replace(bld, "\"linux\"", "\"plan9\"", 1)), code: CodeValueInvalid,
			path: "platform[1].os",
		},
		{name: "platform asset", src: build(TierExec, "", nil, "[exec]\n",
			strings.Replace(bld, "x.tar.gz", "a b", 1)), code: CodeValueInvalid},
	}

	runRules(t, tests)

	m := mustDecode(t, build(TierExec, "", nil, "[exec]\n", bld))
	if len(m.Builds) != 1 || m.Builds[0].Arch != "amd64" {
		t.Errorf("Builds = %+v", m.Builds)
	}
}

// TestRulesRequires checks the [[requires]] entries.
//
// Parameters:
//   - t: test to report to
func TestRulesRequires(t *testing.T) {
	req := func(bin, extra string) string {
		return decl("", nil, "[[requires]]\nbin = \""+bin+"\"\n"+extra)
	}

	tests := []ruleCase{
		{
			name: "bin missing", src: decl("", nil, "[[requires]]\nhint = \"x\"\n"),
			code: CodeKeyMissing,
			path: "requires[0].bin",
		},
		{name: "bin path", src: req("a/b", ""), code: CodeValueInvalid, path: "requires[0].bin"},
		{name: "bin dash", src: req("-x", ""), code: CodeValueInvalid},
		{name: "bin dotdot", src: req("..", ""), code: CodeValueInvalid},
		{name: "bin long", src: req(times("a", 65), ""), code: CodeValueInvalid},
		{name: "duplicate", src: req("x", "[[requires]]\nbin = \"x\"\n"), code: CodeIDDuplicate},
		{
			name: "hint long", src: req("x", "hint = \""+times("a", 201)+"\"\n"),
			code: CodeLimitExceeded,
			path: "requires[0].hint",
		},
		{name: "hint control", src: req("x", "hint = \"a\\u001bb\"\n"), code: CodeTextInvalid},
		{name: "optional type", src: req("x", "optional = \"yes\"\n"), code: CodeTypeInvalid},
		{
			name: "too many", src: decl("", nil, times("[[requires]]\nbin = \"x\"\n", 17)),
			code: CodeLimitExceeded, path: "requires",
		},
	}

	runRules(t, tests)
}
