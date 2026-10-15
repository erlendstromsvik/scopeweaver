package advisory

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/locale"
	"golang.org/x/net/html"
)

// nsmMonths maps Norwegian month names (lowercase) to calendar months.
var nsmMonths = map[string]time.Month{
	"januar":    time.January,
	"februar":   time.February,
	"mars":      time.March,
	"april":     time.April,
	"mai":       time.May,
	"juni":      time.June,
	"juli":      time.July,
	"august":    time.August,
	"september": time.September,
	"oktober":   time.October,
	"november":  time.November,
	"desember":  time.December,
}

// nsmArchiveDateRe matches the articlelist__meta text "<day> <month> <year>".
var nsmArchiveDateRe = regexp.MustCompile(`^(\d{1,2})\s+([a-zæøå]+)\s+(\d{4})$`)

// nbspRe matches non-breaking spaces, which the archive uses inside meta
// and digest text; they must become plain spaces before date parsing.
var nbspRe = regexp.MustCompile("\u00a0")

// ParseNSMArchivePage parses one page of NSM's alert archive HTML listing
// into Records. Every <article> whose class list contains "list-view" is
// one alert: the link is the itemid attribute without its "#article"
// fragment (fallback: the first <a href>), the ID is the link's last path
// segment (the same rule as ParseNSMRSS, so archive IDs match feed IDs),
// the title is the h3[itemprop=headline] text, the summary is the
// div.digest text, and the date is the articlelist__meta text parsed as
// "<day> <norwegian month> <year>" at noon UTC, which keeps the same
// calendar day as Norway. CVEs come from title + summary. A list-view
// article missing its link, title or date, or carrying an unknown month,
// is an error, as is a page with zero list-view articles (the page
// format may have changed). Records are returned in page order.
func ParseNSMArchivePage(b []byte) ([]Record, error) {
	doc, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		return nil, locale.Errorf("parse NSM archive page: %v", err)
	}
	recs := make([]Record, 0, 10)
	for _, art := range findAllByTag(doc, "article") {
		if !hasClass(art, "list-view") {
			continue
		}
		link := archiveLink(art)
		if link == "" {
			return nil, locale.Errorf("NSM archive article missing link (title %q)", archiveTitle(art))
		}
		title := archiveTitle(art)
		if title == "" {
			return nil, locale.Errorf("NSM archive article missing headline (link %s)", link)
		}
		summary := archiveDigest(art)
		date, derr := parseNSMArchiveDate(archiveMeta(art))
		if derr != nil {
			return nil, locale.Errorf("parse NSM archive date for article %q: %v", title, derr)
		}
		recs = append(recs, Record{
			Source:  SourceNSM,
			ID:      nsmSlug(link),
			Title:   title,
			Date:    date,
			CVEs:    extractCVEs(title + " " + summary),
			Summary: summary,
			Link:    link,
		})
	}
	if len(recs) == 0 {
		return nil, locale.Errorf("no alerts found on the NSM archive page; the page format may have changed")
	}
	return recs, nil
}

// archiveLink returns the article's link: the itemid attribute with the
// "#article" fragment stripped, falling back to the first <a href>.
func archiveLink(art *html.Node) string {
	link := strings.TrimSuffix(attr(art, "itemid"), "#article")
	if link != "" {
		return link
	}
	if a := findByTag(art, "a"); a != nil {
		return attr(a, "href")
	}
	return ""
}

// archiveTitle returns the text of the h3[itemprop=headline] element.
func archiveTitle(art *html.Node) string {
	for _, h3 := range findAllByTag(art, "h3") {
		if attr(h3, "itemprop") == "headline" {
			return cleanNSMText(nodeText(h3))
		}
	}
	return ""
}

// archiveDigest returns the text of the div.digest element, whitespace
// collapsed and &nbsp; turned into a plain space.
func archiveDigest(art *html.Node) string {
	for _, d := range findAllByTag(art, "div") {
		if hasClass(d, "digest") {
			return cleanNSMText(nodeText(d))
		}
	}
	return ""
}

// archiveMeta returns the text of the div.articlelist__meta element.
func archiveMeta(art *html.Node) string {
	for _, d := range findAllByTag(art, "div") {
		if hasClass(d, "articlelist__meta") {
			return cleanNSMText(nodeText(d))
		}
	}
	return ""
}

// parseNSMArchiveDate parses "<day> <norwegian month> <year>" into noon
// UTC on that day, so the stored calendar day matches the Norwegian one.
func parseNSMArchiveDate(s string) (time.Time, error) {
	m := nsmArchiveDateRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, locale.Errorf("unrecognised date %q", s)
	}
	month, ok := nsmMonths[strings.ToLower(m[2])]
	if !ok {
		return time.Time{}, locale.Errorf("unknown Norwegian month %q", m[2])
	}
	day, err := strconv.Atoi(m[1])
	if err != nil {
		return time.Time{}, locale.Errorf("unrecognised day %q", m[1])
	}
	year, err := strconv.Atoi(m[3])
	if err != nil {
		return time.Time{}, locale.Errorf("unrecognised year %q", m[3])
	}
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC), nil
}

// attr returns the value of key on n, or "" when absent.
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// hasClass reports whether n's class attribute contains the class name.
func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

// findByTag returns the first descendant element with the given tag, or nil.
func findByTag(n *html.Node, tag string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return c
		}
		if found := findByTag(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// findAllByTag returns every descendant element with the given tag.
func findAllByTag(n *html.Node, tag string) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			out = append(out, c)
		}
		out = append(out, findAllByTag(c, tag)...)
	}
	return out
}

// nodeText concatenates the text content of n's subtree, turning
// non-breaking spaces into plain spaces so &nbsp; cannot hide inside a
// date or collapse into a different word boundary later.
func nodeText(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(nbspRe.ReplaceAllString(c.Data, " "))
		} else {
			b.WriteString(nodeText(c))
		}
	}
	return b.String()
}
