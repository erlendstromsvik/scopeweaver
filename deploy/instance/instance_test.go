// Package instance_test exercises the per-instance deployment tooling added
// by D.5a: the compose project template and the sw-instance.sh launcher.
// Everything runs in --dry-run mode (no Docker, no network); the tests
// check the generated instance state, the printed commands, and that the
// compose YAML itself is well-formed and secret-free in `environment:`.
package instance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	// deploy/instance -> repo root.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

func scriptPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "scripts", "sw-instance.sh")
}

func composePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "deploy", "instance", "compose.yml")
}

// runScript runs sw-instance.sh with the given args and env overrides,
// returning stdout+stderr and the exit error.
func runScript(t *testing.T, env map[string]string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{scriptPath(t)}, args...)...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestLaunchDryRunCreatesInstanceState runs the launcher in --dry-run mode
// and asserts the instance directory it creates holds the right files:
// 600-mode random secrets, a config.json whose DSN embeds the pg password
// and points at host `postgres`, and a copy of the blueprint. It also
// asserts the compose up command is printed with the sw- project name.
func TestLaunchDryRunCreatesInstanceState(t *testing.T) {
	root := repoRoot(t)
	home := t.TempDir()
	bp := filepath.Join(root, "deploy", "instance", "testdata", "blueprint-file-secrets.yaml")

	out, err := runScript(t, map[string]string{"SW_HOME": home},
		"--dry-run", "launch", "BP="+bp, "DELIVERABLES=/tmp/sw-test-deliverables")
	if err != nil {
		t.Fatalf("dry-run launch failed: %v\noutput:\n%s", err, out)
	}

	// Exactly one instance dir was created under SW_HOME.
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read SW_HOME: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 instance dir, got %d: %v", len(entries), entries)
	}
	id := entries[0].Name()
	if !strings.HasPrefix(id, "sw-") {
		t.Errorf("instance id %q does not start with sw-", id)
	}
	dir := filepath.Join(home, id)

	// secrets/admin_password: 32 chars, mode 0600.
	for _, name := range []string{"admin_password", "pg_password"} {
		b, err := os.ReadFile(filepath.Join(dir, "secrets", name))
		if err != nil {
			t.Fatalf("read secret %s: %v", name, err)
		}
		pw := strings.TrimSpace(string(b))
		if len(pw) != 32 {
			t.Errorf("secret %s has length %d, want 32", name, len(pw))
		}
		st, err := os.Stat(filepath.Join(dir, "secrets", name))
		if err != nil {
			t.Fatalf("stat secret %s: %v", name, err)
		}
		if perm := st.Mode().Perm(); perm != 0o600 {
			t.Errorf("secret %s mode %o, want 600", name, perm)
		}
	}

	// sw/config.json: DSN with the pg password and host `postgres`.
	cfgRaw, err := os.ReadFile(filepath.Join(dir, "sw", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var cfg struct {
		Database struct {
			DSN string `yaml:"dsn"`
		} `yaml:"database"`
	}
	if err := yaml.Unmarshal(cfgRaw, &cfg); err != nil {
		t.Fatalf("config.json is not valid JSON/YAML: %v", err)
	}
	if !strings.Contains(cfg.Database.DSN, "postgres://scopeweaver:") {
		t.Errorf("DSN %q does not use the scopeweaver user on host postgres", cfg.Database.DSN)
	}
	pgPw := strings.TrimSpace(string(mustRead(t, filepath.Join(dir, "secrets", "pg_password"))))
	if !strings.Contains(cfg.Database.DSN, pgPw) {
		t.Error("DSN does not embed the generated pg password")
	}
	if !strings.Contains(cfg.Database.DSN, "@postgres:5432/") {
		t.Errorf("DSN %q does not point at host postgres:5432", cfg.Database.DSN)
	}

	// sw/blueprint.yaml: a copy of the launched blueprint.
	cp, err := os.ReadFile(filepath.Join(dir, "sw", "blueprint.yaml"))
	if err != nil {
		t.Fatalf("read blueprint copy: %v", err)
	}
	orig, err := os.ReadFile(bp)
	if err != nil {
		t.Fatalf("read source blueprint: %v", err)
	}
	if string(cp) != string(orig) {
		t.Error("copied blueprint differs from source")
	}

	// stdout must show the compose up command with the sw- project name.
	for _, want := range []string{"docker compose -p sw-", "up -d"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q\noutput:\n%s", want, out)
		}
	}
	// The printed compose command must use the per-instance compose file.
	if !strings.Contains(out, filepath.Join("deploy", "instance", "compose.yml")) {
		t.Errorf("stdout missing the per-instance compose file path\noutput:\n%s", out)
	}
}

