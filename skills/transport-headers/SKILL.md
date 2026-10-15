---
name: transport-headers
description: Use to check TLS configuration, HTTP security headers and cookie attributes of in-scope web endpoints, non-intrusively, and record one verdict per covered requirement.
covers:
  asvs: [V12.1.1, V12.1.2, V12.2.1, V12.2.2, V3.4.1, V3.4.3, V3.4.4, V3.4.5, V3.4.6, V3.3.1, V3.3.2, V3.3.3, V3.3.4, V4.4.1]
  wstg: [WSTG-CRYP-01, WSTG-CONF-07, WSTG-CONF-12, WSTG-CONF-14, WSTG-SESS-02]
---

# Transport and Headers (TLS, security headers, cookie attributes)

Check how an in-scope HTTPS service is configured: which TLS protocol versions and ciphers it accepts, whether its certificate is trusted and current, which security response headers it sends, and how its cookies are flagged. Read-only observation; record a verdict per covered requirement.

---

## Boundaries and prohibitions (required reading)

This skill observes transport and response metadata. It does not test application logic, authentication, or injectable input.

| Scope | Allowed | Prohibited |
|---|---|---|
| Network | TLS handshakes, one connection per protocol version / cipher probe | Port scanning, host discovery, anything outside the in-scope asset inventory or the task's stated constraints |
| HTTP | `GET` / `HEAD` of in-scope URLs already in the asset inventory; reading response headers and `Set-Cookie` | Any write or state-changing request (`POST`, `PUT`, `PATCH`, `DELETE`); login attempts; payloads; fuzzing; brute force |
| Rate | At most 1 request per second per host | Request floods, parallel bursts, retries that exceed that rate |
| WebSocket | The `Upgrade` request itself (header observation only) | Sending any WebSocket message after the upgrade succeeds |
| Credentials | Reading cookie attributes as sent | Guessing, reusing or testing credentials; replaying sessions for access-control testing |

If a check would need a prohibited action, record `inconclusive` with the reason. Do not escalate.

---

## Workflow

1. **List in-scope HTTPS services.** Use the graph/asset tools to get the in-scope web endpoints and their host:port. Only these are checkable; note any that are plain HTTP as a limitation.
2. **TLS.** For each service, probe protocol versions with one connection each and record which are accepted vs rejected:
   ```
   openssl s_client -connect host:443 -tls1   </dev/null   # expect failure
   openssl s_client -connect host:443 -tls1_1 </dev/null   # expect failure
   openssl s_client -connect host:443 -tls1_2 </dev/null   # expect success
   openssl s_client -connect host:443 -tls1_3 </dev/null   # expect success
   ```
   Then note the negotiated cipher and the server's cipher preference, and inspect the certificate: chain trust, expiry, hostname match, key/signature strength.
3. **Headers.** On a representative set of in-scope responses (`GET`/`HEAD`), read: `Strict-Transport-Security`, `Content-Security-Policy` (including `frame-ancestors`), `X-Content-Type-Options`, `Referrer-Policy`.
4. **Cookies.** From `Set-Cookie` on those responses: `Secure`, `HttpOnly` (where the value need not be script-readable), `SameSite`, and the `__Host-` / `__Secure-` prefix conventions.
5. **WSS.** For WebSocket endpoints already in the inventory, issue the upgrade over TLS and confirm it is served over WSS (not plain WS) and that the upgrade response carries the same header posture. Do not send messages.

Save every command and its output; the saved output pointer is the evidence for a `pass`.

---

## Mapping: requirement → check → verdict

