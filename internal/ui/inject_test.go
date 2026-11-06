package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// canaries are the escape sequences that the hostile data files hold. Each carries a value
// that the interface never writes by itself, so finding one intact in the output proves that
// data reached the terminal. The sequences are given as TOML basic strings and as bytes.
var canaries = []struct {
	name string
	toml string
	raw  string
}{
	{"dcs", `\u001bPevil-dcs\u001b\\`, "\x1bPevil-dcs\x1b\\"},
	{"apc", `\u001b_evil-apc\u001b\\`, "\x1b_evil-apc\x1b\\"},
	{"pm", `\u001b^evil-pm\u001b\\`, "\x1b^evil-pm\x1b\\"},
	{"sos", `\u001bXevil-sos\u001b\\`, "\x1bXevil-sos\x1b\\"},
	{"osc52", `\u001b]52;c;ZXZpbC1vc2M1Mg==\u0007`, "\x1b]52;c;ZXZpbC1vc2M1Mg==\x07"},
	{
		"osc8", `\u001b]8;;http://evil.invalid/\u0007link\u001b]8;;\u0007`,
		"\x1b]8;;http://evil.invalid/\x07link\x1b]8;;\x07",
	},
	{"osctitle", `\u001b]0;evil-title\u0007`, "\x1b]0;evil-title\x07"},
	{"csi", `\u001b[9731;9732H\u001b[?9731h\u001b[9731n`, "\x1b[9731;9732H"},
	{"c1csi", `\u009b9731;9732H`, "\u009b9731;9732H"},
	{"c1osc", `\u009d52;c;evil\u009c`, "\u009d52;c;evil\u009c"},
	{"c1dcs", `\u0090evil\u009c`, "\u0090evil\u009c"},
	{"c0", `\u0007\u0008\u000d\u0000\u007f`, "\x07\x08\r\x00\x7f"},
	{"bidi", `\u202eevil\u202c \u2066x\u2069 \u200f\u061c`, "\u202e"},
	{
		"zwj", `\U0001F468\u200d\U0001F469\u200d\U0001F467 \U0001F3F3\ufe0f\u200d\U0001F308`,
		"\U0001F468\u200d\U0001F469",
	},
	{"tag", `x\U000E0041\U000E007Fy\u200b`, "\U000E0041"},
}

// hostileSet builds the contents of the four data files: one record per canary in every text
// field that the files have.
func hostileSet() (conns, repos, aliases, funcs string) {
	var c, r, a, f strings.Builder

	c.WriteString("schema = 1\n")
	r.WriteString("schema = 1\n")
	a.WriteString("schema = 1\n")
	f.WriteString("schema = 1\n")

	for i, k := range canaries {
		fmt.Fprintf(&c, "\n[[connection]]\nname = \"c%d%s\"\ntarget = \"t%s\"\nuser = \"u%s\"\n"+
			"aliases = [\"a%s\"]\nip = \"%s\"\ndesc = \"%s\"\n", i, k.toml, k.toml, k.toml, k.toml,
			k.toml, k.toml)
		fmt.Fprintf(&c, "\n[[connection]]\nname = \"ok%d\"\ntarget = \"ok%d.example.com\"\n"+
			"desc = \"d%s\"\n", i, i, k.toml)
		fmt.Fprintf(&r, "\n[[repo]]\nname = \"r%d%s\"\npath = \"/srv/%s\"\ndesc = \"%s\"\n",
			i, k.toml, k.toml, k.toml)
		fmt.Fprintf(&r, "\n[[repo]]\nname = \"rok%d\"\npath = \"/srv/ok%d\"\ndesc = \"%s\"\n",
			i, i, k.toml)
		fmt.Fprintf(&a, "\n[[alias]]\nname = \"a%d%s\"\ncmd = \"echo %s\"\ndesc = \"%s\"\n",
			i, k.toml, k.toml, k.toml)
		fmt.Fprintf(&a, "\n[[alias]]\nname = \"aok%d\"\ncmd = \"echo %s\"\ndesc = \"%s\"\n",
			i, k.toml, k.toml)
		fmt.Fprintf(&f, "\n[[func]]\nname = \"f%d%s\"\nkind = \"source\"\nscript = \"~/%s\"\n"+
			"desc = \"%s\"\n", i, k.toml, k.toml, k.toml)
		fmt.Fprintf(&f, "\n[[func]]\nname = \"fok%d\"\nkind = \"exec\"\nscript = \"/opt/%s\"\n"+
			"desc = \"%s\"\n", i, k.toml, k.toml)
	}

	return c.String(), r.String(), a.String(), f.String()
}

