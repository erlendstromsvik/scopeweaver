---
name: dependencies-fingerprint
description: Use to inventory the third-party components an in-scope web application exposes (server, framework, client-side libraries) and their versions, non-intrusively, and record verdicts for the covered dependency and version-disclosure requirements.
covers:
  asvs: [V15.2.1, V15.1.2, V13.4.6, V3.6.1]
  wstg: [WSTG-INFO-02, WSTG-INFO-08, WSTG-INFO-09]
---

# Dependencies and Fingerprinting (component inventory, version disclosure, SRI)

Inventory the third-party components an in-scope web application exposes — server software, framework, client-side libraries — and their versions, by read-only observation of responses the application already serves. Record a verdict per covered requirement.

---

## Boundaries and prohibitions (required reading)

This skill observes what the application exposes. It does not probe for hidden paths, force errors, or touch application state.

| Scope | Allowed | Prohibited |
|---|---|---|
| Network | `GET` / `HEAD` of in-scope URLs in the asset inventory; reading response headers, HTML, linked static JS/CSS files and their banners/comments; well-known static paths that the app itself links to | Path brute force / wordlists; probing admin or debug endpoints; error-forcing requests; payloads; any write or state-changing request; anything outside the in-scope asset inventory or the task's stated constraints |
| Rate | At most 1 request per second per host | Request floods, parallel bursts, retries that exceed that rate |
| Knowledge | Matching observed component names and versions against advisory data that is part of the evidence | Guessing CVEs from memory; asserting a vulnerability the evidence does not name |

If a check would need a prohibited action, record `inconclusive` with the reason. Do not escalate.

---

## Workflow

1. **Server/framework hints.** From response headers of in-scope URLs: `Server`, `X-Powered-By`, cookie names, and `generator` meta tags. Record the type and any exact version shown.
2. **Client-side libraries.** From `<script>` and `<link>` tags in sampled in-scope HTML: list each library and its version, taken from the URL and from the file's own banner/comments (name + version). Fetch only files the page itself links to.
3. **External assets.** Note which libraries load from external hosts (CDNs) and, for each, whether the tag carries an `integrity=` attribute and a versioned URL.
4. **Inventory.** Write the component inventory (component, version or "unknown", where seen) into a fact/hint and use that saved output as the `evidence_ref`.

Save every observation and its source; the saved inventory output is the evidence for a `pass`.

---

## Mapping: requirement → check → verdict

| ID | Requirement (official, abridged) | pass | fail | inconclusive |
|---|---|---|---|---|
| V15.2.1 | Only components that have not breached the documented update and remediation time frames | Never from black-box observation | A fingerprinted component version is known to be vulnerable AND the evidence names the CVE (report_finding first) | Default: record the observed inventory and reason "remediation time frames and full inventory are not observable externally" |
| V15.1.2 | An SBOM / inventory of all third-party libraries is maintained | Never from black-box observation | — | Always: reason "requires the organisation's SBOM; observed components attached as evidence" |
| V13.4.6 | No detailed version information of backend components is exposed (L3) | No exact versions in headers, banners or sampled pages | An exact backend version is disclosed (report_finding: information disclosure) | Too few responses sampled |
| V3.6.1 | Externally hosted client-side assets only if static, versioned and protected with Subresource Integrity (L3) | Every external script/style is versioned and has integrity= | An external script/style lacks integrity= or is unversioned | External assets loaded dynamically and not observable; no external assets -> not_applicable |
| WSTG-INFO-02 | Fingerprint Web Server | Server type identified without an exact version being disclosed | Exact server version disclosed (same finding as V13.4.6) | Server type not identifiable |
| WSTG-INFO-08 | Fingerprint Web Application Framework | Framework identified without version disclosure | Exact framework version disclosed | Framework not identifiable |
| WSTG-INFO-09 | Fingerprint Web Application | Application identified; no version disclosure | Exact application version disclosed | Application not identifiable |

Note: Known-exploited components are also listed in the coverage report's Advisories section (CISA KEV matched against detected technologies); do not guess CVEs from memory. Only fail V15.2.1 when the evidence itself names the CVE.

---

## Verdict rules

- Record **exactly one** `record_control_result` per covered requirement per task. Re-recording replaces the earlier verdict.
- `method` is `"agent"` for every verdict recorded by this skill.
- **pass** ONLY with an `evidence_ref` (a pointer to the saved inventory output that shows the check) **and** `asset_ids` of the assets actually checked. No evidence → not a pass.
- **fail**: call `report_finding` FIRST, then record `fail` with the `finding_ids` returned for that finding. A `fail` without `finding_ids` is invalid.
- **partial**: the requirement holds for some checked assets and not others.
- **inconclusive**: uncertain or only partially observable. Always give a `reason`. NEVER upgrade an uncertain outcome to `pass`.
- **not_applicable**: only with a `reason` (e.g. no external assets are loaded). Do not use it to mean "didn't check".
- Never record `not_tested`; that value is system-only.

---

## Completion

The skill is complete when every ID in `covers:` has a verdict recorded, or, for any ID that could not be checked, a `inconclusive` verdict carrying the reason it could not be checked. No covered ID is left with no result.
