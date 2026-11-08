package plugin

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// srcDir writes a source directory of a plugin below the sandbox, as a developer's checkout: files
// 0644, programs 0755 when the path ends in a star, directories 0755.
//
// Parameters:
//   - name: name of the directory below the sandbox
//   - files: files by slash separated path
func (l *lab) srcDir(name string, files map[string]string) string {
	l.t.Helper()

	dir := filepath.Join(l.root, "src", name)

	for rel, content := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(rel, "*") {
			rel, mode = strings.TrimSuffix(rel, "*"), 0o755
		}

		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			l.t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			l.t.Fatal(err)
		}

		if err := os.Chmod(path, mode); err != nil {
			l.t.Fatal(err)
		}
	}

	return dir
}

// manifest builds a declarative manifest for an id and a version, with one command that runs
// "tool" and the given extra tables.
//
// Parameters:
//   - id: plugin id
//   - version: plugin version
//   - perms: permission entries
//   - body: tables after the header
func manifest(id, version string, perms []string, body string) string {
	src := decl("", perms, body)
	src = strings.Replace(src, `id = "demo"`, `id = "`+id+`"`, 1)

	return strings.Replace(src, `version = "1.0.0"`, `version = "`+version+`"`, 1)
}

// cmdWith builds a command table that runs "tool" with the given extra keys.
//
// Parameters:
//   - id: contribution id
//   - extra: keys written inside the table, one per line
func cmdWith(id, extra string) string {
	return "\n[[command]]\nid = \"" + id + "\"\nlabel = \"L\"\nrun = [\"tool\", \"-a\"]\n" +
		"output = \"pager\"\n" + extra
}

// plan runs Plan for a directory and fails the test on an error.
//
// Parameters:
//   - t: test to report to
//   - r: registry
//   - req: request
func plan(t *testing.T, r *Registry, req InstallRequest) *Staged {
	t.Helper()

	st, err := r.Plan(context.Background(), req)
	if err != nil {
		t.Fatalf("Plan(%+v): %v", req, err)
	}

	return st
}

// planErr runs Plan and returns the *Error, failing the test when it succeeds.
//
// Parameters:
//   - t: test to report to
//   - r: registry
//   - req: request
func planErr(t *testing.T, r *Registry, req InstallRequest) *Error {
	t.Helper()

	st, err := r.Plan(context.Background(), req)
	if err == nil {
		_ = st.Abort()

		t.Fatalf("Plan(%+v) succeeded", req)
	}

	return asError(t, err)
}

// asError converts an error to an *Error, failing the test when it is another type.
//
// Parameters:
//   - t: test to report to
//   - err: the error
func asError(t *testing.T, err error) *Error {
	t.Helper()

	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("error %T %v is not an *Error", err, err)
	}

	return pe
}

// installDir installs a source directory with the consent of the user and returns the result.
//
// Parameters:
//   - t: test to report to
//   - r: registry
//   - dir: source directory
func installDir(t *testing.T, r *Registry, dir string) InstallResult {
	t.Helper()

	st := plan(t, r, InstallRequest{Source: dir})

	res, err := st.Commit(Grant{Prompt: true})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	return res
}

// bundle builds a BundledSet over in-memory files, pinned to what the files really hash to.
//
// Parameters:
//   - t: test to report to
//   - l: sandbox, used to hash the pinned tree
//   - files: files of the plugin by path
func bundle(t *testing.T, l *lab, files map[string]string) *fakeBundle {
	t.Helper()

	m, rep := Decode([]byte(files["plugin.toml"]), DecodeOptions{
		Host:     testHost(),
		Official: true,
	})
	if m == nil {
		t.Fatalf("bundled manifest refused: %v", rep.Issues)
	}

	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys[strings.TrimSuffix(name, "*")] = &fstest.MapFile{Data: []byte(content), Mode: 0o444}
	}

	// The pin is what the tree of the files hashes to when it is installed: build it in a
	// directory with the modes of a staged copy.
	dir := l.srcDir("pin-"+m.ID, files)
	for _, rel := range ExecFiles(m) {
		must(t, os.Chmod(filepath.Join(dir, rel), 0o700))
	}

	for _, rel := range []string{"plugin.toml"} {
		must(t, os.Chmod(filepath.Join(dir, rel), 0o600))
	}

	tree, err := HashTree(dir)
	if err != nil {
		t.Fatal(err)
	}

	return &fakeBundle{
		id: m.ID, fsys: fsys, version: m.Version, tree: tree.SHA256,
		surface: SurfaceSHA256(m),
	}
}

// fakeBundle is a BundledSet of one plugin.
type fakeBundle struct {
	id      string
	fsys    fstest.MapFS
	version string
	tree    string
	surface string
}

// Lookup implements BundledSet.
//
// Parameters:
//   - id: plugin id
func (b *fakeBundle) Lookup(id string) (fsys fs.FS, version, tree, surface string, ok bool) {
	if id != b.id {
		return nil, "", "", "", false
	}

	return b.fsys, b.version, b.tree, b.surface, true
}

// IDs implements BundledSet.
func (b *fakeBundle) IDs() []string { return []string{b.id} }
