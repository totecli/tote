package plugin

import (
	"errors"
	"runtime"
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/totecli/tote/internal/store"
)

// Limits of a manifest, from section 3.1 of the specification. Sizes are in bytes, the others in
// the unit of their name. The limits the specification does not number (features, platforms,
// list actions, exec arguments, confirm text, templates) are chosen here and listed in the
// report of the step.
const (
	MaxManifestSize  = 256 << 10
	MaxLocaleSize    = 128 << 10
	MaxContributions = 64
	MaxFields        = 16
	MaxRequires      = 16
	MaxArgvElems     = 64
	MaxArgvElemBytes = 1024
	MaxLabelRunes    = 60
	MaxTitleRunes    = 40
	MaxDescRunes     = 200
	MaxKeywords      = 8
	MaxKeywordRunes  = 24
	MaxChoices       = 64
	MaxChoiceRunes   = 64
	MaxHomepageBytes = 256
	MaxLicenseBytes  = 128
	MaxPatternBytes  = 256
	MaxFlagBytes     = 64
	MaxFeatures      = 32
	MaxListActions   = 16
	MaxExecArgs      = 16
	MaxColumns       = 8
	MaxBinBytes      = 255
	MaxBinDepth      = 6
	MaxItemDataKey   = 32
)

// Structure limits, checked on the raw text before the TOML library decodes it, because the
// library recurses on nested values and allocates a map per part of a dotted key: a table header
// of 10000 parts costs 1.2 GB and the same text 65000 parts long once took the machine down. The
// numbers leave room for the largest manifest the other limits allow. MaxLineBytes is 64 argv
// elements of 1024 bytes with their quotes and commas; MaxNesting covers an array of strings, an
// inline table and one level more; MaxKeyParts covers [[list.action.field]] and the longest
// dotted key of a locale table; MaxHeaders and MaxKeys exceed what 64 contributions of 16 fields
// can write by hand.
const (
	MaxLineBytes = 70 << 10
	MaxNesting   = 4
	MaxKeyParts  = 8
	MaxHeaders   = 4096
	MaxKeys      = 16384
)

// manifestShape and localeShape are the structure limits of plugin.toml and of a locale file.
var (
	manifestShape = store.ShapeLimits{
		MaxBytes: MaxManifestSize, MaxLineBytes: MaxLineBytes,
		MaxNesting: MaxNesting, MaxKeyParts: MaxKeyParts, MaxHeaders: MaxHeaders, MaxKeys: MaxKeys,
	}
	localeShape = store.ShapeLimits{
		MaxBytes: MaxLocaleSize, MaxLineBytes: 8 << 10,
		MaxNesting: MaxNesting, MaxKeyParts: MaxKeyParts, MaxHeaders: MaxHeaders, MaxKeys: MaxKeys,
	}
)

// Tiers of a plugin.
const (
	TierDeclarative = "declarative"
	TierExec        = "exec"
)

// Host describes the running tote for the checks that make a plugin incompatible.
//
// Version is the version built into the binary, "dev" for a development build, which satisfies
// every range; an empty Version skips the range check. OS and Arch are runtime.GOOS and
// runtime.GOARCH; an empty OS skips the platform checks. Features is the set tote announces in
// host.features. Self lists the base names the tote binary answers to besides "tote" (the base
// name of TOTE_BIN), which no exec: permission may name.
type Host struct {
	Version  string
	OS       string
	Arch     string
	Features []string
	Self     []string
}

// NewHost describes the running tote.
//
// Parameters:
//   - version: version built into the binary, "dev" for a development build
//   - self: base names of the tote binary, such as the base name of TOTE_BIN; may be empty
func NewHost(version string, self ...string) Host {
	return Host{
		Version:  version,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Features: slices.Clone(AnnouncedFeatures),
		Self:     self,
	}
}

// DecodeOptions controls how a manifest is read.
//
// WantID is the id that the manifest must carry (the requested id, the directory name), empty
// to accept any. Official is true when the caller has established that the plugin may use an
// official id: the copy shipped with tote. Origin is the lower case owner/repo of the release the
// package comes from, empty for any other source: an official id is then also allowed when the
// origin is totecli/<id> for the id that the manifest carries. Installed is true
// when the manifest is that of a plugin that is already installed and is read again to load it:
// the reservations of ids are then not applied, so that a name which a later release reserves
// never breaks a plugin that was installed before. The grammar of the id still applies.
type DecodeOptions struct {
	Host      Host
	WantID    string
	Official  bool
	Origin    string
	Installed bool
}

