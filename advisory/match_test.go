package advisory

import (
	"testing"

	"github.com/Autumn-27/artex/db"
)

func rec(id, vendor, product string) Record {
	return Record{Source: SourceCISAKEV, ID: id, Vendor: vendor, Product: product}
}

func asset(id int64, techs ...string) *db.Asset {
	return &db.Asset{ID: id, Technologies: techs}
}

func TestMatchAssetsTable(t *testing.T) {
	cases := []struct {
		name    string
		rec     Record
		tech    string
		matches bool
	}{
		{"exchange full", rec("CVE-1", "Microsoft", "Exchange Server"), "Microsoft Exchange Server 2019", true},
		{"confluence missing server token", rec("CVE-2", "Atlassian", "Confluence Server"), "Atlassian Confluence 8.5", false},
		{"edge short no vendor", rec("CVE-3", "Microsoft", "Edge"), "Cloudflare edge proxy", false},
		{"edge with vendor", rec("CVE-4", "Microsoft", "Edge"), "Microsoft Edge 120", true},
		{"single token >=5 chars", rec("CVE-5", "Acme", "Paloalto"), "deployed paloalto gateway", true},
		{"single token <5 no vendor", rec("CVE-6", "Acme", "Rax"), "rax host", false},
		{"vendor appears single short product", rec("CVE-7", "Acme", "Rax"), "Acme rax host", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchAssets([]Record{tc.rec}, []*db.Asset{asset(1, tc.tech)})
			if tc.matches && len(got) != 1 {
				t.Fatalf("expected match for %q vs %q, got %d", tc.rec.Product, tc.tech, len(got))
			}
			if !tc.matches && len(got) != 0 {
				t.Fatalf("expected no match for %q vs %q, got %+v", tc.rec.Product, tc.tech, got)
			}
			if tc.matches && got[0].Technology != tc.tech {
				t.Fatalf("technology = %q, want %q", got[0].Technology, tc.tech)
			}
		})
	}
}

func TestMatchAssetsSkipsAssetsWithoutTechnologies(t *testing.T) {
	got := MatchAssets([]Record{rec("CVE-1", "Microsoft", "Exchange Server")}, []*db.Asset{{ID: 7}})
	if len(got) != 0 {
		t.Fatalf("expected no match for asset without technologies, got %+v", got)
	}
}

func TestMatchAssetsOrdering(t *testing.T) {
	recs := []Record{
		rec("CVE-9", "Microsoft", "Exchange Server"),
		rec("CVE-1", "Microsoft", "Exchange Server"),
	}
	assets := []*db.Asset{
		asset(2, "Microsoft Exchange Server 2019"),
		asset(1, "Microsoft Exchange Server 2016"),
	}
	got := MatchAssets(recs, assets)
	if len(got) != 4 {
		t.Fatalf("expected 4 matches, got %d", len(got))
	}
	// Sorted by AssetID, then record ID.
	want := []struct {
		asset int64
		id    string
	}{
		{1, "CVE-1"}, {1, "CVE-9"}, {2, "CVE-1"}, {2, "CVE-9"},
	}
	for i, w := range want {
		if got[i].AssetID != w.asset || got[i].Record.ID != w.id {
			t.Fatalf("got[%d] = (%d,%s), want (%d,%s)", i, got[i].AssetID, got[i].Record.ID, w.asset, w.id)
		}
	}
}

func TestMatchAssetsOnePerRecordAssetTechnology(t *testing.T) {
	recs := []Record{rec("CVE-1", "Microsoft", "Exchange Server")}
	assets := []*db.Asset{asset(1, "Microsoft Exchange Server 2019", "Microsoft Exchange Server 2016")}
	got := MatchAssets(recs, assets)
	if len(got) != 2 {
		t.Fatalf("expected 2 matches (one per technology), got %d", len(got))
	}
	if got[0].Technology == got[1].Technology {
		t.Fatal("expected distinct technologies")
	}
}

func TestMatchAssetsEmptyProductNoMatch(t *testing.T) {
	got := MatchAssets([]Record{rec("CVE-1", "Microsoft", "")}, []*db.Asset{asset(1, "Microsoft Exchange Server 2019")})
	if len(got) != 0 {
		t.Fatalf("expected no match for empty product, got %+v", got)
	}
}
