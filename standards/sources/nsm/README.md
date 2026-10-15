# NSM grunnprinsipper — vendored source

Verbatim copy of NSM's *Grunnprinsipper for IKT-sikkerhet v2.1* (21 principles,
~118 measures in four groups: identify/map, protect/maintain, detect,
handle/recover). Used as a **mapping layer** for tenders (Track E.8), not as test
cases.

| File | Source | Version | Retrieved | SHA-256 |
|---|---|---|---|---|
| `nsm-grunnprinsipper-2.1.pdf` | nsm.no `/getfile.php/1313975-1717589722/.../NSMs%20Grunnprinsipper%20for%20IKT-sikkerhet%20v2.1.pdf` | v2.1 (53 pp, Norwegian) | 2026-10-15 | `1ad86834616bb467076fe0ae1ac9c959e40b244dde90658cd46ecac11f0d01d3` |

## Attribution
© Nasjonal sikkerhetsmyndighet (NSM). Redistributed for internal reference /
mapping use. NSM publishes the grunnprinsipper for broad use by Norwegian
organisations; check NSM's terms before external redistribution.

## Extraction status (for E.8)
NSM publishes this **only as a PDF** — there is no machine-readable feed. The
principle/measure structure (ids like `2.1.1`, titles, group) must be extracted
into a structured `standards/nsm-grunnprinsipper.yaml` before E.8's report
appendix can map verified requirements → NSM measures. No `pdftotext`/Go PDF
parser is available in the implementer sandbox, so **extraction is a controller
step** (read the PDF pages, emit the structured YAML), done when E.8 starts.
E.8 (the crosswalk + report appendix) then consumes that YAML.
