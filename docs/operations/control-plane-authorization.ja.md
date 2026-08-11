# LoreHubの認証と認可の運用

[English](control-plane-authorization.md) | [日本語](control-plane-authorization.ja.md)

本番環境の権限、token、鍵、TLS、復旧手順をまとめています。

## 管理面とデータ面

LoreHub PostgreSQLには、利用者、Keycloakとの対応、組織、チーム、役割、リポジトリ方針、監査記録、作成処理の
状態を保存します。Lore Serverにはrevision、branch、ファイル、lockを保存します。

`repositories.lore_repository_id`が、LoreHubのリポジトリとLoreパーティションを結ぶ唯一の対応です。この値は
小文字の32桁16進数で、同じ値を二つのLoreHubリポジトリへ登録できません。認証で使うresource IDは、この値へ
`urc-`を付けたものです。別の`lore_partition_id`列はありません。対応値を変更することは、名前変更ではなく、
アクセス境界の変更です。

一つのLoreパーティションの中にパス単位の秘密を表すACLはありません。パスごとに秘密を分ける必要がある場合は、
別のLoreリポジトリと別のパーティションを作ります。別パーティションのID、revision、content hashを知っていても、
そのresourceへの現在の権限がなければLoreのQUICとgRPCの両方が拒否します。

## 判定の順序と権限

非公開データへのアクセスは、利用者が有効であること、有効な組織所属があること、直接またはteamの有効な
リポジトリ役割があること、要求したtokenの範囲、リポジトリ方針、branch方針、操作のすべてを満たす必要があります。
ownerは、直接またはteam役割がなくても、その組織のリポジトリを管理できる明示的な例外です。ただしownerも有効な
利用者と有効な組織所属でなければなりません。組織maintainerは組織、team、メンバー設定を管理できますが、リポジトリ
データの読み書きや管理権限は自動では持ちません。

| LoreHub role | Lore permission    |
| ------------ | ------------------ |
| read         | read               |
| triage       | read               |
| write        | read, write        |
| maintain     | read, write        |
| admin        | read, write, admin |

通常のadminに`obliterate`は含まれません。これはリポジトリ方針が有効で、対象利用者へ別途付与されたときだけ使える
高リスク権限です。広い役割や組織の役割が、狭い要求tokenの範囲を広げることはありません。

公開リポジトリの匿名readは、専用の`anonymous_reader` service principalへ対象resourceのreadだけを付けて行います。
認証済み利用者が停止中または組織から外れている場合は、匿名として扱い直しません。非公開resourceは認可に失敗した
利用者へ404を返します。

役割、team、collaborator、方針、Link、obliterate、作成処理の変更は、監査記録とoutboxを同じPostgreSQL transaction
で保存します。どちらか一方だけが成功することはありません。

## UCS認証とtoken

Lore 0.8.6の`epic_urc.UrcAuthApi`を実装しています。protoは公式v0.8.6から生成し、出所と再生成手順は
`services/api/internal/loreauth/proto/README.md`に記録しています。

ブラウザ認証が完了すると、`GetAuthSession`は有効な利用者へ短命のbase authentication tokenを返します。このtokenの
`resources`は常に空で、リポジトリがない利用者にも同じように発行されます。ログイン時に全resourceを列挙しません。
base tokenは交換専用で、Loreのデータ面やresource権限の照会には使えません。`expires_at`はLore仕様どおりUnix
ミリ秒です。

Loreが要求するresourceを交換するときは、PostgreSQLの現在の直接役割、team役割、組織所属、状態を読み直します。
要求できるのは、正確な`urc-{32桁ID}`のresourceだけです。`urc-*`、全resourceを含む利用者token、wildcard service
identityは使いません。交換で発行されるtokenは5〜10分だけ有効です。

外部token交換とAPI key交換は、検証済みの連携がないためprotocolの`Unimplemented`を返します。token、session code、
認証URLの秘密値はlog、error、URL、監査内容、metric、traceへ出力しません。

実Loreで境界を確認する場合は、二つのpartitionのURLと、read、base、期限切れ、issuer違い、audience違い、kid違いの
tokenを環境変数へ渡し、PostgreSQLへ接続できる`DATABASE_URL`を設定して、リポジトリrootで
`./scripts/test-lore-auth-boundary.sh`を実行します。このscriptはtokenの値を表示せず、stock Lore SDKを使って
QUICとgRPCの両方を確認した後、teamの付与・取り消し、外部collaborator、protected branchへの直接push拒否、
一度だけ使えるmerge認可をPostgreSQL上で確認します。Lore 0.8.6のstock protocolにはteamやbranch policyの概念が
ないため、これらはControl Planeのpolicy testで検証します。別管理のLore Serverだけを確認する場合に限り、
`LOREHUB_SMOKE_LORE_ONLY=1`を指定できます。二つのLore repositoryと各tokenの準備は運用環境側で行います。

