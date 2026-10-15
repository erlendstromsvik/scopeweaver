package advisory

import (
	"encoding/xml"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Autumn-27/artex/locale"
)

// SourceNSM is the Source value for records from NSM's vulnerability-alert RSS feed.
const SourceNSM = "nsm"

// nsmRSS mirrors the top-level <rss> element of NSM's RSS 0.92 feed.
type nsmRSS struct {
	Channel *nsmChannel `xml:"channel"`
}

// nsmChannel mirrors the <channel> element holding the item list.
type nsmChannel struct {
	Items []nsmItem `xml:"item"`
}

// nsmItem mirrors one <item> element.
type nsmItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

// cveRe matches CVE identifiers (uppercase form).
var cveRe = regexp.MustCompile(`CVE-\d{4}-\d{4,}`)

// wsRe collapses runs of whitespace to a single space.
var wsRe = regexp.MustCompile(`\s+`)

// ParseNSMRSS parses NSM's vulnerability-alert RSS feed (RSS 0.92) into
// Records. It rejects invalid XML, a missing <channel>, and any item
// without a link or with an unparseable pubDate. Zero items is not an
// error. Records are sorted by Date desc, then ID (like ParseKEV).
func ParseNSMRSS(b []byte) ([]Record, error) {
	var feed nsmRSS
	if err := xml.Unmarshal(b, &feed); err != nil {
		return nil, locale.Errorf("parse NSM RSS feed: %v", err)
	}
	if feed.Channel == nil {
		return nil, locale.Errorf("NSM RSS feed has no channel")
	}
	recs := make([]Record, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		if it.Link == "" {
			return nil, locale.Errorf("NSM RSS item missing link (title %q)", it.Title)
		}
		date, derr := parseNSMPubDate(it.PubDate)
		if derr != nil {
			return nil, locale.Errorf("parse NSM RSS pubDate %q for item %q: %v", it.PubDate, it.Title, derr)
		}
		recs = append(recs, Record{
			Source:  SourceNSM,
			ID:      nsmSlug(it.Link),
			Title:   cleanNSMText(it.Title),
			Date:    date,
			CVEs:    extractCVEs(it.Title + " " + it.Description),
			Summary: cleanNSMText(it.Description),
			Link:    it.Link,
		})
	}
	sort.SliceStable(recs, func(i, j int) bool {
		if !recs[i].Date.Equal(recs[j].Date) {
			return recs[i].Date.After(recs[j].Date)
		}
		return recs[i].ID < recs[j].ID
	})
	return recs, nil
}

// parseNSMPubDate parses an RSS pubDate, accepting the numeric-offset
// RFC1123Z form and the GMT-only RFC1123 form.
func parseNSMPubDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC1123Z, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC1123, s)
}

// nsmSlug returns the last non-empty path segment of a link.
func nsmSlug(link string) string {
	trimmed := strings.TrimRight(link, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

// cleanNSMText strips HTML tags, unescapes entities, and collapses
// whitespace. Entities are unescaped after tag stripping so that
// entity-encoded tags (e.g. &lt;b&gt;) are removed as tags too.
func cleanNSMText(s string) string {
	s = stripHTMLTags(s)
	s = html.UnescapeString(s)
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

// stripHTMLTags removes <...> tag markup from text.
func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// extractCVEs returns distinct CVE ids found in text, uppercased, in order
// of first appearance.
func extractCVEs(text string) []string {
	matches := cveRe.FindAllString(strings.ToUpper(text), -1)
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}