| ID | Check | pass | fail | inconclusive |
|---|---|---|---|---|
| V12.1.1 | TLS in use for every in-scope service | All in-scope services serve TLS only | A service is reachable over plain HTTP | Service not reachable / TLS state not observable |
| V12.1.2 | Modern TLS only | Only TLS 1.2/1.3 accepted | TLS 1.0/1.1 or SSL accepted | Version probes blocked or inconclusive at the network |
| V12.2.1 | Certificate trusted by a public CA | Chain validates to a trusted root | Self-signed / untrusted chain | No trust store available to verify |
| V12.2.2 | Certificate valid (not expired, hostname matches) | In date and hostname matches | Expired or hostname mismatch | Certificate not retrievable |
| V3.4.1 | Sensitive data not cached in transit | `Cache-Control: no-store` on sensitive responses | Sensitive response cacheable | Cannot identify which responses are sensitive |
| V3.4.3 | HSTS present and effective | `Strict-Transport-Security` with `max-age` (≥1y) and `includeSubDomains` where applicable | Missing HSTS on an HTTPS-only service | Mixed-scope; header presence not representative |
| V3.4.4 | Valid certificate (extended validation posture) | Chain + validity + hostname all pass | Any of chain/validity/hostname fails | Partially observable |
| V3.4.5 | Strong cipher / key strength | Modern AEAD ciphers; key ≥2048-bit RSA or EC | Weak/export/RC4/SHA-1 or short key | Cipher negotiation not observable |
| V3.4.6 | TLS configuration hardened (no known-weak suites) | No weak suites offered | Any weak suite offered | Probe blocked |
| V3.3.1 | Cookies set with `Secure` | Every cookie over HTTPS carries `Secure` | A cookie lacks `Secure` | No cookies observed (see not_applicable) |
| V3.3.2 | Session cookies `HttpOnly` | Session cookies set `HttpOnly` | Session cookie readable by script | Cannot tell which cookies are session cookies |
| V3.3.3 | Cookie `SameSite` attribute | `SameSite=Lax`/`Strict` present | Missing `SameSite` on a session cookie | Ambiguous cookie purpose |
| V3.3.4 | Cookie path/domain scoped | Path/domain narrow to the app | Overly broad domain/path | Not observable |
| V4.4.1 | File-upload / sensitive-entry endpoints over TLS | Covered by TLS-only posture | Upload or credential entry over plain HTTP | No such endpoint in scope |
| WSTG-CRYP-01 | Testing for weak TLS / broken transport | Modern TLS, strong ciphers | Weak protocol or cipher accepted | Probe blocked |
| WSTG-CONF-07 | Testing for cookies (attributes) | Cookie attributes correct | Attribute missing/wrong | No cookies set |
| WSTG-CONF-12 | Testing for advanced header/SSRF-adjacent config | Header posture consistent | Header missing/inconsistent | Not observable |
| WSTG-CONF-14 | Testing for CORS / cross-origin header config | CORS not overly permissive | `Access-Control-Allow-Origin: *` with credentials | No CORS headers to judge |
| WSTG-SESS-02 | Testing cookies for session management | Session cookie flags correct | Session cookie unprotected | Cannot identify session cookie |

---

## Verdict rules

- Record **exactly one** `record_control_result` per covered requirement per task. Re-recording replaces the earlier verdict.
- `method` is `"agent"` for every verdict recorded by this skill.
- **pass** ONLY with an `evidence_ref` (a traffic snapshot id, or a pointer to the saved command output that shows the check) **and** `asset_ids` of the assets actually checked. No evidence → not a pass.
- **fail**: call `report_finding` FIRST, then record `fail` with the `finding_ids` returned for that finding. A `fail` without `finding_ids` is invalid.
- **partial**: the requirement holds for some checked assets and not others.
- **inconclusive**: uncertain or only partially observable. Always give a `reason`. NEVER upgrade an uncertain outcome to `pass`.
- **not_applicable**: only with a `reason` (e.g. no cookies are set by the service; no WebSocket endpoints in the inventory). Do not use it to mean "didn't check".
- Never record `not_tested`; that value is system-only.

---

## Completion

The skill is complete when every ID in `covers:` has a verdict recorded, or, for any ID that could not be checked, a `inconclusive` verdict carrying the reason it could not be checked. No covered ID is left with no result.
