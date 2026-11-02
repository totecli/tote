package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/i18n"
	"github.com/totecli/tote/internal/store"
)

// newData opens the dataset of a sandbox.
//
// Parameters:
//   - t: running test
//   - s: sandbox
func newData(t *testing.T, s *sandbox) *dataset {
	t.Helper()

	return newDataset(i18n.New("en"), s.Opts.Paths, s.Opts.Home)
}

// TestDatasetCounts checks the number of records of each kind.
//
// Parameters:
//   - t: test to report to
func TestDatasetCounts(t *testing.T) {
	d := newData(t, newSandbox(t))

	for k, want := range map[kind]int{kindConn: 6, kindRepo: 2, kindAlias: 2, kindFunc: 2} {
		if got := d.count(k); got != want {
			t.Errorf("count(%d) = %d, want %d", k, got, want)
		}

		if got := len(d.items(k)); got != want {
			t.Errorf("len(items(%d)) = %d, want %d", k, got, want)
		}

		if d.err(k) != nil || d.readOnlyText(k) != "" {
			t.Errorf("kind %d is read-only: %v", k, d.err(k))
		}
	}
}

// TestDatasetItems checks the prepared text of the connections.
//
// Parameters:
//   - t: test to report to
func TestDatasetItems(t *testing.T) {
	d := newData(t, newSandbox(t))
	byKey := map[string]item{}

	for _, it := range d.items(kindConn) {
		byKey[it.Key] = it

		for _, c := range it.Cells {
			for _, r := range c {
				if unsafeRune(r) {
					t.Errorf("%s: cell %q holds %U", it.Key, c, r)
				}
			}
		}

		if len(it.Cells) != len(kindConn.columns()) {
			t.Errorf("%s: %d cells for %d columns", it.Key, len(it.Cells), len(kindConn.columns()))
		}
	}

	if byKey["legacy"].Warn == "" {
		t.Error("a connection that needs review has no warning")
	}

	if byKey["prod"].Warn != "" {
		t.Errorf("prod warns: %q", byKey["prod"].Warn)
	}

	if got := byKey["prod"].Names; len(got) != 2 || got[1] != "p" {
		t.Errorf("prod names = %v, want the name and its alias", got)
	}

	if !strings.Contains(byKey["prod"].Text, "production web") {
		t.Errorf("prod text = %q", byKey["prod"].Text)
	}

	if got := byKey["emoji"].Cells[3]; strings.ContainsAny(got, "\u200D\uFE0F") {
		t.Errorf("emoji cell keeps modifiers: %q", got)
	}

	if got := byKey["bastion"].Cells[1]; got != "bastion.example.com" {
		t.Errorf("bastion target = %q", got)
	}

	fn := d.items(kindFunc)[0]

	if !strings.Contains(fn.Text, "hello.sh") {
		t.Errorf("a function is not searchable by its script: %q", fn.Text)
	}
}

// TestDatasetItemWithoutTarget checks that the name stands in for a missing target.
//
// Parameters:
//   - t: test to report to
func TestDatasetItemWithoutTarget(t *testing.T) {
	d := newData(t, newSandbox(t))
	it := d.connItem(store.Connection{Name: "solo"}, nil)

	if it.Cells[1] != "solo" {
		t.Errorf("target cell = %q, want the name", it.Cells[1])
	}

	bad := d.connItem(store.Connection{Name: "x", Target: "y"}, store.ErrInvalidField)

	if !strings.Contains(bad.Warn, "invalid") {
		t.Errorf("invalid warning = %q", bad.Warn)
	}
}

// TestDatasetRecord checks the lookup of records and the names of the kinds.
//
// Parameters:
//   - t: test to report to
func TestDatasetRecord(t *testing.T) {
	d := newData(t, newSandbox(t))

	for _, tt := range []struct {
		k   kind
		key string
		ok  bool
	}{
		{kindConn, "prod", true},
		{kindConn, "nope", false},
		{kindRepo, "proj", true},
		{kindAlias, "ll", true},
		{kindAlias, "proj", false},
		{kindFunc, "hello", true},
	} {
		if _, ok := d.record(tt.k, tt.key); ok != tt.ok {
			t.Errorf("record(%d, %q) found = %v, want %v", tt.k, tt.key, ok, tt.ok)
		}
	}

	if d.kindName(kindRepo) != "repository" {
		t.Errorf("kindName(repo) = %q", d.kindName(kindRepo))
	}
}

