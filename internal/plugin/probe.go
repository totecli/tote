package plugin

import (
	"regexp"
	"slices"
)

// featureRe matches the name of a host feature.
var featureRe = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)

// knownOS and knownArch list the operating systems and architectures a plugin can target.
var (
	knownOS   = []string{"linux", "darwin"}
	knownArch = []string{"amd64", "arm64"}
)

// probe is the lenient first pass of Decode over api, tote, tier, requires_features, platforms and
// [[platform]]. It reads the tree without marking anything as used and records only what decides
// between invalid and incompatible.
//
// Parameters:
//   - c: collector that receives the issues
//   - raw: decoded manifest
//   - host: running tote
//
// Returns true when the probe recorded an issue, in which case the strict step must not run.
func probe(c *collector, raw map[string]any, host Host) bool {
	before := len(c.issues)

	probeAPI(c, raw)
	probeTote(c, raw, host)
	probeTier(c, raw)
	probeFeatures(c, raw, host)
	probePlatforms(c, raw, host)

	return len(c.issues) > before
}

// probeAPI checks the api key.
//
// Parameters:
//   - c: collector that receives the issues
//   - raw: decoded manifest
func probeAPI(c *collector, raw map[string]any) {
	v, ok := raw["api"]
	if !ok {
		c.add(CodeKeyMissing, "api")

		return
	}

	api, isInt := v.(int64)
	if !isInt {
		c.add(CodeTypeInvalid, "api", "integer", typeName(v))

		return
	}

	switch {
	case api > 1:
		c.incompatible(CodeAPIUnsupported, "api", itoa64(api), "upgrade_tote")
	case api < 1:
		c.incompatible(CodeAPIUnsupported, "api", itoa64(api), "upgrade_plugin")
	}
}

// probeTote checks the tote range.
//
// Parameters:
//   - c: collector that receives the issues
//   - raw: decoded manifest
//   - host: running tote
func probeTote(c *collector, raw map[string]any, host Host) {
	v, ok := raw["tote"]
	if !ok {
		c.add(CodeKeyMissing, "tote")

		return
	}

	s, isStr := v.(string)
	if !isStr {
		c.add(CodeTypeInvalid, "tote", "string", typeName(v))

		return
	}

	r, err := ParseRange(s)
	if err != nil {
		c.add(CodeRangeInvalid, "tote", s)

		return
	}

	if host.Version == "" {
		return
	}

	allowed, err := r.Allows(host.Version)
	if err == nil && !allowed {
		c.incompatible(CodeToteRange, "tote", r.String(), host.Version)
	}
}

// probeTier checks the tier key. A manifest of tier exec is incompatible while the exec tier is
// not available (execTierAvailable): it is the one place where tote 1.0 refuses that tier, for
// `plugin check`, for every install and update, and for a plugin that is already in the store,
// which is then listed with the reason and never run. The argument is the key as the author wrote
// it, which needs no translation.
//
// Parameters:
//   - c: collector that receives the issues
//   - raw: decoded manifest
func probeTier(c *collector, raw map[string]any) {
	v, ok := raw["tier"]
	if !ok {
		c.add(CodeKeyMissing, "tier")

		return
	}

	s, isStr := v.(string)

	switch {
	case !isStr:
		c.add(CodeTypeInvalid, "tier", "string", typeName(v))
	case s != TierDeclarative && s != TierExec:
		c.add(CodeValueInvalid, "tier", s)
	case s == TierExec && !execTierAvailable:
		c.incompatible(CodeReserved, "tier", "tier = \""+TierExec+"\"")
	}
}

// probeFeatures checks requires_features against what the host announces.
//
// Parameters:
//   - c: collector that receives the issues
//   - raw: decoded manifest
//   - host: running tote
func probeFeatures(c *collector, raw map[string]any, host Host) {
	v, ok := raw["requires_features"]
	if !ok {
		return
	}

	list, isList := v.([]any)
	if !isList {
		c.add(CodeTypeInvalid, "requires_features", "array", typeName(v))

		return
	}

	if len(list) > MaxFeatures {
		c.add(CodeLimitExceeded, "requires_features", "count", itoa(MaxFeatures))

		return
	}

	for i, e := range list {
		path := "requires_features[" + itoa(i) + "]"

		s, isStr := e.(string)
		if !isStr || !featureRe.MatchString(s) {
			c.add(CodeValueInvalid, path)

			continue
		}

		if !slices.Contains(host.Features, s) {
			c.incompatible(CodeFeatureMissing, path, s)
		}
	}
}

// probePlatforms checks platforms and [[platform]] against the host.
//
// Parameters:
//   - c: collector that receives the issues
//   - raw: decoded manifest
//   - host: running tote
func probePlatforms(c *collector, raw map[string]any, host Host) {
	if v, ok := raw["platforms"]; ok {
		probeOSList(c, v, host)
	}

	v, ok := raw["platform"]
	if !ok {
		return
	}

	var builds []map[string]any

	switch arr := v.(type) {
	case []map[string]any:
		builds = arr
	case []any:
		for _, e := range arr {
			m, isMap := e.(map[string]any)
			if !isMap {
				c.add(CodeTypeInvalid, "platform", "array of tables", typeName(e))

				return
			}

			builds = append(builds, m)
		}
	default:
		c.add(CodeTypeInvalid, "platform", "array of tables", typeName(v))

		return
	}

	probeBuilds(c, builds, host)
}

// probeOSList checks the platforms key.
//
// Parameters:
//   - c: collector that receives the issues
//   - v: decoded value of platforms
//   - host: running tote
func probeOSList(c *collector, v any, host Host) {
	list, ok := v.([]any)
	if !ok {
		c.add(CodeTypeInvalid, "platforms", "array", typeName(v))

		return
	}

	if len(list) == 0 || len(list) > len(knownOS) {
		c.add(CodeValueInvalid, "platforms")

		return
	}

	found := host.OS == ""

	for i, e := range list {
		s, isStr := e.(string)
		if !isStr || !slices.Contains(knownOS, s) {
			c.add(CodeValueInvalid, "platforms["+itoa(i)+"]")

			return
		}

		found = found || s == host.OS
	}

	if !found {
		c.incompatible(CodePlatformUnsupported, "platforms", host.OS)
	}
}

// probeBuilds checks that a plugin with [[platform]] tables has a build for the host.
//
// Parameters:
//   - c: collector that receives the issues
//   - builds: decoded [[platform]] tables
//   - host: running tote
func probeBuilds(c *collector, builds []map[string]any, host Host) {
	if host.OS == "" || len(builds) == 0 {
		return
	}

	for _, b := range builds {
		if b["os"] == host.OS && b["arch"] == host.Arch {
			return
		}
	}

	c.incompatible(CodePlatformNone, "platform", host.OS, host.Arch)
}