// hostileSandbox returns a sandbox whose four files are the ones of hostileSet.
//
// Parameters:
//   - t: running test
func hostileSandbox(t *testing.T) *sandbox {
	t.Helper()

	s := frameSandbox(t, dataFull)
	conns, repos, aliases, funcs := hostileSet()

	writeFile(t, s.Opts.Paths.ConnectionsFile(), conns)
	writeFile(t, s.Opts.Paths.ReposFile(), repos)
	writeFile(t, s.Opts.Paths.AliasesFile(), aliases)
	writeFile(t, s.Opts.Paths.FuncsFile(), funcs)

	return s
}

// auditStream checks the bytes written to a terminal.
//
// Parameters:
//   - out: everything that was written
//
// Returns the problems found, empty when the stream is clean. The stream must hold no canary,
// no string sequence (DCS, APC, PM, SOS) and no OSC other than the color queries the interface
// sends itself, no control character in the text but line breaks, no bidirectional control, no
// tag character and no zero width joiner.
func auditStream(out string) []string {
	var (
		problems []string
		state    byte
		parse    = ansi.NewParser()
		rest     = out
	)

	for _, k := range canaries {
		if strings.Contains(out, k.raw) {
			problems = append(problems, "the "+k.name+" canary reached the output")
		}
	}

	for len(rest) > 0 {
		seq, _, n, next := ansi.DecodeSequence(rest, state, parse)
		if n == 0 {
			return append(problems, fmt.Sprintf("the decoder stops at %q",
				rest[:min(20, len(rest))]))
		}

		state, rest = next, rest[n:]

		switch {
		case strings.HasPrefix(seq, "\x1b]"):
			if !strings.HasPrefix(seq, "\x1b]11;?") && !strings.HasPrefix(seq, "\x1b]10;?") {
				problems = append(problems, fmt.Sprintf("unexpected OSC %q", seq))
			}
		case strings.HasPrefix(seq, "\x1bP"), strings.HasPrefix(seq, "\x1b_"),
			strings.HasPrefix(seq, "\x1b^"), strings.HasPrefix(seq, "\x1bX"):
			problems = append(problems, fmt.Sprintf("string sequence %q", seq))
		case strings.HasPrefix(seq, "\x1b"):
			if strings.Contains(seq, "9731") {
				problems = append(problems, fmt.Sprintf("canary parameter in %q", seq))
			}
		default:
			problems = append(problems, auditText(seq)...)
		}
	}

	return problems
}

// auditText checks the printable part of a stream.
//
// Parameters:
//   - text: bytes that are not part of an escape sequence
//
// Returns the problems found.
func auditText(text string) []string {
	var problems []string

	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size

		switch {
		case r == '\n' || r == '\r':
		case r == utf8.RuneError && size == 1:
			problems = append(problems, fmt.Sprintf("invalid UTF-8 in %q", text))
		case unsafeRune(r):
			problems = append(problems, fmt.Sprintf("%U in the text %q", r, text))
		case r == '\u200D':
			problems = append(problems, fmt.Sprintf("zero width joiner in the text %q", text))
		}
	}

	return problems
}

