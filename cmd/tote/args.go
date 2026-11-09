package main

import (
	"fmt"
	"strings"
)

// opt describes one option of a command line.
//
// long is the name after two dashes, short is the one letter form or zero when there is none,
// and value tells whether the option takes an argument.
type opt struct {
	long  string
	short byte
	value bool
}

// optError is a misuse of the command line that names the catalog message describing it.
//
// Key is the catalog key and Args are the values of its Printf directives.
type optError struct {
	Key  string
	Args []any
}

// Error implements error.
func (e *optError) Error() string {
	return fmt.Sprintf("%s %v", e.Key, e.Args)
}

// options is the result of parseOptions.
//
// pos holds the positional arguments in order, vals the last value of every option that takes
// one, all every value of it in order (for an option that may be repeated), set the options that
// appeared and help is true when -h or --help appeared.
type options struct {
	pos  []string
	vals map[string]string
	all  map[string][]string
	set  map[string]bool
	help bool
}

// parseOptions splits a command line into options and positional arguments.
//
// Options and positional arguments may be mixed. A "--" ends the options, a lone "-" is a
// positional argument, and -h and --help are always understood.
//
// Parameters:
//   - args: arguments after the verb
//   - specs: options the verb accepts, nil for none
//
// Returns the parsed options, or an *optError for an unknown option or a missing value.
func parseOptions(args []string, specs []opt) (options, error) {
	return parseOptionsWords(args, specs, 0)
}

// parseOptionsWords is parseOptions for a verb whose last argument is a command line.
//
// Parameters:
//   - args: arguments after the verb
//   - specs: options the verb accepts, nil for none
//   - lead: number of positional arguments before the command line; once the first word of the
//     command line is met, it and everything after it is positional, dashes included. Zero
//     turns this off.
//
// Returns the parsed options, or an *optError for an unknown option or a missing value.
func parseOptionsWords(args []string, specs []opt, lead int) (options, error) {
	o := options{vals: map[string]string{}, all: map[string][]string{}, set: map[string]bool{}}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			o.pos = append(o.pos, args[i+1:]...)
			return o, nil
		}

		if len(arg) < 2 || arg[0] != '-' {
			o.pos = append(o.pos, arg)

			if lead > 0 && len(o.pos) > lead {
				o.pos = append(o.pos, args[i+1:]...)
				return o, nil
			}

			continue
		}

		used, err := o.take(arg, args[i+1:], specs)
		if err != nil {
			return options{}, err
		}

		i += used
	}

	return o, nil
}

// take records one option argument.
//
// Parameters:
//   - arg: the argument, starting with a dash
//   - rest: the arguments that follow it, one of which may be its value
//   - specs: options the verb accepts
//
// Returns how many arguments of rest were consumed, or an *optError.
func (o *options) take(arg string, rest []string, specs []opt) (int, error) {
	name, val, hasVal := arg[1:], "", false
	long := strings.HasPrefix(arg, "--")

	if long {
		name, val, hasVal = strings.Cut(arg[2:], "=")
	}

	if (long && name == "help") || (!long && name == "h") {
		if hasVal {
			return 0, &optError{Key: "cli_unknown_option", Args: []any{arg}}
		}

		o.help = true

		return 0, nil
	}

	spec, ok := findOpt(specs, name, long)
	if !ok || (hasVal && !spec.value) {
		return 0, &optError{Key: "cli_unknown_option", Args: []any{arg}}
	}

	used := 0

	if spec.value && !hasVal {
		if len(rest) == 0 {
			return 0, &optError{Key: "cli_option_needs_value", Args: []any{arg}}
		}

		val, used = rest[0], 1
	}

	if spec.value {
		o.vals[spec.long] = val
		o.all[spec.long] = append(o.all[spec.long], val)
	}

	o.set[spec.long] = true

	return used, nil
}

// findOpt looks an option up by its long or short name.
//
// Parameters:
//   - specs: options the verb accepts
//   - name: name without the dashes
//   - long: true for the two dash form, false for the one dash form
//
// Returns the option and true, or the zero option and false.
func findOpt(specs []opt, name string, long bool) (opt, bool) {
	for _, s := range specs {
		if long && s.long == name {
			return s, true
		}

		if !long && s.short != 0 && len(name) == 1 && s.short == name[0] {
			return s, true
		}
	}

	return opt{}, false
}

// wantArgs checks the number of positional arguments.
//
// Parameters:
//   - pos: positional arguments
//   - lo: fewest accepted
//   - hi: most accepted, negative for no limit
//   - names: catalog keys of the argument names, used in the missing argument message
//
// Returns nil, or an *optError for a missing or an extra argument.
func wantArgs(pos []string, lo, hi int, names ...string) error {
	if len(pos) < lo {
		return &optError{Key: "cli_missing_argument", Args: []any{names[len(pos)]}}
	}

	if hi >= 0 && len(pos) > hi {
		return &optError{Key: "cli_too_many_arguments"}
	}

	return nil
}
