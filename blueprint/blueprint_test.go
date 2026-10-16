package blueprint

import (
	"errors"
	"strings"
	"testing"
)

// TestLoadExample verifies the committed example loads, validates clean (no
// error, no warnings), and populates the struct. This is the primary "Done
// when" check for D.1.
func TestLoadExample(t *testing.T) {
	bp, warnings, err := Load("examples/example.yaml")
	if err != nil {
		t.Fatalf("Load(example.yaml): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("Load(example.yaml): unexpected warnings: %v", warnings)
	}
	if bp == nil {
		t.Fatal("Load(example.yaml): nil blueprint")
	}
	// Spot-check that the struct is actually populated.
	if bp.Version != 1 {
		t.Errorf("Version = %d, want 1", bp.Version)
	}
	if bp.Name != "example-com" {
		t.Errorf("Name = %q, want %q", bp.Name, "example-com")
	}
	if bp.Authorization == nil || bp.Authorization.Window == nil {
		t.Fatal("authorization/window not populated")
	}
	if bp.Authorization.Window.Start != "2026-10-20" {
		t.Errorf("window.start = %q, want %q", bp.Authorization.Window.Start, "2026-10-20")
	}
	if bp.Company.Name != "Example Corp" {
		t.Errorf("company.name = %q, want %q", bp.Company.Name, "Example Corp")
	}
	if len(bp.Scope) != 2 {
		t.Errorf("len(scope) = %d, want 2", len(bp.Scope))
	}
	if len(bp.Constraints) != 2 {
		t.Errorf("len(constraints) = %d, want 2", len(bp.Constraints))
	}
	if bp.LLM.APIKey != "${env:ARTEX_LLM_API_KEY}" {
		t.Errorf("llm.api_key = %q, want the env ref", bp.LLM.APIKey)
	}
}

// TestValidFixtures covers inputs that must validate with no error and no
// warning, including the secret-ref accept case (env + file refs).
func TestValidFixtures(t *testing.T) {
	files := []string{"testdata/valid.yaml", "testdata/secret-env-and-file.yaml", "testdata/keyless-ollama.yaml"}
	for _, f := range files {
		f := f
		t.Run(f, func(t *testing.T) {
			_, warnings, err := Load(f)
			if err != nil {
				t.Fatalf("Load(%s): unexpected error: %v", f, err)
			}
			if len(warnings) != 0 {
				t.Fatalf("Load(%s): unexpected warnings: %v", f, warnings)
			}
		})
	}
}

// TestInvalidFixtures is the table-driven core: one fixture per invalid rule.
// Each must produce a *ValidationError whose message names the offending
// field(s).
func TestInvalidFixtures(t *testing.T) {
	cases := []struct {
		file string
		want []string // substrings that must appear in the aggregated error
	}{
		{"testdata/invalid-version.yaml", []string{"version:"}},
		{"testdata/invalid-name.yaml", []string{"name:"}},
		{"testdata/missing-authorization.yaml", []string{"authorization: section is required"}},
		{"testdata/invalid-window.yaml", []string{"authorization.window.end:", ">= start"}},
		{"testdata/invalid-window-unparseable.yaml", []string{
			"authorization.window.start:", "authorization.window.end:", "not a valid date",
		}},
		{"testdata/empty-company.yaml", []string{"company.name: required"}},
		{"testdata/empty-scope.yaml", []string{"scope: at least one entry"}},
		{"testdata/invalid-scope-entry.yaml", []string{"scope[0].kind:", "scope[1].value: required"}},
		{"testdata/invalid-constraint.yaml", []string{"constraints[1].kind:", "constraints[2].text: required"}},
		{"testdata/invalid-task.yaml", []string{
			"task.goal: required", "task.timeout_seconds:", "task.plan_heartbeat_seconds:",
		}},
		{"testdata/invalid-runtime.yaml", []string{"runtime.workers:", "runtime.llm_max_concurrent:"}},
		{"testdata/invalid-llm.yaml", []string{
			"llm.provider: required", "llm.model: required", "llm.base_url: required", "llm.max_tokens:",
		}},
		{"testdata/invalid-provider.yaml", []string{
			`llm.provider: unknown provider "gemini"`, "allowed:", "openai-compatible",
		}},
		{"testdata/inline-apikey.yaml", []string{"llm.api_key: inline secret not allowed"}},
		{"testdata/inline-password.yaml", []string{"admin.password: inline secret not allowed"}},
		{"testdata/keyless-openai.yaml", []string{"llm.api_key: required for provider \"openai\""}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			_, _, err := Load(tc.file)
			if err == nil {
				t.Fatalf("Load(%s): expected a validation error, got nil", tc.file)
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Load(%s): error is %T, want *ValidationError", tc.file, err)
			}
			msg := err.Error()
			for _, sub := range tc.want {
				if !strings.Contains(msg, sub) {
					t.Errorf("Load(%s): error does not mention %q\nfull error:\n%s", tc.file, sub, msg)
				}
			}
		})
	}
}

