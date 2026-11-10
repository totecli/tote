package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sgrState is the part of the graphic state of a cell that the hover changes: the underline and
// the background, written as the parameters of its sequence, empty for none.
type sgrState struct {
	under bool
	bg    string
}

// apply changes a state with the parameters of a graphic sequence.
//
// Parameters:
//   - params: the parameters, separated by semicolons; empty means a reset
func (s *sgrState) apply(params string) {
	f := strings.Split(params, ";")

	for i := 0; i < len(f); i++ {
		switch p := f[i]; {
		case p == "" || p == "0":
			*s = sgrState{}
		case p == "4" || (strings.HasPrefix(p, "4:") && p != "4:0"):
			s.under = true
		case p == "24" || p == "4:0":
			s.under = false
		case p == "49":
			s.bg = ""
		case p == "38" || p == "48" || p == "58":
			n := 2

			if i+1 < len(f) && f[i+1] == "2" {
				n = 4
			}

			if p == "48" && i+n < len(f) {
				s.bg = strings.Join(f[i:i+n+1], ";")
			}

			i += n
		}
	}
}

// cellsWhere returns the text of a styled line that is drawn in a state that a condition accepts.
//
// Parameters:
//   - line: line of a capture made with the attributes
//   - want: condition on the state of a cell
func cellsWhere(line string, want func(sgrState) bool) string {
	var (
		st  sgrState
		out strings.Builder
	)

	for i := 0; i < len(line); {
		if line[i] == '\x1b' && i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}

			if j < len(line) && line[j] == 'm' {
				st.apply(line[i+2 : j])
			}

			i = j + 1

			continue
		}

		if want(st) {
			out.WriteByte(line[i])
		}

		i++
	}

	return out.String()
}

// styledRow returns a row of the pane with its attributes.
//
// Parameters:
//   - row: row, 1 for the first
func (m *term) styledRow(row int) string {
	m.t.Helper()

	out, err := m.run("capture-pane", "-p", "-e", "-t", m.Sess)
	if err != nil {
		m.t.Fatalf("capture-pane: %v\n%s", err, out)
	}

	rows := strings.Split(out, "\n")
	if row < 1 || row > len(rows) {
		m.t.Fatalf("row %d is not on a screen of %d rows", row, len(rows))
	}

	return rows[row-1]
}

// glide sends the report of the pointer moving to a cell with no button down, as a terminal in
// all-motion mode does.
//
// Parameters:
//   - col: column, 1 for the first
//   - row: row, 1 for the first
func (m *term) glide(col, row int) {
	m.t.Helper()
	m.send(fmt.Sprintf("\x1b[<35;%d;%dM", col, row))
}

// hovered tells what is drawn with the hover look on a row: the text with an underline, or on a
// background.
//
// Parameters:
//   - row: row, 1 for the first
//   - under: true to look for an underline, false for a background
func (m *term) hovered(row int, under bool) string {
	m.t.Helper()

	return strings.TrimSpace(cellsWhere(m.styledRow(row), func(s sgrState) bool {
		if under {
			return s.under
		}

		return s.bg != ""
	}))
}

