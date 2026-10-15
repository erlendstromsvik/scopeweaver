// Package blueprint loads and validates ScopeWeaver "blueprint" YAML files.
//
// A blueprint is a self-contained, declarative description of an ephemeral
// ScopeWeaver instance: who authorized the engagement, the target company and
// scope, the test constraints, the task to run, runtime limits, the local LLM
// binding and admin credentials. This package is pure parse + validate: it
// turns a YAML file into a typed [Blueprint] and reports every problem it finds
// in one aggregated error, plus non-fatal warnings returned separately.
//
// It deliberately does NOT provision anything, resolve secret references, or
// touch the DB/server/engine. Provisioning (turning a valid blueprint into a
// running instance) is a separate task (D.2). See the field -> code mapping
// table below for how each field is intended to be wired there.
//
// # Field -> code mapping (for D.2 — reference only, nothing is wired here)
//
//	| Blueprint field                      | D.2 target (reference only)                               |
//	|--------------------------------------|-----------------------------------------------------------|
//	| company.*, scope[]                   | db.CompanyStore.CreateCompanyWithScope                     |
//	| constraints[]                        | db.ExplorationStore.AddConstraint(kind, text, "blueprint") |
//	| llm.*                                | db.SaveProfile(*db.LLMProfile)                             |
//	| task.* + company id + profile id     | server.Manager.CreateTaskWithOptions                      |
//	| runtime.*                            | settings: workers, llm_max_concurrent, web_search_enabled |
//	| admin.password                       | seed settings.auth.password_hash (bcrypt)                 |
//	| authorization.*                      | recorded at provision + run-gate (D.2 decides storage)    |
package blueprint

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the only blueprint schema version this package understands.
const SchemaVersion = 1

// dateLayout is the accepted format for authorization.window dates.
const dateLayout = "2006-01-02"

var (
	// nameRe constrains the blueprint name so it is safe as a DNS label /
	// docker-compose project prefix.
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

	// secretRefRe matches a secret reference, either an environment variable
	// (${env:VAR}) or a file path (${file:/path}). It only checks the SHAPE of
	// the reference; it does not resolve it.
	secretRefRe = regexp.MustCompile(`^\$\{(env:[A-Z_][A-Z0-9_]*|file:[^}]+)\}$`)

	validScopeKinds      = map[string]bool{"domain": true, "ip": true, "cidr": true, "icp": true, "keyword": true}
	validConstraintKinds = map[string]bool{"allow": true, "deny": true}

	// providerFormats maps each accepted llm.provider to the ScopeWeaver wire
	// "format" the database stores. db.llm_profiles.format has a CHECK of
	// ('openai','anthropic','openai-responses','ollama'), so provider names that
	// are not one of those must be translated. "openai-compatible" covers a local
	// model served behind an OpenAI-compatible API and uses the "openai" format
	// with a configurable base_url. The key set is exactly the accepted providers.
	providerFormats = map[string]string{
		"openai":            "openai",
		"openai-compatible": "openai",
		"anthropic":         "anthropic",
		"openai-responses":  "openai-responses",
		"ollama":            "ollama",
	}
)

// ProviderFormat returns the database wire format for a blueprint llm.provider
// and whether the provider is one the schema accepts. BuildPlan and the
// validator share this single source of truth so they can never disagree about
// which providers are valid.
func ProviderFormat(provider string) (format string, ok bool) {
	format, ok = providerFormats[strings.TrimSpace(provider)]
	return format, ok
}

