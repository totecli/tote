package i18n

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// nbsp is the non-breaking space used before French high punctuation and inside guillemets.
const nbsp = "\u00a0"

// verbs returns the Printf directives of a message in order, as written.
//
// Parameters:
//   - msg: catalogue message
//
// Returns directives such as "%s", "%q" or "%[1]d"; "%%" is not a directive.
func verbs(msg string) []string {
	var out []string

	for i := 0; i < len(msg); i++ {
		if msg[i] != '%' {
			continue
		}

		j := i + 1
		for j < len(msg) && strings.IndexByte("+-# 0123456789.*[]", msg[j]) >= 0 {
			j++
		}

		switch {
		case j >= len(msg):
			out = append(out, msg[i:])
		case j == i+1 && msg[j] == '%':
		default:
			out = append(out, msg[i:j+1])
		}

		i = j
	}

	return out
}

// catalogs parses both embedded catalogues and fails the test when one cannot be decoded.
//
// Parameters:
//   - t: running test
//
// Returns the English and the French messages.
func catalogs(t *testing.T) (map[string]string, map[string]string) {
	t.Helper()

	en, err := parse(enData)
	if err != nil {
		t.Fatalf("en.toml: %v", err)
	}

	fr, err := parse(frData)
	if err != nil {
		t.Fatalf("fr.toml: %v", err)
	}

	return en, fr
}

