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

### Canonical saved-state regression (Build v431)

Repeat Street → Nautical → Street with an older saved account/Guest record that has no keys for currently unchecked overlays or marine-place categories. Save and Discard must both disable after the reversal. Turn an overlay on and verify both activate; turn it off and verify both disable again. Test both report views and both account types. The CI-only real capture-handler fixture now includes these controls instead of an empty map, but Safari acceptance remains required.

### Incremental preference-state acceptance (Build v432)

Repeat the above while the map URL contains viewport parameters such as `map_center_lat`, and with a saved preference record whose `mapType` was empty (legacy default). A Map Type radio event must only change map type/layer in its staged state; switching back to Street must clear Save/Discard. Change and reverse one marine-place category as well. Verify a genuinely changed map center, selected location or station still persists through the existing URL navigation flow; the new event handler must not drop independently staged navigation edits. Safari remains authoritative for UI behavior.

### Tidal-current Range Save regression (Build v433)

On Conditions Now, use the Tidal Current **Range** selector to switch from a saved 1-day range to 3 days: Save and Discard must enable even though the chart refreshes without page navigation. Switch to 7 days; the pending value must become 7, without any Homepage PUT. Return to 1 day; both buttons must disable. Repeat for a verified Guest and personal user, and verify that explicitly saving a 3-day range survives a reload. Confirm that the chart, dates and existing AJAX refresh behavior remain correct; CI-only tests cannot validate Safari rendering.

### Full control-inventory acceptance — Build v434

On both Conditions Now and Planning/Details, test every available dropdown and checkbox. In particular, test Swell playback Speed 1× → 2× → 1×, Step 3h → 6h → 3h, ALERTCalifornia display Clustered → All → Clustered, ALERTCalifornia visibility off/on, marine-place and weather overlays, and tidal-range overlay checked/unchecked. Each true net change must enable Save and Discard; reversing the only change disables both without a leave-page warning. Save must restore settings after navigation/reload; Discard must restore saved controls, map/controller state and chart right-axis display without writing anything. Verify tidal-range checkbox remains saved/pending after in-place Tidal Current Range changes and Previous/Next range navigation. Perform as both a personal account and Guest; only personal saves may PUT to Homepage. CI now asserts exact template selector/checkbox inventories and exercises real account change events plus restoration; Safari remains the authority for user-visible correctness. Use `./devbuild.sh` once for local tests/build, `./sailing-go -version` to confirm v434, and the owner-only launcher for any restart.

### Wind History and preferred-period planning acceptance — Build v435

On both Conditions Now and Planning/Details, with a saved 4-hour Wind History, change History to 8h and then back to 4h. Save/Discard must enable for 8h and disable after returning to 4h, even for an older record that explicitly stores 4 hours. Repeat with another saved window (e.g. 12h). Under **Preferred-period planning hint**, change each of the eight controls—Start, End, Ebb caution, Ebb red, Flood caution, Flood red, Caution time buffer and current-station distance caution—then reverse it. Each distinct value must enable Save; restoring the saved value must disable both controls when nothing else is pending. Test a nondefault saved flood caution of 1.9 kt and a saved preferred period: Save then reload, and Discard without writing. After AJAX current chart updates (including selecting a tidal range), the planning form should reflect the saved or pending values, without a second fetch triggered by restoration. Test personal and Guest, and verify no automatic Homepage PUT. CI exercises the live capture handler with these control IDs but does not replace Mac/Safari acceptance. Run only `./devbuild.sh` for Mac build/tests, and restart using `./sailgo.sh` only with separate owner permission.

## Browser and server access acceptance — Build v436

