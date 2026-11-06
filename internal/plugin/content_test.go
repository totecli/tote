package plugin

import (
	"slices"
	"testing"
)

// TestCheckPackageDeclarative checks which files a declarative package may hold.
//
// Parameters:
//   - t: test to report to
func TestCheckPackageDeclarative(t *testing.T) {
	m := mustDecode(t, decl("", tool, cmdBody))

	ok := []string{
		"plugin.toml", "locales/en.toml", "locales/fr.toml", "README.md", "README", "README.fr.md",
		"LICENSE", "LICENSE-MIT", "LICENSE.txt",
	}
	if issues := CheckPackage(m, ok, testHost()); len(issues) != 0 {
		t.Errorf("legal package refused: %v", issues)
	}

	bad := []string{
		"bin/tool", "run.sh", "locales/de.toml", "locales/en.toml.bak", "docs/README.md",
		"plugin.toml.orig",
		"readme.md", "script.py", "locales/sub/en.toml", ".hidden",
	}

	got := CheckPackage(m, append(slices.Clone(ok), bad...), testHost())
	if len(got) != len(bad) {
		t.Fatalf("issues = %v, want one per illegal file (%d)", got, len(bad))
	}

	for _, is := range got {
		if is.Code != CodeDeclarativeExtra || is.Severity != SeverityError {
			t.Errorf("issue = %v", is)
		}
	}

	if !slices.IsSortedFunc(got, func(a, b Issue) int {
		if a.Path < b.Path {
			return -1
		}

		if a.Path > b.Path {
			return 1
		}

		return 0
	}) {
		t.Errorf("issues are not sorted by path: %v", got)
	}
}

// TestCheckPackageExec checks that an exec package holds its program for the host.
//
// Parameters:
//   - t: test to report to
func TestCheckPackageExec(t *testing.T) {
	m := mustDecode(t, execM("", nil, ""))

	if issues := CheckPackage(m, []string{"plugin.toml", "bin/demo", "anything/else"},
		testHost()); len(issues) != 0 {
		t.Errorf("exec package refused: %v", issues)
	}

	if issues := CheckPackage(m, []string{"plugin.toml"}, testHost()); len(issues) != 1 ||
		issues[0].Code != CodeBinMissing || issues[0].Path != "exec.bin" {
		t.Errorf("missing program = %v", issues)
	}

	bld := "[[platform]]\nos = \"linux\"\narch = \"amd64\"\nbin = \"bin/lin\"\nasset = \"a.tgz\"\n" +
		"sha256 = \"" + times("a", 64) +
		"\"\n[[platform]]\nos = \"darwin\"\narch = \"arm64\"\nbin = \"bin/mac\"\nasset = \"b.tgz\"\n" +
		"sha256 = \"" + times("b", 64) + "\"\n"
	pm := mustDecode(t, build(TierExec, "", nil, "[exec]\n", bld))

	if issues := CheckPackage(pm, []string{"bin/lin"}, testHost()); len(issues) != 0 {
		t.Errorf("platform package refused: %v", issues)
	}

	if issues := CheckPackage(pm, []string{"bin/mac"}, testHost()); len(issues) != 1 ||
		issues[0].Path != "platform[0].bin" {
		t.Errorf("missing program for the host = %v", issues)
	}
}

// TestExecFiles checks the list of files that get the execute bit.
//
// Parameters:
//   - t: test to report to
func TestExecFiles(t *testing.T) {
	if got := ExecFiles(mustDecode(t, decl("", tool, cmdBody))); len(got) != 0 {
		t.Errorf("declarative ExecFiles = %v", got)
	}

	if got := ExecFiles(mustDecode(t, execM("", nil, ""))); !slices.Equal(got, []string{"bin/demo"}) {
		t.Errorf("exec ExecFiles = %v", got)
	}

	bld := "[[platform]]\nos = \"linux\"\narch = \"amd64\"\nbin = \"bin/x\"\nasset = \"a.tgz\"\n" +
		"sha256 = \"" + times("a", 64) +
		"\"\n[[platform]]\nos = \"linux\"\narch = \"arm64\"\nbin = \"bin/x\"\n" +
		"asset = \"b.tgz\"\nsha256 = \"" + times("b", 64) + "\"\n"
	if got := ExecFiles(mustDecode(t, build(TierExec, "", nil, "[exec]\n", bld))); !slices.Equal(got,
		[]string{"bin/x"}) {
		t.Errorf("platform ExecFiles = %v", got)
	}
}

// TestCheckExecBits checks the execute bits of a tree against the manifest: a program of a
// declarative package or a data file of an exec package must not have one, and the program of the
// host must.
//
// Parameters:
//   - t: test to report to
func TestCheckExecBits(t *testing.T) {
	declarative, _ := decode(decl("", []string{"exec:tool"}, cmdBody))
	execP, rep := decode(execM("", []string{"exec:tool"}, cmdBody))

	if declarative == nil || execP == nil {
		t.Fatalf("fixture manifests refused: %v", rep.Issues)
	}

	tests := []struct {
		name  string
		m     *Manifest
		files []TreeFile
		want  []string
	}{
		{"declarative, no program", declarative, []TreeFile{{Path: "plugin.toml"}}, nil},
		{
			"declarative with a program", declarative,
			[]TreeFile{{Path: "plugin.toml"}, {Path: "run.sh", Exec: true}},
			[]string{CodeExecUndeclared},
		},
		{
			"exec, program ready", execP,
			[]TreeFile{{Path: "plugin.toml"}, {Path: "bin/demo", Exec: true}},
			nil,
		},
		{
			"exec, program without the bit", execP,
			[]TreeFile{{Path: "plugin.toml"}, {Path: "bin/demo"}},
			[]string{CodeBinNotExec},
		},
		{
			"exec, another file with the bit", execP,
			[]TreeFile{{Path: "plugin.toml", Exec: true}, {Path: "bin/demo", Exec: true}},
			[]string{CodeExecUndeclared},
		},
		{"exec, program absent", execP, []TreeFile{{Path: "plugin.toml"}}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, i := range CheckExecBits(tt.m, tt.files, testHost()) {
				got = append(got, i.Code)
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("issues = %v, want %v", got, tt.want)
			}
		})
	}
}