// TestDatasetSave checks that records are added, replaced and checked like on the command line.
//
// Parameters:
//   - t: test to report to
func TestDatasetSave(t *testing.T) {
	s := newSandbox(t)
	d := newData(t, s)

	if err := d.save(kindAlias, "", store.Alias{Name: "la", Cmd: "ls -A"}); err != nil {
		t.Fatalf("add alias: %v", err)
	}

	data, _ := os.ReadFile(s.Opts.Paths.AliasesFile())
	if !strings.Contains(string(data), `name = "la"`) {
		t.Errorf("the alias was not written: %s", data)
	}

	if err := d.save(kindAlias, "la", store.Alias{Name: "lb", Cmd: "ls -A"}); err != nil {
		t.Fatalf("rename alias: %v", err)
	}

	if _, ok := d.record(kindAlias, "la"); ok {
		t.Error("the old name is still there after a rename")
	}

	tests := []struct {
		name   string
		k      kind
		oldKey string
		rec    any
		want   string
	}{
		{"duplicate", kindAlias, "", store.Alias{Name: "ll", Cmd: "x"}, "already exists"},
		{"missing replace", kindAlias, "ghost", store.Alias{Name: "zz", Cmd: "x"}, "not found"},
		{"reserved", kindAlias, "", store.Alias{Name: "if", Cmd: "x"}, "reserved"},
		{"taken by repo", kindAlias, "", store.Alias{Name: "proj", Cmd: "x"}, "already used"},
		{"taken by func", kindRepo, "", store.Repo{Name: "hello", Path: "/tmp"}, "already used"},
		{"relative repo", kindRepo, "", store.Repo{Name: "rel", Path: "rel/dir"}, "absolute"},
		{
			"relative script", kindFunc, "",
			store.Func{Name: "f", Kind: store.KindSource, Script: "run.sh"},
			"absolute",
		},
		{
			"connection alias clash", kindConn, "",
			store.Connection{Name: "x", Target: "h", Aliases: []string{"P"}},
			"already used",
		},
		{
			"connection name clash", kindConn, "",
			store.Connection{Name: "prod", Target: "h"},
			"already exists",
		},
		{"unsupported", kindAlias, "", 42, "unsupported"},
		{
			"alias with a command word", kindAlias, "",
			store.Alias{Name: "edit", Cmd: "x"},
			"reserved",
		},
		{
			"alias with a helper prefix", kindAlias, "",
			store.Alias{Name: "_tote_x", Cmd: "x"},
			"reserved",
		},
		{
			"repo with a command word", kindRepo, "",
			store.Repo{Name: "Find", Path: "/tmp"},
			"reserved",
		},
		{"function with a helper prefix", kindFunc, "", store.Func{
			Name: "__tote_x", Kind: store.KindSource, Script: "~/x.sh",
		}, "reserved"},
		{
			"connection name with a command word", kindConn, "",
			store.Connection{Name: "rm", Target: "h"},
			"reserved",
		},
		{
			"connection alias with a command word", kindConn, "",
			store.Connection{Name: "okname", Target: "h", Aliases: []string{"import"}},
			"reserved",
		},
		{
			"connection renamed to a command word", kindConn, "prod",
			store.Connection{Name: "status", Target: "h"},
			"reserved",
		},
		{
			"connection name with a helper prefix", kindConn, "",
			store.Connection{Name: "_tote_x", Target: "h"},
			"reserved",
		},
		{
			"connection name with the long helper prefix", kindConn, "",
			store.Connection{Name: "__TOTE_y", Target: "h"},
			"reserved",
		},
		{
			"connection alias with a helper prefix", kindConn, "",
			store.Connection{Name: "okname", Target: "h", Aliases: []string{"ok", "_toteZ"}},
			"reserved",
		},
	}

	for _, tt := range tests {
		err := d.save(tt.k, tt.oldKey, tt.rec)

		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want one with %q", tt.name, err, tt.want)
		}
	}

	if err := d.save(kindConn, "prod", store.Connection{
		Name: "prod", Target: "new.example.com",
		Aliases: []string{"p"},
	}); err != nil {
		t.Errorf("a connection may keep its own alias: %v", err)
	}

	if err := d.save(kindFunc, "", store.Func{
		Name: "fn", Kind: store.KindSource,
		Script: "~/fn.sh",
	}); err != nil {
		t.Errorf("add function: %v", err)
	}
}

