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

func containsSubstr(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}
