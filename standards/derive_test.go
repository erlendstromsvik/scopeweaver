package standards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDeriveCrosswalkRealSources(t *testing.T) {
	cat := loadRealCatalog(t)

	links, warnings, err := DeriveCrosswalk(sourcesDir, cat)
	if err != nil {
		t.Fatalf("DeriveCrosswalk: %v", err)
	}
	if len(links) == 0 {
		t.Fatal("DeriveCrosswalk returned no links")
	}
	t.Logf("derived %d links; warnings: %v", len(links), warnings)

	// Every emitted link resolves at both ends, carries provenance, and only
	// targets CWE (the two derived families are ASVS->CWE and WSTG->CWE).
	for i, l := range links {
		if !cat.Resolve(l.From.Scheme, l.From.ID) {
			t.Errorf("link %d: From %s:%s does not resolve", i, l.From.Scheme, l.From.ID)
		}
		if !cat.Resolve(l.To.Scheme, l.To.ID) {
			t.Errorf("link %d: To %s:%s does not resolve", i, l.To.Scheme, l.To.ID)
		}
		if l.To.Scheme != SchemeCWE {
			t.Errorf("link %d: To scheme = %q, want cwe", i, l.To.Scheme)
		}
		if !strings.HasPrefix(l.To.ID, "CWE-") {
			t.Errorf("link %d: To id %q not normalized to CWE-<n>", i, l.To.ID)
		}
		if l.Source == "" {
			t.Errorf("link %d (%s:%s->%s): empty source", i, l.From.Scheme, l.From.ID, l.To.ID)
		}
		if l.Confidence == "" {
			t.Errorf("link %d (%s:%s->%s): empty confidence", i, l.From.Scheme, l.From.ID, l.To.ID)
		}
	}

	// Count distinct source requirements/tests and total edges per family.
	asvsReqs := map[string]struct{}{}
	wstgTests := map[string]struct{}{}
	var asvsEdges, wstgEdges int
	for _, l := range links {
		switch l.From.Scheme {
		case SchemeASVS:
			asvsReqs[l.From.ID] = struct{}{}
			asvsEdges++
			if l.Source != sourceOpenCREBridge || l.Confidence != confidenceLow {
				t.Errorf("ASVS link %s->%s: source=%q confidence=%q, want %q/%q",
					l.From.ID, l.To.ID, l.Source, l.Confidence, sourceOpenCREBridge, confidenceLow)
			}
		case SchemeWSTG:
			wstgTests[l.From.ID] = struct{}{}
			wstgEdges++
			if l.Source != sourceOpenCRE || l.Confidence != confidenceMedium {
				t.Errorf("WSTG link %s->%s: source=%q confidence=%q, want %q/%q",
					l.From.ID, l.To.ID, l.Source, l.Confidence, sourceOpenCRE, confidenceMedium)
			}
		default:
			t.Errorf("unexpected From scheme %q", l.From.Scheme)
		}
	}
	t.Logf("ASVS: %d reqs / %d edges; WSTG: %d tests / %d edges",
		len(asvsReqs), asvsEdges, len(wstgTests), wstgEdges)

	// ASVS matches the controller's feasibility join (~174 reqs / ~2000 edges).
	if n := len(asvsReqs); n < 150 || n > 200 {
		t.Errorf("ASVS reqs = %d, want 150..200", n)
	}
	if n := asvsEdges; n < 1800 || n > 2200 {
		t.Errorf("ASVS edges = %d, want 1800..2200", n)
	}
	// WSTG is lower than the brief's 45..59 estimate: OpenCRE's vendored WSTG
	// snapshot predates the catalog's WSTG taxonomy (it uses WSTG-INPV/WSTG-APPE
	// where the catalog uses WSTG-INJT/WSTG-APIT), so 17 of 59 OpenCRE ids do
	// not resolve. The range below brackets the real resolvable overlap; the
	// drift is flagged for the controller. See derive.go.
	if n := len(wstgTests); n < 30 || n > 59 {
		t.Errorf("WSTG tests = %d, want 30..59", n)
	}
	if n := wstgEdges; n < 250 || n > 700 {
		t.Errorf("WSTG edges = %d, want 250..700", n)
	}

	// Deterministic: a second derivation yields an identical slice.
	links2, _, err := DeriveCrosswalk(sourcesDir, cat)
	if err != nil {
		t.Fatalf("DeriveCrosswalk (second run): %v", err)
	}
	if !reflect.DeepEqual(links, links2) {
		t.Error("DeriveCrosswalk is not deterministic across runs")
	}
}