// TestLaunchRejectsEnvSecretBlueprint: a blueprint whose admin.password is
// an ${env:…} ref must be refused (D6 — instances use file secrets).
func TestLaunchRejectsEnvSecretBlueprint(t *testing.T) {
	root := repoRoot(t)
	home := t.TempDir()
	bp := filepath.Join(root, "deploy", "instance", "testdata", "blueprint-env-secrets.yaml")

	out, err := runScript(t, map[string]string{"SW_HOME": home},
		"--dry-run", "launch", "BP="+bp)
	if err == nil {
		t.Fatalf("expected non-zero exit for ${env:…} admin password; output:\n%s", out)
	}
	if !strings.Contains(out, "file secret ref") {
		t.Errorf("error message does not mention the required file secret ref\noutput:\n%s", out)
	}
	// No instance dir should have been created.
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Errorf("expected no instance dirs, got %v", entries)
	}
}

// TestBashSyntax: `bash -n` on the launcher passes.
func TestBashSyntax(t *testing.T) {
	cmd := exec.Command("bash", "-n", scriptPath(t))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n failed: %v\n%s", err, out)
	}
}

// TestComposeYAMLValidAndSecretFree parses deploy/instance/compose.yml and
// asserts the two services exist and no service places a secret-looking
// value in `environment:` (D6).
func TestComposeYAMLValidAndSecretFree(t *testing.T) {
	raw, err := os.ReadFile(composePath(t))
	if err != nil {
		t.Fatalf("read compose.yml: %v", err)
	}
	var doc struct {
		Services map[string]struct {
			Image       string         `yaml:"image"`
			Environment map[string]any `yaml:"environment"`
			Ports       []string       `yaml:"ports"`
			Volumes     []string       `yaml:"volumes"`
			Command     []string       `yaml:"command"`
		} `yaml:"services"`
		Networks map[string]struct {
			Internal bool `yaml:"internal"`
		} `yaml:"networks"`
		Volumes map[string]any `yaml:"volumes"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("compose.yml is not valid YAML: %v", err)
	}
	for _, svc := range []string{"postgres", "app"} {
		if _, ok := doc.Services[svc]; !ok {
			t.Errorf("service %q missing from compose.yml", svc)
		}
	}

	// No secret material in any service's environment.
	secretSubstrs := []string{"password", "secret", "api_key", "token"}
	for name, svc := range doc.Services {
		for k, v := range svc.Environment {
			// *_FILE entries are pointers to mounted secret files, not the
			// secret itself — that is exactly the D6 pattern.
			if strings.HasSuffix(strings.ToUpper(k), "_FILE") {
				continue
			}
			joined := strings.ToLower(k)
			if s, ok := v.(string); ok {
				joined += " " + strings.ToLower(s)
			}
			for _, sub := range secretSubstrs {
				if strings.Contains(joined, sub) {
					t.Errorf("service %q environment entry %q=%v looks like a secret (D6: files only)", name, k, v)
				}
			}
		}
	}

	// Postgres must have no host ports; the app must publish only on loopback.
	if len(doc.Services["postgres"].Ports) != 0 {
		t.Error("postgres must not expose host ports")
	}
	for _, p := range doc.Services["app"].Ports {
		if !strings.HasPrefix(p, "127.0.0.1::") {
			t.Errorf("app port %q is not a loopback-only random port", p)
		}
	}

	// The app command carries the blueprint and deliverable flags.
	joined := strings.Join(doc.Services["app"].Command, " ")
	for _, want := range []string{"-blueprint /run/sw/blueprint.yaml", "-deliverable-dir /deliverables"} {
		if !strings.Contains(joined, want) {
			t.Errorf("app command missing %q (got %q)", want, joined)
		}
	}

	// Postgres reads its password from a file.
	pgEnv := doc.Services["postgres"].Environment
	if v, ok := pgEnv["POSTGRES_PASSWORD_FILE"].(string); !ok || v != "/run/secrets/pg_password" {
		t.Errorf("postgres POSTGRES_PASSWORD_FILE = %v, want /run/secrets/pg_password", pgEnv["POSTGRES_PASSWORD_FILE"])
	}

	// The internal network is internal; egress exists for the app.
	if !doc.Networks["internal"].Internal {
		t.Error("network `internal` must be internal: true")
	}
	if _, ok := doc.Networks["egress"]; !ok {
		t.Error("network `egress` missing (D.6 restricts it)")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
