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
3. **Headers.** On a representative set of in-scope responses (`GET`/`HEAD`), read: `Strict-Transport-Security`, `Content-Security-Policy` (including `frame-ancestors`, `object-src`, `base-uri`), `X-Content-Type-Options`, `Referrer-Policy`, and version-leaking headers (`Server`, `X-Powered-By`).
4. **Cookies.** From `Set-Cookie` on those responses: `Secure`, `HttpOnly` (where the value need not be script-readable), `SameSite`, and the `__Host-` / `__Secure-` prefix conventions.
5. **WSS.** For WebSocket endpoints already in the inventory, issue the upgrade over TLS and confirm it is served over WSS (not plain WS) and that the upgrade response carries the same header posture. Do not send messages.

Save every command and its output; the saved output pointer is the evidence for a `pass`.

---

## Mapping: requirement → check → verdict

| ID | Requirement (official, abridged) | pass | fail | inconclusive |
|---|---|---|---|---|
| V12.1.1 | Only the latest recommended TLS versions (TLS 1.2 and 1.3) are enabled | Only TLS 1.2/1.3 accepted | TLS 1.1 or older (or SSL) accepted | Version probes blocked or not observable |
| V12.1.2 | Only recommended cipher suites, strongest preferred | Only recommended (AEAD, forward-secret) suites offered, strongest first | A weak suite is offered (RC4, 3DES, CBC-SHA1, export, NULL) | Cipher negotiation not observable |
| V12.2.1 | TLS for all client connectivity to external HTTP services, no fallback to insecure protocols | Every in-scope service is HTTPS-only (HTTP redirects to HTTPS or is closed) | A service serves content over plain HTTP | Service unreachable |
| V12.2.2 | External services use publicly trusted certificates | Chain validates to a public root, in date, hostname matches | Self-signed, untrusted, expired or mismatched | No trust store / certificate not retrievable |
| V3.4.1 | Strict-Transport-Security on all responses (max-age >= 1 year; includeSubDomains at L2+) | HSTS present with max-age >= 31536000 on sampled responses | HSTS missing or max-age too short | Only some responses observable |
| V3.4.3 | Content-Security-Policy header present (object-src 'none', base-uri 'none' at minimum) | CSP present with those directives | CSP missing or lacks them | CSP present but only report-only, or not on all sampled responses |
| V3.4.4 | X-Content-Type-Options: nosniff on all responses | Present on all sampled responses | Missing on any sampled response | Too few responses observable |
| V3.4.5 | Referrer policy set to prevent leaking sensitive data to third parties | Referrer-Policy (header or meta) set to a non-leaking value such as strict-origin-when-cross-origin or no-referrer | Missing, or unsafe-url | Only some responses observable |
| V3.4.6 | CSP frame-ancestors directive controls embedding (X-Frame-Options is not enough) | frame-ancestors present and restrictive | frame-ancestors missing | CSP not on all sampled responses |
| V3.3.1 | Cookies have Secure (and __Secure- prefix where __Host- is not used) | Every cookie set over HTTPS has Secure | A cookie lacks Secure | No cookies set -> not_applicable |
| V3.3.2 | SameSite set according to the cookie's purpose | Every cookie has an explicit SameSite matching its use | A cookie lacks SameSite or uses None without need | Cookie purpose unclear |
| V3.3.3 | __Host- prefix unless the cookie must be shared with other hosts | Cookies use __Host- or are documented as shared | A host-only cookie lacks __Host- | Sharing need unknown |
| V3.3.4 | Cookies not meant for scripts (e.g. session tokens) have HttpOnly | Session cookies set HttpOnly | A session cookie lacks HttpOnly | Cannot tell which cookies are session cookies |
| V4.4.1 | WebSocket over TLS (WSS) for all WebSocket connections | All inventory WebSocket endpoints use wss:// | A ws:// endpoint exists | No WebSocket endpoints -> not_applicable |
| WSTG-CRYP-01 | Testing for Weak Transport Layer Security | Protocols, suites and certificate all sound | Any weakness found under V12.1.x/V12.2.x | Probes blocked |
| WSTG-CONF-07 | Test HTTP Strict Transport Security | Same as V3.4.1 | Same as V3.4.1 | Same as V3.4.1 |
| WSTG-CONF-12 | Test for Content Security Policy | CSP present and meaningful (V3.4.3 + V3.4.6) | CSP missing or ineffective | Not on all sampled responses |
| WSTG-CONF-14 | Test Other HTTP Security Header Misconfigurations | X-Content-Type-Options and Referrer-Policy as above, no information-leaking headers (e.g. detailed Server/X-Powered-By versions) | Header missing or leaking versions | Too few responses observable |
| WSTG-SESS-02 | Testing for Cookies Attributes | Secure, HttpOnly (session), SameSite, prefixes as above | Any session-cookie attribute missing | Cannot identify session cookies |

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
