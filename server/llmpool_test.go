package server

import (
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/llmpool"
)

// The status list drives the "Failover order" strip in the UI, so it has to match the
// order PoolProfiles actually runs: active first, then priority DESC, then id ASC
// (input arrives id-ordered, so equal priorities must keep their relative order).
func TestSortPoolStatusMatchesChainOrder(t *testing.T) {
	in := []LLMPoolMemberStatus{
		{ProfileID: "1", Name: "low", Priority: 0},
		{ProfileID: "2", Name: "active", Priority: 0, Active: true},
		{ProfileID: "3", Name: "high", Priority: 10},
		{ProfileID: "4", Name: "mid-a", Priority: 5},
		{ProfileID: "5", Name: "mid-b", Priority: 5},
	}
	sortPoolStatus(in)

	want := []string{"active", "high", "mid-a", "mid-b", "low"}
	for i, w := range want {
		if in[i].Name != w {
			got := make([]string, len(in))
			for j, m := range in {
				got[j] = m.Name
			}
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// The active profile heads the chain no matter how low its own priority is —
// that's the documented precedence, and the UI must not imply otherwise.
func TestActiveProfileHeadsStatusList(t *testing.T) {
	in := []LLMPoolMemberStatus{
		{ProfileID: "1", Name: "loud", Priority: 999},
		{ProfileID: "2", Name: "active", Priority: -5, Active: true},
	}
	sortPoolStatus(in)
	if in[0].Name != "active" {
		t.Fatalf("head = %q, want the active profile regardless of priority", in[0].Name)
	}
}

// chainRank decides each non-head profile's chain rank. Spread off must be
// byte-for-byte the old rule (active profile outranks, others keep priority).
// Spread on must let same-priority profiles share the head's rank so the pool's
// equal-rank rotation alternates the lead, while other priorities are untouched.
func TestChainRank(t *testing.T) {
	cases := []struct {
		name         string
		profile      db.LLMProfile
		headPriority int
		spread       bool
		want         int
	}{
		{"off: default profile outranks", db.LLMProfile{IsDefault: true, Priority: 0}, 5, false, llmpool.RankActive - 1},
		{"off: plain profile keeps priority", db.LLMProfile{Priority: 3}, 5, false, 3},
		{"off: same priority as head still own rank", db.LLMProfile{Priority: 5}, 5, false, 5},
		{"on: same priority shares the lead", db.LLMProfile{Priority: 5}, 5, true, llmpool.RankActive},
		{"on: same priority as head, default profile also spreads", db.LLMProfile{IsDefault: true, Priority: 5}, 5, true, llmpool.RankActive},
		{"on: other priority keeps own rank", db.LLMProfile{Priority: 3}, 5, true, 3},
		{"on: other priority default profile still outranks", db.LLMProfile{IsDefault: true, Priority: 0}, 5, true, llmpool.RankActive - 1},
	}
	for _, c := range cases {
		if got := chainRank(&c.profile, c.headPriority, c.spread); got != c.want {
			t.Errorf("%s: chainRank = %d, want %d", c.name, got, c.want)
		}
	}
}
