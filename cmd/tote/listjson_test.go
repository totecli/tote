package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// decodeList checks the envelope of the JSON form of a list and returns its records.
//
// Parameters:
//   - t: running test
//   - data: standard output of a list verb with --format json
//   - noun: key that must hold the array, after the schema key and alone with it
func decodeList[T any](t *testing.T, data []byte, noun string) []T {
	t.Helper()

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("json: %v in %q", err, data)
	}

	if len(doc) != 2 || string(doc["schema"]) != "1" || doc[noun] == nil {
		t.Fatalf("envelope keys = %v, want schema 1 and %q in %q", doc, noun, data)
	}

	if !bytes.HasPrefix(data, []byte("{\n  \"schema\": 1,\n  \""+noun+"\": ")) {
		t.Fatalf("envelope does not start with schema then %q: %q", noun, data)
	}

	var recs []T
	if err := json.Unmarshal(doc[noun], &recs); err != nil {
		t.Fatalf("%s: %v in %q", noun, err, data)
	}

	if recs == nil {
		t.Fatalf("%s is null in %q", noun, data)
	}

	return recs
}

// TestListJSONEnvelope checks, for every kind that has a list verb, the exact shape of the JSON
// form: the schema key first, the plural noun second, the records unchanged inside, an empty list
// as an empty array and never null, and nothing else around.
//
// Parameters:
//   - t: test to report to
func TestListJSONEnvelope(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		empty string
		full  string
	}{
		{
			"connections",
			[]string{"co", "list", "--format", "json"},
			"{\n  \"schema\": 1,\n  \"connections\": []\n}\n",
			"{\n  \"schema\": 1,\n  \"connections\": [\n    {\n      \"name\": \"web1\",\n" +
				"      \"via\": \"\",\n      \"target\": \"10.0.0.1\",\n" +
				"      \"user\": \"deploy\",\n      \"aliases\": [\n        \"w\",\n" +
				"        \"web\"\n      ],\n      \"ip\": \"\",\n" +
				"      \"desc\": \"Web front\",\n      \"needs_review\": false\n    },\n",
		},
		{
			"repos",
			[]string{"repos", "list", "--format", "json"},
			"{\n  \"schema\": 1,\n  \"repos\": []\n}\n",
			"{\n  \"schema\": 1,\n  \"repos\": [\n    {\n      \"name\": \"proj\",\n" +
				"      \"path\": \"~/code/proj\",\n      \"desc\": \"Main project\"\n    },\n",
		},
		{
			"aliases",
			[]string{"alias", "list", "--format", "json"},
			"{\n  \"schema\": 1,\n  \"aliases\": []\n}\n",
			"{\n  \"schema\": 1,\n  \"aliases\": [\n    {\n      \"name\": \"ll\",\n" +
				"      \"cmd\": \"ls -la\",\n      \"desc\": \"Long list\"\n    },\n",
		},
		{
			"funcs",
			[]string{"funcs", "list", "--format", "json"},
			"{\n  \"schema\": 1,\n  \"funcs\": []\n}\n",
			"{\n  \"schema\": 1,\n  \"funcs\": [\n    {\n      \"name\": \"hello\",\n" +
				"      \"kind\": \"source\",\n      \"script\": \"~/bin/hello.sh\",\n" +
				"      \"managed\": false,\n      \"desc\": \"\"\n    },\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" empty", func(t *testing.T) {
			h := newHarness(t)

			if code := h.run(tt.args...); code != 0 || h.out.String() != tt.empty {
				t.Errorf("code %d, stdout %q, want %q", code, h.out.String(), tt.empty)
			}
		})

		t.Run(tt.name+" full", func(t *testing.T) {
			h := newHarness(t)
			h.fixtures()

			code, out := h.run(tt.args...), h.out.String()
			ok := strings.HasPrefix(out, tt.full) && strings.HasSuffix(out, "  ]\n}\n")
			if code != 0 || !ok {
				t.Errorf("code %d, stdout %q, want the prefix %q", code, out, tt.full)
			}
		})
	}
}

