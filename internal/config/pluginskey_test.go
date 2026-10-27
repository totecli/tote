package config

import (
	"errors"
	"testing"
)

// TestPluginsKeyMustBeTable checks that a plugins value that is not a table is an error.
//
// The TOML library drops such a value without an error, which would make a line like
// "plugins = false" pass for a switch that does nothing.
//
// Parameters:
//   - t: test to report to
func TestPluginsKeyMustBeTable(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantLine int
		wantKind string
	}{
		{"integer", "lang = \"en\"\nplugins = 1\n", 2, "Integer"},
		{"bool", "plugins = false\n", 1, "Bool"},
		{"string", "\n\nplugins = \"off\"\n", 3, "String"},
		{"array", "plugins = []\n", 1, "Array"},
		{"array of tables", "[[plugins]]\nenabled = true\n", 1, "ArrayHash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadSettings(writeConfig(t, tt.content))

			var pe *ParseError
			if !errors.As(err, &pe) || pe.Line != tt.wantLine {
				t.Fatalf("LoadSettings error = %#v, want a ParseError at line %d", err, tt.wantLine)
			}
			checkErrorContains(t, err, []string{"plugins must be a table", tt.wantKind})
		})
	}

	for _, content := range []string{"plugins = {}\n", "[plugins]\n"} {
		s, err := LoadSettings(writeConfig(t, content))
		if err != nil || len(s.Plugins) != 0 {
			t.Errorf("LoadSettings(%q) = %v, %v; want no plugin and no error", content, s.Plugins, err)
		}
	}
}
