//go:build ignore

// Command gen_crosswalk regenerates standards/crosswalk.yaml by deriving the
// advisory cross-scheme crosswalk (ASVS 5.0 -> CWE and WSTG -> CWE) from the
// vendored OpenCRE link files and the OWASP ASVS 5.0 -> 4.0.3 bridge. It is run
// via `go generate ./standards/`. The output is advisory, not authoritative
// (see roadmap task E-D5): ASVS 5.0 has no native CWE mapping, so every link
// carries its derivation source and a confidence grade.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Autumn-27/artex/standards"
	"gopkg.in/yaml.v3"
)

const (
	sourcesDir = "sources"
	outPath    = "crosswalk.yaml"
)

// crosswalkFile mirrors the on-disk shape consumed by standards.LoadCrosswalk.
type crosswalkFile struct {
	Links []standards.CrosswalkLink `yaml:"links"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen_crosswalk:", err)
		os.Exit(1)
	}
}

func run() error {
	cat, warnings, err := standards.Load(sourcesDir)
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "gen_crosswalk: load warning:", w)
	}

	links, dwarn, err := standards.DeriveCrosswalk(sourcesDir, cat)
	if err != nil {
		return fmt.Errorf("derive crosswalk: %w", err)
	}
	for _, w := range dwarn {
		fmt.Fprintln(os.Stderr, "gen_crosswalk: derive warning:", w)
	}

	// Fail loudly if the derived file would not validate, rather than writing a
	// broken crosswalk.
	cat.Crosswalk = links
	if err := cat.Validate(); err != nil {
		return fmt.Errorf("validate derived crosswalk: %w", err)
	}

	body, err := yaml.Marshal(crosswalkFile{Links: links})
	if err != nil {
		return fmt.Errorf("marshal crosswalk: %w", err)
	}

	header := fmt.Sprintf(`# Cross-scheme crosswalk for the standards catalog.
#
# GENERATED FILE — do not edit by hand. Regenerate with:
#   go generate ./standards/
#
# These links are ADVISORY, not authoritative (roadmap task E-D5). OWASP ASVS
# 5.0 removed the per-requirement CWE mapping, so ASVS 5.0 -> CWE links are
# derived indirectly: ASVS5 ->(OWASP 5.0->4.0.3 bridge)-> ASVS4.0.3
# ->(OpenCRE/CRE)-> CWE, stamped source "opencre+asvs4.0.3-bridge",
# confidence "low". WSTG -> CWE links are a single OpenCRE/CRE hop, stamped
# source "opencre", confidence "medium". Every ref resolves in the catalog.
#
# Sources: standards/sources/opencre/*.links.json and
# standards/sources/asvs/mappings/mapping_v5.0.0_to_v4.0.3.yml.
# Snapshot generated: %s. Link count: %d.
`, time.Now().UTC().Format("2006-01-02"), len(links))

	out := append([]byte(header), body...)
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	fmt.Printf("gen_crosswalk: wrote %d links to %s\n", len(links), outPath)
	return nil
}
