"""Build flat FnDepot or V2 source indexes from actual Release assets."""

import argparse
import hashlib
import json
import pathlib
import re
import tarfile
import urllib.parse

from manifest import parse_manifest, read_manifest

ROOT = pathlib.Path(__file__).resolve().parents[1]


def build_source(repository, tag, paths, source_format="v1"):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("Expected a GitHub owner/repository")
    metadata = read_manifest(ROOT / "manifest")
    if tag != "v" + metadata["version"]:
        raise ValueError("Release tag must match the static manifest version")
    homepage = f"https://github.com/{repository}"
    packages = {}
    for path in paths:
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*\.fpk", path.name):
            raise ValueError("Unsafe Release asset name")
        with tarfile.open(path, "r:gz") as archive:
            stream = archive.extractfile("manifest")
            if stream is None:
                raise ValueError("Package has no manifest")
            raw = stream.read(65537)
            if len(raw) > 65536:
                raise ValueError("Package manifest is too large")
            actual = parse_manifest(raw.decode("utf-8"))
        if (actual["appname"], actual["version"]) != (metadata["appname"], metadata["version"]):
            raise ValueError("Package appname/version differs from the static manifest")
        platform = actual["platform"]
        if platform in packages:
            raise ValueError("Duplicate package platform")
        packages[platform] = {
            "download_url": f"{homepage}/releases/download/{urllib.parse.quote(tag, safe='')}/{path.name}",
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
            "size": path.stat().st_size,
        }
    if not packages:
        raise ValueError("At least one built FPK is required")
    if source_format == "v1":
        arch_diff = {
            arch: {
                "download_url": package["download_url"],
                "size": f"{package['size'] / 1024:.1f}KB",
                "size_bytes": package["size"],
                "sha256": package["sha256"],
            }
            for arch, package in packages.items()
        }
        primary = arch_diff.get("x86") or next(iter(arch_diff.values()))
        return {
            metadata["appname"]: {
                "display_name": metadata["display_name"],
                "platform": sorted(packages),
                "desc": metadata["desc"],
                # 应用坏心's V1 reader stringifies labels, then splits on commas.
                "labels": "系统工具,终端",
                "version": metadata["version"],
                "author": metadata["maintainer"],
                "author_url": metadata.get("maintainer_url") or homepage,
                "bug_report_url": f"{homepage}/issues",
                "install_type": "系统空间",
                "icon": f"https://raw.githubusercontent.com/{repository}/{urllib.parse.quote(tag, safe='')}/ICON_256.PNG",
                "size": primary["size"],
                "arch_diff": arch_diff,
            }
        }
    if source_format != "v2":
        raise ValueError("Unsupported source format")
    app = {
        "display_name": metadata["display_name"],
        "desc": metadata["desc"],
        "maintainer": metadata["maintainer"],
        "platform": sorted(packages),
        "categories": ["系统工具"],
        "icon_url": f"https://raw.githubusercontent.com/{repository}/{urllib.parse.quote(tag, safe='')}/ICON_256.PNG",
        "releases": {metadata["version"]: {"packages": packages}},
    }
    if metadata.get("os_min_version"):
        app["os_min_version"] = metadata["os_min_version"]
    return {
        "schema_version": "2",
        "source_info": {
            "name": metadata["display_name"],
            "author": metadata["maintainer"],
            "homepage": homepage,
        },
        "apps": {metadata["appname"]: app},
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--format", choices=("v1", "v2"), default="v1")
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("packages", nargs="+", type=pathlib.Path)
    args = parser.parse_args()
    try:
        index = build_source(args.repository, args.tag, args.packages, args.format)
    except (ValueError, KeyError, OSError, tarfile.TarError) as error:
        parser.exit(1, f"{error}\n")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(index, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Source index: {args.output}")


if __name__ == "__main__":
    main()
