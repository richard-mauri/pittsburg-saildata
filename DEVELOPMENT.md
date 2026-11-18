# SailingGo — app-specific development and validation

Read [AGENTS.md](AGENTS.md) and [Homepage's authoritative suite development policy](https://github.com/richard-mauri/homepage/blob/main/SUITE_DEVELOPMENT_POLICY.md). Shared Issues, branch/PR rules, six-part handoff, FINISH and cleanup are exclusively Homepage policy. These sections retain SailingGo's unique CI, local divergence, build and Mac/Safari requirements.

## GitHub Actions and limitations

SailingGo has its own `.github/workflows/go-ci.yml`: non-blocking formatting report, root Go tests and a root build on PRs targeting `main`. Verify its actual outcome on the exact PR head. The root `go.mod` now declares Go 1.22 because Homepage's pinned shared UI module requires it; CI uses Go 1.23. The nested `ui/` module remains independent and requires separate testing when edited. The shared Homepage policy does not replace app-specific CI.

A CI pass covers only performed jobs; it cannot verify local server startup, Safari, NOAA's current responses or Cloudflare Tunnel. Documentation-only changes require Markdown/content/link/path review and applicable CI, **not** a Mac build/restart.

## Complete owner-side Mac verification for runtime PRs

After checking for uncommitted local changes, substitute the **actual PR branch name** in the following and provide a copy-and-pasteable block in every runtime handoff:

```sh
cd ~/projects/sailing-go
git status --short --branch
# Stop if uncommitted changes are shown.
git fetch origin
git checkout ACTUAL-PR-BRANCH
git pull --ff-only origin ACTUAL-PR-BRANCH
./devbuild.sh
./sailing-go -version
```

`./devbuild.sh` is the **single** owner build/test command: it tests the root application package and builds the root binary. Do not separately request `go test` or `go build` from the owner. Review changed Go files for formatting; the CI formatting report is informational. Run relevant checks from `ui/` if that module changed. Do not start a competing listener or alter the local Cloudflare launcher. The production launcher `./sailgo.sh` may be run **only with separate owner authorization**, since it restarts the app and tunnel.

## Safari and operational acceptance

After the owner intentionally installs/launches a candidate, verify Conditions Now and Planning and Details, History time windows including 1h and 24h, mph/knots switching, wind recency and station overrides, predicted-current semantics, map click versus pan/zoom, overlays, and text/JSON endpoints. The `/wind-readings?station=PSBC1&wind_hours=4&wind_unit=mph` response should maintain its documented schema when upstream NOAA data are available. Record pass/fail/not-performed for every claimed check. Browser and live-weather results are separate from Linux CI.

Follow the fresh-status/diff/staging audit in AGENTS.md before commits. For development source regenerations increment `buildVersion` but do not bump `appVersion` except for owner-approved release preparation. Never include local binaries, credentials, private counters, archives or tunnel configuration in the PR.
