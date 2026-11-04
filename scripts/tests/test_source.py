"""The source must describe real release files, not guessed URLs or checksums."""

import hashlib
import io
import json
import pathlib
import subprocess
import tarfile
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
VERSION = (ROOT / "manifest").read_text().split("version=", 1)[1].splitlines()[0]


class SourceTests(unittest.TestCase):
    def package(self, root, platform, version=VERSION):
        data = (
            f"appname=fnterminal\nversion={version}\nplatform={platform}\n"
            "display_name=飞牛终端\ndesc=Terminal\nmaintainer=FN Terminal\ndistributor=FN Terminal\n"
        ).encode()
        path = root / f"fnterminal-{version}-{platform}.fpk"
        with tarfile.open(path, "w:gz") as archive:
            entry = tarfile.TarInfo("manifest")
            entry.size = len(data)
            archive.addfile(entry, io.BytesIO(data))
        return path

    def generate(self, root, *files, source_format=None):
        return subprocess.run(
            [
                "python3",
                str(ROOT / "scripts/build_source.py"),
                "--repository",
                "test-owner/terminal",
                "--tag",
                f"v{VERSION}",
                "--output",
                str(root / "fnpack.json"),
                *(["--format", source_format] if source_format else []),
                *map(str, files),
            ],
            cwd=ROOT,
            capture_output=True,
            text=True,
        )

    def test_v2_source_binds_both_architectures_to_actual_versioned_assets(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            x86 = self.package(root, "x86")
            arm = self.package(root, "arm")
            result = self.generate(root, x86, arm, source_format="v2")
            self.assertEqual(result.returncode, 0, result.stderr)
            data = json.loads((root / "fnpack.json").read_text())
            self.assertEqual(data["schema_version"], "2")
            app = data["apps"]["fnterminal"]
            self.assertEqual(app["categories"], ["系统工具"])
            self.assertEqual(set(app["platform"]), {"x86", "arm"})
            self.assertEqual(
                app["icon_url"],
                f"https://raw.githubusercontent.com/test-owner/terminal/v{VERSION}/ICON_256.PNG",
            )
            packages = app["releases"][VERSION]["packages"]
            self.assertEqual(
                packages["x86"]["download_url"],
                f"https://github.com/test-owner/terminal/releases/download/v{VERSION}/fnterminal-{VERSION}-x86.fpk",
            )
            self.assertEqual(
                packages["x86"]["sha256"], hashlib.sha256(x86.read_bytes()).hexdigest()
            )
            self.assertEqual(packages["arm"]["size"], arm.stat().st_size)

    def test_default_source_uses_flat_app_map_with_architecture_overrides(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            x86 = self.package(root, "x86")
            arm = self.package(root, "arm")
            result = self.generate(root, x86, arm)
            self.assertEqual(result.returncode, 0, result.stderr)
            data = json.loads((root / "fnpack.json").read_text())
            self.assertEqual(set(data), {"fnterminal"})
            app = data["fnterminal"]
            self.assertEqual(app["version"], VERSION)
            self.assertEqual(app["author"], "medivh-jay")
            self.assertEqual(app["author_url"], "https://github.com/medivh-jay/fn-terminal")
            self.assertEqual(app["bug_report_url"], "https://github.com/test-owner/terminal/issues")
            self.assertEqual(app["install_type"], "系统空间")
            self.assertEqual(set(app["platform"]), {"x86", "arm"})
            self.assertEqual(app["labels"], "系统工具,终端")
            packages = app["arch_diff"]
            self.assertEqual(
                packages["x86"]["download_url"],
                f"https://github.com/test-owner/terminal/releases/download/v{VERSION}/{x86.name}",
            )
            self.assertEqual(packages["arm"]["size_bytes"], arm.stat().st_size)
            self.assertEqual(
                packages["arm"]["sha256"], hashlib.sha256(arm.read_bytes()).hexdigest()
            )
            self.assertTrue(packages["arm"]["size"].endswith("KB"))

    def test_mismatched_version_or_duplicate_platform_cannot_be_published(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            current = self.package(root, "x86")
            old = self.package(root, "arm", "0.0.1")
            for files in [(current, old), (current, current)]:
                self.assertNotEqual(self.generate(root, *files).returncode, 0)
                self.assertFalse((root / "fnpack.json").exists())


if __name__ == "__main__":
    unittest.main()
