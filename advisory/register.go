package advisory

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/locale"
)

//go:embed register/nsm.jsonl
var nsmRegisterFS embed.FS

// RegisterEntry is one line of the append-only advisory register. Dates are
// formatted strings so the JSONL file is stable and diff-friendly; Record
// keeps them as time.Time.
type RegisterEntry struct {
	ID        string   `json:"id"`     // record ID (NSM: link slug)
	Source    string   `json:"source"` // "nsm"
	Title     string   `json:"title"`
	Date      string   `json:"date"` // publication date, "2006-01-02"
	Link      string   `json:"link"`
	CVEs      []string `json:"cves"` // always an array, never null
	Summary   string   `json:"summary"`
	FirstSeen string   `json:"first_seen"` // date the register first recorded it, "2006-01-02"
}

// ReadRegister parses a JSONL register: one JSON object per non-empty line,
// kept in file order. It rejects malformed lines, lines without an id, and
// duplicate ids, so an append-only file cannot silently lose or shadow a
// record.
func ReadRegister(r io.Reader) ([]RegisterEntry, error) {
	var (
		entries []RegisterEntry
		seen    = map[string]bool{}
		lineNo  int
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e RegisterEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, locale.Errorf("register line %d: %v", lineNo, err)
		}
		if e.ID == "" {
			return nil, locale.Errorf("register line %d: missing id", lineNo)
		}
		if seen[e.ID] {
			return nil, locale.Errorf("register line %d: duplicate id %s", lineNo, e.ID)
		}
		seen[e.ID] = true
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, locale.Errorf("read register: %v", err)
	}
	return entries, nil
}

// MergeRegister returns only the incoming records that existing does not
// already hold: records whose ID is absent from existing and not repeated
// within incoming. New entries carry FirstSeen = seen's UTC date and a
// non-nil CVEs slice; the Date is the record's own calendar date (RSS
// dates carry +0200/+0100 offsets and archive dates are noon UTC, so a
// UTC conversion would shift late-night Norwegian alerts to the previous
// day), ordered oldest first (Date asc, then ID) so appending
// keeps the register roughly chronological. existing is not modified.
func MergeRegister(existing []RegisterEntry, incoming []Record, seen time.Time) []RegisterEntry {
	have := make(map[string]bool, len(existing))
	for _, e := range existing {
		have[e.ID] = true
	}
	firstSeen := seen.UTC().Format("2006-01-02")
	out := make([]RegisterEntry, 0, len(incoming))
	for _, rec := range incoming {
		if rec.ID == "" || have[rec.ID] {
			continue
		}
		have[rec.ID] = true
		cves := rec.CVEs
		if cves == nil {
			cves = []string{}
		}
		out = append(out, RegisterEntry{
			ID:        rec.ID,
			Source:    rec.Source,
			Title:     rec.Title,
			Date:      rec.Date.Format("2006-01-02"),
			Link:      rec.Link,
			CVEs:      cves,
			Summary:   rec.Summary,
			FirstSeen: firstSeen,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// AppendRegister writes entries as one compact JSON object per line, with
// no indentation and no HTML escaping, so titles containing "&" or "<"
// survive a round trip through the register file.
func AppendRegister(w io.Writer, entries []RegisterEntry) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return locale.Errorf("append register entry %s: %v", e.ID, err)
		}
	}
	return nil
}

var (
	embeddedNSMOnce     sync.Once
	embeddedNSMEntries  []RegisterEntry
	embeddedNSMRegister error
)

// EmbeddedNSMRegister parses the register embedded in the binary once. The
// file is seeded by a backfill and appended by the host-side sync; an empty
// file yields zero entries and no error.
func EmbeddedNSMRegister() ([]RegisterEntry, error) {
	embeddedNSMOnce.Do(func() {
		raw, err := nsmRegisterFS.ReadFile("register/nsm.jsonl")
		if err != nil {
			embeddedNSMRegister = locale.Errorf("read embedded NSM register: %v", err)
			return
		}
		embeddedNSMEntries, embeddedNSMRegister = ReadRegister(bytes.NewReader(raw))
	})
	return embeddedNSMEntries, embeddedNSMRegister
}
