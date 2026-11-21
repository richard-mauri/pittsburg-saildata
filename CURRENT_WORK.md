# Current work — SailingGo

**Active:** [Issue #37](https://github.com/richard-mauri/pittsburg-saildata/issues/37), [draft PR #38](https://github.com/richard-mauri/pittsburg-saildata/pull/38), branch `charts/37-ncds-mbtiles-evaluation` targeting `main`. Baseline is merged `main` `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47` (PR #36). Only this PR is active for downloaded-chart evaluation.

**NEXT 2026-11-21:** Identified NOAA's official NCDS ArcGIS regional polygon metadata service, exposing `name`, `date_creat`, `size` (MB), `baseline` and polygon extent. Actual results were blocked by external DNS/network access, so **region identity, current size, file timestamps, SQLite zooms, binary format, package coverage, visual parity and NCDS hosting terms remain unverified**. The existing [evaluation](docs/NOAA_MBTILES_EVALUATION.md) documents the source and blockers. Added `scripts/noaa_mbtiles_audit.py` standard-library Python read-only NOAA catalog / downloaded SQLite inspector, `scripts/test_noaa_mbtiles_audit.py` synthetic offline regression, and a GitHub CI step to run the fixture. No NOAA binary was downloaded; the server-hosted **Go tile endpoint is still a future prototype**, not implemented or validated in this NEXT.

**Data safety:** No modifications to authentication, user/Guest Save Changes/Discard Changes, private preference records, embedded map/overlays, NOAA environmental routes, production listeners, tunnels, deployment, tags, versions or Git-private build counters. Owner Mac local checkout/state, tokens, browser history and persistent server storage could not be inspected and were not changed. `*.mbtiles` remains gitignored; audit is read-only.

**Documentation / policy:** Full current Homepage merged-main SUITE_DEVELOPMENT_POLICY.md v1.1.0 and SailingGo AGENTS, README, ARCHITECTURE, DEVELOPMENT, CURRENT_WORK reviewed. Only evaluation and CURRENT_WORK were updated this iteration; README/ARCHITECTURE/DEVELOPMENT/AGENTS unchanged because runtime contracts are unchanged.

**Verification:** Standalone inspector's equivalent fixture ran locally (three unit tests passed) against synthetic SQLite bytes; source committed to PR, and latest-head GitHub CI must be verified after push. Live NOAA, owner Mac/Safari, chart images, zoom/coverage, persistence and actual tile-serving remain **unverified**.

**Approval boundary:** Follow-up dataset verification and isolated Go/SQLite prototype belong in Issue #37/PR #38 after `NEXT` and source access. No automatic integration, merge, Issue closure, branch cleanup, production restart/deploy, tag or release. `FINISH` alone authorizes accepted PR closeout.

**Owner Mac inspection (without restarting or launching any service):**
```sh
cd ~/projects/sailing-go
git status --short --branch
# Stop if any uncommitted changes; never overwrite owner-local work.
git fetch origin
git checkout charts/37-ncds-mbtiles-evaluation
git pull --ff-only origin charts/37-ncds-mbtiles-evaluation
git status --short --branch
python3 -m unittest discover -s scripts -p 'test_noaa_mbtiles_audit.py' -v
python3 scripts/noaa_mbtiles_audit.py --catalog
# Once a NOAA-selected file has been downloaded OUTSIDE the repository:
# python3 scripts/noaa_mbtiles_audit.py --audit ~/Downloads/sailing-noaa-evaluation/candidate.mbtiles
```
