# Current work — SailingGo

## Issue #21 — optional Homepage activity telemetry (November 12, 2026)

- **Authoritative base:** GitHub `main` (before this branch: v1.16.0, build v401), existing Homepage direct-visit recovery integration already on main. **New draft:** [Issue #21](https://github.com/richard-mauri/pittsburg-saildata/issues/21), branch `feat/21-suite-activity`, [PR #22](https://github.com/richard-mauri/pittsburg-saildata/pull/22).
- **Scope:** optional browser-tab presence and server work instrumentation, including bounded background swell prefetch. Separate service-only `HOMEPAGE_ACTIVITY_KEY_SAILING` and `SAILING_ACTIVITY_URL`, with service name `sailing` and 60-second aggregate reports to Homepage draft collector PR #30. Browser IDs remain local to SailingGo; personal coordinates, station IDs, tokens and query data are excluded. Signed-in classification is unverified and included in anonymous counts.
- **Preserved:** NOAA observed wind, predicted tidal currents, station/units/overlays, all HTML/text/JSON routes, Homepage identity and per-user persistence. Production listener unchanged. Development `buildVersion` increments v401→v402; semantic `appVersion` remains 1.16.0. No deployment/restart/release/merge.
- **Validation:** Go unit tests for reporter opt-in, same-origin validation, protocol, presence windows and template wiring. GitHub Actions Go CI #144 passed tests and build at code checkpoint `db11e9e` after replacing a Go 1.18-only alias to respect the root Go 1.16 language version; this documentation head needs recheck. Mac/Safari and live NOAA/tunnel behavior not yet tested.
- **Next:** verify CI, then run a detached isolated Mac test binary on loopback 8087 alongside the existing disposable Homepage test server on 8788 with a fresh matching private key. Confirm the SailingGo row changes from unknown to reporting in the admin dashboard, without stopping production. Owner approval required before any merge or restart.

## Full owner Mac acceptance procedure (runtime restart separately authorized)

```sh
cd ~/projects/sailing-go
git status --short --branch
git fetch origin
git checkout feat/21-suite-activity
git pull --ff-only origin feat/21-suite-activity
gofmt -l main.go wind.go currents.go activity.go activity_test.go ui_templates.go
go test -count=1 ./...
go build .
git diff --check
```

Do not launch the owner’s production process or change Cloudflare tunnel settings without explicit permission. This procedure could disturb local unpushed work, so inspect status/diffs and use a separate worktree for the isolated UI smoke test.
