package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ent is one entry of a test archive: a name, a type flag, the contents of a regular file, the
// target of a link, the mode and the extended records of the header.
type ent struct {
	name string
	typ  byte
	body string
	link string
	mode int64
	pax  map[string]string
}

// file builds a regular file entry.
//
// Parameters:
//   - name: path in the archive
//   - body: contents
func file(name, body string) ent {
	return ent{name: name, typ: tar.TypeReg, body: body, mode: 0o644}
}

// dirEnt builds a directory entry.
//
// Parameters:
//   - name: path in the archive
func dirEnt(name string) ent { return ent{name: name, typ: tar.TypeDir, mode: 0o755} }

// tarBytes writes entries as an uncompressed tar archive.
//
// Parameters:
//   - t: test to report to
//   - ents: the entries, in order
func tarBytes(t testing.TB, ents []ent) []byte {
	t.Helper()

	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)

	for _, e := range ents {
		h := &tar.Header{
			Name: e.name, Typeflag: e.typ, Mode: e.mode, Linkname: e.link,
			Size: int64(len(e.body)), Format: tar.FormatGNU,
		}

		if e.typ == tar.TypeChar || e.typ == tar.TypeBlock {
			h.Devmajor, h.Devminor = 1, 3
		}

		if e.typ != tar.TypeReg {
			h.Size = 0
		}

		if e.pax != nil {
			h.PAXRecords, h.Format = e.pax, tar.FormatPAX
		}

		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("header %q: %v", e.name, err)
		}

		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// gzipBytes compresses data as one gzip stream.
//
// Parameters:
//   - data: bytes to compress
func gzipBytes(data []byte) []byte {
	var buf bytes.Buffer

	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	_, _ = zw.Write(data)
	_ = zw.Close()

	return buf.Bytes()
}

// tgz builds a .tar.gz archive.
//
// Parameters:
//   - t: test to report to
//   - ents: the entries, in order
func tgz(t testing.TB, ents ...ent) []byte { return gzipBytes(tarBytes(t, ents)) }

// rawBlock writes a ustar header by hand, so that a name or a type can be anything.
//
// Parameters:
//   - name: the name field, at most 100 bytes
//   - typ: type flag
//   - size: size field
func rawBlock(name string, typ byte, size int) []byte {
	return rawBlockMagic(name, typ, size, false)
}

// rawBlockMagic writes a header by hand, in the ustar or the old GNU flavour.
//
// Parameters:
//   - name: the name field, at most 100 bytes
//   - typ: type flag
//   - size: size field
//   - gnu: true for the magic "ustar  " of GNU tar
func rawBlockMagic(name string, typ byte, size int, gnu bool) []byte {
	b := make([]byte, 512)
	copy(b, name)
	copy(b[100:], "0000644\x00")
	copy(b[108:], "0000000\x00")
	copy(b[116:], "0000000\x00")
	copy(b[124:], fmt.Sprintf("%011o\x00", size))
	copy(b[136:], "00000000000\x00")
	copy(b[148:], "        ")
	b[156] = typ
	copy(b[257:], "ustar\x0000")

	if gnu {
		copy(b[257:], "ustar  \x00")
	}

	sum := 0
	for _, c := range b {
		sum += int(c)
	}

	copy(b[148:], fmt.Sprintf("%06o\x00 ", sum))

	return b
}

// endBlocks is the end of a tar archive.
var endBlocks = make([]byte, 1024)

// pad rounds a body up to a whole number of tar blocks.
//
// Parameters:
//   - s: the body
func pad(s string) []byte {
	b := []byte(s)

	if r := len(b) % 512; r != 0 || len(b) == 0 {
		b = append(b, make([]byte, 512-r)...)
	}

	return b
}

// paxPath builds a tar archive whose extended header overrides the path of a file.
//
// Parameters:
//   - name: name in the ustar header
//   - path: path in the extended header
func paxPath(name, path string) []byte { return paxRecord(name, "path", path) }