// TestCrosswalkArchiveCrossCheck is a sanity check, not a hard gate: it derives
// ASVS 5.0 -> CWE independently via the archived ASVS-5.0-beta direct CWE map
// (v5.0.be_cwe_mapping.json + mapping_v5.0.be_to_v5.0.0.yml) and reports how
// much it overlaps the OpenCRE-spine route. A non-empty overlap means the two
// independent derivations agree on at least some ASVS->CWE pairs.
func TestCrosswalkArchiveCrossCheck(t *testing.T) {
	cat := loadRealCatalog(t)

	links, _, err := DeriveCrosswalk(sourcesDir, cat)
	if err != nil {
		t.Fatalf("DeriveCrosswalk: %v", err)
	}
	opencreSet := map[string]struct{}{}
	for _, l := range links {
		if l.From.Scheme == SchemeASVS {
			opencreSet[l.From.ID+"|"+l.To.ID] = struct{}{}
		}
	}

	// Archived beta route: be-id -> CWE and be-id -> v5.0.0-id.
	beCWE := map[string]string{}
	b, err := os.ReadFile(filepath.Join(sourcesDir, "asvs/mappings/v5.0.be_cwe_mapping.json"))
	if err != nil {
		t.Fatalf("read beta CWE map: %v", err)
	}
	if err := json.Unmarshal(b, &beCWE); err != nil {
		t.Fatalf("parse beta CWE map: %v", err)
	}
	beTo5 := map[string]string{}
	b, err = os.ReadFile(filepath.Join(sourcesDir, "asvs/mappings/mapping_v5.0.be_to_v5.0.0.yml"))
	if err != nil {
		t.Fatalf("read beta->5.0.0 map: %v", err)
	}
	if err := yaml.Unmarshal(b, &beTo5); err != nil {
		t.Fatalf("parse beta->5.0.0 map: %v", err)
	}

	archiveSet := map[string]struct{}{}
	for beID, cwe := range beCWE {
		v5, ok := beTo5[beID]
		if !ok {
			continue
		}
		// v5.0.0-<C.S.R> -> V<C.S.R>
		parts := strings.SplitN(v5, "-", 2)
		if len(parts) != 2 {
			continue
		}
		asvs5ID := "V" + parts[1]
		cweID := "CWE-" + strings.TrimSpace(cwe)
		if !cat.Resolve(SchemeASVS, asvs5ID) || !cat.Resolve(SchemeCWE, cweID) {
			continue
		}
		archiveSet[asvs5ID+"|"+cweID] = struct{}{}
	}

	var overlap int
	for k := range archiveSet {
		if _, ok := opencreSet[k]; ok {
			overlap++
		}
	}
	var pctArchive, pctOpenCRE float64
	if len(archiveSet) > 0 {
		pctArchive = 100 * float64(overlap) / float64(len(archiveSet))
	}
	if len(opencreSet) > 0 {
		pctOpenCRE = 100 * float64(overlap) / float64(len(opencreSet))
	}
	t.Logf("ASVS5->CWE cross-check: opencre pairs=%d, archive pairs=%d, overlap=%d (%.1f%% of archive, %.1f%% of opencre)",
		len(opencreSet), len(archiveSet), overlap, pctArchive, pctOpenCRE)

	if overlap == 0 {
		t.Error("ASVS5->CWE cross-check: zero overlap between OpenCRE and archived routes")
	}
}
