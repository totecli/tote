package config

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

// TestLoadSettingsHostileShape checks that a config.toml with a table header, a dotted key or an
// inline table of thousands of parts is refused with a *ParseError before the TOML library
// sees it, which would allocate gigabytes, and without a noticeable allocation.
//
// Parameters:
//   - t: test to report to
func TestLoadSettingsHostileShape(t *testing.T) {
	deep := strings.Repeat("a.", 12000) + "a"

	tests := map[string]string{
		"header": "[" + deep + "]\n",
		"dotted": deep + " = 1\n",
		"inline": "x = {" + deep + " = 1}\n",
		"arrays": "x = " + strings.Repeat("[", 20000) + strings.Repeat("]", 20000) + "\n",
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats

			runtime.ReadMemStats(&before)

			_, err := LoadSettings(writeConfig(t, "schema = 1\n"+body))

			runtime.ReadMemStats(&after)

			var perr *ParseError
			if !errors.As(err, &perr) || perr.Line != 2 {
				t.Fatalf("LoadSettings error = %v, want a *ParseError at line 2", err)
			}

			if grown := after.TotalAlloc - before.TotalAlloc; grown > 32<<20 {
				t.Errorf("loading the file allocated %d bytes", grown)
			}
		})
	}
}
