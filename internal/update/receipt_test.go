package update

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestReceiptRoundTrip checks that a written receipt reads back, with the right modes.
//
// Parameters:
//   - t: test to report to
func TestReceiptRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "install.json")
	want := Receipt{
		Schema: 1, InstallMethod: "script", Version: "1.0.0", Channel: "stable",
		Path: "/home/u/.local/bin/tote", OS: "linux", Arch: "amd64",
		Source: "https://github.com/totecli/tote/releases", VerifiedBy: "minisign",
		ReleaseTimestamp: 1762000000, InstalledAt: "2026-11-03T10:00:00Z",
	}

	if err := WriteReceipt(path, want); err != nil {
		t.Fatal(err)
	}

	got, err := ReadReceipt(path)
	if err != nil || got != want {
		t.Errorf("read = %+v, %v", got, err)
	}

	info, _ := os.Stat(path)
	dir, _ := os.Stat(filepath.Dir(path))

	if info.Mode().Perm() != 0o600 || dir.Mode().Perm() != 0o700 {
		t.Errorf("modes: file %v, directory %v", info.Mode().Perm(), dir.Mode().Perm())
	}

	if names := names(t, filepath.Dir(path)); len(names) != 1 {
		t.Errorf("directory holds %v", names)
	}
}

// TestReadInstallerReceipt checks that the receipt written by install.sh is understood.
//
// Parameters:
//   - t: test to report to
func TestReadInstallerReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.json")
	text := `{
    "schema": 1,
    "install_method": "script",
    "version": "1.1.0-beta.1",
    "channel": "beta",
    "path": "/home/u/.local/bin/tote",
    "os": "linux",
    "arch": "arm64",
    "source": "https://github.com/totecli/tote/releases",
    "verified_by": "openssl",
    "release_timestamp": 1762000000,
    "installed_at": "2026-11-03T10:00:00Z"
}
`

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := ReadReceipt(path)
	if err != nil || r.Channel != "beta" || r.ReleaseTimestamp != 1762000000 || r.Arch != "arm64" {
		t.Errorf("receipt = %+v, %v", r, err)
	}
}

// TestReadReceiptErrors checks the receipts that are refused.
//
// Parameters:
//   - t: test to report to
func TestReadReceiptErrors(t *testing.T) {
	dir := t.TempDir()

	if _, err := ReadReceipt(filepath.Join(dir, "none")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}

	tests := map[string]string{
		"not json":          "not json",
		"empty":             "",
		"wrong schema":      `{"schema": 2}`,
		"no schema":         `{"install_method": "script"}`,
		"wrong type":        `{"schema": 1, "release_timestamp": "x"}`,
		"too large":         `{"schema": 1, "path": "` + strings.Repeat("a", 20000) + `"}`,
		"trailing nonsense": `{"schema": 1} x`,
	}

	for name, text := range tests {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := ReadReceipt(path); !errors.Is(err, ErrReceipt) {
			t.Errorf("%s: error %v, want ErrReceipt", name, err)
		}
	}

	if _, err := ReadReceipt(dir); err == nil {
		t.Error("a directory was read as a receipt")
	}
}

// TestWriteReceiptKeepsOld checks that a failed write leaves the previous receipt.
//
// Parameters:
//   - t: test to report to
func TestWriteReceiptKeepsOld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.json")

	if err := WriteReceipt(path, Receipt{Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}

	// A directory in the place of the file makes the final rename fail.
	other := filepath.Join(t.TempDir(), "install.json")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := WriteReceipt(other, Receipt{Version: "2.0.0"}); err == nil {
		t.Error("write over a directory succeeded")
	}

	if got := names(t, filepath.Dir(other)); len(got) != 1 {
		t.Errorf("temporary file left: %v", got)
	}

	r, err := ReadReceipt(path)
	if err != nil || r.Version != "1.0.0" {
		t.Errorf("old receipt = %+v, %v", r, err)
	}
}

// TestWriteReceiptKeepsUnknownFields checks that rewriting a receipt overwrites the fields that
// this version knows and keeps the others, whatever their type, and that a file that cannot
// supply fields is replaced.
//
// Parameters:
//   - t: test to report to
func TestWriteReceiptKeepsUnknownFields(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		keep     map[string]string
	}{
		{
			"later fields",
			`{"schema":1,"version":"0.9.0","zeta":{"a":[1,2]},"alpha":"x","flag":true,"n":7}`,
			map[string]string{"zeta": `{"a":[1,2]}`, "alpha": `"x"`, "flag": "true", "n": "7"},
		},
		{"no file", "", nil},
		{"not JSON", "{", nil},
		{"another schema", `{"schema":2,"extra":"x"}`, nil},
		{"not an object", `[1]`, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "install.json")
			if tt.existing != "" {
				if err := os.WriteFile(path, []byte(tt.existing), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			want := Receipt{InstallMethod: "script", Version: "1.0.0", ReleaseTimestamp: 5}
			if err := WriteReceipt(path, want); err != nil {
				t.Fatal(err)
			}

			got, err := ReadReceipt(path)
			if err != nil || got.Version != "1.0.0" || got.ReleaseTimestamp != 5 {
				t.Fatalf("receipt = %+v, %v", got, err)
			}

			data, _ := os.ReadFile(path)

			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatalf("%q: %v", data, err)
			}

			for name, raw := range tt.keep {
				var a, b any

				_ = json.Unmarshal(fields[name], &a)
				_ = json.Unmarshal([]byte(raw), &b)

				if !reflect.DeepEqual(a, b) {
					t.Errorf("field %s = %s, want %s", name, fields[name], raw)
				}
			}

			if tt.keep == nil && len(fields) != 11 {
				t.Errorf("fields = %d in %s, want the 11 of the receipt", len(fields), data)
			}

			if !strings.HasPrefix(string(data), "{\n    \"schema\": 1,") {
				t.Errorf("the schema is not first: %s", data)
			}

			if tt.keep != nil {
				keys := topLevelKeys(t, data)
				if tail := keys[len(keys)-len(tt.keep):]; !slices.Equal(tail, []string{
					"alpha", "flag", "n", "zeta",
				}) {
					t.Errorf("the unknown fields end as %v, want them sorted: %v", tail, keys)
				}
			}
		})
	}
}

// topLevelKeys lists the keys of a JSON object in the order of the text.
//
// Parameters:
//   - t: running test
//   - data: JSON text of an object
func topLevelKeys(t *testing.T, data []byte) []string {
	t.Helper()

	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %q", data)
	}

	var keys []string

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}

		keys = append(keys, tok.(string))

		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}

	return keys
}
