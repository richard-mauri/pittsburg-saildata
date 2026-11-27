# POI Phase 2 — isolated sample and index evaluation

Issue #51 / draft PR #52. No production acquisition, installation, launch or dependency changes. Source selection remains blocked pending actual samples. Remote Go 1.23.2 Linux amd64 shell cannot resolve Geofabrik or Overture S3/Azure endpoints; no sqlite3, duckdb or osmium CLI on PATH. Web metadata is available, but regional Taginfo stats pages/API are inaccessible. Rechecked 2026-11-27. Do not treat publisher sizes as measured download bytes or global counts as California counts.

## Source inventory and historic evidence

[California Geofabrik](https://download.geofabrik.de/north-america/us/california/index.html) lists 1.2 GB whole state and 627 MB Northern / 643 MB Southern California PBF; pinned California 261126 and MD5 are listed. The smallest two California subregions still exceed the previous 100 MB pilot network cap. No California download approved merely by this procedure. [Overture DuckDB guide](https://docs.overturemaps.org/getting-data/duckdb/) documents 2026-11-18.0 public Parquet and taxonomy.primary. [Overture data repo](https://github.com/OvertureMaps/data) describes bbox and WKB geometry; [taxonomy explorer](https://docs.overturemaps.org/guides/places/taxonomy-explorer/) must be reconciled with release. Do not perform an unbounded wildcard cloud scan; DuckDB LIMIT and memory_limit do not enforce HTTP-byte limits. Before acquisition choose a verified local/offline tool or a bounded network environment, exact release/file inventory, elapsed time and byte/disk budget. No dependency installation authorized here.

Read-only GitHub inspection of existing tracked assets/marine_places.json v14 (updated 2026-09-25; regional Bodega Bay–Monterey/Bay–Delta), total 774: marinas 234 (40 OpenStreetMap / 194 California State Parks DBW), launch ramps 151 (13 OSM / 137 DBW / 1 override), fuel docks 80 (3 OSM / 77 DBW). Source attribution is as stored, not a completeness measure; dedup and curated preferences bias counts. No pumpout category in this asset. This suggests marine-source coverage deserves careful testing; it is not a contemporary OSM-vs-Overture comparison and does not authorize use or redistribution of protected marine asset. Do not regenerate it.

## Go index probe

cmd/poiprobe is a disposable diagnostic, not the manager, schema validator or runtime index. It creates new temporary files only, then removes its own directory; no network, no persistent cache, no third-party Go dependency. The grid stores 24-byte coordinate/category/ordinal records in a contiguous file grouped by 0.05 degree cell, holds cell offsets in memory, reads matching cells with ReadAt and applies exact inclusive predicates. SQLite comparator uses installed sqlite3 CLI only, numeric point/category values, R-Tree overlap + exact float64 filters, and ordered IDs. Both compare full result sets to brute force. No production waypoint payload or index metadata serialization; SQLite file includes coordinate table/R-Tree whereas grid size excludes memory-only directory, so not equivalent production disk sizes. SQLite batch elapsed includes CLI startup/serialization; grid reports query latency and concurrency 1/4. Neither is a true cold-cache benchmark. Synthetic distribution deliberately clusters points around five fixed sample boxes plus scattered synthetic coordinates; no claim of actual density, state boundary or business counts. CI uses synthetic data only.

Owner preflight (run after clean checkout; do not install anything):
```sh
go version
command -v sqlite3 || true
command -v duckdb || true
command -v osmium || true
df -h .
```
Run independent probe (no service build or restart):
```sh
go test -count=1 -v ./cmd/poiprobe
if command -v sqlite3 >/dev/null 2>&1; then
  /usr/bin/time -l go run ./cmd/poiprobe -synthetic 100000 -sqlite
else
  /usr/bin/time -l go run ./cmd/poiprobe -synthetic 100000
fi
```
Mac time includes compiler/process overhead; to isolate probe RSS use go build -o into a newly created private temporary directory, then time binary once per mode. Report hardware, versions, stdout and time -l max RSS, no credentials. CI results not Mac results. No live HTTP calls.

## Real sample gate and return format

Use sample boxes defined in [design](CALIFORNIA_POI_DESIGN.md), independent same-release samples for each source. Start with a single sample (SF downtown or Pittsburg/Delta), not statewide acquisition. For Overture plan a release-pinned object inventory and bbox pushdown, use point geometry and explicit release taxonomy mapping, preserve license/source fields, filter closed/invalid, exact CA polygon. For OSM use pinned regional PBF (or smaller separately verified licensed extract), checksum, osmium tags-filter with referenced objects, extract by sample bounds, resolve node/way/relation geometry and preserve source type/id and tags. Osmium export may produce polygons; do not substitute arbitrary bounding-box centroids or treat location as safe entry. Measure raw matches and reasons for exclusion, dedup, category counts; no guessed mapping or fabricated names. Census FIPS 06 polygon and edition/hash required for border comparison. Keep sources separate and assess ODbL before public use.

Owner decision needed before any missing tool install and before exceeding 100 MB per small sample. If only the full PBF is available, propose explicit e.g. 1.5 GB network cap, 4 GiB free isolated disk minimum and 15 minute cap (review temporary space for chosen filter/export); not permission to download. Overture ranged requests need an independently enforced egress cap, not just SQL LIMIT. Keep raw inputs/outputs in a new Git-ignored private/poi-study-<date> or cache/poi/staging directory; no existing operational caches touched. Record actual downloaded bytes, time, release and SHA256, extraction peak RSS, raw/normalized/rejected/category counts, and inspect sample false positives. No live Overpass or public API bulk requests.

Once normalized independently, export a CSV header exactly id,lat,lon,category,name with unique IDs and one category per row for this diagnostic (separate multiple-category records for explicit membership study; do not silently duplicate IDs). Max 100k rows, 64 MiB; CSV names and IDs not written to probe output. Validation is deliberately limited to coordinate/category/ID/count/input-size checks, not production Unicode/waypoint/state validity. Use one input per source/region and report counts. Run:
```sh
/usr/bin/time -l go run ./cmd/poiprobe -input private/poi-study-YYYYMMDD/normalized.csv -sqlite
```
Omit -sqlite if unavailable; report as not measured. Use actual path, no placeholder execution. Record measurements in PR discussion, then choose source and index only after coverage, licenses, boundary quality, build/index size and latency are compared. Real source normalization, acquisition adapter, persisted index format and production API remain separate approved work.
