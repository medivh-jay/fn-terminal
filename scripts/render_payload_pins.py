"""Pin payload bytes inside trusted lifecycle scripts after assembling the app."""

import hashlib
import pathlib
import re
import sys

stage = pathlib.Path(sys.argv[1])
app = stage / "app"
pins = []
for path in sorted(app.rglob("*")):
    if path.is_symlink() or not (path.is_dir() or path.is_file()):
        raise SystemExit(f"Unsupported payload entry: {path}")
    if not path.is_file():
        continue
    relative = path.relative_to(app).as_posix()
    if not re.fullmatch(r"[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*", relative) or any(
        part in (".", "..") for part in pathlib.PurePosixPath(relative).parts
    ):
        raise SystemExit(f"Unsafe payload path: {relative}")
    data = path.read_bytes()
    if len(data) > 64 * 1024 * 1024:
        raise SystemExit(f"Payload file too large: {relative}")
    mode = "0755" if relative == "bin/fn-terminal" else "0644"
    pins.append(f"  '{hashlib.sha256(data).hexdigest()}:{mode}:{len(data)}:{relative}'")
if not pins or len(pins) > 128:
    raise SystemExit("Invalid payload file count")
begin, end = "# BEGIN PAYLOAD PINS\n", "# END PAYLOAD PINS\n"
block = "readonly -a FNTERMINAL_PAYLOAD_PINS=(\n" + "\n".join(pins) + "\n)\n"
for name in ("install_callback", "upgrade_callback"):
    path = stage / "cmd" / name
    text = path.read_text()
    before, rest = text.split(begin, 1)
    _, after = rest.split(end, 1)
    path.write_text(before + begin + block + end + after)
