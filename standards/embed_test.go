package standards

import "testing"

func TestEmbeddedCounts(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded(): %v", err)
	}
	if got, want := len(c.ASVS), 345; got != want {
		t.Errorf("embedded ASVS count = %d, want %d", got, want)
	}
	if got, want := len(c.WSTG), 114; got != want {
		t.Errorf("embedded WSTG count = %d, want %d", got, want)
	}
	if got, want := len(c.Top10), 10; got != want {
		t.Errorf("embedded Top10 count = %d, want %d", got, want)
	}
	if got := len(c.CWE); got != 0 {
		t.Errorf("embedded CWE count = %d, want 0", got)
	}

	disk := loadRealCatalog(t)
	if got, want := len(c.ASVS), len(disk.ASVS); got != want {
		t.Errorf("embedded ASVS count = %d, want %d (Load)", got, want)
	}
	if got, want := len(c.WSTG), len(disk.WSTG); got != want {
		t.Errorf("embedded WSTG count = %d, want %d (Load)", got, want)
	}
	if got, want := len(c.Top10), len(disk.Top10); got != want {
		t.Errorf("embedded Top10 count = %d, want %d (Load)", got, want)
	}
}

func TestEmbeddedResolve(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded(): %v", err)
	}
	tests := []struct {
		name   string
		scheme string
		id     string
		want   bool
	}{
		{"asvs hit", SchemeASVS, "V1.1.1", true},
		{"asvs miss", SchemeASVS, "V99.99.99", false},
		{"wstg hit", SchemeWSTG, "WSTG-INFO-01", true},
		{"wstg miss", SchemeWSTG, "WSTG-NOPE-99", false},
		{"top10 hit", SchemeTop10, "A01:2025", true},
		{"cwe miss (not embedded)", SchemeCWE, "79", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Resolve(tt.scheme, tt.id); got != tt.want {
				t.Errorf("Resolve(%q,%q) = %v, want %v", tt.scheme, tt.id, got, tt.want)
			}
		})
	}
}

func TestEmbeddedIsCached(t *testing.T) {
	a, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded(): %v", err)
	}
	b, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded() second call: %v", err)
	}
	if a != b {
		t.Errorf("Embedded() returned different pointers: %p vs %p", a, b)
	}
}

// TestEmbeddedCrosswalk checks the embedded crosswalk matches the shipped file:
// same link count, provenance on every link, and a cached (same-slice) result.
func TestEmbeddedCrosswalk(t *testing.T) {
	embedded, err := EmbeddedCrosswalk()
	if err != nil {
		t.Fatalf("EmbeddedCrosswalk(): %v", err)
	}
	disk, err := LoadCrosswalk("crosswalk.yaml")
	if err != nil {
		t.Fatalf("LoadCrosswalk(crosswalk.yaml): %v", err)
	}
	if len(embedded) != len(disk) {
		t.Errorf("embedded crosswalk has %d links, want %d (LoadCrosswalk)", len(embedded), len(disk))
	}
	if len(embedded) <= 2000 {
		t.Errorf("embedded crosswalk has %d links, want > 2000", len(embedded))
	}
	for i, l := range embedded {
		if l.From.Scheme == "" || l.From.ID == "" || l.To.Scheme == "" || l.To.ID == "" {
			t.Fatalf("link %d has an empty endpoint: %+v", i, l)
		}
		if l.Source == "" || l.Confidence == "" {
			t.Errorf("link %d missing source/confidence: %+v", i, l)
		}
	}
	again, err := EmbeddedCrosswalk()
	if err != nil {
		t.Fatalf("EmbeddedCrosswalk() second call: %v", err)
	}
	if &again[0] != &embedded[0] {
		t.Errorf("EmbeddedCrosswalk() re-parsed instead of returning the cached slice")
	}
}
