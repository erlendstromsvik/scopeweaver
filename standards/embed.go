package standards

import (
	_ "embed"
	"sync"

	"github.com/Autumn-27/artex/locale"
)

//go:embed sources/asvs/OWASP_ASVS_5.0.0_en.flat.json
var embeddedASVS []byte

//go:embed sources/wstg/checklist.json
var embeddedWSTG []byte

//go:embed sources/owasp-top10/top10-2025-categories.yaml
var embeddedTop10 []byte

var (
	embeddedOnce sync.Once
	embeddedCat  *Catalog
	embeddedErr  error
)

// Embedded returns a Catalog assembled from the ASVS, WSTG and Top 10
// sources compiled into the binary via //go:embed, for runtime lookups in
// deployments that ship only the binary (the Docker runtime image has no
// sources directory). CWE is not embedded (its source is a 2 MB archive),
// so the returned catalog's CWE map is empty and Resolve("cwe", …) is
// always false. The sources are parsed once; the result is shared across
// callers and must be treated as read-only. The Crosswalk is left empty,
// as with Load.
func Embedded() (*Catalog, error) {
	embeddedOnce.Do(func() {
		asvs, asvsDups, err := parseASVS(embeddedASVS)
		if err != nil {
			embeddedErr = locale.Errorf("parse ASVS source: %w", err)
			return
		}
		wstg, wstgDups, err := parseWSTG(embeddedWSTG)
		if err != nil {
			embeddedErr = locale.Errorf("parse WSTG source: %w", err)
			return
		}
		top10, top10Dups, err := parseTop10(embeddedTop10)
		if err != nil {
			embeddedErr = locale.Errorf("parse Top 10 source: %w", err)
			return
		}
		cat := &Catalog{
			ASVS:  asvs,
			WSTG:  wstg,
			Top10: top10,
			CWE:   map[int]Weakness{},
		}
		cat.dups = append(cat.dups, asvsDups...)
		cat.dups = append(cat.dups, wstgDups...)
		cat.dups = append(cat.dups, top10Dups...)
		embeddedCat = cat
	})
	return embeddedCat, embeddedErr
}
