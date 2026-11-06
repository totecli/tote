package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

// surfaceBase is a declarative manifest with one confirmed command, an allow_dash name and an env
// entry, the three things whose change asks for a new consent.
//
// Parameters:
//   - confirm: lines of the confirm key, empty for none
//   - dash: lines of the allow_dash key, empty for none
//   - env: lines of an env table, empty for none
func surfaceBase(confirm, dash, env string) string {
	body := "[[command]]\nid = \"go\"\nlabel = \"Go\"\nrun = [\"tool\", \"{x}\"]\n" +
		"output = \"pager\"\n" + confirm + dash + env +
		"[[command.field]]\nid = \"x\"\ntype = \"text\"\nlabel = \"X\"\n"

	return decl("", []string{"exec:tool", "env:A", "env:B"}, body)
}

// TestSurfaceCanonical checks the canonical JSON: sorted keys, no spaces, no HTML escaping, fixed
// shape, and a stable hash.
//
// Parameters:
//   - t: test to report to
func TestSurfaceCanonical(t *testing.T) {
	m := mustDecode(t, surfaceBase("confirm = \"Run <b> & go?\"\n", "allow_dash = [\"x\"]\n", ""))

	raw := SurfaceJSON(m)
	if strings.ContainsAny(string(raw), "\n ") && !strings.Contains(string(raw), "Run <b> & go?") {
		t.Errorf("JSON holds whitespace outside strings: %s", raw)
	}

	if !strings.Contains(string(raw), "Run <b> & go?") || strings.Contains(string(raw), "\\u003c") {
		t.Errorf("HTML characters were escaped: %s", raw)
	}

	want := `[{"allow_dash":["x"],"confirm":"Run <b> & go?","fields":[{"id":"x","type":"text"}],` +
		`"id":"go","output":"pager","run":["tool","{x}"],"table":"command"}]`
	if string(raw) != want {
		t.Errorf("canonical JSON =\n%s\nwant\n%s", raw, want)
	}

	var back []map[string]any
	if err := json.Unmarshal(raw, &back); err != nil || len(back) != 1 {
		t.Errorf("surface is not valid JSON: %v", err)
	}

	for i := 0; i < 50; i++ {
		if SurfaceSHA256(mustDecode(t, surfaceBase("confirm = \"Run <b> & go?\"\n",
			"allow_dash = [\"x\"]\n", ""))) !=
			SurfaceSHA256(m) {
			t.Fatal("the surface hash is not deterministic")
		}
	}

	if h := SurfaceSHA256(m); len(h) != 64 || strings.Trim(h, "0123456789abcdef") != "" {
		t.Errorf("hash = %q", h)
	}
}

// TestSurfaceInputs checks what does and does not take part in the hash.
//
// Parameters:
//   - t: test to report to
func TestSurfaceInputs(t *testing.T) {
	base := mustDecode(t, surfaceBase("", "", ""))
	hash := SurfaceSHA256(base)

	// Texts that only describe the command do not change the surface.
	same := strings.NewReplacer("label = \"Go\"", "label = \"Other label\"").Replace(surfaceBase("",
		"", ""))
	same = strings.Replace(same, "output = \"pager\"\n",
		"output = \"pager\"\ndescription = \"d\"\nkeywords = [\"k\"]\nkey = \"x\"\n", 1)
	same = strings.Replace(same, "label = \"X\"", "label = \"Y\"\nhint = \"h\"", 1)

	if got := SurfaceSHA256(mustDecode(t, same)); got != hash {
		t.Errorf("a label, description, keyword, key or hint changed the surface hash")
	}

	// Everything that runs does.
	changes := map[string]string{
		"argv": strings.Replace(surfaceBase("", "", ""), "\"{x}\"", "\"--v\", \"{x}\"", 1),
		"output": strings.Replace(surfaceBase("", "", ""), "output = \"pager\"",
			"output = \"silent\"", 1),
		"confirm":    surfaceBase("confirm = true\n", "", ""),
		"allow_dash": surfaceBase("", "allow_dash = [\"x\"]\n", ""),
		"env":        surfaceBase("", "", "[command.env]\nA = \"1\"\n"),
		"field type": strings.Replace(surfaceBase("", "", ""), "type = \"text\"",
			"type = \"text\"\nrequired = true", 1),
		"timeout": surfaceBase("timeout = \"1m\"\n", "", ""),
	}

	for name, src := range changes {
		m, rep := decode(src)
		if m == nil {
			t.Fatalf("%s: %v", name, rep.Issues)
		}

		if SurfaceSHA256(m) == hash {
			t.Errorf("a change of %s left the surface hash alone", name)
		}
	}
}

