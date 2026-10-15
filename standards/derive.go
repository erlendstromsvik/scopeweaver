package standards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/locale"
	"gopkg.in/yaml.v3"
)

// Source file locations relative to the sources directory, used by
// DeriveCrosswalk. The OpenCRE link files map each standard section to shared
// Common Requirement Enumeration (CRE) nodes; the ASVS bridge maps ASVS 5.0
// requirement ids back to their 4.0.3 predecessors.
const (
	opencreCWELinksPath  = "opencre/cwe.links.json"
	opencreASVSLinksPath = "opencre/asvs-4.0.3.links.json"
	opencreWSTGLinksPath = "opencre/wstg.links.json"
	asvsBridgePath       = "asvs/mappings/mapping_v5.0.0_to_v4.0.3.yml"
)

// Provenance values stamped onto derived links. ASVS 5.0 has no native CWE
// mapping, so its links travel ASVS5 ->(OWASP bridge)-> ASVS4.0.3 ->(OpenCRE)->
// CWE and are the weaker of the two (confidence "low"); WSTG links are a single
// OpenCRE hop (confidence "medium").
const (
	sourceOpenCRE       = "opencre"
	sourceOpenCREBridge = "opencre+asvs4.0.3-bridge"
	confidenceLow       = "low"
	confidenceMedium    = "medium"
)

// asvs403TokenRe extracts every "v4.0.3-<C.S.R>" token from a bridge tag value.
// asvs5KeyRe matches a bridge key "v5.0.0-<C.S.R>" and captures the id part.
var (
	asvs403TokenRe = regexp.MustCompile(`v4\.0\.3-([\d.]+)`)
	asvs5KeyRe     = regexp.MustCompile(`^v5\.0\.0-([\d.]+)$`)
)

// opencreLink is one entry of an OpenCRE *.links.json file: a standard section
// id and the CRE nodes it links to. Other fields in the file are ignored.
type opencreLink struct {
	ID       string   `json:"id"`
	CRELinks []string `json:"cre_links"`
}

// asvsBridgeEntry is the value side of the ASVS 5.0 -> 4.0.3 bridge mapping. The
// tag is free text (e.g. "MODIFIED, MOVED FROM v4.0.3-1.2.4, COVERS
// v4.0.3-1.2.3") from which every v4.0.3 token is extracted.
type asvsBridgeEntry struct {
	Tag string `yaml:"tag-v4.0.3"`
}

