# Hetzner single-node preset

This preset creates one Ubuntu 24.04 Hetzner Cloud server, one SSH key resource, and one firewall. The Compose stack
places Postgres, Keycloak, LoreHub API, web, Lore Server, and the runner services on the same server.

Application data lives in Docker named volumes on the server's included disk. Lore Server files are bind-mounted from
`/var/lib/lorehub/lore-data` on that disk. App redeploys keep those volumes. Replacing or destroying the server is the
operation that deletes the data. The server resource ignores user-data and image changes, and it sets
`prevent_destroy`, so a normal apply will not recreate the host.

An extra Hetzner Cloud volume is off by default. Set `enable_lore_data_volume = true` only when a separate disk is
needed. Extra volume storage is about €0.057 per GB per month, so the default 10 GB volume is about €0.57 per month.
Treat those figures as approximate. When the volume is enabled, it is mounted at `/var/lib/lorehub` and the Compose
bind for `lore-data` stays the same.

The preset is for a small public test deployment. It is not highly available. The server disk is not a backup.

If a previous apply already created a volume, set `enable_lore_data_volume = true` before the next apply so Terraform
does not delete it.

## Requirements

- Terraform 1.6 or newer
- A Hetzner Cloud project and `HCLOUD_TOKEN` in the environment
- An SSH public key and administrator CIDR ranges
- A public DNS name and an SMTP relay for Keycloak email verification
- Production TLS files listed in the manual setup section

The HCloud provider reads `HCLOUD_TOKEN` from the environment. Terraform state contains the rendered cloud-init data,
so protect the state and the `terraform.tfvars` file.

## Ingress modes

- Direct public ingress: set `enable_public_ingress = true` and keep the tunnel token empty. The firewall allows
  TCP 80 and 443. cloud-init installs Nginx for the web and Keycloak hostnames.
- Cloudflare Tunnel: set `cloudflared_tunnel_token` to a non-empty token. cloud-init installs `cloudflared` and a
  systemd service. Nginx listens on local HTTP for the tunnel. The firewall keeps 80 and 443 closed, even if
  `enable_public_ingress` is true.
- SSH only: keep both settings disabled. The firewall allows SSH only. Use SSH port forwarding for the
  host-published Compose ports.

The firewall does not expose the other Compose-published ports. Configure the required Lore client and policy network
access separately if the deployment needs external Lore clients. The preset does not create DNS records or a Cloudflare
Tunnel route.

## Apply the preset

1. Copy the example variables file.

   ```bash
   cp terraform.tfvars.example terraform.tfvars
   ```

2. Set `ssh_public_key`, `admin_cidrs`, `root_domain`, `public_origin_url`, and the Keycloak SMTP values. Set
   `instance_admin_usernames` if the initial account is known. Choose one ingress mode. Leave
   `enable_lore_data_volume` false unless a paid extra volume is required.

3. Export the Hetzner token and initialize Terraform.

   ```bash
   export HCLOUD_TOKEN='replace-with-a-hetzner-token'
   terraform init
   terraform validate
   ```

4. Review the plan and create the resources.

   ```bash
   terraform plan
   terraform apply
   ```

5. Save the `server_ipv4`, `server_ipv6`, `ssh_command`, and `url_summary` outputs. Save `volume_device_path` when
   the extra volume is enabled.

When `enable_lore_data_volume` is true, user-data waits for the attached volume, formats a new volume as ext4 when
necessary, writes `/etc/fstab`, and refuses to start the stack unless `/var/lib/lorehub` is mounted. When the extra
volume is disabled, `/var/lib/lorehub/lore-data` is a directory on the server disk. A systemd unit runs the Compose
command on boot and stops the Compose stack during shutdown.

## Redeploy the application

From this directory, after apply:

```bash
./redeploy.sh
./redeploy.sh main
```

The helper reads the server IP from `terraform output` and runs `lorehub-redeploy [git-ref]` over SSH. On the server
the script fetches `/opt/lorehub`, checks out the ref (default: the Git ref configured at first boot), reapplies the
production env overrides, and restarts `lorehub.service`. That restart runs `docker compose up --detach --build`.
Named volumes stay on the server disk.

Use this for application updates. Do not replace the server to ship a new Git ref.

## Manual setup after apply

