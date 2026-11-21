# NOAA NCDS MBTiles evaluation — Issue #37

**Status (2026-11-21): conditional / incomplete. Not approved for basemap integration.**  
**Source baseline:** SailingGo merged `main` commit `01b5fab6c3e2d8c67dbd07bd9419ab5c14c99c47` (merge of PR #36).  
**Scope:** NOAA chart tiles only; no auth, map UI, settings, deployment or production changes.

## Verified information versus blocked measurements

| Property | Evidence as of 2026-11-21 | Status |
| --- | --- | --- |
| Product | NOAA Chart Display Service (NCDS) MBTiles from NOAA ENC, **raster**, intended for chart-like basemaps | **Verified from NOAA official distribution** |
| Baseline refresh policy | NOAA says regional *baseline MBTiles packages are updated weekly*; this does not guarantee every downloaded file's ENC correction age | **Verified published policy** |
| Official landing page | https://distribution.charts.noaa.gov/ncds/ | **Verified** |
| Actual regional package(s) covering Pittsburg, Delta, SF Bay, Golden Gate | Region footprint must be selected on NOAA map and checked at all four sites; third-party 2023 catalog lists opaque IDs only and is not a geographic crosswalk | **Unverified; do not choose by ID guess** |
| Current download URL(s), byte count, ETag/Last-Modified | No NOAA binary could be reached from the evaluation runtime. NOAA page does not publish byte counts in accessible text | **Unverified** |
| Actual MBTiles bounds / tile occupancy at locations | Requires opening the downloaded SQLite file | **Unverified** |
| In-file zoom levels, tile dimensions, raster MIME PNG/JPEG/WebP | NCDS is raster; specific MBTiles metadata and tile signatures require inspection | **Unverified** |
| Actual NOAA dataset/package production date | NOAA weekly policy is not an individual file timestamp; inspect HTTP headers, MBTiles metadata, and NOAA index | **Unverified** |
| Appearance/detail parity with SailingGo live WMS | Requires side-by-side screenshots in Safari and source chart access; data may differ by ENC rendering/profile or vintage | **Unverified** |
| Reuse / distribution | NOAA ENC agreement permits reuse but redistributed ENCs lose official status absent certified distribution; NCDS **raster display product terms must be confirmed separately** before public serving | **Partially verified; public redistribution gate open** |

**No fabricated download size, geographic bounds, zoom range, MIME format or data date is supplied.** A third-party catalog (https://github.com/chartcatalogs/catalogs/blob/master/NOAA_MBTiles_Catalog.xml) enumerates NCDS region filenames and 2023 dates, but its timestamps are **not** November 2026 file dates and its IDs cannot establish California coverage.

## Acquisition and reproducible verification (non-production only)

1. At NOAA's official NCDS page, select the map polygon containing **Pittsburg, Sacramento–San Joaquin Delta, San Francisco Bay, and Golden Gate**. Record every intersecting region ID and its *current* official Baseline download URL. Do not infer that a single package spans all four locations. Check polygons at package seams.
2. On a machine with working NOAA access, for **each actual NOAA-selected URL** use the commands below; never write the package into the Git repository or serve it from the production listener.
3. Record the raw HTTP headers and SHA-256, compare file length with any advertised Content-Length, and inspect SQLite integrity/metadata and tile coverage. Repeat at a current-package renewal to verify how updates work.
4. Compare representative visual detail, using a disposable local viewer only, at Pittsburg waterfront, Antioch Bridge, Suisun Bay, Carquinez Strait, central SF Bay, and Golden Gate. Review soundings, channels, bridges, aids to navigation, text density, color, boundaries and zoom transitions. Compare against the existing Nautical WMS at same center/zoom, documenting any rendering differences. NOAA outage means visual equivalence cannot be signed off now.

Commands (replace the placeholder URL with the *verified NOAA Baseline link* and run **outside** the repository):

```sh
mkdir -p ~/Downloads/sailing-noaa-evaluation
cd ~/Downloads/sailing-noaa-evaluation
URL='PASTE_VERIFIED_NOAA_BASELINE_MBTILES_URL'
curl -fIL --max-time 45 "$URL" > noaa-headers.txt
curl -fL --retry 2 --continue-at - -o candidate.mbtiles "$URL"
wc -c candidate.mbtiles
shasum -a 256 candidate.mbtiles
file candidate.mbtiles
sqlite3 -readonly candidate.mbtiles 'PRAGMA integrity_check; SELECT name, value FROM metadata ORDER BY name; SELECT MIN(zoom_level),MAX(zoom_level),COUNT(*) FROM tiles; SELECT zoom_level,COUNT(*) FROM tiles GROUP BY zoom_level ORDER BY zoom_level;'
```

Use a disposable Python 3 stdlib inspection to check exact **individual** tile availability for the four sites and image signatures; metadata bounds alone is insufficient. MBTiles stores **TMS tile_row**, so for an XYZ request `y` at `z`, the SQLite row is `(1 << z) - 1 - y`.

```python
import math, sqlite3
db = sqlite3.connect("file:candidate.mbtiles?mode=ro", uri=True)
sites = {"Pittsburg": (38.028, -121.885), "Delta": (38.095, -121.590),
         "SF Bay": (37.800, -122.365), "Golden Gate": (37.819, -122.478)}
for z in (8, 9, 10, 11, 12, 13, 14, 15):
    print("zoom", z)
    for name, (lat, lon) in sites.items():
        lat = max(-85.05112878, min(85.05112878, lat))
        x = int((lon + 180) / 360 * (1 << z))
        y = int((1 - math.asinh(math.tan(math.radians(lat))) / math.pi) / 2 * (1 << z))
        tms_y = (1 << z) - 1 - y
        row = db.execute("SELECT tile_data FROM tiles WHERE zoom_level=? AND tile_column=? AND tile_row=?", (z,x,tms_y)).fetchone()
        blob = row[0] if row else b""
        sig = "PNG" if blob.startswith(b"\\x89PNG\\r\\n\\x1a\\n") else "JPEG" if blob.startswith(b"\\xff\\xd8") else "WEBP" if blob[0:4] == b"RIFF" and blob[8:12] == b"WEBP" else "none/other"
        print(f"  {name}: {'present' if row else 'absent'}, {len(blob)} bytes, {sig}")
db.close()
```

These sample probes are not a substitute for polygon coverage or tileset boundary seam inspection, and zoom 8–15 are **test probes, not a claim about the NOAA zoom range**. If NOAA uses `map`/`images` normalized schemas rather than a direct `tiles` table, first inspect `sqlite_master` and adapt the read-only query.

## Proposed Go/Leaflet integration after dataset acceptance

Keep chart databases at a **server-side durable path outside Git** (e.g., an operator-configured `SAILING_NOAA_MBTILES_PATH`), with `*.mbtiles` excluded from Git. Do **not** change deployment configuration or persistent storage without separate owner approval. Verify capacity, persistence, cold start and bandwidth on the *actual* host before choosing the storage path; local Mac disk is not automatically the deployed server's disk.

Implement a dedicated read-only SQLite tile store opened once per process with a maintained, pinned Go SQLite driver (prefer a no-CGO option only if compatible with the existing Go/CI environment). Keep it behind a separately wired same-origin endpoint such as `GET /charts/noaa/{z}/{x}/{y}`, using the existing suite authorization boundary when integration is approved; do **not** introduce an unauthenticated bypass. Validate integer ranges, per-zoom tile dimensions and max request size, translate **XYZ y to TMS tile_row**, query with parameters, sniff actual tile content, and send correct `image/png`, `image/jpeg` or `image/webp` with cache headers. Return 404 for missing coordinates/coverage, 503 for unavailable chart store, and never silently claim stale/missing charts are current. Avoid arbitrary filesystem paths from client input. Test concurrent reads, invalid coordinates, out-of-bounds tiles, corruption, missing file, and source swap.

In Leaflet, replace only the **Nautical basemap layer**, not Leaflet itself or overlays; use `L.tileLayer('/charts/noaa/{z}/{x}/{y}', {tms:false, ...})` (server already performs inversion). Preserve the map controls, selected sailing location versus viewport, markers, NOAA wind and predicted currents, weather layers, and current personal/Guest Save/Discard semantics. Avoid source-selector UI in Phase 1 or 2. Ensure chart misses are visible rather than allowing a misleading blank chart. Choose a tested low-zoom fallback or explicit no-chart message without implicit live NOAA dependence.

Provide a small `/charts/noaa/status` metadata response only when authorized and safe: source, package identifier, SHA-256, verified download/production date (distinguish them), source update policy, actual min/max zoom and availability. Do not label the server's deployment date as the ENC issue date.

For periodic refresh: download into a **temporary file outside the repository**, use size/hash checks, SQLite integrity and sample-location tile tests, retain the previous known-good database, and only then atomically promote a complete verified file on the **same filesystem**. Prefer explicit owner/operator-triggered updates initially; automatic schedules, deployment or service restarts need separate approval. If connections hold open file handles, implement safe store reopening/handle swap without moving or deleting still-open source files.

### Important chart legal/safety distinction

NOAA states the ENC viewer/Chart Display Service images are **not suitable for navigation** (https://nauticalcharts.noaa.gov/learn/faq.html). The ENC agreement (https://charts.noaa.gov/ENCs/Agreement.shtml) allows download/reuse of ENCs but distinguishes unofficial redistribution. Do **not** assume this agreement settles NCDS tile-hosting terms; verify applicable NOAA NCDS-specific notices/attribution before public deployment. SailingGo must clearly attribute NOAA and retain its non-navigation/planning-only warning.

## Acceptance gates / handoff

**Gate A (blocked):** Identify exact California region(s), verify current exact byte count, timestamps, SQLite zoom / raster signatures, and tile coverage at all target sites.  
**Gate B (blocked):** Safari visual comparison and server-disk feasibility.  
**Gate C (blocked):** NCDS redistribution/attribution review.  
**Gate D (future):** Standalone Go/SQLite local tile-server prototype, tests and measured latency. No production route or map integration until accepted.

Current change is **documentation-only**, without a Go prototype, binary acquisition, source selection UI, running process or listener. Await the owner's `NEXT` and actual NOAA dataset access before proceeding.

## References

- NOAA NCDS official download and weekly baseline notice: https://distribution.charts.noaa.gov/ncds/
- NOAA ENC / GIS services and offline tiles: https://nauticalcharts.noaa.gov/data/gis-data-and-services.html
- NOAA Chart Display and chart imagery safety: https://nauticalcharts.noaa.gov/learn/faq.html
- NOAA ENC user agreement (not proof of raster hosting rights): https://charts.noaa.gov/ENCs/Agreement.shtml
- MBTiles technical reference: https://gdal.org/en/stable/drivers/raster/mbtiles.html

## NEXT 2026-11-21 — authoritative NOAA regional metadata discovery

NOAA also publishes an authoritative ArcGIS **region polygon layer** at:

https://encdirect.noaa.gov/arcgis/rest/services/MarineChart_Services/ncds_tilecache_metadata/MapServer/0

The verified layer schema provides `name` (region), `date_creat` (creation string), `size` (published **MB**, **not exact byte count**), `baseline` (package locator), and bounding-box coordinate fields `ext_min_x`, `ext_min_y`, `ext_max_x`, `ext_max_y`. **These are available published fields, not yet retrieved live values.** NOAA polygon intersection queries in this evaluation environment were blocked by network/DNS restrictions; the layer's metadata was reachable through a browser index, but no four-site coverage results could be collected. Do not treat a bounding box or presence of a region index as confirmation that raster tiles exist at a requested zoom.

A new standalone, read-only standard-library tool, [scripts/noaa_mbtiles_audit.py](../scripts/noaa_mbtiles_audit.py), makes this check repeatable. It uses `esriSpatialRelIntersects` point queries to discover matching NOAA **regions for each of four representative locations**, then reports the exact attributes returned by NOAA:

```sh
cd ~/projects/sailing-go
python3 scripts/noaa_mbtiles_audit.py --catalog
```

Run the catalog query only when NOAA ArcGIS connectivity is available; it requires no credentials, and its output is evidence about **catalog region intersections**, not downloaded chart detail. After downloading a NOAA-selected regional file outside the repository:

```sh
python3 scripts/noaa_mbtiles_audit.py --audit ~/Downloads/sailing-noaa-evaluation/candidate.mbtiles
```

The local audit reads SQLite **read-only**, verifies `PRAGMA integrity_check`, hashes the file with SHA-256, reports actual on-disk byte count and per-level tile counts, and probes the same four locations at **every actual level** after translating XYZ to TMS. It detects PNG/JPEG/WebP binary signatures. It works on a conventional MBTiles `tiles` relation (physical table or view), and will fail explicitly on unsupported schemas. A present tile at a single center point does not prove all surrounding waterways are covered. No MBTiles package or artifact is committed.

Offline synthetic fixtures exercise TMS inversion, raster signature, SQLite integrity and negative coverage. Tests: `python3 -m unittest discover -s scripts -p 'test_noaa_mbtiles_audit.py' -v`. These tests **do not validate NOAA's live contents** or a Go tile server. CI runs the fixture test independently of the Go build.

**Implementation hold:** The standalone inspector is not the Go tile-server prototype. Choosing/pinning a Go SQLite backend, adding an actual chart endpoint, chart source selection, exposing server storage, or changing production settings requires subsequent approved steps after the exact NOAA dataset and hosting constraints are verified. This iteration makes no change to SailingGo's runtime, map, authentication, user accounts or preferences.
