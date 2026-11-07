"""Prepare the RF4 4.0.25034 shop compatibility patch without changing the installation.

The server marks timed discontinued tackle with catalog flag 0x80. Vanilla purchase
checks use bit 0, while the patched family model recognizes both bits for filtering.
"""

import argparse
import hashlib
import json
import struct
from pathlib import Path

ORIGINAL_SHA256 = "8a6d9e6c1375d03cbd215881bb2f1e92be3b4a8a34a990eb7c9f4a7379344971"
PATCHES = (
    # kalbiclomli.opblpacdbhb: retain the timed marker in the family status.
    (0x12F2E76, bytes.fromhex("80e101"), bytes.fromhex("80e181")),
    # memapjfggln.lfdebmfmeec: use available stock/price even for a timed family.
    (0x12F114E, bytes.fromhex("752a"), bytes.fromhex("9090")),
    # Family card: status 0x80 uses the existing out-of-stock message when price=0.
    # Signed status <= 0 covers 0 and 0x80; genuine discontinued status 1 stays blocked.
    (0x8BCD50, bytes.fromhex("74"), bytes.fromhex("7e")),
)


def file_offset(data, rva):
    pe = struct.unpack_from("<I", data, 0x3C)[0]
    if data[pe:pe + 4] != b"PE\0\0":
        raise ValueError("Not a PE image")
    count = struct.unpack_from("<H", data, pe + 6)[0]
    optional_size = struct.unpack_from("<H", data, pe + 20)[0]
    for index in range(count):
        section = pe + 24 + optional_size + 40 * index
        virtual_size, address, raw_size, raw_offset = struct.unpack_from("<IIII", data, section + 8)
        if address <= rva < address + min(virtual_size, raw_size):
            return raw_offset + rva - address
    raise ValueError(f"Unmapped RVA {rva:#x}")


def prepare(source, output):
    original = source.read_bytes()
    digest = hashlib.sha256(original).hexdigest()
    if digest != ORIGINAL_SHA256:
        raise ValueError(f"Unsupported GameAssembly.dll SHA256: {digest}")
    patched = bytearray(original)
    changes = []
    for rva, before, after in PATCHES:
        offset = file_offset(original, rva)
        if original[offset:offset + len(before)] != before:
            raise ValueError(f"Unexpected instructions at {rva:#x}")
        patched[offset:offset + len(before)] = after
        changes.append({"rva": hex(rva), "offset": offset, "before": before.hex(), "after": after.hex()})
    if output.exists():
        raise ValueError(f"Output already exists: {output}")
    output.write_bytes(patched)
    return {"source": str(source.resolve()), "output": str(output.resolve()),
            "originalSHA256": digest, "patchedSHA256": hashlib.sha256(patched).hexdigest(),
            "changedBytes": sum(a != b for a, b in zip(original, patched)), "patches": changes}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    print(json.dumps(prepare(args.source, args.output), indent=2))
