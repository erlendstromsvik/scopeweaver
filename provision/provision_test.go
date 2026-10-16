package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/blueprint"
	"golang.org/x/crypto/bcrypt"
)

// writeFile writes content to path or fails the test.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestResolveRef(t *testing.T) {
	dir := t.TempDir()
	goodFile := filepath.Join(dir, "secret.txt")
	if err := writeFile(goodFile, "  file-secret\n"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	emptyFile := filepath.Join(dir, "empty.txt")
	if err := writeFile(emptyFile, "   \n"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Setenv("PROVISION_TEST_SET", "env-value")
	t.Setenv("PROVISION_TEST_EMPTY", "")

	tests := []struct {
		name    string
		ref     string
		want    string
		wantErr bool
	}{
		{name: "env set", ref: "${env:PROVISION_TEST_SET}", want: "env-value"},
		{name: "env unset", ref: "${env:PROVISION_TEST_MISSING}", wantErr: true},
		{name: "env empty", ref: "${env:PROVISION_TEST_EMPTY}", wantErr: true},
		{name: "env empty name", ref: "${env:}", wantErr: true},
		{name: "file present trimmed", ref: "${file:" + goodFile + "}", want: "file-secret"},
		{name: "file missing", ref: "${file:" + filepath.Join(dir, "nope.txt") + "}", wantErr: true},
		{name: "file empty", ref: "${file:" + emptyFile + "}", wantErr: true},
		{name: "file empty path", ref: "${file:}", wantErr: true},
		{name: "literal not a ref", ref: "plain-value", wantErr: true},
		{name: "unknown kind", ref: "${vault:foo}", wantErr: true},
		{name: "missing close brace", ref: "${env:FOO", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveRef(tc.ref)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolveRef(%q) = %q, want error", tc.ref, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveRef(%q) unexpected error: %v", tc.ref, err)
			}
			if got != tc.want {
				t.Fatalf("ResolveRef(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

func TestResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "api.key")
	if err := writeFile(keyFile, "file-api-key\n"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv("PROVISION_ADMIN_PW", "admin-pw")
	t.Setenv("PROVISION_BASE_URL", "http://ref-host:9000/v1")

	t.Run("literal base_url passthrough", func(t *testing.T) {
		bp := &blueprint.Blueprint{
			Admin: blueprint.Admin{Password: "${env:PROVISION_ADMIN_PW}"},
			LLM: blueprint.LLM{
				APIKey:  "${file:" + keyFile + "}",
				BaseURL: "http://localhost:1234/v1",
			},
		}
		s, err := ResolveSecrets(bp)
		if err != nil {
			t.Fatalf("ResolveSecrets: %v", err)
		}
		if s.AdminPassword != "admin-pw" {
			t.Errorf("AdminPassword = %q, want %q", s.AdminPassword, "admin-pw")
		}
		if s.LLMAPIKey != "file-api-key" {
			t.Errorf("LLMAPIKey = %q, want %q", s.LLMAPIKey, "file-api-key")
		}
		if s.LLMBaseURL != "http://localhost:1234/v1" {
			t.Errorf("LLMBaseURL = %q, want literal passthrough", s.LLMBaseURL)
		}
	})

	t.Run("base_url as ref is resolved", func(t *testing.T) {
		bp := &blueprint.Blueprint{
			Admin: blueprint.Admin{Password: "${env:PROVISION_ADMIN_PW}"},
			LLM: blueprint.LLM{
				APIKey:  "${file:" + keyFile + "}",
				BaseURL: "${env:PROVISION_BASE_URL}",
			},
		}
		s, err := ResolveSecrets(bp)
		if err != nil {
			t.Fatalf("ResolveSecrets: %v", err)
		}
		if s.LLMBaseURL != "http://ref-host:9000/v1" {
			t.Errorf("LLMBaseURL = %q, want resolved ref value", s.LLMBaseURL)
		}
	})

	t.Run("error surfaces field", func(t *testing.T) {
		bp := &blueprint.Blueprint{
			Admin: blueprint.Admin{Password: "${env:PROVISION_MISSING}"},
			LLM:   blueprint.LLM{APIKey: "${file:" + keyFile + "}", BaseURL: "http://x"},
		}
		if _, err := ResolveSecrets(bp); err == nil {
			t.Fatal("ResolveSecrets: want error for unset admin password env")
		}
	})

	t.Run("keyless ollama leaves LLMAPIKey empty", func(t *testing.T) {
		bp := &blueprint.Blueprint{
			Admin: blueprint.Admin{Password: "${env:PROVISION_ADMIN_PW}"},
			LLM: blueprint.LLM{
				Provider: "ollama",
				Model:    "qwen-3.8",
				BaseURL:  "http://127.0.0.1:11434",
			},
		}
		s, err := ResolveSecrets(bp)
		if err != nil {
			t.Fatalf("ResolveSecrets: %v", err)
		}
		if s.LLMAPIKey != "" {
			t.Errorf("LLMAPIKey = %q, want empty", s.LLMAPIKey)
		}
		if s.AdminPassword != "admin-pw" {
			t.Errorf("AdminPassword = %q, want %q", s.AdminPassword, "admin-pw")
		}
		plan, err := BuildPlan(bp, s)
		if err != nil {
			t.Fatalf("BuildPlan: %v", err)
		}
		if plan.Profile.APIKey != "" {
			t.Errorf("Plan.Profile.APIKey = %q, want empty", plan.Profile.APIKey)
		}
		if plan.Profile.Format != "ollama" {
			t.Errorf("Plan.Profile.Format = %q, want %q", plan.Profile.Format, "ollama")
		}
	})
}

func TestBuildPlan(t *testing.T) {
	bp := &blueprint.Blueprint{
		Version: blueprint.SchemaVersion,
		Name:    "acme-test",
		Company: blueprint.Company{Name: "Acme Corp", Logo: "acme.png"},
		Scope: []blueprint.ScopeEntry{
			{Kind: "domain", Value: "acme.example"},
			{Kind: "cidr", Value: "10.0.0.0/24"},
		},
		Constraints: []blueprint.Constraint{
			{Kind: "allow", Text: "only staging"},
			{Kind: "deny", Text: "no prod writes"},
		},
		Task: blueprint.Task{
			Goal:                 "map the attack surface",
			TimeoutSeconds:       3600,
			CoverageEnabled:      true,
			SeedFirstIntent:      true,
			PlanHeartbeatSeconds: 900,
		},
		Runtime: blueprint.Runtime{
			Workers:          4,
			LLMMaxConcurrent: 2,
			WebSearchEnabled: true,
		},
		LLM: blueprint.LLM{
			Provider:  "openai",
			Model:     "local-model",
			BaseURL:   "http://localhost:1234/v1",
			APIKey:    "${env:PROVISION_PLAN_KEY}",
			MaxTokens: 8192,
		},
		Admin: blueprint.Admin{
			Username: "root",
			Password: "${env:PROVISION_PLAN_PW}",
		},
	}
	t.Setenv("PROVISION_PLAN_KEY", "the-api-key")
	t.Setenv("PROVISION_PLAN_PW", "s3cret-pw")

	secrets, err := ResolveSecrets(bp)
	if err != nil {
		t.Fatalf("ResolveSecrets: %v", err)
	}
	plan, err := BuildPlan(bp, secrets)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	// Company.
	if plan.CompanyName != "Acme Corp" {
		t.Errorf("CompanyName = %q", plan.CompanyName)
	}
	if plan.CompanyLogo != "acme.png" {
		t.Errorf("CompanyLogo = %q", plan.CompanyLogo)
	}

	// Scope.
	wantScope := []ScopeSpec{
		{Kind: "domain", Value: "acme.example"},
		{Kind: "cidr", Value: "10.0.0.0/24"},
	}
	if len(plan.Scope) != len(wantScope) {
		t.Fatalf("Scope len = %d, want %d", len(plan.Scope), len(wantScope))
	}
	for i, want := range wantScope {
		if plan.Scope[i] != want {
			t.Errorf("Scope[%d] = %+v, want %+v", i, plan.Scope[i], want)
		}
	}

	// Constraints.
	wantCons := []ConstraintSpec{
		{Kind: "allow", Text: "only staging"},
		{Kind: "deny", Text: "no prod writes"},
	}
	if len(plan.Constraints) != len(wantCons) {
		t.Fatalf("Constraints len = %d, want %d", len(plan.Constraints), len(wantCons))
	}
	for i, want := range wantCons {
		if plan.Constraints[i] != want {
			t.Errorf("Constraints[%d] = %+v, want %+v", i, plan.Constraints[i], want)
		}
	}

	// Profile (with resolved secrets). provider "openai" maps to format "openai".
	wantProfile := ProfileSpec{
		Name:      "acme-test",
		Format:    "openai",
		Model:     "local-model",
		BaseURL:   "http://localhost:1234/v1",
		APIKey:    "the-api-key",
		MaxTokens: 8192,
	}
	if plan.Profile != wantProfile {
		t.Errorf("Profile = %+v, want %+v", plan.Profile, wantProfile)
	}

	// Task.
	wantTask := TaskSpec{
		Description:          "map the attack surface",
		Goal:                 "map the attack surface",
		TimeoutSeconds:       3600,
		PlanHeartbeatSeconds: 900,
		CoverageEnabled:      true,
		SeedFirstIntent:      true,
	}
	if plan.Task != wantTask {
		t.Errorf("Task = %+v, want %+v", plan.Task, wantTask)
	}

	// Settings.
	// LLMPool normalizes "" to "off".
	wantSettings := SettingsSpec{Workers: 4, LLMMaxConcurrent: 2, WebSearchEnabled: true, LLMPool: "off"}
	if plan.Settings != wantSettings {
		t.Errorf("Settings = %+v, want %+v", plan.Settings, wantSettings)
	}

	// Admin username + password hash.
	if plan.AdminUsername != "root" {
		t.Errorf("AdminUsername = %q", plan.AdminUsername)
	}
	if plan.AdminPasswordHash == "" {
		t.Fatal("AdminPasswordHash is empty")
	}
	if plan.AdminPasswordHash == "s3cret-pw" {
		t.Fatal("AdminPasswordHash is the plaintext password, not a hash")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(plan.AdminPasswordHash), []byte("s3cret-pw")); err != nil {
		t.Errorf("admin hash does not verify against the resolved password: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(plan.AdminPasswordHash), []byte("wrong")); err == nil {
		t.Error("admin hash verified against a wrong password")
	}
}

func TestBuildPlanProviderFormat(t *testing.T) {
	cases := []struct {
		name       string
		provider   string
		wantFormat string
		wantErr    bool
	}{
		{"openai", "openai", "openai", false},
		{"openai-compatible maps to openai", "openai-compatible", "openai", false},
		{"anthropic", "anthropic", "anthropic", false},
		{"openai-responses", "openai-responses", "openai-responses", false},
		{"ollama", "ollama", "ollama", false},
		{"empty", "", "", true},
		{"unknown", "gpt-foo", "", true},
		{"case-sensitive", "OpenAI", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bp := &blueprint.Blueprint{LLM: blueprint.LLM{Provider: tc.provider}}
			plan, err := BuildPlan(bp, Secrets{})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("BuildPlan(provider=%q): want error, got format %q", tc.provider, plan.Profile.Format)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildPlan(provider=%q): %v", tc.provider, err)
			}
			if plan.Profile.Format != tc.wantFormat {
				t.Errorf("provider %q -> format %q, want %q", tc.provider, plan.Profile.Format, tc.wantFormat)
			}
		})
	}
}

// TestResolveSecretsExtra checks each llm_extra entry's api_key and base_url
// resolve by the same rules as the main llm block, in order.
func TestResolveSecretsExtra(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "spark.key")
	if err := writeFile(keyFile, "spark-key\n"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv("PROVISION_ADMIN_PW", "admin-pw")
	t.Setenv("PROVISION_SPARK_URL", "http://ref-spark:8080/v1")

	bp := &blueprint.Blueprint{
		Admin: blueprint.Admin{Password: "${env:PROVISION_ADMIN_PW}"},
		LLM: blueprint.LLM{
			Provider: "openai-compatible",
			Model:    "main",
			BaseURL:  "http://main:1234/v1",
			APIKey:   "${env:PROVISION_PLAN_KEY}",
		},
		LLMExtra: []blueprint.ExtraLLM{
			{
				Name: "spark",
				LLM: blueprint.LLM{
					Provider: "openai-compatible",
					Model:    "spark-llm",
					BaseURL:  "http://spark:8080/v1",
					APIKey:   "${file:" + keyFile + "}",
				},
			},
			{
				Name: "ollie",
				LLM: blueprint.LLM{
					Provider: "ollama",
					Model:    "qwen3",
					BaseURL:  "${env:PROVISION_SPARK_URL}",
				},
			},
		},
	}
	t.Setenv("PROVISION_PLAN_KEY", "main-key")

	s, err := ResolveSecrets(bp)
	if err != nil {
		t.Fatalf("ResolveSecrets: %v", err)
	}
	if len(s.Extra) != 2 {
		t.Fatalf("len(Extra) = %d, want 2", len(s.Extra))
	}
	if s.Extra[0].APIKey != "spark-key" {
		t.Errorf("Extra[0].APIKey = %q, want spark-key", s.Extra[0].APIKey)
	}
	if s.Extra[0].BaseURL != "http://spark:8080/v1" {
		t.Errorf("Extra[0].BaseURL = %q, want literal passthrough", s.Extra[0].BaseURL)
	}
	if s.Extra[1].APIKey != "" {
		t.Errorf("Extra[1].APIKey = %q, want empty (keyless ollama)", s.Extra[1].APIKey)
	}
	if s.Extra[1].BaseURL != "http://ref-spark:8080/v1" {
		t.Errorf("Extra[1].BaseURL = %q, want resolved ref", s.Extra[1].BaseURL)
	}

	t.Run("error names the extra field", func(t *testing.T) {
		bad := &blueprint.Blueprint{
			Admin: blueprint.Admin{Password: "${env:PROVISION_ADMIN_PW}"},
			LLM:   blueprint.LLM{Provider: "ollama", Model: "m", BaseURL: "http://x"},
			LLMExtra: []blueprint.ExtraLLM{
				{Name: "spark", LLM: blueprint.LLM{
					Provider: "openai-compatible", Model: "s",
					BaseURL: "http://spark:8080/v1",
					APIKey:  "${env:PROVISION_MISSING_EXTRA_KEY}",
				}},
			},
		}
		_, err := ResolveSecrets(bad)
		if err == nil {
			t.Fatal("want error for unset extra api_key env")
		}
		if !strings.Contains(err.Error(), "llm_extra[0].api_key") {
			t.Errorf("error %q does not name llm_extra[0].api_key", err)
		}
	})
}

// TestBuildPlanExtraLLM checks the plan carries one profile per LLM (main +
// extras) with its reasoning effort and concurrency, and that the pool mode
// normalizes "" to "off" and passes failover/spread through.
func TestBuildPlanExtraLLM(t *testing.T) {
	t.Setenv("PROVISION_ADMIN_PW", "admin-pw")
	t.Setenv("PROVISION_PLAN_KEY", "main-key")
	t.Setenv("PROVISION_SPARK_KEY", "spark-key")

	bp := &blueprint.Blueprint{
		Version: blueprint.SchemaVersion,
		Name:    "acme-test",
		Runtime: blueprint.Runtime{Workers: 2, LLMMaxConcurrent: 2, LLMPool: "spread"},
		LLM: blueprint.LLM{
			Provider:        "openai-compatible",
			Model:           "qwen-3.8",
			BaseURL:         "http://main:11434/v1",
			APIKey:          "${env:PROVISION_PLAN_KEY}",
			ReasoningEffort: "xhigh",
		},
		LLMExtra: []blueprint.ExtraLLM{
			{
				Name: "spark",
				LLM: blueprint.LLM{
					Provider:        "openai-compatible",
					Model:           "spark-llm",
					BaseURL:         "http://spark:8080/v1",
					APIKey:          "${env:PROVISION_SPARK_KEY}",
					ReasoningEffort: "xhigh",
					MaxConcurrent:   3,
				},
			},
		},
		Admin: blueprint.Admin{Username: "root", Password: "${env:PROVISION_ADMIN_PW}"},
	}

	secrets, err := ResolveSecrets(bp)
	if err != nil {
		t.Fatalf("ResolveSecrets: %v", err)
	}
	plan, err := BuildPlan(bp, secrets)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	if plan.Profile.Name != "acme-test" {
		t.Errorf("Profile.Name = %q, want acme-test (bp.Name)", plan.Profile.Name)
	}
	if plan.Profile.ReasoningEffort != "xhigh" {
		t.Errorf("Profile.ReasoningEffort = %q, want xhigh", plan.Profile.ReasoningEffort)
	}
	if plan.Profile.MaxConcurrent != 0 {
		t.Errorf("Profile.MaxConcurrent = %d, want 0 (global)", plan.Profile.MaxConcurrent)
	}
	if len(plan.ExtraProfiles) != 1 {
		t.Fatalf("len(ExtraProfiles) = %d, want 1", len(plan.ExtraProfiles))
	}
	wantExtra := ProfileSpec{
		Name:            "spark",
		Format:          "openai",
		Model:           "spark-llm",
		BaseURL:         "http://spark:8080/v1",
		APIKey:          "spark-key",
		ReasoningEffort: "xhigh",
		MaxConcurrent:   3,
	}
	if plan.ExtraProfiles[0] != wantExtra {
		t.Errorf("ExtraProfiles[0] = %+v, want %+v", plan.ExtraProfiles[0], wantExtra)
	}
	if plan.Settings.LLMPool != "spread" {
		t.Errorf("Settings.LLMPool = %q, want spread", plan.Settings.LLMPool)
	}

	// "" normalizes to "off"; "failover" passes through.
	for pool, want := range map[string]string{"": "off", "failover": "failover"} {
		bp.Runtime.LLMPool = pool
		p, err := BuildPlan(bp, secrets)
		if err != nil {
			t.Fatalf("BuildPlan(pool=%q): %v", pool, err)
		}
		if p.Settings.LLMPool != want {
			t.Errorf("pool %q -> %q, want %q", pool, p.Settings.LLMPool, want)
		}
	}
}
