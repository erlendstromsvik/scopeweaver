package advisory

import (
	"os"
	"strings"
	"testing"
	"time"
)

const (
	nsmArchivePage00 = "testdata/nsm-archive-page-00.html"
	nsmArchivePage25 = "testdata/nsm-archive-page-25.html"
	citrixSlug       = "aktivt-utnyttelse-av-kritiske-sarbarheter-i-citrix-netscaler"
)

func TestParseNSMArchivePage00(t *testing.T) {
	b, err := os.ReadFile(nsmArchivePage00)
	if err != nil {
		t.Fatalf("read archive page 00: %v", err)
	}
	recs, err := ParseNSMArchivePage(b)
	if err != nil {
		t.Fatalf("ParseNSMArchivePage: %v", err)
	}
	if len(recs) != 10 {
		t.Fatalf("len(recs) = %d, want 10", len(recs))
	}
	seen := map[string]bool{}
	for _, r := range recs {
		if r.ID == "" {
			t.Errorf("record %q has empty ID", r.Title)
		}
		if seen[r.ID] {
			t.Errorf("duplicate ID %s", r.ID)
		}
		seen[r.ID] = true
		if r.Source != SourceNSM {
			t.Errorf("%s Source = %q, want %q", r.ID, r.Source, SourceNSM)
		}
	}
	var citrix *Record
	for i := range recs {
		if recs[i].ID == citrixSlug {
			citrix = &recs[i]
		}
	}
	if citrix == nil {
		t.Fatalf("no record with ID %s; got %v", citrixSlug, ids(recs))
	}
	if got, want := citrix.Date.Format("2006-01-02"), "2026-09-27"; got != want {
		t.Errorf("citrix Date = %s, want %s", got, want)
	}
	want := []string{"CVE-2026-88771", "CVE-2026-88772"}
	if strings.Join(citrix.CVEs, " ") != strings.Join(want, " ") {
		t.Errorf("citrix CVEs = %v, want %v", citrix.CVEs, want)
	}
	if !strings.Contains(citrix.Summary, "NSM ønsker å fremheve") {
		t.Errorf("citrix Summary = %q, want the digest text", citrix.Summary)
	}
}