// allowedProvidersList returns the accepted llm.provider values, sorted, for
// use in validation error messages.
func allowedProvidersList() string {
	ps := make([]string, 0, len(providerFormats))
	for p := range providerFormats {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return strings.Join(ps, ", ")
}

// Blueprint is a parsed blueprint file. Authorization and its Window are
// pointers so that a missing section can be told apart from an empty one.
type Blueprint struct {
	Version       int            `yaml:"version"`
	Name          string         `yaml:"name"`
	Authorization *Authorization `yaml:"authorization"`
	Company       Company        `yaml:"company"`
	Scope         []ScopeEntry   `yaml:"scope"`
	Constraints   []Constraint   `yaml:"constraints"`
	Task          Task           `yaml:"task"`
	Runtime       Runtime        `yaml:"runtime"`
	LLM           LLM            `yaml:"llm"`
	Admin         Admin          `yaml:"admin"`
}

// Authorization records who authorized the engagement and when it is valid.
type Authorization struct {
	AuthorizedBy  string  `yaml:"authorized_by"`
	ClientContact string  `yaml:"client_contact"`
	Window        *Window `yaml:"window"`
	Reference     string  `yaml:"reference"`
	Notes         string  `yaml:"notes"`
}

// Window is the inclusive date range during which testing is authorized.
type Window struct {
	Start string `yaml:"start"`
	End   string `yaml:"end"`
}

// Company identifies the target organization.
type Company struct {
	Name string `yaml:"name"`
	Logo string `yaml:"logo"`
}

// ScopeEntry is a single in-scope target.
type ScopeEntry struct {
	Kind  string `yaml:"kind"`
	Value string `yaml:"value"`
}

// Constraint is an allow/deny rule describing what the run may or may not do.
type Constraint struct {
	Kind string `yaml:"kind"`
	Text string `yaml:"text"`
}

// Task describes the exploration goal and its limits.
type Task struct {
	Goal                 string `yaml:"goal"`
	TimeoutSeconds       int    `yaml:"timeout_seconds"`
	CoverageEnabled      bool   `yaml:"coverage_enabled"`
	SeedFirstIntent      bool   `yaml:"seed_first_intent"`
	PlanHeartbeatSeconds int    `yaml:"plan_heartbeat_seconds"`
}

// Runtime holds process-level concurrency and feature settings.
type Runtime struct {
	Workers          int  `yaml:"workers"`
	LLMMaxConcurrent int  `yaml:"llm_max_concurrent"`
	WebSearchEnabled bool `yaml:"web_search_enabled"`
}

// LLM binds the instance to a local, OpenAI-compatible model.
type LLM struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"`
	MaxTokens int    `yaml:"max_tokens"`
}

// Admin holds the instance admin credentials. Password must be a secret ref.
type Admin struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// ValidationError aggregates every validation problem found in a blueprint.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("blueprint invalid (%d problem(s)):\n  - %s",
		len(e.Problems), strings.Join(e.Problems, "\n  - "))
}

// Load reads and parses the blueprint at path, then validates it. It returns
// the parsed blueprint, any non-fatal warnings, and an aggregated error if the
// blueprint is invalid or could not be read/parsed.
func Load(path string) (*Blueprint, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read blueprint %q: %w", path, err)
	}
	return Parse(data)
}

// Parse unmarshals YAML into a Blueprint and validates it. On a YAML syntax
// error it returns (nil, nil, err). On a valid-YAML-but-invalid-content input
// it returns the parsed blueprint alongside the warnings and the aggregated
// *ValidationError, so callers may inspect both.
func Parse(data []byte) (*Blueprint, []string, error) {
	var bp Blueprint
	if err := yaml.Unmarshal(data, &bp); err != nil {
		return nil, nil, fmt.Errorf("parse blueprint YAML: %w", err)
	}
	warnings, err := bp.Validate()
	return &bp, warnings, err
}

