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

	"github.com/Autumn-27/artex/standards"
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

	// allowedReasoningEfforts are the accepted llm.reasoning_effort values,
	// mirroring the DB profile field (db.LLMProfile.ReasoningEffort: empty
	// omits it; low/medium/high/xhigh/max select intensity).
	allowedReasoningEfforts = []string{"", "low", "medium", "high", "xhigh", "max"}

	// allowedLLMPool are the accepted runtime.llm_pool modes; "" means off.
	allowedLLMPool = []string{"", "off", "failover", "spread"}
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
	// LLMExtra declares additional LLMs (D.7b) that provisioning saves as
	// extra profiles so the instance can alternate requests across them.
	LLMExtra []ExtraLLM `yaml:"llm_extra"`
	Admin    Admin      `yaml:"admin"`
	// Standards names test profiles (see standards/profiles.yaml) whose
	// requirements provisioning seeds as not_tested control results so
	// coverage is visible from the first boot (E.4). Optional; each name
	// must be a known profile.
	Standards []string `yaml:"standards"`
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
	// LLMPool selects the failover-pool mode for this instance: "" (= off),
	// "failover" or "spread". See settings llm_pool_enabled / llm_pool_spread.
	LLMPool string `yaml:"llm_pool"`
}

// LLM binds the instance to a local, OpenAI-compatible model.
type LLM struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"` // optional for provider ollama; otherwise a secret ref
	MaxTokens int    `yaml:"max_tokens"`
	// ReasoningEffort selects reasoning intensity: "" (default, omitted),
	// low, medium, high, xhigh or max — the same values a DB profile accepts.
	ReasoningEffort string `yaml:"reasoning_effort"`
	// MaxConcurrent caps this profile's in-flight LLM requests; 0 = the global
	// llm_max_concurrent setting.
	MaxConcurrent int `yaml:"max_concurrent"`
}

// ExtraLLM is one additional LLM declared under `llm_extra:`. It carries the
// same fields as the main `llm` block plus a required unique name. The name
// must differ from the blueprint name, which is the main profile's name.
type ExtraLLM struct {
	Name string `yaml:"name"`
	LLM  `yaml:",inline"`
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
	if !oneOf(bp.Runtime.LLMPool, allowedLLMPool) {
		add("runtime.llm_pool: must be one of off,failover,spread (got %q)", bp.Runtime.LLMPool)
	}

	// llm — the main profile's binding.
	validateLLM(add, "llm", bp.LLM)

	// llm_extra — additional LLMs (D.7b). Each entry gets the same field
	// checks as `llm`, plus a required, unique name that differs from the
	// blueprint name (the main profile's name).
	seen := map[string]bool{}
	for i, ex := range bp.LLMExtra {
		field := fmt.Sprintf("llm_extra[%d]", i)
		n := strings.TrimSpace(ex.Name)
		switch {
		case n == "":
			add("%s.name: required", field)
		case !nameRe.MatchString(n):
			add("%s.name: %q must match %s", field, ex.Name, nameRe.String())
		case n == bp.Name:
			add("%s.name: %q is the main profile's name; pick a different name", field, n)
		case seen[n]:
			add("%s.name: duplicate extra LLM name %q", field, n)
		default:
			seen[n] = true
		}
		validateLLM(add, field, ex.LLM)
	}

	// admin.password must be a reference, never an inline secret.
	if !secretRefRe.MatchString(bp.Admin.Password) {
		add("admin.password: inline secret not allowed; use ${env:VAR} or ${file:/path}")
	}

	// standards (optional): every name must be a known test profile.
	for i, s := range bp.Standards {
		if !standardsProfileAllowed(s) {
			add("standards[%d]: unknown profile %q; allowed: %s", i, s,
				strings.Join(standards.ProfileNames(), ", "))
		}
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

// validateLLM checks one LLM block (the main `llm` or an `llm_extra` entry)
// against the schema, recording problems via add with the given field prefix
// (e.g. "llm" or "llm_extra[1]"). The secret-ref rule applies here too:
// api_key may be empty only for provider ollama.
func validateLLM(add func(string, ...any), prefix string, l LLM) {
	if strings.TrimSpace(l.Provider) == "" {
		add("%s.provider: required", prefix)
	} else if _, ok := ProviderFormat(l.Provider); !ok {
		add("%s.provider: unknown provider %q; allowed: %s", prefix, l.Provider, allowedProvidersList())
	}
	if strings.TrimSpace(l.Model) == "" {
		add("%s.model: required", prefix)
	}
	if strings.TrimSpace(l.BaseURL) == "" {
		add("%s.base_url: required", prefix)
	}
	if l.MaxTokens < 0 {
		add("%s.max_tokens: must be >= 0 (got %d)", prefix, l.MaxTokens)
	}
	if !oneOf(l.ReasoningEffort, allowedReasoningEfforts) {
		add("%s.reasoning_effort: must be one of low,medium,high,xhigh,max (got %q)", prefix, l.ReasoningEffort)
	}
	if l.MaxConcurrent < 0 {
		add("%s.max_concurrent: must be >= 0 (got %d)", prefix, l.MaxConcurrent)
	}
	// Secret-ref rule: api_key must be a reference, never an inline secret.
	// It may be omitted (empty) only for provider ollama: a local Ollama
	// needs no key (agent/provider.go "Ready allows keyless native Ollama").
	// Every other provider must supply a secret ref.
	if strings.TrimSpace(l.APIKey) == "" {
		if l.Provider != "ollama" {
			add("%s.api_key: required for provider %q (only ollama may omit it)", prefix, l.Provider)
		}
	} else if !secretRefRe.MatchString(l.APIKey) {
		add("%s.api_key: inline secret not allowed; use ${env:VAR} or ${file:/path}", prefix)
	}
}

// oneOf reports whether v is a member of allowed.
func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
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

// standardsProfileAllowed reports whether name is one of the embedded test
// profiles (standards/profiles.yaml).
func standardsProfileAllowed(name string) bool {
	for _, n := range standards.ProfileNames() {
		if n == name {
			return true
		}
	}
	return false
}
