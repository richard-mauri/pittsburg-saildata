# Current work — SailingGo

**Active Issue and PR:** [Issue #37](https://github.com/richard-mauri/pittsburg-saildata/issues/37); single [draft PR #38](https://github.com/richard-mauri/pittsburg-saildata/pull/38) on `charts/37-ncds-mbtiles-evaluation` targeting `main`. Verified merged baseline `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47` after PR #36.

**Owner finding / correction (2026-11-21):** Running `./devbuild.sh\n./sailing-go -version\n# Optional read-only NOAA metadata diagnosis (may timeout nonfatally):\n# go run scripts/noaa_mbtiles_audit.go -catalog` on the owner Mac produced a NOAA `Client.Timeout exceeded while awaiting headers`, `exit status 1`, and Terminal `Saving session... [Process completed]`. This indicates an upstream connection timeout; termination of the parent Terminal session may additionally depend on shell/Terminal configuration and is not directly caused by a child process invoking `os.Exit`. **The Go audit CLI now avoids every nonzero diagnostic exit, reporting any timeout/error as a nonfatal warning and returning normally.** The process exit code is deliberately not a dataset verification signal; actual NOAA JSON output is required. Added isolated Go regression tests that simulate timeouts and missing files without HTTP calls.

**Language constraint:** Go only; no Python. The read-only inspector `scripts/noaa_mbtiles_audit.go` uses the system `sqlite3` command for local MBTiles diagnostics; no new Go module dependency. CI tests standalone auditor helpers via `go test scripts/noaa_mbtiles_audit.go scripts/noaa_mbtiles_audit_test.go`.

**Application notice:** The existing Nautical WMS all-tile failure fallback now displays **“NOAA nautical charts are temporarily unavailable. Showing Street Map instead; please try again later.”** The source remains WMS, and the Street underlay, chosen map type and explicit Save/Discard are unchanged. This notice is not a live NOAA maintenance diagnosis. The existing CI-only map fallback test now checks the user-facing message and its status element. No new endpoint, retry or network health monitoring is implemented.\n\n**Dataset and implementation:** Official NOAA NCDS regional polygon service publishes region/name/date/size fields, but its live endpoint timed out on owner Mac. Current California package IDs, exact bytes, download dates, tile zoom levels, formats, geographic coverage, Safari appearance, storage and redistribution terms **remain unverified**. No MBTiles binary downloaded or committed; no Go tile HTTP endpoint implemented. [Technical evaluation](docs/NOAA_MBTILES_EVALUATION.md) documents the blockers and design.

**Data and operations safety:** No change to merged authentication, verified personal/Guest Save Changes/Discard Changes, browser history/accounts/settings, weather/current data, station markers, map controls, overlays, production binaries or running processes, launchers, tunneling, deployment, versions/build counters, tags or release. Owner Mac checkout, tokens and persistent private settings were not inspected; no owner files modified remotely.

**Docs / policy:** Current Homepage `main` policy v1.1.0 reviewed; SailingGo AGENTS, README, ARCHITECTURE, DEVELOPMENT and CURRENT_WORK reviewed. The NOAA evidence is appended to Issue #37; CURRENT_WORK, architecture and development acceptance notes updated alongside a minimal map UI status copy change. Runtime NOAA fetch/source behavior untouched.

**Verification:** Exact-head GitHub CI and isolated Go/JavaScript regressions required; Mac/Safari rendering, live NOAA and Terminal-session behavior remain owner-side checks. A successful CI cannot establish NOAA reachability.

**Approvals:** No merge, Issue closure, remote branch deletion, deployment, restart, tag or release. `NEXT` only for further approved scoped work; `FINISH` for accepted closeout.

**Owner Mac commands (normal interactive shell; do not launch or restart SailingGo):**
```sh
cd ~/projects/sailing-go
git status --short --branch
# STOP if any uncommitted changes exist.
git fetch origin
git checkout charts/37-ncds-mbtiles-evaluation
git pull --ff-only origin charts/37-ncds-mbtiles-evaluation
git status --short --branch

go test scripts/noaa_mbtiles_audit.go scripts/noaa_mbtiles_audit_test.go
go run scripts/noaa_mbtiles_audit.go -catalog
```
The catalog command may print a nonfatal timeout notice. Only successful JSON records verify coverage, not a zero process exit.
