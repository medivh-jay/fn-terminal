"""Rasterize the project's simple geometric terminal mark without external assets."""

import math
import pathlib
import struct
import zlib

root = pathlib.Path(__file__).resolve().parents[1]


def chunk(tag, data):
    return (
        struct.pack(">I", len(data))
        + tag
        + data
        + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
    )


def segment(x, y, a, b):
    dx, dy = b[0] - a[0], b[1] - a[1]
    t = max(0, min(1, ((x - a[0]) * dx + (y - a[1]) * dy) / (dx * dx + dy * dy)))
    return math.hypot(x - a[0] - t * dx, y - a[1] - t * dy)


def icon(size):
    rows = bytearray()
    for row in range(size):
        rows.append(0)
        for col in range(size):
            total = [0, 0, 0, 0]
            for sy in (0.25, 0.75):
                for sx in (0.25, 0.75):
                    x, y = (col + sx) * 64 / size, (row + sy) * 64 / size
                    inside = math.hypot(max(abs(x - 32) - 16, 0), max(abs(y - 32) - 16, 0)) <= 16
                    mark = (
                        min(
                            segment(x, y, (17, 21), (28, 32)),
                            segment(x, y, (28, 32), (17, 43)),
                            segment(x, y, (36, 43), (47, 43)),
                        )
                        < 2.4
                    )
                    color = (
                        (141, 229, 191, 255)
                        if inside and mark
                        else (23, 59, 53, 255) if inside else (0, 0, 0, 0)
                    )
                    for i, value in enumerate(color):
                        total[i] += value
            rows.extend(value // 4 for value in total)
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(rows, 9))
        + chunk(b"IEND", b"")
    )


for size in (64, 256):
    data = icon(size)
    (root / ("ICON.PNG" if size == 64 else "ICON_256.PNG")).write_bytes(data)
    (root / "app/ui/images" / f"icon_{size}.png").write_bytes(data)
print("Generated 64px and 256px terminal icons")
