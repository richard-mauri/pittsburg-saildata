# Current work — SailingGo

**Active:** [Issue #37](https://github.com/richard-mauri/pittsburg-saildata/issues/37), [draft PR #38](https://github.com/richard-mauri/pittsburg-saildata/pull/38), branch `charts/37-ncds-mbtiles-evaluation` targeting `main`. The verified merged `main` baseline is `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47` (PR #36). All chart evaluation changes stay in PR #38.

**Owner constraint:** **No Python allowed.** Deleted both previous Python auditor files; replaced their functionality with [scripts/noaa_mbtiles_audit.go](scripts/noaa_mbtiles_audit.go) plus [scripts/noaa_mbtiles_audit_test.go](scripts/noaa_mbtiles_audit_test.go), and changed CI to test Go instead. The Go audit uses the pre-existing system `sqlite3` CLI in **read-only** mode for MBTiles file inspection and makes HTTP catalog queries to NOAA using Go standard library. The Go utility is standalone and not imported by the SailingGo server.

**Current evaluation:** NOAA NCDS polygon catalog exposes region ID, creation string, estimated MB size, baseline locator and extents. Actual regional responses remain blocked by NOAA network access. Exact California package IDs, true bytes, data timestamps, tile zooms, binary formats, coverage/seams, visual parity and NCDS-specific hosting terms remain **unverified**. Read [docs/NOAA_MBTILES_EVALUATION.md](docs/NOAA_MBTILES_EVALUATION.md). No NOAA chart database downloaded or served and no live Go HTTP XYZ tile prototype yet implemented.

**Safety:** No authentication changes, no preferences/account/Guest changes, no map or weather route modifications, no private data or server persistence changes, no production launcher, services, listeners, tunnels, restart, deployment, tag, release, or version/build-counter changes. Owner Mac uncommitted files, browser state and private configuration remain inaccessible/uninspected. `*.mbtiles` stays excluded by Git.

**Docs / policy:** The complete Homepage merged-main policy v1.1.0 and SailingGo AGENTS, README, ARCHITECTURE, DEVELOPMENT and CURRENT_WORK reviewed; evaluation and CURRENT_WORK updated. No changed runtime contracts or source selector.

**Checks:** Isolated Go helper tests and exact-head CI must be verified. Live NOAA, downloaded binary, owner Mac/Safari, actual server tile endpoint, and deployment storage **unverified**.

**Approval:** Existing draft PR only; further scope uses owner `NEXT`, closeout requires `FINISH`. No merge, Issue closure, remote branch deletion, deployment, tag or release authorized.

**Mac review (do not restart anything):**
```sh
cd ~/projects/sailing-go
git status --short --branch
# STOP if uncommitted changes are shown; do not discard work.
git fetch origin
git checkout charts/37-ncds-mbtiles-evaluation
git pull --ff-only origin charts/37-ncds-mbtiles-evaluation
git status --short --branch

go test scripts/noaa_mbtiles_audit.go scripts/noaa_mbtiles_audit_test.go
go run scripts/noaa_mbtiles_audit.go -catalog
# After obtaining a verified NOAA MBTiles database OUTSIDE this repo:
# go run scripts/noaa_mbtiles_audit.go -audit ~/Downloads/sailing-noaa-evaluation/candidate.mbtiles
```
