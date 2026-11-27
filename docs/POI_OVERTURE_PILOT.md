# Overture full-object terrestrial pilot — Issue #51

Owner NEXT authorizes one dated object, 836166912 bytes expected, body cap 1000000000, 15-minute acquisition deadline, up to 4 GiB isolated study working allowance. Prior catalog inventory (51,685 metadata bytes) in POI_SOURCE_SPLIT.md. No live API, no production install or source selection. Preserve previous OSM and marine study. No native installation.

## Procedure (owner Mac, each command separately)

From verified feature branch and repository root, inspect free disk (at least 4 GiB; no automated filesystem quota), then pull the official version-pinned CLI image. ARM64 and AMD64 published; version tag not digest pinned across hosts, local image ID captured and used for extraction. No Docker build or spatial extension download.

```sh
df -h .
docker pull duckdb/duckdb:1.4.2
docker image inspect --format '{{.Id}} {{.Architecture}}' duckdb/duckdb:1.4.2
go run ./cmd/poioverturesample download
go run ./cmd/poioverturesample download -apply
go run ./cmd/poioverturesample sample -sample delta
go run ./cmd/poiprobe -input private/poi-overture-2026-11-18.0/delta/normalized.csv -sqlite
go run ./cmd/poioverturesample sample -sample sf
go run ./cmd/poiprobe -input private/poi-overture-2026-11-18.0/sf/normalized.csv -sqlite
du -sh private/poi-overture-2026-11-18.0
```

Stop on failure, do not repeat completed download or overwrite existing study/sample. Preserve partial outputs for diagnosis. Only run probe if Rows >0. Return aggregate reports, manifest, image ID and disk usage, not raw features. Download progress printed to stderr, manifest to stdout. Source HTTPS fixed URL no proxy/redirect, expected Content-Length if present or exact streamed length plus EOF, 1 GB body cap, SHA256 and Parquet magic verified, ETag recorded as object metadata not a publisher checksum; no independently published checksum confirmed. Fresh private/poi-overture-2026-11-18.0 only; failure preserves partial file. Rehash before each extraction. Docker image pulled separately; image transfer not included in data body cap. Docker --pull=never, network none, read-only container and only one source-file mount, uid/gid, 4 GiB cgroup, 2 CPUs, 128 PIDs, 1 GiB /tmp tmpfs, DuckDB 2 GB memory and 512 MB temp allowance. Context deadline 15 minutes per extraction, on failure remove only uniquely named study container. Raw CSV cap 64 MiB, row cap 100k, normalized cap 64 MiB. Host output files limited by these budgets; no absolute disk quota or peak RSS measured.

## Normalization

Schema from https://docs.overturemaps.org/schema/reference/places/place/ and https://docs.overturemaps.org/guides/places/ (November 18 release). Select only id, primary name, basic category, primary taxonomy and hierarchy, operating status, confidence, exact WKB and source metadata. Bbox overlap pruning accommodates outward rounded bbox; missing bbox excluded. No spatial extension: disable GeoParquet conversion and decode only ordinary 2-D WKB Point in Go, exact inclusive sample bounds. Raw candidate count includes overlaps outside exact bounds; reject invalid/unnamed/closed/zero-confidence and conflicting duplicates; no inference from names or proximity. Single primary category for comparable benchmark; conservative provisional exact hierarchy/basic/primary token mapping, ignore alternates. Cafe proxy for coffee, gas_station/fuel_station, grocery_store/supermarket, lodging/hotel/motel/hostel/guest_house, parking/parking_lot/parking_garage, EV charging explicit candidates and restaurant ancestry. Exact token mapping not yet validated against full November taxonomy; inspect reported basic and primary distributions before final source decision. Unknown status value rejected, missing status permitted, nonzero confidence not a guarantee. Sources/licenses aggregated for accepted rows (multiple possible per feature), raw source metadata retained only locally. No assumed source license clearance. Names and stable source IDs retained; no public waypoint or runtime dataset created. Compare counts with OSM point-only pilot, not unique statewide physical coverage; no border/clipping or cold-cache/RSS benchmark. Existing probe includes queries outside sample; not a production latency measurement.

CI Go unit tests + synthetic local Parquet DuckDB fixture using same extraction function. No public Overture transfer in CI. Real GeoParquet metadata, owner ARM64 behavior and exact release taxonomy require owner measurement. Official image https://hub.docker.com/r/duckdb/duckdb/tags and CLI https://duckdb.org/docs/current/operations_manual/duckdb_docker .

## Owner acquisition and nullable CSV correction — 2026-11-27

Download verified exact 836166912 bytes in 11.059865958 seconds, SHA256 be8157302e8d94b648f884ae65b10f6073960b439dd0aceda2e145c75bfe5441, ETag "4deda9314cbe9252124f6c0967072f1a-13", last modified Wed, 18 Nov 2026 17:01:59 GMT, downloaded 2026-11-27T22:04:46.895873Z. ARM64 local DuckDB image sha256:3e2cca396e228ba2a458a21919a83948eef637e843c35cb0cf51606465d28402. Delta extraction finished; Go failed on hierarchy JSON. Owner read-only diagnostic found 56 literal uppercase NULL hierarchy values, not valid JSON. Normalize SQL NULL sentinels across nullable columns (including status, confidence, source metadata); missing names and IDs still rejected, malformed non-null JSON still errors. Added regression fixture. For this owner-confirmed completed CSV only, run `go run ./cmd/poioverturesample normalize -sample delta` after guarded pull. Rehashes source, reads bounded regular existing CSV, refuses existing normalized/report files, no download or Docker. Legacy CSV has no extraction-complete marker, so resume relies on owner evidence; original extraction timing not captured, no independent completeness assertion. Then probe if nonempty, proceed to SF using normal sample command. No production changes.
