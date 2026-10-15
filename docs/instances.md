# Ephemeral blueprint instances

A blueprint describes one engagement end to end. `scripts/sw-instance.sh`
turns one blueprint into one throwaway ScopeWeaver: its own app container,
its own Postgres, its own volumes and networks. When the run is over you
destroy the instance, and only the deliverable bundle stays on the host.

This page covers the whole cycle: what an instance is, what you need before
launching one, how to write the blueprint, the commands, what the
deliverable contains, and what is not yet enforced.

## What an instance is

One instance = one Docker Compose project, named after the instance id
(`sw-<UTC timestamp>-<hex>`):

- **App** — the ScopeWeaver server, started with `-blueprint
  /run/sw/blueprint.yaml` and `-deliverable-dir /deliverables`. Its HTTP
  port 8787 is published on `127.0.0.1` only, on a random host port.
- **Postgres** — `postgres:16-alpine`, on an `internal` network with **no
  host ports**. Reachable only from the app container.
- **Volumes** — `<id>-pgdata` and `<id>-appdata`, both named per instance,
  so two instances never share state.
- **Networks** — `internal` (app ↔ Postgres, `internal: true`, no route
  out) and `egress` (app ↔ the local LLM and the declared target scope).

Everything the instance writes lives in those volumes or in the host
deliverables directory. `destroy` runs `docker compose down -v
--remove-orphans` and deletes the instance's state directory, including
its generated secrets. The deliverable is deliberately outside the
instance directory, so it survives (owner decision D4: kept until you
delete it by hand).

## Prerequisites

- **Docker with the Compose plugin.** The launcher calls `docker compose`
  (`up -d`, `down -v`, `port`, `ls`).
- **The app image, tagged `scopeweaver:local`.** The compose file defaults
  to it; override with `SW_IMAGE=<tag>`. Build it from this checkout with
  the repo [Dockerfile](../Dockerfile), e.g.:

  ```bash
  docker build -t scopeweaver:local .
  ```

  (The plain `docker compose up -d --build` flow in the main README builds
  the `scopeweaver` image for the long-lived install; the instance flow
  needs the `:local` tag, or `SW_IMAGE` pointed at whatever you built.)
- **A local LLM endpoint** reachable from the container — Ollama or any
  OpenAI-compatible server. The blueprint binds it (see below).
- **A blueprint you are authorized to run.** Real blueprints stay outside
  this repository; `blueprint/examples/example.yaml` is the generic
  template. Never point an instance at a target without written
  authorization.

## The blueprint

Fields and their validation rules are documented inline in
[`blueprint/examples/example.yaml`](../blueprint/examples/example.yaml).
Three things matter specifically for instances:

**Secrets are file references only.** The launcher refuses a blueprint
whose `admin.password` is not a `${file:…}` ref:

```yaml
admin:
  password: ${file:/run/secrets/admin_password}
```

`${env:…}` refs are rejected by `launch` (owner decision D6). The instance
reads secrets as read-only files mounted at `/run/secrets`, so nothing
secret appears in `docker inspect` output or process listings. The same
rule applies to `llm.api_key` when the provider needs one:

```yaml
llm:
  provider: openai-compatible
  base_url: http://192.168.1.10:11434/v1
  api_key: ${file:/run/secrets/llm_api_key}
```

**`standards:` selects test profiles.** Optional. Each name must be a
profile in `standards/profiles.yaml`; unknown names fail validation. The
three shipped profiles are `asvs-l1-web`, `asvs-l2-web` and `asvs-l2-api`.
At provisioning, every requirement the selected profiles select is seeded
as a `not_tested` control result, so coverage is visible from the first
boot, and requirements flagged as not automatable carry a reason.

```yaml
standards: [asvs-l2-web]
```

**`llm.api_key` may be omitted for `provider: ollama`.** Ollama needs no
key; every other provider still requires a secret ref, and inline keys are
rejected.

## The cycle

All commands run from the repo root. The `make` targets are thin wrappers
over `scripts/sw-instance.sh`; the script also accepts `--dry-run` before
the subcommand, which prints every `docker`/`curl`/`go` command instead of
running it while still creating the instance directory, so you can inspect
the generated files first.

### 1. Sync the advisory register (optional)

```bash
make advisory-sync
```

Refreshes the NSM register (`advisory/register/nsm.jsonl`) and the CISA
KEV snapshot on the **host**. Needs network access, which the instance is
not supposed to have, so this runs before the container boots. `launch`
runs it for you; run it by hand only if you want a fresh register without
launching.

### 2. Launch

```bash
make instance BP=blueprint.yaml
# or directly:
./scripts/sw-instance.sh launch BP=blueprint.yaml DELIVERABLES=~/my-deliverables
```

