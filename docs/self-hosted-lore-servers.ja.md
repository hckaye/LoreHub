# セルフホステッド Lore Server

[English](self-hosted-lore-servers.md) | [日本語](self-hosted-lore-servers.ja.md)

セルフホステッド Lore Serverは1つのOrganizationに属します。LoreHubは広告する`lores://` URLを保存し、登録と
ハートビートAPI用のクレデンシャルを発行します。

## 登録の流れ

Organization ownerがOrganization設定からLore Server登録tokenを作成します。tokenは`lhsr_`で始まり、一度だけ
消費できます。

Lore Serverのhostで`configure`を実行します。

```bash
sudo -u lorehub-lores env \
  LOREHUB_LORE_VERSION=0.8.6 \
  LOREHUB_HOOK_MODULE_VERSION=1.0.0 \
  /usr/local/bin/lorehub-lores-agent configure \
  --url https://lorehub.example \
  --lores-url lores://lore.example:41337 \
  --name lore-storage-1 \
  --config-dir /var/lib/lorehub-lores-agent \
  < /path/to/lhsr-registration-token
```

コマンドはserver name、広告URL、Lore build version、hook module versionを
`POST /api/v1/lore-servers/register`へ送ります。APIは`lhss_`で始まるクレデンシャルとserver IDを返します。
agentはこれらを`config.json`へ保存します。設定fileのpermissionは`0600`、親directoryのpermissionは`0700`です。

tokenは既定で標準入力から読みます。自動化用に`--token`も使えますが、command line引数はprocess listに表示される
ことがあるためwarningを表示します。登録tokenは保存しません。

agentの`configure`と`run`は`--lore-version`と`--hook-module-version`を受け付けます。対応する環境変数は
`LOREHUB_LORE_VERSION`と`LOREHUB_HOOK_MODULE_VERSION`です。既定値は`0.8.6`と`1.0.0`です。Lore buildまたは
hook moduleが別の対応versionの場合は上書きします。

## hook用client証明書

登録後に最初のclient証明書を取得します。

```bash
sudo -u lorehub-lores /usr/local/bin/lorehub-lores-agent renew-certificate \
  --config-dir /var/lib/lorehub-lores-agent
```

このコマンドは保存済みの`lhss_`クレデンシャルで認証し、`POST /api/v1/lore-servers/certificate`を呼びます。
設定directoryに`hook-client.crt`と`hook-client.key`をpermission `0600`で保存します。証明書の有効期間は30日で、
CommonNameは`lore-server-<server-id>`です。LoreHubが保存するのはserial number、発行日時、有効期限だけです。

Loreのhook設定では、生成されたfileをclient証明書とclient鍵に指定します。CA fileはLoreHubのpolicy endpointが使う
CAを信頼する必要があります。

```toml
[hooks.lorehub_policy]
ca_certificate = "/etc/lorehub/lorehub-ca.crt"
client_certificate = "/var/lib/lorehub-lores-agent/hook-client.crt"
client_key = "/var/lib/lorehub-lores-agent/hook-client.key"
```

fileのpermissionを広げずにLore processから読めるようにします。既定のpathを使う場合は、Loreを`lorehub-lores`
accountで実行します。hookはLoreの起動時に証明書と鍵を読むため、`renew-certificate`を手動で実行した後はLoreを
再起動します。

agentはfileがない場合と、証明書の残り期間が7日以下の場合に新しい証明書を取得します。各ハートビートの前に期限を
確認します。実行中のLoreがTLS fileを再読込しないversionの場合は、更新成功のlogを確認した後にLoreをreloadまたは
再起動します。

LoreHubはpolicyとobservationのrequestごとにserverの登録状態とリポジトリの割り当てを確認します。server証明書を
別のserverへ割り当てたリポジトリには使えません。serverを失効させると、発行済みの証明書も直ちに拒否されます。

## agentのinstallと起動

コマンドをbuildし、binaryとsystemd unitをinstallします。

```bash
go build -o /tmp/lorehub-lores-agent ./services/api/cmd/lorehub-lores-agent
sudo ./scripts/lores-agent/install.sh --binary /tmp/lorehub-lores-agent
```

installerは`lorehub-lores` system user、`/var/lib/lorehub-lores-agent`、
`lorehub-lores-agent.service`を作成します。unitは`/var/lib/lorehub-lores-agent`を設定directoryとして使います。

systemdにversion値を渡す必要がある場合は、`/etc/lorehub/lorehub-lores-agent.env`を作成します。

```text
LOREHUB_LORE_VERSION=0.8.6
LOREHUB_HOOK_MODULE_VERSION=1.0.0
```

serverを登録してからunitを起動します。

```bash
sudo -u lorehub-lores /usr/local/bin/lorehub-lores-agent configure \
  --url https://lorehub.example \
  --lores-url lores://lore.example:41337 \
  --config-dir /var/lib/lorehub-lores-agent
sudo systemctl enable --now lorehub-lores-agent.service
```

`run`はhook証明書がなければ取得してから最初のハートビートを送り、その後は既定で60秒ごとに送ります。間隔は
`--interval 30s`のように変更できます。各requestにはbuild version、process ID、起動時刻、`healthMetadata`内の
uptimeを含めます。

LoreHubが401を返すと、serverが失効した場合も含めて、agentは認証エラーを表示して終了します。それ以外の
ハートビート失敗はlogに記録して再試行します。SIGTERMを受けるとエラーにせずloopを終了します。

ハートビートが示すのはagent hostが動作していることです。広告したLore endpointへ到達できることは示しません。
APIはprovisioning前とserver healthの判定時にendpointへ別途接続します。

## リポジトリで使うServerの選択

リポジトリ作成では次の順にServerを選びます。

1. リポジトリで明示したserver ID
2. Organizationの既定Lore Server
3. instance Lore Server

Organizationに登録したserverは、`hosted_lore_server` entitlementがなくても選択できます。instance serverは別の
扱いです。明示的に選ぶ場合も、Organizationのserverがなくて最後に選ばれる場合も、featureが
`hosted_lore_server`である有効なOrganization entitlementが必要です。

このentitlementがない場合、リポジトリ作成はOrganization Lore Serverの登録またはentitlementの取得を案内して失敗します。
既存Organizationにはmigration grantが付与されるため、schema upgrade後も既存の動作は変わりません。

リポジトリのimportにも登録済みserver IDが必要です。importする`lores://` URLのauthorityは、登録済みserverのauthorityと
一致しなければなりません。