// TestInjectionFrames draws every list, form, confirmation and palette over hostile data at the
// sizes of the matrix and checks the frames and the canaries.
//
// Parameters:
//   - t: test to report to
func TestInjectionFrames(t *testing.T) {
	s := hostileSandbox(t)

	flows := []struct {
		start Start
		keys  []string
	}{
		{StartHome, nil},
		{StartConnections, nil},
		{StartConnections, []string{"end"}},
		{StartConnections, []string{"e"}},
		{StartConnections, []string{"end", "e"}},
		{StartConnections, []string{"d"}},
		{StartConnections, []string{"/", "type:evil"}},
		{StartRepos, []string{"end"}},
		{StartRepos, []string{"e"}},
		{StartRepos, []string{"d"}},
		{StartAliases, []string{"down", "e"}},
		{StartAliases, []string{"end", "d"}},
		{StartFuncs, []string{"end", "e"}},
		{StartFuncs, []string{"d"}},
		{StartConnections, []string{"ctrl+k", "type:evil"}},
		{StartConnections, []string{"ctrl+k", "type:c "}},
		{StartConnections, []string{"ctrl+k", "type:>"}},
	}

	for _, f := range flows {
		for _, size := range frameSizes(frameCase{}) {
			c := frameCase{start: f.start, keys: f.keys}
			frame := renderFrame(t, s, c, frameModes()[1], size)
			name := fmt.Sprintf("start %d keys %v %dx%d", f.start, f.keys, size[0], size[1])

			checkScreen(t, name, frame, size[0], size[1], false)

			for _, p := range auditText(sgr.ReplaceAllString(frame, "")) {
				t.Errorf("%s: %s", name, p)
			}

			for _, k := range canaries[:12] {
				if strings.Contains(frame, k.raw) {
					t.Errorf("%s: the %s canary is in the frame", name, k.name)
				}
			}
		}
	}
}

// TestInjectionRun drives the real program loop over hostile data through lists, forms,
// confirmations and the palette, and audits every byte it writes to its output.
//
// Parameters:
//   - t: test to report to
func TestInjectionRun(t *testing.T) {
	s := hostileSandbox(t)

	var keys []string

	for range allKinds {
		keys = append(keys, "\x1b[F", "e", "\x1b", "d", "\x1b", "\x0b", "evil", "\x1b[B", "\x1b",
			"l")
	}

	keys = append(keys, "\x04")

	res, err, out := runPaced(t, s, 70*time.Millisecond, keys...)
	if err != nil || !res.Empty() {
		t.Fatalf("Run = %+v, %v", res, err)
	}

	if len(out) < 2000 {
		t.Fatalf("the run wrote only %d bytes: %q", len(out), out)
	}

	for _, p := range auditStream(out) {
		t.Errorf("run: %s", p)
	}

	for _, want := range []string{
		"Connections", "Repos", "Aliases", "Functions",
		"evil-dcs", "ZXZpbC1vc2M1Mg", "ok3",
	} {
		if !strings.Contains(ansi.Strip(out), want) {
			t.Errorf("the run never showed %q", want)
		}
	}
}

// TestInjectionAudit checks the auditor itself: it must flag every canary and accept what the
// interface writes.
//
// Parameters:
//   - t: test to report to
func TestInjectionAudit(t *testing.T) {
	clean := "\x1b[?1049h\x1b[2J\x1b[1;1H\x1b[38;5;208mhello\x1b[0m\r\nworld \u2764\x1b]11;?\x1b\\"

	if p := auditStream(clean); len(p) != 0 {
		t.Fatalf("the auditor refuses a clean stream: %v", p)
	}

	for _, k := range canaries {
		if len(auditStream(clean+"x"+k.raw+"y")) == 0 {
			t.Errorf("the auditor accepts the %s canary", k.name)
		}
	}

	if len(auditStream("\x1b[?1049h\x1b]52;c;AAAA\x07")) == 0 {
		t.Error("the auditor accepts an OSC 52 without a canary")
	}
}
