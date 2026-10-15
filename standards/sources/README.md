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
| `cisa-kev/known_exploited_vulnerabilities.json` | cisa.gov `/sites/default/files/feeds/known_exploited_vulnerabilities.json` | catalog `2026.10.15` (1746 entries) | 2026-10-15 | `0a4807c44c302e6c3f8ba5adbcd45f8163514f52059f636ad9048728cf0c57b8` |
| `opencre/asvs-4.0.3.links.json` | opencre.org `/rest/v1/standard/ASVS` (all pages) | OpenCRE snapshot; ASVS **4.0.3** → CRE (277 entries) | 2026-10-15 | `da60c283a5951b482db3f2de9ab59f898ae317aa0c21eb884eaa0c4f9a6765f7` |
| `opencre/cwe.links.json` | opencre.org `/rest/v1/standard/CWE` (all pages) | OpenCRE snapshot; CWE → CRE (886 entries) | 2026-10-15 | `75e1e79642000ea8b31df6c9c9993d4663d53d8904b38cc6ea6d9ff33595ff51` |
| `opencre/wstg.links.json` | opencre.org `/rest/v1/standard/OWASP Web Security Testing Guide (WSTG)` | OpenCRE snapshot; WSTG → CRE (59 entries) | 2026-10-15 | `f2f8df6768395e119a3de2b69d72cd21e8c93e79f0a1bcc4b5f22645fedacbcf` |
| `asvs/mappings/mapping_v5.0.0_to_v4.0.3.yml` | github.com/OWASP/ASVS `5.0/mappings/` @ tag `v5.0.0` | official ASVS 5.0.0→4.0.3 bridge | 2026-10-15 | `d92aafd2410526f6e62666cbb12bb4ae170c54de5021fd48bf0f481af955207f` |
| `asvs/mappings/mapping_v4.0.3_to_v5.0.0.yml` | github.com/OWASP/ASVS `5.0/mappings/` @ tag `v5.0.0` | official ASVS 4.0.3→5.0.0 bridge | 2026-10-15 | `f1e57762df69183e8759069da5b05402b439a55f80283b40217847dc09677664` |
| `asvs/mappings/v5.0.be_cwe_mapping.json` | github.com/OWASP/ASVS `5.0/mappings/` @ tag `v5.0.0` | archived ASVS 5.0-beta→CWE map (cross-check only) | 2026-10-15 | `dda6ae18ef6fd171ae2846466ce601c53061d991eaa78939ce833563b9c62c19` |
| `asvs/mappings/mapping_v5.0.be_to_v5.0.0.yml` | github.com/OWASP/ASVS `5.0/mappings/` @ tag `v5.0.0` | ASVS 5.0-beta→5.0.0 (to apply the archived CWE map) | 2026-10-15 | `af799f74a571a5fa19b306d4a2243e7dad2736c8ff2bc4144deb4787ec93eadc` |

Notes:
- The CISA KEV file is a **dated snapshot** used as a test fixture / starting
  point for Track E.7 (advisory intake). The live catalog changes daily; E.7's
  adapter fetches the current feed at runtime — this snapshot is not meant to be
  kept current.
- CWE is distributed only as a zipped XML; it is stored as the verbatim zip
  (`unzip -p cwe/cwec_v4.20.xml.zip` to read). E.1 extracts the id→name map it needs.
- **Crosswalk (E-D5 → OpenCRE spine).** ASVS 5.0 ships **no** per-requirement CWE
  (OWASP moved mappings to OpenCRE). So the ASVS↔CWE crosswalk is derived
  (task E.9): `ASVS 5.0 →(asvs/mappings/mapping_v5.0.0_to_v4.0.3.yml)→ ASVS 4.0.3
  →(opencre/asvs-4.0.3.links.json → CRE)→(opencre/cwe.links.json → CRE)→ CWE`, and
  `WSTG →(opencre/wstg.links.json → CRE)→ CWE` directly. Because of the 4.0.3 hop +
  OpenCRE imprecision these links are **advisory** (tagged `source`/`confidence`),
  never authoritative. `asvs/mappings/v5.0.be_cwe_mapping.json` (+ the be→5.0.0
  bridge) is an archived direct map kept only as a cross-check. The OpenCRE files
  are a dated API snapshot, not a verbatim upstream release file.
- NSM grunnprinsipper and NSM NCSC advisories are **not** vendored here: the
  grunnprinsipper is PDF-only (a hand-built mapping layer, Track E.8) and NSM
  NCSC has no confirmed machine-readable feed (open question for E.7).

## Refreshing

Re-run the fetch with the pinned URLs above (controller step — the implementer
sandbox has no network), update the version/hash rows, and let E.1's validator
re-import. Pin ASVS/WSTG to a release tag where one exists; otherwise record the
commit date + hash as done here.
