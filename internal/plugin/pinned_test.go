package plugin

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"testing"
)

// surfaceEveryKey is a declarative manifest that sets every key of API 1 that the surface hash
// reads, on a command, an item action (with the target) and a command that opens an address.
const surfaceEveryKey = `[[command]]
id = "all"
label = "All"
run = ["tool", "{a}", "{b?}", "{c}", "--", "{d}"]
cwd = "{home}"
output = "pager"
timeout = "2m"
confirm = "Go on?"
allow_dash = ["d", "c"]
[command.env]
A = "1"
B = "two"
[[command.field]]
id = "a"
type = "text"
label = "A"
required = true
pattern = "^[a-z]+$"
[[command.field]]
id = "b"
type = "bool"
label = "B"
flag = "-b"
[[command.field]]
id = "c"
type = "choice"
label = "C"
choices = ["x", "y"]
[[command.field]]
id = "d"
type = "text"
label = "D"

[[command]]
id = "term"
label = "Term"
run = ["tool"]
output = "terminal"
pause = "error"

[[command]]
id = "web"
label = "Web"
open = "https://example.org/{q}"
[[command.field]]
id = "q"
type = "text"
label = "Q"

[[item_action]]
id = "act"
target = "repo"
label = "Act"
run = ["tool", "{repo.path}"]
output = "silent"
confirm = true
`

// surfaceEveryKeyHash is the surface hash of surfaceEveryKey. It must never change: a release that
// reads a new manifest key has to leave the hash of a manifest that does not set it alone, or
// every installed plugin asks for consent again. If this test fails, the shape of the surface
// changed; do not edit the value.
const surfaceEveryKeyHash = "e9372fac458114e40c1bed0276e759c82fd7b3cdd08ada3f85baebfcc2899f53"

// surfaceGftHash is the surface hash of the bundled gft entries, which pins.toml also holds.
const surfaceGftHash = "aa61a534bc43b24875969ea4188bb8afff5637e2f8287a6717e4f97938491e6e"

// TestSurfaceHashPinned pins the hash of a manifest that uses every key of API 1 that the surface
// reads, and the hash of the bundled gft. Neither may change when a later release adds a key.
//
// Parameters:
//   - t: test to report to
func TestSurfaceHashPinned(t *testing.T) {
	perms := []string{"exec:tool", "env:A", "env:B", "host:read:repos", "host:open-url"}
	m := mustDecode(t, decl("", perms, surfaceEveryKey))

	if got := SurfaceSHA256(m); got != surfaceEveryKeyHash {
		t.Errorf("surface hash of the every-key manifest = %s, want %s\n%s", got,
			surfaceEveryKeyHash, SurfaceJSON(m))
	}

	data, err := os.ReadFile(filepath.Join("bundled", "plugins", "gft", "plugin.toml"))
	if err != nil {
		t.Fatal(err)
	}

	g, rep := Decode(data, DecodeOptions{Host: testHost(), WantID: "gft", Official: true})
	if g == nil {
		t.Fatalf("gft refused: %v", rep.Issues)
	}

	if got := SurfaceSHA256(g); got != surfaceGftHash {
		t.Errorf("surface hash of gft = %s, want %s", got, surfaceGftHash)
	}
}

// objectKeys lists the keys of a surface object, sorted.
//
// Parameters:
//   - obj: surface object
func objectKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// TestSurfaceZeroValuesAbsent is the guard of the rule "a key counts only when set". An Entry and
// a Field at their zero value must write nothing but table and id (and the type of a field), so a
// field added to either struct and written at its zero value fails here before it can change the
// hash of an installed plugin. It also checks that every key is written once it is set.
//
// Parameters:
//   - t: test to report to
func TestSurfaceZeroValuesAbsent(t *testing.T) {
	if got := objectKeys(entryObject("command", "x", &Entry{})); !slices.Equal(got,
		[]string{"id", "table"}) {
		t.Errorf("a zero Entry writes %v, want only id and table", got)
	}

	if got := objectKeys(fieldObject(Field{ID: "x", Type: "text"})); !slices.Equal(got,
		[]string{"id", "type"}) {
		t.Errorf("a zero Field writes %v, want only id and type", got)
	}

	// An entry with empty but non-nil lists and tables is still at its zero value.
	empty := &Entry{
		Run: []string{}, Env: map[string]string{}, AllowDash: []string{},
		Fields: []Field{},
	}
	if got := objectKeys(entryObject("command", "x", empty)); !slices.Equal(got,
		[]string{"id", "table"}) {
		t.Errorf("an empty Entry writes %v, want only id and table", got)
	}

	full := &Entry{
		Target: "repo", Run: []string{"a"}, Open: "https://example.org/",
		Env: map[string]string{"A": "1"}, Cwd: "{home}", AllowDash: []string{"x"},
		Output: "pager", Pause: "error", Timeout: "1m",
		Confirm: Confirm{Set: true},
		Fields: []Field{{
			ID: "x", Type: "choice", Flag: "-x", Pattern: "^a$", Choices: []string{"a"},
			Required: true,
		}},
	}
	want := []string{
		"allow_dash", "confirm", "cwd", "env", "fields", "id", "open", "output", "pause", "run",
		"table", "target", "timeout",
	}

	if got := objectKeys(entryObject("command", "x", full)); !slices.Equal(got, want) {
		t.Errorf("a full Entry writes %v, want %v", got, want)
	}

	fields := entryObject("command", "x", full)["fields"].([]map[string]any)
	wantField := []string{"choices", "flag", "id", "pattern", "required", "type"}

	if got := objectKeys(fields[0]); !slices.Equal(got, wantField) {
		t.Errorf("a full Field writes %v, want %v", got, wantField)
	}

	// Every field of Entry is either read by entryObject or listed here as display text, so a new
	// field has to be decided on: put it in the surface (written only when set) or in this list.
	display := []string{"Table", "Path", "ID", "Label", "Description", "Keywords", "Key", "broken"}
	typ := reflect.TypeOf(Entry{})

	var undecided []string

	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if !slices.Contains(display, name) && !surfaceReads(name) {
			undecided = append(undecided, name)
		}
	}

	if len(undecided) > 0 {
		t.Errorf("Entry fields neither in the surface nor listed as display text: %v", undecided)
	}
}

// surfaceReads reports whether a field of Entry takes part in the surface: it does when setting it
// adds a key to the object that entryObject builds.
//
// Parameters:
//   - name: name of the field of Entry
func surfaceReads(name string) bool {
	e := Entry{}
	v := reflect.ValueOf(&e).Elem().FieldByName(name)

	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Slice:
		switch v.Type().Elem().Kind() {
		case reflect.String:
			v.Set(reflect.ValueOf([]string{"x"}))
		case reflect.Struct:
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		default:
			return false
		}
	case reflect.Map:
		v.Set(reflect.ValueOf(map[string]string{"x": "y"}))
	case reflect.Struct:
		if f := v.FieldByName("Set"); f.IsValid() {
			f.SetBool(true)
		}
	default:
		return false
	}

	return len(entryObject("command", "x", &e)) > 2
}
