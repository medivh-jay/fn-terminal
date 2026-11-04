"""Check the actual package's structure, architecture, defaults and script syntax."""

import json
import pathlib
import struct
import subprocess
import sys

from manifest import read_manifest

p = pathlib.Path(sys.argv[1])
for name in [
    "manifest",
    "config/privilege",
    "config/resource",
    "wizard/install",
    "wizard/config",
    "ICON.PNG",
    "ICON_256.PNG",
    "app/bin/fn-terminal",
    "app/ui/config",
]:
    if not (p / name).is_file():
        raise SystemExit(f"Missing package file: {name}")
for hook in [
    "main",
    "install_init",
    "install_callback",
    "upgrade_init",
    "upgrade_callback",
    "uninstall_init",
    "uninstall_callback",
    "config_init",
    "config_callback",
]:
    if not (p / "cmd" / hook).is_file():
        raise SystemExit(f"Missing lifecycle hook: cmd/{hook}")
manifest = read_manifest(p / "manifest")
binary = (p / "app/bin/fn-terminal").read_bytes()
if binary[:4] != b"\x7fELF" or binary[4:6] != b"\x02\x01":
    raise SystemExit("Expected a 64-bit little-endian Linux ELF binary")
machine = struct.unpack_from("<H", binary, 18)[0]
if machine != {"x86": 62, "arm": 183}.get(manifest.get("platform")):
    raise SystemExit("Manifest architecture does not match executable")
for name in [
    "config/privilege",
    "config/resource",
    "wizard/install",
    "wizard/config",
    "app/ui/config",
    "app/config.example.json",
]:
    json.loads((p / name).read_text())
if json.loads((p / "app/config.example.json").read_text())["enabled"]:
    raise SystemExit("Terminal must be disabled before administrator opt-in")
entry = json.loads((p / "app/ui/config").read_text())[".url"]["fnterminal.main"]
if entry["gatewayPrefix"] != "/app/fnterminal" or entry["gatewaySocket"] != "app.sock":
    raise SystemExit("Gateway routing does not match the application")
for script in (p / "cmd").iterdir():
    if script.is_file():
        subprocess.run(["bash", "-n", str(script)], check=True)
for name, size in [("ICON.PNG", 64), ("ICON_256.PNG", 256)]:
    png = (p / name).read_bytes()
    if png[:8] != b"\x89PNG\r\n\x1a\n" or struct.unpack_from(">II", png, 16) != (size, size):
        raise SystemExit(f"Invalid PNG icon: {name}")
print(
    f"Package checks passed: {manifest['appname']} / {manifest['platform']} / {manifest['version']}"
)
