# Hetzner single-node preset

この preset は、Ubuntu 24.04 の Hetzner Cloud server 1 台、ext4 の 10 GB volume 1 台、SSH key resource 1 個、
firewall 1 個を作成します。Postgres、Keycloak、LoreHub API、web、Lore Server、runner は同じ server 上の Compose
stack で動作します。Lore Server のデータは接続した volume を `/var/lib/lorehub` に mount します。

小規模な公開テスト用の構成です。高可用構成ではありません。Postgres とその他の Compose named volume は server
の primary disk に残ります。接続 volume は Lore data の容量を制限しますが、backup にはなりません。

## 必要なもの

- Terraform 1.6 以上
- Hetzner Cloud project と環境変数 `HCLOUD_TOKEN`
- SSH public key と管理者 CIDR range
- 公開 DNS name と Keycloak の email verification 用 SMTP relay
- 手動設定の節に記載した production TLS file

HCloud provider は環境変数 `HCLOUD_TOKEN` を読みます。Terraform state には生成した cloud-init data が含まれるため、
state と `terraform.tfvars` を保護してください。

## ingress mode

- 直接公開: `enable_public_ingress = true` にして tunnel token を空にします。firewall が TCP 80 と 443 を
  許可します。cloud-init が web と Keycloak 用の Nginx を設定します。
- Cloudflare Tunnel: `cloudflared_tunnel_token` に空でない token を設定します。cloud-init が `cloudflared` と
  systemd service を設定します。Nginx は Tunnel 用に local HTTP で待ち受けます。`enable_public_ingress` が
  true でも firewall の 80 と 443 は閉じます。
- SSH のみ: 両方を無効にします。firewall は SSH だけを許可します。server が公開する Compose port には
  SSH port forwarding を使います。

firewall は Compose が公開するその他の port を開けません。外部 Lore client が必要な場合は、必要な Lore client と
policy の network access を別途設定してください。この preset は DNS record や Cloudflare Tunnel route を作成しません。

## preset の適用

1. example の変数ファイルをコピーします。

   ```bash
   cp terraform.tfvars.example terraform.tfvars
   ```

2. `ssh_public_key`、`admin_cidrs`、`root_domain`、`public_origin_url`、Keycloak SMTP の値を設定します。最初の
   account が決まっている場合は `instance_admin_usernames` に設定します。ingress mode を 1 つ選びます。

3. Hetzner token を export して Terraform を初期化します。

   ```bash
   export HCLOUD_TOKEN='replace-with-a-hetzner-token'
   terraform init
   terraform validate
   ```

4. plan を確認して resource を作成します。

   ```bash
   terraform plan
   terraform apply
   ```

5. `server_ipv4`、`server_ipv6`、`volume_device_path`、`ssh_command`、`url_summary` の output を保存します。

server の user-data は接続した volume が現れるまで待ち、新しい volume なら ext4 に format し、`/etc/fstab` に
書き込みます。`/var/lib/lorehub` が mount されていない場合は stack を起動しません。systemd unit が boot 時に Compose
command を実行し、shutdown 時に Compose stack を停止します。

## apply 後に行う作業

1. 選んだ公開 name の DNS record を作成します。直接公開では web name と `auth.<root_domain>` を server の IPv4、
   IPv6 に向けます。Tunnel mode では token が指定する Cloudflare Tunnel を、server 上の `http://127.0.0.1:80` に
   両方の name を送るよう設定します。Terraform は record を作成も確認もできません。

2. production TLS file を `tls_source_dir` に置きます。以下の command は default の
   `/etc/lorehub/tls-source` を使います。`tls_source_dir` を変更した場合は、その path に置き換えてください。この directory には、空でない `ca.crt`、`ca.key`、
   `server.crt`、`server.key`、`lore-client.crt`、`lore-client.key` が必要です。server certificate は、
   `internal_domain` と `root_domain` から作る `LOREHUB_TLS_SERVER_NAMES` の name を含む必要があります。値を
   override した場合はその一覧を使います。default の SAN list には public origin、Lore root、Keycloak、Lore、internal API の
   host が入ります。`infra/.local-tls` の local development file は production 用ではありません。file をコピーした後に
   LoreHub service を restart し、`tls-init` に共有 TLS volume を作成させます。直接公開では Nginx も restart されます。

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

3. stack の status を確認します。起動していなければ cloud-init または Compose の log を確認します。

   ```bash
   sudo systemctl status lorehub.service
   sudo journalctl -u lorehub.service -n 100 --no-pager
   sudo docker compose --env-file /opt/lorehub/.env -f /opt/lorehub/infra/compose.yaml \
     -f /opt/lorehub/compose.hetzner.yaml ps
   ```

4. Keycloak の初回 administrator 設定を完了します。直接公開または Tunnel mode では `url_summary` の Keycloak URL
   を開きます。SSH のみの場合は、先に Keycloak の公開 port を forward します。

   ```bash
   ssh -N -L 8280:127.0.0.1:8280 root@SERVER_IPV4
   ```

   初期 username は `KEYCLOAK_ADMIN_USERNAME` です。生成された password は `/opt/lorehub/.env` の
   `KEYCLOAK_ADMIN_PASSWORD` にあります。初回 login 後に Keycloak で password を変更します。

5. 最初の LoreHub account を作成します。apply 前に username を `instance_admin_usernames` に入れていなかった場合は、
   SSH で `/opt/lorehub/.env` の `LOREHUB_INSTANCE_ADMIN_USERNAMES` を編集し、`lorehub.service` を restart します。

6. `docs/operations/backup-and-recovery.md` に従って backup を設定します。Hetzner volume と server の primary disk
   だけでは database と object の recovery copy になりません。

## SSH のみで接続する場合

どちらの ingress mode も設定しない場合、HCloud は管理者 CIDR からの port 22 だけを許可します。stack には SSH port
forwarding でだけ接続できます。web、Keycloak、API の port は次のように forward できます。

```bash
ssh -N \
  -L 3000:127.0.0.1:3000 \
  -L 8080:127.0.0.1:8080 \
  -L 8280:127.0.0.1:8280 \
  root@SERVER_IPV4
```

production URL は、完全な production authentication flow のために、引き続き名前解決できて HTTPS を使える必要があります。
通常の browser access には、直接公開または Tunnel を設定してください。

## preset の変更

`terraform.tfvars` を変更して、もう一度 `terraform apply` を実行します。cloud-init の変更は、server resource に新しい
user-data が渡された場合だけ自動で適用されます。既存 server の環境変数だけを変更する場合は、`/opt/lorehub/.env` を
編集して `lorehub.service` を restart するか、backup を保持して server を意図的に置き換えます。