// TestSurfaceExec checks the exec tier: [exec], [[platform]] and the list contributions.
//
// Parameters:
//   - t: test to report to
func TestSurfaceExec(t *testing.T) {
	bld := func(sum string) string {
		return "[[platform]]\nos = \"linux\"\narch = \"amd64\"\nbin = \"bin/x\"\nasset = \"x.tgz\"\n" +
			"sha256 = \"" +
			times(sum, 64) + "\"\n"
	}

	lst := "[[list]]\nid = \"l\"\nlabel = \"L\"\ntimeout = \"1m\"\n[[list.column]]\nid = \"c\"\n" +
		"label = \"C\"\n" +
		"[[list.action]]\nid = \"a\"\nlabel = \"A\"\nrun = [\"tool\"]\noutput = \"pager\"\n"

	one := mustDecode(t, build(TierExec, "", tool, "[exec]\n", bld("a")+lst))
	two := mustDecode(t, build(TierExec, "", tool, "[exec]\n", bld("b")+lst))

	if SurfaceSHA256(one) == SurfaceSHA256(two) {
		t.Error("a platform hash is not part of the surface")
	}

	raw := string(SurfaceJSON(one))
	for _, want := range []string{
		`"table":"platform"`, `"id":"linux/amd64"`, `"table":"list"`,
		`"table":"list.action"`, `"id":"l.a"`, `"table":"exec"`, `"modes":["list","run"]`,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("surface lacks %s:\n%s", want, raw)
		}
	}

	// Declared modes replace the default ones.
	withModes := mustDecode(t, execM("", tool, lst))
	m2 := mustDecode(t, strings.Replace(execM("", tool, lst), "bin = \"bin/demo\"\n",
		"bin = \"bin/demo\"\nmodes = [\"run\", \"list\"]\n", 1))

	if !strings.Contains(string(SurfaceJSON(m2)), `"modes":["run","list"]`) ||
		SurfaceSHA256(m2) == SurfaceSHA256(withModes) {
		t.Errorf("declared modes are not in the surface: %s", SurfaceJSON(m2))
	}
}

