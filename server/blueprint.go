package server

import (
	"log"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/blueprint"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/artex/provision"
	"github.com/Autumn-27/artex/standards"
)

// blueprintProvisionedKey is the settings row that marks a database as already
// seeded from a blueprint. Its presence is the idempotency guard: a second boot
// against the same database skips all seeding (restoreTaskRuntimes has already
// resumed the prior run).
const blueprintProvisionedKey = "blueprint.provisioned"

// blueprintTaskIDKey records the id of the task this blueprint admitted, so the
// instance status endpoint and the completion watcher can find it without the UI.
const blueprintTaskIDKey = "blueprint.task_id"

// scopeSpecsToInputs maps neutral provisioning scope specs onto the db scope
// input rows CreateCompanyWithScope expects, 1:1. Pure; DB-free.
func scopeSpecsToInputs(specs []provision.ScopeSpec) []db.ScopeInput {
	inputs := make([]db.ScopeInput, len(specs))
	for i, s := range specs {
		inputs[i] = db.ScopeInput{Kind: s.Kind, Value: s.Value}
	}
	return inputs
}

// blueprintTaskOptions builds the task-creation options that bind the seeded
// task to the provisioned company and LLM profile. Description/Goal are passed
// positionally to CreateTaskWithOptions, not here. Pure; DB-free.
//
// Pool mode changes the binding: with llm_pool off the task pins the main
// profile; with the pool on, LLMProfileIDs stays nil so the task runs on the
// pooled global provider (a task bound to one profile bypasses the pool —
// see server/task_llm.go current()).
func blueprintTaskOptions(plan *provision.Plan, companyID, profileID int64) db.TaskCreateOptions {
	// CoverageEnabled is a *bool so unaware callers can default it; take the
	// address of a local copy of the plan's explicit value.
	coverage := plan.Task.CoverageEnabled
	opts := db.TaskCreateOptions{
		CompanyIDs:           []int64{companyID},
		TimeoutSeconds:       plan.Task.TimeoutSeconds,
		PlanHeartbeatSeconds: plan.Task.PlanHeartbeatSeconds,
		CoverageEnabled:      &coverage,
	}
	if plan.Settings.LLMPool == "off" {
		opts.LLMProfileIDs = []int64{profileID}
	}
	return opts
}

