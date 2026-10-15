package advisory

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// sameEntry compares two register entries field by field; the struct holds a
// slice, so it is not directly comparable.
func sameEntry(a, b RegisterEntry) bool {
	return a.ID == b.ID && a.Source == b.Source && a.Title == b.Title &&
		a.Date == b.Date && a.Link == b.Link && a.Summary == b.Summary &&
		a.FirstSeen == b.FirstSeen && strings.Join(a.CVEs, ",") == strings.Join(b.CVEs, ",")
}

func entry(id, date string) RegisterEntry {
	return RegisterEntry{
		ID:        id,
		Source:    SourceNSM,
		Title:     "title " + id,
		Date:      date,
		Link:      "https://nsm.no/" + id,
		CVEs:      []string{},
		Summary:   "summary " + id,
		FirstSeen: "2026-10-15",
	}
}

func TestRegisterRoundTrip(t *testing.T) {
	in := []RegisterEntry{
		entry("a", "2026-10-01"),
		entry("b", "2026-10-02"),
		{ID: "amp", Source: SourceNSM, Title: `Acme & Co <remote code>`, Date: "2026-10-03",
			Link: "https://nsm.no/amp", CVEs: []string{"CVE-2026-0001"}, Summary: "a & b <c>", FirstSeen: "2026-10-15"},
	}
	var buf bytes.Buffer
	if err := AppendRegister(&buf, in); err != nil {
		t.Fatalf("AppendRegister: %v", err)
	}
	out := buf.String()
	if strings.HasSuffix(out, "\n\n") || strings.Contains(out, "\n\n") {
		t.Fatalf("output has a blank line: %q", out)
	}
	if lines := strings.Split(strings.TrimRight(out, "\n"), "\n"); len(lines) != len(in) {
		t.Fatalf("lines = %d, want %d\n%s", len(lines), len(in), out)
	}
	if !strings.Contains(out, "&") || strings.Contains(out, `&amp;`) {
		t.Errorf("ampersand was escaped: %s", out)
	}
	if !strings.Contains(out, "<") || strings.Contains(out, `&lt;`) {
		t.Errorf("angle bracket was escaped: %s", out)
	}
	if strings.Contains(out, "  ") {
		t.Errorf("output is indented: %s", out)
	}
	got, err := ReadRegister(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadRegister: %v", err)
	}
	if len(got) != len(in) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(in))
	}
	for i := range in {
		if !sameEntry(got[i], in[i]) {
			t.Errorf("entry %d round trip: got %+v, want %+v", i, got[i], in[i])
		}
	}
}

func TestReadRegisterBlankLinesIgnored(t *testing.T) {
	src := "\n" + `{"id":"a","source":"nsm","cves":[]}` + "\n\n   \n" + `{"id":"b","source":"nsm","cves":[]}` + "\n\n"
	got, err := ReadRegister(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ReadRegister: %v", err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("entries = %+v, want a then b", got)
	}
}

func TestReadRegisterMalformedLineReportsLineNumber(t *testing.T) {
	src := `{"id":"a","source":"nsm"}` + "\n" + `{"id":"b", oops}` + "\n"
	_, err := ReadRegister(strings.NewReader(src))
	if err == nil {
		t.Fatal("malformed line must error")
	}
	if !strings.Contains(err.Error(), "register line 2") {
		t.Errorf("error %q must name line 2", err)
	}
}

func TestReadRegisterMissingID(t *testing.T) {
	src := `{"id":"a","source":"nsm"}` + "\n" + `{"source":"nsm","title":"no id"}` + "\n"
	_, err := ReadRegister(strings.NewReader(src))
	if err == nil {
		t.Fatal("missing id must error")
	}
	if !strings.Contains(err.Error(), "register line 2: missing id") {
		t.Errorf("error %q must report missing id on line 2", err)
	}
}

func TestReadRegisterDuplicateID(t *testing.T) {
	src := `{"id":"a","source":"nsm"}` + "\n" + `{"id":"b","source":"nsm"}` + "\n" + `{"id":"a","source":"nsm"}` + "\n"
	_, err := ReadRegister(strings.NewReader(src))
	if err == nil {
		t.Fatal("duplicate id must error")
	}
	if !strings.Contains(err.Error(), "register line 3: duplicate id a") {
		t.Errorf("error %q must report duplicate id a on line 3", err)
	}
}

