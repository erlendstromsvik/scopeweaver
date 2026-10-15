// Package provision turns a validated [blueprint.Blueprint] into a neutral,
// store-agnostic provisioning plan.
//
// It is the DB-free half of task D.2 (phase 2a). It resolves the blueprint's
// secret references, bcrypt-hashes the resolved admin password, and translates
// every blueprint field into "spec" values that the boot wiring (phase 2b) maps
// onto the real db/server calls. Keeping the translation here — pure CPU plus
// the secrets already read — lets it be unit-tested with no database, no
// network and no running server.
//
// Deliberately, this package imports only the blueprint package, the standard
// library and golang.org/x/crypto/bcrypt. It must NOT import db, server or
// cmd/..., and it opens no connections. The neutral spec types below are the
// decoupling seam: phase 2b depends on them, this package never depends on 2b.
package provision

import (
	"fmt"
	"os"
	"strings"

	"github.com/Autumn-27/artex/blueprint"
	"golang.org/x/crypto/bcrypt"
)

// ResolveRef resolves a single secret reference of the form ${env:VAR} or
// ${file:/path}.
//
//   - ${env:VAR} reads os.Getenv(VAR) and errors if it is unset or empty.
//   - ${file:/path} reads the file and returns its trimmed contents; it errors
//     if the file cannot be read or is empty after trimming.
//
// Any other shape (including a bare literal) is an error: callers that want to
// allow literals must check for the reference shape first (see ResolveSecrets).
func ResolveRef(ref string) (string, error) {
	inner, ok := strings.CutPrefix(ref, "${")
	if !ok {
		return "", fmt.Errorf("not a secret reference: %q (want ${env:VAR} or ${file:/path})", ref)
	}
	inner, ok = strings.CutSuffix(inner, "}")
	if !ok {
		return "", fmt.Errorf("not a secret reference: %q (want ${env:VAR} or ${file:/path})", ref)
	}

	switch {
	case strings.HasPrefix(inner, "env:"):
		name := strings.TrimPrefix(inner, "env:")
		if name == "" {
			return "", fmt.Errorf("secret reference %q: empty env var name", ref)
		}
		val, ok := os.LookupEnv(name)
		if !ok || val == "" {
			return "", fmt.Errorf("secret reference %q: env var %s is unset or empty", ref, name)
		}
		return val, nil
	case strings.HasPrefix(inner, "file:"):
		path := strings.TrimPrefix(inner, "file:")
		if path == "" {
			return "", fmt.Errorf("secret reference %q: empty file path", ref)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("secret reference %q: %w", ref, err)
		}
		val := strings.TrimSpace(string(data))
		if val == "" {
			return "", fmt.Errorf("secret reference %q: file %s is empty", ref, path)
		}
		return val, nil
	default:
		return "", fmt.Errorf("secret reference %q: unknown kind (want env: or file:)", ref)
	}
}

// isRef reports whether s has the shape of a secret reference (${...}). It does
// not validate the inner kind; ResolveRef does that.
func isRef(s string) bool {
	return strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}")
}

// Secrets holds the resolved secret values for a blueprint. They are kept in
// memory only for the duration of provisioning.
type Secrets struct {
	AdminPassword string // resolved from blueprint.Admin.Password (always a ref)
	LLMAPIKey     string // resolved from blueprint.LLM.APIKey (a ref, or "" when omitted for ollama)
	LLMBaseURL    string // blueprint.LLM.BaseURL, resolved if a ref, else literal
}

// ResolveSecrets resolves every secret the blueprint needs.
//
// The blueprint validator (D.1) guarantees admin.password is a reference, so
// it is resolved via ResolveRef. llm.api_key is a reference too, except that
// an ollama blueprint may omit it entirely (keyless local Ollama); an empty
// api_key resolves to "" without a ResolveRef call. llm.base_url may be a
// literal URL or a reference: it is resolved if it has the reference shape
// and passed through verbatim otherwise.
func ResolveSecrets(bp *blueprint.Blueprint) (Secrets, error) {
	var s Secrets
	var err error

	if s.AdminPassword, err = ResolveRef(bp.Admin.Password); err != nil {
		return Secrets{}, fmt.Errorf("admin.password: %w", err)
	}
	if strings.TrimSpace(bp.LLM.APIKey) != "" {
		if s.LLMAPIKey, err = ResolveRef(bp.LLM.APIKey); err != nil {
			return Secrets{}, fmt.Errorf("llm.api_key: %w", err)
		}
	}
	if isRef(bp.LLM.BaseURL) {
		if s.LLMBaseURL, err = ResolveRef(bp.LLM.BaseURL); err != nil {
			return Secrets{}, fmt.Errorf("llm.base_url: %w", err)
		}
	} else {
		s.LLMBaseURL = bp.LLM.BaseURL
	}
	return s, nil
}

