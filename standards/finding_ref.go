package standards

import (
	"strings"

	"github.com/Autumn-27/artex/locale"
)

// Finding-ref schemes are the names used by the finding_refs store
// (db/finding_refs.go). They are repeated here as literals so this package
// stays free of a db dependency; the two lists must stay in sync.
const (
	findingSchemeCWE        = "cwe"
	findingSchemeASVS       = "asvs"
	findingSchemeWSTG       = "wstg"
	findingSchemeOWASPTop10 = "owasp-top10"
	findingSchemeCVSS       = "cvss"
	findingSchemeCVE        = "cve"
)

// ResolveFindingRef checks one classification ref against the embedded
// standards catalog. asvs/wstg/owasp-top10 must resolve as real catalog ids
// (owasp-top10 maps to the catalog's "top10"); cwe, cvss and cve return nil because
// CWE is not embedded, a CVSS vector is not a catalog id, and there is no CVE
// catalog — the db store's shape check covers those. An unknown scheme is an error.
func ResolveFindingRef(scheme, ref string) error {
	switch scheme {
	case findingSchemeCWE, findingSchemeCVSS, findingSchemeCVE:
		return nil
	case findingSchemeASVS, findingSchemeWSTG, findingSchemeOWASPTop10:
	default:
		return locale.Errorf("unknown %s ref: %s", scheme, ref)
	}
	cat, err := Embedded()
	if err != nil {
		return err
	}
	catalogScheme := scheme
	if scheme == findingSchemeOWASPTop10 {
		catalogScheme = SchemeTop10
	}
	if !cat.Resolve(catalogScheme, strings.TrimSpace(ref)) {
		return locale.Errorf("unknown %s ref: %s", scheme, ref)
	}
	return nil
}
