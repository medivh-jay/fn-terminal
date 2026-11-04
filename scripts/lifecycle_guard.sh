# Embedded into each lifecycle callback; no unvalidated helper is sourced as root.
# fnOS's protected lifecycle dispatcher is the initial trust boundary.
require_trusted_path() {
  local original="$1" parent_mode="${2:-}" resolved start node metadata owner mode
  [[ "$original" == /* ]] || {
    TASK_TRUST_ERROR="路径不是绝对路径：$original"
    return 1
  }
  resolved="$(readlink -e -- "$original")" || {
    TASK_TRUST_ERROR="路径不存在或无法解析：$original"
    return 1
  }
  for start in "$original" "$resolved"; do
    node="$start"
    while :; do
      if [[ -L "$node" && "$(stat -c '%u' -- "$node")" != 0 ]]; then
        TASK_TRUST_ERROR="符号链接不是 root 所有：$node"
        return 1
      fi
      metadata="$(stat -Lc '%u %a' -- "$node")" || {
        TASK_TRUST_ERROR="无法读取路径权限：$node"
        return 1
      }
      read -r owner mode <<<"$metadata"
      [[ "$owner" == 0 && "$mode" =~ ^[0-7]+$ ]] || {
        TASK_TRUST_ERROR="路径不是 root 所有：$node (uid=$owner)"
        return 1
      }
      if (((8#$mode & 8#022) != 0)); then
        # Sticky root-owned ancestors protect their root-owned child entries.
        # Do not accept a public directory as the protected object itself.
        if [[ ! -d "$node" ]] || (((8#$mode & 8#1000) == 0)) || [[ "$node" == "$start" && "$parent_mode" != directory-parent ]]; then
          TASK_TRUST_ERROR="路径可被非 root 用户写入：$node (mode=$mode)"
          return 1
        fi
      fi
      [[ "$node" == / ]] && break
      node="$(dirname -- "$node")"
    done
  done
}
require_optional_regular() {
  if [[ -e "$1" || -L "$1" ]]; then
    [[ -f "$1" && ! -L "$1" ]] || {
      TASK_TRUST_ERROR="运行文件不是普通文件或是符号链接：$1"
      return 1
    }
    require_trusted_path "$1" || return 1
  fi
}
secure_data_directory() {
  local original="$1" resolved owner package_uid parent_mode=''
  [[ "$original" == /* ]] || {
    TASK_TRUST_ERROR="数据目录不是绝对路径：$original"
    return 1
  }
  resolved="$(readlink -e -- "$original")" || {
    TASK_TRUST_ERROR="数据目录不存在：$original"
    return 1
  }
  [[ -d "$resolved" ]] || {
    TASK_TRUST_ERROR="数据路径不是目录：$original"
    return 1
  }
  if [[ -L "$original" && "$(stat -c '%u' -- "$original")" != 0 ]]; then
    TASK_TRUST_ERROR="数据目录链接不是 root 所有：$original"
    return 1
  fi
  owner="$(stat -c '%u' -- "$resolved")" || return 1
  package_uid="$(id -u "${TRIM_USERNAME:?}")" || {
    TASK_TRUST_ERROR="应用用户不存在：$TRIM_USERNAME"
    return 1
  }
  [[ "$owner" == 0 || "$owner" == "$package_uid" ]] || {
    TASK_TRUST_ERROR="数据目录不属于 root 或应用用户：$resolved (uid=$owner)"
    return 1
  }
  # Only a root-owned entry is safe to adopt under a public sticky parent.
  [[ "$owner" != 0 ]] || parent_mode=directory-parent
  require_trusted_path "$(dirname -- "$original")" "$parent_mode" || return 1
  require_trusted_path "$(dirname -- "$resolved")" "$parent_mode" || return 1
  # Take ownership only of these data directories, never executable content or shared ancestors.
  chown "root:${TRIM_GROUPNAME:?}" "$resolved" || return 1
  chmod 0750 "$resolved" || return 1
  require_trusted_path "$original"
}
trust_fail() {
  echo "${TASK_TRUST_ERROR:-拒绝不可信的特权启动路径或运行文件。}" >&2
  exit 1
}
lifecycle_log_record() {
  local task_event="$1" task_detail="$2" task_saved_error="${TASK_TRUST_ERROR:-}" task_record task_fixed_log=/var/log/fnterminal-install.log
  printf -v task_record '%s [%s] %s: %s' "$(date -Is)" "$task_event" "${task_lifecycle_script:-unknown}" "${task_detail:0:4096}"
  # This root-protected path survives rollback of /var/apps/fnterminal. Never
  # follow a log symlink or trust a file created by an unprivileged account.
  if [[ "$(id -u)" == 0 ]] && require_trusted_path /var/log; then
    if [[ ! -e "$task_fixed_log" && ! -L "$task_fixed_log" ]]; then (
      umask 077
      set -o noclobber
      : >"$task_fixed_log"
    ) 2>/dev/null || true; fi
    if [[ -f "$task_fixed_log" && ! -L "$task_fixed_log" ]] && require_trusted_path "$task_fixed_log" && [[ "$(stat -c '%a' -- "$task_fixed_log")" == 600 ]]; then
      printf '%s\n' "$task_record" >>"$task_fixed_log" || true
    fi
  fi
  if command -v logger >/dev/null 2>&1; then logger -t fnterminal-install -- "$task_record" 2>/dev/null || true; fi
  TASK_TRUST_ERROR="$task_saved_error"
  return 0
}
lifecycle_on_exit() {
  local task_status="$1" task_line="$2" task_message task_log="${TRIM_TEMP_LOGFILE:-}"
  if [[ "$task_status" == 0 ]]; then
    if [[ "${task_lifecycle_script:-}" != main || "${task_lifecycle_action:-}" != status ]]; then lifecycle_log_record OK "${TASK_PHASE:-执行回调} 已完成" || true; fi
    return 0
  fi
  if [[ "${task_lifecycle_script:-}" == main && "${task_lifecycle_action:-}" == status && "$task_status" == 3 ]]; then return 0; fi
  trap - EXIT
  set +e
  task_message="飞牛终端回调 ${task_lifecycle_script:-unknown} 失败：${TASK_PHASE:-执行回调}；行 $task_line，退出码 $task_status。${TASK_TRUST_ERROR:-}"
  printf '%s\n' "$task_message" >&2
  lifecycle_log_record FAIL "$task_message" || true
  # The platform supplies this path. Reject symlinks and unsafe parents; create
  # an absent root-owned error file exclusively before publishing diagnostics.
  if [[ -n "$task_log" && "$task_log" == /* ]] && require_trusted_path "$(dirname -- "$task_log")" directory-parent; then
    if [[ ! -e "$task_log" && ! -L "$task_log" ]]; then (
      umask 077
      set -o noclobber
      : >"$task_log"
    ) 2>/dev/null; fi
    if [[ -f "$task_log" && ! -L "$task_log" ]] && require_trusted_path "$task_log"; then
      printf '%s\n' "$task_message" >"$task_log"
    fi
  fi
  exit "$task_status"
}
task_lifecycle_script="${BASH_SOURCE[0]##*/}"
task_lifecycle_action="${1:-}"
TASK_PHASE=验证回调执行路径
TASK_TRUST_ERROR=''
trap 'lifecycle_on_exit "$?" "$LINENO"' EXIT
task_lifecycle_path="${BASH_SOURCE[0]}"
[[ "$task_lifecycle_path" == /* ]] || task_lifecycle_path="$(pwd -P)/$task_lifecycle_path"
if [[ "$task_lifecycle_script" != main || "$task_lifecycle_action" != status ]]; then
  # Only integration context is logged, never the full environment or credentials.
  printf -v task_lifecycle_context 'uid=%q bash=%q script=%q app_user=%q app_group=%q appdest=%q etc=%q var=%q fnos=%q' "$(id -u)" "$BASH_VERSION" "$task_lifecycle_path" "${TRIM_USERNAME:-<unset>}" "${TRIM_GROUPNAME:-<unset>}" "${TRIM_APPDEST:-<unset>}" "${TRIM_PKGETC:-<unset>}" "${TRIM_PKGVAR:-<unset>}" "${TRIM_SYS_VERSION:-<unset>}"
  lifecycle_log_record BEGIN "$task_lifecycle_context" || true
fi
require_trusted_path "$task_lifecycle_path" || trust_fail