func TestParseNSMArchivePage25(t *testing.T) {
	b, err := os.ReadFile(nsmArchivePage25)
	if err != nil {
		t.Fatalf("read archive page 25: %v", err)
	}
	recs, err := ParseNSMArchivePage(b)
	if err != nil {
		t.Fatalf("ParseNSMArchivePage: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("len(recs) = %d, want 2", len(recs))
	}
	if recs[0].Date.Format("2006-01-02") != "2019-01-09" {
		t.Errorf("first record date = %s, want 2019-01-09", recs[0].Date.Format("2006-01-02"))
	}
	if recs[1].Date.Format("2006-01-02") != "2018-01-25" {
		t.Errorf("second record date = %s, want 2018-01-25", recs[1].Date.Format("2006-01-02"))
	}
}

// The archive slug must equal the ID ParseNSMRSS derives for the same
// alert, so a backfilled register and a feed sync never double-record it.
func TestNSMArchiveIDMatchesRSS(t *testing.T) {
	ab, err := os.ReadFile(nsmArchivePage00)
	if err != nil {
		t.Fatalf("read archive page 00: %v", err)
	}
	archive, err := ParseNSMArchivePage(ab)
	if err != nil {
		t.Fatalf("ParseNSMArchivePage: %v", err)
	}
	rb, err := os.ReadFile(vendoredNSM)
	if err != nil {
		t.Fatalf("read vendored NSM feed: %v", err)
	}
	feed, err := ParseNSMRSS(rb)
	if err != nil {
		t.Fatalf("ParseNSMRSS: %v", err)
	}
	feedIDs := map[string]bool{}
	for _, r := range feed {
		feedIDs[r.ID] = true
	}
	if !feedIDs[citrixSlug] {
		t.Fatalf("vendored feed has no %s; IDs: %v", citrixSlug, ids(feed))
	}
	found := false
	for _, r := range archive {
		if r.ID == citrixSlug {
			found = true
		}
	}
	if !found {
		t.Errorf("archive has no %s; IDs: %v", citrixSlug, ids(archive))
	}
}

func TestParseNSMArchiveUnknownMonth(t *testing.T) {
	html := `<html><body><article class="default article card list-view" itemid="https://nsm.no/x/foo#article">` +
		`<div class="articlelist__meta">27 september 2026</div>` +
		`<h3 itemprop="headline">Tittel</h3>` +
		`<div class="digest">sum</div></article>` +
		`<article class="default article card list-view" itemid="https://nsm.no/x/bar#article">` +
		`<div class="articlelist__meta">27 novmber 2026</div>` +
		`<h3 itemprop="headline">Måned feil</h3>` +
		`<div class="digest">sum</div></article></body></html>`
	_, err := ParseNSMArchivePage([]byte(html))
	if err == nil {
		t.Fatal("unknown month must error")
	}
	if !strings.Contains(err.Error(), "novmber") || !strings.Contains(err.Error(), "Måned feil") {
		t.Errorf("error %q must name the month and the title", err)
	}
}

func TestParseNSMArchiveMissingHeadline(t *testing.T) {
	html := `<html><body><article class="default article card list-view" itemid="https://nsm.no/x/foo#article">` +
		`<div class="articlelist__meta">27 september 2026</div>` +
		`<div class="digest">sum</div></article></body></html>`
	_, err := ParseNSMArchivePage([]byte(html))
	if err == nil {
		t.Fatal("article without headline must error")
	}
	if !strings.Contains(err.Error(), "missing headline") {
		t.Errorf("error %q must report missing headline", err)
	}
}

func TestParseNSMArchiveNoArticles(t *testing.T) {
	html := `<html><body><article class="default full-view">not a listing</article></body></html>`
	_, err := ParseNSMArchivePage([]byte(html))
	if err == nil {
		t.Fatal("page without list-view articles must error")
	}
	if !strings.Contains(err.Error(), "no alerts found on the NSM archive page") {
		t.Errorf("error %q must report the format change", err)
	}
}

func TestParseNSMArchiveMissingLink(t *testing.T) {
	html := `<html><body><article class="default article card list-view">` +
		`<div class="articlelist__meta">27 september 2026</div>` +
		`<h3 itemprop="headline">Uten lenke</h3>` +
		`<div class="digest">sum</div></article></body></html>`
	_, err := ParseNSMArchivePage([]byte(html))
	if err == nil {
		t.Fatal("article without link must error")
	}
	if !strings.Contains(err.Error(), "missing link") || !strings.Contains(err.Error(), "Uten lenke") {
		t.Errorf("error %q must report missing link with the title", err)
	}
}

func TestParseNSMArchiveLinkFallsBackToAnchor(t *testing.T) {
	html := `<html><body><article class="default article card list-view">` +
		`<a href="https://nsm.no/fagomrader/varsler-fra-nsm/fallback-item">x</a>` +
		`<div class="articlelist__meta">1 mars 2025</div>` +
		`<h3 itemprop="headline">Fallback</h3>` +
		`<div class="digest">sum</div></article></body></html>`
	recs, err := ParseNSMArchivePage([]byte(html))
	if err != nil {
		t.Fatalf("ParseNSMArchivePage: %v", err)
	}
	if recs[0].ID != "fallback-item" {
		t.Errorf("ID = %q, want fallback-item", recs[0].ID)
	}
	if recs[0].Date.Format("2006-01-02") != "2025-03-01" {
		t.Errorf("date = %s, want 2025-03-01", recs[0].Date.Format("2006-01-02"))
	}
}

func TestParseNSMArchiveNBSPCollapsed(t *testing.T) {
	html := `<html><body><article class="default article card list-view" itemid="https://nsm.no/x/nbsp#article">` +
		`<div class="articlelist__meta">1&nbsp;juli&nbsp;2024</div>` +
		`<h3 itemprop="headline">Nbsp&nbsp;tittel</h3>` +
		`<div class="digest"><p>sum&nbsp;med&nbsp;nbsp</p></div></article></body></html>`
	recs, err := ParseNSMArchivePage([]byte(html))
	if err != nil {
		t.Fatalf("ParseNSMArchivePage: %v", err)
	}
	if recs[0].Title != "Nbsp tittel" {
		t.Errorf("Title = %q, want \"Nbsp tittel\"", recs[0].Title)
	}
	if recs[0].Summary != "sum med nbsp" {
		t.Errorf("Summary = %q, want \"sum med nbsp\"", recs[0].Summary)
	}
	if recs[0].Date.Format("2006-01-02") != "2024-07-01" {
		t.Errorf("date = %s, want 2024-07-01", recs[0].Date.Format("2006-01-02"))
	}
}

func TestMergeRegisterUsesRecordCalendarDate(t *testing.T) {
	rec := Record{
		Source: SourceNSM,
		ID:     "late-night",
		Title:  "Late night",
		Date:   time.Date(2026, 9, 27, 0, 30, 0, 0, time.FixedZone("CEST", 2*3600)),
		Link:   "https://nsm.no/late-night",
	}
	got := MergeRegister(nil, []Record{rec}, time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Date != "2026-09-27" {
		t.Errorf("Date = %q, want 2026-09-27 (record's own calendar day, not UTC)", got[0].Date)
	}
}

func ids(recs []Record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.ID
	}
	return out
}