// TestMouseHover sends real SGR reports of the pointer, with no button down, to the real program:
// the terminal is asked for all-motion reporting (1003) and not for the drag reporting of 1002,
// the element under the pointer takes the hover look and loses it when the pointer leaves, a
// footer hint takes it too, nothing is selected, run or probed by the moves, a drag still
// selects text, and the modes are gone when the program ends.
//
// Parameters:
//   - t: test to report to
func TestMouseHover(t *testing.T) {
	looks := []struct {
		name  string
		env   []string
		under bool
	}{
		{"underline", []string{"TOTE_COLOR=none"}, true},
		{"background", []string{"TOTE_COLOR=truecolor"}, false},
	}

	for _, look := range looks {
		t.Run(look.name, func(t *testing.T) {
			sb := newSandbox(t)
			sb.write(t, configPath(sb), "schema = 1\n\n[ui]\ntheme = \"dark\"\n")

			tm := newTerm(t, sb, startOptions{Hold: true, Env: look.env}, "tote", "co")
			raw := filepath.Join(sb.Out, "raw")

			if out, err := tm.run("pipe-pane", "-t", tm.Sess, "cat > "+raw); err != nil {
				t.Fatalf("pipe-pane: %v\n%s", err, out)
			}

			tm.release()
			tm.waitText(cmdOf("alpha"))
			tm.waitMouse(wantMouse)

			sent := waitFile(t, raw, func(c string) bool {
				return strings.Contains(c, "\x1b[?1003h")
			}, func() string { return tm.look().Text })

			for _, want := range []string{"\x1b[?1003h", "\x1b[?1006h", "\x1b[?1004h"} {
				if !strings.Contains(sent, want) {
					t.Errorf("the program did not ask for %q:\n%q", want, sent)
				}
			}

			for _, not := range []string{"\x1b[?1002h", "\x1b[?1000h", "\x1b[?1015h"} {
				if strings.Contains(sent, not) {
					t.Errorf("the program asked for %q:\n%q", not, sent)
				}
			}

			col, row := tm.where("bravo.example.com", false)

			if got := tm.hovered(row, look.under); got != "" {
				t.Fatalf("a row is drawn with the hover look before the pointer is there: %q", got)
			}

			tm.glide(col+2, row)
			tm.waitFor("the row to take the hover look", func(snap) bool {
				return strings.Contains(tm.hovered(row, look.under), "bravo.example.com")
			})

			// A move to another cell of the same row changes nothing on screen.
			tm.glide(col+5, row)
			time.Sleep(150 * time.Millisecond)

			if got := tm.hovered(row, look.under); !strings.Contains(got, "bravo.example.com") {
				t.Errorf("the hover look went away on the same row: %q", got)
			}

			// The keyboard selection did not move and nothing ran.
			if !tm.look().has(cmdOf("alpha")) || tm.look().has(cmdOf("bravo")) {
				t.Errorf("the pointer moved the selection:\n%s", tm.look().Text)
			}

			// The pointer goes to another row, the hover follows it.
			col2, row2 := tm.where("hang.example.com", false)
			tm.glide(col2, row2)
			tm.waitFor("the hover to move to the next row", func(snap) bool {
				return strings.Contains(tm.hovered(row2, look.under), "hang.example.com") &&
					tm.hovered(row, look.under) == ""
			})

			// Out of every element: the title row.
			tm.glide(2, 1)
			tm.waitFor("the hover to go away", func(snap) bool {
				return tm.hovered(row2, look.under) == ""
			})

			// A hint of the footer.
			fcol, frow := tm.where("ctrl+k", true)
			tm.glide(fcol, frow)
			tm.waitFor("the hint to take the hover look", func(snap) bool {
				return strings.Contains(tm.hovered(frow, look.under), "ctrl+k")
			})

			// A drag still selects text, and takes the hover away while it lasts.
			tm.glide(col, row)
			tm.waitFor("the hover on the row", func(snap) bool {
				return strings.Contains(tm.hovered(row, look.under), "bravo")
			})
			tm.send(fmt.Sprintf("\x1b[<0;%d;%dM", col, row))
			tm.send(fmt.Sprintf("\x1b[<32;%d;%dM", col+8, row))
			tm.waitText("Shift+drag")
			tm.send(fmt.Sprintf("\x1b[<0;%d;%dm", col+8, row))

			ran := lines(filepath.Join(sb.Out, "ssh.log"))
			probed := lines(filepath.Join(sb.Out, "ssh.probe"))

			if ran != 0 || probed != 0 {
				t.Errorf("the pointer started ssh %d times and asked it %d times", ran, probed)
			}

			// A lone Escape is held back by the key parser for a moment, long enough to take
			// the next key for Alt+key.
			tm.key("Escape")
			time.Sleep(300 * time.Millisecond)
			tm.quit()
			tm.waitMouse(off)

			end, err := os.ReadFile(raw)
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(string(end), "\x1b[?1003l") {
				t.Errorf("the program never gave up the all-motion reporting:\n%q", end)
			}
		})
	}
}
