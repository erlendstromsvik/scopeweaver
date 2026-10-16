# Verification and known limitations

[한국어](ko/VERIFICATION.md)

## Native hardening candidate — 2026-10-15

Local and independent checks on `feat/ollama-zero-egress-hardening`, based on main `33b131e`. These are candidate results; exact-head remote CI and PR delivery remain pending. Historical sections below retain their original results.

| Check actually run | Result |
| --- | --- |
| Independent Go run, 20 packages with isolated disposable PostgreSQL databases | 1,366 tests/subtests passed, 0 failures; `enrich` has no test files; 2 opt-in skips (`TestAgentAdapterE2E`, `TestLiveContextReview`) |
| Agent adapter, checked separately | 7 unit tests and 3 real MCP/Pi scenarios passed; external live-provider review intentionally not run |
| Focused backend `-race` suites | Passed with no race reports: transport, wire/usage, context, origin selection/failure, search/update and admission |
| `go vet ./...` | Passed |
| Frontend unit/localization and static export | 45 passed; 3,168 EN/KO/zh-Hans keys preserve parity, placeholders and rich tags; TypeScript and all 31 static pages passed |
| Backend catalog | 1,943 production keys passed raw three-language completeness/parity and printf checks |
| Installer, Compose and notices | 8 installer fixtures, 11 Compose credential cases, notice checks in 5 packaging-fixture archives per language (EN/KO), and 6 release-notice tests under Python 3.12 passed |
| Static builds and dependency check | macOS embedded build and Windows amd64 cross-build passed; no `go.mod`/`go.sum` drift; native Windows runtime not run |
| Embedded-browser smoke | Rebuilt final UI: 60 passed (48 smoke + 12 dashboard), 0 failures. Settled desktop/mobile screenshots and horizontal bounds passed in EN/KO/zh-Hans: sheet [0,390] at 390px and [864,1440] at 1440px; all 20 critical contents fit. The earlier 420px minimum-width clipping was reproduced and corrected |

The [native HTTP priority regression](../server/ollama_priority_test.go) holds worker 1 inside `/api/chat`, queues workers 2/3, then invokes the real wired judge without a caller-supplied urgency marker. It passed under `-race` with HTTP order **worker 1 → judge → worker 2 → worker 3**, peak concurrency 1, a valid native judge schema/verdict, all calls completed and no leaked slot. Existing queue tests cover FIFO roles, cancellation and lowered limits.

The [native-origin failure regression](../server/provider_origin_failure_test.go) failed before the fix: a pending native tool followed by a stream error could inherit the global cloud `allow` default. After the fix, Stream/Complete and terminal routing errors retain the concrete native profile, 90-second/`ask` model policy and 300-second/`deny` human-approval expiry, without calling the reviewer. Actual cloud failover still clears native preference. The regression and affected race suites passed.

Real authenticated API checks toggled strict mode **false → true → false** against a local proxy that never forwards requests. All four search backends (`ddgs`, `brave-free`, `tavily`, `deepseek`) reached that sink only while strict mode was off; each produced zero additional sink requests while strict. Strict update checks and apply were blocked while local status remained available. [Update regressions](../server/update_policy_test.go) also passed for cached data, redirects, live body reads and cleanup/failure completion after an admitted asynchronous download is blocked by a later toggle. These checks do not claim whole-host egress control.

Host Ollama **0.32.15**, using existing `qwen3:4b` with a **4,096-token output budget**, passed native diagnostics, decision/comment schema, structured tool call/result replay, usage and requested 32K context (`/api/ps`: **32,768**). Earlier failures with 256/512-token budgets remain part of the evidence; they were not discarded as successful runs. The observed load/speed/offload values are one warm-run snapshot, not a benchmark or residency guarantee. No model was downloaded or host configuration changed, and no autonomous task completion or security-assessment quality is claimed.

Browser coverage includes three-language diagnostics/warnings, strict-update status, judge loading/failure protection and existing dashboard/usage flows. Visual QA reproduced a fixed 420px minimum-width editor extending 30px beyond the left edge of a 390px viewport. The minimum now fits the viewport; the rebuilt app passed all 60 checks, and all six desktop/mobile captures show readable labels with settled content inside the viewport. An independent embedded rebuild matched the browser binary byte for byte. Exact-head remote CI and PR readback are separate delivery gates. No merge, release, paid provider call, external target scan or real notification is claimed.

