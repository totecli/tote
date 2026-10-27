package store

import (
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// rtmChunk is one piece of the expected file of the byte model.
//
// Key is empty for text that no edit may ever touch. For a record, Prefix is the run of comment
// lines that touches its header from above, Header is the header line without its line ending,
// Desc is the description (empty when the key is absent), Note is the trailing comment of the
// description line and Extra is raw text of other keys that follows it, which a replace drops
// because the replacement has no such field. Text is unused for a record. Fixed text keeps its
// bytes in Text.
type rtmChunk struct {
	Key    string
	Prefix string
	Header string
	Desc   string
	Note   string
	Extra  string
	Text   string
}

// rtmFixture is the starting file of the byte model, written with bare newlines.
//
// It holds a file header, a section comment, comment runs owned by blocks, an inline comment, a
// header with a trailing comment, a multi-line string and a multi-line array that both hold
// lines that look like headers, and a comment that is directly under a block but separated from
// the next one by a blank line.
func rtmFixture() []rtmChunk {
	return []rtmChunk{
		{Text: "# header comment\n\nschema = 1\n\n# --- clients ---\n\n"},
		{
			Key: "a", Prefix: "# owned a1\n# owned a2\n", Header: "[[rec]]", Desc: "alpha",
			Note: "   # inline",
		},
		{Text: "\n# detached note\n\n"},
		{Key: "h", Header: "[[rec]]   # trailing", Extra: "tags = [\n  \"x\",   # [[rec]]\n" +
			"  \"[[rec]]\",\n]\n", Desc: "\"\"\"\n[[rec]]\nname = \"fake\"\n# not a comment\n\"\"\""},
		{Text: "\n# --- internal ---\n\n"},
		{Key: "b", Header: "[[rec]]"},
		{Text: "# after b\n\n"},
		{Key: "c", Prefix: "# about c\n", Header: "[[rec]]"},
		{Text: "\n# end of file\n"},
	}
}

// rtmBody renders the block of a record chunk from its header line to its last key line.
//
// Parameters:
//   - ch: record chunk
func rtmBody(ch rtmChunk) string {
	out := ch.Header + "\nname = " + strconv.Quote(ch.Key) + "\n"
	if ch.Desc != "" {
		desc := ch.Desc
		if !strings.HasPrefix(desc, "\"\"\"") {
			desc = strconv.Quote(desc)
		}
		out += "desc = " + desc + ch.Note + "\n"
	}

	return out + ch.Extra
}

// rtmJoin concatenates the chunks and applies the line ending of the variant under test.
//
// Parameters:
//   - chunks: model of the file
//   - eol: line ending to use
func rtmJoin(chunks []rtmChunk, eol string) string {
	var b strings.Builder
	for _, ch := range chunks {
		b.WriteString(ch.Text)
		if ch.Key != "" {
			b.WriteString(ch.Prefix + rtmBody(ch))
		}
	}
	if eol == "\n" {
		return b.String()
	}

	return strings.ReplaceAll(b.String(), "\n", eol)
}

// rtmIndex returns the position of the chunk of a record key.
//
// Parameters:
//   - chunks: model of the file
//   - key: record key
func rtmIndex(chunks []rtmChunk, key string) int {
	return slices.IndexFunc(chunks, func(ch rtmChunk) bool { return ch.Key == key })
}

// rtmKeys lists the record keys of the model in file order.
//
// Parameters:
//   - chunks: model of the file
func rtmKeys(chunks []rtmChunk) []string {
	var keys []string
	for _, ch := range chunks {
		if ch.Key != "" {
			keys = append(keys, ch.Key)
		}
	}

	return keys
}

// rtmAddChunks appends a block to the model with the separator rule of the writer.
//
// Parameters:
//   - chunks: model of the file
//   - name: record name
//   - desc: record description
func rtmAddChunks(chunks []rtmChunk, name, desc string) []rtmChunk {
	text := rtmJoin(chunks, "\n")
	last := strings.TrimSuffix(text, "\n")
	last = last[strings.LastIndex(last, "\n")+1:]
	if strings.TrimSpace(last) != "" {
		chunks = append(chunks, rtmChunk{Text: "\n"})
	}

	return append(chunks, rtmChunk{Key: name, Header: "[[rec]]", Desc: desc})
}

// rtmStep applies one random edit to a collection and to the model.
//
// Parameters:
//   - t: test to report to
//   - rng: source of randomness
//   - c: collection to edit
//   - chunks: model of the file before the edit
//   - step: step number, used to build unique names
//
// Returns the model after the edit.
func rtmStep(t *testing.T, rng *rand.Rand, c *Collection[testRec], chunks []rtmChunk,
	step int,
) []rtmChunk {
	t.Helper()

	name := fmt.Sprintf("n%d", step)
	desc := []string{"", "plain words", "with [[rec]] inside", "# not a comment"}[rng.Intn(4)]
	keys := rtmKeys(chunks)
	if len(keys) == 0 || rng.Intn(3) == 0 {
		if err := c.Add(testRec{Name: name, Desc: desc}); err != nil {
			t.Fatalf("step %d: Add = %v", step, err)
		}
		return rtmAddChunks(chunks, name, desc)
	}

	key := keys[rng.Intn(len(keys))]
	i := rtmIndex(chunks, key)
	if rng.Intn(2) == 0 {
		if err := c.Delete(key); err != nil {
			t.Fatalf("step %d: Delete(%s) = %v", step, key, err)
		}
		return slices.Delete(chunks, i, i+1)
	}

	if rng.Intn(2) == 0 {
		name = key
	}
	if err := c.Replace(key, testRec{Name: name, Desc: desc}); err != nil {
		t.Fatalf("step %d: Replace(%s) = %v", step, key, err)
	}
	chunks[i].Key = name
	chunks[i].Extra = ""
	if desc == "" {
		chunks[i].Note = ""
	}
	chunks[i].Desc = desc

	return chunks
}

// TestReviewByteModel checks, byte for byte, that an edit touches nothing outside its block.
//
// The expected file is computed by a model made of untouchable text chunks and record chunks,
// for a file with bare newlines and for a file with CRLF line endings, over long random
// sequences of adds, replaces, renames and deletes.
//
// Parameters:
//   - t: test to report to
func TestReviewByteModel(t *testing.T) {
	for _, eol := range []string{"\n", "\r\n"} {
		for seed := int64(1); seed <= 6; seed++ {
			t.Run(fmt.Sprintf("%q/seed%d", eol, seed), func(t *testing.T) {
				rtmRun(t, eol, seed)
			})
		}
	}
}

// rtmRun plays one random sequence of edits against the byte model.
//
// Parameters:
//   - t: test to report to
//   - eol: line ending of the file
//   - seed: seed of the random sequence
func rtmRun(t *testing.T, eol string, seed int64) {
	t.Helper()

	chunks := rtmFixture()
	path := engWrite(t, rtmJoin(chunks, eol))
	c := engOpen(t, path)
	rng := rand.New(rand.NewSource(seed))

	for step := range 40 {
		chunks = rtmStep(t, rng, c, chunks, step)

		if got, want := engRead(t, path), rtmJoin(chunks, eol); got != want {
			t.Fatalf("step %d: file differs from the model\n got: %q\nwant: %q", step, got, want)
		}
		if step%7 == 0 {
			c = engOpen(t, path)
		}
	}
	if got := engKeys(c.List()); !slices.Equal(got, rtmKeys(chunks)) {
		t.Errorf("keys = %v, want %v", got, rtmKeys(chunks))
	}
}