// Validate checks every rule in the schema. It collects all problems into a
// single *ValidationError (never stopping at the first), and returns non-fatal
// warnings separately. A nil error means the blueprint is valid; warnings may
// still be present.
func (bp *Blueprint) Validate() (warnings []string, err error) {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	// version
	if bp.Version != SchemaVersion {
		add("version: must be %d (got %d)", SchemaVersion, bp.Version)
	}

	// name
	switch {
	case bp.Name == "":
		add("name: required")
	case !nameRe.MatchString(bp.Name):
		add("name: %q must match %s", bp.Name, nameRe.String())
	}

	// authorization
	if bp.Authorization == nil {
		add("authorization: section is required")
	} else {
		az := bp.Authorization
		if strings.TrimSpace(az.AuthorizedBy) == "" {
			add("authorization.authorized_by: required")
		}
		if strings.TrimSpace(az.Reference) == "" {
			add("authorization.reference: required")
		}
		if az.Window == nil {
			add("authorization.window: required")
		} else {
			start, startOK := parseDate(az.Window.Start, "authorization.window.start", add)
			end, endOK := parseDate(az.Window.End, "authorization.window.end", add)
			if startOK && endOK && end.Before(start) {
				add("authorization.window.end: %q must be >= start %q",
					az.Window.End, az.Window.Start)
			}
		}
	}

	// company
	if strings.TrimSpace(bp.Company.Name) == "" {
		add("company.name: required")
	}

	// scope
	if len(bp.Scope) == 0 {
		add("scope: at least one entry is required")
	}
	for i, s := range bp.Scope {
		if !validScopeKinds[s.Kind] {
			add("scope[%d].kind: must be one of domain,ip,cidr,icp,keyword (got %q)", i, s.Kind)
		}
		if strings.TrimSpace(s.Value) == "" {
			add("scope[%d].value: required", i)
		}
	}

	// constraints (0+ allowed; see warnings for the no-allow case)
	for i, c := range bp.Constraints {
		if !validConstraintKinds[c.Kind] {
			add("constraints[%d].kind: must be one of allow,deny (got %q)", i, c.Kind)
		}
		if strings.TrimSpace(c.Text) == "" {
			add("constraints[%d].text: required", i)
		}
	}

	// task
	if strings.TrimSpace(bp.Task.Goal) == "" {
		add("task.goal: required")
	}
	if bp.Task.TimeoutSeconds < 0 {
		add("task.timeout_seconds: must be >= 0 (got %d)", bp.Task.TimeoutSeconds)
	}
	if bp.Task.PlanHeartbeatSeconds != 0 && bp.Task.PlanHeartbeatSeconds < 600 {
		add("task.plan_heartbeat_seconds: must be 0 or >= 600 (got %d)", bp.Task.PlanHeartbeatSeconds)
	}

	// runtime
	if bp.Runtime.Workers < 1 {
		add("runtime.workers: must be >= 1 (got %d)", bp.Runtime.Workers)
	}
	if bp.Runtime.LLMMaxConcurrent < 1 {
		add("runtime.llm_max_concurrent: must be >= 1 (got %d)", bp.Runtime.LLMMaxConcurrent)
	}

	// llm
	if strings.TrimSpace(bp.LLM.Provider) == "" {
		add("llm.provider: required")
	} else if _, ok := ProviderFormat(bp.LLM.Provider); !ok {
		add("llm.provider: unknown provider %q; allowed: %s", bp.LLM.Provider, allowedProvidersList())
	}
	if strings.TrimSpace(bp.LLM.Model) == "" {
		add("llm.model: required")
	}
	if strings.TrimSpace(bp.LLM.BaseURL) == "" {
		add("llm.base_url: required")
	}
	if bp.LLM.MaxTokens < 0 {
		add("llm.max_tokens: must be >= 0 (got %d)", bp.LLM.MaxTokens)
	}

	// secret-ref rule: these fields must be references, never inline secrets.
	if !secretRefRe.MatchString(bp.LLM.APIKey) {
		add("llm.api_key: inline secret not allowed; use ${env:VAR} or ${file:/path}")
	}
	if !secretRefRe.MatchString(bp.Admin.Password) {
		add("admin.password: inline secret not allowed; use ${env:VAR} or ${file:/path}")
	}

	// warnings (non-fatal)
	if !hasAllowConstraint(bp.Constraints) {
		warnings = append(warnings, "no 'allow' constraint: scope will not act as an allowlist")
	}
	if bp.Task.TimeoutSeconds == 0 {
		warnings = append(warnings, "task.timeout_seconds is 0: the run is unbounded (no timeout)")
	}

	if len(problems) > 0 {
		return warnings, &ValidationError{Problems: problems}
	}
	return warnings, nil
}

// parseDate parses a YYYY-MM-DD date, recording a problem via add if it is
// empty or unparseable. ok is false when a problem was recorded.
func parseDate(value, field string, add func(string, ...any)) (t time.Time, ok bool) {
	if strings.TrimSpace(value) == "" {
		add("%s: required", field)
		return time.Time{}, false
	}
	t, err := time.Parse(dateLayout, value)
	if err != nil {
		add("%s: %q is not a valid date (expected YYYY-MM-DD)", field, value)
		return time.Time{}, false
	}
	return t, true
}

func hasAllowConstraint(cs []Constraint) bool {
	for _, c := range cs {
		if c.Kind == "allow" {
			return true
		}
	}
	return false
}
