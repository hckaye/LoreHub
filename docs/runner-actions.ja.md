# Actions runnerの運用

[English](runner-actions.md) | [日本語](runner-actions.ja.md)

LoreHub Actionsは`act`をworkflow engineとして使います。runnerは指定されたLore revisionをcloneし、そのrevisionの
`.github/workflows/*.yml`と`.yaml`だけを検出します。対応しているtriggerとruntime定義を検証し、保存済みevent JSONと
一つのworkflow fileを`act`へ渡します。workflow内の`actions/checkout`はそのまま記述できます。runner adapterが
準備済みのLore workspaceをremote jobへ配置します。

## Job scheduling

YAMLのjobごとに、正規化した`runs-on` labelを持つqueue項目を作成します。一つのworkflowでmanaged jobと
self-hosted jobを併用できます。runnerは自分の実行先に一致するjobだけを取得し、`act --job`にjob名を指定して
実行します。managed runnerのentitlementはmanaged jobごとにrunの登録時に確認します。entitlementがない場合、runは
`failure_reason=entitlement_required`で失敗し、queueに残る他のjobをcancelします。

`needs`を指定したjobは、指定したすべての依存jobが成功すると取得可能になります。依存jobが失敗、timeout、cancelの
いずれかで終了した場合、後続jobはskippedになります。skippedを含むすべてのjobが終了するとrunも終了します。

artifactはrunに紐付けて保存し、既存のartifact download APIから取得できます。各jobは別のworkspaceと`act`用artifact
directoryを使います。依存関係の処理では、後続jobへfileやjob outputをコピーしません。後続jobでfileが必要な場合は、
保存済みartifactを明示的にdownloadしてください。別々の`act`実行間でjob outputは引き継がれません。

## Trust boundary

Composeの既定profileはrunnerを起動せず、`/var/run/docker.sock`をmountしません。`runner` profileは
`docker:29.4.0-dind-rootless` imageを使います。engineだけがこのCompose構成の特権境界です。job containerには
`--privileged=false`、CPU 1個、memory 1 GiB、PID 256、capability削除、`no-new-privileges`を設定します。

engineはport 2376のDocker mTLSだけを公開します。runnerはclient証明書directoryをread-onlyで受け取り、engine clientと
`act`に`DOCKER_HOST=tcp://runner-engine:2376`、`DOCKER_TLS_VERIFY=1`、
`DOCKER_CERT_PATH=/etc/lorehub/docker-client`を使います。job containerにはこれらの環境変数やfileを渡しません。
job optionからdaemon credential、host mount、device、capability、host namespaceを追加することもできません。

networkは用途ごとに分けます。

- `runner-data`はinternal networkで、runner、PostgreSQL、Lore、APIが接続します。
- `runner-control`はinternal networkで、runnerとengineだけが接続します。
- `runner-egress`はinternal networkで、engineとforward proxyだけが接続します。
- `runner-action`はinternal networkで、runnerとaction download用forward proxyを接続します。
- `runner-uplink`はproxyだけが接続する外向きnetworkです。

APIとWebは`runner-control`に接続しません。engineは`runner-data`に接続せず、runnerは外向きnetworkを持ちません。
engineはproxyを通してimageを取得します。各jobには使い捨てのinternal networkを作ります。小さなHAProxy gateway
sidecarをjob networkと使い捨てengine uplinkへ接続し、外側のSquid IPだけへ転送します。jobはbridgeへ接続せず、
内部service名も渡しません。jobのHTTP・HTTPS通信はSquidを通ります。gatewayには固定した
`haproxy:3.2.4-alpine` imageを使います。

Squidには`ubuntu/squid:7.2-26.04_edge` tagを使います。安全なHTTP・HTTPS portと443へのCONNECTだけを許可します。
宛先ACLはloopback、RFC1918、link-local、CGNAT、文書・test用range、multicast・reserved range、IPv6 private・
link-local rangeを拒否します。hostnameの解決後にもACLを適用するため、private addressへ解決されるpublic hostnameも
拒否します。runner-action artifact endpointの`172.28.244.2:34567`だけは、runner networkからのartifact upload・
download用に許可します。

Docker Desktopではcgroup制限が適用されない場合があります。その場合、ComposeのCPU、memory、PID設定は外側のengine
container全体に適用され、job単位のsecurity limitにはなりません。本番ではtrust domainごとにLoreHub API・Webと分離した
専用の使い捨てrunner nodeまたはpodを使い、gVisor、Kata Containersなど検証済みの隔離層を組み合わせます。Composeの
smoke testはローカルDockerの境界を確認し、本番用の隔離層は対象にしません。

## Lore credential

branch観測にはobserver service principalを使い、exact revisionのcloneには別のCI service principalを使います。
どちらも正確なrepository partitionと`read` scopeを要求します。tokenを署名する前に、PostgreSQLの有効なgrantを
確認します。repository URLとpartition IDも一緒に検証します。本番ではfile credentialや共有identityへfallbackしません。
static identity adapterは明示した開発・local test設定だけで利用できます。

databaseにはpublic Lore URLを保存します。runner接続時はpartition pathを維持し、authorityだけを
`LOREHUB_LORE_INTERNAL_URL`へ置き換えます。Composeの`runner-data` networkは、このinternal authorityをLore Serverへ
解決します。UCS authorityも同じnetwork上のAPI compatibility listenerへ解決されます。

