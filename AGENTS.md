# Sailing-Go — AI Development Process and Chat Handoff

**Read this first in every development chat.** This is the authoritative engineering workflow and current handoff. `README.md` is the sailor-facing product overview.

## Identity

- Application: **Mauri's Weather & Water Conditions** (sailing conditions dashboard).
- GitHub repository: https://github.com/richard-mauri/pittsburg-saildata — **not** a repository named sailing-go.
- Local project directory: `~/projects/sailing-go` on macOS.
- Public site: https://weather.themauris.org
- Locally running Go server exposed by Cloudflare Tunnel; **not Render** in the current handoff.
- Default wind station: `PSBC1`.
- `main.go` owns routing and embedded UI; `wind.go`, `currents.go` and other package files supply environmental data processing. **Build the entire Go package**, never `go run main.go` alone.

## Current development handoff — October 20, 2026

Last documented user-confirmed local build: **appVersion 1.16.0, buildVersion v382**, from October 19. The GitHub README states v382 came from an uploaded v381 source and the user reported **“works now”** after the History fix. **Do not treat the GitHub main branch as authoritative if current local source differs.** The user's current installed build, local Git status, push status and any edits after this date have not been reconfirmed.

The v382 fix added missing `GET /wind-readings` to match a browser History selector that previously failed with HTTP 404 / “Update failed.” Endpoint should provide `{hours, readings, summary}` using `?station=PSBC1&wind_hours=4&wind_unit=mph` and supported windows of 1, 4, 8, 12, 16, 20 and 24 hours. Verify actual handler/schema before editing. Last known next provisional build is **v383**, only if source inspection confirms v382 remains current.

**Version rule:** increment development **buildVersion vNNN** for each source regeneration. Do **not** bump semantic `appVersion` each iteration; revisit it when preparing a release.

## Product invariants

- Distinguish **observed wind**, **predicted tidal currents**, and **weather/marine forecasts**. Never label predictions as current direct observations.
- NOAA/NDBC observations are primary. Validated CO-OPS station mappings for PSBC1 (9415115), MZXC1 (9415102) and PPXC1 (9414847) may supply fresher wind; don't mix overlapping provider samples in summary statistics. Automatic nearest-usable wind selection uses a documented 30-minute freshness criterion.
- Clicking the map selects a sailing location. **Panning, zooming or Center Map coordinates must not change that selected sailing location.** Preserve both automatic station selection and manual explicit `station=` override.
- Preserve current-station matching as distinct from wind-station matching. Ensure report formats (`html`, `text`, `json`), wind-history endpoint and forecasting/map routes remain compatible.
- Protect Conditions Now / Planning and Details layout; map overlays, wind barbs, pressure, smoke/fire, radar, temperature and other weather/map features. Test changed behavior without disrupting unrelated NOAA sources or tunnel operation.
- Avoid using stale/no-coverage data as if current. Different anemometer heights and station exposure matter when comparing wind.

## Development, regeneration and verification

First inspect actual local source constants and status. If the current local copy is not available, ask for latest affected files; GitHub may lag. Backup originals before replacement. Do not leave extra `.go` backups in the package root: they may compile and cause duplicate declarations.

Source-change workflow documented by this project:

```sh
cd ~/projects/sailing-go
gofmt -w main.go
go test ./...
git diff --check
git status
```

Format **all changed Go files**, not just `main.go`, and review the diff; use the existing local service stop/restart procedure. A manual `go run . -server -port 8080` is only an option if the existing instance is stopped. Do not replace Cloudflare configuration, launch a competing server, or assume deployment to Render.

For user-requested regeneration, preserve the project's established paired-artifact convention: typically `main-updated-vNNN.go` and `README-updated-vNNN.md`. **With the new documentation split, the development handoff belongs in `AGENTS.md` instead**: provide an updated `AGENTS.md` along with changed Go artifacts, plus an updated `README.md` only if user-facing content changed. If maintaining historical filenames is operationally required, ask the user rather than overwrite this division. Do not gratuitously rewrite the README or inflate an archive with every previous build note.

Regression check: open `https://weather.themauris.org/wind-readings?station=PSBC1&wind_hours=4&wind_unit=mph`; distinguish valid JSON from transient NOAA failures, and check History selector/chart, mph/knots, 1h/24h selection, other report views and unaffected map selection. Never call this checked unless executed or user-confirmed.

## Non-negotiable agent conduct

- Read this file and `README.md` in full before changing code, advising Git operations, or claiming completion.
- The user's verified **local source tree** is authoritative whenever it differs from GitHub. Obtain current affected files and reconcile local changes; never overwrite them from older remote sources.
- Trace **both definitions and actual call sites**, handlers, UI uses, tests, and configuration dependencies. A new function that is not called is **not** a finished fix.
- Protect unrelated working behavior. Keep changes minimal and test existing regression areas.
- Distinguish **drafted/generated**, **verified file exists**, **formatted**, **tested in full project**, **tested in isolated harness**, **installed**, **user-verified live**, **committed**, **pushed**, **tagged**, and **released**. Never imply one means another.
- If you cannot run tests or produce a real artifact, say so. Never invent a download link, test result, deployment, or Git action.
- Do not commit, push, tag, release, restart services, deploy, or modify tunnel configuration without the user's explicit authorization.

## Mandatory Git pre-commit audit — learned from ScraperGo failure

**Never compose a `git add` filename list from memory, an earlier ZIP manifest, or an assumption about changed files.** A previous agent omitted a crucial `readability.go` integration change in ScraperGo; the user found it before pushing. This is a cross-project safeguard.

For a commit request, **separately from normal patch installation**:

1. Obtain fresh `git status`, including staged, unstaged and untracked files, and ahead/behind state.
2. Examine `git diff --check`, `git diff`, `git diff --cached`, and relevant untracked files. If commits already exist locally, review `git log origin/main..HEAD` and commit file inventories.
3. Check all changed source files **and integration points**, tests, both documentation files, and build/configuration artifacts as applicable. Exclude secrets, local tokens, binaries, logs and unrelated temporary files.
4. Only after that evidence recommend exact staging commands. Before committing, verify `git diff --cached --name-status` and `git diff --cached` against the intended scope.
5. Do not infer that a clean working tree means the correct commits were made, or that ahead-of-origin commits were pushed. Verify publication independently.

## Cross-chat handoff obligations

At the start of a new chat, use `AGENTS.md` for the **current engineering handoff** and `README.md` for the public-facing project context. Confirm current local source and Git state before applying edits. Maintain this file with a short, dated snapshot of the most recent verified baseline, pending work, confirmed tests, known regressions and next action; **replace obsolete current-state claims**, rather than accumulating contradictory updates. Keep the README focused on users and general architecture, not chat history or agent rules. Preserve the existing project's patch/install convention; do not transplant another project's conventions blindly.

