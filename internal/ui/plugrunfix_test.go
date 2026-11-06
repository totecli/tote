package ui

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/totecli/tote/internal/plugin"
)

// dataPlugRun is the data set of the frames of the plugin runs: the plugin gft installed, a fake
// gft and a fake opener first on the PATH, and a saved repository "proj" whose directory is /usr,
// the same on every machine, which is not a git repository, so that the actions that need a
// GitHub remote are greyed and the ones that work in the directory are not.
const dataPlugRun = dataPlugins + 1

// fakeGft is the script that stands for gft: it records its arguments, one line per run, in the
// file named by the first line of the script, prints a few lines and exits with the status that
// the last argument asks for.
const fakeGft = `echo "$@" >> "LOG"
echo "gft: $1 $2"
case "$*" in
*fail*) echo "boom" >&2; exit 3;;
*many*) i=0; while [ $i -lt 400 ]; do echo "line $i"; i=$((i+1)); done;;
*color*) printf '\033[31mred\033[0m \033[1;32mgreen\033[0m\n';;
*hang*) exec sleep 30;;
esac
exit 0
`

// runTools puts a fake gft and a fake opener on the PATH of the registry, after the directory of
// the sandbox, so that git and the shell are the ones of the system. The fake gft appends its
// arguments to a log file, whose path is returned, and the opener appends the address.
func (l *plugLab) runTools() string {
	l.t.Helper()

	bin := filepath.Join(l.sb.Dir, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		l.t.Fatal(err)
	}

	log := filepath.Join(l.sb.Dir, "gft.log")

	script := func(name, body string) {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
			l.t.Fatal(err)
		}
	}

	script("gft", strings.ReplaceAll(fakeGft, "LOG", log))
	script(plugin.OpenerName(runtime.GOOS), "echo \"$@\" >> \""+log+"\"\nexit 0\n")

	l.env["PATH"] = bin + ":/usr/bin:/bin"

	return log
}

// installGft installs the plugin that ships with tote, with the consent of the user, as the
// plugins screen does.
func (l *plugLab) installGft() {
	l.t.Helper()

	st, err := l.reg.Plan(context.Background(), plugin.InstallRequest{Source: "gft"})
	if err != nil {
		l.t.Fatalf("Plan(gft): %v", err)
	}

	if _, err := st.Commit(plugin.Grant{Prompt: true}); err != nil {
		l.t.Fatalf("Commit(gft): %v", err)
	}
}

// gftRig returns a rig over a sandbox where gft is installed and runnable, started on the
// repositories, with the log file of the fake programs.
//
// Parameters:
//   - t: running test
//   - keys: key names to press first
func gftRig(t *testing.T, keys ...string) (*rig, *plugLab, string) {
	t.Helper()

	sb := newSandbox(t)
	l := newPlugLab(t, sb)
	log := l.runTools()
	l.installGft()

	sb.Opts.Start = StartRepos
	g := newRig(t, sb)
	g.press(keys...)

	return g, l, log
}