## Provider authentication follow-up — 2026-10-15

Local regression checks on `fix/provider-env-auth`, based on merged main `38fde23`, reproduce and fix two authenticated-local-gateway failures. Startup provider-key fallback and blank-key connection testing each returned HTTP 401 with the previous implementation for Anthropic, OpenAI Chat Completions and OpenAI Responses. The same fixtures pass with the fixes.

| Check | Result |
| --- | --- |
| [`FromEnv → NewProvider` local HTTP authentication](../agent/provider_env_test.go) | 21 scenario cases passed: 18 generic-format cases covering provider keys, file keys, modern/legacy overrides and explicit blank overrides; 3 native cases covering cloud-key isolation, an Ollama token and an explicit blank override |
| [Connection-test handler through local HTTP fixtures](../server/llm_gateway_test.go) | 16 scenario cases passed: 15 generic-format cases covering a matching normalized service, different endpoint/format/proxy and preserved non-local fallback; 1 native case rejecting active cloud-key inheritance |
| Both focused groups with `-race` | Passed, no race reports |

The **37 cases** above count leaf scenarios, excluding parent tests. Authentication failures were reproduced before each product fix and the same tests passed afterward. Public documentation addresses in the non-local handler cases use an explicit local fixture proxy; no public provider is contacted.

A blank-key connection test without a profile ID can reuse the active local service's key only when the generic format, normalized base URL and proxy all match, including for a new draft. Different local services and native Ollama remain isolated. Saving a new blank-key profile does not import environment credentials.

These checks establish the tested protocol and credential boundaries, not paid-account availability or every historical feature. Full-suite, browser, CI, merged-main and release-artifact results after these fixes are separate gates. No paid provider calls were made.

## Installed-model selection follow-up — 2026-10-15

Local verification of the new-profile Ollama default selection on `feat/ollama-native-flexible-providers`. This follow-up supersedes the earlier template behavior: selecting Ollama now performs read-only model discovery. The historical 37-check browser snapshot below predates this behavior; its “no implicit requests” assertion is historical, not the current Ollama contract.

| Check | Result |
| --- | --- |
| Frontend unit and localization tests (`npm --prefix web test`) | 45 passed, 0 failures |
| Production static export (`npm --prefix web run build:static`) | Passed, including TypeScript and all 31 pages |
| Real host installed-model discovery through the browser and authenticated backend | Passed on an unsaved new profile, same-machine Ollama, blank proxy and API key |
| Expanded full browser smoke on the embedded binary and fresh `scopeweaver_test_default_final` database | 43 passed, 0 failures (31 smoke checks + 12 dashboard scenarios). Covers English/Korean installed defaults for all three templates, direct format selection, cloud compatibility, metadata-only selection, failed/empty discovery, manual edits and late-response rejection, reopening drafts and existing profiles, plus existing setup, sign-in, localization, SSE and dashboard regressions |

The real host's `/api/tags` returned `qwen2.5-coder:7b`, `qwen3:4b`, and `embeddinggemma:latest`. The browser selected **`qwen2.5-coder:7b`**, the first returned installed model declaring `tools`. Its metadata reported `completion`, `tools`, `insert`, 7.6B parameters and Q4_K_M quantization. `qwen3:4b` also declared `tools`; `embeddinggemma:latest` declared only `embedding`.

The browser sent exactly one LLM operation, `POST /api/llm/models`, using the host endpoint with no key or proxy. The new draft was closed without save, activation, connection testing or inference. Model and machine configuration were unchanged. This proves installed-model selection and metadata discovery; it does not establish tool execution or autonomous task reliability.

## Historical native Ollama branch — 2026-10-15 (branch-local, not a release)

Local checks on `feat/ollama-native-flexible-providers`, recorded before the pull request was opened. Remote PR CI is tracked separately in GitHub Actions; nothing here is a release, merge or CI result.

