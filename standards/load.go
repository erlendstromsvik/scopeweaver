package standards

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/locale"
	"gopkg.in/yaml.v3"
)

// Source file locations relative to the sources directory.
const (
	asvsPath  = "asvs/OWASP_ASVS_5.0.0_en.flat.json"
	wstgPath  = "wstg/checklist.json"
	top10Path = "owasp-top10/top10-2025-categories.yaml"
	cwePath   = "cwe/cwec_v4.20.xml.zip"
)

// Load reads the vendored standard sources from sourcesDir (typically
// "standards/sources") and returns the assembled catalog. The returned
// warnings are non-fatal notes (currently duplicate identifiers found while
// parsing); a non-nil error means a source could not be read or parsed. Load
// does not validate semantics — call Validate for that. The Crosswalk is left
// empty; populate it via LoadCrosswalk before validating crosswalk links.
func Load(sourcesDir string) (*Catalog, []string, error) {
	c := &Catalog{}

	asvsBytes, err := os.ReadFile(filepath.Join(sourcesDir, asvsPath))
	if err != nil {
		return nil, nil, locale.Errorf("read ASVS source: %w", err)
	}
	asvs, asvsDups, err := parseASVS(asvsBytes)
	if err != nil {
		return nil, nil, locale.Errorf("parse ASVS source: %w", err)
	}
	c.ASVS = asvs

	wstgBytes, err := os.ReadFile(filepath.Join(sourcesDir, wstgPath))
	if err != nil {
		return nil, nil, locale.Errorf("read WSTG source: %w", err)
	}
	wstg, wstgDups, err := parseWSTG(wstgBytes)
	if err != nil {
		return nil, nil, locale.Errorf("parse WSTG source: %w", err)
	}
	c.WSTG = wstg

	top10Bytes, err := os.ReadFile(filepath.Join(sourcesDir, top10Path))
	if err != nil {
		return nil, nil, locale.Errorf("read Top 10 source: %w", err)
	}
	top10, top10Dups, err := parseTop10(top10Bytes)
	if err != nil {
		return nil, nil, locale.Errorf("parse Top 10 source: %w", err)
	}
	c.Top10 = top10

	cwe, cweDups, err := loadCWE(filepath.Join(sourcesDir, cwePath))
	if err != nil {
		return nil, nil, locale.Errorf("load CWE source: %w", err)
	}
	c.CWE = cwe

	c.dups = append(c.dups, asvsDups...)
	c.dups = append(c.dups, wstgDups...)
	c.dups = append(c.dups, top10Dups...)
	c.dups = append(c.dups, cweDups...)

	var warnings []string
	for _, d := range c.dups {
		warnings = append(warnings, "duplicate "+d.Scheme+" id "+d.ID)
	}
	return c, warnings, nil
}

// --- ASVS ---

type asvsFile struct {
	Requirements []struct {
		ChapterID   string `json:"chapter_id"`
		ChapterName string `json:"chapter_name"`
		SectionID   string `json:"section_id"`
		SectionName string `json:"section_name"`
		ReqID       string `json:"req_id"`
		ReqDesc     string `json:"req_description"`
		L           string `json:"L"`
	} `json:"requirements"`
}

func parseASVS(b []byte) (map[string]ASVSRequirement, []Ref, error) {
	var f asvsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, nil, err
	}
	out := make(map[string]ASVSRequirement, len(f.Requirements))
	var dups []Ref
	for _, r := range f.Requirements {
		// An unparseable level becomes 0 so Validate flags it as out of range.
		level, _ := strconv.Atoi(strings.TrimSpace(r.L))
		if _, seen := out[r.ReqID]; seen {
			dups = append(dups, Ref{Scheme: SchemeASVS, ID: r.ReqID})
			continue
		}
		out[r.ReqID] = ASVSRequirement{
			ID:          r.ReqID,
			Chapter:     r.ChapterName,
			Section:     r.SectionName,
			Description: r.ReqDesc,
			Level:       Level(level),
		}
	}
	return out, dups, nil
}

// --- WSTG ---

type wstgFile struct {
	Categories map[string]struct {
		ID    string `json:"id"`
		Tests []struct {
			Name       string   `json:"name"`
			ID         string   `json:"id"`
			Reference  string   `json:"reference"`
			Objectives []string `json:"objectives"`
		} `json:"tests"`
	} `json:"categories"`
}

func parseWSTG(b []byte) (map[string]WSTGTest, []Ref, error) {
	var f wstgFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, nil, err
	}
	out := make(map[string]WSTGTest)
	var dups []Ref
	for category, cat := range f.Categories {
		for _, t := range cat.Tests {
			if _, seen := out[t.ID]; seen {
				dups = append(dups, Ref{Scheme: SchemeWSTG, ID: t.ID})
				continue
			}
			out[t.ID] = WSTGTest{
				ID:         t.ID,
				Category:   category,
				Name:       t.Name,
				Reference:  t.Reference,
				Objectives: t.Objectives,
			}
		}
	}
	return out, dups, nil
}

// --- Top 10 ---

type top10File struct {
	Version    string `yaml:"version"`
	Source     string `yaml:"source"`
	Categories []struct {
		ID    string `yaml:"id"`
		Title string `yaml:"title"`
	} `yaml:"categories"`
}

func parseTop10(b []byte) (map[string]Top10Category, []Ref, error) {
	var f top10File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, nil, err
	}
	out := make(map[string]Top10Category, len(f.Categories))
	var dups []Ref
	for _, cat := range f.Categories {
		if _, seen := out[cat.ID]; seen {
			dups = append(dups, Ref{Scheme: SchemeTop10, ID: cat.ID})
			continue
		}
		out[cat.ID] = Top10Category{ID: cat.ID, Title: cat.Title}
	}
	return out, dups, nil
}

// --- CWE ---

// loadCWE opens the zipped CWE XML catalog and streams it, collecting Weakness
// id->name pairs. The archive is read in place (archive/zip over the file); it
// is never unzipped to disk.
func loadCWE(zipPath string) (map[int]Weakness, []Ref, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return nil, nil, err
	}
	var xmlFile *zip.File
	for _, zf := range zr.File {
		if strings.HasSuffix(strings.ToLower(zf.Name), ".xml") {
			xmlFile = zf
			break
		}
	}
	if xmlFile == nil {
		return nil, nil, locale.Errorf("CWE archive contains no .xml entry")
	}
	rc, err := xmlFile.Open()
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()
	return parseCWE(rc)
}

// parseCWE streams the CWE XML and extracts <Weakness ID Name> elements. It
// matches on the element's local name, so the document's default namespace is
// irrelevant, and it ignores <Category> and <View> elements.
func parseCWE(r io.Reader) (map[int]Weakness, []Ref, error) {
	dec := xml.NewDecoder(r)
	out := make(map[int]Weakness)
	var dups []Ref
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Weakness" {
			continue
		}
		var (
			idStr, name string
			haveID      bool
		)
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "ID":
				idStr = a.Value
				haveID = true
			case "Name":
				name = a.Value
			}
		}
		// Skip the element's children so the decoder stays cheap and we never
		// treat nested elements as top-level Weaknesses.
		if err := dec.Skip(); err != nil {
			return nil, nil, err
		}
		if !haveID {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(idStr))
		if err != nil {
			continue
		}
		if _, seen := out[id]; seen {
			dups = append(dups, Ref{Scheme: SchemeCWE, ID: strconv.Itoa(id)})
			continue
		}
		out[id] = Weakness{ID: id, Name: name}
	}
	return out, dups, nil
}
