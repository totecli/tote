package main

import (
	"strings"
	"testing"
)

// TestConfigWarnsPerKey checks that an unknown key, or a value that is not allowed, is reported
// on stderr with its line and does not stop the command, in the commands that read the settings
// and in the init output, and that the other settings still apply.
//
// Parameters:
//   - t: test to report to
func TestConfigWarnsPerKey(t *testing.T) {
	body := "schema = 1\n[shell]\nbind = true\ncompat_mn = true\n[ui]\ntheme = \"neon\"\n"

	tests := []struct {
		name string
		args []string
	}{
		{"alias list", []string{"alias"}},
		{"co", []string{"co", "-l"}},
		{"init defs", []string{"init", "bash", "--defs"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.fixtures()
			h.write("config.toml", body)

			if code := h.run(tt.args...); code != 0 {
				t.Fatalf("code %d, stderr %q", code, h.errb.String())
			}

			want := "tote: config.toml:3: unknown key shell.bind, ignored\n" +
				"tote: config.toml:6: ui.theme is \"neon\", allowed values: auto, dark, light, " +
				"mono, the default applies\n"
			if h.errb.String() != want {
				t.Errorf("stderr = %q, want %q", h.errb.String(), want)
			}
		})
	}
}

// TestConfigRefusedStillFailsClosed checks that a syntax error and a schema other than 1 still
// make the whole file unusable, with one line on stderr.
//
// Parameters:
//   - t: test to report to
func TestConfigRefusedStillFailsClosed(t *testing.T) {
	for _, body := range []string{"schema = 1\n[ui\n", "schema = 2\n[ui]\ntheme = \"dark\"\n"} {
		h := newHarness(t)
		h.fixtures()
		h.write("config.toml", body)

		if code := h.run("alias"); code != 0 {
			t.Fatalf("code %d, stderr %q", code, h.errb.String())
		}

		if n := strings.Count(h.errb.String(), "config.toml ignored"); n != 1 {
			t.Errorf("%q: warnings = %d in %q, want 1", body, n, h.errb.String())
		}
	}
}
