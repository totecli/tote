package i18n

import "fmt"

// langFrench is the code of the French catalogue.
const langFrench = "fr"

// langEnglish is the code of the English catalogue, the default language.
const langEnglish = "en"

// Printer translates message keys into one language.
//
// It reads the catalogue of its language first, then the English catalogue, so a key that is
// still missing in French shows English text instead of nothing. A Printer is read-only after
// New and is safe for concurrent use.
type Printer struct {
	lang string
	own  map[string]string
	base map[string]string
}

// New returns a Printer for a language.
//
// Parameters:
//   - lang: "fr" for French; "en" and any other value, including the empty string, give English
//
// Returns a Printer that is never nil.
func New(lang string) *Printer {
	base := load(enData)

	if lang == langFrench {
		return &Printer{lang: langFrench, own: load(frData), base: base}
	}

	return &Printer{lang: langEnglish, own: base, base: base}
}

// Lang returns the language of the Printer, either "en" or "fr".
func (p *Printer) Lang() string {
	return p.lang
}

// T returns the message of a key in the language of the Printer.
//
// Parameters:
//   - key: catalogue key such as "cli_unknown_command"
//   - args: values for the Printf verbs of the message; with none, the message is returned as is
//     and a literal percent sign must not be doubled
//
// Returns the French message, then the English one when the key is missing in French, then the
// key wrapped as "!key!" so that a missing key is visible on screen.
func (p *Printer) T(key string, args ...any) string {
	msg, ok := p.own[key]
	if !ok {
		msg, ok = p.base[key]
	}

	if !ok {
		return "!" + key + "!"
	}

	if len(args) == 0 {
		return msg
	}

	return fmt.Sprintf(msg, args...)
}
