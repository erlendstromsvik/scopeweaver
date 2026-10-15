package advisory

import (
	"os"
	"strings"
	"testing"
	"time"
)

const vendoredNSM = "testdata/nsm-sarbarhetsvarsler-2026-10-15.rss"

func TestParseNSMRSSVendored(t *testing.T) {
	b, err := os.ReadFile(vendoredNSM)
	if err != nil {
		t.Fatalf("read vendored NSM feed: %v", err)
	}
	recs, err := ParseNSMRSS(b)
	if err != nil {
		t.Fatalf("ParseNSMRSS: %v", err)
	}
	if len(recs) != 10 {
		t.Fatalf("len(recs) = %d, want 10", len(recs))
	}
	for i, r := range recs {
		if r.Source != "nsm" {
			t.Fatalf("recs[%d].Source = %q, want nsm", i, r.Source)
		}
		if r.ID == "" {
			t.Fatalf("recs[%d].ID empty", i)
		}
		if r.Title == "" {
			t.Fatalf("recs[%d].Title empty", i)
		}
		if r.Link == "" {
			t.Fatalf("recs[%d].Link empty", i)
		}
		if r.Date.Year() != 2026 {
			t.Fatalf("recs[%d].Date year = %d, want 2026", i, r.Date.Year())
		}
	}
	// Sorted date desc, then ID.
	for i := 1; i < len(recs); i++ {
		prev, cur := recs[i-1], recs[i]
		if prev.Date.Before(cur.Date) {
			t.Fatalf("not sorted desc at %d: %v before %v", i, prev.Date, cur.Date)
		}
		if prev.Date.Equal(cur.Date) && prev.ID > cur.ID {
			t.Fatalf("same-date tie not sorted by ID at %d: %q > %q", i, prev.ID, cur.ID)
		}
	}
	// The Citrix item that carries CVEs must expose exactly these, in order.
	var citrix *Record
	for i := range recs {
		r := &recs[i]
		if strings.Contains(r.Title, "Citrix") && len(r.CVEs) > 0 {
			citrix = r
			break
		}
	}
	if citrix == nil {
		t.Fatal("no Citrix item with CVEs found")
	}
	want := []string{"CVE-2026-88771", "CVE-2026-88772"}
	if len(citrix.CVEs) != len(want) {
		t.Fatalf("citrix CVEs = %v, want %v", citrix.CVEs, want)
	}
	for i, w := range want {
		if citrix.CVEs[i] != w {
			t.Fatalf("citrix CVEs[%d] = %q, want %q", i, citrix.CVEs[i], w)
		}
	}
}

