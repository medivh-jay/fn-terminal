"""Embed a standalone preflight; no unvalidated helper is sourced during root startup."""

from pathlib import Path

root = Path(__file__).resolve().parents[1]
guard = (root / "scripts/lifecycle_guard.sh").read_text() + (
    root / "scripts/payload_bootstrap.sh"
).read_text()
begin, end = "# BEGIN ROOT TRUST PREFLIGHT\n", "# END ROOT TRUST PREFLIGHT\n"
for name in [
    "main",
    "install_callback",
    "config_callback",
    "upgrade_callback",
    "upgrade_init",
    "uninstall_init",
    "uninstall_callback",
]:
    path = root / "cmd" / name
    text = path.read_text()
    if begin in text:
        before, rest = text.split(begin, 1)
        _, after = rest.split(end, 1)
        text = before + after
    marker = "export PATH=/usr/sbin:/usr/bin:/sbin:/bin\n"
    if marker not in text:
        raise SystemExit(f"Missing fixed PATH in {name}")
    path.write_text(text.replace(marker, marker + begin + guard + end, 1))
