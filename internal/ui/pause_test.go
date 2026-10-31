package ui

import (
	"os"
	"testing"
)

// TestWaitKey checks which bytes end the wait of the pause and that nothing after them is read.
//
// Parameters:
//   - t: test to report to
func TestWaitKey(t *testing.T) {
	tests := []struct {
		name string
		in   string
		rest string
	}{
		{"line feed", "ab\nrest", "rest"},
		{"carriage return", "ab\rrest", "rest"},
		{"ctrl-c", "ab\x03rest", "rest"},
		{"ctrl-d", "\x04rest", "rest"},
		{"nothing to end it", "abc", ""},
	}

	for _, tt := range tests {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}

		if _, err := w.WriteString(tt.in); err != nil {
			t.Fatal(err)
		}

		_ = w.Close()

		waitKey(r, pauseKeys, nil, nil)

		buf := make([]byte, 16)
		n, _ := r.Read(buf)

		if string(buf[:n]) != tt.rest {
			t.Errorf("%s: left %q, want %q", tt.name, buf[:n], tt.rest)
		}

		_ = r.Close()
	}
}

// TestRawInputNotATerminal checks that a pipe or a regular file is left alone.
//
// Parameters:
//   - t: test to report to
func TestRawInputNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	if rawInput(r) != nil {
		t.Error("a pipe was put in raw mode")
	}

	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	if rawInput(f) != nil {
		t.Error("the null device was put in raw mode")
	}
}
