package server

import (
	"testing"

	"github.com/Autumn-27/artex/blueprint"
	"github.com/Autumn-27/artex/provision"
)

// TestScopeSpecsToInputs checks the pure 1:1 mapping from neutral scope specs to
// db.ScopeInput rows. DB-free.
func TestScopeSpecsToInputs(t *testing.T) {
	specs := []provision.ScopeSpec{
		{Kind: "domain", Value: "example.com"},
		{Kind: "ip", Value: "203.0.113.10"},
		{Kind: "keyword", Value: "example"},
	}
	got := scopeSpecsToInputs(specs)
	if len(got) != len(specs) {
		t.Fatalf("length: got %d, want %d", len(got), len(specs))
	}
	for i, s := range specs {
		if got[i].Kind != s.Kind || got[i].Value != s.Value {
			t.Errorf("input[%d]: got {%q,%q}, want {%q,%q}", i, got[i].Kind, got[i].Value, s.Kind, s.Value)
		}
	}

	// Empty input must yield an empty (non-nil) slice, not panic.
	if out := scopeSpecsToInputs(nil); len(out) != 0 {
		t.Errorf("nil specs: got %d inputs, want 0", len(out))
	}
}

// TestBlueprintTaskOptions checks the pure plan -> db.TaskCreateOptions mapping,
// including the *bool coverage flag. DB-free.
func TestBlueprintTaskOptions(t *testing.T) {
	plan := &provision.Plan{
		Settings: provision.SettingsSpec{LLMPool: "off"},
		Task: provision.TaskSpec{
			Description:          "desc",
			Goal:                 "goal",
			TimeoutSeconds:       7200,
			PlanHeartbeatSeconds: 600,
			CoverageEnabled:      true,
		},
	}
	opts := blueprintTaskOptions(plan, 42, 99)

	if len(opts.CompanyIDs) != 1 || opts.CompanyIDs[0] != 42 {
		t.Errorf("CompanyIDs: got %v, want [42]", opts.CompanyIDs)
	}
	if len(opts.LLMProfileIDs) != 1 || opts.LLMProfileIDs[0] != 99 {
		t.Errorf("LLMProfileIDs: got %v, want [99]", opts.LLMProfileIDs)
	}
	if opts.TimeoutSeconds != 7200 {
		t.Errorf("TimeoutSeconds: got %d, want 7200", opts.TimeoutSeconds)
	}
	if opts.PlanHeartbeatSeconds != 600 {
		t.Errorf("PlanHeartbeatSeconds: got %d, want 600", opts.PlanHeartbeatSeconds)
	}
	if opts.CoverageEnabled == nil {
		t.Fatal("CoverageEnabled: got nil, want non-nil *bool")
	}
	if *opts.CoverageEnabled != true {
		t.Errorf("CoverageEnabled: got %v, want true", *opts.CoverageEnabled)
	}

	// A false coverage flag must round-trip as a non-nil pointer to false, not
	// be dropped to nil (which CreateTaskWithOptions would treat as "default on").
	planOff := &provision.Plan{Task: provision.TaskSpec{CoverageEnabled: false}}
	if off := blueprintTaskOptions(planOff, 1, 2).CoverageEnabled; off == nil || *off != false {
		t.Errorf("CoverageEnabled off: got %v, want pointer to false", off)
	}
}

