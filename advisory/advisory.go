// Package advisory normalizes security-advisory feeds (CISA KEV) into
// Records and matches them against the asset inventory. Matches are
// candidates for a human/agent check, never verdicts.
package advisory

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/Autumn-27/artex/locale"
)

// SourceCISAKEV is the Source value for records from the CISA KEV catalog.
const SourceCISAKEV = "cisa-kev"

// Record is a normalized advisory entry, independent of the feed format.
type Record struct {
	Source     string    // "cisa-kev"
	ID         string    // the CVE id for KEV
	Title      string    // vulnerabilityName
	Date       time.Time // dateAdded (UTC midnight)
	CVEs       []string
	Vendor     string
	Product    string
	Summary    string // shortDescription
	Link       string // "https://nvd.nist.gov/vuln/detail/<CVE>"
	Ransomware bool   // knownRansomwareCampaignUse == "Known"
	CWEs       []string
}

// kevCatalog mirrors the published KEV JSON envelope.
type kevCatalog struct {
	Title           string     `json:"title"`
	CatalogVersion  string     `json:"catalogVersion"`
	DateReleased    string     `json:"dateReleased"`
	Count           int        `json:"count"`
	Vulnerabilities []kevEntry `json:"vulnerabilities"`
}

// kevEntry mirrors one KEV vulnerability entry.
type kevEntry struct {
	CveID                      string   `json:"cveID"`
	VendorProject              string   `json:"vendorProject"`
	Product                    string   `json:"product"`
	VulnerabilityName          string   `json:"vulnerabilityName"`
	DateAdded                  string   `json:"dateAdded"`
	ShortDescription           string   `json:"shortDescription"`
	RequiredAction             string   `json:"requiredAction"`
	DueDate                    string   `json:"dueDate"`
	KnownRansomwareCampaignUse string   `json:"knownRansomwareCampaignUse"`
	Notes                      string   `json:"notes"`
	Cwes                       []string `json:"cwes"`
}

// ParseKEV parses the CISA KEV catalog JSON. It rejects invalid JSON, a
// count field that disagrees with the entry list, and any entry without a
// cveID. Records are sorted by Date desc, then ID.
func ParseKEV(b []byte) (catalogVersion string, recs []Record, err error) {
	var cat kevCatalog
	if err := json.Unmarshal(b, &cat); err != nil {
		return "", nil, locale.Errorf("parse CISA KEV catalog: %v", err)
	}
	if len(cat.Vulnerabilities) != cat.Count {
		return "", nil, locale.Errorf("CISA KEV count field %d does not match %d entries", cat.Count, len(cat.Vulnerabilities))
	}
	recs = make([]Record, 0, len(cat.Vulnerabilities))
	for _, v := range cat.Vulnerabilities {
		if v.CveID == "" {
			return "", nil, locale.Errorf("CISA KEV entry missing cveID (product %q)", v.Product)
		}
		date, derr := time.Parse("2006-01-02", v.DateAdded)
		if derr != nil {
			return "", nil, locale.Errorf("parse CISA KEV dateAdded %q for %s: %v", v.DateAdded, v.CveID, derr)
		}
		date = date.UTC()
		recs = append(recs, Record{
			Source:     SourceCISAKEV,
			ID:         v.CveID,
			Title:      v.VulnerabilityName,
			Date:       date,
			CVEs:       []string{v.CveID},
			Vendor:     v.VendorProject,
			Product:    v.Product,
			Summary:    v.ShortDescription,
			Link:       "https://nvd.nist.gov/vuln/detail/" + v.CveID,
			Ransomware: v.KnownRansomwareCampaignUse == "Known",
			CWEs:       v.Cwes,
		})
	}
	sort.SliceStable(recs, func(i, j int) bool {
		if !recs[i].Date.Equal(recs[j].Date) {
			return recs[i].Date.After(recs[j].Date)
		}
		return recs[i].ID < recs[j].ID
	})
	return cat.CatalogVersion, recs, nil
}