func TestParseNSMRSSStripsHTMLAndEntities(t *testing.T) {
	src := `<?xml version="1.0"?><rss version="0.92"><channel><title>t</title>` +
		`<item><title>Foo &amp; Bar</title>` +
		`<link>https://nsm.no/a/b/slug-one</link>` +
		`<description>&lt;b&gt;CVE-2026-1234&lt;/b&gt; &amp; more &amp;nbsp; text</description>` +
		`<pubDate>Sun, 27 Sep 2026 20:05:15 +0200</pubDate></item>` +
		`</channel></rss>`
	recs, err := ParseNSMRSS([]byte(src))
	if err != nil {
		t.Fatalf("ParseNSMRSS: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("len(recs) = %d, want 1", len(recs))
	}
	r := recs[0]
	if r.Title != "Foo & Bar" {
		t.Fatalf("title = %q, want %q", r.Title, "Foo & Bar")
	}
	if strings.ContainsAny(r.Summary, "<>") {
		t.Fatalf("summary still has tags: %q", r.Summary)
	}
	if !strings.Contains(r.Summary, "& more") {
		t.Fatalf("summary missing unescaped ampersand: %q", r.Summary)
	}
	if strings.Contains(r.Summary, "  ") {
		t.Fatalf("summary not whitespace-collapsed: %q", r.Summary)
	}
	if len(r.CVEs) != 1 || r.CVEs[0] != "CVE-2026-1234" {
		t.Fatalf("CVEs = %v", r.CVEs)
	}
	if r.ID != "slug-one" {
		t.Fatalf("ID = %q, want slug-one", r.ID)
	}
}

func TestParseNSMRSSDedupesCVEs(t *testing.T) {
	src := `<?xml version="1.0"?><rss version="0.92"><channel><title>t</title>` +
		`<item><title>CVE-2026-5555 dup</title>` +
		`<link>https://nsm.no/x/dup</link>` +
		`<description>mentions CVE-2026-5555 and CVE-2026-7777 and cve-2026-5555 again</description>` +
		`<pubDate>Sun, 27 Sep 2026 20:05:15 +0200</pubDate></item>` +
		`</channel></rss>`
	recs, err := ParseNSMRSS([]byte(src))
	if err != nil {
		t.Fatalf("ParseNSMRSS: %v", err)
	}
	want := []string{"CVE-2026-5555", "CVE-2026-7777"}
	if len(recs[0].CVEs) != len(want) {
		t.Fatalf("CVEs = %v, want %v", recs[0].CVEs, want)
	}
	for i, w := range want {
		if recs[0].CVEs[i] != w {
			t.Fatalf("CVEs[%d] = %q, want %q", i, recs[0].CVEs[i], w)
		}
	}
}

func TestParseNSMRSSRFC1123NoZone(t *testing.T) {
	// RFC1123 form: "Mon, 02 Jan 2006 15:04:05 GMT" (no numeric offset).
	src := `<?xml version="1.0"?><rss version="0.92"><channel><title>t</title>` +
		`<item><title>GMT item</title>` +
		`<link>https://nsm.no/x/gmt</link>` +
		`<description>plain</description>` +
		`<pubDate>Mon, 05 Oct 2026 09:30:00 GMT</pubDate></item>` +
		`</channel></rss>`
	recs, err := ParseNSMRSS([]byte(src))
	if err != nil {
		t.Fatalf("ParseNSMRSS: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("len(recs) = %d, want 1", len(recs))
	}
	want := time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)
	if !recs[0].Date.Equal(want) {
		t.Fatalf("date = %v, want %v", recs[0].Date, want)
	}
}

func TestParseNSMRSSBadPubDate(t *testing.T) {
	src := `<?xml version="1.0"?><rss version="0.92"><channel><title>t</title>` +
		`<item><title>Broken Date Item</title>` +
		`<link>https://nsm.no/x/bad</link>` +
		`<description>plain</description>` +
		`<pubDate>not a date</pubDate></item>` +
		`</channel></rss>`
	_, err := ParseNSMRSS([]byte(src))
	if err == nil {
		t.Fatal("expected pubDate error")
	}
	if !strings.Contains(err.Error(), "Broken Date Item") {
		t.Fatalf("error should mention the title, got %v", err)
	}
}

func TestParseNSMRSSMissingLink(t *testing.T) {
	src := `<?xml version="1.0"?><rss version="0.92"><channel><title>t</title>` +
		`<item><title>No Link Item</title>` +
		`<description>plain</description>` +
		`<pubDate>Sun, 27 Sep 2026 20:05:15 +0200</pubDate></item>` +
		`</channel></rss>`
	_, err := ParseNSMRSS([]byte(src))
	if err == nil {
		t.Fatal("expected missing-link error")
	}
	if !strings.Contains(err.Error(), "No Link Item") {
		t.Fatalf("error should mention the title, got %v", err)
	}
}

func TestParseNSMRSSInvalidXML(t *testing.T) {
	_, err := ParseNSMRSS([]byte("<rss><channel><broken"))
	if err == nil {
		t.Fatal("expected invalid XML error")
	}
}

func TestParseNSMRSSNoChannel(t *testing.T) {
	_, err := ParseNSMRSS([]byte(`<?xml version="1.0"?><rss version="0.92"></rss>`))
	if err == nil {
		t.Fatal("expected no-channel error")
	}
	if !strings.Contains(err.Error(), "channel") {
		t.Fatalf("error should mention channel, got %v", err)
	}
}

func TestParseNSMRSSEmptyChannel(t *testing.T) {
	src := `<?xml version="1.0"?><rss version="0.92"><channel><title>t</title></channel></rss>`
	recs, err := ParseNSMRSS([]byte(src))
	if err != nil {
		t.Fatalf("empty channel should not error, got %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("len(recs) = %d, want 0", len(recs))
	}
}
