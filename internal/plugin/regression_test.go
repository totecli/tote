package plugin

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// envCmd is a declarative command with an env table holding one value.
const envCmd = `
[[command]]
id = "go"
label = "Go"
run = ["tool"]
output = "pager"
env = { MY_MODE = "%s" }
`

// TestFixConsentShowsEnvTable checks that the env table, with its values, is part of what the
// consent view lists for a command, and that a change of a value alone is a command change and an
// escalation: the value of a variable can make a declared program start another one.
//
// Parameters:
//   - t: test to report to
func TestFixConsentShowsEnvTable(t *testing.T) {
	perms := []string{"exec:tool", "env:MY_MODE"}
	old := mustDecode(t, decl("", perms, strings.Replace(envCmd, "%s", "a", 1)))
	next := mustDecode(t, decl("", perms, strings.Replace(envCmd, "%s", "b", 1)))

	cmds := Commands(old)
	if len(cmds) != 1 || !slices.Equal(cmds[0].Env, []string{"MY_MODE=a"}) {
		t.Fatalf("Commands = %+v, want the env table with its value", cmds)
	}

	changes := DiffCommands(old, next)
	if len(changes) != 1 || changes[0].Kind != "changed" {
		t.Errorf("DiffCommands = %+v, want one changed command", changes)
	}

	esc := SurfaceEscalations(old, next)
	if len(esc) != 1 || esc[0].Kind != EscEnvChanged {
		t.Errorf("SurfaceEscalations = %+v, want env_changed", esc)
	}

	if got := SurfaceEscalations(old, old); len(got) != 0 {
		t.Errorf("an identical version escalates: %+v", got)
	}
}

// TestFixPreparedShowsEnvTable checks that the confirmation of a run can show the env table.
//
// Parameters:
//   - t: test to report to
func TestFixPreparedShowsEnvTable(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()
	l.script("tool", "exit 0\n")
	l.plug("demo", []string{"exec:tool", "env:MY_MODE"}, strings.Replace(envCmd, "%s", "fast", 1))

	p := prepare(t, l.reg(nil), Call{Plugin: "demo", Table: TableCommand, Entry: "go"})
	if !slices.Equal(p.EnvTable, []string{"MY_MODE=fast"}) {
		t.Errorf("EnvTable = %v", p.EnvTable)
	}
}

// TestFixExecBinIsNotData checks that the program of an exec plugin cannot be the manifest or a
// locale file, which the host parses as data.
//
// Parameters:
//   - t: test to report to
func TestFixExecBinIsNotData(t *testing.T) {
	for _, bin := range []string{"plugin.toml", "PLUGIN.TOML", "locales/en.toml"} {
		src := strings.Replace(execM("", []string{"exec:demo"}, ""), `bin = "bin/demo"`,
			`bin = "`+bin+`"`, 1)

		if m, _ := decode(src); m != nil {
			t.Errorf("bin %q accepted", bin)
		}
	}

	if m, rep := decode(execM("", []string{"exec:demo"}, "")); m == nil {
		t.Errorf("the ordinary program was refused: %v", rep.Issues)
	}
}

// TestFixExecSelfThroughAlias checks that a program which is the tote binary under another name,
// here a link, is refused at Prepare, and that the base name of Options.Bin is refused at Decode.
//
// Parameters:
//   - t: test to report to
func TestFixExecSelfThroughAlias(t *testing.T) {
	l := newLab(t)
	l.useSystemTools()
	l.script("tool", "exit 0\n")

	self := filepath.Join(l.root, "mytote")
	must(t, os.WriteFile(self, []byte("#!/bin/sh\n"), 0o700))
	must(t, os.Symlink(self, filepath.Join(l.bin, "alias")))

	body := strings.ReplaceAll(strings.ReplaceAll(runBody, `"tool"`, `"alias"`),
		"exec:tool", "exec:alias")
	l.plug("demo", []string{"exec:alias"}, body)

	r := l.reg(func(o *Options) { o.Bin = self })

	if err := refusal(t, r, Call{Plugin: "demo", Table: TableCommand, Entry: "sl"}); err.Code !=
		CodeExecSelf {
		t.Errorf("code %s, want %s", err.Code, CodeExecSelf)
	}

	named := Open(Options{Bin: "/opt/mytote"})
	if !slices.Contains(named.opts.Host.Self, "mytote") {
		t.Errorf("Host.Self = %v, want the base name of Bin", named.opts.Host.Self)
	}
}

// TestFixPluginsDirWritableByOthers checks that a plugins directory that others can write makes
// the plugins broken at Load, not only at install.
//
// Parameters:
//   - t: test to report to
func TestFixPluginsDirWritableByOthers(t *testing.T) {
	l := newLab(t)
	l.install("demo", demoFiles("demo", ""))

	base := l.paths.PluginsDir()

	must(t, os.Chmod(base, 0o777))
	t.Cleanup(func() { _ = os.Chmod(base, 0o700) })

	p := load(t, l, "demo")
	if p.State != StateBroken || !hasReason(p.Reasons, CodeStoreWritable) {
		t.Errorf("state %s reasons %v", p.State, reasonCodes(p.Reasons))
	}
}

// TestFixCleanRemoteHelperSyntax checks that a <transport>::<address> remote is refused while an
// IPv6 literal in a url still passes.
//
// Parameters:
//   - t: test to report to
func TestFixCleanRemoteHelperSyntax(t *testing.T) {
	for _, raw := range []string{"fd::17", "foo::bar", "foo::https://x.example/a", "a@b::c:path"} {
		if _, ok := CleanRemote(raw); ok {
			t.Errorf("CleanRemote(%q) accepted", raw)
		}
	}

	if _, ok := CleanRemote("https://[::1]/a/b.git"); !ok {
		t.Error("an IPv6 literal in an https url was refused")
	}
}

// TestFixOpenURLSchemeCase checks that the scheme must be written in lower case, so that the URL
// handed to the opener is exactly the one that was checked.
//
// Parameters:
//   - t: test to report to
func TestFixOpenURLSchemeCase(t *testing.T) {
	if _, err := CheckOpenURL("Https://example.org/x", ""); err == nil {
		t.Error("an upper case scheme was accepted")
	}

	if _, err := CheckOpenURL("https://example.org/x", ""); err != nil {
		t.Errorf("a plain url was refused: %v", err)
	}
}

// TestFixGftTagPattern checks the tag pattern of the bundled gft commands against the words that
// gft reads as its own subcommands and against a leading dash.
//
// Parameters:
//   - t: test to report to
func TestFixGftTagPattern(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("bundled", "plugins", "gft", "plugin.toml"))
	if err != nil {
		t.Fatal(err)
	}

	m := regexp.MustCompile(`(?m)^id = "tag"\n.*\n.*\npattern = "(.*)"$`).FindSubmatch(data)
	if m == nil {
		t.Fatal("no tag field found")
	}

	re := regexp.MustCompile(strings.ReplaceAll(string(m[1]), `\\`, `\`))

	for _, ok := range []string{"", "v1.2.3", "1.0", "V2", "rc-1", "nightly-2024", "v1.0.0-rc.1+b"} {
		if !re.MatchString(ok) {
			t.Errorf("tag %q refused", ok)
		}
	}

	for _, bad := range []string{"update", "delete", "uninstall", "stash", "--install", "-x", "a b"} {
		if re.MatchString(bad) {
			t.Errorf("tag %q accepted", bad)
		}
	}
}
