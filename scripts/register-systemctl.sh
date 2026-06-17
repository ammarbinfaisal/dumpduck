#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"

binary_path="${DUMPDUCK_BINARY:-${repo_root}/bin/dumpduck}"
config_path="${DUMPDUCK_CONFIG:-/etc/dumpduck/config.yaml}"
unit_path="${DUMPDUCK_SYSTEMD_UNIT:-/etc/systemd/system/dumpduck.service}"
rclone_config_path="${DUMPDUCK_RCLONE_CONFIG:-}"

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "register-systemctl.sh must be run on Linux." >&2
  exit 1
fi

cd "${repo_root}"
go build -o "${binary_path}" ./cmd/dumpduck

if [[ ! -f "${config_path}" ]]; then
  sudo "${binary_path}" config init --path "${config_path}"
fi

if [[ -n "${rclone_config_path}" ]]; then
  sudo "${binary_path}" config set \
    --path "${config_path}" \
    upload.rclone_config_path \
    "${rclone_config_path}"
fi

sudo install -d -m 0755 "$(dirname -- "${unit_path}")"
sudo tee "${unit_path}" >/dev/null <<UNIT
[Unit]
Description=DumpDuck tcpdump capture and upload service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${binary_path} run --config ${config_path}
Restart=always
RestartSec=10
StandardOutput=append:/var/log/dumpduck/dumpduck.log
StandardError=append:/var/log/dumpduck/dumpduck.log

[Install]
WantedBy=multi-user.target
UNIT

sudo install -d -m 0755 /var/log/dumpduck
sudo systemctl daemon-reload
sudo systemctl enable --now "$(basename -- "${unit_path}")"
sudo systemctl restart "$(basename -- "${unit_path}")"

sudo systemctl --no-pager --full status "$(basename -- "${unit_path}")"
