package advisory

import (
	"sort"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// Match is one candidate pairing of an advisory record with one technology
// string on one asset. It is a candidate for a human/agent check, never a
// verdict.
type Match struct {
	Record     Record
	AssetID    int64
	Technology string
}

// normalize lowercases s and keeps only runs of [a-z0-9] as tokens.
func normalize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
}

// MatchAssets finds candidate (record, asset, technology) pairings. An asset
// technology matches a record when ALL tokens of the record's Product appear
// among the technology's tokens, and additionally one of:
//   - the record's Vendor tokens appear in the technology, or
//   - the product has >= 2 tokens, or
//   - the single product token is at least 5 chars.
//
// Known imprecision (deliberate, keep simple): the rule is token-set based,
// so short single-token products can over- or under-match (e.g. product
// "Edge" only matches when the vendor appears or the token is >= 5 chars;
// "IOS"/"iOS" normalize to the same token "ios", so case-insensitive
// collisions like "ios" vs an unrelated "iOS app" string are possible).
// These matches are candidates for a human/agent check, never verdicts.
func MatchAssets(recs []Record, assets []*db.Asset) []Match {
	var out []Match
	for _, rec := range recs {
		prodTokens := normalize(rec.Product)
		if len(prodTokens) == 0 {
			continue
		}
		vendorTokens := normalize(rec.Vendor)
		for _, asset := range assets {
			if asset == nil || len(asset.Technologies) == 0 {
				continue
			}
			for _, tech := range asset.Technologies {
				techTokens := normalize(tech)
				if len(techTokens) == 0 {
					continue
				}
				set := make(map[string]bool, len(techTokens))
				for _, t := range techTokens {
					set[t] = true
				}
				if !allIn(prodTokens, set) {
					continue
				}
				vendorOK := len(vendorTokens) > 0 && allIn(vendorTokens, set)
				if vendorOK || len(prodTokens) >= 2 || (len(prodTokens) == 1 && len(prodTokens[0]) >= 5) {
					out = append(out, Match{Record: rec, AssetID: asset.ID, Technology: tech})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].AssetID != out[j].AssetID {
			return out[i].AssetID < out[j].AssetID
		}
		return out[i].Record.ID < out[j].Record.ID
	})
	return out
}

// allIn reports whether every token of want appears in set.
func allIn(want []string, set map[string]bool) bool {
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