// paxRecord builds a tar archive whose extended header holds one record.
//
// Parameters:
//   - name: name in the ustar header
//   - key: key of the record
//   - value: value of the record
func paxRecord(name, key, value string) []byte {
	rec := " " + key + "=" + value + "\n"
	rec = fmt.Sprintf("%d%s", len(rec)+len(fmt.Sprint(len(rec)+2)), rec)

	var out []byte

	out = append(out, rawBlock("PaxHeader", 'x', len(rec))...)
	out = append(out, pad(rec)...)
	out = append(out, rawBlock(name, '0', 1)...)
	out = append(out, pad("x")...)

	return append(out, endBlocks...)
}

// unpacked is the result of extracting a test archive into a fresh directory.
type unpacked struct {
	stage  string
	canary string
	parent string
	err    error
}

// extractBytes extracts an archive with the unpacker into <parent>/stage, next to a canary
// directory that must stay empty.
//
// Parameters:
//   - t: test to report to
//   - data: the compressed archive
func extractBytes(t *testing.T, data []byte) unpacked {
	t.Helper()

	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	canary := filepath.Join(parent, "canary")

	for _, d := range []string{stage, canary} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	root, err := os.OpenRoot(stage)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = root.Close() }()

	u := newUnpacker(context.Background(), root)

	return unpacked{stage: stage, canary: canary, parent: parent, err: u.run(bytes.NewReader(data))}
}