// TestListJSONFormatIsUnchanged checks that the other formats still print the records alone, as
// scripts read them, and that the spellings of the list verb give the same envelope.
//
// Parameters:
//   - t: test to report to
func TestListJSONFormatIsUnchanged(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	h.run("co", "list", "--format", "json")
	want := h.out.String()

	for _, args := range [][]string{
		{"co", "ls", "--format=json"},
		{"co", "--format", "json"},
		{"co", "-l", "--format", "json"},
	} {
		if code := h.run(args...); code != 0 || h.out.String() != want {
			t.Errorf("%v: code %d, stdout %q, want %q", args, code, h.out.String(), want)
		}
	}

	h.run("co", "list", "--format", "tsv")

	if strings.ContainsAny(h.out.String(), "{}\"") || strings.HasPrefix(h.out.String(), "[") {
		t.Errorf("tsv carries JSON: %q", h.out.String())
	}
}

// TestListJSONFlag checks that --json is the same as --format json on the four list verbs, in
// either order with it, that it is a usage error with another format, and that the characters of
// HTML are left as they are.
//
// Parameters:
//   - t: test to report to
func TestListJSONFlag(t *testing.T) {
	verbs := [][]string{
		{"co", "list"},
		{"repos", "list"},
		{"alias", "list"},
		{"funcs", "list"},
		{"conn", "-l"},
		{"alias"},
	}

	for _, verb := range verbs {
		t.Run(strings.Join(verb, " "), func(t *testing.T) {
			h := newHarness(t)
			h.fixtures()

			h.run(append(append([]string{}, verb...), "--format", "json")...)
			want := h.out.String()

			if !strings.HasPrefix(want, "{\n  \"schema\": 1,") {
				t.Fatalf("no envelope for --format json: %q", want)
			}

			for _, extra := range [][]string{
				{"--json"},
				{"--json", "--format", "json"},
				{"--format=json", "--json"},
			} {
				code := h.run(append(append([]string{}, verb...), extra...)...)
				if code != 0 || h.out.String() != want {
					t.Errorf("%v: code %d, stdout %q, want %q", extra, code, h.out.String(), want)
				}
			}

			for _, extra := range [][]string{
				{"--json", "--format", "tsv"},
				{"--format", "table", "--json"},
				{"--json", "--format=xml"},
			} {
				code := h.run(append(append([]string{}, verb...), extra...)...)
				if code != exitUsage || h.out.Len() != 0 || h.errb.Len() == 0 {
					t.Errorf("%v: code %d, stdout %q, stderr %q, want a usage error", extra, code,
						h.out.String(), h.errb.String())
				}
			}
		})
	}
}

// TestListJSONLeavesHTMLAlone checks that the angle brackets and the ampersand of a description
// come out of the JSON form as they are, not as < escapes.
//
// Parameters:
//   - t: test to report to
func TestListJSONLeavesHTMLAlone(t *testing.T) {
	h := newHarness(t)
	h.write("aliases.toml", "schema = 1\n\n[[alias]]\nname = \"ll\"\ncmd = \"ls\"\n"+
		"desc = \"a<b>&c\"\n")
	h.write("funcs.toml", "schema = 1\n\n[[func]]\nname = \"hello\"\nkind = \"source\"\n"+
		"script = \"~/bin/hello.sh\"\ndesc = \"x<y>&z\"\n")

	for _, tt := range []struct {
		verb string
		want string
	}{{"alias", `"desc": "a<b>&c"`}, {"funcs", `"desc": "x<y>&z"`}} {
		if code := h.run(tt.verb, "list", "--json"); code != 0 ||
			!strings.Contains(h.out.String(), tt.want) {
			t.Errorf("%s: code %d, stdout %q, want it to hold %s", tt.verb, code, h.out.String(),
				tt.want)
		}
	}
}
