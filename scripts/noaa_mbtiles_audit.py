#!/usr/bin/env python3
"""Standalone read-only NOAA NCDS regional catalog and MBTiles auditor."""
import argparse
import hashlib
import json
import math
import sqlite3
import urllib.parse
import urllib.request
from pathlib import Path

LAYER = "https://encdirect.noaa.gov/arcgis/rest/services/MarineChart_Services/ncds_tilecache_metadata/MapServer/0/query"
SITES = {
    "Pittsburg": (-121.885, 38.028),
    "Delta": (-121.590, 38.095),
    "SF Bay": (-122.365, 37.800),
    "Golden Gate": (-122.478, 37.819),
}


def tile_xyz(lon, lat, zoom):
    if not (0 <= zoom <= 22 and -180 <= lon <= 180 and -85.05112878 <= lat <= 85.05112878):
        raise ValueError("out of range coordinate/zoom")
    scale = 1 << zoom
    x = min(scale - 1, int((lon + 180) / 360 * scale))
    y = min(scale - 1, int((1 - math.asinh(math.tan(math.radians(lat))) / math.pi) / 2 * scale))
    return x, y, scale - 1 - y


def signature(blob):
    if blob.startswith(b"\x89PNG\r\n\x1a\n"):
        return "png"
    if blob.startswith(b"\xff\xd8\xff"):
        return "jpeg"
    if blob.startswith(b"RIFF") and blob[8:12] == b"WEBP":
        return "webp"
    return "other/empty"


def query_noaa_site(lon, lat, timeout=20):
    params = {
        "geometry": json.dumps({"x": lon, "y": lat, "spatialReference": {"wkid": 4326}}),
        "geometryType": "esriGeometryPoint", "inSR": "4326",
        "spatialRel": "esriSpatialRelIntersects", "where": "1=1",
        "outFields": "name,date_creat,size,baseline,ext_min_x,ext_max_x,ext_min_y,ext_max_y",
        "returnGeometry": "false", "f": "json",
    }
    req = urllib.request.Request(LAYER + "?" + urllib.parse.urlencode(params),
                                 headers={"User-Agent": "SailingGo-NCDS-evaluation/1"})
    with urllib.request.urlopen(req, timeout=timeout) as response:
        payload = json.load(response)
    if "error" in payload:
        raise RuntimeError(f"NOAA ArcGIS error: {payload['error']}")
    return [feature["attributes"] for feature in payload.get("features", [])]


def catalog():
    return {name: query_noaa_site(lon, lat) for name, (lon, lat) in SITES.items()}


def audit(path):
    path = Path(path).resolve(strict=True)
    sha = hashlib.sha256()
    with path.open("rb") as fd:
        for chunk in iter(lambda: fd.read(1024 * 1024), b""):
            sha.update(chunk)
    uri = f'file:{urllib.parse.quote(str(path), safe="/")}?mode=ro'
    with sqlite3.connect(uri, uri=True) as db:
        integrity = db.execute("PRAGMA integrity_check").fetchone()[0]
        if integrity != "ok":
            raise RuntimeError(f"SQLite integrity check: {integrity}")
        metadata = dict(db.execute("SELECT name,value FROM metadata"))
        levels = [{"z": z, "count": n} for z, n in db.execute(
            "SELECT zoom_level,COUNT(*) FROM tiles GROUP BY zoom_level ORDER BY zoom_level")]
        if not levels:
            raise RuntimeError("no tile rows")
        samples = {}
        for name, (lon, lat) in SITES.items():
            samples[name] = {}
            for item in levels:
                z = item["z"]
                x, _, tms_y = tile_xyz(lon, lat, z)
                row = db.execute(
                    "SELECT tile_data FROM tiles WHERE zoom_level=? AND tile_column=? AND tile_row=? LIMIT 1",
                    (z, x, tms_y)).fetchone()
                blob = row[0] if row else b""
                samples[name][str(z)] = {
                    "present": row is not None, "format": signature(blob), "bytes": len(blob)}
    return {
        "file": path.name, "bytes": path.stat().st_size, "sha256": sha.hexdigest(),
        "integrity": integrity, "metadata": metadata,
        "zoom_levels": levels, "site_samples": samples,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--catalog", action="store_true", help="query NOAA polygon metadata at four locations")
    group.add_argument("--audit", metavar="FILE.mbtiles", help="inspect downloaded SQLite MBTiles")
    args = parser.parse_args()
    print(json.dumps(catalog() if args.catalog else audit(args.audit), indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