// TestVerbs checks the directive scanner on plain, quoted, indexed and escaped percent signs.
//
// Parameters:
//   - t: test to report to
func TestVerbs(t *testing.T) {
	tests := []struct {
		msg  string
		want []string
	}{
		{"no verb", nil},
		{"added %s %q", []string{"%s", "%q"}},
		{"100%% of %d", []string{"%d"}},
		{"%[2]s then %[1]s", []string{"%[2]s", "%[1]s"}},
		{"%-10s|%5.2f", []string{"%-10s", "%5.2f"}},
		{"dangling %", []string{"%"}},
	}

	for _, tt := range tests {
		if got := verbs(tt.msg); !slices.Equal(got, tt.want) {
			t.Errorf("verbs(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

// TestParity checks that both catalogues define exactly the same keys.
//
// Parameters:
//   - t: test to report to
func TestParity(t *testing.T) {
	en, fr := catalogs(t)

	var missingFr, missingEn []string

	for key := range en {
		if _, ok := fr[key]; !ok {
			missingFr = append(missingFr, key)
		}
	}

	for key := range fr {
		if _, ok := en[key]; !ok {
			missingEn = append(missingEn, key)
		}
	}

	slices.Sort(missingFr)
	slices.Sort(missingEn)

	if len(missingFr) > 0 {
		t.Errorf("fr.toml lacks %d key(s) present in en.toml:\n  %s",
			len(missingFr), strings.Join(missingFr, "\n  "))
	}

	if len(missingEn) > 0 {
		t.Errorf("en.toml lacks %d key(s) present in fr.toml:\n  %s",
			len(missingEn), strings.Join(missingEn, "\n  "))
	}
}

// TestVerbParity checks that a key has the same Printf directives in both languages.
//
// Parameters:
//   - t: test to report to
func TestVerbParity(t *testing.T) {
	en, fr := catalogs(t)

	for _, key := range slices.Sorted(maps.Keys(en)) {
		frMsg, ok := fr[key]
		if !ok {
			continue
		}

		if a, b := verbs(en[key]), verbs(frMsg); !slices.Equal(a, b) {
			t.Errorf("%s: directives differ: en %q, fr %q", key, a, b)
		}
	}
}

// TestFormatsCleanly checks that every message formats without a Printf error.
//
// Parameters:
//   - t: test to report to
func TestFormatsCleanly(t *testing.T) {
	en, fr := catalogs(t)

	for name, messages := range map[string]map[string]string{"en": en, "fr": fr} {
		for _, key := range slices.Sorted(maps.Keys(messages)) {
			found := verbs(messages[key])
			if slices.ContainsFunc(found, func(v string) bool { return strings.Contains(v, "[") }) {
				continue
			}

			args := make([]any, len(found))
			for i, v := range found {
				args[i] = "x"
				if strings.HasSuffix(v, "d") {
					args[i] = 1
				}
			}

			if got := fmt.Sprintf(messages[key], args...); strings.Contains(got, "%!") {
				t.Errorf("%s.%s: bad format: %q", name, key, got)
			}
		}
	}
}

// TestDuplicateKey checks that a repeated key is a decoding error.
//
// Parameters:
//   - t: test to report to
func TestDuplicateKey(t *testing.T) {
	if _, err := parse([]byte("a = \"x\"\na = \"y\"\n")); err == nil {
		t.Error("parse accepted a duplicate key")
	}
}

// TestNoDuplicateLines checks the raw files line by line for a key defined twice.
//
// Parameters:
//   - t: test to report to
func TestNoDuplicateLines(t *testing.T) {
	keyLine := regexp.MustCompile(`^([A-Za-z0-9_]+)\s*=`)

	for name, data := range map[string][]byte{"en.toml": enData, "fr.toml": frData} {
		seen := map[string]int{}
		sc := bufio.NewScanner(bytes.NewReader(data))

		for n := 1; sc.Scan(); n++ {
			m := keyLine.FindStringSubmatch(sc.Text())
			if m == nil {
				continue
			}

			if first, dup := seen[m[1]]; dup {
				t.Errorf("%s:%d: key %s already defined at line %d", name, n, m[1], first)
			}

			seen[m[1]] = n
		}
	}
}

// TestSortedFiles checks that each file has one key per line in strictly ascending order.
//
// Parameters:
//   - t: test to report to
func TestSortedFiles(t *testing.T) {
	keyLine := regexp.MustCompile(`^([A-Za-z0-9_]+) = "`)

	for name, data := range map[string][]byte{"en.toml": enData, "fr.toml": frData} {
		prev := ""
		sc := bufio.NewScanner(bytes.NewReader(data))

		for n := 1; sc.Scan(); n++ {
			m := keyLine.FindStringSubmatch(sc.Text())
			if m == nil {
				t.Errorf("%s:%d: not a `key = \"value\"` line: %q", name, n, sc.Text())
				continue
			}

			if m[1] <= prev {
				t.Errorf("%s:%d: key %s is not after %s", name, n, m[1], prev)
			}

			prev = m[1]
		}
	}
}

// TestValues checks that values are non-empty, valid UTF-8 and free of trailing space.
//
// Parameters:
//   - t: test to report to
func TestValues(t *testing.T) {
	en, fr := catalogs(t)

	for name, messages := range map[string]map[string]string{"en": en, "fr": fr} {
		for key, msg := range messages {
			switch {
			case msg == "":
				t.Errorf("%s.%s: empty value", name, key)
			case !utf8.ValidString(msg):
				t.Errorf("%s.%s: invalid UTF-8", name, key)
			case strings.TrimRight(msg, " \t\r\n\u00a0") != msg:
				t.Errorf("%s.%s: trailing space in %q", name, key, msg)
			}
		}
	}
}

// TestFrenchStyle checks the typography rules of the French catalogue.
//
// Parameters:
//   - t: test to report to
func TestFrenchStyle(t *testing.T) {
	_, fr := catalogs(t)

	for _, key := range slices.Sorted(maps.Keys(fr)) {
		msg := fr[key]

		switch {
		case strings.Contains(msg, "'"):
			t.Errorf("%s: straight apostrophe in %q, use the typographic one", key, msg)
		case strings.ContainsAny(msg, "\"") || strings.Contains(msg, "..."):
			t.Errorf("%s: straight quote or three dots in %q", key, msg)
		}

		for _, p := range []string{" :", " ;", " !", " ?", "\u00ab ", " \u00bb"} {
			if strings.Contains(msg, p) {
				t.Errorf("%s: plain space before %q in %q, use a non-breaking space", key, p, msg)
			}
		}

		if strings.Contains(msg, "\u00ab") != strings.Contains(msg, nbsp+"\u00bb") ||
			strings.Contains(msg, "\u00ab") != strings.Contains(msg, "\u00ab"+nbsp) {
			t.Errorf("%s: guillemets need a non-breaking space inside in %q", key, msg)
		}
	}
}

// TestKeys checks that Keys lists the English keys in sorted order.
//
// Parameters:
//   - t: test to report to
func TestKeys(t *testing.T) {
	en, _ := catalogs(t)
	got := Keys()

	if len(got) == 0 || !slices.IsSorted(got) {
		t.Fatalf("Keys() = %q, want a non-empty sorted list", got)
	}

	if !slices.Equal(got, slices.Sorted(maps.Keys(en))) {
		t.Error("Keys() does not match the keys of en.toml")
	}
}

// TestNew checks the language selection and its fallback to English.
//
// Parameters:
//   - t: test to report to
func TestNew(t *testing.T) {
	tests := []struct{ in, want string }{
		{"fr", "fr"},
		{"en", "en"},
		{"", "en"},
		{"de", "en"},
		{"FR", "en"},
	}

	for _, tt := range tests {
		if got := New(tt.in).Lang(); got != tt.want {
			t.Errorf("New(%q).Lang() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestT checks translation, formatting, the English fallback and the missing-key marker.
//
// Parameters:
//   - t: test to report to
func TestT(t *testing.T) {
	en, fr := New("en"), New("fr")

	if got, want := en.T("cli_unknown_command", "foo"), `unknown command "foo"`; got != want {
		t.Errorf("en unknown command = %q, want %q", got, want)
	}

	if got, want := fr.T("cli_unknown_command", "foo"), `commande inconnue "foo"`; got != want {
		t.Errorf("fr unknown command = %q, want %q", got, want)
	}

	if got, want := fr.T("header_name"), "NOM"; got != want {
		t.Errorf("fr header_name = %q, want %q", got, want)
	}

	if got, want := en.T("no_such_key", 1), "!no_such_key!"; got != want {
		t.Errorf("missing key = %q, want %q", got, want)
	}

	if got, want := New("de").T("header_name"), "NAME"; got != want {
		t.Errorf("unknown language = %q, want %q", got, want)
	}
}

// TestFallback checks that a key missing in the own catalogue is read from the English one.
//
// Parameters:
//   - t: test to report to
func TestFallback(t *testing.T) {
	p := &Printer{
		lang: "fr",
		own:  map[string]string{"a": "un"},
		base: map[string]string{"a": "one", "b": "two %d"},
	}

	if got := p.T("a"); got != "un" {
		t.Errorf("T(a) = %q, want %q", got, "un")
	}

	if got := p.T("b", 2); got != "two 2" {
		t.Errorf("T(b) = %q, want %q", got, "two 2")
	}

	if got := p.T("c"); got != "!c!" {
		t.Errorf("T(c) = %q, want %q", got, "!c!")
	}
}

// TestFold checks the folding of accents, quotes, spaces and symbols to ASCII.
//
// Parameters:
//   - t: test to report to
func TestFold(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain text %s", "plain text %s"},
		{"\u00E9t\u00E9 \u00E0 l\u2019\u00E9cole", "ete a l'ecole"},
		{"\u00C9cran \u00E7a \u00EE\u00F4\u00F9\u00EA\u00E8", "Ecran ca iouee"},
		{"\u00AB\u00A0oui\u00A0\u00BB : 3\u00A0\u00D7\u00A04", "\" oui \" : 3 x 4"},
		{"wait\u2026", "wait..."},
		{"\u0153uvre \u00E6", "oeuvre ae"},
		{"\u4E2D\u6587", "??"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := Fold(tt.in); got != tt.want {
			t.Errorf("Fold(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestFoldCatalogs checks that every message of both catalogs is pure ASCII once folded, that
// folding keeps the Printf directives, and that the ASCII printer says the folded messages in
// the language of the original while the arguments are left alone.
//
// Parameters:
//   - t: test to report to
func TestFoldCatalogs(t *testing.T) {
	for _, lang := range []string{"en", "fr"} {
		p := New(lang)
		a := p.ASCII()

		if a.Lang() != lang {
			t.Errorf("%s: the ASCII printer speaks %q", lang, a.Lang())
		}

		for _, key := range Keys() {
			orig, folded := p.T(key), a.T(key)

			for _, r := range folded {
				if r >= 0x80 {
					t.Fatalf("%s: %s folds to %q, which is not ASCII", lang, key, folded)
				}
			}

			if strings.Count(orig, "%") != strings.Count(folded, "%") {
				t.Errorf("%s: %s lost a directive: %q then %q", lang, key, orig, folded)
			}

			if folded != Fold(orig) {
				t.Errorf("%s: %s = %q, want %q", lang, key, folded, Fold(orig))
			}
		}
	}

	got := New("fr").ASCII().T("ui_no_results", "caf\u00E9")
	if !strings.Contains(got, "caf\u00E9") || !strings.HasPrefix(got, "Aucun resultat pour") {
		t.Errorf("an argument was folded or the message was not: %q", got)
	}
}