func TestMergeRegisterReturnsOnlyNewEntriesOldestFirst(t *testing.T) {
	existing := []RegisterEntry{entry("a", "2026-09-01")}
	incoming := []Record{
		{Source: SourceNSM, ID: "c", Title: "C", Date: time.Date(2026, 10, 3, 8, 0, 0, 0, time.FixedZone("CEST", 2*3600)), Link: "https://nsm.no/c"},
		{Source: SourceNSM, ID: "a", Title: "A", Date: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Link: "https://nsm.no/a"},
		{Source: SourceNSM, ID: "b", Title: "B", Date: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Link: "https://nsm.no/b", CVEs: []string{"CVE-2026-1111"}},
		{Source: SourceNSM, ID: "b", Title: "B dup", Date: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Link: "https://nsm.no/b"},
	}
	seen := time.Date(2026, 10, 15, 23, 30, 0, 0, time.FixedZone("AEST", 10*3600))
	got := MergeRegister(existing, incoming, seen)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("order = %s, %s; want b, c (oldest first)", got[0].ID, got[1].ID)
	}
	for _, e := range got {
		if e.FirstSeen != "2026-10-15" {
			t.Errorf("%s FirstSeen = %q, want 2026-10-15 (seen in UTC)", e.ID, e.FirstSeen)
		}
		if e.CVEs == nil {
			t.Errorf("%s CVEs = nil, want non-nil", e.ID)
		}
	}
	if got[0].CVEs[0] != "CVE-2026-1111" {
		t.Errorf("b CVEs = %v, want [CVE-2026-1111]", got[0].CVEs)
	}
	if got[1].Date != "2026-10-03" {
		t.Errorf("c Date = %q, want 2026-10-03 (record's own calendar day)", got[1].Date)
	}
	// A record with no CVEs must still marshal as an empty array, not null.
	var buf bytes.Buffer
	if err := AppendRegister(&buf, got[:1]); err != nil {
		t.Fatalf("AppendRegister: %v", err)
	}
	empty := RegisterEntry{ID: "nocve", Source: SourceNSM, Title: "no cves", Date: "2026-10-04", Link: "https://nsm.no/nocve", FirstSeen: "2026-10-15"}
	merged := MergeRegister(nil, []Record{{Source: SourceNSM, ID: "nocve", Title: empty.Title, Date: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), Link: empty.Link}}, seen)
	if merged[0].CVEs == nil {
		t.Fatal("CVEs must be non-nil for a record without CVEs")
	}
	buf.Reset()
	if err := AppendRegister(&buf, merged); err != nil {
		t.Fatalf("AppendRegister: %v", err)
	}
	if strings.Contains(buf.String(), `"cves":null`) {
		t.Errorf("cves serialized null: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"cves":[]`) {
		t.Errorf("cves must be an empty array: %s", buf.String())
	}
}

func TestMergeRegisterNoNewEntries(t *testing.T) {
	existing := []RegisterEntry{entry("a", "2026-09-01"), entry("b", "2026-09-02")}
	got := MergeRegister(existing, []Record{{Source: SourceNSM, ID: "a"}, {Source: SourceNSM, ID: "b"}}, time.Unix(0, 0))
	if len(got) != 0 {
		t.Fatalf("got %d entries, want 0", len(got))
	}
}

func TestEmbeddedNSMRegisterEmpty(t *testing.T) {
	got, err := EmbeddedNSMRegister()
	if err != nil {
		t.Fatalf("EmbeddedNSMRegister: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0 (register is seeded in E.7d): %+v", len(got), got)
	}
	raw, err := os.ReadFile("register/nsm.jsonl")
	if err != nil {
		t.Fatalf("read register file: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("register/nsm.jsonl is %d bytes, want 0", len(raw))
	}
	// Second call must reuse the cached parse, not re-read or error.
	again, err := EmbeddedNSMRegister()
	if err != nil || len(again) != 0 {
		t.Fatalf("second call: %d entries, err %v", len(again), err)
	}
}

func TestNSMRSSIntoRegisterEndToEnd(t *testing.T) {
	b, err := os.ReadFile(vendoredNSM)
	if err != nil {
		t.Fatalf("read vendored NSM feed: %v", err)
	}
	recs, err := ParseNSMRSS(b)
	if err != nil {
		t.Fatalf("ParseNSMRSS: %v", err)
	}
	seen := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	merged := MergeRegister(nil, recs, seen)
	if len(merged) != 10 {
		t.Fatalf("len(merged) = %d, want 10", len(merged))
	}
	var buf bytes.Buffer
	if err := AppendRegister(&buf, merged); err != nil {
		t.Fatalf("AppendRegister: %v", err)
	}
	back, err := ReadRegister(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadRegister: %v", err)
	}
	if len(back) != 10 {
		t.Fatalf("re-read %d entries, want 10", len(back))
	}
	for i := range merged {
		if !sameEntry(back[i], merged[i]) {
			t.Errorf("entry %d: got %+v, want %+v", i, back[i], merged[i])
		}
	}
	// Merging the same feed again adds nothing.
	if again := MergeRegister(back, recs, seen); len(again) != 0 {
		t.Fatalf("second merge added %d entries, want 0", len(again))
	}
	// Oldest first: dates are non-decreasing.
	for i := 1; i < len(back); i++ {
		if back[i].Date < back[i-1].Date {
			t.Errorf("out of order at %d: %s before %s", i, back[i-1].Date, back[i].Date)
		}
	}
}
