# Current work — SailingGo

## Issue #17 / draft PR #18 — seamless Homepage preferences (November 10, 2026)

- **Merged baseline:** GitHub `main` at `931d2e142fda0e70b5bb36e21cd4bf10dded2fc1`, source `v1.16.0` / build `v395`. The candidate uses build `v397` and preserves semantic version `1.16.0`.
- **Active branch/PR:** `feat/17-homepage-preferences` / [draft PR #18](https://github.com/richard-mauri/pittsburg-saildata/pull/18). Owner reported v396 correctly displayed authenticated identity and manual Save/Load, but requested automatic save/load with NewsGo-style identity label. This branch revises the same PR; no new branch, merge, release or restart.
- **Companion dependency:** Homepage Issue #26 / draft PR #27 authorized SailingGo's handoff, `/api/storage/sailing` and CORS. The owner's screenshot confirmed SailingGo was already authenticated as `richard` with the prior candidate. No Homepage changes are required for this UI behavior update; leave unrelated Homepage PR #25 alone.
- **Implementation:** The embedded `web/static/account.js` now displays `Signed in as [user] (Homepage)` below Back to Homepage. It removes manual Save/Load controls, hydrates revisioned user preferences automatically and stages edits for debounced PUT. Changes that navigate to a new URL (e.g. Knots → MPH) are recovered by a tab-scoped pending draft/last-navigation record. Browser-only preferences use a separate localStorage copy; cross-user and revision-conflict writes are blocked. Map overlays, types and category choices remain included.
- **Validation:** `account_preferences_test.go` updated to reject manual controls and check auto-hydration contracts. GitHub Actions `test` passed for the first fully documented v397 head `92ed40bdccf4cd3c37b1c159b90c292d583d8e4c` (run 42843531067). Recheck the exact head after this handoff-document update; completed CI covers automated Go tests/build checks only. Automated Go/JS static checks cannot establish live Mac/Safari behavior; no new Mac binary was built or deployed by the assistant.
- **Outstanding owner acceptance:** Update/check out the branch without overwriting divergent local work; Go formatting/build/tests; restart only with owner authorization. Safari: authenticated identity under Homepage link; Knots→MPH survives return via Homepage, reload and new login; wind/distance, selected location, current station, Planning overlays, 1h/24h history; browser-only after disconnect; second account isolation; revocation and concurrency conflict. Check report text/JSON and that the original tunnel/NOAA behavior remains intact.
- **Merge boundary:** Draft PR only. Owner must explicitly approve merge, version/release and service restart. Do not change the Cloudflare tunnel or unrelated apps.

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

Inspect all changed Go files and local status before installing the candidate. The service is locally run behind the existing Cloudflare Tunnel; use the owner's established launcher and do not start a competing listener. Only restart it after the owner has authorized installation.
