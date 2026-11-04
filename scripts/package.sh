#!/usr/bin/env bash
set -euo pipefail
task_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
task_arch="${1:-amd64}"
case "$task_arch" in amd64 | arm64) ;; *)
  echo 'Usage: scripts/package.sh [amd64|arm64]' >&2
  exit 2
  ;;
esac
task_manifest="$(python3 "$task_root/scripts/manifest.py" "$task_root" "$task_arch" path)"
task_version="$(python3 "$task_root/scripts/manifest.py" "$task_root" "$task_arch" version)"
task_appname="$(python3 "$task_root/scripts/manifest.py" "$task_root" "$task_arch" appname)"
[[ -f "$task_root/build/fn-terminal-linux-$task_arch" ]] || {
  echo 'Run scripts/build.sh first' >&2
  exit 2
}
mkdir -p "$task_root/build"
task_stage="$(mktemp -d "$task_root/build/fpk-$task_arch.XXXXXX")"
for task_path in app config wizard ICON.PNG ICON_256.PNG; do
  cp -R "$task_root/$task_path" "$task_stage/"
done
mkdir -p "$task_stage/cmd"
for task_hook in "$task_root/cmd/"*; do
  [[ -f "$task_hook" ]] || {
    echo "Unexpected lifecycle entry: $task_hook" >&2
    exit 2
  }
  install -m 0755 "$task_hook" "$task_stage/cmd/"
done
install -m 0644 "$task_manifest" "$task_stage/manifest"
mkdir -p "$task_stage/app/bin"
install -m 0755 "$task_root/build/fn-terminal-linux-$task_arch" "$task_stage/app/bin/fn-terminal"
install -m 0644 "$task_root/configs/config.example.json" "$task_stage/app/config.example.json"
# fnpack also places these declarations inside app.tgz. Stage them explicitly
# before generating pins so the trusted list matches the actual final payload.
mkdir -p "$task_stage/app/config"
install -m 0644 "$task_stage/config/privilege" "$task_stage/app/config/privilege"
install -m 0644 "$task_stage/config/resource" "$task_stage/app/config/resource"
python3 "$task_root/scripts/render_payload_pins.py" "$task_stage"
chmod 0755 "$task_stage/cmd/"*
python3 "$task_root/scripts/check_package.py" "$task_stage"
if [[ "${FNTERMINAL_STAGE_ONLY:-0}" == 1 ]]; then
  echo "Validated package directory: $task_stage (no FPK built)"
else
  task_fnpack="${FNTERMINAL_FNPACK:-fnpack}"
  command -v "$task_fnpack" >/dev/null || {
    echo 'fnpack not found; install the official tool or set FNTERMINAL_STAGE_ONLY=1 to validate the directory only' >&2
    exit 2
  }
  task_output="$(mktemp -d "$task_root/build/fnpack-$task_arch.XXXXXX")"
  cd "$task_output"
  if [[ "$(id -u)" == 0 ]]; then
    "$task_fnpack" build --directory "$task_stage"
  else
    command -v unshare >/dev/null || {
      echo 'Rootless FPK packaging requires util-linux unshare to emit root-owned archive metadata' >&2
      exit 2
    }
    # Map this build user to root only inside a new user namespace. Host file
    # permissions do not change; fnpack sees UID/GID 0 and writes correct headers.
    unshare --user --map-root-user -- "$task_fnpack" build --directory "$task_stage"
  fi
  [[ -s "$task_appname.fpk" ]] || {
    echo 'fnpack did not produce an FPK; inspect its validation errors above' >&2
    exit 1
  }
  python3 "$task_root/scripts/check_fpk.py" "$task_appname.fpk"
  mv -- "$task_appname.fpk" "$task_root/build/$task_appname-$task_version-$task_arch.fpk"
  cd "$task_root"
  rmdir -- "$task_output"
  echo "Built build/$task_appname-$task_version-$task_arch.fpk"
fi
