# Current work — SailingGo

## Issue #23 — Homepage guest/session identity and activity

- **Baseline:** GitHub `main` includes optional Homepage activity and persistent direct-visit Homepage recovery (baseline `2e38845`, build v402). Branch `feat/23-session-aware-activity` starts from clean GitHub main. Issue https://github.com/richard-mauri/pittsburg-saildata/issues/23; draft PR https://github.com/richard-mauri/pittsburg-saildata/pull/24.
- **Server change:** When optional activity reporting is configured, validate each supplied Homepage child bearer against `/api/session/profile` using the trusted SailingGo origin. Count verified parent sessions and distinct users using Homepage's origin-scoped pseudonymous aliases. Duplicate tabs share one signed-in session; names are summarized privately to the existing Homepage collector, but bearer values and aliases never leave SailingGo in telemetry. Revoked/invalid supplied bearer yields 401; temporary Homepage verification failure yields 503; anonymous pings remain distinct.
- **Browser and preferences:** The existing account client recognizes verified one-hour guests without attempting `/api/storage/sailing`; guest changes stay browser-only. A non-dismissible sign-in dialog offers an explicit Go to Homepage link when a public site's handoff is absent/expired/revoked. No automatic redirection on revocation. The existing fixed Homepage `/launch/sailing` recovery handles first direct visits, and existing named-user revisioned storage is unchanged.
- **Preserved:** NOAA observations, predictions, station selection, map location/viewport, history, wind and distance units, overlays, Planning, HTML/text/JSON routes, listener and Cloudflare tunnel. Development build v403; semantic appVersion remains 1.16.0.
- **Validation:** JavaScript parse and whitespace/source checks passed locally. GitHub Go CI #148 passed both root package tests and build at code commit `3bcc97c22385d63e69637b94263f0c24837649a5`; this documentation status update needs its own exact-head check if committed. Mac Safari/live weather/tunnel acceptance not performed by agent. No merge, restart, tag, release, credential edit or deployment authorized.

## Owner Mac acceptance (no automatic restart)

```sh
cd ~/projects/sailing-go
git status --short --branch
git fetch origin
git checkout feat/23-session-aware-activity
git pull --ff-only origin feat/23-session-aware-activity
gofmt -l main.go wind.go currents.go activity.go activity_test.go
go test -count=1 ./...
go build -o /tmp/sailing-session-check .
git diff --check
/tmp/sailing-session-check -version
```

Review status and local unpushed changes before checkout; use a separate worktree if needed. Do not run the owner's normal launcher or compete for port 8080 without separate authorization. Safari acceptance: named and one-hour guest handoffs, browser-only guest preferences, revoked-session modal, Homepage per-user activity, duplicate-tab session count, Conditions Now/Planning/History, units, NOAA wind/current interpretation, map pan versus location selection, overlays and JSON/text routes.
