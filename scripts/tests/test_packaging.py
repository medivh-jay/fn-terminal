import os
import pathlib
import shutil
import tarfile
import io
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]


class PackagingTests(unittest.TestCase):
    @unittest.skipUnless(
        os.environ.get("FNTERMINAL_FNPACK") or shutil.which("fnpack"),
        "archive ownership test requires official fnpack",
    )
    def test_official_fpk_has_root_owned_scripts_and_payload_without_host_root(self):
        result = subprocess.run(
            ["bash", "scripts/package.sh", "amd64"],
            cwd=ROOT,
            env=dict(os.environ, FNTERMINAL_STAGE_ONLY="0"),
            capture_output=True,
            text=True,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        with tarfile.open(
            ROOT
            / f"build/fnterminal-{(ROOT / 'manifest').read_text().split('version=',1)[1].splitlines()[0]}-amd64.fpk",
            "r:gz",
        ) as outer:
            for entry in outer.getmembers():
                self.assertEqual((entry.uid, entry.gid), (0, 0), entry.name)
            with tarfile.open(
                fileobj=io.BytesIO(outer.extractfile("app.tgz").read()), mode="r:gz"
            ) as payload:
                for entry in payload.getmembers():
                    self.assertEqual((entry.uid, entry.gid), (0, 0), entry.name)

    def test_real_trust_guard_accepts_system_paths_and_rejects_replaceable_paths(self):
        # Run the actual production functions without its lifecycle-entry self-check.
        guard = (
            (ROOT / "scripts/lifecycle_guard.sh").read_text().split("task_lifecycle_script=", 1)[0]
        )
        with tempfile.TemporaryDirectory() as directory:
            link = pathlib.Path(directory) / "link"
            link.symlink_to("/usr/bin/env")
            for path, allowed in [
                ("/usr/bin/env", True),
                ("/bin/bash", True),
                ("/tmp", False),
                (str(link), False),
            ]:
                result = subprocess.run(
                    ["bash", "-c", guard + '\nrequire_trusted_path "$1"', "guard-test", path],
                    capture_output=True,
                )
                self.assertEqual(
                    result.returncode == 0, allowed, f"wrong trust decision for {path}"
                )

    def test_start_rejects_writable_paths_before_any_candidate_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            base = pathlib.Path(directory)
            (base / "app/bin").mkdir(parents=True)
            (base / "etc").mkdir()
            (base / "var").mkdir()
            candidate = base / "app/bin/fn-terminal"
            marker = base / "executed"
            candidate.write_text(f"#!/bin/sh\ntouch '{marker}'\n")
            candidate.chmod(0o755)
            (base / "etc/config.json").write_text('{"enabled":false}')
            env = dict(
                os.environ,
                TRIM_APPDEST=str(base / "app"),
                TRIM_PKGETC=str(base / "etc"),
                TRIM_PKGVAR=str(base / "var"),
                TRIM_USERNAME="nobody",
            )
            result = subprocess.run(
                ["bash", "cmd/main", "start"],
                cwd=ROOT,
                env=env,
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(marker.exists(), "untrusted candidate executed before path validation")
            self.assertFalse(
                (base / "var/service.log").exists(), "opened runtime paths before trust validation"
            )

    def test_fnpack_failure_with_zero_exit_code_is_not_success(self):
        # fnpack 1.2.3 has been observed returning 0 after reporting a validation failure.
        # A stale artifact in build/ must not turn that into a successful release.
        with tempfile.TemporaryDirectory() as directory:
            tool = pathlib.Path(directory) / "fnpack"
            tool.write_text("#!/bin/sh\necho 'Packing failed.'\nexit 0\n")
            tool.chmod(0o755)
            env = dict(
                os.environ,
                PATH=directory + os.pathsep + os.environ["PATH"],
                FNTERMINAL_FNPACK=str(tool),
                FNTERMINAL_STAGE_ONLY="0",
            )
            result = subprocess.run(
                ["bash", "scripts/package.sh", "amd64"],
                cwd=ROOT,
                env=env,
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(result.returncode, 0, result.stdout)

    def test_configuration_wizard_rejects_invalid_opt_in(self):
        env = dict(os.environ, wizard_terminal_enabled="unexpected")
        result = subprocess.run(["bash", "cmd/config_init"], cwd=ROOT, env=env, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        env = dict(os.environ, wizard_terminal_enabled="true", wizard_reset_origins="unexpected")
        result = subprocess.run(["bash", "cmd/config_init"], cwd=ROOT, env=env, capture_output=True)
        self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
