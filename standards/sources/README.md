# standards/sources — vendored standards source data

Verbatim, pinned copies of the published security-standard sources that Track E
(standards coverage & reporting) builds on. They are vendored so the build and
the test suite never depend on network access, so the exact version is pinned
and reproducible, and so the required attribution travels with the data.

**These are raw upstream sources — do not hand-edit them.** Task E.1 parses them
into ScopeWeaver's own catalog/crosswalk; downstream code should read E.1's
output, not these files directly. The one exception is
`owasp-top10/top10-2025-categories.yaml`, which is transcribed (there is no
upstream machine-readable file) and may be edited to track the source.

Licensing and attribution for every item are in [`NOTICE`](./NOTICE). ASVS,
WSTG and the Top 10 are CC BY-SA 4.0 — keep the copies verbatim and the
attribution intact.

## Provenance

| File | Upstream source | Pinned version | Retrieved | SHA-256 |
|---|---|---|---|---|
| `asvs/OWASP_ASVS_5.0.0_en.flat.json` | github.com/OWASP/ASVS `5.0/docs_en/` @ tag `v5.0.0` | ASVS 5.0.0 (345 requirements) | 2026-10-15 | `8201b20eec2908c3380ac600c91c8ba746346fbb808859366abb232027532311` |
| `asvs/OWASP_ASVS_5.0.0_en.csv` | github.com/OWASP/ASVS `5.0/docs_en/` @ tag `v5.0.0` | ASVS 5.0.0 | 2026-10-15 | `98c8fe911b9edb403af8ee05d3ce8201ecac2659e313b053890a62847cdcf680` |
| `wstg/checklist.json` | github.com/OWASP/wstg `checklists/checklist.json` @ `master` | WSTG stable 4.2 content (114 test IDs); checklist postdates the v4.2 tag, so pinned by hash | 2026-10-15 | `0b526c474d41c9631ca4b59fcab6f00e0ebfce079f6f15cc7e86d1debaba500a` |
| `cwe/cwec_v4.20.xml.zip` | cwe.mitre.org `/data/xml/cwec_latest.xml.zip` | CWE 4.20 (zipped XML; `cwec_v4.20.xml`) | 2026-10-15 | `3976f599e5e5200219a3108bb896d06e2a88fbb293369e1883cb423a5e9d7d50` |
| `owasp-top10/top10-2025-categories.yaml` | transcribed from top10.owasp.org/2025 | Top 10:2025 (10 categories) | 2026-10-15 | (authored; not a verbatim upstream file) |
| `cisa-kev/known_exploited_vulnerabilities.2026-10-15.json` | cisa.gov `/sites/default/files/feeds/known_exploited_vulnerabilities.json` | catalog `2026.10.14` (1745 entries) | 2026-10-15 | `47b463a136b8e0956756cfbcdbc2e4b4ccb60c96b4ca7a1ca9f507817cc83045` |

Notes:
- The CISA KEV file is a **dated snapshot** used as a test fixture / starting
  point for Track E.7 (advisory intake). The live catalog changes daily; E.7's
  adapter fetches the current feed at runtime — this snapshot is not meant to be
  kept current.
- CWE is distributed only as a zipped XML; it is stored as the verbatim zip
  (`unzip -p cwe/cwec_v4.20.xml.zip` to read). E.1 extracts the id→name map it
  needs; ASVS already carries the per-requirement CWE references used for the
  crosswalk.
- NSM grunnprinsipper and NSM NCSC advisories are **not** vendored here: the
  grunnprinsipper is PDF-only (a hand-built mapping layer, Track E.8) and NSM
  NCSC has no confirmed machine-readable feed (open question for E.7).

## Refreshing

Re-run the fetch with the pinned URLs above (controller step — the implementer
sandbox has no network), update the version/hash rows, and let E.1's validator
re-import. Pin ASVS/WSTG to a release tag where one exists; otherwise record the
commit date + hash as done here.
