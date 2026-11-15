package plugin

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// surfaceChoices is a manifest that sets choices_from on two fields of a command and on a field of
// an item action: composite and optional placeholders, a setting, the repository and an earlier
// field.
const surfaceChoices = `[[setting]]
id = "pre"
type = "bool"
label = "Pre"
default = false

[[command]]
id = "get"
label = "Get"
run = ["tool", "--", "{slug}", "{tag?}"]
output = "pager"
[[command.field]]
id = "slug"
type = "text"
label = "Slug"
required = true
[[command.field]]
id = "tag"
type = "text"
label = "Tag"
pattern = "^[a-z0-9]+$"
choices_from = ["tool", "--list", "--pre={setting.pre}", "--", "{slug}"]
[[command.field]]
id = "asset"
type = "text"
label = "Asset"
choices_from = ["tool", "--assets", "--", "{slug}", "{tag?}"]

[[item_action]]
id = "pick"
target = "repo"
label = "Pick"
run = ["tool", "--", "{repo.slug}", "{tag}"]
output = "pager"
[[item_action.field]]
id = "tag"
type = "text"
label = "Tag"
choices_from = ["tool", "--list", "--", "{repo.slug}"]
`

// surfaceChoicesHash freezes the canonical encoding of choices_from. It must never change: a
// consented plugin would ask again.
const surfaceChoicesHash = "13b72a94044f50080f87f9f978c693a1c5b8b93a2ac574d266228a5478446cc5"

// TestSurfaceChoicesPinned pins the canonical encoding of choices_from: the key sits between
// choices and flag.
//
// Parameters:
//   - t: test to report to
func TestSurfaceChoicesPinned(t *testing.T) {
	m := mustDecode(t, decl("", []string{"exec:tool", "host:read:repos"}, surfaceChoices))

	raw := string(SurfaceJSON(m))

	for _, want := range []string{
		`{"choices_from":["tool","--list","--pre={setting.pre}","--","{slug}"],"id":"tag",` +
			`"pattern":"^[a-z0-9]+$","type":"text"}`,
		`{"choices_from":["tool","--assets","--","{slug}","{tag?}"],"id":"asset","type":"text"}`,
		`{"choices_from":["tool","--list","--","{repo.slug}"],"id":"tag","type":"text"}`,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("surface lacks %s:\n%s", want, raw)
		}
	}

	if got := SurfaceSHA256(m); got != surfaceChoicesHash {
		t.Errorf("surface hash of the choices_from manifest = %s, want %s\n%s", got,
			surfaceChoicesHash, raw)
	}
}

// TestSurfaceChoicesOnlyWhenSet checks that an unset choices_from writes no key, so the hash of a
// plugin that does not use it does not move, and that a set one changes the hash.
//
// Parameters:
//   - t: test to report to
func TestSurfaceChoicesOnlyWhenSet(t *testing.T) {
	for _, f := range []Field{
		{ID: "x", Type: "text"},
		{ID: "x", Type: "text", ChoicesFrom: []string{}},
		{ID: "x", Type: "text", ChoicesFrom: nil},
	} {
		if _, has := fieldObject(f)["choices_from"]; has {
			t.Errorf("field %+v writes choices_from", f)
		}
	}

	full := Field{ID: "x", Type: "text", ChoicesFrom: []string{"a"}}

	if _, has := fieldObject(full)["choices_from"]; !has {
		t.Error("a field with choices_from does not write it")
	}

	base := mustDecode(t, surfaceBase("", "", ""))
	with := mustDecode(t, strings.Replace(surfaceBase("", "", ""),
		"label = \"X\"\n", "label = \"X\"\nchoices_from = [\"tool\", \"--list\"]\n", 1))

	if SurfaceSHA256(base) == SurfaceSHA256(with) {
		t.Error("adding choices_from does not change the surface hash")
	}

	other := mustDecode(t, strings.Replace(surfaceBase("", "", ""),
		"label = \"X\"\n", "label = \"X\"\nchoices_from = [\"tool\", \"--list\", \"-a\"]\n", 1))

	if SurfaceSHA256(with) == SurfaceSHA256(other) {
		t.Error("changing the choices_from argv does not change the surface hash")
	}
}

// TestFieldFieldsAreDecided is the guard of Field like the one of Entry: a new member of Field must
// be written by fieldObject when set, or be listed here as display text or a derived value.
//
// Parameters:
//   - t: test to report to
func TestFieldFieldsAreDecided(t *testing.T) {
	display := []string{"Path", "Label", "Hint", "Default", "HasDefault", "MaxLen", "broken"}
	read := []string{"ID", "Type", "Flag", "Pattern", "Choices", "ChoicesFrom", "Required"}

	typ := reflect.TypeOf(Field{})

	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if !slices.Contains(display, name) && !slices.Contains(read, name) {
			t.Errorf("Field.%s is neither read by fieldObject nor listed as display text", name)
		}
	}
}

