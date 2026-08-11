# ADR 0001: LoreHubの基本構成

[English](0001-platform-architecture.md) | [日本語](0001-platform-architecture.ja.md)

- 状態: 採用
- 日付: 2026-08-09

## 背景

LoreHubはLoreリポジトリの閲覧、Issue、review、アクセス管理、監査記録、CIを提供します。Loreはversion 1.0未満で、
APIや保存形式が変わる可能性があります。連携には公開C APIと公式Go SDKを使います。共同開発の流れと画面構成は
GitHub、GitLab、Giteaを参考にします。

## 決定

### 実行するcomponent

- Next.jsで`src/app`のWebアプリケーションを実装します。
- GoでAPI、認可、PostgreSQL更新、非同期処理、CIの制御を実装します。
- Lore Serverにrevision、branch、file、lockを保存します。
- PostgreSQLにuser、organization、permission、Issue、review、CIの状態、監査記録を保存します。
- object storageにCI logとartifactを保存し、PostgreSQLに保管先と状態を保存します。

最初の構成では、`serve`、`migrate`、`runner` commandを一つのGo binaryに含め、別々のprocessとして実行します。
機能ごとにpackageを分け、負荷や障害の影響が大きくなったcomponentは後から別serviceへ分離できるようにします。

### Loreとの連携

Go backendは公式の[Lore Go SDK](https://github.com/EpicGames/lore-go)からLoreへ接続します。SDK固有のtypeは
`internal/lore`内に置き、Webや共同開発機能から参照しません。

読み取り処理ではbare cloneの共有cacheを使い、必要なrevision treeだけを取得します。CIは指定されたrevisionを
一時workspaceへcloneしてから実行します。

ブラウザsessionとLore credentialは、有効期間と適用範囲を分けます。APIはuser session、permission、CSRF tokenを
確認してから、repository partitionと操作に対応するcredentialを取得します。本番credentialには、PostgreSQLの
現在のpermissionを使って発行した短命tokenを使います。tokenにはsubject、partition、scope、Lore Auth URLを含めます。
署名鍵はsecret manager、KMS、またはアクセスを制限したfileから読み込みます。本番用の認証設定が不足している場合は、
起動を止めるか操作を拒否します。認証情報のfallbackは、明示した開発用・test fixtureだけで使用できます。

ブラウザからのログインにはOIDC Authorization Code FlowとPKCEを使います。APIはissuerとaudienceを検証し、OIDC tokenを
期限付きのサーバー側sessionに保存します。ブラウザにはsession cookieだけを送ります。Bearer API clientも利用できます。

### データ更新

複数のtableを更新する処理はPostgreSQL transaction内で実行します。外部通知は同じtransactionでoutboxへ記録し、
workerが再送します。外部event IDまたは業務上のunique constraintで重複送信を防ぎます。

pull requestには、作成時と最終確認時の対象branch revisionを保存します。merge直前にLoreへ問い合わせ、対象revisionが
変わっていた場合はreviewとCIを再評価します。Loreのbranch mergeが成功してからpull requestをmerge済みに更新します。

### GitHub Actions互換CI

`.github/workflows/*.yml`と`.yaml`からworkflowを読み込み、対応しているjobを
[nektos/act](https://github.com/nektos/act)で実行します。LoreHubは次の処理を担当します。

1. Loreのbranch push eventを受け取り、一度だけqueueへ登録します。
2. 指定されたLore revisionを隔離したworkspaceへcloneします。
3. GitHub Actions互換のevent payload、環境変数、短命job tokenを作成します。
4. 非特権の隔離環境で`act`を実行し、状態、log、artifactを保存します。

Lore hookが通知した最新revisionと、workflow catalogを読み込んだrevisionは別々に保存します。pollingでbranchを確認する
前にhook eventが届いた場合も、workflowの検出とpush runの登録を行います。

Actionsのvariableとsecretはorganization、repository、environmentのscopeに対応します。PostgreSQLにはvariableと
AES-256-GCMで暗号化したsecretを保存します。runnerは有効なCI service principalとrepository grantを確認した場合だけ
secretを復号します。`GITHUB_TOKEN`にはjob、run、attempt、repository、有効なleaseに限定した短命RS256 JWTを使います。
SARIF uploadなどの内部APIでも同じ条件を確認します。

`act`とGitHub-hosted runnerには違いがあります。Windows・macOS runner、GitHub固有API、未対応のworkflow構文は
利用できない機能として明記します。GitHubが公開しているworkflow例をcompatibility testで実行し、差分を検出します。

本番runnerはtrust domainごとの専用・短命な基盤で実行し、APIとhostを共有しません。Composeのrunner profileは、
`docker:29.4.0-dind-rootless`へport 2376のmTLSで接続します。internal networkの`runner-data`、`runner-control`、
`runner-egress`でAPI、runner、engine、job container、外向きproxyを分離します。job containerは使い捨てproxy gatewayを
利用し、外部へ直接接続できません。

engineのpermissionとjobのCPU、memory、PID、capability、namespace制限は別々に設定します。Docker Desktopではjob単位の
cgroup制限をすべて適用できないため、本番では専用の使い捨てnodeまたはpodと、gVisorやKata Containersなどの検証済み
隔離環境を使います。本番でLoreを読み取るときは、service subject、repository partition、read scopeに限定した短命
credentialが必要です。

### 画面と翻訳

URLの先頭にlocaleを置きます。componentは表示文をlocale dictionaryから取得します。英語と日本語のdictionaryは同じ
keyを持ちます。情報の配置はGitHubとGiteaを参考にし、`revision`、`branch latest`、`working tree`などLoreの用語を
そのまま使います。

componentは役割ごとに分割します。formatとlintで1行120文字以内、1file 1000行未満を必須にします。

## 採用しなかった案

### Next.jsだけでLoreへ接続する

native library、長時間処理、接続cache、CI workerがWeb processに集まるため採用しません。

### LoreのfileとrevisionをPostgreSQLへ保存する

大容量file、重複排除、部分取得などLoreの機能を使えなくなり、同じrepository dataを独立して更新できる場所が
二つになるため採用しません。

## 運用への影響

Lore Go SDKと対応するnative libraryは同時にreleaseします。Loreを更新するときはadapterのintegration testを
deployment前に実行します。Lore Server storageとLoreHub PostgreSQLは、backupと復旧を別々に行います。
