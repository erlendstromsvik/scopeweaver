package standards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// skillCovers mirrors the `covers:` block of a skill's SKILL.md frontmatter.
// A skill without a `covers:` key is skipped (api-recon and the like do not
// declare requirement coverage).
type skillCovers struct {
	Covers struct {
		ASVS []string `yaml:"asvs"`
		WSTG []string `yaml:"wstg"`
	} `yaml:"covers"`
}

// TestSkillsCoverage checks that every requirement ID declared in a skill's
// `covers:` frontmatter resolves against the embedded standards catalog, and
// that no ID is declared twice within a scheme. This keeps the coverage skills
// honest: a typo'd or nonexistent ASVS/WSTG ID fails the build.
func TestSkillsCoverage(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded(): %v", err)
	}

	matches, err := filepath.Glob("../skills/*/SKILL.md")
	if err != nil {
		t.Fatalf("glob skills: %v", err)
	}

	checked := 0
	for _, path := range matches {
		covers, ok := parseSkillCovers(t, path)
		if !ok {
			continue
		}
		checked++
		seen := map[string]bool{}
		checkCoverageIDs(t, c, SchemeASVS, covers.Covers.ASVS, path, seen)
		checkCoverageIDs(t, c, SchemeWSTG, covers.Covers.WSTG, path, seen)
	}

	if checked == 0 {
		t.Fatal("no skill with a `covers:` block was checked; test passed vacuously")
	}
}

// checkCoverageIDs asserts every id in ids resolves under scheme and has not
// been seen before in the same file. seen is keyed by "scheme:id" so the same
// string under two schemes is not a duplicate.
func checkCoverageIDs(t *testing.T, c *Catalog, scheme string, ids []string, path string, seen map[string]bool) {
	t.Helper()
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		key := scheme + ":" + id
		if seen[key] {
			t.Errorf("%s: duplicate %s id %q", path, scheme, id)
			continue
		}
		if !c.Resolve(scheme, id) {
			t.Errorf("%s: %s id %q does not resolve in the catalog", path, scheme, id)
		}
	}
}

// parseSkillCovers reads a SKILL.md and returns its covers block. ok is false
// when the file has no frontmatter or no `covers:` key, so the caller skips it.
func parseSkillCovers(t *testing.T, path string) (skillCovers, bool) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: read: %v", path, err)
	}
	fm, ok := frontmatter(string(body))
	if !ok {
		return skillCovers{}, false
	}
	var c skillCovers
	if err := yaml.Unmarshal([]byte(fm), &c); err != nil {
		t.Fatalf("%s: parse frontmatter: %v", path, err)
	}
	if len(c.Covers.ASVS) == 0 && len(c.Covers.WSTG) == 0 {
		return skillCovers{}, false
	}
	return c, true
}

// frontmatter returns the YAML between the first two `---` lines. ok is false
// when the file does not open with a `---` fence or has no closing fence.
func frontmatter(body string) (string, bool) {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[1:i], "\n"), true
		}
	}
	return "", false
}
