"""Read and validate static fnOS manifests; never rewrite their contents."""

import pathlib
import re
import sys


def read_manifest(path):
    return parse_manifest(pathlib.Path(path).read_text(encoding="utf-8"))


def parse_manifest(text):
    values = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, separator, value = line.partition("=")
        key, value = key.strip(), value.strip()
        if not separator or not key or key in values or any(ord(char) < 32 for char in value):
            raise ValueError(f"Invalid or duplicate manifest field: {key}")
        values[key] = value
    for key in (
        "appname",
        "version",
        "display_name",
        "desc",
        "platform",
        "maintainer",
        "distributor",
    ):
        if not values.get(key):
            raise ValueError(f"Missing manifest field: {key}")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*", values["appname"]):
        raise ValueError("Invalid manifest appname")
    if not re.fullmatch(r"\d+(?:\.\d+){1,3}(?:[-+][A-Za-z0-9.-]+)?", values["version"]):
        raise ValueError("Invalid manifest version")
    if values["platform"] not in ("x86", "arm"):
        raise ValueError("Terminal packages require a specific x86 or arm platform")
    return values


def architecture_manifest(root, arch):
    root = pathlib.Path(root)
    primary = read_manifest(root / "manifest")
    if primary["platform"] != "x86":
        raise ValueError("Root manifest must describe the x86 package")
    if arch == "amd64":
        return root / "manifest", primary
    if arch != "arm64":
        raise ValueError("Unsupported architecture")
    alternate = read_manifest(root / "manifest.arm64")
    if alternate["platform"] != "arm":
        raise ValueError("ARM manifest must describe the arm package")
    if {k: v for k, v in alternate.items() if k != "platform"} != {
        k: v for k, v in primary.items() if k != "platform"
    }:
        raise ValueError("Static manifests differ in fields other than platform")
    return root / "manifest.arm64", alternate


if __name__ == "__main__":
    try:
        path, metadata = architecture_manifest(sys.argv[1], sys.argv[2])
        print(str(path) if sys.argv[3] == "path" else metadata[sys.argv[3]])
    except (ValueError, KeyError, IndexError) as error:
        raise SystemExit(str(error)) from error
