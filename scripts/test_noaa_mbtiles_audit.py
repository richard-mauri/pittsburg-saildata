"""Offline fixture tests for the standalone NOAA MBTiles auditor."""
import sqlite3
import tempfile
import unittest
from pathlib import Path

from noaa_mbtiles_audit import audit, signature, tile_xyz

PNG = b"\x89PNG\r\n\x1a\n" + b"fixture"


class TestNOAAMBTilesAudit(unittest.TestCase):
    def test_xyz_tms(self):
        self.assertEqual(tile_xyz(0, 0, 1), (1, 1, 0))
        x, y, tms_y = tile_xyz(-121.885, 38.028, 9)
        self.assertEqual(tms_y, 511 - y)
        with self.assertRaises(ValueError):
            tile_xyz(-122, 100, 3)

    def test_signature(self):
        self.assertEqual(signature(PNG), "png")
        self.assertEqual(signature(b"\xff\xd8\xffsample"), "jpeg")
        self.assertEqual(signature(b"RIFF0000WEBP"), "webp")

    def test_synthetic_sqlite(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "sample.mbtiles"
            db = sqlite3.connect(path)
            db.executescript(
                "CREATE TABLE metadata (name TEXT, value TEXT);"
                "CREATE TABLE tiles (zoom_level INTEGER, tile_column INTEGER, tile_row INTEGER, tile_data BLOB);"
            )
            db.execute("INSERT INTO metadata VALUES (?,?)", ("format", "png"))
            x, _, y = tile_xyz(-121.885, 38.028, 9)
            db.execute("INSERT INTO tiles VALUES (?,?,?,?)", (9, x, y, PNG))
            db.commit()
            db.close()
            result = audit(path)
            self.assertEqual(result["integrity"], "ok")
            self.assertEqual(result["zoom_levels"], [{"z": 9, "count": 1}])
            self.assertEqual(result["site_samples"]["Pittsburg"]["9"]["format"], "png")
            self.assertFalse(result["site_samples"]["Golden Gate"]["9"]["present"])
            self.assertEqual(len(result["sha256"]), 64)


if __name__ == "__main__":
    unittest.main()
