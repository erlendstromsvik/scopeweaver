package report

import (
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
)

// refRows parses a FindingsCSV export, stripping the UTF-8 BOM.
func refRows(t *testing.T, out []byte) [][]string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(out), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	return rows
}

// TestFindingsCSVRefColumns checks the five trailing classification columns and
// that each scheme's refs are joined with "; " (empty when none).
func TestFindingsCSVRefColumns(t *testing.T) {
	f := &db.DBFinding{
		ID: 1, Name: "XSS", Severity: "high", Status: "pending",
		CreatedAt: time.Unix(0, 0),
		Refs: []*db.FindingRef{
			{FindingID: 1, Scheme: db.SchemeCWE, Ref: "CWE-79"},
			{FindingID: 1, Scheme: db.SchemeCWE, Ref: "CWE-89"},
			{FindingID: 1, Scheme: db.SchemeASVS, Ref: "V5.3.3"},
			{FindingID: 1, Scheme: db.SchemeOWASPTop10, Ref: "A05:2025"},
		},
	}
	rows := refRows(t, FindingsCSV([]*db.DBFinding{f}, locale.En))
	if len(rows) != 2 {
		t.Fatalf("want header + 1 row, got %d lines", len(rows))
	}
	hdr := rows[0]
	wantCols := []string{"CWE", "ASVS", "WSTG", "OWASP Top 10", "CVSS"}
	if len(hdr) < 5 {
		t.Fatalf("header too short: %v", hdr)
	}
	for i, want := range wantCols {
		if got := hdr[len(hdr)-5+i]; got != want {
			t.Errorf("trailing header column %d = %q, want %q", i, got, want)
		}
	}
	row := rows[1]
	if len(row) != len(hdr) {
		t.Fatalf("row has %d cells, header has %d", len(row), len(hdr))
	}
	want := []string{"CWE-79; CWE-89", "V5.3.3", "", "A05:2025", ""}
	for i, w := range want {
		if got := row[len(row)-5+i]; got != w {
			t.Errorf("ref column %d (%s) = %q, want %q", i, wantCols[i], got, w)
		}
	}
}

// TestFindingsCSVNoRefsEmptyCells checks a finding without refs still gets five
// empty trailing cells.
func TestFindingsCSVNoRefsEmptyCells(t *testing.T) {
	f := &db.DBFinding{ID: 2, Name: "Info leak", Severity: "low", Status: "pending", CreatedAt: time.Unix(0, 0)}
	rows := refRows(t, FindingsCSV([]*db.DBFinding{f}, locale.En))
	if len(rows) != 2 {
		t.Fatalf("want header + 1 row, got %d lines", len(rows))
	}
	row := rows[1]
	if len(row) < 5 {
		t.Fatalf("row too short: %v", row)
	}
	for i, got := range row[len(row)-5:] {
		if got != "" {
			t.Errorf("empty-refs column %d = %q, want empty", i, got)
		}
	}
}
