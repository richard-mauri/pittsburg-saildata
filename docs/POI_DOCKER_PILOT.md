# Docker Northern California POI pilot

Owner NEXT approved Docker-only acquisition 2026-11-27: Docker 29.9.3 linux/aarch64, Go 1.27.3 Darwin arm64, 695 GiB available, no native Osmium/DuckDB. No Homebrew installs or operational data/runtime changes. Source selection remains open.

[Geofabrik Northern California](https://download.geofabrik.de/north-america/us/california/norcal/index.html) lists norcal-261126.osm.pbf and matching MD5 (~627 MB). Go poisample download defaults to dry run; -apply creates new Git-ignored private/poi-study-* directory, fixed HTTPS GETs, no redirects/proxy, known Content-Length, total response body <=1 GB, 15 minute context, publisher MD5 and SHA256 recorded. MD5 is integrity, not a signature. No latest fallback if the dated extract expires. Failed download cleans only its own new directory.

Docker Debian trixie-slim uses pinned precompiled osmium-tool 1.18.0-1; base/transitive packages not digest pinned. Record image ID and version. Registry/apt traffic separate from dataset budget. Offline extraction, read-only root, cap-drop, no-new-privileges, 2 GiB memory, 2 CPUs, 128 pids, 64 MiB tmpfs, only isolated study directory bind mount. File limit per output <=512 MiB (shell units), two samples and input nominally within 4 GiB; monitor actual disk. Extract deadlines total 900 s per sample. No production data or repo mounted.

Run from repository root after safe checkout, with set -e:
```bash
docker build -t sailinggo-poi-osmium:1 tools/poi-osmium
docker image inspect --format '{{.Id}} {{.Architecture}}' sailinggo-poi-osmium:1
go run ./cmd/poisample download
study="$(go run ./cmd/poisample download -apply)"
test -f "$study/source.json"
cat "$study/source.json"
for sample in delta sf; do
  docker run --rm --network none --read-only --cap-drop ALL \
    --security-opt no-new-privileges --memory 2g --cpus 2 \
    --pids-limit 128 --tmpfs /tmp:rw,noexec,nosuid,size=64m \
    --user "$(id -u):$(id -g)" \
    --mount "type=bind,source=$PWD/$study,target=/study" \
    sailinggo-poi-osmium:1 "$sample"
  test -f "$study/$sample/complete.txt"
  cat "$study/$sample/osmium-version.txt"
  /usr/bin/time -l go run ./cmd/poisample normalize -sample "$sample" \
    -input "$study/$sample/features.geojsonseq" -output "$study/$sample/normalized"
  /usr/bin/time -l go run ./cmd/poiprobe \
    -input "$study/$sample/normalized/normalized.csv" -sqlite
done
du -sh "$study"
```

Stop if failure, preserve failed extraction, no overwrite or automatic retry. If zero accepted rows, report normalizer and skip probe. Return source manifest, image ID/version, normalizer reports, probe output and disk/time; do not return raw private data. Time -l includes Go build overhead, Docker memory limit not peak RSS.

Osmium complete_ways extract, tags-filter with references, GeoJSON sequence export with @type/@id ([export docs](https://docs.osmcode.org/osmium/latest/osmium-export.html)); relations can be incomplete, invalid geometry omitted, ways may be both lines and polygons. Raw category counts are exported feature counts, not unique source object counts. Go bounded normalization (128 MiB, 2 MiB line, 100k features), exact interior sample point predicate, stable type/id, explicit invalid/closed/unnamed/duplicate/non-point and secondary membership counts, reject conflicting duplicates, deterministic single-category CSV for poiprobe. Cafe proxy for coffee; explicit marine tags, no inference by proximity. **Point-only pilot** excludes non-point marine/parking facilities and unnamed infrastructure; raw counts not coverage proof, no arbitrary centroid or asserted navigable entrance. Actual geometry resolution, lifecycle and cross-type dedup, state polygon and border tests remain required. No production waypoint/index built; ODbL attribution/redistribution decision pending. Overture comparison deferred. CI synthetic only; real source and ARM64 image validation owner-run.
