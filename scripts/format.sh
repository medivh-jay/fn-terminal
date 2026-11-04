#!/usr/bin/env bash
set -euo pipefail
task_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$task_root"
task_mode="${1:-}"
case "$task_mode" in
  '')
    task_prettier=--write
    task_black=()
    task_shfmt=(-w)
    ;;
  --check)
    task_prettier=--check
    task_black=(--check)
    task_shfmt=(-d)
    ;;
  *)
    echo 'Usage: scripts/format.sh [--check]' >&2
    exit 2
    ;;
esac
task_prettier_bin="$task_root/web/node_modules/.bin/prettier"
[[ -x "$task_prettier_bin" ]] || {
  echo 'Run npm --prefix web ci first' >&2
  exit 2
}
command -v uv >/dev/null || {
  echo 'Install uv to run the pinned Black formatter' >&2
  exit 2
}
"$task_prettier_bin" "$task_prettier" 'web/**/*.{ts,mjs,css,html,json}' 'configs/*.json' '.github/**/*.yml' .prettierrc.json
"$task_prettier_bin" "$task_prettier" --parser json app/ui/config config/privilege config/resource wizard/config wizard/install
mapfile -t task_go < <(rg --files backend internal -g '*.go')
if [[ "$task_mode" == --check ]]; then
  task_unformatted="$(gofmt -l "${task_go[@]}")"
  [[ -z "$task_unformatted" ]] || {
    echo "$task_unformatted"
    exit 1
  }
else
  gofmt -w "${task_go[@]}"
fi
uv tool run --from black==26.10.1 black --line-length 100 "${task_black[@]}" scripts
mapfile -t task_shell < <(rg --files scripts cmd -g '*.sh' -g 'cmd/*')
go run mvdan.cc/sh/v3/cmd/shfmt@v3.14.1 -i 2 -ci "${task_shfmt[@]}" "${task_shell[@]}"
if [[ "$task_mode" != --check ]]; then
  python3 scripts/render_lifecycle.py
fi