// Manifest is a decoded and validated plugin.toml.
//
// Permissions are the declared entries, sorted. Platforms is the effective list of operating
// systems, the default when the key is absent. Exec is nil unless an [exec] table is present, and
// Builds holds the [[platform]] tables. I18n holds the [i18n.<lang>] tables of the manifest,
// flattened to dotted keys. A Manifest returned by Decode has no error issue; every list is in
// file order.
type Manifest struct {
	API              int
	ID               string
	Version          string
	Tote             string
	Title            string
	Description      string
	License          string
	Homepage         string
	Tier             string
	RequiresFeatures []string
	Platforms        []string
	Permissions      []string
	Requires         []Requires
	Exec             *Exec
	Builds           []Build
	Commands         []Entry
	ItemActions      []Entry
	Actions          []Entry
	Lists            []List
	Settings         []Setting
	I18n             map[string]map[string]string
}

// Requires is a [[requires]] entry: an external program that the plugin needs.
//
// Hint is shown, never run. Optional marks a program whose absence is a warning.
type Requires struct {
	Bin      string
	Hint     string
	Optional bool
}

// Exec is the [exec] table of the exec tier: the program, its fixed leading arguments, the
// request kinds it answers and the session mode.
type Exec struct {
	Bin     string
	Args    []string
	Modes   []string
	Session string
}

// Build is a [[platform]] entry: a compiled program for one operating system and architecture,
// delivered as a separate release asset whose hash is pinned by the manifest.
type Build struct {
	OS     string
	Arch   string
	Bin    string
	Asset  string
	SHA256 string
}

// Confirm is the confirm key of a contribution: false, true (generic text) or an English text.
//
// Set is true for both true and a text; Text is empty for the generic text.
type Confirm struct {
	Set  bool
	Text string
}

// Entry is one contribution built from the execution keys of section 3.6: a [[command]], an
// [[item_action]], an [[action]] or a [[list.action]].
//
// Table is the name of the table it came from ("command", "item_action", "action" or
// "list.action") and Path its issue path. Run and Open are exclusive; a key that was absent is the
// zero value, so the defaults are applied by the reader of the entry, not stored. Env maps variable
// names to templates. An entry whose reading already recorded an issue is marked broken, which
// keeps the validator from adding follow-up issues about what is missing.
type Entry struct {
	Table       string
	Path        string
	ID          string
	Label       string
	Description string
	Run         []string
	Open        string
	Cwd         string
	Env         map[string]string
	Confirm     Confirm
	Output      string
	Pause       string
	Timeout     string
	AllowDash   []string
	Target      string
	Key         string
	Keywords    []string
	Fields      []Field
	broken      bool
}

// Field is a form field ([[command.field]] and its siblings).
//
// Type is "text", "choice" or "bool". Default is a template for text and choice and the literal
// "true" or "false" for a bool; HasDefault tells a default from an empty one. MaxLen is the limit
// of a text field, Choices the static values of a choice, Flag the literal flag of a bool. A field
// whose reading already recorded an issue is marked broken.
type Field struct {
	Path       string
	ID         string
	Type       string
	Label      string
	Hint       string
	Default    string
	HasDefault bool
	Required   bool
	Pattern    string
	MaxLen     int
	Choices    []string
	Flag       string
	broken     bool
}

// List is a [[list]] of the exec tier: rows produced by the program. Actions are the
// [[list.action]] entries, the first of which runs on Enter.
type List struct {
	Path        string
	ID          string
	Label       string
	Description string
	Keywords    []string
	Timeout     string
	Columns     []Column
	Actions     []Entry
}

// Column is a [[list.column]].
//
// Width is 0 when absent, which shares the room; Align is "left" or "right", empty meaning left.
type Column struct {
	ID    string
	Label string
	Width int
	Align string
}

// Setting is a [[setting]]: a typed value the user can change.
//
// Type is "string", "bool", "int", "enum", "path" or "list". HasDefault tells a declared default
// from the zero value; Default is then a string, a bool, an int64 or a []string according to the
// type. MaxLen applies to a string and to each list item, Min and Max to an int (HasMin and HasMax
// tell a declared bound), MaxItems to a list. A setting whose reading already recorded an issue is
// marked broken.
type Setting struct {
	Path        string
	ID          string
	Type        string
	Label       string
	Description string
	Default     any
	HasDefault  bool
	Pattern     string
	MaxLen      int
	Min         int64
	Max         int64
	HasMin      bool
	HasMax      bool
	Choices     []string
	MustExist   bool
	MaxItems    int
	broken      bool
}

