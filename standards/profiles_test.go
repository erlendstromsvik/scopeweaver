package standards

import (
	"strings"
	"testing"
)

// The profile counts below are pinned to the vendored catalogs: 70 ASVS L1
// requirements, 253 L1+L2 (70 + 183), 114 WSTG tests, 14 of them WSTG-CLNT-*.

func TestProfilesHasThreeNames(t *testing.T) {
	profiles, err := Profiles()
	if err != nil {
		t.Fatalf("Profiles(): %v", err)
	}
	want := []string{"asvs-l1-web", "asvs-l2-api", "asvs-l2-web"}
	if len(profiles) != len(want) {
		t.Fatalf("Profiles() has %d entries, want %d: %v", len(profiles), len(want), ProfileNames())
	}
	for _, n := range want {
		if _, ok := profiles[n]; !ok {
			t.Errorf("missing profile %q", n)
		}
	}
	if got := ProfileNames(); !stringsEqual(got, want) {
		t.Errorf("ProfileNames() = %v, want %v", got, want)
	}
}

func TestSelectRequirementsL1Web(t *testing.T) {
	c := embeddedCatalog(t)
	reqs, err := c.SelectRequirements([]string{"asvs-l1-web"})
	if err != nil {
		t.Fatalf("SelectRequirements: %v", err)
	}
	asvs, wstg := countByStandard(reqs)
	if asvs != 70 {
		t.Errorf("asvs count = %d, want 70 (L1 requirements)", asvs)
	}
	if wstg != 114 {
		t.Errorf("wstg count = %d, want 114 (all WSTG tests)", wstg)
	}
	// No L2/L3 requirement leaked in.
	for _, r := range reqs {
		if r.Standard != SchemeASVS {
			continue
		}
		if lvl := int(c.ASVS[r.ID].Level); lvl > 1 {
			t.Errorf("asvs %s level %d > 1 in asvs-l1-web", r.ID, lvl)
		}
	}
}

func TestSelectRequirementsL2ApiExcludesClient(t *testing.T) {
	c := embeddedCatalog(t)
	reqs, err := c.SelectRequirements([]string{"asvs-l2-api"})
	if err != nil {
		t.Fatalf("SelectRequirements: %v", err)
	}
	asvs, wstg := countByStandard(reqs)
	if asvs != 253 {
		t.Errorf("asvs count = %d, want 253 (L1+L2 requirements)", asvs)
	}
	if wstg != 100 {
		t.Errorf("wstg count = %d, want 100 (114 minus 14 WSTG-CLNT-*)", wstg)
	}
	for _, r := range reqs {
		if strings.HasPrefix(r.ID, "WSTG-CLNT-") {
			t.Errorf("WSTG-CLNT-* test %q not excluded by asvs-l2-api", r.ID)
		}
	}
}

func TestSelectRequirementsUnknownProfile(t *testing.T) {
	c := embeddedCatalog(t)
	if _, err := c.SelectRequirements([]string{"nope"}); err == nil {
		t.Fatal("SelectRequirements([nope]): want error, got nil")
	} else if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not name the unknown profile", err)
	}
}

