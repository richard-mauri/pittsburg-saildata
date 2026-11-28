#!/bin/sh
set -eu
ulimit -f 524288
case "$#:$*" in
  1:delta) bbox="-121.94,38.00,-121.84,38.06"; sample=delta ;;
  1:sf) bbox="-122.43,37.77,-122.39,37.81"; sample=sf ;;
  *) echo "usage: poi-extract delta|sf" >&2; exit 2 ;;
esac
cd /study
test -f norcal-261126.osm.pbf
test -f source.json
mkdir "$sample"
cd "$sample"
osmium --version > osmium-version.txt
timeout 600 osmium extract --strategy=complete_ways --bbox="$bbox" ../norcal-261126.osm.pbf -o region.osm.pbf
timeout 180 osmium tags-filter region.osm.pbf amenity=fuel,restaurant,cafe,charging_station,parking shop=supermarket,grocery tourism=hotel,motel,guest_house,hostel leisure=marina,slipway waterway=fuel,sanitary_dump_station seamark:type=fuel_station,sanitary_dump_station -o filtered.osm.pbf
timeout 120 osmium export -a type,id -f geojsonseq filtered.osm.pbf -o features.geojsonseq
printf '%s\n' "$bbox" > complete.txt
