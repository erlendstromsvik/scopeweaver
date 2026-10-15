package server

import (
	"log"

	"github.com/Autumn-27/artex/blueprint"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/locale"
	"github.com/Autumn-27/artex/provision"
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
func blueprintTaskOptions(plan *provision.Plan, companyID, profileID int64) db.TaskCreateOptions {
	// CoverageEnabled is a *bool so unaware callers can default it; take the
	// address of a local copy of the plan's explicit value.
	coverage := plan.Task.CoverageEnabled
	return db.TaskCreateOptions{
		CompanyIDs:           []int64{companyID},
		LLMProfileIDs:        []int64{profileID},
		TimeoutSeconds:       plan.Task.TimeoutSeconds,
		PlanHeartbeatSeconds: plan.Task.PlanHeartbeatSeconds,
		CoverageEnabled:      &coverage,
	}
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

	// 5. LLM profile. There is no GetProfileByName, so match the existing list by
	// name; else save a new profile. SaveProfile does not set is_default, so
	// always SetActiveProfile — otherwise the engine has no active provider.
	profiles, err := pg.ListProfiles()
	if err != nil {
		return locale.Errorf("blueprint provisioning: list LLM profiles: %w", err)
	}
	var profileID int64
	for _, p := range profiles {
		if p.Name == bp.Name {
			profileID = p.ID
			break
		}
	}
	if profileID == 0 {
		id, err := pg.SaveProfile(&db.LLMProfile{
			Name:      bp.Name,
			Format:    plan.Profile.Format, // already resolved from llm.provider
			Model:     plan.Profile.Model,
			BaseURL:   plan.Profile.BaseURL,
			APIKey:    plan.Profile.APIKey,
			MaxTokens: plan.Profile.MaxTokens,
		})
		if err != nil {
			return locale.Errorf("blueprint provisioning: save LLM profile: %w", err)
		}
		profileID = id
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
	// optional first intent, background goal decomposition, and admitTask.
	s.launchTask(task, plan.Task.Description+" "+plan.Task.Goal, plan.Task.SeedFirstIntent)
	log.Printf(locale.Text(locale.ServerDefault(), "[blueprint] provisioned task #%s for company %q on profile %q"),
		task.ID, plan.CompanyName, bp.Name)
	return nil
}