Follow the documented clean-tree fetch/checkout and run ./devbuild.sh once plus ./sailing-go -version. The owner alone may authorize executing ./sailgo.sh to restart the separate production listener/tunnel. From Homepage test personal and Guest handoff in Safari, verify fragment removal and report reloads, Conditions Now, Planning and Details, unit changes, wind history and tidal current chart AJAX, station selection, map clicks vs pan, NOAA observed wind vs predicted tidal-current semantics, and forecast overlays. Direct anonymous JSON/text/weather and marine-places data routes must be 401; anonymous report HTML must contain only the no-data sign-in bootstrap; revoked/expired identities must be denied and Homepage outages must not grant access. Check same-origin browser fetches, legitimate activity reporting, public health/welcome, Safari Private mode and Guest local preferences. Direct API clients require Authorization Bearer credentials; never put tokens in URLs, shell history, screenshots or reports. CI cannot establish live Safari/Cloudflare/NOAA acceptance, and deployment is not authorized by NEXT.

### Safari acceptance for no-flash launch and chart updates — Build v437

After running `./devbuild.sh` once on the checked-out development branch, intentionally start the candidate only with separately authorized owner operations. Test Homepage personal and Guest launch on Safari in both light and dark appearance. The authentication handoff must never show a stark white page on dark appearance: the interim page background and content panel must match the shared SailingGo colors. On Planning and Details change tidal-current Range (1, 3, 7 days), preferred-period Start/End and threshold/time-buffer inputs. The existing dashboard cards must remain fully opaque while updates arrive; no entire-page flash, reload, duplicated loading animation or automatic Save should occur. Simulate a chart network failure and ensure the old chart and unsaved preferences stay intact with an inline error. Verify switching back to saved values disables Save/Discard, and a manual Save persists. Check that live NOAA observed wind, predicted current and map overlays still refresh correctly; CI does not substitute for these real browser checks. Never run `./sailgo.sh` without separate owner authorization.

### Safari Save/Discard layout acceptance — Build v438

Test both Conditions Now and Planning and Details at wide and narrow Safari window widths. Change a preference, click Save Changes and Discard Changes, repeat with Guest browser-only settings, and verify that the unit selectors and the two buttons do not shift horizontally when the status changes between unsaved, saving, saved, cleared, conflict and failure notices. A small status message must appear on its own reserved line below the controls; the space stays reserved even when no message is present. Verify the status is still announced as an ARIA live region and explicit Save remains the only persistence action. Follow normal clean-tree/build procedures; do not run the launcher or restart a service without separate owner approval.

### Nautical map and radar acceptance — Build v439

On Mac Safari with the owner's candidate running, zoom to 9 or closer and select Nautical Chart. Verify that the map never becomes an empty gray/white plane: Street Map should remain visible beneath NOAA chart content, and an explicit message should appear if all NOAA chart tiles fail. Return to Street Map, then select Nautical again and verify the saved/unsaved Map Type state still behaves correctly; an upstream failure must NOT silently change the selected preference or cause an account PUT. Test NOAA chart at the Bay/Delta and at different zoom levels. Enable Weather Radar with clear and active precipitation views, pan/zoom and confirm no blank opaque square blocks cover the basemap; regions with no rain should be transparent. Check partial/unavailable tile errors display understandable status and map remains usable. Also regression-test satellite, hybrid, overlays, marine places, map click selection vs pan, units, chart updates and Conditions Now/Planning navigation. Tile-server behavior and NEXRAD alpha cannot be established by CI; capture Safari network/image evidence if still incorrect. Use only documented clean-tree `./devbuild.sh` and an owner-authorized launcher; do not restart processes/tunnels automatically.

### NOAA chart outage notice — Issue #37 acceptance

When the owner intentionally launches the candidate after `./devbuild.sh` and `./sailing-go -version`, select Nautical at zoom 9+ while the NOAA chart WMS is unavailable. Confirm the visible notice reads “NOAA nautical charts are temporarily unavailable. Showing Street Map instead; please try again later.” Confirm the map remains usable on Street, selected Nautical preference is not overwritten/saved, no Homepage account settings PUT occurs, the overlay and station markers remain visible, and Street/Satellite switching is unaffected. Verify that a functioning NOAA service does not show the notice. The existing CI-only `scripts/test-explicit-save.cjs` checks the message wiring and prior fallback; it does not simulate a real outage in Safari. Do not deliberately interrupt other services or modify tunnels to trigger this test. NOAA weekend maintenance is unconfirmed. The downloaded 19c file remains outside Git and unserved. Launching `./sailgo.sh` still requires separate owner authorization.

