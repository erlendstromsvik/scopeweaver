package standards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sourcesDir = "sources"

func loadRealCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, warnings, err := Load(sourcesDir)
	if err != nil {
		t.Fatalf("Load(%q): %v", sourcesDir, err)
	}
	if len(warnings) != 0 {
		t.Fatalf("Load returned unexpected warnings: %v", warnings)
	}
	return c
}

func TestLoadRealSourcesCounts(t *testing.T) {
	c := loadRealCatalog(t)

	if got, want := len(c.ASVS), 345; got != want {
		t.Errorf("ASVS count = %d, want %d", got, want)
	}
	if got, want := len(c.WSTG), 114; got != want {
		t.Errorf("WSTG count = %d, want %d", got, want)
	}
	if got, want := len(c.Top10), 10; got != want {
		t.Errorf("Top10 count = %d, want %d", got, want)
	}
	if got := len(c.CWE); got <= 900 {
		t.Errorf("CWE count = %d, want > 900", got)
	}
}

func TestValidateRealSourcesPasses(t *testing.T) {
	c := loadRealCatalog(t)
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() on real sources: %v", err)
	}
}

func TestEmptyCrosswalkFileValidates(t *testing.T) {
	links, err := LoadCrosswalk("crosswalk.yaml")
	if err != nil {
		t.Fatalf("LoadCrosswalk(crosswalk.yaml): %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("shipped crosswalk.yaml should be empty, got %d links", len(links))
	}
	c := loadRealCatalog(t)
	c.Crosswalk = links
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() with empty crosswalk: %v", err)
	}
}

func TestResolve(t *testing.T) {
	c := loadRealCatalog(t)
	tests := []struct {
		name   string
		scheme string
		id     string
		want   bool
	}{
		{"asvs hit", SchemeASVS, "V1.1.1", true},
		{"asvs miss", SchemeASVS, "V99.99.99", false},
		{"wstg hit", SchemeWSTG, "WSTG-INFO-01", true},
		{"wstg miss", SchemeWSTG, "WSTG-INFO-99", false},
		{"top10 hit", SchemeTop10, "A01:2025", true},
		{"top10 miss", SchemeTop10, "A99:2025", false},
		{"cwe hit numeric", SchemeCWE, "79", true},
		{"cwe hit prefixed", SchemeCWE, "CWE-79", true},
		{"cwe miss", SchemeCWE, "99999999", false},
		{"cwe non-numeric", SchemeCWE, "nope", false},
		{"scheme case-insensitive", "ASVS", "V1.1.1", true},
		{"unknown scheme", "bogus", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Resolve(tt.scheme, tt.id); got != tt.want {
				t.Errorf("Resolve(%q,%q) = %v, want %v", tt.scheme, tt.id, got, tt.want)
			}
		})
	}
}

func TestValidateRejectsInvalidFixtures(t *testing.T) {
	tests := []struct {
		name       string
		build      func(t *testing.T) *Catalog
		wantSubstr string
	}{
		{
			name: "duplicate ASVS id",
			build: func(t *testing.T) *Catalog {
				b := readFixture(t, "asvs_dup.json")
				m, dups, err := parseASVS(b)
				if err != nil {
					t.Fatalf("parseASVS: %v", err)
				}
				if len(dups) == 0 {
					t.Fatal("parseASVS did not detect the duplicate id")
				}
				return &Catalog{ASVS: m, dups: dups}
			},
			wantSubstr: "duplicate asvs id",
		},
		{
			name: "ASVS level out of range",
			build: func(t *testing.T) *Catalog {
				b := readFixture(t, "asvs_badlevel.json")
				m, _, err := parseASVS(b)
				if err != nil {
					t.Fatalf("parseASVS: %v", err)
				}
				return &Catalog{ASVS: m}
			},
			wantSubstr: "outside 1..3",
		},
		{
			name: "WSTG id pattern",
			build: func(t *testing.T) *Catalog {
				b := readFixture(t, "wstg_badid.json")
				m, _, err := parseWSTG(b)
				if err != nil {
					t.Fatalf("parseWSTG: %v", err)
				}
				return &Catalog{WSTG: m}
			},
			wantSubstr: "WSTG id",
		},
		{
			name: "Top 10 id pattern",
			build: func(t *testing.T) *Catalog {
				b := readFixture(t, "top10_badid.yaml")
				m, _, err := parseTop10(b)
				if err != nil {
					t.Fatalf("parseTop10: %v", err)
				}
				return &Catalog{Top10: m}
			},
			wantSubstr: "Top 10 id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.build(t)
			err := c.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("Validate() error = %q, want substring %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}

func TestValidateRejectsDanglingCrosswalk(t *testing.T) {
	c := loadRealCatalog(t)
	links, err := LoadCrosswalk(filepath.Join("testdata", "crosswalk_mixed.yaml"))
	if err != nil {
		t.Fatalf("LoadCrosswalk: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("fixture should have 2 links, got %d", len(links))
	}
	c.Crosswalk = links

	err = c.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want dangling-ref error")
	}
	if !strings.Contains(err.Error(), "dangling") || !strings.Contains(err.Error(), "cwe:99999999") {
		t.Errorf("Validate() error = %q, want it to flag the dangling cwe ref", err.Error())
	}

	// With only the valid link, Validate passes: proves the mechanism accepts
	// resolvable links and rejects only the dangling one.
	c.Crosswalk = links[:1]
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() with the resolvable link only: %v", err)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return b
}