// listAll lists every path below a directory, slash separated and sorted by the walk.
//
// Parameters:
//   - t: test to report to
//   - dir: directory to walk
func listAll(t *testing.T, dir string) []string {
	t.Helper()

	var out []string

	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if p != dir {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// assertSealed checks that nothing was created outside the stage directory.
//
// Parameters:
//   - t: test to report to
//   - u: result of an extraction
func assertSealed(t *testing.T, u unpacked) {
	t.Helper()

	if got := listAll(t, u.canary); len(got) != 0 {
		t.Errorf("canary directory holds %v", got)
	}

	top := listAll(t, u.parent)

	for _, p := range top {
		if !strings.HasPrefix(p, "stage") && !strings.HasPrefix(p, "canary") {
			t.Errorf("file %q created next to the stage directory", p)
		}
	}
}

// wantArchiveError checks an error against a code and the second argument.
//
// Parameters:
//   - t: test to report to
//   - err: the error
//   - code: expected code
//   - reason: expected second argument, empty to skip
func wantArchiveError(t *testing.T, err error, code, reason string) {
	t.Helper()

	if err == nil {
		t.Fatalf("accepted, want %s %s", code, reason)
	}

	pe := asError(t, err)

	if pe.Code != code {
		t.Fatalf("code = %s %v, want %s", pe.Code, pe.Args, code)
	}

	if reason != "" && (len(pe.Args) < 2 || pe.Args[1] != reason) {
		t.Fatalf("args = %v, want reason %s", pe.Args, reason)
	}
}

// zeros returns n zero bytes.
//
// Parameters:
//   - n: length
func zeros(n int) []byte { return make([]byte, n) }

// hostileArchives are the fixtures SEC-X01 to SEC-X18 of the security draft, built in code.
func hostileArchives(t *testing.T) []struct {
	name   string
	data   []byte
	code   string
	reason string
} {
	t.Helper()

	var many []ent

	for i := range MaxTreeEntries + 1 {
		many = append(many, file(fmt.Sprintf("f%d", i), "x"))
	}

	good := tarBytes(t, []ent{file("plugin.toml", "x")})
	garbage := append(gzipBytes(good), []byte("trailing garbage")...)
	second := append(gzipBytes(good), gzipBytes(good)...)
	whole := gzipBytes(tarBytes(t, []ent{file("plugin.toml", strings.Repeat("abcdefgh", 4096))}))

	lie := append(append(rawBlock("f", '0', 1), pad("x")...), zeros(40<<20)...)
	tail := append(append([]byte(nil), good...), zeros(40<<20)...)
	corrupt := append([]byte(nil), whole...)
	corrupt[len(corrupt)-6] ^= 0xff

	return []struct {
		name   string
		data   []byte
		code   string
		reason string
	}{
		{"X01 parent", tgz(t, file("../evil", "x")), CodeArchiveUnsafe, UnsafeParent},
		{
			"X02 parent in the middle", tgz(t, file("a/../../evil", "x")), CodeArchiveUnsafe,
			UnsafeParent,
		},
		{"X03 absolute", tgz(t, file("/etc/cron.d/x", "x")), CodeArchiveUnsafe, UnsafeAbsolute},
		{"X04 backslash parent", tgz(t, file(`..\evil`, "x")), CodeArchiveUnsafe, UnsafeName},
		{"X04 backslash inside", tgz(t, file(`a\b`, "x")), CodeArchiveUnsafe, UnsafeName},
		{
			"X06 symlink then a file through it", tgz(t,
				ent{name: "link", typ: tar.TypeSymlink, link: "/etc"}, file("link/x", "x")),
			CodeArchiveUnsafe, UnsafeSymlink,
		},
		{
			"X07 symlink to parent", tgz(t, ent{name: "up", typ: tar.TypeSymlink, link: ".."}),
			CodeArchiveUnsafe, UnsafeSymlink,
		},
		{
			"X08 hard link", tgz(t, ent{name: "h", typ: tar.TypeLink, link: "/etc/passwd"}),
			CodeArchiveUnsafe, UnsafeLink,
		},
		{
			"X09 duplicate", tgz(t, file("a", "1"), file("a", "2")), CodeArchiveUnsafe,
			UnsafeDuplicate,
		},
		{
			"X09 duplicate directory", tgz(t, dirEnt("d/"), dirEnt("d/")), CodeArchiveUnsafe,
			UnsafeDuplicate,
		},
		{"X10 case clash", tgz(t, file("A", "1"), file("a", "2")), CodeArchiveUnsafe, UnsafeCase},
		{
			"X10 case clash in a parent", tgz(t, file("Dir/x", "1"), file("dir/y", "2")),
			CodeArchiveUnsafe, UnsafeCase,
		},
		{
			"X11 directory below a file", tgz(t, file("x", "1"), file("x/y", "2")),
			CodeArchiveUnsafe, UnsafeConflict,
		},
		{
			"X11 file over a directory", tgz(t, dirEnt("x"), file("x", "1")), CodeArchiveUnsafe,
			UnsafeConflict,
		},
		{
			"X13 character device", tgz(t, ent{name: "dev", typ: tar.TypeChar}), CodeArchiveUnsafe,
			UnsafeSpecial,
		},
		{
			"X13 block device", tgz(t, ent{name: "dev", typ: tar.TypeBlock}), CodeArchiveUnsafe,
			UnsafeSpecial,
		},
		{
			"X13 fifo", tgz(t, ent{name: "pipe", typ: tar.TypeFifo}), CodeArchiveUnsafe,
			UnsafeSpecial,
		},
		{
			"X14 pax path override", gzipBytes(paxPath("ok", "../../evil")), CodeArchiveUnsafe,
			UnsafePAX,
		},
		{
			"X14 pax global header with a path", tgz(t, ent{
				name: "g", typ: tar.TypeXGlobalHeader,
				pax: map[string]string{"path": "../../evil"},
			}, file("plugin.toml", "x")),
			CodeArchiveUnsafe, UnsafePAX,
		},
		{
			"X14 pax global header with a link path", tgz(t, ent{
				name: "g",
				typ:  tar.TypeXGlobalHeader, pax: map[string]string{"linkpath": "/etc"},
			}),
			CodeArchiveUnsafe, UnsafePAX,
		},
		{
			"X14 gnu long name with a parent", tgz(t, file(strings.Repeat("a", 120)+"/../x", "x")),
			CodeArchiveUnsafe, UnsafeParent,
		},
		{"X15 gnu sparse type", gzipBytes(append(append(rawBlockMagic("s", 'S', 1, true),
			pad("x")...), endBlocks...)), CodeArchiveUnsafe, UnsafeSpecial},
		{
			"X15 pax sparse record", gzipBytes(paxRecord("s", "GNU.sparse.major", "1")),
			CodeArchiveUnsafe, UnsafePAX,
		},
		{"X15 size lies, a bomb follows", gzipBytes(lie), CodeArchiveLimits, LimitBytes},
		{"X16 declared size of one gibibyte", gzipBytes(append(rawBlock("big", '0', 1<<30),
			endBlocks...)), CodeArchiveLimits, LimitBytes},
		{
			"X16 over the file cap", tgz(t, file("big", strings.Repeat("a", MaxTreeBytes+1))),
			CodeArchiveLimits, LimitBytes,
		},
		{"X16 zeros after the tar end", gzipBytes(tail), CodeArchiveLimits, LimitBytes},
		{"X17 too many entries", tgz(t, many...), CodeArchiveLimits, LimitEntries},
		{"X17 depth seven", tgz(t, file("a/b/c/d/e/f/g", "x")), CodeArchiveLimits, LimitDepth},
		{
			"X17 path of 256 bytes", tgz(t, file(strings.Repeat("a", 77)+"/"+strings.Repeat("b",
				77)+"/"+strings.Repeat("c", 49)+"/"+strings.Repeat("d", 50), "x")),
			CodeArchiveLimits, LimitPath,
		},
		{
			"X17 component of 256 bytes", tgz(t, file(strings.Repeat("a", 256), "x")),
			CodeArchiveLimits, LimitPath,
		},
		{"X18 truncated gzip", whole[:len(whole)/2], CodeArchiveFormat, ArchiveTruncated},
		{
			"X18 truncated before the trailer", whole[:len(whole)-4], CodeArchiveFormat,
			ArchiveTruncated,
		},
		{"X18 trailing garbage", garbage, CodeArchiveFormat, ArchiveTrailing},
		{"X18 two streams", second, CodeArchiveFormat, ArchiveTrailing},
		{"X18 corrupt checksum", corrupt, CodeArchiveFormat, ArchiveCorrupt},
		{"X18 non zero bytes after the tar end", gzipBytes(append(append([]byte(nil), good...),
			[]byte("junk")...)), CodeArchiveFormat, ArchiveTrailing},
		{"plain tar", good, CodeArchiveFormat, ArchiveMagic},
		{"empty", nil, CodeArchiveFormat, ArchiveMagic},
		{"text", []byte("hello"), CodeArchiveFormat, ArchiveMagic},
		{
			"garbage after the magic",
			[]byte{0x1f, 0x8b, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			CodeArchiveFormat, "",
		},
	}
}

// TestHostileArchives extracts every hostile fixture and checks the code, the reason, and that
// nothing was created outside the staging directory.
//
// Parameters:
//   - t: test to report to
func TestHostileArchives(t *testing.T) {
	for _, tt := range hostileArchives(t) {
		t.Run(tt.name, func(t *testing.T) {
			u := extractBytes(t, tt.data)

			wantArchiveError(t, u.err, tt.code, tt.reason)
			assertSealed(t, u)
		})
	}
}

// TestEntryNames checks the rules of a name on their own, for the cases that no tar writer or
// reader lets through: the reader cuts a name at a NUL byte, so a NUL cannot reach the check from
// a ustar header, and the rule is a second line of defence.
//
// Parameters:
//   - t: test to report to
func TestEntryNames(t *testing.T) {
	tests := []struct {
		raw    string
		dir    bool
		want   string
		reason string
	}{
		{"plugin.toml", false, "plugin.toml", ""},
		{"./plugin.toml", false, "plugin.toml", ""},
		{"a/b/", true, "a/b", ""},
		{"./", true, "", ""},
		{".", true, "", ""},
		{"a\x00b", false, "", UnsafeName},
		{"a\\b", false, "", UnsafeName},
		{"", false, "", UnsafeName},
		{"./", false, "", UnsafeName},
		{"x/", false, "", UnsafeName},
		{"a//b", false, "", UnsafeName},
		{"a/./b", false, "", UnsafeName},
		{"././a", false, "", UnsafeName},
		{"-a", false, "", UnsafeName},
		{"a/-b", false, "", UnsafeName},
		{"a b", false, "", UnsafeName},
		{"caf\u00e9", false, "", UnsafeName},
		{"/a", false, "", UnsafeAbsolute},
		{"//a", false, "", UnsafeAbsolute},
		{"..", false, "", UnsafeParent},
		{"a/..", false, "", UnsafeParent},
		{"../a", true, "", UnsafeParent},
		{".hidden", false, ".hidden", ""},
		{"a...b", false, "a...b", ""},
	}

	for _, tt := range tests {
		got, err := entryName(tt.raw, tt.dir)

		if tt.reason == "" {
			if err != nil || got != tt.want {
				t.Errorf("entryName(%q) = %q, %v; want %q", tt.raw, got, err, tt.want)
			}

			continue
		}

		wantArchiveError(t, err, CodeArchiveUnsafe, tt.reason)
	}
}

// TestHostileArchiveNamesTheEntry checks that the error carries the entry that broke the rules.
//
// Parameters:
//   - t: test to report to
func TestHostileArchiveNamesTheEntry(t *testing.T) {
	u := extractBytes(t, tgz(t, file("ok", "x"), file("sub/../../evil", "x")))

	pe := asError(t, u.err)
	if len(pe.Args) < 1 || pe.Args[0] != "sub/../../evil" {
		t.Errorf("args = %v", pe.Args)
	}
}

// TestUnpackGoodArchive extracts a fine archive, with the layouts that real tools produce: a root
// entry, ./ prefixes, directory entries with and without a trailing slash and files whose parents
// have no entry. Modes are normalised and no special bit survives.
//
// Parameters:
//   - t: test to report to
func TestUnpackGoodArchive(t *testing.T) {
	data := tgz(t,
		dirEnt("./"),
		file("./plugin.toml", "manifest"),
		dirEnt("locales/"),
		file("locales/en.toml", "en"),
		dirEnt("docs"),
		file("docs/a/b.txt", "b"),
		ent{name: "bin/demo", typ: tar.TypeReg, body: "#!/bin/sh\n", mode: 0o4777},
		ent{name: "set", typ: tar.TypeReg, body: "x", mode: 0o2755},
		ent{name: "sticky", typ: tar.TypeReg, body: "x", mode: 0o1777},
		dirEnt("docs/a"),
	)

	u := extractBytes(t, data)
	if u.err != nil {
		t.Fatal(u.err)
	}

	assertSealed(t, u)

	want := []string{
		"bin", "bin/demo", "docs", "docs/a", "docs/a/b.txt", "locales",
		"locales/en.toml", "plugin.toml", "set", "sticky",
	}

	if got := listAll(t, u.stage); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tree = %v, want %v", got, want)
	}

	for _, rel := range want {
		info, err := os.Lstat(filepath.Join(u.stage, rel))
		if err != nil {
			t.Fatal(err)
		}

		wantMode := fs.FileMode(0o600)
		if info.IsDir() {
			wantMode = fs.ModeDir | 0o700
		}

		if info.Mode() != wantMode {
			t.Errorf("%s mode = %v, want %v", rel, info.Mode(), wantMode)
		}
	}

	body, _ := os.ReadFile(filepath.Join(u.stage, "docs/a/b.txt"))
	if string(body) != "b" {
		t.Errorf("content = %q", body)
	}
}

// TestUnpackZerosAfterTheEnd checks that the padding real tools write after a tar is accepted.
//
// Parameters:
//   - t: test to report to
func TestUnpackZerosAfterTheEnd(t *testing.T) {
	data := gzipBytes(append(tarBytes(t, []ent{file("plugin.toml", "x")}), zeros(10240)...))

	if u := extractBytes(t, data); u.err != nil {
		t.Fatal(u.err)
	}
}

// TestUnpackAtTheLimits checks the largest archives that are accepted: 512 entries, depth six,
// a path of 255 bytes and exactly 16 MiB of contents.
//
// Parameters:
//   - t: test to report to
func TestUnpackAtTheLimits(t *testing.T) {
	var ents []ent

	for i := range MaxTreeEntries {
		ents = append(ents, file(fmt.Sprintf("f%d", i), "x"))
	}

	cases := map[string][]ent{
		"512 entries": ents,
		"depth six":   {file("a/b/c/d/e/f", "x")},
		"path of 255": {file(strings.Repeat("a", 77)+"/"+strings.Repeat("b", 77)+"/"+
			strings.Repeat("c", 49)+"/"+strings.Repeat("d", 49), "x")},
		"16 MiB": {
			file("a", strings.Repeat("a", MaxTreeBytes/2)),
			file("b", strings.Repeat("b", MaxTreeBytes/2)),
		},
	}

	for name, es := range cases {
		t.Run(name, func(t *testing.T) {
			if u := extractBytes(t, tgz(t, es...)); u.err != nil {
				t.Fatal(u.err)
			}
		})
	}
}

// TestUnpackOneByteOver checks that 16 MiB plus one byte is refused, whichever file holds it.
//
// Parameters:
//   - t: test to report to
func TestUnpackOneByteOver(t *testing.T) {
	data := tgz(t, file("a", strings.Repeat("a", MaxTreeBytes/2)),
		file("b", strings.Repeat("b", MaxTreeBytes/2+1)))

	u := extractBytes(t, data)

	wantArchiveError(t, u.err, CodeArchiveLimits, LimitBytes)
}

// TestUnpackStopsAtCancel checks that a canceled context stops the extraction.
//
// Parameters:
//   - t: test to report to
func TestUnpackStopsAtCancel(t *testing.T) {
	dir := t.TempDir()

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = root.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = newUnpacker(ctx, root).run(bytes.NewReader(tgz(t, file("a", "x"))))
	if err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
}

// TestUnpackTwoArchivesShareNames checks that a path in both archives of a package is an error,
// and that two archives that do not overlap make one tree.
//
// Parameters:
//   - t: test to report to
func TestUnpackTwoArchivesShareNames(t *testing.T) {
	dir := t.TempDir()

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = root.Close() }()

	u := newUnpacker(context.Background(), root)

	first := tgz(t, file("plugin.toml", "a"), file("bin/x", "1"))
	if err := u.run(bytes.NewReader(first)); err != nil {
		t.Fatal(err)
	}

	if err := u.run(bytes.NewReader(tgz(t, file("bin/y", "2")))); err != nil {
		t.Fatalf("a second archive in a shared directory: %v", err)
	}

	err = u.run(bytes.NewReader(tgz(t, file("bin/x", "evil"))))

	wantArchiveError(t, err, CodeArchiveUnsafe, UnsafeDuplicate)

	body, _ := os.ReadFile(filepath.Join(dir, "bin/x"))
	if string(body) != "1" {
		t.Errorf("a file was overwritten: %q", body)
	}
}

// FuzzUnpack feeds the extractor with arbitrary bytes: it must not panic and must not write
// outside its directory.
//
// Parameters:
//   - f: fuzz target
func FuzzUnpack(f *testing.F) {
	f.Add(gzipBytes([]byte("hello")))
	f.Add(tgz(f, file("plugin.toml", "x"), dirEnt("a/")))
	f.Add(tgz(f, file("../x", "y")))
	f.Add([]byte{0x1f, 0x8b})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}

		u := extractBytes(t, data)
		assertSealed(t, u)
	})
}

