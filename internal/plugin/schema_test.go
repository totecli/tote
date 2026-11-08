package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"testing"
)

// schemaFile is the JSON Schema of plugin.toml, as published with the documentation.
var schemaFile = filepath.Join("..", "..", "docs", "plugins", "plugin.schema.json")

// manifestKeys maps each field of Manifest to the top-level key of plugin.toml that fills it. A
// new field in Manifest fails TestSchemaListsEveryKey until it is listed here and in the schema.
var manifestKeys = map[string]string{
	"API": "api", "ID": "id", "Version": "version", "Tote": "tote", "Title": "title",
	"Description": "description", "License": "license", "Homepage": "homepage", "Tier": "tier",
	"RequiresFeatures": "requires_features", "Platforms": "platforms",
	"Permissions": "permissions", "Requires": "requires", "Exec": "exec", "Builds": "platform",
	"Commands": "command", "ItemActions": "item_action", "Actions": "action", "Lists": "list",
	"Settings": "setting", "I18n": "i18n",
}

// loadSchema reads the schema file as generic JSON.
//
// Parameters:
//   - t: test to report to
func loadSchema(t *testing.T) map[string]any {
	t.Helper()

	data, err := os.ReadFile(schemaFile)
	if err != nil {
		t.Skipf("the schema is not readable: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s is not valid JSON: %v", schemaFile, err)
	}

	return doc
}

// schemaStrings reads a JSON array of strings and returns it sorted.
//
// Parameters:
//   - t: test to report to
//   - v: value read by encoding/json
func schemaStrings(t *testing.T, v any) []string {
	t.Helper()

	list, ok := v.([]any)
	if !ok {
		t.Fatalf("expected an array, found %T", v)
	}

	out := make([]string, len(list))

	for i, e := range list {
		s, isStr := e.(string)
		if !isStr {
			t.Fatalf("expected a string, found %T", e)
		}

		out[i] = s
	}

	sort.Strings(out)

	return out
}

// sub walks a path of object keys.
//
// Parameters:
//   - t: test to report to
//   - v: value to start from
//   - path: keys to follow; a number selects an element of an array
func sub(t *testing.T, v any, path ...any) any {
	t.Helper()

	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("no object before %q", k)
			}

			v = m[k]
		case int:
			l, ok := v.([]any)
			if !ok || k >= len(l) {
				t.Fatalf("no element %d", k)
			}

			v = l[k]
		}
	}

	return v
}

// TestSchemaIsJSON checks that the schema parses and has the shape of a 2020-12 object schema
// that refuses unknown keys.
//
// Parameters:
//   - t: test to report to
func TestSchemaIsJSON(t *testing.T) {
	doc := loadSchema(t)

	if doc["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema = %v", doc["$schema"])
	}

	if doc["type"] != "object" || doc["additionalProperties"] != false {
		t.Errorf("the root must be an object that refuses unknown keys")
	}

	if _, ok := doc["$defs"].(map[string]any); !ok {
		t.Errorf("the schema has no $defs")
	}
}

// TestSchemaListsEveryKey checks that the top-level properties of the schema are exactly the keys
// the code reads plus the reserved tables, which the schema forbids, and that the decoder does
// not call any listed key unknown.
//
// Parameters:
//   - t: test to report to
func TestSchemaListsEveryKey(t *testing.T) {
	props, ok := loadSchema(t)["properties"].(map[string]any)
	if !ok {
		t.Fatal("the schema has no properties")
	}

	typ := reflect.TypeOf(Manifest{})

	for i := 0; i < typ.NumField(); i++ {
		if f := typ.Field(i); f.IsExported() && manifestKeys[f.Name] == "" {
			t.Errorf("Manifest.%s is not mapped to a key in manifestKeys", f.Name)
		}
	}

	for _, key := range manifestKeys {
		if v, found := props[key]; !found || v == false {
			t.Errorf("the key %q is read by the code and missing from the schema", key)
		}
	}

	for _, key := range reservedTables {
		if v, found := props[key]; !found || v != false {
			t.Errorf("the reserved table %q must be listed as false", key)
		}
	}

	host := Host{Version: "dev", Features: AnnouncedFeatures}
	head := "api = 1\ntote = \">=1.0.0\"\ntier = \"declarative\"\n"

	for key := range props {
		if key == "api" || key == "tote" || key == "tier" {
			continue
		}

		_, rep := Decode([]byte(head+key+" = true\n"), DecodeOptions{Host: host})

		for _, is := range rep.Issues {
			if is.Code == CodeUnknownKey && is.Path == key {
				t.Errorf("the schema lists %q but the decoder does not know it", key)
			}
		}
	}

	if len(props) != len(manifestKeys)+len(reservedTables) {
		t.Errorf("the schema lists %d keys, expected %d", len(props),
			len(manifestKeys)+len(reservedTables))
	}
}

// TestSchemaListsMatchCode checks that the lists copied into the schema are those of the code:
// the reserved plugin ids, the grammar of a feature name and the announced features.
//
// Parameters:
//   - t: test to report to
func TestSchemaListsMatchCode(t *testing.T) {
	doc := loadSchema(t)

	var want []string

	for _, list := range [][]string{coreVerbs, plannedVerbs, reservedWords, recordFields} {
		want = append(want, list...)
	}

	sort.Strings(want)

	want = slices.Compact(want)

	got := schemaStrings(t, sub(t, doc, "$defs", "pluginId", "not", "anyOf", 0, "enum"))
	if !slices.Equal(got, want) {
		t.Errorf("reserved ids in the schema differ from the code:\n got %v\nwant %v", got, want)
	}

	// The schema does not list the features as an enumeration: a later tote announces more, and
	// a manifest that needs one must stay valid. It checks the grammar and gives the announced
	// names as examples.
	items := sub(t, doc, "properties", "requires_features", "items")

	if pattern := sub(t, items, "pattern"); pattern != featureRe.String() {
		t.Errorf("feature pattern in the schema is %v, the code has %q", pattern, featureRe)
	}

	if sub(t, items, "enum") != nil {
		t.Error("requires_features must not be a closed enumeration")
	}

	feats := schemaStrings(t, sub(t, items, "examples"))

	wantFeats := slices.Clone(AnnouncedFeatures)
	sort.Strings(wantFeats)

	if !slices.Equal(feats, wantFeats) {
		t.Errorf("features in the schema differ from the code:\n got %v\nwant %v", feats, wantFeats)
	}
}