// TestSurfaceEscalations checks the rules of decision (f), one by one and together.
//
// Parameters:
//   - t: test to report to
func TestSurfaceEscalations(t *testing.T) {
	plain := surfaceBase("", "", "")
	confirmed := surfaceBase("confirm = true\n", "", "")
	text := surfaceBase("confirm = \"Sure?\"\n", "", "")
	dash := surfaceBase("", "allow_dash = [\"x\"]\n", "")
	envA := surfaceBase("", "", "[command.env]\nA = \"1\"\n")
	envAB := surfaceBase("", "", "[command.env]\nA = \"1\"\nB = \"2\"\n")

	tests := []struct {
		name      string
		old, next string
		want      []SurfaceChange
	}{
		{"identical", confirmed, confirmed, nil},
		{"confirm removed", confirmed, plain, []SurfaceChange{{EscConfirmRemoved, "command", "go"}}},
		{"confirm text removed", text, plain, []SurfaceChange{{EscConfirmRemoved, "command", "go"}}},
		{"confirm text to generic", text, confirmed, nil},
		{"confirm added", plain, confirmed, nil},
		{"allow_dash added", plain, dash, []SurfaceChange{{EscAllowDashAdded, "command", "go"}}},
		{"allow_dash kept", dash, dash, nil},
		{"allow_dash removed", dash, plain, nil},
		{"env added", plain, envA, []SurfaceChange{{EscEnvAdded, "command", "go"}}},
		{"env name added", envA, envAB, []SurfaceChange{{EscEnvAdded, "command", "go"}}},
		{"env removed", envAB, envA, nil},
		{
			"env value changed", envA, strings.Replace(envA, "A = \"1\"", "A = \"2\"", 1),
			[]SurfaceChange{{EscEnvChanged, "command", "go"}},
		},
		{"argv changed", plain, strings.Replace(plain, "\"tool\", \"{x}\"", "\"tool\", \"-v\", \"{x}\"",
			1), nil},
		{
			"several", confirmed, dash,
			[]SurfaceChange{{EscAllowDashAdded, "command", "go"}, {
				EscConfirmRemoved, "command",
				"go",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SurfaceEscalations(mustDecode(t, tt.old), mustDecode(t, tt.next))
			if len(got) != len(tt.want) {
				t.Fatalf("changes = %v, want %v", got, tt.want)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("change %d = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}

	// A new command counts as gaining all of its names, but a new confirmed one asks for nothing.
	other := strings.Replace(dash, "id = \"go\"", "id = \"fresh\"", 1)
	if got := SurfaceEscalations(mustDecode(t, plain), mustDecode(t, other)); len(got) != 1 ||
		got[0] != (SurfaceChange{EscAllowDashAdded, "command", "fresh"}) {
		t.Errorf("new command with allow_dash = %v", got)
	}

	// A platform build hash changes under the same version only.
	bld := func(sum string) string {
		return "[[platform]]\nos = \"linux\"\narch = \"amd64\"\nbin = \"bin/x\"\nasset = \"x.tgz\"\n" +
			"sha256 = \"" +
			times(sum, 64) + "\"\n"
	}

	a := mustDecode(t, build(TierExec, "", nil, "[exec]\n", bld("a")))
	b := mustDecode(t, build(TierExec, "", nil, "[exec]\n", bld("b")))

	if got := SurfaceEscalations(a, b); len(got) != 1 || got[0] != (SurfaceChange{
		EscPlatformChanged,
		"platform", "linux/amd64",
	}) {
		t.Errorf("same version, new hash = %v", got)
	}

	b.Version = "1.0.1"

	if got := SurfaceEscalations(a, b); len(got) != 0 {
		t.Errorf("new version, new hash = %v", got)
	}
}

// TestDiffCommands checks the added, removed and changed commands of the consent diff.
//
// Parameters:
//   - t: test to report to
func TestDiffCommands(t *testing.T) {
	old := mustDecode(t, decl("", []string{"exec:tool", "host:open-url"},
		"[[command]]\nid = \"a\"\nlabel = \"A\"\nrun = [\"tool\", \"one\"]\noutput = \"pager\"\n"+
			"[[command]]\nid = \"b\"\nlabel = \"B\"\nrun = [\"tool\", \"two\"]\noutput = \"pager\"\n"+
			"[[command]]\nid = \"c\"\nlabel = \"C\"\nopen = \"https://x.example/a\"\n"))
	next := mustDecode(t, decl("", []string{"exec:tool", "host:open-url"},
		"[[command]]\nid = \"a\"\nlabel = \"Renamed\"\nrun = [\"tool\", \"one\"]\noutput = \"pager\"\n"+
			"[[command]]\nid = \"b\"\nlabel = \"B\"\nrun = [\"tool\", \"changed\"]\noutput = \"pager\"\n"+
			"[[command]]\nid = \"c\"\nlabel = \"C\"\nopen = \"https://x.example/b\"\n"+
			"[[command]]\nid = \"d\"\nlabel = \"D\"\nrun = [\"tool\", \"new\"]\noutput = \"pager\"\n"))

	got := DiffCommands(old, next)
	kinds := ""

	for _, c := range got {
		kinds += c.Kind + ":" + c.New.ID + c.Old.ID + " "
	}

	if kinds != "changed:bb changed:cc added:d " {
		t.Errorf("diff = %q", kinds)
	}

	if got := DiffCommands(next, old); len(got) != 3 || got[2].Kind != "removed" ||
		got[2].Old.ID != "d" {
		t.Errorf("reverse diff = %+v", got)
	}

	if got := DiffCommands(old, old); len(got) != 0 {
		t.Errorf("identical diff = %+v", got)
	}

	cmds := Commands(next)
	if len(cmds) != 4 || cmds[0].ID != "a" || cmds[3].ID != "d" || cmds[2].Open == "" {
		t.Errorf("Commands = %+v", cmds)
	}
}