// TestUnpackGlobalHeader checks that the global extended header of git archive, which only holds
// a note, is skipped and does not count as an entry, while one with a path is refused.
//
// Parameters:
//   - t: test to report to
func TestUnpackGlobalHeader(t *testing.T) {
	note := ent{
		name: "pax_global_header", typ: tar.TypeXGlobalHeader,
		pax: map[string]string{"comment": "0123456789abcdef0123456789abcdef01234567"},
	}

	ents := []ent{note}

	for i := range MaxTreeEntries {
		ents = append(ents, file(fmt.Sprintf("f%d", i), "x"), note)
	}

	u := extractBytes(t, tgz(t, ents...))
	if u.err != nil {
		t.Fatal(u.err)
	}

	assertSealed(t, u)

	if got := listAll(t, u.stage); len(got) != MaxTreeEntries {
		t.Errorf("%d files, want %d: the notes are not entries", len(got), MaxTreeEntries)
	}
}

// TestUnpackBomb feeds the extractor a valid archive followed by 256 MiB of zeros inside the same
// gzip stream, which is about 250 KiB once compressed, and checks that the work stops at the cap:
// only a few percent of the compressed bytes are read, and nothing is written outside the staging
// directory.
//
// Parameters:
//   - t: test to report to
func TestUnpackBomb(t *testing.T) {
	var packed bytes.Buffer

	zw, _ := gzip.NewWriterLevel(&packed, gzip.BestSpeed)
	_, _ = zw.Write(tarBytes(t, []ent{file("plugin.toml", "x")}))

	chunk := zeros(1 << 20)

	for range 256 {
		_, _ = zw.Write(chunk)
	}

	_ = zw.Close()

	if packed.Len() > 2<<20 {
		t.Fatalf("the fixture is %d bytes: not a bomb", packed.Len())
	}

	counted := &countingReader{r: bytes.NewReader(packed.Bytes())}
	parent := t.TempDir()

	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = root.Close() }()

	err = newUnpacker(context.Background(), root).run(counted)
	wantArchiveError(t, err, CodeArchiveLimits, LimitBytes)

	if got := listAll(t, parent); strings.Join(got, ",") != "plugin.toml" {
		t.Errorf("the bomb left %v", got)
	}

	if counted.n > int64(packed.Len())/4 {
		t.Errorf("read %d of %d compressed bytes: the cap does not stop the work", counted.n,
			packed.Len())
	}
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

