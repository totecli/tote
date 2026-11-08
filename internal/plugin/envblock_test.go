package plugin

import "testing"

// TestReviewEnvTableBlockedNames pins the last line of defence for the variables that tote sets
// itself from the plugin's [env] table: a name on the block list never reaches the program, even
// when the manifest check upstream let it through. Removing the check in buildEnv was not caught
// by any test (the earlier "killed" result came from an unrelated flaky test).
//
// Parameters:
//   - t: test to report to
func TestReviewEnvTableBlockedNames(t *testing.T) {
	l := newLab(t)
	r := l.reg(nil)

	table := map[string]string{
		"LD_PRELOAD": "/tmp/evil.so", "DYLD_INSERT_LIBRARIES": "/tmp/evil.dylib", "PATH": "/tmp",
		"BASH_ENV": "/tmp/rc", "GIT_DIR": "/tmp/g", "TOTE_PLUGIN_ID": "spoofed",
		"PYTHONPATH": "/tmp", "FOO": "bar longer than eight",
	}

	env, secrets := r.buildEnv(Plugin{ID: "demo", Manifest: &Manifest{}}, "/usr/bin", Avoid{},
		envPlan{Table: table})

	got := envMap(env)

	for name := range table {
		switch name {
		case "FOO":
			if got[name] != table[name] {
				t.Errorf("FOO = %q, want the value of the table", got[name])
			}
		case "PATH":
			if got[name] != "/usr/bin" {
				t.Errorf("PATH = %q, want the scrubbed host PATH", got[name])
			}
		case "TOTE_PLUGIN_ID":
			if got[name] != "demo" {
				t.Errorf("TOTE_PLUGIN_ID = %q, want the real id", got[name])
			}
		default:
			if v, ok := got[name]; ok {
				t.Errorf("blocked variable %s reached the program with %q", name, v)
			}
		}
	}

	for _, s := range secrets {
		if s == table["LD_PRELOAD"] {
			t.Errorf("a refused value was registered as a secret: %v", secrets)
		}
	}
}
