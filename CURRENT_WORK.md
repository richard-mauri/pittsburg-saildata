# Current work — SailingGo

## Issue #19 — direct-visit Homepage session recovery (November 10, 2026)

- **Merged baseline:** GitHub `main` at `d5ccd2c712ab3fc9d2da559148bf6bee069729d0` (SailingGo issue #17 / PR #18), app v1.16.0 build v400. Candidate build v401, semantic version unchanged.
- **Branch:** `fix/19-direct-visit-session-recovery`. Homepage Issue #24 / PR #25 was reconciled against Homepage main with fixed `/launch/sailing` route added; companion NewsGo PR #80 and already-merged MauriStocks PR #12 use the same recovery pattern.
- **Behavior:** Without bearer, a public direct SailingGo visit redirects to trusted Homepage recovery. A previously valid parent session issues a fresh handoff and per-user preferences restore. Missing/revoked/expired session returns explicit browser-only mode, with no loop. Existing account saving and map/unit UX are unchanged.
- **Checks:** JavaScript syntax checked; Go static regression test added. GitHub CI latest head must be checked; local Mac, Safari, Cloudflare, browser-close and dual-account acceptance not performed remotely.
- **Authorization:** Owner requested coordinated reconciliation and merge. GitHub merge will be performed only after CI passes and dependency constraints are satisfied. No remote server restart or tunnel change authorized.

## Owner-side Mac validation

```sh
cd ~/projects/sailing-go
git status --short --branch
git fetch origin
git switch main
git pull --ff-only origin main
gofmt -l main.go wind.go currents.go ui_templates.go account_preferences_test.go
go test -count=1 ./...
go build .
git diff --check
```

After authorized build/server restart, test direct SailingGo visit from a new tab with valid Homepage login, direct visit with expired/no login, explicit `#suite_mode=local`, cross-account isolation, and account preference hydration, plus both views, wind units, overlays, report text/JSON and live Cloudflare behavior.
