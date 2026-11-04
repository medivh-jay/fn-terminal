"""Exercise a standalone package checkout with a hand-maintained manifest."""

import os
import pathlib
import shutil
import subprocess
import tarfile
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]


class RepositoryTests(unittest.TestCase):
    def test_arm_metadata_cannot_silently_diverge_from_root_manifest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            text = (ROOT / "manifest").read_text()
            (root / "manifest").write_text(text)
            (root / "manifest.arm64").write_text(text.replace("platform=x86", "platform=arm"))
            command = ["python3", str(ROOT / "scripts/manifest.py"), str(root), "arm64", "version"]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            (root / "manifest.arm64").write_text(
                text.replace("platform=x86", "platform=arm").replace(
                    "\nmaintainer=", "\nmaintainer=Different ", 1
                )
            )
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("differ", result.stderr)

    def test_staging_preserves_root_manifest_without_generating_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            shutil.copytree(ROOT / "scripts", root / "scripts")
            source = ROOT / "packaging" if (ROOT / "packaging").exists() else ROOT
            for name in ["app", "cmd", "config", "wizard"]:
                shutil.copytree(source / name, root / name)
            for name in ["ICON.PNG", "ICON_256.PNG"]:
                shutil.copy2(source / name, root / name)
            # The legacy directory exists to show that old packaging ignores the
            # checked-in manifest rather than failing only on a missing path.
            legacy = root / "packaging"
            legacy.mkdir()
            for name in ["app", "cmd", "config", "wizard", "ICON.PNG", "ICON_256.PNG"]:
                if (root / name).is_dir():
                    shutil.copytree(root / name, legacy / name)
                else:
                    shutil.copy2(root / name, legacy / name)
            shutil.copytree(ROOT / "configs", root / "configs")
            (root / "build").mkdir()
            shutil.copy2(
                ROOT / "build/fn-terminal-linux-amd64", root / "build/fn-terminal-linux-amd64"
            )
            manifest = (
                "# Maintained by hand; preserve this comment and field order.\n"
                "appname=fnterminal\nversion=6.7.8\ndisplay_name=飞牛终端\n"
                "desc=Fixture terminal\nsource=thirdparty\nplatform=x86\n"
                "maintainer=Fixture Maintainer\ndistributor=Fixture Maintainer\n"
                "desktop_uidir=ui\ndesktop_applaunchname=fnterminal.main\n"
                "ctl_stop=true\ncheckport=false\nmicro_app=true\n"
                "disable_authorization_path=true\nchangelog=Fixture\n"
            )
            (root / "manifest").write_text(manifest)
            (root / "manifest.arm64").write_text(manifest.replace("platform=x86", "platform=arm"))
            result = subprocess.run(
                ["bash", "scripts/package.sh", "amd64"],
                cwd=root,
                env=dict(os.environ, FNTERMINAL_STAGE_ONLY="1"),
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            stages = list((root / "build").glob("fpk-amd64.*"))
            self.assertEqual(len(stages), 1)
            self.assertEqual((stages[0] / "manifest").read_text(), manifest)
            self.assertEqual((root / "manifest").read_text(), manifest)
            self.assertFalse((stages[0] / "cmd/fn-terminal").exists())
            if os.environ.get("FNTERMINAL_FNPACK") or shutil.which("fnpack"):
                result = subprocess.run(
                    ["bash", "scripts/package.sh", "amd64"],
                    cwd=root,
                    env=dict(os.environ, FNTERMINAL_STAGE_ONLY="0"),
                    capture_output=True,
                    text=True,
                )
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                with tarfile.open(root / "build/fnterminal-6.7.8-amd64.fpk") as archive:
                    self.assertIn(b"6.7.8", archive.extractfile("manifest").read())
            (stages[0] / "manifest").write_text(manifest.replace("platform=x86", "platform=arm"))
            result = subprocess.run(
                ["python3", "scripts/check_package.py", str(stages[0])],
                cwd=root,
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("architecture", result.stderr)


if __name__ == "__main__":
    unittest.main()
