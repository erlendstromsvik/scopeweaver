package db

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/artex/locale"
)

// FindingRef is a row in finding_refs: one structured classification ref for a
// finding. A finding may carry many refs across schemes; findings.vulnclass stays
// free text for compatibility.
type FindingRef struct {
	ID        int64
	FindingID int64
	Scheme    string // one of the classification schemes below
	Ref       string // e.g. 'CWE-79', 'V1.1.1', 'WSTG-INFO-01', 'A05:2025', a CVSS vector
	CreatedAt time.Time
}

// Classification schemes (finding_refs.scheme). Validated in Go (no schema CHECK)
// so adding a scheme needs no migration.
const (
	SchemeCWE        = "cwe"         // CWE weakness, e.g. CWE-79.
	SchemeASVS       = "asvs"        // OWASP ASVS requirement, e.g. V1.1.1.
	SchemeWSTG       = "wstg"        // OWASP WSTG test, e.g. WSTG-INFO-01.
	SchemeOWASPTop10 = "owasp-top10" // OWASP Top 10 category, e.g. A05:2025.
	SchemeCVSS       = "cvss"        // CVSS vector string.
)

// ValidRefScheme reports whether s is a known classification scheme.
func ValidRefScheme(s string) bool {
	switch s {
	case SchemeCWE, SchemeASVS, SchemeWSTG, SchemeOWASPTop10, SchemeCVSS:
		return true
	}
	return false
}

// Lenient per-scheme shape checks. These catch obvious typos without pretending
// to resolve the ref against a catalog.
var (
	reCWE        = regexp.MustCompile(`^CWE-\d+$`)
	reOWASPTop10 = regexp.MustCompile(`^A\d{2}:2025$`)
)

// validateFindingRef validates the scheme and ref and returns the trimmed ref.
// Shape checks stay deliberately lenient.
//
// Catalog resolution happens at the server layer (server/finding_refs_api.go).
func validateFindingRef(scheme, ref string) (string, error) {
	if !ValidRefScheme(scheme) {
		return "", locale.Errorf("unknown finding ref scheme %q", scheme)
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", locale.Errorf("finding ref requires a non-empty ref")
	}
	switch scheme {
	case SchemeCWE:
		if !reCWE.MatchString(ref) {
			return "", locale.Errorf("cwe ref %q must look like CWE-79", ref)
		}
	case SchemeOWASPTop10:
		if !reOWASPTop10.MatchString(ref) {
			return "", locale.Errorf("owasp-top10 ref %q must look like A05:2025", ref)
		}
	}
	return ref, nil
}

// findingRefSelectCols is the column list every finding_refs query selects, so
// scanFindingRefs stays in sync across callers.
const findingRefSelectCols = `id, finding_id, scheme, ref, created_at`

// scanFindingRefs materializes rows selected via findingRefSelectCols.
func scanFindingRefs(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*FindingRef, error) {
	var out []*FindingRef
	for rows.Next() {
		r := &FindingRef{}
		if err := rows.Scan(&r.ID, &r.FindingID, &r.Scheme, &r.Ref, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddFindingRef validates and inserts one classification ref for a finding. It is
// idempotent on (finding_id, scheme, ref): a duplicate add is a no-op that returns
// the id of the row already present.
func (d *DB) AddFindingRef(findingID int64, scheme, ref string) (int64, error) {
	if findingID <= 0 {
		return 0, locale.Errorf("finding ref requires a finding id")
	}
	ref, err := validateFindingRef(scheme, ref)
	if err != nil {
		return 0, err
	}
	var id int64
	err = d.QueryRow(`
		INSERT INTO finding_refs (finding_id, scheme, ref)
		VALUES ($1, $2, $3)
		ON CONFLICT (finding_id, scheme, ref) DO NOTHING
		RETURNING id`, findingID, scheme, ref).Scan(&id)
	if err == sql.ErrNoRows {
		// Already present: DO NOTHING returned no row, so fetch the existing id.
		err = d.QueryRow(`
			SELECT id FROM finding_refs
			WHERE finding_id = $1 AND scheme = $2 AND ref = $3`,
			findingID, scheme, ref).Scan(&id)
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListFindingRefs returns every ref for a finding, ordered by scheme then ref.
func (d *DB) ListFindingRefs(findingID int64) ([]*FindingRef, error) {
	rows, err := d.Query(`
		SELECT `+findingRefSelectCols+`
		FROM finding_refs
		WHERE finding_id = $1
		ORDER BY scheme, ref`, findingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFindingRefs(rows)
}

// listFindingRefsFor loads refs for many findings in one query, keyed by
// finding id. Within each finding the refs are ordered by scheme then ref,
// matching ListFindingRefs. Findings with no refs have no map entry.
func (d *DB) listFindingRefsFor(ids []int64) (map[int64][]*FindingRef, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := d.Query(`
		SELECT `+findingRefSelectCols+`
		FROM finding_refs
		WHERE finding_id IN (`+strings.Join(ph, ",")+`)
		ORDER BY finding_id, scheme, ref`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs, err := scanFindingRefs(rows)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]*FindingRef)
	for _, r := range refs {
		out[r.FindingID] = append(out[r.FindingID], r)
	}
	return out, nil
}

// SetFindingRefs replaces all refs for a finding with the given set, in one tx.
// Every ref is validated before anything is written; duplicates in the input
// collapse to one row via the unique index.
func (d *DB) SetFindingRefs(findingID int64, refs []FindingRef) error {
	if findingID <= 0 {
		return locale.Errorf("finding ref requires a finding id")
	}
	type cleanRef struct{ scheme, ref string }
	cleaned := make([]cleanRef, 0, len(refs))
	for _, r := range refs {
		ref, err := validateFindingRef(r.Scheme, r.Ref)
		if err != nil {
			return err
		}
		cleaned = append(cleaned, cleanRef{r.Scheme, ref})
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM finding_refs WHERE finding_id = $1`, findingID); err != nil {
		return err
	}
	for _, r := range cleaned {
		if _, err := tx.Exec(`
			INSERT INTO finding_refs (finding_id, scheme, ref)
			VALUES ($1, $2, $3)
			ON CONFLICT (finding_id, scheme, ref) DO NOTHING`,
			findingID, r.scheme, r.ref); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteFindingRef removes one ref by id. Deleting a missing id is a no-op.
func (d *DB) DeleteFindingRef(id int64) error {
	_, err := d.Exec(`DELETE FROM finding_refs WHERE id = $1`, id)
	return err
}
