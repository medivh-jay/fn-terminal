# Restore a known payload from SDK group-writable extraction defaults without
# executing or promoting an unverified inode. Pins live in the trusted callback.
secure_payload_directory() {
  local path="$1" package_uid="$2" owner mode
  [[ -d "$path" && ! -L "$path" ]] || {
    TASK_TRUST_ERROR="程序目录不是普通目录或是符号链接：$path"
    return 1
  }
  read -r owner mode < <(stat -c '%u %a' -- "$path") || return 1
  [[ "$owner" == 0 || "$owner" == "$package_uid" ]] || {
    TASK_TRUST_ERROR="程序目录所有者异常：$path"
    return 1
  }
  (((8#$mode & 8#002) == 0)) || {
    TASK_TRUST_ERROR="程序目录允许所有用户写入：$path"
    return 1
  }
  chown root:root "$path" && chmod 0755 "$path" && require_trusted_path "$path"
}
cleanup_payload_workspace() {
  local directory="$1" file
  [[ -d "$directory" && ! -L "$directory" ]] || return 0
  for file in "$directory"/copy.*; do
    if [[ -f "$file" && ! -L "$file" ]]; then unlink "$file" || true; fi
  done
  rmdir "$directory" 2>/dev/null || true
  return 0
}
prepare_app_payload() {
  local original="${TRIM_APPDEST:?}" root package_uid owner mode parent_mode='' entry digest final_mode size relative directory component source temporary actual workspace i
  local -a copies=() destinations=() modes=() parts=()
  if ! declare -p FNTERMINAL_PAYLOAD_PINS >/dev/null 2>&1 || ((${#FNTERMINAL_PAYLOAD_PINS[@]} == 0 || ${#FNTERMINAL_PAYLOAD_PINS[@]} > 128)); then
    TASK_TRUST_ERROR=安装包缺少有效的程序完整性校验信息
    return 1
  fi
  [[ "$original" == /* ]] || {
    TASK_TRUST_ERROR="程序目录不是绝对路径：$original"
    return 1
  }
  root="$(readlink -e -- "$original")" || return 1
  if [[ -L "$original" && "$(stat -c '%u' -- "$original")" != 0 ]]; then
    TASK_TRUST_ERROR="程序目录链接不是 root 所有：$original"
    return 1
  fi
  [[ -d "$root" && ! -L "$root" ]] || return 1
  package_uid="$(id -u "${TRIM_USERNAME:?}")" || return 1
  owner="$(stat -c '%u' -- "$root")" || return 1
  [[ "$owner" != 0 ]] || parent_mode=directory-parent
  require_trusted_path "$(dirname -- "$original")" "$parent_mode" || return 1
  require_trusted_path "$(dirname -- "$root")" "$parent_mode" || return 1
  secure_payload_directory "$root" "$package_uid" || return 1
  workspace="$(mktemp -d "$root/.verified.XXXXXXXX")" || return 1
  for entry in "${FNTERMINAL_PAYLOAD_PINS[@]}"; do
    IFS=: read -r digest final_mode size relative <<<"$entry"
    if [[ ! "$digest" =~ ^[0-9a-f]{64}$ || ! "$size" =~ ^[0-9]{1,8}$ || ! "$relative" =~ ^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$ || "/$relative/" == */../* || "/$relative/" == */./* ]] || ((10#$size > 67108864)) || [[ "$final_mode" != 0644 && "$final_mode" != 0755 ]]; then
      TASK_TRUST_ERROR=安装包程序校验信息无效
      cleanup_payload_workspace "$workspace"
      return 1
    fi
    directory="$root"
    IFS=/ read -r -a parts <<<"$relative"
    for ((i = 0; i < ${#parts[@]} - 1; i++)); do
      component="${parts[i]}"
      directory="$directory/$component"
      if ! secure_payload_directory "$directory" "$package_uid"; then
        cleanup_payload_workspace "$workspace"
        return 1
      fi
    done
    source="$root/$relative"
    if [[ ! -f "$source" || -L "$source" ]]; then
      TASK_TRUST_ERROR="程序文件不是普通文件或是符号链接：$source"
      cleanup_payload_workspace "$workspace"
      return 1
    fi
    read -r owner mode < <(stat -c '%u %a' -- "$source") || {
      cleanup_payload_workspace "$workspace"
      return 1
    }
    if [[ "$owner" != 0 && "$owner" != "$package_uid" ]] || (((8#$mode & 8#002) != 0)); then
      TASK_TRUST_ERROR="程序文件所有者或写权限异常：$source"
      cleanup_payload_workspace "$workspace"
      return 1
    fi
    temporary="$(mktemp "$workspace/copy.XXXXXXXX")" || {
      cleanup_payload_workspace "$workspace"
      return 1
    }
    # Bound reads by the trusted expected size; hash only the new private inode.
    if ! head -c "$((10#$size + 1))" -- "$source" >"$temporary"; then
      TASK_TRUST_ERROR="无法复制程序文件：$source"
      cleanup_payload_workspace "$workspace"
      return 1
    fi
    actual="$(sha256sum -- "$temporary")" || {
      cleanup_payload_workspace "$workspace"
      return 1
    }
    if [[ "${actual%% *}" != "$digest" || "$(stat -c '%s' -- "$temporary")" != "$size" ]]; then
      TASK_TRUST_ERROR="程序文件 SHA-256 校验失败：$source"
      cleanup_payload_workspace "$workspace"
      return 1
    fi
    copies+=("$temporary")
    destinations+=("$source")
    modes+=("$final_mode")
  done
  # Verify the entire bundle before publishing any replacement or executing code.
  for ((i = 0; i < ${#copies[@]}; i++)); do
    if ! chown root:root "${copies[i]}" || ! chmod "${modes[i]}" "${copies[i]}" || ! mv -fT -- "${copies[i]}" "${destinations[i]}"; then
      TASK_TRUST_ERROR="无法发布已校验程序：${destinations[i]}"
      cleanup_payload_workspace "$workspace"
      return 1
    fi
  done
  cleanup_payload_workspace "$workspace"
  require_trusted_path "$original/bin/fn-terminal" && require_trusted_path "$original/config.example.json"
}
