package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/agent"
)

func TestShutdownContextPreservesNamedCause(t *testing.T) {
	signalCtx, signalCancel := context.WithCancel(context.Background())
	ctx, shutdown := shutdownContext(signalCtx)
	defer shutdown(nil)
	signalCancel()
	<-ctx.Done()
	if code, _, _, ok := agent.AbortReason(ctx); !ok || code != "shutdown" {
		t.Fatalf("code=%q ok=%v, want shutdown", code, ok)
	}
}

// TestLoadBlueprintPlanErrors verifies the boot-time blueprint loader fails
// clearly for a missing file, a malformed file, and a schema-invalid file —
// each of which must abort the boot (main calls log.Fatalf). DB-free.
func TestLoadBlueprintPlanErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		if _, _, err := loadBlueprintPlan(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
			t.Fatal("got nil error for a missing blueprint path, want an error")
		}
	})

	t.Run("malformed yaml", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.yaml")
		writeFile(t, path, "version: 1\nname: [unterminated\n")
		if _, _, err := loadBlueprintPlan(path); err == nil {
			t.Fatal("got nil error for malformed YAML, want an error")
		}
	})

	t.Run("schema invalid", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid.yaml")
		// Valid YAML but fails blueprint.Validate (wrong version, no name, no
		// authorization, no scope, etc.). blueprint.Load returns the aggregated
		// *ValidationError.
		writeFile(t, path, "version: 2\n")
		_, _, err := loadBlueprintPlan(path)
		if err == nil {
			t.Fatal("got nil error for a schema-invalid blueprint, want an error")
		}
		if !strings.Contains(err.Error(), "blueprint invalid") {
			t.Errorf("error %q does not look like a validation error", err)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
