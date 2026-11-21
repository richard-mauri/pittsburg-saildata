# Current work — SailingGo

**Active scope:** [Issue #37](https://github.com/richard-mauri/pittsburg-saildata/issues/37), branch `charts/37-ncds-mbtiles-evaluation`, one draft PR to `main`. Initial first deliverable is the documented NCDS MBTiles evaluation in [docs/NOAA_MBTILES_EVALUATION.md](docs/NOAA_MBTILES_EVALUATION.md). **No integration or chart source selector is authorized in this phase.**

**Verified starting baseline:** GitHub `main` merge commit `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47` (PR #36 merged). This chart project is independent of authentication; the existing auth implementation, per-user/Guest explicit Save Changes / Discard Changes, and all NOAA weather/current and map overlay features must remain unchanged.

**Dataset evaluation:** NOAA's official NCDS page confirms raster ENC-derived chart MBTiles and weekly baseline package updates. **Current region ID(s), exact byte sizes, effective zoom, image format, geographic tile occupancy, dataset date, visual parity and NCDS-specific redistribution terms remain unverified.** The evaluation environment cannot fetch the NOAA binary. The evidence table and reproducible on-Mac verification commands are in the evaluation document. Do not claim test success or integrate chart basemaps until those checks are complete.

**Data safety:** Documentation-only evaluation; no MBTiles binary committed, no authenticated routes, settings, Homepage storage, browser history, private user data, Mac checkout, build counters, binaries, running services, launchers, Cloudflare tunnels, deployment, tags or releases touched. Owner Mac local tree and production storage are inaccessible and **not inspected**.

**Policy and docs:** Homepage `main` [SUITE_DEVELOPMENT_POLICY.md](https://github.com/richard-mauri/homepage/blob/main/SUITE_DEVELOPMENT_POLICY.md) v1.1.0 read in full on November 21, 2026; SailingGo AGENTS.md, README.md, ARCHITECTURE.md, DEVELOPMENT.md and CURRENT_WORK.md reviewed. New evaluation doc and this snapshot changed; older handoffs remain in Git history.

**Checks:** Documentation source and links inspected. Exact-head CI status must be verified against the latest PR head; no owner Mac/Safari comparisons, live NOAA measurements or local prototype have been performed. `NEXT` only for later approved scoped development; `FINISH` for PR closeout after acceptance. No merge, Issue closure, remote branch deletion, restart, deployment, tag or release authorized.

**Owner Mac handoff (docs-only; don't start/restart services):**
```sh
cd ~/projects/sailing-go
git status --short --branch
# STOP if there are uncommitted local changes; do not discard them.
git fetch origin
git checkout charts/37-ncds-mbtiles-evaluation
git pull --ff-only origin charts/37-ncds-mbtiles-evaluation
git status --short --branch
# Read docs/NOAA_MBTILES_EVALUATION.md, then perform the NOAA measurements outside this checkout.
```