// TestSurfaceChoicesEscalation checks the escalation of choices: a field that gains a choices_from,
// or whose argv changes, asks again; a kept argv, a removed key and an unrelated change do not.
//
// Parameters:
//   - t: test to report to
func TestSurfaceChoicesEscalation(t *testing.T) {
	plain := surfaceBase("", "", "")
	list := func(argv string) string {
		return strings.Replace(plain, "label = \"X\"\n", "label = \"X\"\nchoices_from = ["+argv+"]\n", 1)
	}
	one, two := list(`"tool", "--list"`), list(`"tool", "--list", "-a"`)
	esc := []SurfaceChange{{EscChoices, "command", "go"}}

	tests := []struct {
		name      string
		old, next string
		want      []SurfaceChange
	}{
		{"added", plain, one, esc},
		{"argv changed", one, two, esc},
		{"kept", one, one, nil},
		{"removed", one, plain, nil},
		{"with confirm added", one, strings.Replace(one, "output = \"pager\"\n",
			"output = \"pager\"\nconfirm = true\n", 1), nil},
		{"new command", plain, plain + strings.Replace(one[strings.Index(one, "[[command]]"):],
			"id = \"go\"", "id = \"fresh\"", 1), []SurfaceChange{{EscChoices, "command", "fresh"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SurfaceEscalations(mustDecode(t, tt.old), mustDecode(t, tt.next))
			if !slices.Equal(got, tt.want) {
				t.Errorf("changes = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCommandsCarrySuggestions checks the suggestions of a command: field order, diff and the
// documented JSON shape.
//
// Parameters:
//   - t: test to report to
func TestCommandsCarrySuggestions(t *testing.T) {
	perms := []string{"exec:tool", "host:read:repos"}
	m := mustDecode(t, decl("", perms, surfaceChoices))

	var get, pick CommandRef

	for _, c := range Commands(m) {
		switch c.ID {
		case "get":
			get = c
		case "pick":
			pick = c
		}
	}

	if len(get.Suggestions) != 2 || get.Suggestions[0].Field != "tag" ||
		get.Suggestions[1].Field != "asset" {
		t.Fatalf("suggestions of get = %+v", get.Suggestions)
	}

	if !slices.Equal(pick.Suggestions[0].Run, []string{"tool", "--list", "--", "{repo.slug}"}) {
		t.Errorf("suggestion of pick = %+v", pick.Suggestions)
	}

	raw, err := json.Marshal(get.Suggestions[1])
	if err != nil {
		t.Fatal(err)
	}

	want := `{"field":"asset","run":["tool","--assets","--","{slug}","{tag?}"]}`
	if string(raw) != want {
		t.Errorf("suggestion JSON = %s, want %s", raw, want)
	}

	more := strings.Replace(surfaceChoices, `"--assets"`, `"--assets", "-x"`, 1)
	changed := mustDecode(t, decl("", perms, more))

	diff := DiffCommands(m, changed)
	if len(diff) != 1 || diff[0].Kind != "changed" || diff[0].New.ID != "get" {
		t.Errorf("diff of a changed suggestion = %+v", diff)
	}

	if DiffCommands(m, m) != nil {
		t.Errorf("identical manifests differ: %+v", DiffCommands(m, m))
	}
}

// TestConsentShowsSuggestions checks that the consent view lists the program of the suggestions of
// a command, and that the diff of an update shows the old and the new one.
//
// Parameters:
//   - t: test to report to
func TestConsentShowsSuggestions(t *testing.T) {
	perms := []string{"exec:tool", "host:read:repos"}
	m := mustDecode(t, decl("", perms, surfaceChoices))
	cmd := Commands(m)[0]

	for _, c := range Commands(m) {
		if c.ID == "get" {
			cmd = c
		}
	}

	lines := commandExtras(cmd)
	if len(lines) != 2 || lines[0].Key != "consent_cmd_choices" || lines[0].Args[0].Text != "tag" ||
		!strings.Contains(lines[0].Args[1].Text, "--pre={setting.pre}") {
		t.Errorf("extras = %+v", lines)
	}

	more := strings.Replace(surfaceChoices, `"--assets"`, `"--assets", "-x"`, 1)
	old := mustDecode(t, decl("", perms, more))

	v := &ConsentView{}
	v.commandDiff(DiffCommands(old, m))

	var keys []string

	for _, l := range v.Lines {
		keys = append(keys, l.Level+":"+l.Key)
	}

	if !slices.Contains(keys, LevelDrop+":consent_cmd_choices") || !slices.Contains(keys,
		LevelAdd+":consent_cmd_choices") {
		t.Errorf("the diff does not show the suggestions: %v", keys)
	}
}