func TestSelectRequirementsUnionNoDuplicates(t *testing.T) {
	c := embeddedCatalog(t)
	reqs, err := c.SelectRequirements([]string{"asvs-l1-web", "asvs-l2-web"})
	if err != nil {
		t.Fatalf("SelectRequirements: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range reqs {
		key := r.Standard + "\x00" + r.ID
		if seen[key] {
			t.Errorf("duplicate requirement %s/%s in union", r.Standard, r.ID)
		}
		seen[key] = true
	}
	// The union of L1-web and L2-web is L2-web: 253 ASVS + 114 WSTG.
	asvs, wstg := countByStandard(reqs)
	if asvs != 253 || wstg != 114 {
		t.Errorf("union counts = (%d asvs, %d wstg), want (253, 114)", asvs, wstg)
	}
}

func TestSelectRequirementsNotAutomatableReason(t *testing.T) {
	c := embeddedCatalog(t)
	reqs, err := c.SelectRequirements([]string{"asvs-l1-web"})
	if err != nil {
		t.Fatalf("SelectRequirements: %v", err)
	}
	byID := map[string]ScopedRequirement{}
	for _, r := range reqs {
		byID[r.ID] = r
	}
	// V2.1.1 is a documentation-review requirement: it must carry the reason.
	v211, ok := byID["V2.1.1"]
	if !ok {
		t.Fatal("V2.1.1 missing from asvs-l1-web selection")
	}
	if v211.NotAutomatableReason == "" {
		t.Error("V2.1.1 has no not-automatable reason")
	}
	// V1.2.2 is a level-1 requirement without documentation wording: no reason.
	v122, ok := byID["V1.2.2"]
	if !ok {
		t.Fatal("V1.2.2 missing from asvs-l1-web selection")
	}
	if v122.NotAutomatableReason != "" {
		t.Errorf("V1.2.2 unexpectedly has reason %q", v122.NotAutomatableReason)
	}
	// WSTG tests never carry a reason.
	for _, r := range reqs {
		if r.Standard == SchemeWSTG && r.NotAutomatableReason != "" {
			t.Errorf("wstg %s carries a not-automatable reason", r.ID)
		}
	}
}

func TestSelectRequirementsSorted(t *testing.T) {
	c := embeddedCatalog(t)
	reqs, err := c.SelectRequirements([]string{"asvs-l2-api"})
	if err != nil {
		t.Fatalf("SelectRequirements: %v", err)
	}
	for i := 1; i < len(reqs); i++ {
		a, b := reqs[i-1], reqs[i]
		if a.Standard > b.Standard {
			t.Fatalf("not sorted by standard at %d: %s > %s", i, a.Standard, b.Standard)
		}
		if a.Standard == b.Standard && !naturalLess(a.ID, b.ID) && a.ID != b.ID {
			t.Fatalf("not in natural id order at %d: %s vs %s", i, a.ID, b.ID)
		}
	}
}

func TestProfilesMaxASVSLevel(t *testing.T) {
	if got := ProfilesMaxASVSLevel(nil); got != 0 {
		t.Errorf("ProfilesMaxASVSLevel(nil) = %d, want 0", got)
	}
	if got := ProfilesMaxASVSLevel([]string{}); got != 0 {
		t.Errorf("ProfilesMaxASVSLevel([]) = %d, want 0", got)
	}
	if got := ProfilesMaxASVSLevel([]string{"asvs-l1-web"}); got != 1 {
		t.Errorf("ProfilesMaxASVSLevel(l1-web) = %d, want 1", got)
	}
	if got := ProfilesMaxASVSLevel([]string{"asvs-l1-web", "asvs-l2-api"}); got != 2 {
		t.Errorf("ProfilesMaxASVSLevel(l1-web, l2-api) = %d, want 2", got)
	}
	// An unknown name yields 0 so the caller keeps its default scope.
	if got := ProfilesMaxASVSLevel([]string{"asvs-l2-web", "nope"}); got != 0 {
		t.Errorf("ProfilesMaxASVSLevel with unknown name = %d, want 0", got)
	}
}

func TestProfilesIncludeWSTG(t *testing.T) {
	if !ProfilesIncludeWSTG([]string{"asvs-l1-web"}) {
		t.Error("asvs-l1-web should include WSTG")
	}
	if ProfilesIncludeWSTG(nil) {
		t.Error("no profiles should not include WSTG")
	}
}

func embeddedCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded(): %v", err)
	}
	return c
}

func countByStandard(reqs []ScopedRequirement) (asvs, wstg int) {
	for _, r := range reqs {
		switch r.Standard {
		case SchemeASVS:
			asvs++
		case SchemeWSTG:
			wstg++
		}
	}
	return
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
