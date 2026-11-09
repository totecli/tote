package main

import (
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
)

// version is the release version, injected at build time with -X main.version.
var version = "dev"

// pseudoVersionRe matches the end of a Go pseudo-version: a UTC timestamp and a commit prefix,
// after a hyphen or after the dot of a pre-release such as 0.
var pseudoVersionRe = regexp.MustCompile(`[-.][0-9]{14}-[0-9a-f]{12}$`)

// init falls back to the module version of the build information when no version was injected,
// which is the case for a binary made by go install.
func init() {
	if version != "dev" {
		return
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	version = moduleVersion(info.Main.Version)
}

// moduleVersion turns the module version of the build information into the version to report.
//
// A pseudo-version such as 0.0.0-20261109205113-159407959e94 is what go build gives to a checkout
// that is not on a tag. It is not a release, so it must not stand in for one: the literal dev
// stays, which satisfies every version range of a plugin.
//
// Parameters:
//   - v: Main.Version of the build information
//
// Returns the version without its leading v, or dev when v is empty, the development marker, a
// pseudo-version or a build with local changes.
func moduleVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if v == "" || v == "(devel)" || strings.Contains(v, "+") || pseudoVersionRe.MatchString(v) {
		return "dev"
	}

	return v
}

// main runs tote and exits with its status code.
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one tote command line on the real process environment.
//
// Parameters:
//   - args: command line without the program name
//   - stdout: destination of normal output
//   - stderr: destination of errors and hints
//
// Returns the exit code of the process.
func run(args []string, stdout, stderr io.Writer) int {
	return newApp(stdout, stderr).run(args)
}