// ApplyBlueprint idempotently seeds this instance from a validated blueprint and
// its resolved plan, then admits the task. It is the DB/server half of task D.2
// (phase 2b): main.go does blueprint.Load -> Validate -> provision.ResolveSecrets
// -> provision.BuildPlan, then calls this once, after the stores are open and
// restoreTaskRuntimes has run, but before the HTTP server starts serving.
//
// The method lives in package server because launchTask/admitTask are
// unexported. Any error is returned so the caller can fail the boot (fail fast).
func (s *Server) ApplyBlueprint(bp *blueprint.Blueprint, plan *provision.Plan) error {
	if bp == nil || plan == nil {
		return locale.Errorf("blueprint provisioning: nil blueprint or plan")
	}
	pg := s.m.PG()

	// 1. Idempotency guard. The primary key makes this the atomic marker: the
	// first boot inserts it and seeds; every later boot sees it and skips.
	inserted, err := pg.InsertSettingIfAbsent(blueprintProvisionedKey, bp.Name)
	if err != nil {
		return locale.Errorf("blueprint provisioning: record marker: %w", err)
	}
	if !inserted {
		log.Printf(locale.Text(locale.ServerDefault(), "[blueprint] already provisioned (%s); skipping seed"), bp.Name)
		return nil
	}
	log.Printf(locale.Text(locale.ServerDefault(), "[blueprint] provisioning instance from blueprint %q"), bp.Name)

	// 2. Admin password. The plan already carries the bcrypt hash; store it only
	// if absent so a pre-set password is never overwritten. Do NOT re-hash.
	if _, err := pg.InsertSettingIfAbsent(authPassKey, plan.AdminPasswordHash); err != nil {
		return locale.Errorf("blueprint provisioning: seed admin password: %w", err)
	}

	// 3. Runtime settings.
	if err := s.m.SetWorkers(plan.Settings.Workers); err != nil {
		return locale.Errorf("blueprint provisioning: set workers: %w", err)
	}
	if err := s.m.SetLLMMaxConcurrent(plan.Settings.LLMMaxConcurrent); err != nil {
		return locale.Errorf("blueprint provisioning: set llm_max_concurrent: %w", err)
	}
	// Pool mode from runtime.llm_pool: enabled = anything but off; spread only
	// when explicitly "spread". reapplyActiveProfile below rebuilds the chain.
	pool := plan.Settings.LLMPool
	if err := s.m.SetLLMPoolEnabled(pool != "off"); err != nil {
		return locale.Errorf("blueprint provisioning: set llm_pool_enabled: %w", err)
	}
	if err := s.m.SetLLMPoolSpread(pool == "spread"); err != nil {
		return locale.Errorf("blueprint provisioning: set llm_pool_spread: %w", err)
	}

	// Manager setter so the in-memory webSearchOn flag updates too (not just the
	// persisted row). Backend "" keeps the default; nil keys leave any unset.
	if err := s.m.SetWebSearch(plan.Settings.WebSearchEnabled, "", nil, nil, nil); err != nil {
		return locale.Errorf("blueprint provisioning: set web_search_enabled: %w", err)
	}

	// 4. Company + scope. Reuse an existing company by name (idempotent); else
	// create it with its scope rules in one transaction.
	companies := pg.Companies()
	company, err := companies.GetCompanyByName(plan.CompanyName)
	if err != nil {
		return locale.Errorf("blueprint provisioning: look up company: %w", err)
	}
	var companyID int64
	if company != nil {
		companyID = company.ID
	} else {
		id, _, _, invalid, validationErrors, err := companies.CreateCompanyWithScope(
			plan.CompanyName, plan.CompanyLogo, scopeSpecsToInputs(plan.Scope), "blueprint")
		if err != nil {
			return locale.Errorf("blueprint provisioning: create company: %w", err)
		}
		if invalid > 0 {
			log.Printf(locale.Text(locale.ServerDefault(), "[blueprint] %d scope rule(s) were rejected: %v"), invalid, validationErrors)
		}
		companyID = id
	}

	// 5. LLM profiles. There is no GetProfileByName, so match the existing
	// list by name; else save a new profile (idempotent by name). SaveProfile
	// does not set is_default, so the MAIN profile is always the one passed
	// to SetActiveProfile — otherwise the engine has no active provider.
	// Extra LLMs (D.7b) are saved the same way with the main profile's
	// priority (0), so spread mode can share the lead across them.
	profiles, err := pg.ListProfiles()
	if err != nil {
		return locale.Errorf("blueprint provisioning: list LLM profiles: %w", err)
	}
	profileID, err := ensureBlueprintProfile(pg, profiles, plan.Profile)
	if err != nil {
		return locale.Errorf("blueprint provisioning: save LLM profile: %w", err)
	}
	for _, ex := range plan.ExtraProfiles {
		if _, err := ensureBlueprintProfile(pg, profiles, ex); err != nil {
			return locale.Errorf("blueprint provisioning: save extra LLM profile %q: %w", ex.Name, err)
		}
	}
	if err := pg.SetActiveProfile(profileID); err != nil {
		return locale.Errorf("blueprint provisioning: activate LLM profile: %w", err)
	}

	// Hot-reload the engine from the now-active profile so the global provider /
	// readiness indicator reflects the seeded LLM and the web-search change takes
	// hold. Best-effort (the task also runs via its explicit profile chain).
	s.reapplyActiveProfile()

	// 6. Task, bound to the company and profile above.
	task, err := s.m.CreateTaskWithOptions(plan.Task.Description, plan.Task.Goal,
		blueprintTaskOptions(plan, companyID, profileID))
	if err != nil {
		return locale.Errorf("blueprint provisioning: create task: %w", err)
	}

	// Record the task id so the instance status endpoint and the completion
	// watcher (D.3) can locate the blueprint's task without the UI.
	if err := pg.SetSetting(blueprintTaskIDKey, task.ID); err != nil {
		return locale.Errorf("blueprint provisioning: record task id: %w", err)
	}

	// 6b. Standards coverage seed. Every requirement the blueprint's test
	// profiles select becomes a not_tested control result owned by the
	// system, so coverage is visible from the first boot. Requirements the
	// profiles flag as not automatable carry their reason.
	if len(plan.Standards) > 0 {
		taskID, err := strconv.ParseInt(task.ID, 10, 64)
		if err != nil {
			return locale.Errorf("blueprint provisioning: parse task id: %w", err)
		}
		if err := pg.SetSetting("blueprint.standards", strings.Join(plan.Standards, ",")); err != nil {
			return locale.Errorf("blueprint provisioning: record standards: %w", err)
		}
		catalog, err := standards.Embedded()
		if err != nil {
			return locale.Errorf("blueprint provisioning: load standards catalog: %w", err)
		}
		reqs, err := catalog.SelectRequirements(plan.Standards)
		if err != nil {
			return locale.Errorf("blueprint provisioning: select standards requirements: %w", err)
		}
		for _, req := range reqs {
			if _, err := pg.UpsertControlResult(db.ControlResultInput{
				TaskID:        taskID,
				Standard:      req.Standard,
				RequirementID: req.ID,
				Verdict:       db.VerdictNotTested,
				Actor:         "system",
				Reason:        req.NotAutomatableReason,
			}); err != nil {
				return locale.Errorf("blueprint provisioning: seed control result %s/%s: %w",
					req.Standard, req.ID, err)
			}
		}
	}

	// 7. Operation constraints, authored by the system (the blueprint).
	for _, c := range plan.Constraints {
		if _, err := task.Store.AddConstraint(c.Kind, c.Text, "system"); err != nil {
			return locale.Errorf("blueprint provisioning: add constraint: %w", err)
		}
	}

	// 8. Authorization audit trail. Validate (D.1) already rejected a blueprint
	// with no authorization, so these are present; record them for traceability.
	if az := bp.Authorization; az != nil {
		if err := pg.SetSetting("blueprint.authorization.reference", az.Reference); err != nil {
			return locale.Errorf("blueprint provisioning: record authorization reference: %w", err)
		}
		if err := pg.SetSetting("blueprint.authorization.authorized_by", az.AuthorizedBy); err != nil {
			return locale.Errorf("blueprint provisioning: record authorized_by: %w", err)
		}
		if az.Window != nil {
			window := az.Window.Start + "/" + az.Window.End
			if err := pg.SetSetting("blueprint.authorization.window", window); err != nil {
				return locale.Errorf("blueprint provisioning: record authorization window: %w", err)
			}
		}
	}

	// 9. Admit the task — mirrors the HTTP create handler: seed root assets, the
	// optional first intent, background goal decomposition, and admitTask. Seed
	// from the blueprint SCOPE, not the goal prose: parsing the goal text would
	// pick the first domain-shaped token (e.g. "Next.js" -> next.js), which can
	// be an out-of-scope third party.
	s.launchTaskSeeded(task, func() { s.seedScope(task, plan.Scope) }, plan.Task.SeedFirstIntent)
	log.Printf(locale.Text(locale.ServerDefault(), "[blueprint] provisioned task #%s for company %q on profile %q"),
		task.ID, plan.CompanyName, bp.Name)
	return nil
}

// ensureBlueprintProfile returns the id of the profile named spec.Name,
// reusing an entry from the pre-fetched profiles list, or saving a new one
// when no profile with that name exists. Extra LLMs share the main profile's
// priority (0) so spread mode can share the lead across them.
func ensureBlueprintProfile(pg *db.DB, profiles []*db.LLMProfile, spec provision.ProfileSpec) (int64, error) {
	for _, p := range profiles {
		if p.Name == spec.Name {
			return p.ID, nil
		}
	}
	return pg.SaveProfile(&db.LLMProfile{
		Name:            spec.Name,
		Format:          spec.Format, // already resolved from llm.provider
		Model:           spec.Model,
		BaseURL:         spec.BaseURL,
		APIKey:          spec.APIKey,
		MaxTokens:       spec.MaxTokens,
		ReasoningEffort: spec.ReasoningEffort,
		MaxConcurrent:   spec.MaxConcurrent,
		Priority:        0,
	})
}
