// Command artex runs the ScopeWeaver backend: the PostgreSQL graph stores,
// the event-driven exploration engine, and the JSON HTTP API consumed by the
// shadcn/ui frontend.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/blueprint"
	"github.com/Autumn-27/artex/config"
	"github.com/Autumn-27/artex/provision"
	"github.com/Autumn-27/artex/selfupdate"
	"github.com/Autumn-27/artex/server"
)

// version is the build version, injected at release time via
// -ldflags "-X main.version=<tag>". Defaults to "dev" for local builds.
var version = "dev"

const banner = `
  ScopeWeaver
`

// printBanner writes the startup banner + version/runtime info to stdout.
func printBanner(addr string) {
	fmt.Print(banner)
	fmt.Println(locale.Text(locale.ServerDefault(), "  Autonomous AI penetration-testing system"))
	fmt.Printf(locale.Text(locale.ServerDefault(), "  Version %s  ·  %s/%s  ·  %s  ·  Listening on %s\n\n"),
		version, runtime.GOOS, runtime.GOARCH, runtime.Version(), addr)
}

// main only maps run's result onto the process exit code. The exit code is part
// of the update protocol — the supervising start script reads it to decide
// whether to relaunch us (see selfupdate.ExitRestart) — so the body has to live
// in a function that can *return* rather than os.Exit past its own defers.
func main() {
	lang, _ := locale.FromEnv(os.Getenv)
	locale.SetServerDefault(lang)
	os.Exit(run())
}

func run() int {
	var (
		addr    = flag.String("addr", ":8787", "HTTP listen address")
		dataDir = flag.String("data", filepath.Join(config.BaseDir(), "data"), "data directory for SQLite stores (default: data/ next to the executable)")
		proxy   = flag.String("proxy", "127.0.0.1:8788", "traffic recording proxy address (empty to disable)")
		// blueprintPath, when set, seeds this instance from a provisioning
		// blueprint YAML (admin auth, company+scope, constraints, LLM profile,
		// task) and admits the task at boot. Empty = unchanged behaviour.
		blueprintPath = flag.String("blueprint", "", "path to a provisioning blueprint YAML; seed and start the instance at boot (empty to disable)")
		// deliverableDir, when set, enables the deliverable bundle export:
		// POST /api/instance/deliverable writes the run's findings, evidence
		// and assets into a timestamped directory under here (task D.4a).
		deliverableDir = flag.String("deliverable-dir", "", "directory the deliverable bundle is written to (empty disables)")
	)
	flag.Parse()

	// hand the build version to the server package so GET /api/health can report it
	// to the frontend top bar.
	server.BuildVersion = version

	printBanner(*addr)

	// capture backend logs into the in-memory sink (still to stderr) so the /logs
	// page can show a live log stream. Do this first, to catch startup logs too.
	server.StartLogCapture()

	// Self-update bootstrap: swap in a staged binary, or count a post-swap boot
	// attempt and roll back if the new build keeps dying. Must run before we open
	// the stores or bind a port — this may end with "exit and let the start script
	// relaunch me", and there is no point paying for either first.
	action, upState := selfupdate.Bootstrap()
	server.SetBootUpdateState(upState)
	if action == selfupdate.Restart {
		return selfupdate.ExitRestart
	}

	// surface which config file the binary reads (absolute, so `go run`'s relative
	// "config.json" — resolved against the CWD — is unambiguous).
	cfgPath := config.Path()
	if abs, e := filepath.Abs(cfgPath); e == nil {
		cfgPath = abs
	}
	if _, e := os.Stat(cfgPath); e == nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[config] Configuration file: %s"), cfgPath)
	} else {
		log.Printf(locale.Text(locale.ServerDefault(), "[config] Configuration file: %s (not found; using ARTEX_PG_DSN only)"), cfgPath)
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, shutdown := shutdownContext(sigCtx)
	defer shutdown(agent.AbortShutdown)

	mgr, err := server.NewManager(*dataDir, *proxy)
	if err != nil {
		log.Fatalf("open stores: %v", err)
	}
	defer mgr.Close()

	// Surviving this long means a freshly swapped-in build actually works, so drop
	// the upgrade marker and stop counting attempts. Until it fires, every boot
	// increments the count and a build that keeps dying gets rolled back.
	settle := time.AfterFunc(selfupdate.SettleDelay, selfupdate.Settle)
	defer settle.Stop()

	skillDir := config.SkillDir()
	if abs, err := filepath.Abs(skillDir); err == nil {
		skillDir = abs
	}
	log.Printf(locale.Text(locale.ServerDefault(), "[config] Skill directory: %s"), skillDir)
	srv := server.New(ctx, mgr, skillDir, *dataDir, config.BaseDir())
	srv.SetDeliverableDir(*deliverableDir)

	// Blueprint boot mode: seed and admit the instance before serving. The stores
	// are open and restoreTaskRuntimes has resumed any prior run; nothing is
	// served yet. Any failure is fatal — a half-provisioned instance must not run.
	if *blueprintPath != "" {
		bp, plan, err := loadBlueprintPlan(*blueprintPath)
		if err != nil {
			log.Fatalf("blueprint: %v", err)
		}
		if err := srv.ApplyBlueprint(bp, plan); err != nil {
			log.Fatalf("blueprint: apply: %v", err)
		}
		// Watch the admitted task and emit the completion marker when it goes
		// terminal, so the orchestrator can detect completion without the UI.
		srv.WatchBlueprintCompletion(5 * time.Second)
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("ScopeWeaver %s backend listening on %s (data=%s, workers=%d)", version, *addr, *dataDir, mgr.Workers())
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	// Two ways out: a signal (normal stop → exit 0, the start script stops looping)
	// or a staged update / rollback (→ exit 75, the script relaunches us and the
	// bootstrap above installs the new build).
	code := 0
	select {
	case <-ctx.Done():
	case <-server.RestartRequested():
		code = selfupdate.ExitRestart
		shutdown(agent.AbortShutdown)
	}

	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	return code
}

// loadBlueprintPlan loads, validates and translates a provisioning blueprint
// into the plan ApplyBlueprint applies. blueprint.Load already runs Validate and
// returns the aggregated validation error, so a non-nil error here means the
// file was unreadable, malformed, or invalid — the caller fails the boot. Any
// non-fatal validation warnings are logged.
func loadBlueprintPlan(path string) (*blueprint.Blueprint, *provision.Plan, error) {
	bp, warnings, err := blueprint.Load(path)
	if err != nil {
		return nil, nil, err
	}
	for _, w := range warnings {
		log.Printf(locale.Text(locale.ServerDefault(), "[blueprint] warning: %s"), w)
	}
	secrets, err := provision.ResolveSecrets(bp)
	if err != nil {
		return nil, nil, err
	}
	plan, err := provision.BuildPlan(bp, secrets)
	if err != nil {
		return nil, nil, err
	}
	return bp, plan, nil
}

// shutdownContext deliberately does not derive from signalCtx. If it did, the
// parent's plain context.Canceled could win the race before AbortShutdown was
// attached to the child, losing the diagnostic cause in every running Agent.
func shutdownContext(signalCtx context.Context) (context.Context, context.CancelCauseFunc) {
	ctx, shutdown := context.WithCancelCause(context.Background())
	go func() {
		select {
		case <-signalCtx.Done():
			shutdown(agent.AbortShutdown)
		case <-ctx.Done():
		}
	}()
	return ctx, shutdown
}
