# LoreHub認可境界の運用

この文書は、LoreHubを本番に置くときに、どの情報をどのサービスで守るかを説明します。LoreHubはGitの
サービスではありません。Loreのリポジトリ、branch、revision、ファイル、lockがデータの正本です。

## 管理面とデータ面

LoreHub PostgreSQLは管理面です。ユーザー、Keycloakとの対応、組織所属、チーム、リポジトリ役割、branch
方針、監査記録を保存します。Lore Serverはデータ面で、revisionとbranchの内容を保存します。PostgreSQLに
Loreのファイル本文やtreeを複製しません。

`repositories.lore_repository_id` がLoreのパーティションIDとの唯一の対応です。登録時に32文字の小文字
16進数であること、同じIDが二つのLoreHubリポジトリに対応しないことを確認します。`urc-`を付けた値が
UCS認証のresource IDです（例: `urc-0123456789abcdef0123456789abcdef`）。別のpartition列は追加しません。
この対応を変更すると、アクセス境界が変わるため、通常のリポジトリ編集として扱ってはいけません。

非公開リポジトリを読めない利用者には404を返します。ID、revision、content hashを知っていても、Lore Server
のJWT検証とresource scopeを通過しない限り、別パーティションのデータは読めません。

## 権限の判定

アクセスは次の条件をすべて満たす必要があります。

1. 利用者が有効な組織メンバーであること。
2. そのリポジトリへの有効な直接役割、または有効なチーム役割があること。組織の一般メンバーであるだけでは、
   非公開リポジトリを読めません。組織のownerとmaintainerは組織管理者として扱います。
3. 発行を求めたresource IDとpermissionが、現在のPostgreSQLの権限の範囲内であること。
4. リポジトリ方針、branch方針、操作の種類を満たすこと。

`read`と`triage`はLoreの`read`、`write`と`maintain`はLoreの`read`と`write`、`admin`はLoreの
`read`、`write`、`admin`になります。通常の`admin`に`obliterate`は含まれません。obliterateはリポジトリ
ownerが機能を有効にしたうえで、対象利用者へ明示的に付与する別の高リスク権限です。

権限変更は監査記録を残し、次に発行する短命tokenには反映されます。Loreの操作を扱う管理面の処理も毎回
PostgreSQLを再確認します。tokenには正確な`urc-{repository_id}`だけを入れ、`urc-*`は利用できません。

組織のメンバーとteamは`/api/v1/organizations/{organization}/members`と`/teams`で管理します。repositoryの
collaborator、方針、obliterate、Linksは`/api/v1/repositories/{owner}/{repository}`以下で管理します。これらの
変更はすべて監査記録と同じtransactionで保存されます。Lore repositoryを登録するAPIは既存のLore partitionを
確認して対応を保存する処理です。新しいpartitionを作るときは、先に組織とcanonical IDの対応を予約してから
Lore側を作成し、最後に登録APIでLoreの内容を確認します。対応がないままLore側だけを作らないでください。
登録画面では、Lore auth loginで得た現在のuser tokenをHTTPSのrequest headerで一度だけ送ります。LoreHubはこの
tokenを保存せず、URLや監査記録にも入れません。

## UCS認証とJWT

Lore 0.8.6の`epic_urc.UrcAuthApi`をそのまま実装しています。protoの出所と再生成方法は
`services/api/internal/loreauth/proto/README.md`にあります。通常のブラウザ認証は次の流れです。

1. Loreが高エントロピーのsession codeとclient stateを送ります。
2. 利用者をKeycloak／LoreHubのログイン画面へ送り、固定された確認画面で明示的に許可します。
3. Loreがclient stateとsession codeでpollします。sessionは短時間で失効し、poll間隔を制限し、完了時に一度だけ
   消費します。URLへtokenを載せず、ログや監査payloadにもtokenを保存しません。
4. PostgreSQLで現在の権限を確認し、必要なresourceだけを含むLore JWTを返します。

JWTはLore 0.8.6の`AuthorizationToken`に合わせ、`sub`、`iss`、`iat`、`exp`、`aud`、`env`、`name`、
`preferred_username`、`resources`、`is_service_account=false`、`idp`を使います。ユーザーtokenの有効期限は
5〜10分です。外部token交換とAPI key交換は、検証済みの仕組みを持たない現在は`Unimplemented`を返します。
成功したように見せる代替処理はありません。

### 0.8.6の環境URL互換性

APIが提供するUCS認証endpointの表記は`ucs-auth://api:8443`です。Loreのauth clientはこの表記をHTTPSへ変換して
接続します。一方、Lore 0.8.6の`[environment.endpoint].auth_url`は、UCS clientだけでなくRebac clientにも同じ
文字列のまま渡されます。公式Rebac clientは`https://`で始まる場合だけTLSを有効にするため、`ucs-auth://`を
この設定へ入れると、TLS endpointへ平文gRPCを送ってrepository作成が失敗します。

このため、このツリーの本番形Compose設定は、同じTLS UCS endpointを`https://api:8443`として広告しています。
これは平文や別の認証サービスではありません。stock Lore 0.8.6を使う限り、環境広告に`ucs-auth://`を厳密に要求する
場合は、Lore公式側でRebacのURL処理を修正したリリース、または公式に認められた設定拡張が必要です。LoreHub側で
平文接続の受け入れやプロトコル多重化を追加してこの差を隠してはいけません。

