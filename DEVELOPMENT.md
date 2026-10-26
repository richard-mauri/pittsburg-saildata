# Mauri's Weather & Water Conditions — development and validation

This is the **authoritative reusable GitHub workflow**, modeled on ScraperGo. Start with [AGENTS.md](AGENTS.md), check [CURRENT_WORK.md](CURRENT_WORK.md), and respect [ARCHITECTURE.md](ARCHITECTURE.md). Normal development takes place in GitHub Issues, short-lived branches and draft PRs; it is **not** a sequence of downloadable replacement-file archives.

## Baseline, Issues, branches and PRs

GitHub `main` is the authoritative merged baseline. Before modifying *local* code, reconcile it with the owner's potentially newer/unpushed `~/projects/sailing-go` tree: inspect status, branches, changes and ahead/behind rather than overwriting it. Check open GitHub PRs and branches at the start of each session.

For a defect, reproduce it and open an Issue with observed versus expected behavior, build, steps, evidence and regression risk. For scoped features or documentation changes, track the agreed change in an Issue as appropriate. Create a dedicated branch from current `main` for that task, e.g. `docs/issue-1-five-file-workflow` or `feature/scrapergo-ui-theme`; avoid reusing stale shared `dev` branches. Open a **draft PR targeting main** with Issue reference, rationale, file/scope inventory, regression risks, test results and owner-side acceptance still required. Unrelated findings belong in new Issues and independent PRs.

Check the PR's **latest exact head commit** before claiming CI passed. No automatic merges. Owner authorization is required before merge, semantic version change, tag, release, deployment or restart.

After an authorized and verified merge, delete a short-lived branch only after confirming its work is merged, it is not used by an open PR, and it has no unmerged unique work. Never bulk-delete historical branches or force-push casually.

## GitHub Actions and limitations

ScraperGo uses `.github/workflows/go-ci.yml` for non-blocking gofmt reporting, `go test -count=1 ./...` and `go build ./...` on PRs targeting `main`, with manual invocation. **As of this documentation PR, the sailing repository has no corresponding workflow.** Add a sailing-specific GitHub Actions workflow in a **separate scoped PR**. Sailing's root `go.mod` currently declares Go 1.13, whereas ScraperGo declares Go 1.23.2. Select a supported CI Go version deliberately and test compatibility rather than copying blindly. If changing the nested `ui/` module, explicitly validate it too.

A CI pass covers only performed jobs; it cannot verify local server startup, Safari, NOAA's current responses or Cloudflare Tunnel. Documentation-only changes require Markdown/content/link/path review and applicable CI, **not** a Mac build/restart.

## Complete owner-side Mac verification for runtime PRs

After checking for uncommitted local changes, substitute the **actual PR branch name** in the following and provide a copy-and-pasteable block in every runtime handoff:

```sh
cd ~/projects/sailing-go
git status --short --branch
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
