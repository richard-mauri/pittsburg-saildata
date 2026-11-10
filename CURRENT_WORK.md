# Current work — SailingGo

## Issue #17 / draft PR #18 — no-reload units and seamless Homepage preferences (November 10, 2026)

- **Merged baseline:** GitHub `main` at `931d2e142fda0e70b5bb36e21cd4bf10dded2fc1`, source `v1.16.0` / build `v395`. The candidate is `v1.16.0` / build `v399`; semantic version unchanged.
- **Active branch/PR:** `feat/17-homepage-preferences` / [draft PR #18](https://github.com/richard-mauri/pittsburg-saildata/pull/18). This is the same issue and PR; no new branch. The owner tested v398 and reported that manual Knots/MPH changes and saved preference restoration caused disruptive full-page reloads.
- **Companion Homepage:** Issue #26 / draft PR #27 supplies the `sailing` storage namespace, trusted CORS origin and signed-in handoff. Owner's prior screenshot verified that SailingGo displayed authenticated identity; no Homepage changes are required for the no-reload unit correction. Keep Homepage's unrelated draft PR #25 separate.
- **Current change:** Existing unit select handlers now dispatch a `sailing:unit-change` event and update URL with `history.replaceState` instead of `window.location.href`. New embedded `web/static/unit_refresh.js` fetches refreshed server values, patches static display text while retaining original DOM/map/chart/event handlers and uses the existing wind history JSON refresh controller. Failed requests restore the prior unit selector and URL instead of leaving mismatched units. The account client applies unit-only changes in place without `location.replace`; a bounded initial mask hides only unit-dependent values until the authenticated account preference read completes, avoiding a visible wrong-units → full-refresh sequence. Location/station/planning changes still use deliberate server navigation where required. No change to NOAA observation vs current prediction semantics.
- **Other account behaviors retained:** Homepage-only identity, per-user revisioned GET/PUT, no manual Save/Load buttons, cyan `Signed in as [user] (Homepage)` under Back to Homepage, browser-only localStorage, draft recovery, 409 conflicts and user isolation, overlays/map selections.
- **Validation:** `account_preferences_test.go` adds a no-unit-navigation contract. JavaScript sources parsed successfully in code inspection; GitHub Actions `test` previously passed for the intermediate code/docs head `12d77b209872a21861045988667fc6f1bc793f49`. Recheck the *latest exact PR head* after this handoff update. Mac/Safari/live tunnel checks for v399 **not performed** by assistant; source-only CI does not verify browser appearance or live NOAA behavior.
- **Owner acceptance:** Inspect local Git tree for unpushed changes before checkout. Run complete Go formatting/test/build procedure below. After owner-approved install, use Safari to verify selector Knots↔MPH changes values *in place* (no full document reload or map reset), account return restores MPH with no visible wrong-units flash/reload, distance selectors, wind history 1h/24h and both unit labels, Planning map overlays and station/manual location selection, click vs pan, signed-out browser-only, another user isolation, expired bearer and 409 conflict. Verify report text/JSON compatibility. No merge/tag/release/restart or Cloudflare modifications authorized by this PR.

## Complete owner-side Mac verification and build

```sh
cd ~/projects/sailing-go
git status --short --branch
git fetch origin
git checkout feat/17-homepage-preferences
git pull --ff-only origin feat/17-homepage-preferences
gofmt -l main.go wind.go currents.go ui_templates.go account_preferences_test.go
go test -count=1 ./...
go build .
git diff --check
```

Review all changed Go files and any local unpushed work before installing. Use the existing service launcher only after the owner authorizes the restart, and do not start an additional listener or Cloudflare tunnel.