署名はRSAの非対称鍵です。現在の公開鍵と直前の公開鍵をJWKSで同時に公開できます。鍵を交代するときは、先に
新しい公開鍵をJWKSへ追加してから署名鍵を切り替え、旧tokenが期限切れになった後に直前鍵を外します。秘密鍵は
Secret/KMS連携または権限を絞ったファイルから渡し、リポジトリへ入れません。productionでは署名鍵、kid、TLS、
認証設定が欠けるとAPIが起動しません。

ファイル方式で旧鍵を残す場合は、`LOREHUB_AUTH_PREVIOUS_KEYS=old-kid=/run/secrets/old-public.pem`のように公開鍵
だけを指定します。現在鍵の秘密鍵は`LOREHUB_AUTH_SIGNING_KEY_PATH`またはSecret/KMS連携で渡し、環境変数やログへ
展開しません。切替後は旧tokenの期限（最大10分）を待ってから旧公開鍵を外します。

## Lore ServerとTLS

Lore Serverは`[server.auth]`、`jwt_issuer`、`jwt_audience`、`[server.auth.jwk]`を設定して起動します。
設定がない場合はJWT検証を無効にするLoreの既定動作になるため、productionでは起動を許可しません。公開QUIC
と公開gRPCは、TLS接続の後にLore JWTを検証します。issuer、audience、kid、期限、resource、permissionが不正な
tokenは拒否されます。

ローカルComposeは`tls-init`が実際のローカルCA、API/Lore用サーバー証明書、Lore hook用クライアント証明書を
作ります。APIのUCS gRPCは8443、内部の認可問い合わせはクライアント証明書必須の8444です。ホストのLore CLI
から接続する場合は、`infra/.local-tls/lorehub-local-ca.crt`をOSまたは使用するTLS trust storeへ明示的に追加します。
このCAや秘密鍵は開発用であり、本番へ移しません。

## protected branchとmerge

Lore 0.8.6の公式hook registryに、LoreHubの小さなhook moduleだけを注入した専用イメージを作ります。Loreの
ソース全体をこのリポジトリへコピーしたり、forkを保守したりしません。hookはBranchPush、BranchCreate、
BranchDelete、RepositoryCreate、Obliterateの前処理から、固定された8444の認可endpointへ約150ms以内に問い合
わせます。応答がない、証明書が不正、内容が不完全、または拒否応答の場合は失敗させます。

protected branchへの直接pushはhookで拒否します。mergeだけは、利用者、Lore repository、branch、expected base、
expected head、期限に結び付いた一度限りの認可を発行できます。LoreHubはその認可をハッシュで保存し、Lore hookの
問い合わせと同時に原子的に消費するため、別branchや別revisionへの使い回しはできません。

Lore 0.8.6の公式`HookContext`はBranchCreateに新しいbranch名を渡しません。このhookは名前を安全に解決できない
branch作成を失敗させます。BranchDeleteは既存branchの記録を解決できる場合だけ判定します。これはprotected branchを
名前の不明な操作で迂回しないためのfail-closed動作です。branch作成を安全に許可するには、Lore公式側で名前を渡す
hook APIが必要です。LoreHub側で推測した名前や別のwildcard権限を追加してはいけません。

Loreを更新するときは、まず公式releaseのcommitを確認し、proto、JWT verifier、auth client、hook API、environment
広告の差分を確認します。次にhookだけを同じregistry/build機構で注入し、直接push拒否、feature branch成功、方針
変更、endpoint停止時の拒否、正しい一度限りのmergeを実スタックで確認してからイメージを更新します。

## リポジトリ分離とLore Links

一つのLore partition内に、パスごとの秘密を表すACLはありません。パス単位で公開範囲を分けたい場合は、LoreHub
で別のLoreリポジトリ／partitionを作ります。

LoreHubのLinks画面は、sourceとtargetの両方の管理者権限と、両リポジトリのlink方針を確認して「宣言」を保存
します。ただしLore 0.8.6が実行するLinkの仕様が確定していないため、現在の状態は`declared_only`です。宣言だけで
targetの内容が読めることはなく、画面も完成したpath ACLとして表示しません。

## runnerと障害復旧

runnerは専用の有効な利用者IDを`LOREHUB_RUNNER_USER_ID`へ設定し、各Loreリポジトリにその利用者のread役割を
付与したうえで、リポジトリごとに5分tokenを発行します。通常のユーザーや全体を読める共有管理者identityを使い
ません。runner profileを使わない場合、この変数は空のままで構いません。productionでは`LOREHUB_LORE_IDENTITY`を
設定すると起動を拒否します。

PostgreSQLは定期バックアップと復元試験を行い、Loreのデータストア、Keycloak PostgreSQL、署名鍵、TLS秘密鍵を
別々の復旧対象として扱います。PostgreSQLだけを戻してもLoreの内容は戻りません。Loreを先に復旧し、そのcanonical
partition IDが登録値と一致することを確認してからLoreHub APIを公開します。署名鍵を失った場合は新しいkidで鍵を
追加し、全tokenを短い期限で切り替えます。CAを失った場合は新しいCAと証明書を発行し、Lore、API、hook、利用者の
trust storeを同じ停止計画で更新します。