## URL、audience、鍵

issuer、audience、AuthURL、JWKS、Lore公開URLは、同じ管理対象root domainの名前を使います。例えば本番は次の構成です。

```text
issuer:   auth.lorehub.example
audience: lorehub.example
AuthURL:  ucs-auth://auth.lorehub.example:8443
JWKS:     https://lorehub.example/.well-known/jwks.json
Lore:     lores://lorehub.example:41337
```

ローカルは`lorehub.localhost`をrootにし、HTTPの認証endpoint、JWKS、確認画面を使えます。JWT issuerはURLではなく、
stock clientがremoteの許可ドメインとして扱う`auth.lorehub.localhost`です。LoreのUCS gRPC endpointは
`ucs-auth://auth.lorehub.localhost:8443`で、Lore 0.8.6のclientがHTTPSへ変換します。`lore`、`api`、Docker内部名を
public URLやaudienceには設定しません。hostのLore CLIが解決できるpublic AuthURLを広告し、Lore Serverの
バックエンド接続に`LOREHUB_LORE_INTERNAL_AUTH_URL`を使います。この接続はHTTPSのCAとSANを検証し、公開AuthURLと
token storeのキーは書き換えません。policy、JWKS、認証APIの接続先には
`LOREHUB_LORE_INTERNAL_DOMAIN`を使います。ローカルの既定値は`lorehub.internal`です。`.localhost`はHTTPクライアントが
loopbackへ固定して解決するため、コンテナ間通信には使いません。productionでは内部DNSでこの名前をAPIへ解決します。

CI runnerは`LOREHUB_LORE_INTERNAL_URL`を使い、公開Lore URLのpartitionを保ったまま内部authorityへ接続します。
`runner-data`では`lore.<root-domain>`をLore Serverへ解決します。Loreが広告する公開UCS AuthURLのポートは、APIの
`LOREHUB_LORE_AUTH_COMPAT_ADDRESS`でも同じTLSサービスを待ち受けます。DBに保存する公開URLは内部URLへ変更しません。

本番で認証endpoint、JWKS、確認画面がHTTPSでない、署名鍵、kid、TLS設定、JWT検証設定がない場合、APIは起動しません。ローカル
のHTTP設定は開発用profileだけに限定されます。

署名はRSAの非対称鍵です。JWKSには現在鍵と直前の公開鍵を同時に載せます。鍵を交代するときは、新しい公開鍵を先に
公開し、次にkidと秘密鍵を切り替え、旧tokenの最大10分の期限を待ってから旧公開鍵を外します。秘密鍵はSecret、KMS、
または権限を絞ったファイルで渡し、リポジトリへ保存しません。ローカルで秘密鍵がなければ専用volumeへ生成しますが、
本番での自動生成は行いません。

## TLSとLore Server

Lore Serverの`[server.auth]`、issuer、audience、`[server.auth.jwk]`は必須です。QUICとgRPCは同じissuer、audience、
JWKSの公開鍵、kid、期限、resource、permissionを検証します。欠落、期限切れ、別issuer、別audience、未知kid、別partition、
read tokenによるwriteは拒否します。

ローカルComposeの`tls-init`は、CA、Lore/API用サーバー証明書、hook用client証明書を実際に作ります。証明書のSANには
`lorehub.localhost`、`auth.lorehub.localhost`、`api.lorehub.localhost`、`lore.lorehub.localhost`、
`api.lorehub.internal`を含めます。hostのLore CLIで接続するときは、
`infra/.local-tls/lorehub-local-ca.crt`をTLS trust storeへ追加してください。これはローカル用で、本番のCAや秘密鍵として
使いません。

hookからLoreHubのpolicy endpointへは、設定したmanaged root配下の
`https://<policy-host>:8444/internal/lore/policy`を使い、相互TLSと1秒のtimeoutを適用します。timeoutは100msから5秒の
範囲に制限します。観測endpointも同じroot配下の固定パスにします。hookのclient証明書は専用の`lore-policy-hook`
identityとclientAuth用途を持たなければなりません。接続失敗、証明書不正、SAN不一致、形式不正、拒否応答はすべて
拒否にします。本番ではendpoint、root、JWKS、
AuthURL、TLS CA、client証明書、client鍵を省略できず、サービス証明書とhook証明書を共有しません。

## protected branchとmerge

