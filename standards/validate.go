package standards

import (
	"errors"
	"regexp"
	"sort"

	"github.com/Autumn-27/artex/locale"
)

var (
	wstgIDPattern  = regexp.MustCompile(`^WSTG-[A-Z]+-\d+$`)
	top10IDPattern = regexp.MustCompile(`^A\d{2}:2025$`)
)

// Validate reports every structural problem it finds, aggregated into a single
// error (via errors.Join) so callers see all of them at once. It rejects:
// duplicate ids within a scheme, ASVS levels outside 1..3, WSTG ids not
// matching ^WSTG-[A-Z]+-\d+$, Top 10 ids not matching ^A\d{2}:2025$, and
// crosswalk links whose From/To ref does not resolve in its catalog. A valid
// catalog (including one with an empty crosswalk) returns nil.
func (c *Catalog) Validate() error {
	var problems []error

	for _, d := range c.dups {
		problems = append(problems, locale.Errorf("duplicate %s id %q", d.Scheme, d.ID))
	}

	for id, r := range c.ASVS {
		if r.Level < 1 || r.Level > 3 {
			problems = append(problems, locale.Errorf("ASVS requirement %q has level %d outside 1..3", id, int(r.Level)))
		}
	}

	for id := range c.WSTG {
		if !wstgIDPattern.MatchString(id) {
			problems = append(problems, locale.Errorf("WSTG id %q does not match %s", id, wstgIDPattern.String()))
		}
	}

	for id := range c.Top10 {
		if !top10IDPattern.MatchString(id) {
			problems = append(problems, locale.Errorf("Top 10 id %q does not match %s", id, top10IDPattern.String()))
		}
	}

	for _, link := range c.Crosswalk {
		if !c.Resolve(link.From.Scheme, link.From.ID) {
			problems = append(problems, locale.Errorf("crosswalk link has dangling From ref %s:%s", link.From.Scheme, link.From.ID))
		}
		if !c.Resolve(link.To.Scheme, link.To.ID) {
			problems = append(problems, locale.Errorf("crosswalk link has dangling To ref %s:%s", link.To.Scheme, link.To.ID))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	// Sort for deterministic output; map iteration order is otherwise random.
	sort.Slice(problems, func(i, j int) bool {
		return problems[i].Error() < problems[j].Error()
	})
	return errors.Join(problems...)
}
