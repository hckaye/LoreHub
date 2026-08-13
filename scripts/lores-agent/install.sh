#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
binary_source=""
temporary_binary=""
service_user=lorehub-lores
binary_destination=/usr/local/bin/lorehub-lores-agent
service_destination=/etc/systemd/system/lorehub-lores-agent.service
config_directory=/var/lib/lorehub-lores-agent

usage() {
	cat <<'EOF'
Usage: install.sh [--binary PATH]

Install lorehub-lores-agent and its systemd unit. Run as root.
If --binary is omitted, build the command from this repository.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	--binary)
		if [ "$#" -lt 2 ]; then
			echo "--binary requires a path" >&2
			exit 2
		fi
		binary_source=$2
		shift 2
		;;
	-h|--help)
		usage
		exit 0
		;;
*)
		echo "unknown argument: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

if [ "$(id -u)" -ne 0 ]; then
	echo "install.sh must run as root" >&2
	exit 1
fi

cleanup() {
	if [ -n "$temporary_binary" ]; then
		rm -f -- "$temporary_binary"
	fi
}
trap cleanup EXIT INT TERM

if [ -z "$binary_source" ]; then
	binary_source="$repository_root/services/api/lorehub-lores-agent"
fi
if [ ! -x "$binary_source" ]; then
	if [ "$binary_source" = "$repository_root/services/api/lorehub-lores-agent" ] &&
		[ -f "$repository_root/services/api/go.mod" ] && command -v go >/dev/null 2>&1; then
		temporary_binary=$(mktemp)
		(
			cd "$repository_root/services/api"
			go build -o "$temporary_binary" ./cmd/lorehub-lores-agent
		)
		binary_source=$temporary_binary
	else
		echo "agent binary not found or not executable: $binary_source" >&2
		exit 1
	fi
fi

if ! id "$service_user" >/dev/null 2>&1; then
	if ! command -v useradd >/dev/null 2>&1; then
		echo "useradd is required to create $service_user" >&2
		exit 1
	fi
	useradd --system --user-group --home-dir "$config_directory" \
		--shell /usr/sbin/nologin "$service_user"
fi

install -d -m 0755 /usr/local/bin
install -m 0755 "$binary_source" "$binary_destination"
install -d -m 0700 -o "$service_user" -g "$service_user" "$config_directory"
install -d -m 0755 /etc/lorehub
install -d -m 0755 /etc/systemd/system
if [ -f "$script_dir/lorehub-lores-agent.service" ]; then
	install -m 0644 "$script_dir/lorehub-lores-agent.service" "$service_destination"
else
	cat >"$service_destination" <<'EOF'
[Unit]
Description=LoreHub Lore Server agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=lorehub-lores
Group=lorehub-lores
ExecStart=/usr/local/bin/lorehub-lores-agent run --config-dir /var/lib/lorehub-lores-agent
EnvironmentFile=-/etc/lorehub/lorehub-lores-agent.env
Restart=on-failure
RestartSec=5s
TimeoutStopSec=15s
KillSignal=SIGTERM
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectControlGroups=true
ProtectKernelTunables=true
ProtectKernelModules=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=/var/lib/lorehub-lores-agent

[Install]
WantedBy=multi-user.target
EOF
	chmod 0644 "$service_destination"
fi

if ! command -v systemctl >/dev/null 2>&1; then
	echo "systemctl is required to install the service" >&2
	exit 1
fi
systemctl daemon-reload

cat <<EOF
Installed $binary_destination.
Installed $service_destination.

Configure the agent as $service_user, then enable it:
  sudo -u $service_user $binary_destination configure --url LOREHUB_URL --lores-url ADVERTISED_LORES_URL --config-dir $config_directory
  systemctl enable --now lorehub-lores-agent.service
EOF
