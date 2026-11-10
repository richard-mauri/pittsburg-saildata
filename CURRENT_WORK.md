# Current work — SailingGo

## Issue #17 / draft PR #18 — authenticated Homepage preferences (November 10, 2026)

- Authoritative merged baseline: GitHub main at `931d2e142fda0e70b5bb36e21cd4bf10dded2fc1`, source version `1.16.0` / build `v395`.
- Branch: `feat/17-homepage-preferences`, draft PR #18. Candidate build increments to `v396`; semantic version remains `1.16.0`.
- Related Homepage issue #26 / draft PR #27 must be approved and deployed in coordination for account storage and handoffs. No source edits to Homepage's open unrelated draft PR #25.
- Scope: embedded Homepage handoff/profile/storage client; explicit Save/Load, schema checks, optimistic concurrency, map and URL preferences, browser-only fallback and account switch isolation. Report/UI behavior otherwise unchanged.
- Added `account_preferences_test.go` with basic injection and safety assertions. Latest-head GitHub CI pending verification; Mac Go test/build and Safari live acceptance not performed. Need test genuine account roundtrip, browser-only mode, second user data isolation, conflict, logout/revocation, map overlay restore, Conditions Now ↔ Planning changes and station selection.
- GitHub main is the merged baseline; owner local `~/projects/sailing-go` may contain newer unpushed work. Verify status/ahead/behind and do not overwrite. No merge, release, launch or tunnel change authorized.

## Owner-side Mac checkout and validation

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

Check all changed Go files for formatting. Launch only after owner authorization and using the existing launcher; never start a competing listener. In Safari verify Conditions Now, Planning and Details, map overlays, selected map point vs pan, wind history 1h/24h, mph/knots, report text/JSON and the signed-in preference roundtrip.
