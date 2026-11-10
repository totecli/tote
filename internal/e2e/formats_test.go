package e2e

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runIn runs the binary under test in a sandbox with no terminal.
//
// Parameters:
//   - t: running test
//   - sb: sandbox that holds the directories
//   - args: arguments
//
// Returns the standard output, the standard error and the exit code.
func runIn(t *testing.T, sb *sandbox, args ...string) (string, string, int) {
	t.Helper()

	cmd := exec.Command(sb.tote(), args...)
	cmd.Env = sb.Env()

	var stdout, stderr bytes.Buffer

	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	code := 0

	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}

	return stdout.String(), stderr.String(), code
}

// TestFormatsOfTheRealBinary drives the binary through the formats that scripts and later
// releases rely on: the schema key of the JSON documents, the error object of update, the
// warning for a key that a newer tote wrote, the reserved record names and the development
// version.
//
// Parameters:
//   - t: test to report to
func TestFormatsOfTheRealBinary(t *testing.T) {
	sb := newSandbox(t)

	t.Run("schema is the first key", func(t *testing.T) {
		for _, args := range [][]string{{"version", "--json"}, {"doctor", "--json"}} {
			out, _, _ := runIn(t, sb, args...)

			if !strings.HasPrefix(out, "{\n  \"schema\": 1,\n") {
				t.Errorf("%v: output starts with %.40q", args, out)
			}
		}
	})

	t.Run("update failure is an error object", func(t *testing.T) {
		cmd := exec.Command(sb.tote(), "update", "--json")
		cmd.Env = sb.Env("TOTE_UPDATE_BASE=http://mirror.example.org/tote")

		var stdout, stderr bytes.Buffer

		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		_ = cmd.Run()

		var doc struct {
			Schema int
			OK     bool
			Status string
			Error  struct {
				Code, Message string
				Exit          int
			}
		}

		if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
			t.Fatalf("stdout %q: %v", stdout.String(), err)
		}

		if doc.Schema != 1 || doc.OK || doc.Status != "error" || doc.Error.Code == "" ||
			doc.Error.Message == "" || doc.Error.Exit != 1 {
			t.Errorf("document %+v", doc)
		}

		if stderr.Len() != 0 {
			t.Errorf("stderr %q in JSON mode", stderr.String())
		}
	})

	t.Run("a key of a newer version is a warning", func(t *testing.T) {
		sb.write(t, filepath.Join(sb.Dir, "config", "config.toml"),
			"schema = 1\n[shell]\nbind = true\n[ui]\ntheme = \"neon\"\n")

		out, errOut, code := runIn(t, sb, "alias", "--json")
		if code != 0 || !strings.Contains(out, "\"schema\": 1") {
			t.Errorf("code %d, stdout %.60q", code, out)
		}

		want := "tote: config.toml:3: unknown key shell.bind, ignored\n" +
			"tote: config.toml:5: ui.theme is \"neon\", allowed values: auto, dark, light, mono, " +
			"the default applies\n"
		if errOut != want {
			t.Errorf("stderr %q, want %q", errOut, want)
		}

		doc, _, code := runIn(t, sb, "doctor", "--json")
		if code != 0 || !strings.Contains(doc, "some settings are ignored") {
			t.Errorf("doctor: code %d, output lacks the warning:\n%s", code, doc)
		}

		sb.write(t, filepath.Join(sb.Dir, "config", "config.toml"), "schema = 2\n")

		_, errOut, _ = runIn(t, sb, "alias")
		if !strings.Contains(errOut, "config.toml ignored") {
			t.Errorf("schema 2: stderr %q", errOut)
		}

		sb.write(t, filepath.Join(sb.Dir, "config", "config.toml"), "schema = 1\n")
	})

	t.Run("reserved record names", func(t *testing.T) {
		for _, args := range [][]string{
			{"co", "add", "edit", "--target", "h.example.org"},
			{"co", "add", "h1", "--target", "h.example.org", "--alias", "sync"},
			{"alias", "add", "find", "ls"},
			{"repos", "add", "__tote_pick", "/tmp"},
		} {
			_, errOut, code := runIn(t, sb, args...)
			if code == 0 || !strings.Contains(errOut, "is a reserved name") {
				t.Errorf("%v: code %d, stderr %q", args, code, errOut)
			}
		}
	})

	t.Run("a development build reports dev", func(t *testing.T) {
		out, _, _ := runIn(t, sb, "version")
		if out != "tote dev\n" {
			t.Errorf("version = %q, want the literal dev, not a pseudo-version", out)
		}
	})
}