### Nautical selected but no visible chart — NEXT acceptance (Issue #37)

After the normal `./devbuild.sh` and `./sailing-go -version` checks, test the draft candidate **only with an owner-authorized local launcher**. At zoom 11 around Pittsburg select Nautical. If WMS tiles never respond, after about 12 seconds the app should keep Street visible and display **“NOAA nautical chart tiles are not responding. Showing Street Map instead; please try again later.”** If WMS responds with images but nautical detail remains invisible, after about 12 seconds it should show **“If NOAA chart detail is missing, the service may be unavailable or outside chart coverage. Street Map remains underneath; try again later.”** Do not assume successful WMS tile loads guarantee nontransparent nautical content, and do not claim NOAA maintenance unless separately confirmed. The notice must be readable on narrow Safari, selected Nautical input/preferences must remain unchanged, no account PUT may occur, other map types and overlays must work, and switching back to Nautical after changing map types should attempt a fresh load. A genuine working nautical layer may still show the conditional advisory; this is acceptable pending independent image-pixel evidence. Owner Safari, real network and service process are unverified by CI. No `./sailgo.sh` restart without separate authorization.

### Owner Safari verification — selected-map timeout fix, Issue #37

Owner's Nov. 21, 2026 Safari diagnostic recorded ten pending chart tiles, zero loaded or failed, and Nautical selected with the warning hidden. After safely building the draft branch using `./devbuild.sh` once and checking `./sailing-go -version`, run the candidate only through a **separately owner-authorized** local launcher. In Safari at zoom 10–11 near Pittsburg, select Nautical (or reload with a saved Nautical selection). Even if NOAA WMS requests remain pending and never emit a Leaflet `loading` event, the on-map notice should appear after approximately 12 seconds: **“NOAA nautical chart tiles are not responding. Showing Street Map instead; please try again later.”** The map must remain usable on Street without switching the saved Nautical preference or creating a Homepage settings PUT. Switching Street → Nautical again should re-arm a single bounded timeout; leaving Nautical cancels it. Verify markers, other overlays and Save/Discard, and confirm the notice wraps at narrow Safari widths. If NOAA tiles load, chart detail cannot be inferred from request completion; the conditional advisory still applies. This CI-only regression does not prove the owner Mac/Safari result; no automatic service/tunnel restarts or NOAA maintenance claim.

### Loading feedback and automatic radio reset — Issue #37 Safari acceptance

After an owner-authorized local candidate launch, select Nautical at zoom 9+ in Safari. Confirm a visible **Loading NOAA nautical chart…** message and **Cancel** control appear promptly, with no fictitious percentage or blocked map interactions. Click Cancel; expect Street radio selected, Nautical tiles hidden, loading status cleared, no saved account setting write, and Save/Discard state reflecting the pending map choice as appropriate. Re-select Nautical with NOAA WMS requests stalled; after about 12 seconds expect a readable **not responding / Street fallback** message and **Street radio selected**, not Nautical. Confirm the warning remains visible after the radio switches, the map and markers still work, no infinite busy state remains, and reselecting Nautical gives another attempt. Confirm successful NOAA tile loads clear the busy state (existing conditional coverage advisory may remain). Verify both personal and Guest Save/Discard without auto-saving either setting. The root build remains `./devbuild.sh` once; launch/restart still needs separate owner authorization. GitHub CI cannot prove live Safari event handling or NOAA availability.

Verify the NOAA loading state now includes an **indeterminate animated progress bar** labeled for assistive technology. Cancel and timeouts must hide the bar, and `prefers-reduced-motion` must disable animation. No automatic account preference save occurs.

### One Nautical loading/failure panel — Safari acceptance

Select Nautical around Pittsburg while NOAA WMS tile requests are stalled. The original map status panel should immediately show loading text, indeterminate progress and **Cancel**. After 12 seconds with zero successful tiles, the **same panel** must show the try-later failure message and **Close**, with progress and Cancel gone and Street radio selected. Click Close to dismiss it instantly. On a second attempt, allow it to time out without clicking Close and verify the panel **auto-dismisses after another eight seconds**. A different map selection must clear any lingering failure message. Confirm the map, markers and overlays remain usable, no Homepage settings PUT happens automatically, and normal Save/Discard still operates for personal and Guest sessions. No distinct warning popup is permitted. The candidate still requires owner Mac/Safari acceptance; CI cannot substitute for it.