// TestDatasetSaveKeepsHeldReservedName checks that editing a record that a hand edited file
// already gave a reserved name is accepted when the name is kept, and refused for a new word.
//
// Parameters:
//   - t: test to report to
func TestDatasetSaveKeepsHeldReservedName(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.ConnectionsFile(),
		"schema = 1\n[[connection]]\nname = \"tags\"\ntarget = \"h\"\naliases = [\"_tote_old\"]\n")

	d := newData(t, s)

	if err := d.save(kindConn, "tags", store.Connection{
		Name: "tags", Target: "h2", Aliases: []string{"_tote_old"},
	}); err != nil {
		t.Errorf("the held names were refused: %v", err)
	}

	err := d.save(kindConn, "tags", store.Connection{
		Name: "tags", Target: "h2", Aliases: []string{"_tote_old", "_tote_new"},
	})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("a new reserved alias gave %v", err)
	}
}

// TestDatasetRemove checks the deletion of a record.
//
// Parameters:
//   - t: test to report to
func TestDatasetRemove(t *testing.T) {
	d := newData(t, newSandbox(t))

	for _, k := range allKinds {
		first := d.items(k)[0].Key

		if err := d.remove(k, first); err != nil {
			t.Errorf("remove(%d, %q): %v", k, first, err)
		}

		if err := d.remove(k, first); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("second remove(%d) = %v, want not found", k, err)
		}
	}
}

// TestDatasetReadOnly checks a file with a syntax error and a file that cannot be opened.
//
// Parameters:
//   - t: test to report to
func TestDatasetReadOnly(t *testing.T) {
	s := newSandbox(t)
	writeFile(t, s.Opts.Paths.ConnectionsFile(), "schema = 1\n[[connection\nname = \"x\"\n")

	if err := os.Remove(s.Opts.Paths.ReposFile()); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(s.Opts.Paths.ReposFile(), 0o700); err != nil {
		t.Fatal(err)
	}

	d := newData(t, s)

	if d.err(kindConn) == nil || !strings.Contains(d.readOnlyText(kindConn), "syntax error") ||
		!strings.Contains(d.readOnlyText(kindConn), "connections.toml") {
		t.Errorf("syntax error text = %q", d.readOnlyText(kindConn))
	}

	if d.err(kindRepo) == nil || !strings.Contains(d.readOnlyText(kindRepo), "cannot be read") {
		t.Errorf("io error text = %q", d.readOnlyText(kindRepo))
	}

	for _, k := range []kind{kindConn, kindRepo} {
		if d.count(k) != 0 || len(d.items(k)) != 0 {
			t.Errorf("kind %d shows records of an unreadable file", k)
		}

		if _, ok := d.record(k, "x"); ok {
			t.Errorf("kind %d finds a record in an unreadable file", k)
		}

		if err := d.save(k, "", store.Alias{Name: "z"}); err == nil {
			t.Errorf("kind %d accepted a write", k)
		}

		if err := d.remove(k, "z"); err == nil {
			t.Errorf("kind %d accepted a deletion", k)
		}
	}

	if _, ok := d.record(kindAlias, "ll"); !ok {
		t.Error("a healthy kind lost its records")
	}
}

// TestDatasetReload checks that a change of the files is seen after a reload.
//
// Parameters:
//   - t: test to report to
func TestDatasetReload(t *testing.T) {
	s := newSandbox(t)
	d := newData(t, s)

	writeFile(t, s.Opts.Paths.AliasesFile(), "schema = 1\n")
	d.reloadKind(kindAlias)

	if d.count(kindAlias) != 0 || d.count(kindConn) != 6 {
		t.Errorf("reloadKind: aliases %d, connections %d", d.count(kindAlias), d.count(kindConn))
	}

	writeFile(t, s.Opts.Paths.ConnectionsFile(), "schema = 1\n")
	d.reload()

	if d.count(kindConn) != 0 {
		t.Errorf("reload: connections %d", d.count(kindConn))
	}
}

// TestUserError checks the unwrapping of the errors shown to the user.
//
// Parameters:
//   - t: test to report to
func TestUserError(t *testing.T) {
	d := newData(t, newSandbox(t))
	err := d.explain(kindAlias, "x", store.ErrConflict)

	var ue *userError
	if !strings.Contains(err.Error(), "changed") || !asUserError(err, &ue) ||
		ue.Unwrap() != store.ErrConflict {
		t.Errorf("conflict error = %v", err)
	}

	err = d.explain(kindAlias, "x", os.ErrPermission)

	if !strings.Contains(err.Error(), "permission") {
		t.Errorf("generic error = %v", err)
	}
}

// asUserError reports whether an error is a *userError and stores it.
//
// Parameters:
//   - err: error to test
//   - out: receives the error when it matches
func asUserError(err error, out **userError) bool {
	ue, ok := err.(*userError)
	*out = ue

	return ok
}