`DELIVERABLES` is optional; the default is `~/scopeweaver-deliverables`.
The launcher generates the instance id (`sw-<UTC>-<hex>`), the admin and
Postgres passwords, writes the state, syncs the advisory register, and
brings the compose project up. It prints:

```
instance:  sw-20261016-091500-a1b2
url:       http://127.0.0.1:55012
secrets: /Users/you/.scopeweaver/instances/sw-20261016-091500-a1b2/secrets (admin_password, pg_password; mode 600)
deliverables: /Users/you/scopeweaver-deliverables/sw-20261016-091500-a1b2
login:   username ARTEX, password in .../secrets/admin_password
```

**Where secrets live:** `~/.scopeweaver/instances/<id>/secrets/` — the
instance directory and the `secrets` and `sw` subdirectories are mode 700;
each secret file is written under `umask 077` (mode 600). The files are
`admin_password`, `pg_password`, and `llm_api_key` if you supplied
`SW_LLM_API_KEY_FILE=<host file>` in the environment before launching.
`destroy` deletes the whole directory.

The instance state root is `~/.scopeweaver/instances` (override with
`SW_HOME`).

### 3. Watch it

```bash
./scripts/sw-instance.sh status ID=sw-20261016-091500-a1b2
```

Logs in as `ARTEX` with the generated admin password and prints
`GET /api/instance/status` — the provisioned task and its state.

### 4. Land the deliverable

```bash
./scripts/sw-instance.sh report ID=sw-20261016-091500-a1b2
```

Calls `POST /api/instance/deliverable` and prints the bundle directory
name and its host path. The task must be terminal; the endpoint answers 409
if the instance is not provisioned, the task id is missing, the task is
not finished, or the bundle already exists.

### 5. Destroy

```bash
./scripts/sw-instance.sh destroy ID=sw-20261016-091500-a1b2
```

`docker compose down -v --remove-orphans`, then `rm -rf` of the instance
directory. Containers, volumes and networks go; the deliverables directory
is kept.

### 6. List

```bash
./scripts/sw-instance.sh ls
```

Prints the instance directories under `SW_HOME` and the running `sw-`
compose projects.

**Make targets vs. script.** The Makefile ships one launch target,
`instance` (`./scripts/sw-instance.sh launch $(BP)`). The other
subcommands are invoked through the script directly:

```bash
./scripts/sw-instance.sh status  ID=<instance-id>
./scripts/sw-instance.sh report  ID=<instance-id>
./scripts/sw-instance.sh destroy ID=<instance-id>
./scripts/sw-instance.sh ls
```

## What the deliverable contains

One bundle directory per export, named `<instance>-<UTC timestamp>`,
staged in a `.partial-*` temp directory and renamed into place only after
every file is on disk — you see a complete bundle or nothing. Contents:

| File | What it is |
| --- | --- |
| `findings.md` | The findings report. |
| `findings.csv` | The same findings as CSV. |
| `findings-evidence.zip` | Per-finding traffic evidence (`evidence/<finding_id>/<binding_id>/…`). |
| `assets.csv` | The asset inventory. |
| `task-archive.tar.zst` | A copy-only snapshot of the task's full archive; the live task is left intact and stays restorable. |
| `manifest.json` | Written last: instance, task id, task status at export time, creation timestamp, and size + SHA-256 for every file above. |

Verify a bundle with the checksums in `manifest.json` before trusting it.

The coverage report is a separate endpoint on the running instance, not a
bundle file:

```
GET /api/tasks/{id}/coverage-report?format=md|json|csv
```

`format` defaults to `md`; `asvs_level` takes 1–3 (default 2). A
blueprint-provisioned instance with no `asvs_level` parameter takes its
scope from its own `standards:` profiles; an explicit `asvs_level` wins.
Requires an authenticated request (the same JWT the script obtains).

## Limits, stated plainly

- **Egress is not enforced.** The `egress` network in
  `deploy/instance/compose.yml` is a plain bridge network. The allowlist
  that restricts it to the local LLM plus the declared target scope is
  task D.6, still open (owner decision D5). Until then, an instance can
  reach anything the host can reach.
- **Docker verification of these scripts is pending.** The launcher and
  compose file are covered by dry-run tests (state files, secret modes,
  the generated compose command, secret-free environment). The real
  launch/status/report/destroy cycle, concurrent instances, and the
  end-to-end run are a controller check on a machine with a Docker daemon.
- **The advisory register only knows what the last sync fetched.** The
  instance embeds `advisory/register/nsm.jsonl` and the KEV snapshot as
  they were on the host when it booted; it never fetches. A stale register
  means the Advisories section of the coverage report is stale. Run
  `make advisory-sync` before launching.
