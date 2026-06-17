#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"

binary_path="${DUMPDUCK_BINARY:-${repo_root}/bin/dumpduck}"
config_path="${DUMPDUCK_CONFIG:-/etc/dumpduck/config.yaml}"
plist_path="${DUMPDUCK_PLIST:-/Library/LaunchDaemons/com.dumpduck.service.plist}"
rclone_config_path="${DUMPDUCK_RCLONE_CONFIG:-}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "register-launchctl.sh must be run on macOS." >&2
  exit 1
fi

cd "${repo_root}"
go build -o "${binary_path}" ./cmd/dumpduck

sudo "${binary_path}" install \
  --binary "${binary_path}" \
  --config "${config_path}" \
  --plist "${plist_path}" \
  --skip-load

if [[ -n "${rclone_config_path}" ]]; then
  sudo "${binary_path}" config set \
    --path "${config_path}" \
    upload.rclone_config_path \
    "${rclone_config_path}"
fi

sudo launchctl bootout system/com.dumpduck.service >/dev/null 2>&1 || true
sudo launchctl bootstrap system "${plist_path}"
sudo launchctl kickstart -k system/com.dumpduck.service

"${binary_path}" status --config "${config_path}"
