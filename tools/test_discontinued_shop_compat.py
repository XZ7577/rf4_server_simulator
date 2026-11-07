"""Regression checks for the shipped catalog and local client compatibility patch."""

import hashlib
import json
import lzma
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def unpack_catalog(blob):
    prop = blob[0]
    filters = [{"id": lzma.FILTER_LZMA1, "lc": prop % 9,
                "lp": prop // 9 % 5, "pb": prop // 45,
                "dict_size": int.from_bytes(blob[1:5], "little")}]
    size = int.from_bytes(blob[5:13], "little")
    return lzma.LZMADecompressor(format=lzma.FORMAT_RAW, filters=filters).decompress(blob[13:], max_length=size)


def catalog_status_offsets(raw):
    assert raw[:5] == b"\x03\x03139"
    count = int.from_bytes(raw[5:7], "little")
    pos = 7
    rows = {}
    for _ in range(count):
        assert raw[pos:pos + 5] == b"\x01\xf4\x01\0\0"
        pos += 5
        size = raw[pos]
        pos += 1
        item_id = raw[pos:pos + size].decode("utf-8")
        pos += size
        rows[item_id] = pos
        pos += 11  # BYTE status, U16 channels, U64 price
        for _ in range(2):
            marker = raw[pos]
            pos += 1
            if marker == 0:
                continue
            assert marker == 4
            elements = int.from_bytes(raw[pos:pos + 2], "little")
            pos += 2
            for _ in range(elements):
                length = int.from_bytes(raw[pos:pos + 4], "little")
                pos += 4
                if length != 0x7FFFFFFF:
                    pos += length
    assert pos == len(raw)
    return rows


class DiscontinuedCatalogCompatibility(unittest.TestCase):
    def test_timed_tackle_remains_filterable_without_vanilla_purchase_block(self):
        asset = "internal/gameserver/assets/config_wire/type_2.bin"
        blob = (ROOT / asset).read_bytes()
        current = unpack_catalog(blob)
        restored = bytearray(current)
        offsets = catalog_status_offsets(current)
        timed = set(json.loads((ROOT / "internal/store/assets/discontinued_shop_items.json").read_text()))
        self.assertEqual(len(timed), 192)
        self.assertEqual(len(offsets), 12611)
        for item_id, offset in offsets.items():
            if item_id in timed:
                self.assertEqual(current[offset], 128, item_id)
                self.assertTrue(current[offset] & 0x81, item_id)  # patched family filter
                self.assertFalse(current[offset] & 1, item_id)  # vanilla buy checks
                restored[offset] = 1
            else:
                self.assertFalse(current[offset] & 128, item_id)
        # Restoring only those status bytes must reproduce the original shipped catalog.
        self.assertEqual(hashlib.sha256(restored).hexdigest(),
                         "7a114b842842ad4dd9ea27d63c8e6ee3c50ddefeb1753e1e81eaca4488fcdde7")
        index = json.loads((ROOT / "internal/gameserver/assets/config_wire/index.json").read_text())
        self.assertEqual(hashlib.sha1(blob).hexdigest(), index["versions"][1])
        self.assertEqual(index["types"]["2"]["sha1"], index["versions"][1])
        self.assertEqual(index["types"]["2"]["bytes"], len(blob))


if __name__ == "__main__":
    unittest.main()
