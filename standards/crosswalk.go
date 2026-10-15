package standards

import (
	"os"

	"github.com/Autumn-27/artex/locale"
	"gopkg.in/yaml.v3"
)

type crosswalkFile struct {
	Links []CrosswalkLink `yaml:"links"`
}

// LoadCrosswalk reads a crosswalk YAML file (shape: {links: [{from, to}, ...]})
// and returns its links. An empty or links-less file yields no links and no
// error. The returned links are not validated here; assign them to
// Catalog.Crosswalk and call Validate to reject dangling references.
func LoadCrosswalk(path string) ([]CrosswalkLink, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, locale.Errorf("read crosswalk: %w", err)
	}
	var f crosswalkFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, locale.Errorf("parse crosswalk: %w", err)
	}
	return f.Links, nil
}
