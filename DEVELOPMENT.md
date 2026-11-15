# SailingGo — app-specific development and validation

Read [AGENTS.md](AGENTS.md) and the complete [generated suite policy](docs/suite-development-policy.md). Shared Issues, branch/PR rules, six-part handoff, FINISH and cleanup are exclusively Homepage policy. These sections retain SailingGo's unique CI, local divergence, build and Mac/Safari requirements.

## GitHub Actions and limitations

ScraperGo uses `.github/workflows/go-ci.yml` for non-blocking gofmt reporting, `go test -count=1 ./...` and `go build ./...` on PRs targeting `main`, with manual invocation. **The repository now has a documentation policy validation workflow; it is **not** a Go CI workflow.** A sailing-specific Go Actions workflow remains a separate scoped runtime/CI decision. Sailing's root `go.mod` currently declares Go 1.13, whereas ScraperGo declares Go 1.23.2. Select a supported CI Go version deliberately and test compatibility rather than copying blindly. If changing the nested `ui/` module, explicitly validate it too.

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
gofmt -l main.go wind.go currents.go
go test -count=1 ./...
go build .
git diff --check
```

Review **all** changed Go files for formatting, not only the three listed. Run relevant checks from `ui/` if that module changed. Do not start a competing listener or alter the local Cloudflare launcher. A manual `go run . -server -port 8080` is only appropriate if the existing instance has been stopped and current flags support it.

## Safari and operational acceptance

After the owner intentionally installs/launches a candidate, verify Conditions Now and Planning and Details, History time windows including 1h and 24h, mph/knots switching, wind recency and station overrides, predicted-current semantics, map click versus pan/zoom, overlays, and text/JSON endpoints. The `/wind-readings?station=PSBC1&wind_hours=4&wind_unit=mph` response should maintain its documented schema when upstream NOAA data are available. Record pass/fail/not-performed for every claimed check. Browser and live-weather results are separate from Linux CI.

Follow the fresh-status/diff/staging audit in AGENTS.md before commits. For development source regenerations increment `buildVersion` but do not bump `appVersion` except for owner-approved release preparation. Never include local binaries, credentials, private counters, archives or tunnel configuration in the PR.