Loreイメージは公式v0.8.6を浅くcloneしてビルドし、公式hook registryへLoreHubのhook moduleを登録します。Lore 0.8.6は
BranchCreateのhook contextへbranch名を渡さないため、二つのhandlerへbranch名metadataを追加します。また、利用者へ広告する
AuthURLとLore Serverが接続するAuthURLを分ける設定を追加します。変更は二つのpatchに限定し、Loreのソース全体をこの
リポジトリへコピーしません。更新時は公式tagの変更で`HookContext`、JWT検証、UCS client、environment広告、hook registryを
確認し、patchの必要性を再評価します。

hookが使う`HookContext`はrepository、user、branch ID、branch名、proposed revision、client_ip metadataです。現在の
revisionはbranch IDをキーにPostgreSQLの観測状態から解決します。観測がない、2分より古い、または状態が不足するpushと
deleteは拒否します。BranchCreateは受け取った名前を既存のbranch ruleと照合し、直接pushを禁止した名前の作成を拒否します。
成功したBranchPushはrevisionを更新し、BranchDeleteは状態を削除します。hookのpost観測を失った場合は、
専用observer service principalによる定期pollerがLoreのbranch一覧を読み、状態を補正します。

protected branchへの直接pushは拒否します。merge workerが正確な提案revisionを作った後、内部mTLS endpointへ一度だけ
準備を依頼します。DBには利用者、repository、target branch IDと名前、期待する現在revision、正確な提案revision、source
revision、期限、消費状態を保存します。merge workerはsource revisionを確認してから登録します。Lore 0.8.6のhook
contextにはsource revisionがないため、hookは受け取れる利用者、repository、branch、現在revision、提案revisionをDBの
tupleと原子的に照合し、source revisionが登録済みで期限内であることも確認して消費します。別tuple、期限切れ、再利用は拒否し、
利用者へ返すbearer secretやdigestはありません。

## リポジトリ作成とLinks

新規作成ではLoreHubが32桁IDを生成し、pending repository、方針、counter、監査、outbox、
provisioning状態を一つのtransactionで保存します。その後、実行者本人へ対象IDだけの正確なadmin resource tokenを短時間発行し、
そのIDを指定してstock LoreのRepositoryCreateを呼びます。冪等確認と再試行の内部処理には、別に対象IDだけの
`lorehub-provisioner` service principal tokenを発行します。成功後だけactiveへ変更します。失敗はfailedと理由を保存し、
同じpending IDを使ってretryとreconcileを行います。public URLは`lores://`で保存し、内部authorityへの書き換えは接続時だけです。

既存Lore repositoryのimportでは、現在の利用者の正確なadmin resource tokenを要求し、Loreのrepository情報と
Lore repository IDを確認します。新規作成とは別のendpointと認可処理を使います。

Linksはsourceとtarget双方の管理者権限と方針を確認して`declared`として保存します。Lore 0.8.6で実際のLink適用が観測される
までは`active`にしません。宣言だけでtargetのデータを読めることはなく、path ACLを提供する画面でもありません。

## service principal、runner、障害復旧

通常のWeb、merge、CI checkout、公開read、branch observer、provisioningは、対象principal、対象partition、必要最小限の
permissionだけを持つ短命tokenを毎回発行します。service principalには`is_service_account=true`を設定し、DBの監査対象に
します。service principalはanonymous reader、CI runner、observer、provisionerに分離し、全repositoryの権限を一つにまとめません。
通常経路で`LOREHUB_LORE_IDENTITY`は使いません。legacy identityは`local-insecure`かつAPI認証disabledのprofileでだけ許可し、
本番設定では起動を拒否します。

Composeのrunnerも`LOREHUB_ENV`を継承します。runnerはブラウザや外部APIクライアントを認証しないため、本番でも
`LOREHUB_RUNNER_AUTH_MODE=disabled`を使用できます。LoreとCIの認証にはmanaged root domain、Lore署名鍵、JWKS、
AuthURL、TLS、Actions暗号鍵、CI service principal、PostgreSQLの本番値が必要です。設定が
足りないrunnerは起動せず、CI checkoutも専用service principalの短命tokenを発行できない限り失敗します。

復旧時は、Lore data store、LoreHub PostgreSQL、Keycloak PostgreSQL、署名鍵、TLS秘密鍵を別々に復元します。Loreの各
repository IDと`repositories.lore_repository_id`が一致すること、active/failedの
provisioning状態、監査とoutboxを確認してからAPIを公開します。署名鍵を失った場合は新しいkidで鍵を発行して旧tokenを短く
失効させます。CAを失った場合は新しいCAと証明書を作り、Lore、API、hook、利用者のtrust storeを同じ停止計画で更新します。