// Neutral spec types. These mirror the blueprint's shape but carry no YAML tags
// and no blueprint-package dependency on the db/server side, so phase 2b can map
// them onto the real store calls without reaching back into this package.

// ScopeSpec is one in-scope target.
type ScopeSpec struct {
	Kind  string
	Value string
}

// ConstraintSpec is one allow/deny rule.
type ConstraintSpec struct {
	Kind string
	Text string
}

// ProfileSpec is the LLM profile to persist, with secrets already resolved.
// Format is the db wire format (db.llm_profiles.format), already translated from
// the blueprint's llm.provider by [blueprint.ProviderFormat].
type ProfileSpec struct {
	Format    string
	Model     string
	BaseURL   string
	APIKey    string
	MaxTokens int
}

// TaskSpec is the exploration task to create and its limits.
type TaskSpec struct {
	Description          string
	Goal                 string
	TimeoutSeconds       int
	PlanHeartbeatSeconds int
	CoverageEnabled      bool
	SeedFirstIntent      bool
}

// SettingsSpec is the process-level runtime configuration.
type SettingsSpec struct {
	Workers          int
	LLMMaxConcurrent int
	WebSearchEnabled bool
}

// Plan is the complete, store-agnostic provisioning plan derived from a
// blueprint. Every field is ready to be applied by phase 2b; no further secret
// resolution or hashing is needed.
type Plan struct {
	CompanyName string
	CompanyLogo string
	Scope       []ScopeSpec
	Constraints []ConstraintSpec
	Profile     ProfileSpec
	Task        TaskSpec
	Settings    SettingsSpec
	// Standards are the blueprint's test-profile names; phase 2b seeds the
	// requirements they select as not_tested control results (E.4).
	Standards []string

	AdminUsername     string
	AdminPasswordHash string // bcrypt hash of the resolved admin password
}

// BuildPlan translates a validated blueprint plus its resolved secrets into a
// Plan. It assumes the blueprint already passed blueprint.Validate (D.1) and
// that s came from ResolveSecrets for the same blueprint.
//
// It is pure: CPU work plus the one bcrypt hash of the already-resolved admin
// password. It performs no I/O of its own.
func BuildPlan(bp *blueprint.Blueprint, s Secrets) (*Plan, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(s.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash admin password: %w", err)
	}

	scope := make([]ScopeSpec, len(bp.Scope))
	for i, e := range bp.Scope {
		scope[i] = ScopeSpec{Kind: e.Kind, Value: e.Value}
	}

	constraints := make([]ConstraintSpec, len(bp.Constraints))
	for i, c := range bp.Constraints {
		constraints[i] = ConstraintSpec{Kind: c.Kind, Text: c.Text}
	}

	// Translate the blueprint provider to the db wire format. A validated
	// blueprint always resolves (Validate rejects unknown providers); guard
	// anyway so BuildPlan is safe to call directly in tests.
	format, ok := blueprint.ProviderFormat(bp.LLM.Provider)
	if !ok {
		return nil, fmt.Errorf("llm.provider %q has no known wire format", bp.LLM.Provider)
	}

	return &Plan{
		CompanyName: bp.Company.Name,
		CompanyLogo: bp.Company.Logo,
		Scope:       scope,
		Constraints: constraints,
		Standards:   append([]string(nil), bp.Standards...),
		Profile: ProfileSpec{
			Format:    format,
			Model:     bp.LLM.Model,
			BaseURL:   s.LLMBaseURL,
			APIKey:    s.LLMAPIKey,
			MaxTokens: bp.LLM.MaxTokens,
		},
		Task: TaskSpec{
			// The blueprint carries a single task.goal and no separate
			// description; it serves as both the exploration's starting-point
			// description and its objective.
			Description:          bp.Task.Goal,
			Goal:                 bp.Task.Goal,
			TimeoutSeconds:       bp.Task.TimeoutSeconds,
			PlanHeartbeatSeconds: bp.Task.PlanHeartbeatSeconds,
			CoverageEnabled:      bp.Task.CoverageEnabled,
			SeedFirstIntent:      bp.Task.SeedFirstIntent,
		},
		Settings: SettingsSpec{
			Workers:          bp.Runtime.Workers,
			LLMMaxConcurrent: bp.Runtime.LLMMaxConcurrent,
			WebSearchEnabled: bp.Runtime.WebSearchEnabled,
		},
		AdminUsername:     bp.Admin.Username,
		AdminPasswordHash: string(hash),
	}, nil
}
