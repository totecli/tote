package plugin

import (
	"regexp"
	"slices"
)

var (
	// hexRe matches a sha256 digest in lower case hexadecimal.
	hexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

	// assetRe matches the name of a release asset.
	assetRe = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,255}$`)
)

// defaultPlatforms is the value of platforms when the key is absent.
var defaultPlatforms = []string{"linux", "darwin"}

// extractor reads a decoded manifest into a Manifest. It checks types, text, sizes and the values
// of enumerations, and leaves the grammar of identifiers and every cross-key rule to validate.
type extractor struct {
	c    *collector
	m    *Manifest
	tier string
}

// extract reads the strict part of a manifest.
//
// Parameters:
//   - c: collector that receives the issues
//   - raw: decoded manifest that passed the probe
//   - opts: how the manifest is read
//
// Returns the manifest, complete even when issues were recorded, so that validate can report
// everything in one pass.
func extract(c *collector, raw map[string]any, _ DecodeOptions) *Manifest {
	n := newNode(c, "", "", raw)
	x := &extractor{c: c, m: &Manifest{}}

	x.top(n)
	x.requires(n)
	x.execTable(n)
	x.builds(n)

	x.m.Commands = x.entries(n, "command", commandKeys)
	x.m.ItemActions = x.entries(n, "item_action", itemActionKeys)
	x.m.Actions = x.entries(n, "action", actionKeys)
	x.m.Lists = x.lists(n)
	x.m.Settings = x.settings(n)

	x.i18n(n)

	n.reserve(reservedTables...)
	n.finish()

	return x.m
}

// top reads the top-level scalar keys.
//
// Parameters:
//   - n: root of the manifest
func (x *extractor) top(n *node) {
	m := x.m

	if v, ok := n.integer("api", 0, 1<<31-1); ok {
		m.API = int(v)
	}

	var ok bool

	if m.ID, ok = n.str("id", strOpt{required: true}); ok && m.ID == "" {
		x.c.add(CodeIDInvalid, "id", "", "grammar")
	}

	if m.Version, ok = n.str("version", strOpt{required: true}); ok && m.Version == "" {
		x.c.add(CodeVersionInvalid, "version", "")
	}

	m.Tote, _ = n.str("tote", strOpt{required: true})
	m.Tier, _ = n.str("tier", strOpt{required: true})
	m.Title, _ = n.str("title", strOpt{visible: true, runes: MaxTitleRunes})
	m.Description, _ = n.str("description",
		strOpt{required: true, visible: true, runes: MaxDescRunes})
	m.License, _ = n.str("license", strOpt{required: true, nonEmpty: true, bytes: MaxLicenseBytes})
	m.Homepage, _ = n.str("homepage", strOpt{nonEmpty: true, bytes: MaxHomepageBytes})
	m.RequiresFeatures, _ = n.strs("requires_features",
		listOpt{limit: MaxFeatures, item: strOpt{nonEmpty: true}})

	x.tier = m.Tier

	platforms := listOpt{limit: len(knownOS), item: strOpt{nonEmpty: true}}

	if list, ok := n.strs("platforms", platforms); ok {
		m.Platforms = list
	} else if !n.has("platforms") {
		m.Platforms = slices.Clone(defaultPlatforms)
	}

	if list, ok := n.strs("permissions",
		listOpt{limit: maxPermissions, item: strOpt{nonEmpty: true, bytes: maxPermLen}}); ok {
		m.Permissions = list
	}
}

// requires reads the [[requires]] tables.
//
// Parameters:
//   - n: root of the manifest
func (x *extractor) requires(n *node) {
	tables := n.tables("requires")
	if len(tables) > MaxRequires {
		x.c.add(CodeLimitExceeded, "requires", "count", itoa(MaxRequires))
	}

	for _, t := range tables {
		var r Requires

		r.Bin, _ = t.str("bin", strOpt{required: true, nonEmpty: true})
		r.Hint, _ = t.str("hint", strOpt{runes: MaxDescRunes})
		r.Optional, _ = t.boolean("optional")

		t.finish()

		x.m.Requires = append(x.m.Requires, r)
	}
}

// execTable reads the [exec] table.
//
// Parameters:
//   - n: root of the manifest
func (x *extractor) execTable(n *node) {
	t, ok := n.table("exec")
	if !ok {
		return
	}

	e := &Exec{}

	e.Bin, _ = t.str("bin", strOpt{nonEmpty: true, bytes: MaxBinBytes})
	e.Args, _ = t.strs("args", listOpt{limit: MaxExecArgs, item: strOpt{bytes: MaxArgvElemBytes}})
	e.Modes, _ = t.strs("modes", listOpt{limit: 2, item: strOpt{nonEmpty: true}})
	e.Session, _ = t.str("session", strOpt{nonEmpty: true})

	for i, mode := range e.Modes {
		switch {
		case slices.Contains([]string{"list", "run"}, mode):
		case slices.Contains([]string{"health", "search"}, mode):
			x.c.add(CodeReserved, t.at("modes")+"["+itoa(i)+"]", mode)
		default:
			x.c.add(CodeValueInvalid, t.at("modes")+"["+itoa(i)+"]", mode)
		}
	}

	switch e.Session {
	case "", "once":
	case "long":
		x.c.add(CodeReserved, t.at("session"), e.Session)
	default:
		x.c.add(CodeValueInvalid, t.at("session"), e.Session)
	}

	t.finish()

	x.m.Exec = e
}

// builds reads the [[platform]] tables.
//
// Parameters:
//   - n: root of the manifest
func (x *extractor) builds(n *node) {
	for _, t := range n.tables("platform") {
		var b Build

		b.OS, _ = x.enumOf(t, "os", knownOS)
		b.Arch, _ = x.enumOf(t, "arch", knownArch)
		b.Bin, _ = t.str("bin", strOpt{required: true, nonEmpty: true, bytes: MaxBinBytes})
		b.Asset, _ = t.str("asset", strOpt{required: true, nonEmpty: true})

		if s, ok := t.str("sha256", strOpt{required: true}); ok {
			b.SHA256 = s

			if !hexRe.MatchString(s) {
				x.c.add(CodeValueInvalid, t.at("sha256"), "hex64")
			}
		}

		if b.Asset != "" && (!assetRe.MatchString(b.Asset) || !validSegment(b.Asset)) {
			x.c.add(CodeValueInvalid, t.at("asset"), b.Asset)
		}

		t.finish()

		x.m.Builds = append(x.m.Builds, b)
	}
}

// enumOf reads a required string that must be one of a list.
//
// Parameters:
//   - n: table that holds the key
//   - key: key name
//   - allowed: accepted values
//
// Returns the value and true when it is accepted; value_invalid is recorded otherwise.
func (x *extractor) enumOf(n *node, key string, allowed []string) (string, bool) {
	s, ok := n.str(key, strOpt{required: true, nonEmpty: true})
	if !ok {
		return "", false
	}

	if !slices.Contains(allowed, s) {
		x.c.add(CodeValueInvalid, n.at(key), s)

		return "", false
	}

	return s, true
}

// i18n reads the [i18n.<lang>] tables, flattening them to dotted keys.
//
// Parameters:
//   - n: root of the manifest
func (x *extractor) i18n(n *node) {
	t, ok := n.table("i18n")
	if !ok {
		return
	}

	x.m.I18n = map[string]map[string]string{}

	for _, lang := range sortedKeys(t.m) {
		sub, ok := t.table(lang)
		if !ok {
			continue
		}

		if !slices.Contains(localeLangs, lang) {
			x.c.add(CodeUnknownKey, sub.path, lang)

			continue
		}

		out := map[string]string{}
		flattenLocale(x.c, sub.path, sub.m, "", 0, out)
		x.m.I18n[lang] = out
	}
}