// TestMultiBadAggregates verifies that a fixture with many problems reports all
// of them in one error rather than stopping at the first.
func TestMultiBadAggregates(t *testing.T) {
	_, _, err := Load("testdata/multi-bad.yaml")
	if err == nil {
		t.Fatal("Load(multi-bad.yaml): expected a validation error, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error is %T, want *ValidationError", err)
	}
	want := []string{
		"version:",
		"name:",
		"authorization.authorized_by: required",
		"authorization.reference: required",
		"authorization.window.end:",
		"company.name: required",
		"scope[0].kind:",
		"scope[0].value: required",
		"constraints[0].kind:",
		"constraints[0].text: required",
		"task.goal: required",
		"task.timeout_seconds:",
		"task.plan_heartbeat_seconds:",
		"runtime.workers:",
		"runtime.llm_max_concurrent:",
		"llm.provider: required",
		"llm.model: required",
		"llm.base_url: required",
		"llm.max_tokens:",
		"llm.api_key: inline secret not allowed",
		"admin.password: inline secret not allowed",
	}
	for _, sub := range want {
		if !strings.Contains(err.Error(), sub) {
			t.Errorf("aggregate error missing %q", sub)
		}
	}
	if len(ve.Problems) < len(want) {
		t.Errorf("got %d problems, expected at least %d", len(ve.Problems), len(want))
	}
}

// TestWarnings verifies the two warning cases surface without failing
// validation.
func TestWarnings(t *testing.T) {
	cases := []struct {
		file string
		want string
	}{
		{"testdata/warn-no-allow.yaml", "no 'allow' constraint"},
		{"testdata/warn-zero-timeout.yaml", "task.timeout_seconds is 0"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			_, warnings, err := Load(tc.file)
			if err != nil {
				t.Fatalf("Load(%s): unexpected error: %v", tc.file, err)
			}
			if !containsSubstr(warnings, tc.want) {
				t.Errorf("Load(%s): warnings %v do not include %q", tc.file, warnings, tc.want)
			}
		})
	}
}

// TestSecretRefShapes exercises the secret-ref regex directly: accept env and
// file refs, reject inline values and malformed refs.
func TestSecretRefShapes(t *testing.T) {
	accept := []string{
		"${env:ARTEX_LLM_API_KEY}",
		"${env:A}",
		"${env:_X0}",
		"${file:/run/secrets/key}",
		"${file:relative/path}",
	}
	reject := []string{
		"",
		"sk-literalkey",
		"hunter2",
		"${env:lowercase}",  // env var must be upper/underscore
		"${env:1BAD}",       // must not start with a digit
		"${ENV:FOO}",        // wrong prefix casing
		"${file:}",          // empty path
		"prefix ${env:FOO}", // extra text around the ref
		"${env:FOO} suffix", // extra text around the ref
	}
	for _, s := range accept {
		if !secretRefRe.MatchString(s) {
			t.Errorf("secretRefRe should accept %q", s)
		}
	}
	for _, s := range reject {
		if secretRefRe.MatchString(s) {
			t.Errorf("secretRefRe should reject %q", s)
		}
	}
}

// TestParseMalformedYAML ensures a YAML syntax error is a parse error, not a
// *ValidationError.
func TestParseMalformedYAML(t *testing.T) {
	_, _, err := Parse([]byte("version: 1\nname: : : bad\n  - broken"))
	if err == nil {
		t.Fatal("Parse(malformed): expected an error, got nil")
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		t.Fatalf("Parse(malformed): got *ValidationError, want a YAML parse error: %v", err)
	}
}