// DeriveCrosswalk builds the advisory cross-scheme crosswalk from the vendored
// OpenCRE link files plus the OWASP ASVS 5.0->4.0.3 bridge, keeping only links
// whose From and To both resolve in cat. It emits two families:
//
//   - ASVS 5.0 -> CWE, routed ASVS5 ->(bridge)-> ASVS4.0.3 ->(OpenCRE CRE)->
//     CWE, stamped source "opencre+asvs4.0.3-bridge", confidence "low".
//   - WSTG -> CWE, routed WSTG ->(OpenCRE CRE)-> CWE, stamped source "opencre",
//     confidence "medium".
//
// The result is deduplicated and sorted deterministically. CWE ids are
// normalized to the "CWE-<n>" form. warnings reports how many candidate links
// were dropped because an endpoint did not resolve in the catalog. It is pure:
// it reads only the given sources directory and writes nothing.
func DeriveCrosswalk(sourcesDir string, cat *Catalog) ([]CrosswalkLink, []string, error) {
	cweLinks, err := loadOpenCRELinks(filepath.Join(sourcesDir, opencreCWELinksPath))
	if err != nil {
		return nil, nil, locale.Errorf("load OpenCRE CWE links: %w", err)
	}
	asvsLinks, err := loadOpenCRELinks(filepath.Join(sourcesDir, opencreASVSLinksPath))
	if err != nil {
		return nil, nil, locale.Errorf("load OpenCRE ASVS links: %w", err)
	}
	wstgLinks, err := loadOpenCRELinks(filepath.Join(sourcesDir, opencreWSTGLinksPath))
	if err != nil {
		return nil, nil, locale.Errorf("load OpenCRE WSTG links: %w", err)
	}
	bridge, err := loadASVSBridge(filepath.Join(sourcesDir, asvsBridgePath))
	if err != nil {
		return nil, nil, locale.Errorf("load ASVS 5.0->4.0.3 bridge: %w", err)
	}

	// 1. cre -> set of CWE numeric ids.
	creToCWE := make(map[string]map[int]struct{})
	for _, l := range cweLinks {
		n, err := strconv.Atoi(strings.TrimSpace(l.ID))
		if err != nil {
			continue
		}
		for _, cre := range l.CRELinks {
			set := creToCWE[cre]
			if set == nil {
				set = make(map[int]struct{})
				creToCWE[cre] = set
			}
			set[n] = struct{}{}
		}
	}

	// 2. asvs 4.0.3 id -> set of CWEs (union of its CREs' CWEs).
	asvs403ToCWE := make(map[string]map[int]struct{}, len(asvsLinks))
	for _, l := range asvsLinks {
		set := make(map[int]struct{})
		for _, cre := range l.CRELinks {
			for n := range creToCWE[cre] {
				set[n] = struct{}{}
			}
		}
		asvs403ToCWE[l.ID] = set
	}

	var (
		links       []CrosswalkLink
		seen        = make(map[string]struct{})
		skippedFrom int
		skippedTo   int
	)
	emit := func(from, to Ref, source, confidence string) {
		key := from.Scheme + "|" + from.ID + "|" + to.Scheme + "|" + to.ID
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		if !cat.Resolve(from.Scheme, from.ID) {
			skippedFrom++
			return
		}
		if !cat.Resolve(to.Scheme, to.ID) {
			skippedTo++
			return
		}
		links = append(links, CrosswalkLink{From: from, To: to, Source: source, Confidence: confidence})
	}

	// 3. ASVS 5.0 -> CWE via bridge.
	for key, entry := range bridge {
		m := asvs5KeyRe.FindStringSubmatch(key)
		if m == nil {
			continue
		}
		asvs5ID := "V" + m[1]
		cweSet := make(map[int]struct{})
		for _, tok := range asvs403TokenRe.FindAllStringSubmatch(entry.Tag, -1) {
			for n := range asvs403ToCWE["V"+tok[1]] {
				cweSet[n] = struct{}{}
			}
		}
		for n := range cweSet {
			emit(
				Ref{Scheme: SchemeASVS, ID: asvs5ID},
				Ref{Scheme: SchemeCWE, ID: "CWE-" + strconv.Itoa(n)},
				sourceOpenCREBridge, confidenceLow,
			)
		}
	}

	// 4. WSTG -> CWE via OpenCRE CRE.
	for _, l := range wstgLinks {
		cweSet := make(map[int]struct{})
		for _, cre := range l.CRELinks {
			for n := range creToCWE[cre] {
				cweSet[n] = struct{}{}
			}
		}
		for n := range cweSet {
			emit(
				Ref{Scheme: SchemeWSTG, ID: l.ID},
				Ref{Scheme: SchemeCWE, ID: "CWE-" + strconv.Itoa(n)},
				sourceOpenCRE, confidenceMedium,
			)
		}
	}

	sort.Slice(links, func(i, j int) bool {
		a, b := links[i], links[j]
		if a.From.Scheme != b.From.Scheme {
			return a.From.Scheme < b.From.Scheme
		}
		if a.From.ID != b.From.ID {
			return a.From.ID < b.From.ID
		}
		if a.To.Scheme != b.To.Scheme {
			return a.To.Scheme < b.To.Scheme
		}
		return a.To.ID < b.To.ID
	})

	var warnings []string
	if skippedFrom > 0 {
		warnings = append(warnings, "skipped "+strconv.Itoa(skippedFrom)+" link(s): From ref not in catalog")
	}
	if skippedTo > 0 {
		warnings = append(warnings, "skipped "+strconv.Itoa(skippedTo)+" link(s): To (CWE) ref not in catalog")
	}
	return links, warnings, nil
}

// loadOpenCRELinks reads and parses an OpenCRE *.links.json array.
func loadOpenCRELinks(path string) ([]opencreLink, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var links []opencreLink
	if err := json.Unmarshal(b, &links); err != nil {
		return nil, err
	}
	return links, nil
}

// loadASVSBridge reads and parses the ASVS 5.0 -> 4.0.3 bridge YAML.
func loadASVSBridge(path string) (map[string]asvsBridgeEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bridge map[string]asvsBridgeEntry
	if err := yaml.Unmarshal(b, &bridge); err != nil {
		return nil, err
	}
	return bridge, nil
}
