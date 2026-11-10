package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

// canary is a hostile string of a data file: Toml is how it is written in the file, Raw the
// bytes that would reach the terminal if the interface forgot to clean it.
type canary struct {
	name string
	toml string
	raw  string
}

// canaries are the hostile strings. Each escape sequence carries a value that the interface
// never writes by itself, so that finding one intact in the output proves a leak.
var canaries = []canary{
	{"dcs", `\u001bPevil-dcs\u001b\\`, "\x1bPevil-dcs\x1b\\"},
	{"apc", `\u001b_evil-apc\u001b\\`, "\x1b_evil-apc\x1b\\"},
	{"pm", `\u001b^evil-pm\u001b\\`, "\x1b^evil-pm\x1b\\"},
	{"osc52", `\u001b]52;c;ZXZpbC1vc2M1Mg==\u0007`, "\x1b]52;c;ZXZpbC1vc2M1Mg==\x07"},
	{
		"osc8", `\u001b]8;;http://evil.invalid/\u0007x\u001b]8;;\u0007`,
		"\x1b]8;;http://evil.invalid/\x07",
	},
	{"osctitle", `\u001b]0;evil-title\u0007`, "\x1b]0;evil-title\x07"},
	{"csi", `\u001b[9731;9732H\u001b[?9731h`, "\x1b[9731;9732H"},
	{"c1csi", `\u009b9731;9732H`, "\u009b9731;9732H"},
	{"c1osc", `\u009d52;c;evil\u009c`, "\u009d52;c;evil\u009c"},
	{"c0", `\u0007\u0008\u000d\u0000`, "\x07\x08\r\x00"},
	{"bidi", `\u202eevil\u202c \u2066x\u2069`, "\u202e"},
	{"zwj", `\U0001F468\u200d\U0001F469\u200d\U0001F467`, "\U0001F468\u200d\U0001F469"},
	{"tag", `x\U000E0041\U000E007Fy`, "\U000E0041"},
}

// writeHostile replaces the data files of the sandbox by files whose every text field holds a
// canary.
//
// Parameters:
//   - t: running test
//   - sb: sandbox to write into
func writeHostile(t *testing.T, sb *sandbox) {
	t.Helper()

	var c, r, a, f strings.Builder

	c.WriteString("schema = 1\n")
	r.WriteString("schema = 1\n")
	a.WriteString("schema = 1\n")
	f.WriteString("schema = 1\n")

	for i, k := range canaries {
		fmt.Fprintf(&c, "\n[[connection]]\nname = \"c%d%s\"\ntarget = \"t%s\"\nuser = \"u%s\"\n"+
			"desc = \"%s\"\n", i, k.toml, k.toml, k.toml, k.toml)
		fmt.Fprintf(&c, "\n[[connection]]\nname = \"ok%d\"\ntarget = \"ok%d.example.com\"\n"+
			"desc = \"d%s\"\n", i, i, k.toml)
		fmt.Fprintf(&r, "\n[[repo]]\nname = \"r%d%s\"\npath = \"/srv/%s\"\ndesc = \"%s\"\n",
			i, k.toml, k.toml, k.toml)
		fmt.Fprintf(&a, "\n[[alias]]\nname = \"a%d%s\"\ncmd = \"echo %s\"\ndesc = \"%s\"\n",
			i, k.toml, k.toml, k.toml)
		fmt.Fprintf(&f, "\n[[func]]\nname = \"f%d%s\"\nkind = \"source\"\nscript = \"~/%s\"\n"+
			"desc = \"%s\"\n", i, k.toml, k.toml, k.toml)
	}

	cfg := filepath.Join(sb.Dir, "config")

	sb.write(t, filepath.Join(cfg, "connections.toml"), c.String())
	sb.write(t, filepath.Join(cfg, "repos.toml"), r.String())
	sb.write(t, filepath.Join(cfg, "aliases.toml"), a.String())
	sb.write(t, filepath.Join(cfg, "funcs.toml"), f.String())
}