// Read implements io.Reader.
//
// Parameters:
//   - p: buffer to fill
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)

	return n, err
}

// TestUnpackSystemTar extracts archives made by the tar of this machine, in the three formats it
// writes, with and without the leading "./" and the directory entries: the rules must refuse
// hostile archives, not the ones real tools make.
//
// Parameters:
//   - t: test to report to
func TestUnpackSystemTar(t *testing.T) {
	bin, err := exec.LookPath("tar")
	if err != nil {
		t.Skip("no tar on this machine")
	}

	src := t.TempDir()

	for name, body := range map[string]string{
		"plugin.toml": "m", "locales/en.toml": "e",
		"README.md": "r",
	} {
		p := filepath.Join(src, filepath.FromSlash(name))

		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte(body), 0o644))
	}

	want := []string{"README.md", "locales", "locales/en.toml", "plugin.toml"}

	for _, format := range []string{"gnu", "pax", "ustar", "v7"} {
		for _, names := range [][]string{
			{"."},
			{"plugin.toml", "locales", "README.md"},
			{"./plugin.toml", "./locales/", "./README.md"},
		} {
			t.Run(format+" "+strings.Join(names, " "), func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "x.tar.gz")
				args := append([]string{"--format=" + format, "-czf", out, "-C", src}, names...)

				if msg, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
					t.Skipf("tar: %v %s", err, msg)
				}

				data, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}

				u := extractBytes(t, data)
				if u.err != nil {
					t.Fatal(u.err)
				}

				assertSealed(t, u)

				if got := listAll(t, u.stage); strings.Join(got, ",") != strings.Join(want, ",") {
					t.Errorf("tree = %v, want %v", got, want)
				}
			})
		}
	}
}
