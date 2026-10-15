package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/artex/standards"
)

// Finding refs (db/finding_refs.go) are read and replaced here so every ref can
// be checked against the standards catalog compiled into the binary
// (standards.Embedded) before anything is written. The db store keeps only its
// lenient shape checks; the catalog lives at this layer.

// resolveFindingRef checks one classification ref against the embedded
// standards catalog. asvs/wstg/owasp-top10 must resolve as real catalog ids
// (owasp-top10 maps to the catalog's "top10"); cwe and cvss return nil because
// CWE is not embedded and a CVSS vector is not a catalog id — the db store's
// shape check covers those.
func resolveFindingRef(scheme, ref string) error {
	if !db.ValidRefScheme(scheme) {
		return locale.Errorf("unknown %s ref: %s", scheme, ref)
	}
	ref = strings.TrimSpace(ref)
	var cat *standards.Catalog
	var err error
	if cat, err = standards.Embedded(); err != nil {
		return err
	}
	switch scheme {
	case db.SchemeASVS, db.SchemeWSTG:
		if !cat.Resolve(scheme, ref) {
			return locale.Errorf("unknown %s ref: %s", scheme, ref)
		}
	case db.SchemeOWASPTop10:
		if !cat.Resolve(standards.SchemeTop10, ref) {
			return locale.Errorf("unknown %s ref: %s", scheme, ref)
		}
	}
	return nil
}

// listFindingRefs returns a finding's classification refs: {"refs":[{scheme,ref}]}.
func (s *Server) listFindingRefs(w http.ResponseWriter, r *http.Request) {
	id := int64(atoiDefault(r.PathValue("id"), 0))
	if id <= 0 {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad finding id"))
		return
	}
	f, err := s.m.pg.GetFinding(id)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if f == nil {
		writeErr(w, 404, locale.Text(responseLanguage(w), "finding not found"))
		return
	}
	refs, err := s.m.pg.ListFindingRefs(id)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	dtos := findingRefDTOs(refs)
	if dtos == nil {
		dtos = []FindingRefDTO{}
	}
	writeJSON(w, 200, map[string]any{"refs": dtos})
}

// setFindingRefs replaces a finding's classification refs with the given set and
// returns the stored list. Every ref is resolved against the catalog before
// anything is written, so an unknown ref leaves the previous refs untouched.
func (s *Server) setFindingRefs(w http.ResponseWriter, r *http.Request) {
	id := int64(atoiDefault(r.PathValue("id"), 0))
	if id <= 0 {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad finding id"))
		return
	}
	f, err := s.m.pg.GetFinding(id)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if f == nil {
		writeErr(w, 404, locale.Text(responseLanguage(w), "finding not found"))
		return
	}
	var body struct {
		Refs []FindingRefDTO `json:"refs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad json: ")+locale.ErrorMessage(responseLanguage(w), err))
		return
	}
	for _, ref := range body.Refs {
		if err := resolveFindingRef(ref.Scheme, ref.Ref); err != nil {
			writeErr(w, 400, locale.ErrorMessage(responseLanguage(w), err))
			return
		}
	}
	refs := make([]db.FindingRef, 0, len(body.Refs))
	for _, ref := range body.Refs {
		refs = append(refs, db.FindingRef{Scheme: ref.Scheme, Ref: ref.Ref})
	}
	if err := s.m.pg.SetFindingRefs(id, refs); err != nil {
		writeErr(w, 400, locale.ErrorMessage(responseLanguage(w), err))
		return
	}
	stored, err := s.m.pg.ListFindingRefs(id)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	dtos := findingRefDTOs(stored)
	if dtos == nil {
		dtos = []FindingRefDTO{}
	}
	writeJSON(w, 200, map[string]any{"refs": dtos})
}
