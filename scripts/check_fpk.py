"""Validate ownership and checksum of the actual final privileged FPK archive."""

import hashlib
import io
import pathlib
import re
import sys
import tarfile

path = pathlib.Path(sys.argv[1])
with tarfile.open(path, "r:gz") as outer:
    for entry in outer.getmembers():
        if (
            (entry.uid, entry.gid) != (0, 0)
            or entry.uname not in ("", "root")
            or entry.gname not in ("", "root")
        ):
            raise SystemExit(
                f"Non-root archive ownership: {entry.name} ({entry.uid}:{entry.gid}, {entry.uname}:{entry.gname})"
            )
        if (
            entry.name.startswith("/")
            or ".." in pathlib.PurePosixPath(entry.name).parts
            or entry.issym()
            or entry.islnk()
        ):
            raise SystemExit(f"Unexpected outer archive path or link: {entry.name}")
    manifest = dict(
        tuple(value.strip() for value in line.split("=", 1))
        for line in outer.extractfile("manifest").read().decode().splitlines()
        if "=" in line
    )
    payload = outer.extractfile("app.tgz").read()
    if hashlib.md5(payload).hexdigest() != manifest.get("checksum"):
        raise SystemExit("FPK payload checksum mismatch")
    for name in [
        "cmd/main",
        "cmd/install_init",
        "cmd/install_callback",
        "cmd/config_callback",
        "cmd/uninstall_init",
    ]:
        if not outer.getmember(name).isreg() or not outer.getmember(name).mode & 0o111:
            raise SystemExit(f"Missing executable lifecycle hook: {name}")
    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:gz") as inner:
        expected_pins = {}
        for entry in inner.getmembers():
            if (
                (entry.uid, entry.gid) != (0, 0)
                or entry.uname not in ("", "root")
                or entry.gname not in ("", "root")
            ):
                raise SystemExit(f"Non-root payload ownership: {entry.name}")
            if (
                entry.name.startswith("/")
                or ".." in pathlib.PurePosixPath(entry.name).parts
                or entry.issym()
                or entry.islnk()
            ):
                raise SystemExit(f"Unexpected payload path or link: {entry.name}")
            if entry.isreg():
                name = entry.name.removeprefix("./")
                mode = "0755" if name == "bin/fn-terminal" else "0644"
                expected_pins[name] = (
                    hashlib.sha256(inner.extractfile(entry).read()).hexdigest(),
                    mode,
                    str(entry.size),
                )
        for name in ["install_callback", "upgrade_callback"]:
            text = outer.extractfile("cmd/" + name).read().decode()
            block = (
                text.split("# BEGIN PAYLOAD PINS\n", 1)[1]
                .split("# END PAYLOAD PINS\n", 1)[0]
                .splitlines()
            )
            if not block or block[0] != "readonly -a FNTERMINAL_PAYLOAD_PINS=(" or block[-1] != ")":
                raise SystemExit(f"Missing trusted payload pins: {name}")
            actual_pins = {}
            for line in block[1:-1]:
                match = re.fullmatch(
                    r"  '([0-9a-f]{64}):(0[0-7]{3}):([0-9]+):([A-Za-z0-9_./-]+)'", line
                )
                if not match or match[4] in actual_pins:
                    raise SystemExit(f"Malformed payload pin: {name}")
                actual_pins[match[4]] = (match[1], match[2], match[3])
            if actual_pins != expected_pins:
                raise SystemExit(f"Payload pins do not match final archive: {name}")
print(
    f"Final FPK ownership, lifecycle and checksum checks passed: {manifest['version']} / {manifest['platform']}"
)