### Nautical Go tile framework validation — Issue #37

The browser must fetch `/api/charts/tiles/{z}/{x}/{y}.png` from SailingGo, never NOAA WMS. With `SAILING_NAUTICAL_TILE_DIR` unset, the endpoint responds HTTP 503 and the pre-existing loading/Cancel/error/Close panel returns to Street Map; there should be **no direct NOAA chart traffic in Safari's Network inspector**. Root Go tests include unconfigured (503), missing (404), valid PNG (200), HEAD and invalid paths/methods, and Node CI verifies the browser URL. These tests are offline and do not require NOAA. The optional local directory must contain actual XYZ `z/x/y.png` PNGs; the downloaded `ncds_19c.mbtiles` database is **not** directly supported yet. Do not configure a nonempty directory against unverified NOAA data. With validated local XYZ tiles, configure the absolute directory using the service's normal environment setup **only with separate owner authorization to change runtime configuration**, rebuild/restart when authorized, and verify a real map at multiple zoom levels in Safari. Keep chart downloads outside Git; no automatically staged download, credentials or new background task.

### Go NOAA WMS fallback/cache — acceptance (Issue #37)

The browser remains on same-origin `/api/charts/tiles/{z}/{x}/{y}.png`. Verify in Safari Network that **NOAA chart WMS does not appear as a browser request**. The Go handler now uses `SAILING_NAUTICAL_TILE_DIR` when set; otherwise uses `os.UserCacheDir()/sailing-go/nautical-tiles` (typically `~/Library/Caches/sailing-go/nautical-tiles` on the owner's Mac). No manual installer is needed: the directory is created on the first valid successful NOAA tile. With an empty cache and NOAA unavailable, each initial tile request can fail within the bounded upstream timeout and the single-panel error/Street fallback should appear. During a temporary 30-second cooldown, the server returns an uncached error for misses without new NOAA requests. A successful NOAA GetMap PNG is stored at XYZ `z/x/y.png` and subsequent Safari views should return it even when the NOAA WMS host is unavailable. Validate PNG visual content and chart coverage; an HTTP 200 or valid PNG can still be blank or geographically inappropriate. Never use the chart as sole navigation information. Local cache tiles are not auto-refreshed and therefore may become stale.

The CI Go tests use an `httptest` NOAA server, including WMS XYZ bbox, image validation, initial fetch and atomic disk save, offline cache reuse, errors not being cached and coalescing simultaneous identical requests. The browser Node regression still asserts no direct NOAA WMS URL and that the existing chart status and map preference staging remain. CI tests cannot confirm actual NOAA compatibility, uptime, coverage, on-disk permissions or Safari behavior; those remain owner Mac acceptance items. Running/restarting the owner's server, tunnel or changing runtime environment must be separately authorized. `./devbuild.sh` performs root Go test and build once, then `./sailing-go -version` checks the binary; do not run duplicate standalone builds.

### Owner Safari NOAA tile diagnostics — Issue #37

After an authorized launch of the updated binary, open Safari Web Inspector → Network, select Nautical at zoom 9+, and inspect an individual `/api/charts/tiles/{z}/{x}/{y}.png` response. The **Response Headers** should show `X-Sailing-Chart-Source: noaa` and `X-Sailing-Chart-Result: noaa-fetched` on a successful Go fetch, or `cache/cache-hit` when served from disk. An upstream failure may say `unavailable/noaa-timeout`, `unavailable/noaa-http-error` with `X-Sailing-Chart-Upstream-Status`, or `unavailable/cooldown`; cooldown means **Go did not call NOAA for that particular request**. In the owner Go server logs, a real attempt has `NAUTICAL tile=z/x/y upstream=noaa result=... status=... duration_ms=...`. No direct NOAA chart host should appear in Safari's requests. A plain HTTP 503 is not enough to tell which component failed, and no diagnostic header proves NOAA's real chart detail/coverage until the response image is visually checked. The existing loading/Close/Street fallback remains. Existing per-user settings, credentials and unrelated activity API 503s are separate and unchanged; owner Mac/Safari live test remains required.

## Issue #37 Stage A — offline chart-cache positioning fixture

For owner-controlled Safari testing, after the clean-tree checkout and ordinary `./devbuild.sh`, generate deliberately fake tiles into **one new directory outside Git and the real NOAA cache**. This command is a test fixture generator, **not** an installer or a downloader:

```sh
cd ~/projects/sailing-go
MOCK_DIR="$HOME/Downloads/sailing-nautical-mock-$(date +%Y%m%d-%H%M%S)"
go run ./cmd/mocknauticaltiles -dir "$MOCK_DIR"
```

The generator creates labeled **MOCK TEST DATA / NOT FOR NAVIGATION** PNGs at exact XYZ `z/x/y.png` coordinates around Pittsburg and the Delta (3×3 neighborhoods at zoom 10, 11, 12). It rejects existing destination directories. It does not touch NOAA, the service, authentication, account data, the default cache or any local browser settings. **Do not use `SAILING_NAUTICAL_TILE_DIR` in the production launcher to point at this directory.** A separate owner-authorized test instance using that variable is necessary for live Safari placement checks; do not start/restart a server or tunnel without explicit authorization. Fixture cache hits require no NOAA; missing fixture tiles may still trigger the existing bounded NOAA fallback, so use an isolated/mock upstream in automated tests. All tiles are fictional, never nautical chart evidence. Remove the directory manually when finished. Historical NCDS MBTiles evaluation is a separate Stage B.

CI's root Go test invokes this same Go generator in a disposable directory and verifies real `/api/charts/tiles/z/x/y.png` GET/HEAD cache-hit behavior, 256×256 fully decoded opaque PNGs, response bytes, and zero calls to a local upstream sentinel. It tests API plumbing, **not** real Safari appearance, NOAA coverage or navigation quality.

### Issue #37 Stage A — opt-in loopback Safari inspection

The normal public site keeps its NOAA failure-to-Street behavior. For the owner-controlled **separate loopback-only test server**, open the local test address with `?nautical_mock_test=1` (or append `&nautical_mock_test=1` to an existing query). Inspection mode is enabled only when **both** the explicit URL parameter equals `1` **and** Safari's hostname is `localhost` or `127.0.0.1`; adding that flag to `weather.themauris.org` does nothing. Use an otherwise isolated server instance and the already-generated `SAILING_NAUTICAL_TILE_DIR`, never the owner's production launcher/tunnel. The owner's authorization is required before launching any additional listener.

In loopback inspection mode, mock tile misses neither force Street selection nor start the 12-second NOAA response timeout/progress panel; valid XYZ tiles remain visible while missing tiles expose the Street underlay. The rest of the application and explicit Save/Discard pathways are unchanged. Zoom below 9 still uses the normal map minimum; test zoom 10–12. Successful nautical tiles no longer create the persistent generic NOAA advisory on normal or test hosts. A plain normal public Nautical view still shows Loading/Cancel and dismissible Failure/Close with automatic Street fallback on actual upstream failure.

The owner already demonstrated labeled mock XYZ tiles in Safari at zoom 10 while using the public Cloudflare site. That proves visible serving at this zoom, **not** intended isolated testing or complete zoom/corridor coverage. Before any further evaluation, restore public SailingGo to its normal cache (unset a test variable for any future startup, and arrange any necessary production restart separately under owner control; unsetting a shell variable alone does not change a running process). Test mocks only on loopback. Never claim NOAA online.

### Updated owner Safari mock acceptance — one status panel, zooms 9–14

The 45-tile z10–z12 directory created earlier remains untouched but does **not** cover the expanded test. After a clean checkout and `./devbuild.sh` once, generate a **fresh** directory:

```sh
cd ~/projects/sailing-go
MOCK_DIR="$HOME/Downloads/sailing-nautical-mock-$(date +%Y%m%d-%H%M%S)"
go run ./cmd/mocknauticaltiles -dir "$MOCK_DIR"
```

Use that new output **only** for a separately owner-approved loopback test server, with `SAILING_NAUTICAL_TILE_DIR` set on that test process, and open its actual local `/planning?nautical_mock_test=1` page in Safari. Do not point the owner production `s` launcher or Cloudflare Tunnel at test tiles; confirm public service no longer uses mock cache first. The loopback browser flag **must not** be expected to work at `weather.themauris.org`. No process/tunnel restarts are authorized by this document.

At z9–z14 near Pittsburg and Delta, select Nautical: one existing panel must display Loading and Cancel before mock tiles appear. Once tiles settle the panel must disappear and the labeled imagery must be visible in the correct places. Pan into an uncovered area: the Street underlay should be visible without a forced Street radio change; zooming should retain Nautical, apart from the preexisting below-z9 minimum. On normal non-mock pages, failed NOAA requests should reuse the **same** panel for error + Close and automatic dismissal (no second popup); the Street fallback should remain staged until Save Changes. Verify map click versus pan/zoom, overlays and both Guest/personal Save/Discard. CI mocks cannot establish Safari appearance. Historical 2024 NOAA MBTiles remain diagnostic-only and out of scope.

### Latest Safari regression acceptance — never force Street on missing chart tiles

In owner-authorized isolated Safari testing at z9–z14, deliberately pan or zoom into a place with missing mock tiles. When the chart request fails or times out, verify **the Nautical radio remains selected**, any available mock tile remains displayed, Street stays beneath missing cells, and the **same** progress status panel displays **“Nautical chart tiles are unavailable at this zoom. Try changing zoom levels or moving the map.”** with Close and automatic dismissal. No second popup, automatic Street radio change or account PUT is permitted. Zoom again; the prior error should clear and loading can succeed without manually reselecting Nautical. Explicit Cancel should still stage Street and respect Save/Discard. The user previously reported forced Street fallback when testing on the public Cloudflare site; owner must independently restore the normal public cache and must not use the public tunnel as a mock fixture server. Existing 45-tile directory is insufficient for z9–z14; generate a new bounded fixture directory using the prior documented generator command. This document does not authorize any process restart or tunnel change.

### Full supported zoom mock evaluation (z9–z18) — revised handoff

The current generator creates **954 test tiles** over all ten displayed Nautical zooms 9–18, rather than only z9–z14. Generate to a **fresh**, separate directory (it refuses an existing destination):

```sh
cd ~/projects/sailing-go
MOCK_DIR="$HOME/Downloads/sailing-nautical-mock-$(date +%Y%m%d-%H%M%S)"
go run ./cmd/mocknauticaltiles -dir "$MOCK_DIR"
```

Never replace/merge the prior 45-tile output directory or use `s`/Cloudflare to publish these artificial images. Use an **owner-authorized separate localhost test server** with `SAILING_NAUTICAL_TILE_DIR` set specifically for that process and Safari `?nautical_mock_test=1`. That URL enables native mock images at 17–18 while normal NOAA production continues native up to 16. No process restart or test-listener launch has been authorized by this code commit.

At zooms 9–18, check a Pittsburg/Delta position supported by the bounded fixture patches. Zoom repeatedly. The same status panel may indicate loading, but **existing image tiles must not disappear** because of progress. After peripheral or partially missing tile requests, valid imagery remains visible and the popup must **not falsely claim no tiles are available** for that zoom. On a genuinely empty zoom, a single error with Close suggests another zoom and keeps Nautical selected; Street remains underneath gaps. Check the owner-selected position (38.040,-121.887) at zooms 15–18. Outside tiny high-zoom patches, missing tiles are expected. Normal public site must use real chart cache and not mock fixtures. CI does not substitute for Mac/Safari visualization, which remains unverified.

### Acceptance: progress must not appear after cached Nautical tiles

After the approved local build and a separately owner-authorized local test listener, zoom between known fixture areas (z9–18). If the current tile batch completes within **200 ms**, **no progress popup may appear**; an old 200 ms callback must never resurrect it. A slower request may display the one existing panel only after the 200 ms threshold, and completion must close it without waiting for 2.5/12 seconds. Missing coverage may use the same panel for the zoom-advice error, without switching Nautical to Street. Repeated quick zooms must not leave obsolete loading notices. Keep the layer continuously visible. These behaviors have CI-only JS regression checks; Mac/Safari rendering still requires owner acceptance. Never use the public Cloudflare service as the fixture server.

### Safari acceptance — pan outside chart coverage at an unchanged zoom

With separately approved **local** mock fixtures (954 PNGs across display zooms 9–18), verify these cases without publishing mock files: (1) navigate to a covered Pittsburg location at z10 or z12; fast cached imagery displays without a late progress popup; (2) pan far away **without changing zoom**; when tile requests settle or no new request is necessary, the existing single panel explains “No nautical chart tiles are available in this area at the current zoom” and suggests panning back or changing zoom; (3) Nautical remains selected and Street remains under missing cells; (4) pan back to known fixtures; no stale warning remains; (5) pan partially outside the mock boundary, retaining a successful visible tile: no misleading total-outage popup. Repeat at z17/18 in opt-in localhost mock mode. Normal NOAA native ceiling remains z16. Browser regression tests use the real nautical event handlers with current-viewport projected tile coordinates and test same-zoom distant pans, partial coverage and failure; owner Mac/Safari remains an independent acceptance gate.

### Safari acceptance — unobtrusive Nautical status instead of progress popup

Verify that there is **no progress animation, loading bubble, or error panel drawn over the map**, even when cached mock tiles at zoom 10/11 arrive immediately while other chart requests remain pending. After the map footer and before Map Types/Overlays, the small Nautical status should read `Chart tiles visible` when at least one visible XYZ tile was loaded; it may say `Checking chart coverage…` when no tiles are yet known. Panning outside fixture coverage at a valid zoom changes only this status line to an area-specific no-coverage message; returning into coverage changes it back. Slow or timed-out chart requests leave the map interactive; the existing request safety bound updates text, never shows a popup. No Cancel, Close, or progress markup is allowed. The 954 distinct z9–18 fictional fixture tiles remain isolated to a separately approved loopback test process. CI uses the actual UI event handlers and asserts status placement, no over-map widget/progress nodes, coverage, timeout and stale callbacks. Mac/Safari visual verification remains owner-run and unverified until observed. Do not invoke the public `s` launcher with mock data.

### Safari visual check — Nautical status legibility

When the current viewport has no chart coverage, the small out-of-map Nautical status line below the map footer must use high-contrast pale amber `#ffe1a1` on the dark page (not the previous unreadable dark red `#9b1c1c`). Text has a .82rem base size and stronger unavailable-state font weight. Ensure the message is readable while panning at zoom 9 and the map remains unobstructed. Verify in owner Safari; CI enforces the CSS value but cannot establish visual acceptance.

## Nautical Tile Manager test and dry-run handoff

The CLI is exercised by CI with `go test -count=1 ./cmd/nauticaltilemanager` plus `go build ./cmd/nauticaltilemanager`; its test NOAA endpoint is a local `httptest` server. The regular `./devbuild.sh` remains the owner Mac root application test/build command. Planning is read-only and safe:

```sh
cd ~/projects/sailing-go
go run ./cmd/nauticaltilemanager -west -123 -south 36.5 -east -121.45 -north 38.45 -min-zoom 9 -max-zoom 12 -max-tiles 1000
```

Only an explicit subsequent owner action with `-download` and a **new distinct** `-dir` makes disk writes or live NOAA requests. An optional `-max-bytes` guards bytes. Resume is permitted only against the manager's matching marker/configuration, with individual existing PNG files validated and preserved. The CLI never runs implicitly during map navigation and does not touch local production services/tunnels. Do not run download mode from CI or as part of the Mac root build. To verify the command's own tests locally, `go test -count=1 ./cmd/nauticaltilemanager` is separate from `./devbuild.sh`, which tests only the root application package. Do not update or restart the production instance without owner authorization.