// TestBlueprintTaskOptionsPool checks the D.7b pool rule: with llm_pool off
// the task pins the main profile; with the pool on (failover or spread)
// LLMProfileIDs stays nil so the task runs on the pooled global provider
// (a task bound to one profile bypasses the pool — task_llm.go current()).
func TestBlueprintTaskOptionsPool(t *testing.T) {
	for _, tc := range []struct {
		pool     string
		wantBind bool
	}{
		{"off", true},
		{"failover", false},
		{"spread", false},
	} {
		plan := &provision.Plan{Settings: provision.SettingsSpec{LLMPool: tc.pool}}
		opts := blueprintTaskOptions(plan, 7, 21)
		if tc.wantBind {
			if len(opts.LLMProfileIDs) != 1 || opts.LLMProfileIDs[0] != 21 {
				t.Errorf("pool %q: LLMProfileIDs = %v, want [21]", tc.pool, opts.LLMProfileIDs)
			}
		} else if opts.LLMProfileIDs != nil {
			t.Errorf("pool %q: LLMProfileIDs = %v, want nil (pooled global provider)", tc.pool, opts.LLMProfileIDs)
		}
		if len(opts.CompanyIDs) != 1 || opts.CompanyIDs[0] != 7 {
			t.Errorf("pool %q: CompanyIDs = %v, want [7]", tc.pool, opts.CompanyIDs)
		}
	}
}

// TestApplyBlueprintNilArgs verifies the guard rejects nil inputs before touching
// any store, so it is safe to call on a zero-value Server. DB-free.
func TestApplyBlueprintNilArgs(t *testing.T) {
	s := &Server{} // s.m is nil; the nil-arg guard must return before dereferencing it.
	if err := s.ApplyBlueprint(nil, nil); err == nil {
		t.Error("ApplyBlueprint(nil, nil): got nil error, want error")
	}
	if err := s.ApplyBlueprint(nil, &provision.Plan{}); err == nil {
		t.Error("ApplyBlueprint(nil, plan): got nil error, want error")
	}
	if err := s.ApplyBlueprint(&blueprint.Blueprint{Name: "x"}, nil); err == nil {
		t.Error("ApplyBlueprint(bp, nil): got nil error, want error")
	}
}

// TestScopeSeedTargetsUsesScopeNotProse pins the P0 fix: blueprint seeding comes
// from the scope entries, never from the goal prose. parseTarget on the real
// karriereveiledning goal would grab "Next.js" -> next.js (asserted here as the
// hazard), while scopeSeedTargets yields exactly the in-scope hosts and nothing
// framework-shaped.
func TestScopeSeedTargetsUsesScopeNotProse(t *testing.T) {
	// The hazard the fix removes: parsing the goal prose yields an out-of-scope
	// host because "Next.js" is the first domain-shaped token.
	goal := "Map and fingerprint the Next.js frontend at karriereveiledning.no and the Drupal backend at cms.karriereveiledning.no"
	if _, host, _, ok := parseTarget(goal); !ok || host != "next.js" {
		t.Fatalf("expected parseTarget to grab the framework name next.js (the hazard), got host=%q ok=%v", host, ok)
	}

	scope := []provision.ScopeSpec{
		{Kind: "domain", Value: "karriereveiledning.no"},
		{Kind: "domain", Value: "cms.karriereveiledning.no"},
		{Kind: "cidr", Value: "10.0.0.0/8"},  // skipped: a matcher, not a target
		{Kind: "keyword", Value: "karriere"}, // skipped
	}
	got := scopeSeedTargets(scope)
	want := []scopeSeedTarget{
		{scheme: "https", host: "karriereveiledning.no", port: 443},
		{scheme: "https", host: "cms.karriereveiledning.no", port: 443},
	}
	if len(got) != len(want) {
		t.Fatalf("scopeSeedTargets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("target[%d] = %+v, want %+v", i, got[i], want[i])
		}
		if got[i].host == "next.js" {
			t.Errorf("seed target must never be the framework name next.js")
		}
	}
}

// TestScopeSeedTargetsIP maps an ip scope entry to a target and skips icp.
func TestScopeSeedTargetsIP(t *testing.T) {
	got := scopeSeedTargets([]provision.ScopeSpec{
		{Kind: "ip", Value: "203.0.113.4"},
		{Kind: "icp", Value: "something"},
		{Kind: "domain", Value: "  "}, // blank -> skipped
	})
	if len(got) != 1 || got[0].host != "203.0.113.4" || got[0].scheme != "https" {
		t.Fatalf("scopeSeedTargets(ip) = %+v, want one https 203.0.113.4 target", got)
	}
}
