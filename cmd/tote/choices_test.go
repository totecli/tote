package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// suggestionsManifest is a plugin with a command whose second field has a choices_from.
const suggestionsManifest = `api = 1
id = "demo"
version = "1.1.0"
tote = ">=1.0.0, <2"
title = "Demo"
description = "A neutral demo plugin."
license = "MIT"
tier = "declarative"
permissions = ["exec:echo"]

[[command]]
id = "pick"
label = "Pick"
run = ["echo", "{slug}", "{tag?}"]
output = "pager"
[[command.field]]
id = "slug"
type = "text"
label = "Slug"
[[command.field]]
id = "tag"
type = "text"
label = "Tag"
choices_from = ["echo", "--list", "--", "{slug}"]
`

// TestPluginSuggestionsAreListed checks that check --argv prints the choices_from of a field
// without starting it, and that the commands of a dry run carry it in the documented JSON shape and
// in the consent screen.
//
// Parameters:
//   - t: test to report to
func TestPluginSuggestionsAreListed(t *testing.T) {
	e := newPluginEnv(t)
	dir := e.pluginText("demo", suggestionsManifest)

	out := e.must("plugin", "check", dir, "--argv")
	if !strings.Contains(out, "suggestions for the field tag: echo --list -- '{slug}'") {
		t.Errorf("check does not list the suggestions:\n%s", out)
	}

	out = e.must("plugin", "check", dir, "--argv", "--json")

	var chk struct {
		Contributions []struct {
			ID          string `json:"id"`
			Suggestions []struct {
				Field string   `json:"field"`
				Run   []string `json:"run"`
			} `json:"suggestions"`
		} `json:"contributions"`
	}

	if err := json.Unmarshal([]byte(out), &chk); err != nil {
		t.Fatal(err)
	}

	if len(chk.Contributions) != 1 || len(chk.Contributions[0].Suggestions) != 1 ||
		chk.Contributions[0].Suggestions[0].Field != "tag" {
		t.Errorf("check json: %+v", chk)
	}

	if out = e.must("plugin", "install", dir, "--dry-run"); !strings.Contains(out,
		"suggests values for the field tag with: echo --list -- '{slug}'") {
		t.Errorf("the consent screen does not list the suggestions:\n%s", out)
	}

	out = e.must("plugin", "install", dir, "--dry-run", "--json")

	var doc struct {
		Plugin struct {
			Commands []struct {
				ID          string `json:"id"`
				Suggestions []struct {
					Field string   `json:"field"`
					Run   []string `json:"run"`
				} `json:"suggestions"`
			} `json:"commands"`
		} `json:"plugin"`
	}

	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}

	c := doc.Plugin.Commands
	if len(c) != 1 || len(c[0].Suggestions) != 1 || c[0].Suggestions[0].Field != "tag" ||
		strings.Join(c[0].Suggestions[0].Run, " ") != "echo --list -- {slug}" {
		t.Errorf("dry run json: %+v", doc.Plugin.Commands)
	}

	raw, err := json.Marshal(c[0].Suggestions)
	if err != nil {
		t.Fatal(err)
	}

	if want := `[{"field":"tag","run":["echo","--list","--","{slug}"]}]`; string(raw) != want {
		t.Errorf("the JSON shape of a suggestion changed: %s, want %s", raw, want)
	}

	if strings.Contains(e.must("plugin", "install", e.plugin("plain", "1.0.0", "exec:echo"),
		"--dry-run", "--json"), "suggestions") {
		t.Error("a command with no choices_from has a suggestions key")
	}
}
