# Proposed ASVS L3 selection — PROPOSED, pending owner approval (E-D4)

> Status: **proposed, not enforced.** This list is for owner review under
> roadmap decision E-D4. Nothing in the catalog or runtime consumes it yet.

ASVS 5.0 marks 92 requirements as Level 3 (L3). L3 is the most rigorous tier and
most of it needs source access, design review, or intrusive testing that an
automated/agent engagement cannot do non-intrusively. Per the selection criteria
in `docs/reference/security-standards-and-coverage.md` section 5.2 — an L3
requirement is included only when it (1) is verifiable by automation or the agent
without extra access/credentials, (2) uses non-intrusive methods (or the
blueprint explicitly authorises the method), (3) is relevant to the asset type,
and (4) has a reasonable verification cost — the shortlist below is the subset of
L3 requirements that are observable from the outside of a running web target
(HTTP responses, headers, cookies, served HTML, TLS handshake).

This is deliberately short: it is a starting point for review, not an exhaustive
mapping. The owner decides which of these are in scope and whether any TLS-level
checks count as non-intrusive for a given engagement.

| ASVS ID | Section | Why it qualifies (observable, non-intrusive) |
|---------|---------|----------------------------------------------|
| V3.3.5  | Cookie Setup | Cookie name+value length is visible in `Set-Cookie`. |
| V3.4.7  | Browser Security Mechanism Headers | `Content-Security-Policy` `report-to`/`report-uri` visible in response headers. |
| V3.4.8  | Browser Security Mechanism Headers | Security headers on document-rendering responses are directly observable. |
| V3.6.1  | External Resource Integrity | Subresource Integrity (SRI) on external assets is visible in served HTML. |
| V3.7.4  | Other Browser Security Considerations | HSTS preload status of the top-level domain is externally checkable. |
| V4.1.4  | Generic Web Service Security | Supported HTTP methods observable via a non-intrusive `OPTIONS`/method probe. |
| V13.4.6 | Unintended Information Leakage | Detailed backend version banners leak in headers/error pages. |
| V14.2.5 | General Data Protection | Cache-control headers on sensitive responses are observable. |

### Deferred / needs owner decision

- **TLS-level L3 checks** (e.g. V12.1.4 OCSP/revocation, V12.1.5 Encrypted Client
  Hello) are observable from a TLS handshake but require active TLS probing;
  whether that counts as "non-intrusive" for an engagement is an owner call.
- The remaining ~84 L3 requirements need source/design access or intrusive
  testing and are **out of scope** for automated non-intrusive verification.
