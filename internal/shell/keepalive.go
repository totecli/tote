package shell

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// probeTimeout is how long "ssh -G" may take before the connection starts without it.
	probeTimeout = 2 * time.Second

	// probeGrace is how long the probe waits for its output pipe after it was killed, so that
	// a Match exec command that outlives ssh cannot hold the caller.
	probeGrace = 100 * time.Millisecond

	// keepInterval is the ServerAliveInterval, in seconds, given to a connection that has none.
	keepInterval = 30

	// keepCount is the ServerAliveCountMax given with it, and the default value of ssh.
	keepCount = 3
)

// Prober runs a command and returns what it wrote on its standard output. SSHProbe is the real
// one; tests give a fake.
type Prober func(ctx context.Context, argv, environ []string) (string, error)

// SSHProbe runs a command with a time limit and returns what it wrote on its standard output.
// Its input and its error output are the null device, so that nothing reaches the terminal.
//
// Parameters:
//   - ctx: ends the command when it is done
//   - argv: command and arguments
//   - environ: environment of the command
func SSHProbe(ctx context.Context, argv, environ []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var out bytes.Buffer

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = environ
	cmd.Stdout = &out
	cmd.WaitDelay = probeGrace

	err := cmd.Run()

	return out.String(), err
}

// KeepEligible reports whether keepalive options may be added to a command: it must be ssh
// itself, and must not already say something about ServerAlive, which is how a via keeps its
// own settings.
//
// Parameters:
//   - argv: command and arguments of a connection
func KeepEligible(argv []string) bool {
	if len(argv) < 2 || filepath.Base(argv[0]) != plainSSH {
		return false
	}

	return !slices.ContainsFunc(argv[1:], func(a string) bool {
		return strings.Contains(strings.ToLower(a), "serveralive")
	})
}

// keepaliveFrom reads the output of "ssh -G" and decides which options to add.
//
// Parameters:
//   - out: output of "ssh -G", one lowercase "key value" pair per line
//
// Returns nil when ServerAliveInterval is missing or above 0, which means that the user has
// configured it. Otherwise it returns -o ServerAliveInterval=30, followed by -o
// ServerAliveCountMax=3 unless the count differs from the default of ssh, which means that the
// user chose it.
func keepaliveFrom(out string) []string {
	interval, count := -1, keepCount

	for _, line := range strings.Split(out, "\n") {
		name, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			continue
		}

		switch strings.ToLower(name) {
		case "serveraliveinterval":
			interval = n
		case "serveralivecountmax":
			count = n
		}
	}

	if interval != 0 {
		return nil
	}

	extra := []string{"-o", "ServerAliveInterval=" + strconv.Itoa(keepInterval)}
	if count == keepCount {
		extra = append(extra, "-o", "ServerAliveCountMax="+strconv.Itoa(keepCount))
	}

	return extra
}

// Keepalive completes the command of a connection with ssh keepalive options when its ssh
// configuration has none. It asks "ssh -G", which can run the Match exec commands of the user's
// ssh_config, so it is meant to be called at the moment a connection starts or a command is
// copied, never to preview a command. The interface and "tote co NAME" share it, which is why it
// lives here, next to the construction of the command, and not in either of them.
//
// Parameters:
//   - on: the connect.keepalive setting; when false nothing is asked and argv is returned as is
//   - probe: runs "ssh -G"; nil means SSHProbe
//   - environ: environment of the lookup
//   - argv: command and arguments of the connection
//
// Returns argv itself when nothing is to be added: the setting is off, the command is not plain
// ssh or has its own ServerAlive option, the lookup failed or timed out, or the user's ssh
// configuration sets an interval. Otherwise it returns a new slice with the options right after
// the program name.
func Keepalive(on bool, probe Prober, environ, argv []string) []string {
	if !on || !KeepEligible(argv) {
		return argv
	}

	if probe == nil {
		probe = SSHProbe
	}

	ask := append([]string{argv[0], "-G"}, argv[1:]...)

	out, err := probe(context.Background(), ask, environ)
	if err != nil {
		return argv
	}

	extra := keepaliveFrom(out)
	if len(extra) == 0 {
		return argv
	}

	res := make([]string, 0, len(argv)+len(extra))
	res = append(res, argv[0])
	res = append(res, extra...)

	return append(res, argv[1:]...)
}