runnerはservice subject、repository・organization partition、job environmentを使って`actions:execute` contextを
取得します。repository、organization、environmentのvariable・secretはenvironmentを優先してmergeします。variableは
`act`の`--var`として渡します。secretは一時的なpermission `0600`の`--secret-file`で渡し、logでmaskし、終了時に
削除します。resolver errorは`act`の開始前にjobを失敗させます。PostgreSQL resolverは有効な`ci_runner` grantを確認し、
repeatable-read transactionで各scopeの値を読み、AES-256-GCM secretをrunner memory内だけで復号します。管理APIは
variableの値とsecretのmetadataを返します。

本番`JobTokenIssuer`はjob、run、attempt、repository、actor、service subject、REST・GraphQL scope、有効期間を受け取り、
最大15分の`kid`付きRS256 `GITHUB_TOKEN`を発行します。発行時と検証時に、有効なorganization・repository、job lease、
cancel状態、`ci_runner` principal、repository grantを確認します。tokenはsecret fileだけを通して`act`へ渡し、
`github.token`と`secrets.GITHUB_TOKEN`から利用できます。SARIF endpointも同じjob境界を検証し、上限内のSARIF 2.1.0
documentとalertをPostgreSQLへ保存します。static token・context adapterは開発・test専用です。

`LOREHUB_RUNNER_PLATFORM_IMAGES`では、operatorが管理するrunner labelとimageの対応を追加できます。既定値は
`ubuntu-latest=ghcr.io/catthehacker/ubuntu:act-24.04`です。対応がないlabelは拒否し、workflowからmappingを追加・
上書きすることはできません。

`actions/checkout`以外のremote actionは、operatorが設定した`LOREHUB_ACTION_SOURCE_URL`からrunner proxy経由で取得します。
一時的なlocal repository mappingへ展開し、workspaceと一緒に削除します。workflowから取得元は変更できません。
GitHub contextのpublic URLには、設定済みのLoreHub URLを使います。

`workflow_dispatch.inputs`のdescription、required、default、type、choice optionを保持します。APIとUIで定義を表示し、
serverで入力値を検証してから、確定した文字列をevent payloadへ保存します。実行時は`github.event.inputs`と
`inputs.*` contextを利用できます。GitHub contextと`GITHUB_*`環境変数には、設定済みLoreHub public origin、API URL、
GraphQL URLを設定します。

workflowには公式の`actions/checkout@v4`を記述できます。Loreのworkspaceを使うため、`ref`、`repository`、`path`、
`filter`、`sparse-checkout`、`ssh-key`、`lfs: true`、submoduleには対応しません。これらのinputを指定したworkflowは、
理由を表示して無効にします。

## Workflow catalogとbranch

workflow catalogは既定branchから読み込みます。Lore hookはpush policy用の最新branch revisionを記録します。branch
pollerはworkflowを確認したrevisionを別に記録します。hook通知が先に到着した場合も、pollerはworkflowを検出します。
初回検出ではworkflow recordだけを同期し、push runは作りません。以降は確認したrevisionごとに、条件が一致する
workflowのrunを一つqueueへ登録します。消えたworkflowは無効にし、不正または未対応のworkflowはerror状態で表示します。

feature branchからcatalogを更新、削除、無効化することはできません。feature branchのexact revisionにあるworkflow定義は
revision tableへ保存し、そのrevisionのpush runだけを登録できます。既定branchのcatalogに同じworkflowが存在するまで、
dispatch対象にはなりません。

## Resourceと出力の上限

job timeoutの上限は24時間で、runnerはleaseを更新します。`act`の実行中もcancelをpollingし、終了signalを送った後、
grace periodを過ぎたprocessを停止します。leaseを失ったrunnerは完了状態を公開できません。workspace、途中のartifact tree、
使い捨てjob network、proxy gateway、`act --rm` containerは終了時に削除します。

log上限は`LOREHUB_RUNNER_LOG_MAX_BYTES`で、既定値は10 MiBです。artifactは既定で100 file、1 file 100 MiB、
1 job 500 MiBまでです。symlink、特殊file、staging tree外のpathは保存しません。すべてのfileを検証してから、完成した
staging treeを保存先へ移動します。保存に失敗した場合はartifactを成功扱いにしません。

## APIの公開範囲

public repositoryのworkflow・run metadataと上限内のjob logは匿名で閲覧できます。internal repositoryには、有効なuserと
organization membershipが必要です。private repositoryには、直接のrepository membership、team membershipとteam role、
またはorganization owner権限が必要です。public repositoryのartifactは匿名でdownloadできます。internal・privateのlogと
artifactにはread permissionが必要です。dispatch、cancel、rerunにはwrite permissionが必要です。rerunは新しいrun numberを
取得し、`runAttempt`と`rerunOf`を保存します。

browser sessionからの更新にはcookieとCSRFを検証します。bearer authenticationも利用できます。権限がないprivate・
internal repository requestは`404`を返します。

## Compose smoke test

他のDocker projectへ影響しない固有のproject名を使います。smoke testは`runner`と`runner-smoke` profileを使い、host公開
portを持つ一時canaryを別networkで起動します。実際の`act` jobから次を確認します。

- `actions/checkout`後に`.github`外のLore file内容がworkspaceにあること
- PostgreSQLとLore serviceのIPへ接続できないこと
- 外側の`runner-egress` gateway、`host.docker.internal`、canary、既存のhost公開portへ接続できないこと
- private・local宛先をproxyが拒否し、public HTTPSはproxy経由で成功し、raw public TCPは失敗すること
- `docker info`が`rootless`を返し、認証なしの2375が失敗し、2376ではclient証明書が必要なこと
- jobからDocker client証明書directoryと環境変数を参照できないこと
- 終了後に`act` containerと使い捨てjob networkが残らないこと

smoke testで指定したprojectとvolumeだけを削除します。Docker全体のcleanup commandは実行しません。
