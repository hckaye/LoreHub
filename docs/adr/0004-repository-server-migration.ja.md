# ADR 0004: リポジトリのサーバー間移行

[English](0004-repository-server-migration.md) | [日本語](0004-repository-server-migration.ja.md)

- Status: Accepted
- Date: 2026-08-13

## Context

ADR 0002 では、各リポジトリが登録済みの Lore Server を使えるようにした。既存リポジトリの移動方法は定義していない。

- Control plane は `repositories.lore_url` と `repositories.lore_server_id` を保存する。Lore のデータは割り当てた
  serverにあるため、データをコピーする前に参照先を変えると、リポジトリが空または不整合に見える。
- 現在の Lore SDK には、リポジトリの clone、branch の一覧取得、branch の切り替え、branch の push がある。リポジトリ
  全体を server 間でコピーする機能や、server 間で原子的に移行する機能はない。
- 運用者が意図的に実行でき、監査記録を残せる保守処理が必要である。コピーに失敗した場合は現在の参照先を変えず、
  失敗前に作った移行先は運用者が確認して削除できる状態にする。

## Decision

移行は、instance administratorが1つのリポジトリを1つの登録済み移行先へ移す、運用者主導のオフライン処理とする。

### Lifecycle

1. administratorが `POST /api/v1/admin/repositories/{owner}/{repo}/migrate` を `targetServerId` 付きで呼ぶ。
2. serializable transactionでリポジトリと移行先を確認する。移行先はactiveで、リポジトリのOrganizationから参照できなければ
   ならない。transactionで `pending` の監査行を作り、`repositories.migrating_at` を設定する。
3. このflagがある間、リポジトリへのwrite権限をread-onlyにする。readは継続できる。Lore policy hookはこの移行中だけ移行先の
   serverを受け付ける。
4. 制限時間付きのbackground taskが行を `mirroring` にして、Lore dataをコピーする。初回実装のtask期限は10分とする。
5. コピーに成功したら行を `repointing` にする。1つのtransactionでLoreの参照先2列を更新し、`migrating_at` を消し、行を
   `completed` にする。
6. 失敗時は行を `failed` にし、エラーを保存し、現在の参照先を保ったまま `migrating_at` を1つのtransactionで消す。移行先の
   partitionを作成済みなら孤立するため、運用者が削除する。

### Bounded mirror operation

移行処理は同じ Lore repository IDを持つ新しい移行先 partitionを作る。sourceを一時workspaceへcloneし、workspaceのremoteを
移行先serverに変え、sourceのactiveかつ空でないbranchを記録済みの先端revisionで1つずつpushする。

これはLore server全体の完全なmirrorではない。現在のSDKには原子的なmirror pushがないため、完全コピーとは扱わない。コピーした
branchの先端から到達できるhistoryは保持するが、archived branch、branch metadata、その他のserver固有metadataのコピーは保証しない。
そのため移行中はwriteを止め、参照先を変える前に内容を確認する。

### API and audit states

endpointは新しい移行行を `202 Accepted` で返す。両endpointはinstance administrator middlewareで保護する。

| Endpoint                            | Result                       |
| ----------------------------------- | ---------------------------- |
| `POST .../{owner}/{repo}/migrate`   | `targetServerId`で移行を開始 |
| `GET .../{owner}/{repo}/migrations` | 監査行の一覧を返す           |

| State        | Meaning                                     |
| ------------ | ------------------------------------------- |
| `pending`    | read-onlyを設定し、copy開始を待っている     |
| `mirroring`  | 移行先partitionを作成または投入している     |
| `repointing` | copyが終わり、参照先更新を待っている        |
| `completed`  | 移行先を割り当て、read-onlyを解除した       |
| `failed`     | sourceを割り当てたまま、read-onlyを解除した |

## Rejected alternatives

### writeを許したままのlive migration

コピー後にsourceへ届いたwriteを扱えない。競合を解決するprotocolがないため、結果が不明確になる。

### server間のmirror push

現在のLore SDKにはリポジトリ全体のmirror primitiveも、server間のatomic pushもない。利用できる操作以上の保証を表明することになる。

### 検証前に参照先を変える

branchをすべてpushする前に参照先を変えると、不完全なdataを公開し、復旧も難しくなる。

### 既存の移行先partitionを再利用または上書きする

既存partitionには無関係なdataが入っている可能性がある。新しいpartitionなら、失敗後に確認して削除する対象が明確になる。

## Operational consequences

- instance administratorはread-onlyの時間帯を確保する必要がある。Lore clientとリポジトリのwrite APIはcopy中に拒否し、readは継続する。
- sourceと移行先のLore ServerはLoreHubから到達可能で、移行先には新しいpartitionを作る容量が必要である。
- 失敗時に移行先を自動削除しない。sourceが利用できることを確認してから、運用者が孤立した移行先partitionを調べて削除する。
- 移行行には現在の状態とエラー内容を保存する。retryでは失敗行を残したまま新しい行を作る。
- 制限付きのcopyが保持するのはactive branchの先端から到達できるrevision historyであり、Lore server固有metadataのすべてではない。
  通常のwriteを戻す前に、branchと重要なリポジトリdataを確認する。
