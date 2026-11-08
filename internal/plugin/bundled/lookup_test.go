package bundled

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

// TestLookupChoosesTheTreeById checks that the files returned for an id are the ones of its own
// directory, which is what lets a second bundled plugin be added with no change of Go code.
//
// Parameters:
//   - t: test to report to
func TestLookupChoosesTheTreeById(t *testing.T) {
	all := fstest.MapFS{
		"plugins/alpha/plugin.toml": {Data: []byte("alpha")},
		"plugins/zulu/plugin.toml":  {Data: []byte("zulu")},
	}

	pins := []Pin{
		{ID: "alpha", Version: "1.0.0", Tree: "t1", Surface: "s1"},
		{ID: "zulu", Version: "2.0.0", Tree: "t2", Surface: "s2"},
		{ID: "nodir", Version: "3.0.0", Tree: "t3", Surface: "s3"},
	}

	tests := []struct {
		id      string
		ok      bool
		content string
		version string
		tree    string
		surface string
	}{
		{"alpha", true, "alpha", "1.0.0", "t1", "s1"},
		{"zulu", true, "zulu", "2.0.0", "t2", "s2"},
		{"unknown", false, "", "", "", ""},
		{"", false, "", "", "", ""},
		{"nodir", true, "", "3.0.0", "t3", "s3"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			fsys, version, tree, surface, ok := lookup(all, pins, tt.id)
			if ok != tt.ok || version != tt.version || tree != tt.tree || surface != tt.surface {
				t.Fatalf("lookup = %q %q %q %v", version, tree, surface, ok)
			}

			if !ok {
				if fsys != nil {
					t.Error("a file system for an id that is not bundled")
				}

				return
			}

			data, err := fs.ReadFile(fsys, "plugin.toml")
			if tt.content == "" {
				if err == nil {
					t.Errorf("a plugin without a directory has a manifest: %q", data)
				}

				return
			}

			if err != nil || string(data) != tt.content {
				t.Errorf("plugin.toml = %q, %v, want %q", data, err, tt.content)
			}
		})
	}
}