| Check | Result |
| --- | --- |
| Go suites, all 20 packages run separately, each on a fresh disposable PostgreSQL 16 database | 1,197 tests/subtests passed; 2 opt-in skips (`TestAgentAdapterE2E`, `TestLiveContextReview`) |
| Agent adapter, run separately | 3 real MCP/Pi scenarios passed |
| `go vet ./...` | Passed |
| Frontend unit and localization tests | 43 passed; 3,159 unique keys in each of English, Korean and Simplified Chinese |
| Backend message catalog | 1,938 unique keys in each of the three languages; printf and parity tests passed |
| TypeScript and production static export | Passed, rebuilt after the final health UI changes |
| Frontend dependency audit (`npm audit`) | 0 vulnerabilities |
| Builds | Embedded `CGO_ENABLED=0` binary rebuilt after the final health UI changes; Windows amd64 `CGO_ENABLED=0` cross-build passed (native Windows runtime not run) |
| Browser smoke (`npm run smoke`) on the real embedded binary and a fresh `scopeweaver_test_ollama_final_health` database | 37 passed, 0 failures (25 smoke checks + 12 dashboard scenarios). Covers the three Ollama templates in English and Korean with no implicit requests, keyless save/activate, keyless local compatible health versus a public cloud profile with a missing key, cloud formats and editable GLM, per-model metadata and tool warnings, the exact missing-model pull command, tool/context/load/speed diagnostics and the default-off local-only policy, plus the existing English/Korean/Simplified Chinese setup, sign-in, language, draft, SSE and approval-usage regressions |
| Compose credentials | 11 cases passed in real `network_mode: none` containers, through container environment and `FromEnv` |
| Installer and packaging | 8 installer fixtures passed in total, including English and Korean selection cases; 5 archives passed in each of English and Korean |
| Release notice (Python 3.12) | 6 tests passed |
| Independent backend security/correctness review | Passed, including the DeepSeek local-only policy, live CIDR checks and 404 classification |

### Real Ollama run

Host Ollama 0.32.15 with `qwen3:4b`, through the native streaming API:

- In an SDK-level streaming check with a synthetic tool fixture, the model made a `record_fact` tool call; ScopeWeaver executed it locally, replayed the result, and the turn completed. This was not a platform database `record_fact`.
- 32K context was requested; `/api/ps` reported 32,768 allocated.
- Exact usage in that check: streaming turn 186 input / 750 output tokens; completing turn 826 input / 193 output.
- Keyless save/activate through the real API passed. Diagnostics reported `tool_call_verified` and `context_verified` true, context 32,768, load 2.225375 ms, 29.231 tokens/s and latency 18,958 ms. **This was one warm run, not a cold-start benchmark or a hardware generalization.**
- The exact Korean pull hint for a missing model, and the strict-policy toggle blocking a public IP, passed.
- **Platform task worker Bash proof:** a real task worker (task 1) running `qwen3:4b` (32K requested, 32,768 allocated; concurrency cap 1; keep-alive 30m; thinking disabled; max 4,096 tokens) made a native Bash tool call `call_219yv72x`. Activity 9 (call) and 10 (`tool_result`) share that ID with no error, and the output contained the marker `LOCAL_TASK_OLLAMA_OK`. The ledger recorded exactly 6,704 input / 4,096 output tokens for that native call, and the next native call replayed the same `tool_call_id`. The intent was seeded manually through the existing `db.AddIntent`, not created by the LLM. The output also contains a literal `\n` from fixture escaping; no newline-handling or model-bug conclusion is drawn. The run used a binary built from the uncommitted branch source before the final UI/DTO changes.

### Autonomous task reliability is not validated

A passing tool-call diagnostic proves the wire path, not that a model can complete a task.

- `qwen2.5-coder:7b` declares `tools`, yet wrote tool-shaped JSON in `assistant.content` instead of wire `tool_calls`, so no Bash or fact tool ran. ScopeWeaver correctly kept it as text.
- `qwen3:4b` as the main agent, given a long prompt, deliberated and wrote the expected marker as text. As a task worker it did run Bash (above), but the task was paused at 360 s without `record_fact` or autonomous completion; one successful worker tool call does not establish autonomous completion.

**No end-to-end autonomous task completion or security-assessment quality is claimed for any local model, and 7B-class models are not validated as a full agent.** The [hardware tiers](llm-providers.md#hardware-tiers) are estimates, not benchmarks.

### Not exercised

No paid cloud calls, external target scans, real notifications, publication, deployment, release or merge. Anthropic, OpenAI Chat/Responses and custom gateways were verified through wire-contract tests, regressions and editable browser flows, not live provider accounts. All test databases were disposable, and the host Ollama configuration was not changed. The optional `local_llm_only` policy is off by default; servers still need `OLLAMA_NO_CLOUD` set separately.

## Running database-backed tests

The `db`, `server` and `agent` suites need PostgreSQL. They resolve their
connection string **only** from the `ARTEX_TEST_DSN` environment variable and
will never fall back to the application's own DSN (`ARTEX_PG_DSN` /
`config.json`). This is deliberate: it keeps `go test` from mutating the
live/production database the running app uses.

