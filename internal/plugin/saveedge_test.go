package plugin

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestSaveSettingsFileProblems checks the refusals that come from the settings file and from what
// is on disk: a tree without a record is not an installed plugin, a file in a layout that cannot be
// edited is refused with its shape, and a file that another process holds is reported as busy.
//
// Parameters:
//   - t: test to report to
func TestSaveSettingsFileProblems(t *testing.T) {
	twelve := map[string]any{"limit": int64(12)}

	t.Run("a tree without a record", func(t *testing.T) {
		l := newLab(t)
		l.install("demo", demoFiles("demo", settingsManifest))
		l.writeFile(filepath.Join(l.root, "data", "plugins", "orphan", "plugin.toml"), "x")

		reg := l.registry(nil)
		p, _ := reg.Load().Get("demo")
		p.ID = "orphan"

		if e := asError(t, reg.SaveSettings(p, twelve)); e.Code != CodeNotInstalled {
			t.Errorf("error = %+v, want %s", e, CodeNotInstalled)
		}
	})

	t.Run("a layout that cannot be edited", func(t *testing.T) {
		l := newLab(t)
		l.install("demo", demoFiles("demo", settingsManifest))

		file := filepath.Join(l.root, "config", "plugins", "demo.toml")
		l.writeFile(file, "schema = 1\nlimit.deep = 1\n")

		reg := l.registry(nil)
		p, _ := reg.Load().Get("demo")

		e := asError(t, reg.SaveSettings(p, twelve))
		if e.Code != CodeSettingsFile || len(e.Args) == 0 || e.Args[0] != RejectShape {
			t.Errorf("error = %+v, want %s %s", e, CodeSettingsFile, RejectShape)
		}
	})

	t.Run("a file that is held", func(t *testing.T) {
		if testing.Short() {
			t.Skip("waits for the lock of the file")
		}

		l := newLab(t)
		l.install("demo", demoFiles("demo", settingsManifest))

		file := filepath.Join(l.root, "config", "plugins", "demo.toml")
		l.writeFile(file, "schema = 1\n")

		held, err := os.OpenFile(file+".lock", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = held.Close() }()

		if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}

		reg := l.registry(nil)
		p, _ := reg.Load().Get("demo")

		if e := asError(t, reg.SaveSettings(p, twelve)); e.Code != CodeBusy {
			t.Errorf("error = %+v, want %s", e, CodeBusy)
		}
	})
}

// TestCheckSettingTextKinds checks that the text of a form is read by the type of its setting: a
// bool is read without regard to case and a list is not typed on one line.
//
// Parameters:
//   - t: test to report to
func TestCheckSettingTextKinds(t *testing.T) {
	m := settingsFor(t)

	for _, s := range m.Settings {
		switch s.ID {
		case "verbose":
			for _, raw := range []string{"True", "FALSE", "tRuE"} {
				if v, code := CheckSettingText(s, t.TempDir(), raw); code != "" {
					t.Errorf("bool %q = %v, %q", raw, v, code)
				}
			}
		case "tags":
			if v, code := CheckSettingText(s, t.TempDir(), "x"); code != RejectType {
				t.Errorf("a list typed as text = %v, %q, want %q", v, code, RejectType)
			}
		}
	}
}
