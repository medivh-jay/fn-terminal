#!/usr/bin/env bash
set -euo pipefail
task_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
task_arch="${1:-amd64}"
case "$task_arch" in amd64 | arm64) ;; *)
  echo 'Usage: scripts/build.sh [amd64|arm64]' >&2
  exit 2
  ;;
esac
cd "$task_root"
task_version="$(python3 scripts/manifest.py "$task_root" "$task_arch" version)"
npm --prefix web ci --cache "${FNTERMINAL_NPM_CACHE:-/tmp/fn-terminal-npm}"
npm --prefix web test
npm --prefix web run build
mkdir -p build
CGO_ENABLED=0 GOOS=linux GOARCH="$task_arch" go build -buildvcs=false -trimpath -ldflags="-s -w -X main.version=$task_version" -o "build/fn-terminal-linux-$task_arch" ./backend/cmd/fn-terminal
echo "Built build/fn-terminal-linux-$task_arch"
