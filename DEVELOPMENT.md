# SailingGo — app-specific development and validation

Read [AGENTS.md](AGENTS.md) and [Homepage's authoritative suite development policy](https://github.com/richard-mauri/homepage/blob/main/SUITE_DEVELOPMENT_POLICY.md). Shared Issues, branch/PR rules, six-part handoff, FINISH and cleanup are exclusively Homepage policy. These sections retain SailingGo's unique CI, local divergence, build and Mac/Safari requirements.

## GitHub Actions and limitations

For Issue #33, compare all four HTML pages with Homepage, NewsGo, ScraperGo and
MauriStocks at identical Safari widths. The embedded Homepage banner and
`.suite-content` must share the left/right boundaries, top spacing and
responsive breakpoints. Confirm one locally served Tanner portrait, one
Homepage return link, no app-specific banner offsets, and an unsuffixed verified
identity. Check that preference conflict/guest warnings remain visible outside
the banner. Preserve all NOAA, map and preferences behavior. Run the documented
`./devbuild.sh` once; do not add standalone `go test`/`go build` owner commands.

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

## SailingGo shared-auth security migration — Issue #35

This is a staged cross-route migration. The first phase adds `sailing_suite_auth.go` and deterministic `sailing_suite_auth_test.go`, pins the Homepage `shared/auth` module and verifies the standalone guard. The guard is not wired into the running listener, so neither CI nor current Safari behavior establishes protected weather APIs. A subsequent scoped phase must inventory and migrate root HTML/text/JSON weather routes and each map/overlay background request together, preserve direct Homepage handoff and Guest access, then enable and test the gate. Never enable an API gate alone while browser fetches lack an authenticated header, and never silently exempt sensitive NOAA/report routes. Keep browser-only fallback disabled in the final auth design. Run `./devbuild.sh` once on the owner's Mac and verify `./sailing-go -version`; do not run `./sailgo.sh` without separate restart permission.

## Verified Guest display acceptance (Issue #35 draft)

After building the current `auth/35-verified-suite-access` candidate using only `./devbuild.sh`, open SailingGo from Homepage as a verified Guest. Confirm that **Signed in as guest-…** appears in the shared banner, and no second **Guest (browser-only preferences)** line appears above the weather hero. Guest unit/overlay choices must still persist locally and must never issue a personal Homepage settings PUT. Sign-in-required and revoked/unavailable session warnings must still appear. This visual check requires owner Safari acceptance; the Go string-contract regression cannot verify actual Safari rendering. `./sailgo.sh` remains owner-authorized only, because it restarts SailingGo and its own tunnel.

## Owner Safari acceptance — explicit Save Changes (draft PR #36)

Build the branch once with `./devbuild.sh`, check `./sailing-go -version`, and intentionally start the owner-managed launcher only with separate authorization. In a verified personal session, change knots to mph, change an overlay, and navigate to Planning and Details: Save Changes should become enabled, a pending notice should appear, and leaving without saving should warn. Save, then reload, confirming persisted units/overlays and a cleared pending state. In a verified Guest session, repeat: Save Changes should commit only to browser-local preferences and never call Homepage personal `PUT /api/storage/sailing`. Verify new accounts do not auto-create records, failed writes retain drafts, a 409 conflict blocks overwriting another revision, a change across URL navigation is retained, and another personal identity cannot inherit a previous user's unsaved draft. The previous verified Guest status remains quiet unless there is an actual error. CI runs `scripts/test-explicit-save.cjs` on its GitHub runner; owner Mac needs only Go. Map overlays, selected location vs pan/zoom, NOAA observational and prediction data, and text/JSON routes must not regress.

### Safari acceptance for Discard Changes — Build v429

On either report view with a verified personal or Guest session, switch **Map Type** Street → Nautical and observe Save/Discard enabled, then return to Street and observe both disabled and no leave-page prompt. Change an overlay and click **Discard Changes**: confirm the last saved overlay/map state is restored, no personal Homepage account PUT occurs, and a new visit does not resurrect the draft. Verify a genuine unsaved change still warns when returning to Homepage; if choosing Leave and later returning in the same tab, the pending edit remains recoverable until Discard. Repeat for unit changes (including in-place refresh) and with a saved Guest preference. Also check account 409 conflicts: Discard should be available, whereas Save remains blocked. Exact-head CI runs Go tests and CI-only Node browser-behavior checks; this does not replace Safari acceptance. Use only `./devbuild.sh` for Mac test/build.

### Repro for Map Type event-order regression (Build v430)

On the owner Mac with the current PR candidate, in Safari's Planning and Details map, start with saved **Street Map**. Choose **Nautical Chart** at a supported zoom (9+), confirm Save/Discard activate; choose **Street Map** again, confirm both disable, the unsaved-status text clears and returning to Homepage no longer prompts for unsaved changes. Repeat as Guest and as a personal user. The CI-only JavaScript regression intentionally runs the real capture listener before the simulated map controller updates the `map_layer` URL; this is materially different from testing manually assembled preference values. The map controller must still update the URL normally after the account listener; selected location and overlays remain intact. Mac build/test: `./devbuild.sh` only.
