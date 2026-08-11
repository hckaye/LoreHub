# Personal access token

[English](personal-access-tokens.md) | [日本語](personal-access-tokens.ja.md)

Personal access tokenは、Lore CLIとLoreHub APIを呼び出すscriptの認証に使います。作成と失効は
**アカウント設定**の**Personal access token**から行います。

## tokenを作成する

1. 端末やアプリを識別できる名前を入力します。
2. 7日から1年までの有効期限を選びます。
3. アプリに必要な権限を選びます。
4. **tokenを発行**を選びます。
5. `lhp_`で始まる値をコピーし、password managerまたはsecret storeへ保存します。

tokenの値が表示されるのは作成直後だけです。再読み込み後も名前、先頭部分、権限、有効期限、最終利用時刻は
確認できますが、tokenの値は再表示できません。

## 権限

| 権限               | 操作                                               |
| ------------------ | -------------------------------------------------- |
| `read_api`         | LoreHub APIからデータを読み取る                    |
| `api`              | LoreHub APIからデータを読み書きする                |
| `read_repository`  | アカウントが利用できるLoreリポジトリを読み取る     |
| `write_repository` | アカウントが利用できるLoreリポジトリを読み書きする |

tokenの権限だけではリポジトリへアクセスできません。アカウントにも対象のLoreリポジトリに対する権限が必要です。
`write_repository`にはリポジトリ管理と`obliterate`の権限は含まれません。

Personal access tokenでは、Personal access tokenの設定APIを操作できません。tokenの作成と失効には、ログイン済みの
ブラウザセッションを使います。

## Lore CLIでログインする

リポジトリ画面からLore URLをコピーし、次を実行します。

```bash
lore auth login --token-type api-key --token "$LOREHUB_TOKEN" "$LORE_URL"
```

`LOREHUB_TOKEN`にはtoken作成後に表示された値を設定します。`LORE_URL`にはリポジトリのLore URLを設定します。
Lore CLIはPersonal access tokenを短時間だけ有効なLore tokenへ交換し、選択したリポジトリ権限を適用します。

端末から接続する必要がなくなったら、保存済みのLore認証を削除します。

```bash
lore auth logout
```

## LoreHub APIを呼び出す

tokenをBearer認証として送ります。

```bash
curl --fail-with-body \
  --header "Authorization: Bearer $LOREHUB_TOKEN" \
  --header "Accept: application/json" \
  "$LOREHUB_URL/api/v1/dashboard"
```

`GET`だけを送るclientには`read_api`を使います。データを変更するリクエストには`api`が必要です。

## tokenを失効する

**アカウント設定**で、対象tokenの**失効**を選びます。失効済みまたは期限切れのtokenは、新しいAPIリクエストの認証と
Lore tokenの取得には使えません。Lore CLIへ発行済みのLore tokenは、最長10分の有効期限が切れるまで利用できます。
アカウントを停止した場合も新しい認証を拒否します。

**最終利用**の表示は、リクエストから最大5分遅れて更新される場合があります。拒否されたリクエストでは更新しません。