1. Create DNS records for the chosen public names. In direct mode, point the web name and `auth.<root_domain>` to the
   server IPv4 and IPv6 addresses. In Tunnel mode, configure the Cloudflare Tunnel named by the token to send both
   names to `http://127.0.0.1:80` on the server. Terraform does not create or verify these records.

2. Place the production TLS files in `tls_source_dir`. The commands below use the default
   `/etc/lorehub/tls-source`; replace that path when `tls_source_dir` is different. The directory must contain
   non-empty `ca.crt`, `ca.key`,
   `server.crt`, `server.key`, `lore-client.crt`, and `lore-client.key`. The server certificate must cover the names in
   `LOREHUB_TLS_SERVER_NAMES`, which are derived from `root_domain` and `internal_domain` unless overridden. The
   local development files under `infra/.local-tls` are not production material. The default SAN list includes the
   public origin host, the Lore root host, the Keycloak host, the Lore host, and the internal API host.
   Restart the LoreHub service after
   copying the files so `tls-init` can create the shared TLS volume. Direct mode also restarts Nginx.

   ```bash
   sudo install -d -m 0700 /etc/lorehub/tls-source
   sudo install -m 0600 /path/to/ca.key /etc/lorehub/tls-source/ca.key
   sudo install -m 0644 /path/to/ca.crt /etc/lorehub/tls-source/ca.crt
   sudo install -m 0600 /path/to/server.key /etc/lorehub/tls-source/server.key
   sudo install -m 0644 /path/to/server.crt /etc/lorehub/tls-source/server.crt
   sudo install -m 0600 /path/to/lore-client.key /etc/lorehub/tls-source/lore-client.key
   sudo install -m 0644 /path/to/lore-client.crt /etc/lorehub/tls-source/lore-client.crt
   sudo systemctl restart lorehub.service
   ```

3. Confirm the stack status and inspect cloud-init or Compose logs if it is not running.

   ```bash
   sudo systemctl status lorehub.service
   sudo journalctl -u lorehub.service -n 100 --no-pager
   sudo docker compose --env-file /opt/lorehub/.env -f /opt/lorehub/infra/compose.yaml \
     -f /opt/lorehub/compose.hetzner.yaml ps
   ```

4. Complete the Keycloak initial administrator setup. In direct or Tunnel mode, open the Keycloak administration URL
   from `url_summary`. In SSH-only mode, forward the published Keycloak port first.

   ```bash
   ssh -N -L 8280:127.0.0.1:8280 root@SERVER_IPV4
   ```

   The initial username is `KEYCLOAK_ADMIN_USERNAME` and the generated password is in
   `/opt/lorehub/.env` as `KEYCLOAK_ADMIN_PASSWORD`. Change that password in Keycloak after the first login.

5. Create the first LoreHub account. If its username was not in `instance_admin_usernames` before apply, edit
   `LOREHUB_INSTANCE_ADMIN_USERNAMES` in `/opt/lorehub/.env` over SSH and restart `lorehub.service`.

6. Set up backups by following `docs/operations/backup-and-recovery.md`. The server disk, and an extra Hetzner volume
   if one is attached, do not replace database and object recovery copies.

## SSH-only access

When neither ingress mode is configured, HCloud permits only the administrator CIDRs on port 22. The stack is reachable
only through SSH port forwarding. For example, the web, Keycloak, and API ports can be forwarded as follows:

```bash
ssh -N \
  -L 3000:127.0.0.1:3000 \
  -L 8080:127.0.0.1:8080 \
  -L 8280:127.0.0.1:8280 \
  root@SERVER_IPV4
```

The production URLs still need to resolve and use HTTPS for the full production authentication flow. Use a configured
direct ingress or Tunnel for normal browser access.

## Change the preset

Change values in `terraform.tfvars` and run `terraform apply` again. The server ignores `user_data` and `image`
changes, because cloud-init runs only on first boot and replacing the server would destroy data. For application
updates, run `./redeploy.sh`. For environment-only changes on an existing server, edit `/opt/lorehub/.env` and
restart `lorehub.service`.

`prevent_destroy` is hardcoded on the server resource. Terraform cannot set it from a variable. To destroy the
server, set `prevent_destroy` to `false` in `infra/terraform/modules/hetzner-server/main.tf`, then run
`terraform destroy`.
