package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// endless is a reader that never produces a newline and counts what was taken from it.
//
// taken is the number of bytes handed out so far. After limit bytes the reader fails, so that a
// consumer without a bound ends the test instead of exhausting the memory.
type endless struct {
	taken int
	limit int
}

// errEndlessCut is the error of an endless reader whose limit was reached.
var errEndlessCut = errors.New("endless reader cut")

// Read fills the buffer with 'x' bytes until the limit is reached.
//
// Parameters:
//   - p: buffer to fill
//
// Returns the number of bytes written, or errEndlessCut once the limit is reached.
func (e *endless) Read(p []byte) (int, error) {
	if e.taken >= e.limit {
		return 0, errEndlessCut
	}

	n := min(len(p), e.limit-e.taken)
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}

	e.taken += n

	return n, nil
}

// TestReviewConfirmReadsABoundedAnswer checks that the confirmation prompt does not read an
// unbounded amount of input when the input is not a real terminal, for instance a character
// device such as /dev/zero that the terminal check cannot tell from a tty.
//
// Parameters:
//   - t: test to report to
func TestReviewConfirmReadsABoundedAnswer(t *testing.T) {
	h := newHarness(t)
	h.fixtures()

	const limit = 64 << 20

	in := &endless{limit: limit}
	a := *h.app
	a.Stdin = in
	a.IsInputTerminal = func(io.Reader) bool { return true }

	ok, rc := a.confirm("kind_alias", "gs")

	if ok || rc != exitFail {
		t.Errorf("confirm = %v, %d; want false, %d", ok, rc, exitFail)
	}

	if in.taken >= limit {
		t.Errorf("the prompt consumed %d bytes of an endless line", in.taken)
	}

	if !strings.Contains(h.read("aliases.toml"), `name = "gs"`) {
		t.Error("the alias was removed")
	}
}
