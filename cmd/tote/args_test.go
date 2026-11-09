package main

import (
	"errors"
	"reflect"
	"testing"
)

// argSpecs are the options used by the parser tests.
var argSpecs = []opt{
	{long: "format", value: true},
	{long: "list", short: 'l'},
	{long: "yes", short: 'y'},
}

// TestParseOptions checks the mix of options and positional arguments.
//
// Parameters:
//   - t: test to report to
func TestParseOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		pos  []string
		vals map[string]string
		set  []string
		help bool
	}{
		{"empty", nil, nil, map[string]string{}, nil, false},
		{"positional", []string{"a", "b"}, []string{"a", "b"}, map[string]string{}, nil, false},
		{
			"mixed",
			[]string{"a", "--format", "json", "b", "-l"},
			[]string{"a", "b"},
			map[string]string{"format": "json"},
			[]string{"format", "list"},
			false,
		},
		{
			"equals",
			[]string{"--format=tsv"},
			nil,
			map[string]string{"format": "tsv"},
			[]string{"format"},
			false,
		},
		{
			"value starting with dash",
			[]string{"--format", "-x"},
			nil,
			map[string]string{"format": "-x"},
			[]string{"format"},
			false,
		},
		{
			"empty value",
			[]string{"--format="},
			nil,
			map[string]string{"format": ""},
			[]string{"format"},
			false,
		},
		{
			"double dash",
			[]string{"a", "--", "-l", "--format"},
			[]string{"a", "-l", "--format"},
			map[string]string{},
			nil, false,
		},
		{"lone dash", []string{"-"}, []string{"-"}, map[string]string{}, nil, false},
		{"empty arg", []string{""}, []string{""}, map[string]string{}, nil, false},
		{"help long", []string{"--help"}, nil, map[string]string{}, nil, true},
		{"help short", []string{"x", "-h"}, []string{"x"}, map[string]string{}, nil, true},
		{"short flag", []string{"-y", "n"}, []string{"n"}, map[string]string{}, []string{"yes"}, false},
	}

	for _, tt := range tests {
		o, err := parseOptions(tt.args, argSpecs)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}

		if !reflect.DeepEqual(o.pos, tt.pos) || !reflect.DeepEqual(o.vals, tt.vals) || o.help != tt.help {
			t.Errorf("%s: got pos %q vals %v help %v", tt.name, o.pos, o.vals, o.help)
		}

		for _, name := range tt.set {
			if !o.set[name] {
				t.Errorf("%s: option %s not set", tt.name, name)
			}
		}
	}
}

// TestParseOptionsErrors checks the errors of the parser.
//
// Parameters:
//   - t: test to report to
func TestParseOptionsErrors(t *testing.T) {
	tests := []struct {
		args []string
		key  string
		arg  string
	}{
		{[]string{"--nope"}, "cli_unknown_option", "--nope"},
		{[]string{"-x"}, "cli_unknown_option", "-x"},
		{[]string{"-ly"}, "cli_unknown_option", "-ly"},
		{[]string{"--yes=1"}, "cli_unknown_option", "--yes=1"},
		{[]string{"--help=1"}, "cli_unknown_option", "--help=1"},
		{[]string{"--format"}, "cli_option_needs_value", "--format"},
		{[]string{"a", "--format"}, "cli_option_needs_value", "--format"},
	}

	for _, tt := range tests {
		_, err := parseOptions(tt.args, argSpecs)

		var oerr *optError
		if !errors.As(err, &oerr) || oerr.Key != tt.key || oerr.Args[0] != tt.arg {
			t.Errorf("%q: error = %v, want %s %s", tt.args, err, tt.key, tt.arg)
		}
	}
}

// TestWantArgs checks the counting of positional arguments.
//
// Parameters:
//   - t: test to report to
func TestWantArgs(t *testing.T) {
	if err := wantArgs([]string{"a"}, 1, 1, "arg_name"); err != nil {
		t.Errorf("one of one: %v", err)
	}

	if err := wantArgs([]string{"a", "b", "c"}, 1, -1, "arg_name"); err != nil {
		t.Errorf("no limit: %v", err)
	}

	var oerr *optError

	err := wantArgs([]string{"a"}, 2, 2, "arg_name", "arg_path")
	if !errors.As(err, &oerr) || oerr.Key != "cli_missing_argument" || oerr.Args[0] != "arg_path" {
		t.Errorf("missing second: %v", err)
	}

	err = wantArgs([]string{"a", "b"}, 0, 1)
	if !errors.As(err, &oerr) || oerr.Key != "cli_too_many_arguments" {
		t.Errorf("too many: %v", err)
	}

	if (&optError{Key: "k", Args: []any{1}}).Error() == "" {
		t.Error("empty error text")
	}
}