// TestLLMExtraValid checks a two-LLM blueprint (main + one extra) validates
// clean and the extra's fields parse, including reasoning_effort and
// max_concurrent.
func TestLLMExtraValid(t *testing.T) {
	bp, warnings, err := Parse([]byte(twoLLMYAML()))
	if err != nil {
		t.Fatalf("two-LLM blueprint: unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("two-LLM blueprint: unexpected warnings: %v", warnings)
	}
	if len(bp.LLMExtra) != 1 {
		t.Fatalf("len(LLMExtra) = %d, want 1", len(bp.LLMExtra))
	}
	ex := bp.LLMExtra[0]
	if ex.Name != "spark" {
		t.Errorf("extra name = %q, want %q", ex.Name, "spark")
	}
	if ex.Provider != "openai-compatible" || ex.Model != "spark-llm" {
		t.Errorf("extra provider/model = %q/%q", ex.Provider, ex.Model)
	}
	if ex.ReasoningEffort != "xhigh" {
		t.Errorf("extra reasoning_effort = %q, want xhigh", ex.ReasoningEffort)
	}
	if ex.MaxConcurrent != 3 {
		t.Errorf("extra max_concurrent = %d, want 3", ex.MaxConcurrent)
	}
	if bp.Runtime.LLMPool != "spread" {
		t.Errorf("runtime.llm_pool = %q, want spread", bp.Runtime.LLMPool)
	}
}

// TestLLMExtraInvalid is the table-driven core for the D.7b extra-LLM rules:
// each input must produce a *ValidationError naming the offending field.
func TestLLMExtraInvalid(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []string
	}{
		{"empty name", withLLMExtra(twoLLMYAML(), `
llm_extra:
  - name: ""
    provider: openai-compatible
    model: spark-llm
    base_url: http://spark:8080/v1
    api_key: ${file:/run/secrets/llm_api_key_spark}
`), []string{"llm_extra[0].name: required"}},
		{"duplicate name", withLLMExtra(twoLLMYAML(), `
llm_extra:
  - name: spark
    provider: openai-compatible
    model: spark-llm
    base_url: http://spark:8080/v1
    api_key: ${file:/run/secrets/llm_api_key_spark}
  - name: spark
    provider: openai-compatible
    model: spark-llm-2
    base_url: http://spark2:8080/v1
    api_key: ${file:/run/secrets/llm_api_key_spark2}
`), []string{`llm_extra[1].name: duplicate extra LLM name "spark"`}},
		{"name equals blueprint name", withLLMExtra(twoLLMYAML(), `
llm_extra:
  - name: example-com
    provider: openai-compatible
    model: spark-llm
    base_url: http://spark:8080/v1
    api_key: ${file:/run/secrets/llm_api_key_spark}
`), []string{"is the main profile's name"}},
		{"bad reasoning_effort", withLLMExtra(twoLLMYAML(), `
llm_extra:
  - name: spark
    provider: openai-compatible
    model: spark-llm
    base_url: http://spark:8080/v1
    api_key: ${file:/run/secrets/llm_api_key_spark}
    reasoning_effort: ultra
`), []string{`llm_extra[0].reasoning_effort: must be one of low,medium,high,xhigh,max (got "ultra")`}},
		{"unknown provider", withLLMExtra(twoLLMYAML(), `
llm_extra:
  - name: spark
    provider: gemini
    model: spark-llm
    base_url: http://spark:8080/v1
    api_key: ${file:/run/secrets/llm_api_key_spark}
`), []string{`llm_extra[0].provider: unknown provider "gemini"`}},
		{"bad runtime.llm_pool", `
version: 1
name: example-com
authorization:
  authorized_by: "Jane Doe"
  reference: "ENG-1"
  window: { start: 2026-10-20, end: 2026-10-24 }
company: { name: Example Corp }
scope: [ { kind: domain, value: example.com } ]
constraints: [ { kind: allow, text: only example.com } ]
task: { goal: map, timeout_seconds: 3600 }
runtime: { workers: 2, llm_max_concurrent: 2, llm_pool: roundrobin }
llm:
  provider: openai-compatible
  model: qwen-3.8
  base_url: http://localhost:11434/v1
  api_key: ${file:/run/secrets/llm_api_key}
admin: { username: ARTEX, password: "${file:/run/secrets/sw_admin_password}" }
`, []string{`runtime.llm_pool: must be one of off,failover,spread (got "roundrobin")`}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected a validation error, got nil")
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error is %T, want *ValidationError", err)
			}
			for _, sub := range tc.want {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error does not mention %q\nfull error:\n%s", sub, err)
				}
			}
		})
	}
}

// twoLLMYAML returns a minimal valid blueprint with one extra LLM named
// "spark" and runtime.llm_pool: spread.
func twoLLMYAML() string {
	return `
version: 1
name: example-com
authorization:
  authorized_by: "Jane Doe"
  reference: "ENG-1"
  window: { start: 2026-10-20, end: 2026-10-24 }
company: { name: Example Corp }
scope: [ { kind: domain, value: example.com } ]
constraints: [ { kind: allow, text: only example.com } ]
task: { goal: map, timeout_seconds: 3600 }
runtime: { workers: 2, llm_max_concurrent: 2, llm_pool: spread }
llm:
  provider: openai-compatible
  model: qwen-3.8
  base_url: http://localhost:11434/v1
  api_key: ${file:/run/secrets/llm_api_key}
  reasoning_effort: xhigh
llm_extra:
  - name: spark
    provider: openai-compatible
    model: spark-llm
    base_url: http://spark:8080/v1
    api_key: ${file:/run/secrets/llm_api_key_spark}
    reasoning_effort: xhigh
    max_concurrent: 3
admin: { username: ARTEX, password: "${file:/run/secrets/sw_admin_password}" }
`
}

// withLLMExtra strips the trailing `llm_extra:` block (and everything up to
// `admin:`) from a blueprint and appends the given replacement block before
// the admin section.
func withLLMExtra(base, extra string) string {
	i := strings.Index(base, "\nllm_extra:")
	j := strings.Index(base, "\nadmin:")
	if i < 0 || j < 0 || j <= i {
		panic("twoLLMYAML shape changed; update withLLMExtra")
	}
	return base[:i] + extra + base[j:]
}

func containsSubstr(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}
