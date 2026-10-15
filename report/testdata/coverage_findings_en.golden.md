# Coverage report

Generated: 2026-10-15 12:00 UTC

## Summary

Verified against 2 of 4 applicable ASVS 5.0 requirements (scope: levels 1–2).

Verified against 1 of 3 applicable WSTG requirements (scope: all tests).

| Standard | In scope | Not applicable | Verified | Pass | Fail | Partial | Inconclusive | Not tested | No result |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| ASVS 5.0 | 5 | 1 | 2 | 1 | 1 | 0 | 1 | 0 | 1 |
| WSTG | 3 | 0 | 1 | 0 | 0 | 1 | 0 | 1 | 1 |

## Coverage matrix

### ASVS 5.0

| Requirement | Level | Verdict | Method | Evidence | Findings |
| --- | --- | --- | --- | --- | --- |
| V1.1.1 | 1 | pass | automated | snap-1 |  |
| V1.1.2 | 2 | fail | manual |  | #12, #15 |
| V1.2.9 | 1 | not_applicable |  |  |  |
| V1.2.10 | 1 | inconclusive | agent |  |  |
| V10.1.1 | 1 | — |  |  |  |

### WSTG

| Requirement | Level | Verdict | Method | Evidence | Findings |
| --- | --- | --- | --- | --- | --- |
| WSTG-ATHN-01 | — | — |  |  |  |
| WSTG-INFO-01 | — | partial | automated | snap-2 |  |
| WSTG-INFO-02 | — | not_tested |  |  |  |

## Limitations

- 0 ASVS 5.0 requirements were not tested, 1 were inconclusive, and 1 have no recorded result.
- 1 WSTG requirements were not tested, 0 were inconclusive, and 1 have no recorded result.
- 1 recorded results were outside the selected scope and are not counted.
- This report states what was verified and how. It is not a certification or a statement of compliance.

## Findings by OWASP Top 10

ASVS (advisory) lists requirements linked to the finding's CWE through the OpenCRE crosswalk. These links are not authoritative.

### A01:2025 — Broken Access Control

| Finding | Severity | Status | CWE | ASVS | ASVS (advisory) |
| --- | --- | --- | --- | --- | --- |
| #2 IDOR | critical | confirmed |  |  |  |

### A05:2025 — Injection

| Finding | Severity | Status | CWE | ASVS | ASVS (advisory) |
| --- | --- | --- | --- | --- | --- |
| #1 XSS in search | high | confirmed | CWE-79 | V5.1.1 | V5.2.2 |

### Unclassified

| Finding | Severity | Status | CWE | ASVS | ASVS (advisory) |
| --- | --- | --- | --- | --- | --- |
| #3 Verbose error | low | pending |  |  |  |
