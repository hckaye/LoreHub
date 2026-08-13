#!/usr/bin/env bash
set -euo pipefail

service_name="lorehub-runner"
binary_source=""
config_dir=""
service_user="${SUDO_USER:-$(id -un)}"

usage() {
  echo "usage: install.sh --binary PATH [--config-dir PATH] [--user USER]" >&2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)
      binary_source="${2:-}"
      shift 2
      ;;
    --config-dir)
      config_dir="${2:-}"
      shift 2
      ;;
    --user)
      service_user="${2:-}"
      shift 2
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

if [[ "$(id -u)" -ne 0 ]]; then
  echo "install.sh must run as root" >&2
  exit 1
fi
if [[ -z "$binary_source" || ! -x "$binary_source" ]]; then
  echo "--binary must name an executable lorehub-runner file" >&2
  exit 2
fi
if ! id "$service_user" >/dev/null 2>&1; then
  echo "service user does not exist: $service_user" >&2
  exit 2
fi

service_home="$(getent passwd "$service_user" | cut -d: -f6)"
if [[ -z "$service_home" || "$service_home" == "/" ]]; then
  echo "service user has no usable home directory: $service_user" >&2
  exit 2
fi
if [[ -z "$config_dir" ]]; then
  config_dir="$service_home/.config/lorehub-runner"
fi
if [[ "$config_dir" != /* ]]; then
  echo "--config-dir must be an absolute path" >&2
  exit 2
fi
if [[ ! -f "$config_dir/config.json" ]]; then
  echo "runner configuration is missing: $config_dir/config.json" >&2
  exit 2
fi

binary_target="/usr/local/bin/lorehub-runner"
unit_path="/etc/systemd/system/$service_name.service"
install -o root -g root -m 0755 "$binary_source" "$binary_target"
chown -R "$service_user":"$(id -gn "$service_user")" "$config_dir"
chmod 0700 "$config_dir"
chmod 0600 "$config_dir/config.json"

temporary_unit="$(mktemp)"
cleanup() {
  rm -f "$temporary_unit"
}
trap cleanup EXIT

cat >"$temporary_unit" <<EOF
[Unit]
Description=LoreHub self-hosted runner
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
Type=simple
User=$service_user
WorkingDirectory=$config_dir
ExecStart=$binary_target run --config-dir $config_dir
Restart=always
RestartSec=5
TimeoutStopSec=30
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=read-only
ProtectSystem=strict
ReadWritePaths=$config_dir

[Install]
WantedBy=multi-user.target
EOF

install -o root -g root -m 0644 "$temporary_unit" "$unit_path"
systemctl daemon-reload
systemctl enable --now "$service_name.service"
echo "Installed and started $service_name.service"
