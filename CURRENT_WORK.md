# Current work — SailingGo

**Active documentation review:** [Issue #25](https://github.com/richard-mauri/pittsburg-saildata/issues/25), branch `docs/25-session-markdown-refresh` targeting `main`, [draft PR #26](https://github.com/richard-mauri/pittsburg-saildata/pull/26). This branch changes only Markdown; exact-head CI and owner merge approval are pending.

GitHub `main` at `bb23d2b` contains merged Issue #23 / PR #24: verified Homepage guest/named identity, browser-local guest preferences, per-user activity and a non-dismissible sign-in dialog. No issues or PRs remain open and no implementation branch is active. The owner verified Safari guest identity, revocation and absence of the white details-transition flash after the companion Homepage bridge fix. Development build v404; semantic appVersion 1.16.0.

Homepage owns sessions and renewal. SailingGo verifies supplied child bearers for optional activity (401 for invalid/revoked, 503 for verification outage); guest choices stay browser-local while named-user `/api/storage/sailing` settings remain revision-controlled. Direct visits recover via the fixed Homepage `/launch/sailing` bridge. Expired/revoked identity offers deliberate Homepage sign-in without silent renewal. NOAA observed wind, tidal-current predictions, station/location vs viewport, Planning, History, overlays, formats, listener and tunnel remain unchanged.

**Next:** reconcile Markdown through a new docs-only draft PR from `main`, checking paths/links and exact-head CI. No Mac restart or Safari retest for documentation alone. Future runtime work requires owner Mac/Safari/tunnel acceptance and approval before merge, tag, version, release or restart; reconcile possible unpushed `~/projects/sailing-go` work first.