- **`ARTEX_TEST_DSN` unset** → every database-backed test skips. No database is
  touched.
- **`ARTEX_TEST_DSN` set to a throwaway database** → the suites run against it.
- **`ARTEX_TEST_DSN` resolving to the same database as the app** (same host,
  port and database name) → the run fails fast with
  `refusing to run tests against the live database; point ARTEX_TEST_DSN at a
  throwaway DB`, before any test connects.

The simplest way to run them is:

```bash
make test-db
```

`make test-db` (via `scripts/test-db.sh`) creates a throwaway database named
`scopeweaver_test_<pid>` owned by the `sw_user_test` role, points
`ARTEX_TEST_DSN` at it, runs `go test ./db/ ./server/ ./agent/ -count=1`, and
drops the database on exit — even if the tests fail. Override `TEST_DB_ROLE`,
`TEST_DB_HOST` or `TEST_DB_NAME` to match your local PostgreSQL. The database
name must start with `scopeweaver_test_` (or `scopeweaver_adapter_` for the
agent-adapter E2E test).

To run a subset against a database you manage yourself, export `ARTEX_TEST_DSN`
and call `go test` directly:

```bash
export ARTEX_TEST_DSN='postgres://sw_user_test@/scopeweaver_test_1?host=/tmp&sslmode=disable'
go test ./server/ -run TestSomething -count=1
```

## v0.2.1 candidate — 2026-10-11

The Windows release candidate was built with the real static frontend and tested against disposable PostgreSQL 16.15 databases. These results supersede the historical dependency and Windows-runtime limitations below; the older record is retained for traceability.

| Check | Result |
| --- | --- |
| Frontend dependency audit | 0 vulnerabilities across all severities, including development dependencies |
| Frontend regression and localization tests | 26 passed; no skips |
| Production static export | TypeScript check passed; 31 pages generated |
| Native Windows Go suites, 18 packages | 1,016 tests/subtests passed; 0 failures; 13 existing platform/opt-in skips |
| Native Windows complete server suite | 311 tests/subtests passed; 3 existing opt-in/platform skips |
| Agent adapter | 7 unit tests and syntax checks passed; 3 MCP/Pi scenarios passed against the real authenticated backend and a fresh database |
| Local installer | All 4 isolated fixtures passed: fresh install, stale-file replacement, dependency failure and frontend-build failure |
| Backend translation catalog | 1,868 production keys passed English/Korean completeness and printf argument checks |
| Notification isolation | Deterministic stale-worker regression failed before repair and passed afterward; management/notification selection passed 5 consecutive runs |
| Windows evidence | Full PostgreSQL-backed suite passed, including concurrent writes, commit failures and long paths |
| Windows Python and skill files | Real Python execution and unusable-launcher fallback passed; nested Unicode paths passed through file listing and read handlers |
| Browser E2E on embedded Windows binary | 8 checks passed: setup outage protection in both languages, setup/login, same-origin SSE, usage API isolation, both localized usage cards, and no uncaught page errors |

Go counts include parent tests and subtests. The 13 native skips comprise ten Unix self-update fixtures, one unavailable symlink privilege, the opt-in adapter integration test, and the live-provider test. Adapter integration is checked separately, and Linux CI covers the Unix fixtures. No tests were disabled to produce these results.

Regressions reproduce the original Windows directory-sync failure, localization separator mismatch, unusable Python launcher, notification worker leakage, and profile-cleanup ordering. The E2E SQL helper now puts options before its connection URL; a direct outage/restore check confirmed that Windows psql executes the statements.

