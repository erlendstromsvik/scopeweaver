package standards

import (
	_ "embed"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/Autumn-27/artex/locale"
	"gopkg.in/yaml.v3"
)

//go:embed profiles.yaml
var embeddedProfilesYAML []byte

// Profile is a named slice of the standards catalog: the ASVS requirements up
// to a level, plus the WSTG tests, optionally minus some WSTG categories.
// A blueprint names profiles under `standards:`; provisioning seeds every
// requirement they select as a not_tested control result (see task E.4).
type Profile struct {
	Name                string
	ASVSMaxLevel        int
	WSTG                bool
	WSTGExcludePrefixes []string
}

// notAutomatableRule flags requirements automation cannot settle. The rule is a
// heuristic meant to be refined: it matches wording that points at a
// documentation review, and only attaches a reason to the seeded row — it
// never removes a requirement from scope.
type notAutomatableRule struct {
	Match  string `yaml:"match"`
	Reason string `yaml:"reason"`
	re     *regexp.Regexp
}

type profilesFile struct {
	Profiles map[string]struct {
		ASVSMaxLevel        int      `yaml:"asvs_max_level"`
		WSTG                bool     `yaml:"wstg"`
		WSTGExcludePrefixes []string `yaml:"wstg_exclude_prefixes"`
	} `yaml:"profiles"`
	NotAutomatable []notAutomatableRule `yaml:"not_automatable"`
}

var (
	profilesOnce sync.Once
	profilesMap  map[string]Profile
	notAutoRules []notAutomatableRule
	profilesErr  error
)

// Profiles returns the test profiles compiled into the binary from
// standards/profiles.yaml via //go:embed. The file is parsed once; the
// returned map must be treated as read-only.
func Profiles() (map[string]Profile, error) {
	profilesOnce.Do(func() {
		profilesMap, notAutoRules, profilesErr = parseProfiles(embeddedProfilesYAML)
	})
	return profilesMap, profilesErr
}

// parseProfiles unmarshals the profiles YAML and compiles the not_automatable
// regexes. A rule with an invalid pattern is an error so a typo can't silently
// stop flagging requirements.
func parseProfiles(b []byte) (map[string]Profile, []notAutomatableRule, error) {
	var f profilesFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, nil, locale.Errorf("parse standards profiles: %w", err)
	}
	out := make(map[string]Profile, len(f.Profiles))
	for name, p := range f.Profiles {
		out[name] = Profile{
			Name:                name,
			ASVSMaxLevel:        p.ASVSMaxLevel,
			WSTG:                p.WSTG,
			WSTGExcludePrefixes: p.WSTGExcludePrefixes,
		}
	}
	rules := make([]notAutomatableRule, 0, len(f.NotAutomatable))
	for _, r := range f.NotAutomatable {
		re, err := regexp.Compile(r.Match)
		if err != nil {
			return nil, nil, locale.Errorf("compile not_automatable pattern %q: %w", r.Match, err)
		}
		r.re = re
		rules = append(rules, r)
	}
	return out, rules, nil
}

// ProfileNames returns the available profile names, sorted, for validation
// messages and UI hints.
func ProfileNames() []string {
	profiles, err := Profiles()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ProfilesMaxASVSLevel returns the highest ASVS level among the named
// profiles, or 0 when no profile is named (or any name is unknown). The
// coverage report uses it to pick its ASVS scope from the blueprint.
func ProfilesMaxASVSLevel(names []string) int {
	profiles, err := Profiles()
	if err != nil {
		return 0
	}
	max := 0
	for _, n := range names {
		p, ok := profiles[n]
		if !ok {
			return 0
		}
		if p.ASVSMaxLevel > max {
			max = p.ASVSMaxLevel
		}
	}
	return max
}

// ProfilesIncludeWSTG reports whether any named profile includes the WSTG
// tests. Unknown names contribute nothing (mirrors ProfilesMaxASVSLevel's
// lenient read: it is a display-scope helper, not a validator).
func ProfilesIncludeWSTG(names []string) bool {
	profiles, err := Profiles()
	if err != nil {
		return false
	}
	for _, n := range names {
		if p, ok := profiles[n]; ok && p.WSTG {
			return true
		}
	}
	return false
}

// ScopedRequirement is one catalog requirement selected by a profile set, with
// the reason it cannot be settled by automation when one applies. Standard is
// a scheme key (SchemeASVS / SchemeWSTG).
type ScopedRequirement struct {
	Standard             string
	ID                   string
	NotAutomatableReason string
}

// SelectRequirements returns the union of the named profiles' requirements:
// ASVS requirements with Level <= the profile's max level, and WSTG tests
// unless a profile excludes their prefix. The result is de-duplicated and
// sorted by standard, then natural requirement-id order. NotAutomatableReason
// rule; WSTG tests never carry one. An unknown profile name is an error.
func (c *Catalog) SelectRequirements(names []string) ([]ScopedRequirement, error) {
	profiles, err := Profiles()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, locale.Errorf("standards catalog is nil")
	}
	seen := map[string]bool{}
	var out []ScopedRequirement
	for _, name := range names {
		p, ok := profiles[name]
		if !ok {
			return nil, locale.Errorf("unknown standards profile %q; allowed: %s",
				name, strings.Join(ProfileNames(), ", "))
		}
		for _, req := range c.ASVS {
			if int(req.Level) > p.ASVSMaxLevel {
				continue
			}
			key := SchemeASVS + "\x00" + req.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ScopedRequirement{
				Standard: SchemeASVS, ID: req.ID,
				NotAutomatableReason: notAutomatableReason(notAutoRules, req.Description),
			})
		}
		if !p.WSTG {
			continue
		}
		for _, test := range c.WSTG {
			if hasAnyPrefix(test.ID, p.WSTGExcludePrefixes) {
				continue
			}
			key := SchemeWSTG + "\x00" + test.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ScopedRequirement{Standard: SchemeWSTG, ID: test.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Standard != out[j].Standard {
			return out[i].Standard < out[j].Standard
		}
		return naturalLess(out[i].ID, out[j].ID)
	})
	return out, nil
}

// notAutomatableReason returns the reason of the first rule matching the
// requirement description, or "" when none matches.
func notAutomatableReason(rules []notAutomatableRule, description string) string {
	for _, r := range rules {
		if r.re != nil && r.re.MatchString(description) {
			return r.Reason
		}
	}
	return ""
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// naturalLess compares requirement ids so digit runs order numerically:
// V1.2.10 sorts after V1.2.9, and WSTG-INFO-2 after WSTG-INFO-1. Copied
// from report/coverage.go so standards can sort selections without importing
// the report package.
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		da, db_ := isDigit(a[i]), isDigit(b[j])
		if da && db_ {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			// Equal numeric value: the run with fewer digits sorts first ("V7" < "V007").
			if (i - si) != (j - sj) {
				return (i - si) < (j - sj)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if a[i] != b[j] {
			return a[i] < b[j]
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
