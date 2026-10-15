// Package standards provides typed, read-only catalogs for the security
// standards ScopeWeaver reports against: OWASP ASVS 5.0 (requirements and
// levels), the OWASP Web Security Testing Guide (WSTG), the OWASP Top 10:2025
// reporting categories, and CWE weakness id->name lookups.
//
// It is pure data: it loads the vendored sources under standards/sources (no
// network, no database), validates them, and resolves references per scheme.
// A crosswalk mechanism links references across schemes, but no cross-scheme
// links are authored yet (see standards/crosswalk.yaml and roadmap task E-D5).
package standards

import (
	"strconv"
	"strings"
)

// Scheme identifiers used by Ref and Resolve. They are lowercase and stable;
// callers compare case-insensitively via Resolve.
const (
	SchemeASVS  = "asvs"
	SchemeWSTG  = "wstg"
	SchemeTop10 = "top10"
	SchemeCWE   = "cwe"
)

// Level is an ASVS verification level: 1, 2 or 3. A value outside that range is
// rejected by Validate.
type Level int

// ASVSRequirement is a single ASVS 5.0 requirement. ASVS 5.0 dropped the
// per-requirement CWE mapping, so there is deliberately no CWE field here.
type ASVSRequirement struct {
	ID          string // req_id, e.g. "V1.1.1"
	Chapter     string // chapter_name
	Section     string // section_name
	Description string // req_description
	Level       Level
}

// WSTGTest is a single Web Security Testing Guide test.
type WSTGTest struct {
	ID         string // e.g. "WSTG-INFO-01"
	Category   string // human-readable category name
	Name       string
	Reference  string // documentation URL
	Objectives []string
}

// Top10Category is a single OWASP Top 10:2025 reporting category.
type Top10Category struct {
	ID    string // e.g. "A01:2025"
	Title string
}

// Weakness is a CWE id->name entry (CWE-<ID>).
type Weakness struct {
	ID   int
	Name string
}

// Ref is a reference into one scheme's catalog.
type Ref struct {
	Scheme string `yaml:"scheme"`
	ID     string `yaml:"id"`
}

// CrosswalkLink connects two references across (or within) schemes.
type CrosswalkLink struct {
	From Ref `yaml:"from"`
	To   Ref `yaml:"to"`
}

// Catalog holds the parsed standards data and any crosswalk links. Maps are
// keyed by the scheme's native identifier.
type Catalog struct {
	ASVS      map[string]ASVSRequirement // key: req_id "V1.1.1"
	WSTG      map[string]WSTGTest        // key: "WSTG-INFO-01"
	Top10     map[string]Top10Category   // key: "A01:2025"
	CWE       map[int]Weakness           // key: numeric id
	Crosswalk []CrosswalkLink

	// dups records identifiers that appeared more than once in a source while
	// loading. Maps cannot represent duplicates, so Validate reports them from
	// here rather than re-deriving them.
	dups []Ref
}

// Resolve reports whether a reference exists in its scheme's catalog. The
// scheme is matched case-insensitively. For CWE, id may be a bare number
// ("79") or the "CWE-79" form.
func (c *Catalog) Resolve(scheme, id string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case SchemeASVS:
		_, ok := c.ASVS[id]
		return ok
	case SchemeWSTG:
		_, ok := c.WSTG[id]
		return ok
	case SchemeTop10:
		_, ok := c.Top10[id]
		return ok
	case SchemeCWE:
		n, err := parseCWEID(id)
		if err != nil {
			return false
		}
		_, ok := c.CWE[n]
		return ok
	}
	return false
}

// parseCWEID accepts "79" or "CWE-79" (any case) and returns the numeric id.
func parseCWEID(id string) (int, error) {
	s := strings.TrimSpace(id)
	if len(s) >= 4 && strings.EqualFold(s[:4], "CWE-") {
		s = s[4:]
	}
	return strconv.Atoi(strings.TrimSpace(s))
}
