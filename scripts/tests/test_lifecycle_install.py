"""Exercise real install callbacks as namespace-root in a private filesystem.

The host user's identity and system files are unchanged. System tools and source
are read-only binds; every writable path lives in one temporary fixture tree.
"""

import pathlib
import os
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]


@unittest.skipUnless(shutil.which("bwrap"), "isolated lifecycle tests require bubblewrap")
class InstallLifecycleTests(unittest.TestCase):
    def run_install(self, scenario):
        with tempfile.TemporaryDirectory(
            prefix="fnterminal-install-"
        ) as directory, tempfile.TemporaryDirectory(prefix="fnterminal-pins-") as pin_directory:
            pin_stage = pathlib.Path(pin_directory)
            (pin_stage / "app/bin").mkdir(parents=True)
            (pin_stage / "cmd").mkdir()
            shutil.copy2(ROOT / "build/fn-terminal-linux-amd64", pin_stage / "app/bin/fn-terminal")
            shutil.copy2(
                ROOT / "configs/config.example.json", pin_stage / "app/config.example.json"
            )
            shutil.copytree(ROOT / "app/ui", pin_stage / "app/ui")
            shutil.copytree(ROOT / "config", pin_stage / "app/config")
            for name in ["install_callback", "upgrade_callback"]:
                shutil.copy2(ROOT / "cmd" / name, pin_stage / "cmd" / name)
            subprocess.run(
                ["python3", str(ROOT / "scripts/render_payload_pins.py"), pin_directory],
                check=True,
                capture_output=True,
            )
            fixture = pathlib.Path(directory)
            for name in ["usr", "lib", "lib64", "proc", "dev", "etc", "source", "payload-fixture"]:
                (fixture / name).mkdir()
            command = [
                "bwrap",
                "--unshare-user",
                "--uid",
                "0",
                "--gid",
                "0",
                "--cap-add",
                "CAP_DAC_OVERRIDE",
                "--unshare-pid",
                "--die-with-parent",
                "--bind",
                directory,
                "/",
                "--ro-bind",
                "/usr",
                "/usr",
                "--ro-bind",
                "/lib",
                "/lib",
                "--ro-bind",
                "/lib64",
                "/lib64",
                "--symlink",
                "usr/bin",
                "/bin",
                "--ro-bind",
                "/etc/group",
                "/etc/group",
                "--ro-bind",
                "/etc/passwd",
                "/etc/passwd",
                "--ro-bind",
                str(ROOT),
                "/source",
                "--ro-bind",
                pin_directory,
                "/payload-fixture",
                "--proc",
                "/proc",
                "--dev",
                "/dev",
                "--setenv",
                "FNTERMINAL_TEST_VERSION",
                (ROOT / "manifest").read_text().split("version=", 1)[1].splitlines()[0],
                "/bin/bash",
                "-c",
                r"""
set -eu
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
export TRIM_APPDEST=/vol1/@appcenter/fnterminal
export TRIM_PKGETC=/vol1/@appconf/fnterminal
export TRIM_PKGVAR=/vol1/@appdata/fnterminal
export TRIM_USERNAME=nobody TRIM_GROUPNAME=root
export TRIM_TEMP_LOGFILE=/tmp/install-message.log wizard_terminal_enabled=true
mkdir -p "$TRIM_APPDEST/bin" "$TRIM_PKGETC" "$TRIM_PKGVAR" /var/apps/fnterminal/cmd /tmp/installer/fnterminal/cmd /var/log
chmod 1777 /tmp
chmod 0700 /tmp/installer
cp /source/build/fn-terminal-linux-amd64 "$TRIM_APPDEST/bin/fn-terminal"
chmod 0755 "$TRIM_APPDEST/bin/fn-terminal"
cp /source/configs/config.example.json "$TRIM_APPDEST/config.example.json"
cp -R /source/app/ui "$TRIM_APPDEST/ui"
cp -R /source/config "$TRIM_APPDEST/config"
cp /payload-fixture/cmd/install_callback /var/apps/fnterminal/cmd/install_callback
cp /payload-fixture/cmd/install_callback /tmp/installer/fnterminal/cmd/install_callback
cp /payload-fixture/cmd/upgrade_callback /var/apps/fnterminal/cmd/upgrade_callback
: > "$TRIM_TEMP_LOGFILE"
chmod 0600 "$TRIM_TEMP_LOGFILE"
printf '%s\n' DO_NOT_CHANGE > /sentinel
chmod 0600 /sentinel
callback=/var/apps/fnterminal/cmd/install_callback
if [ "$1" = archive_install ]; then
  tar -xzf /source/build/fnterminal-${FNTERMINAL_TEST_VERSION}-amd64.fpk -C /var/apps/fnterminal
  tar -xzf /var/apps/fnterminal/app.tgz -C "$TRIM_APPDEST"
  cp /var/apps/fnterminal/cmd/install_callback /tmp/installer/fnterminal/cmd/install_callback
  callback=/tmp/installer/fnterminal/cmd/install_callback
  find "$TRIM_APPDEST" -type d -exec chmod 0775 {} +
  find "$TRIM_APPDEST" -type f -exec chmod 0775 {} +
  chmod 0000 /vol1
fi
case "$1" in
  staging) callback=/tmp/installer/fnterminal/cmd/install_callback ;;
  data_permissions) chmod 0775 "$TRIM_PKGETC" "$TRIM_PKGVAR" ;;
  sdk_permissions) find "$TRIM_APPDEST" -type d -exec chmod 0775 {} +; find "$TRIM_APPDEST" -type f -exec chmod 0775 {} +; chmod 0000 /vol1 ;;
  retained_writer) chmod 0775 "$TRIM_APPDEST/bin/fn-terminal"; exec 9<> "$TRIM_APPDEST/bin/fn-terminal" ;;
  tampered_binary) printf '#!/bin/bash\necho EXECUTED_CANDIDATE > /executed-candidate\n' > "$TRIM_APPDEST/bin/fn-terminal"; chmod 0775 "$TRIM_APPDEST/bin/fn-terminal" ;;
  tampered_template) printf tampered >> "$TRIM_APPDEST/config.example.json" ;;
  tampered_ui) printf tampered >> "$TRIM_APPDEST/ui/config" ;;
  payload_symlink) unlink "$TRIM_APPDEST/config.example.json"; ln -s /sentinel "$TRIM_APPDEST/config.example.json" ;;
  directory_symlink) mv "$TRIM_APPDEST/bin" /external-code; ln -s /external-code "$TRIM_APPDEST/bin" ;;
  public_payload_dir) chmod 0777 "$TRIM_APPDEST/ui" ;;
  missing_pins) cp /source/cmd/install_callback "$callback" ;;
  upgrade) callback=/var/apps/fnterminal/cmd/upgrade_callback; printf '{"enabled":false,"idle_seconds":60}\n' > "$TRIM_PKGETC/config.json"; chmod 0600 "$TRIM_PKGETC/config.json"; find "$TRIM_APPDEST" -type d -exec chmod 0775 {} +; find "$TRIM_APPDEST" -type f -exec chmod 0775 {} + ;;
  bad_binary) chmod 0777 "$TRIM_APPDEST/bin/fn-terminal" ;;
  bad_ancestor) chmod 0777 /vol1/@appcenter ;;
  config_symlink) ln -s /sentinel "$TRIM_PKGETC/config.json" ;;
  log_symlink) unlink "$TRIM_TEMP_LOGFILE"; ln -s /sentinel "$TRIM_TEMP_LOGFILE"; chmod 0777 "$TRIM_APPDEST/bin/fn-terminal" ;;
  no_platform_log) unset TRIM_TEMP_LOGFILE; chmod 0777 "$TRIM_APPDEST/bin/fn-terminal" ;;
  empty_identity) unset TRIM_USERNAME TRIM_GROUPNAME ;;
  unsafe_callback) chmod 0777 "$callback" ;;
  fixed_log_symlink) ln -s /sentinel /var/log/fnterminal-install.log; chmod 0777 "$TRIM_APPDEST/bin/fn-terminal" ;;
esac
set +e
before_inode="$(stat -c '%i' "$TRIM_APPDEST/bin/fn-terminal")"
/bin/bash "$callback"
result=$?
if [[ "$result" == 0 && ( "$1" == config_restore || "$1" == config_preserve ) ]]; then
  printf '{"enabled":true,"allowed_origins":["https://nas.fnos.net"],"allowed_users":["alice"],"idle_seconds":60}\n' > "$TRIM_PKGETC/config.json"
  chmod 0640 "$TRIM_PKGETC/config.json"
  cp /source/cmd/config_callback /var/apps/fnterminal/cmd/config_callback
  # Keep the real configure callback/CLI; replace only service restart to avoid
  # launching a host-user supervisor in this isolated installation fixture.
  printf '#!/bin/bash\nprintf "RESTART_%%s\\n" "$1"\n' > /var/apps/fnterminal/cmd/main
  chmod 0755 /var/apps/fnterminal/cmd/main
  export wizard_reset_origins=false
  if [[ "$1" == config_restore ]]; then export wizard_reset_origins=true; fi
  /bin/bash /var/apps/fnterminal/cmd/config_callback
  result=$?
  stat -c 'CONFIG_MODE=%a:%u' "$TRIM_PKGETC/config.json"
fi
set -e
if [ "$1" = retained_writer ] && [ "$result" = 0 ]; then printf 'UNTRUSTED_WRITER' >&9; fi
echo "CALLBACK_STATUS=$result"
echo 'MANAGER_MESSAGE_BEGIN'
if [ -n "${TRIM_TEMP_LOGFILE:-}" ]; then cat "$TRIM_TEMP_LOGFILE"; fi
echo 'MANAGER_MESSAGE_END'
echo 'PERSISTENT_MESSAGE_BEGIN'
if [ -f /var/log/fnterminal-install.log ]; then cat /var/log/fnterminal-install.log; fi
echo 'PERSISTENT_MESSAGE_END'
if [ -e /var/log/fnterminal-install.log ]; then stat -c 'DIAGNOSTIC_MODE=%a:%u' /var/log/fnterminal-install.log; fi
if [ -f "$TRIM_PKGETC/config.json" ]; then cat "$TRIM_PKGETC/config.json"; fi
stat -c 'DATA_MODE=%a:%u' "$TRIM_PKGETC" "$TRIM_PKGVAR"
stat -c 'CODE_MODE=%a:%u' "$TRIM_APPDEST" "$TRIM_APPDEST/bin" "$TRIM_APPDEST/bin/fn-terminal"
sha256sum "$TRIM_APPDEST/bin/fn-terminal"
echo "BEFORE_INODE=$before_inode"
stat -c 'AFTER_INODE=%i' "$TRIM_APPDEST/bin/fn-terminal"
if [ -e /executed-candidate ]; then cat /executed-candidate; fi
printf 'SENTINEL='; cat /sentinel
exit "$result"
""",
                "install-fixture",
                scenario,
            ]
            return subprocess.run(command, capture_output=True, text=True, timeout=20)

    def test_callback_accepts_root_owned_private_staging_under_sticky_tmp(self):
        result = self.run_install("staging")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('"enabled": true', result.stdout)

    def test_callback_installs_opt_in_with_standard_protected_paths(self):
        result = self.run_install("normal")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('"enabled": true', result.stdout)

    def test_settings_can_restore_automatic_origins_without_losing_policy(self):
        for scenario in ["config_restore", "config_preserve"]:
            with self.subTest(scenario=scenario):
                result = self.run_install(scenario)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('"enabled": true', result.stdout)
                self.assertIn('"idle_seconds": 60', result.stdout)
                self.assertIn('"alice"', result.stdout)
                self.assertIn("CONFIG_MODE=640:0", result.stdout)
                self.assertIn("RESTART_stop", result.stdout)
                self.assertIn("RESTART_start", result.stdout)
                self.assertEqual(
                    '"https://nas.fnos.net"' in result.stdout, scenario == "config_preserve"
                )

    def test_sdk_group_writable_payload_is_verified_and_installed(self):
        result = self.run_install("sdk_permissions")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(result.stdout.count("CODE_MODE=755:0"), 3)
        self.assertIn('"enabled": true', result.stdout)

    def test_tampered_group_writable_binary_is_not_promoted(self):
        result = self.run_install("tampered_binary")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("校验", result.stdout + result.stderr)
        self.assertNotIn('"enabled": true', result.stdout)
        self.assertNotIn("EXECUTED_CANDIDATE", result.stdout)

    def test_entire_payload_is_verified_before_any_replacement(self):
        import re

        for scenario in ["tampered_template", "tampered_ui"]:
            with self.subTest(scenario=scenario):
                result = self.run_install(scenario)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("校验", result.stdout + result.stderr)
                before = re.search(r"BEFORE_INODE=(\d+)", result.stdout)
                after = re.search(r"AFTER_INODE=(\d+)", result.stdout)
                self.assertIsNotNone(before)
                self.assertIsNotNone(after)
                self.assertEqual(
                    before[1], after[1], "published binary before the whole bundle verified"
                )

    def test_payload_symlinks_and_public_directories_remain_rejected(self):
        for scenario in [
            "payload_symlink",
            "directory_symlink",
            "public_payload_dir",
            "missing_pins",
        ]:
            with self.subTest(scenario=scenario):
                result = self.run_install(scenario)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("SENTINEL=DO_NOT_CHANGE\n", result.stdout)
                self.assertNotIn('"enabled": true', result.stdout)

    def test_upgrade_secures_payload_and_preserves_administrator_policy(self):
        result = self.run_install("upgrade")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('"enabled":false', result.stdout)
        self.assertIn('"idle_seconds":60', result.stdout)
        self.assertEqual(result.stdout.count("CODE_MODE=755:0"), 3)

    def test_preexisting_writer_cannot_modify_published_binary(self):
        import hashlib

        expected = hashlib.sha256((ROOT / "build/fn-terminal-linux-amd64").read_bytes()).hexdigest()
        result = self.run_install("retained_writer")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(expected + "  /vol1/@appcenter/fnterminal/bin/fn-terminal", result.stdout)

    @unittest.skipUnless(
        os.environ.get("FNTERMINAL_FNPACK") or shutil.which("fnpack"),
        "full archive install requires official fnpack",
    )
    def test_actual_fpk_extracts_and_installs_from_private_staging(self):
        built = subprocess.run(
            ["bash", "scripts/package.sh", "amd64"],
            cwd=ROOT,
            env=dict(os.environ, FNTERMINAL_STAGE_ONLY="0"),
            capture_output=True,
            text=True,
        )
        self.assertEqual(built.returncode, 0, built.stdout + built.stderr)
        result = self.run_install("archive_install")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('"enabled": true', result.stdout)

    def test_callback_still_rejects_nonsticky_writable_executable_ancestor(self):
        result = self.run_install("bad_ancestor")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('"enabled": true', result.stdout)

    def test_callback_secures_data_directories_before_requiring_final_permissions(self):
        result = self.run_install("data_permissions")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(result.stdout.count("DATA_MODE=750:0"), 2)
        self.assertIn('"enabled": true', result.stdout)

    def test_callback_reports_rejected_binary_path_to_fnos_error_file(self):
        result = self.run_install("bad_binary")
        self.assertNotEqual(result.returncode, 0)
        message = result.stdout.partition("MANAGER_MESSAGE_BEGIN\n")[2].partition(
            "MANAGER_MESSAGE_END"
        )[0]
        self.assertIn("install_callback", message)
        self.assertIn("/vol1/@appcenter/fnterminal/bin/fn-terminal", message)

    def test_existing_config_symlink_is_rejected_without_touching_its_target(self):
        result = self.run_install("config_symlink")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SENTINEL=DO_NOT_CHANGE\n", result.stdout)
        self.assertNotIn('"enabled": true', result.stdout)

    def test_error_log_symlink_does_not_redirect_a_privileged_write(self):
        result = self.run_install("log_symlink")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SENTINEL=DO_NOT_CHANGE\n", result.stdout)
        message = result.stdout.partition("MANAGER_MESSAGE_BEGIN\n")[2].partition(
            "MANAGER_MESSAGE_END"
        )[0]
        self.assertEqual(message, "DO_NOT_CHANGE\n")

    def test_failure_details_persist_without_platform_error_file(self):
        result = self.run_install("no_platform_log")
        self.assertNotEqual(result.returncode, 0)
        message = result.stdout.partition("PERSISTENT_MESSAGE_BEGIN\n")[2].partition(
            "PERSISTENT_MESSAGE_END"
        )[0]
        self.assertIn("install_callback", message)
        self.assertIn("/vol1/@appcenter/fnterminal/bin/fn-terminal", message)
        self.assertIn("DIAGNOSTIC_MODE=600:0", result.stdout)

    def test_missing_identity_is_logged_without_suppressing_failure(self):
        result = self.run_install("empty_identity")
        self.assertNotEqual(result.returncode, 0)
        message = result.stdout.partition("PERSISTENT_MESSAGE_BEGIN\n")[2].partition(
            "PERSISTENT_MESSAGE_END"
        )[0]
        self.assertIn("TRIM_GROUPNAME", message)
        self.assertIn("BEGIN", message)
        self.assertIn("FAIL", message)

    def test_diagnostics_capture_callback_trust_failure_before_execution(self):
        result = self.run_install("unsafe_callback")
        self.assertNotEqual(result.returncode, 0)
        message = result.stdout.partition("PERSISTENT_MESSAGE_BEGIN\n")[2].partition(
            "PERSISTENT_MESSAGE_END"
        )[0]
        self.assertIn("/var/apps/fnterminal/cmd/install_callback", message)
        self.assertIn("FAIL", message)

    def test_fixed_diagnostic_path_cannot_redirect_privileged_writes(self):
        result = self.run_install("fixed_log_symlink")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SENTINEL=DO_NOT_CHANGE\n", result.stdout)