// auditBytes checks what a program wrote to its terminal.
//
// Parameters:
//   - out: the raw bytes, taken before the terminal emulator interprets them
//
// Returns the problems found. The stream must hold no canary, no DCS, APC, PM or SOS, no OSC
// but the color queries the interface sends by itself, no CSI with a canary parameter, no
// control character in the text but line breaks and the backspace and the hard tab that the
// renderer uses to move the cursor left and right (a backspace or a tab from the data would not
// come alone: the canary check covers it), no C1 control, no bidirectional or tag character and
// no zero width joiner.
func auditBytes(out string) []string {
	var problems []string

	for _, k := range canaries {
		if strings.Contains(out, k.raw) {
			problems = append(problems, "the "+k.name+" canary reached the terminal")
		}
	}

	for i := 0; i < len(out); {
		if out[i] != 0x1b {
			r, size := utf8.DecodeRuneInString(out[i:])

			switch {
			case r == '\n' || r == '\r' || r == '\b' || r == '\t':
			case r == utf8.RuneError && size == 1:
				problems = append(problems, fmt.Sprintf("invalid UTF-8 at byte %d", i))
			case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) ||
				unicode.Is(unicode.Zp, r):
				problems = append(problems, fmt.Sprintf("%U in the text at byte %d: %q", r, i,
					out[max(0, i-60):min(len(out), i+30)]))
			}

			i += size

			continue
		}

		end := escapeEnd(out, i)
		seq := out[i:end]

		switch {
		case len(seq) < 2:
		case seq[1] == ']':
			if !strings.HasPrefix(seq, "\x1b]11;?") && !strings.HasPrefix(seq, "\x1b]10;?") {
				problems = append(problems, fmt.Sprintf("OSC %q", seq))
			}
		case strings.ContainsRune("P_^X", rune(seq[1])):
			problems = append(problems, fmt.Sprintf("string sequence %q", seq))
		case seq[1] == '[' && strings.Contains(seq, "9731"):
			problems = append(problems, fmt.Sprintf("CSI with a canary parameter %q", seq))
		}

		i = end
	}

	return problems
}

// escapeEnd returns the index just after the escape sequence that starts at i.
//
// Parameters:
//   - s: stream
//   - i: index of an ESC byte
func escapeEnd(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}

	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
			j++
		}

		return min(j+1, len(s))
	case ']', 'P', '_', '^', 'X':
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}

			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}

		return len(s)
	case ' ', '#', '(', ')', '%':
		return min(i+3, len(s))
	}

	return i + 2
}

// TestAuditBytes checks the auditor: it must accept what the interface writes and flag every
// canary.
//
// Parameters:
//   - t: test to report to
func TestAuditBytes(t *testing.T) {
	clean := "\x1b[?1049h\x1b[2J\x1b[1;1H\x1b[38;5;208mhello\x1b[0m\r\nw \u2764\x1b]11;?\x1b\\"

	if p := auditBytes(clean); len(p) != 0 {
		t.Fatalf("a clean stream is refused: %v", p)
	}

	for _, k := range canaries {
		if len(auditBytes(clean+"x"+k.raw+"y")) == 0 {
			t.Errorf("the %s canary is accepted", k.name)
		}
	}

	for _, bad := range []string{
		"\x1b]52;c;AAAA\x07", "\x1bPq\x1b\\", "\x1b_x\x1b\\", "a\x07b",
		"a\x00b", "a\u009bb",
	} {
		if len(auditBytes(clean+bad)) == 0 {
			t.Errorf("%q is accepted", bad)
		}
	}
}

// settle waits until the raw output stops growing.
//
// Parameters:
//   - t: running test
//   - path: file that receives the output of the pane
//   - quiet: how long the size must stay the same
func settle(t *testing.T, path string, quiet time.Duration) {
	t.Helper()

	var last int64 = -1

	stable := time.Now()
	deadline := time.Now().Add(waitLimit)

	for time.Now().Before(deadline) {
		st, err := os.Stat(path)
		if err == nil && st.Size() != last {
			last, stable = st.Size(), time.Now()
		}

		if last > 0 && time.Since(stable) >= quiet {
			return
		}

		time.Sleep(pollEvery)
	}

	t.Fatalf("the output of %s never settled", path)
}

// TestInjectionProbe runs the real binary over hostile data in a real terminal and audits the
// raw bytes it writes: no sequence that came from the data may reach the terminal.
//
// Parameters:
//   - t: test to report to
func TestInjectionProbe(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	writeHostile(t, sb)

	tm := newTerm(t, sb, startOptions{Hold: true}, "tote")
	raw := filepath.Join(sb.Out, "raw")

	if err := tm.tmux("pipe-pane", "-O", "-t", tm.Sess, "cat > "+raw); err != nil {
		t.Fatal(err)
	}

	tm.release()
	tm.waitText("Connections")

	step := func(keys ...string) {
		tm.key(keys...)
		settle(t, raw, 150*time.Millisecond)
	}

	text := func(s string) {
		tm.typeText(s)
		settle(t, raw, 150*time.Millisecond)
	}

	for range 4 {
		step("End")
		step("e")
		step("Escape")
		step("d")
		step("Escape")
		step("C-k")
		text("evil")
		step("Down")
		step("Escape")
		text("l")
	}

	tm.key("C-d")

	if code := tm.waitDone(); code != 0 {
		t.Errorf("exit status %d, want 0", code)
	}

	settle(t, raw, 150*time.Millisecond)

	out := readFile(t, raw)
	if len(out) < 5000 {
		t.Fatalf("the probe saw only %d bytes", len(out))
	}

	for _, p := range auditBytes(out) {
		t.Error(p)
	}

	for _, want := range []string{"Aliases", "Functions", "Repos", "evil-dcs"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output never shows %q", want)
		}
	}
}