// Decode reads and validates a plugin.toml.
//
// It runs three steps. First the size, the UTF-8 and the TOML syntax. Then a lenient probe of api,
// tote, tier, requires_features, platforms and [[platform]], which decides between invalid and
// incompatible: a manifest that fails the probe is reported and the strict step is not run, so an
// older tote never rejects a newer key as unknown. Finally the strict step: every unknown key is
// an issue (all are listed), reserved tables, keys, types and permissions give
// reserved_not_available, every limit of the specification is enforced, and the semantic rules
// run (identifiers, permissions and their coverage, placeholders and their scope, patterns, lint).
//
// Parameters:
//   - data: content of plugin.toml, any bytes
//   - opts: running tote, expected id and whether an official id is allowed
//
// Returns the manifest, nil when the report holds an error, and the report with the lenient header
// and every issue.
func Decode(data []byte, opts DecodeOptions) (*Manifest, Report) {
	c := &collector{}

	raw, ok := parseDocument(c, data)
	if !ok {
		return nil, Report{Issues: c.issues}
	}

	c.pos = indexPositions(string(data))
	hdr := lenientHeader(raw)

	if probe(c, raw, opts.Host) {
		return nil, Report{Header: hdr, Issues: c.issues}
	}

	m := extract(c, raw, opts)
	validate(c, m, opts)

	sortByLine(c.issues)

	rep := Report{Header: hdr, Issues: c.issues}
	if len(rep.Errors()) > 0 {
		return nil, rep
	}

	return m, rep
}

// parseDocument checks the size and the encoding of a manifest and decodes it to a generic tree.
//
// Parameters:
//   - c: collector that receives the issues
//   - data: content of plugin.toml
//
// Returns the tree and true, or false after recording manifest_too_large or manifest_syntax with
// the position that the TOML library reports.
func parseDocument(c *collector, data []byte) (map[string]any, bool) {
	if len(data) > MaxManifestSize {
		c.add(CodeManifestTooLarge, "", itoa(MaxManifestSize))

		return nil, false
	}

	if !utf8.Valid(data) {
		c.add(CodeManifestSyntax, "", "utf8")

		return nil, false
	}

	if se := store.CheckShape(data, manifestShape); se != nil {
		c.issues = append(c.issues, shapeIssue(se))

		return nil, false
	}

	var raw map[string]any

	if _, err := toml.Decode(string(data), &raw); err != nil {
		var perr toml.ParseError
		if errors.As(err, &perr) {
			line, col := store.SyntaxPosition(data, perr.Position)

			c.issues = append(c.issues, Issue{
				Code: CodeManifestSyntax, Line: line, Column: col,
				Args: []string{CleanLine(perr.Message, issueArgRunes)},
			})
		} else {
			c.add(CodeManifestSyntax, "", "toml")
		}

		return nil, false
	}

	return raw, true
}

// shapeIssue turns a structure limit crossed by a text into a limit_exceeded issue: the name of
// the limit and its value are the arguments, the line is the line where the limit was crossed.
//
// Parameters:
//   - se: limit reported by store.CheckShape
func shapeIssue(se *store.ShapeError) Issue {
	col := 0
	if se.Line > 0 {
		col = 1
	}

	return Issue{Code: CodeLimitExceeded, Line: se.Line, Column: col, Args: []string{
		se.Limit,
		itoa(se.Max),
	}}
}

// lenientHeader reads the identity fields of a decoded manifest without any check, cleaned.
//
// Parameters:
//   - raw: decoded manifest
func lenientHeader(raw map[string]any) Header {
	get := func(key string, limit int) string {
		s, _ := raw[key].(string)

		return CleanLine(s, limit)
	}

	return Header{
		ID:      get("id", 32),
		Version: get("version", maxVersionLen),
		Tier:    get("tier", 16),
		Title:   get("title", MaxTitleRunes),
	}
}

// sortByLine puts issues in document order. The sort is stable, so issues on the same line keep
// the order in which they were found, and the issues without a position come last.
//
// Parameters:
//   - issues: issues to order, sorted in place
func sortByLine(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i].Line, issues[j].Line
		if a == 0 || b == 0 {
			return b == 0 && a != 0
		}

		return a < b
	})
}