The release gate additionally requires the `frontend`, `windows`, `backend`, `agent-adapter`, and browser `smoke` jobs before and after merge. Publication results, platform archives, and checksums are available on the [v0.2.1 release](https://github.com/cskwork/scopeweaver/releases/tag/v0.2.1); this source record does not claim those later jobs or downloaded assets have already passed.

Existing configured Python interpreter paths are preserved. If a prior installation stored an unusable launcher alias, choose a runnable interpreter in Settings. Native Windows tests do not establish Windows self-update behavior or macOS runtime behavior. Live paid providers, external targets, and real notification recipients were not used.

## Historical verification — 2026-10-03

Validation recorded on **2026-10-03** for the English/Korean ScopeWeaver edition, based on upstream ARTEX commit `160fe13`. Tests use disposable PostgreSQL databases and local fixtures. This is source delivery; no binary release or container image has been published.

| Check | Result |
| --- | --- |
| Frontend regression and localization tests | 21 passed; TypeScript check passed |
| Production static export and embedded Go binary | Built successfully |
| Go regression suites, 18 packages | 986 tests/subtests passed; 1 inherited test failed; 1 external-provider test skipped |
| Complete server suite | 285 tests/subtests passed; only the opt-in live-provider test skipped |
| Backend translation catalog | 1,865 production keys passed English/Korean completeness and printf argument checks |
| Authored Chinese text audit | No untranslated authored source/docs or Go comments; deliberate compatibility/data exceptions retained |
| Desktop browser coverage | 24 routes in each language; no missing labels or page-level horizontal overflow |
| Mobile browser coverage | Dashboard, tasks, findings and settings in both languages; no page-level horizontal overflow at 390px |
| Real application screens | Login and notification forms checked in both languages; setup validation and initialization verified |
| Language switching | Navigation/status labels update; selection and unsent drafts are preserved |
| HTTP localization | Markdown/CSV export, language negotiation, saved preference and invalid-language rejection checked |
| GLM-5.3 provider contract | 21 focused tests/subtests passed with local HTTP/SSE fixtures; both endpoint paths, reasoning and tool-call replay covered |
| GLM template browser flow | Both templates checked in both languages; entered name/key/proxy preserved; no save, activation or provider request |
| Packaging and launch scripts | Shell syntax, bilingual packaging fixtures and 10 supervisor cases passed |
| Provenance and dependencies | Original license unchanged; dependency versions and Go module unchanged; documentation links and diff checks passed |

Counts include Go parent tests and their subtests. Package runs use separate databases initialized with the same supplementary metering tables as normal server startup. The original tests and checks remain enabled.

### Inherited issues at the time

`TestGraphOverviewExpandsAssociatedCompanyScope` failed with `task scope missing: <nil>` in both ScopeWeaver and untouched upstream source. The test expected detailed scope in the graph overview, which upstream commit `06a43f3` deliberately made compact. Commit `75a706d` replaced it with `TestGraphOverviewKeepsAssociatedCompanyScopeCompact`, which checks the compact overview and verifies the stored company scope directly. The complete backend CI has passed since then.

`TestTaskMetadataPatchReturnsRenameAndPin` previously produced an intermittent temporary-directory cleanup failure in upstream verification. It passed in the final ScopeWeaver server run; no unrelated lifecycle change was made to hide it.

The upstream mock preview has no notification metadata mock, so its Notifications page errors. The **real backend Notifications page and add-channel form pass** in both languages. No notification was sent during verification.

The v0.1.0 verification recorded **14 npm audit findings: 1 critical, 9 high and 4 moderate**. Framework/tooling updates and removal of the unused shadcn CLI dependency resolved the findings in the v0.2.1 candidate above. Run `cd web && npm audit` for the current registry report; audit results can change as new advisories are published.

### Historical scope and limits

- The optional `TestLiveContextReview` requires private external-provider configuration and was not run. No paid model calls, external target scans, or real notification deliveries were performed.
- GLM tests prove the local request/response contract, not account access or provider availability. Coding Plan is restricted to officially supported tools, and ScopeWeaver is not listed; see [provider setup](llm-providers.md).
- Docker image execution and Windows batch execution were not verified. Packaging fixtures do not claim a real Windows runtime test.
- Setup reached the authenticated route, but one browser wait for the window load event timed out; initialization and the new password were then confirmed through the real API.
- User-entered text, stored evidence, edited prompts, wire identifiers, legacy parsing markers, Unicode fixtures, and original upstream screenshots retain their original content. The English/Korean app does not translate arbitrary user data or third-party tool output.
- Historical validation documents under `sidequestion/` describe upstream work, not new ScopeWeaver runs.

Current remote checks are available in [GitHub Actions](https://github.com/cskwork/scopeweaver/actions/workflows/ci.yml).
