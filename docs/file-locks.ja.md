# ファイルロック

[English](file-locks.md) | [日本語](file-locks.ja.md)

3Dシーン、テクスチャ、音声制作ファイルなど、安全にマージできないファイルにはロックを使用します。
ロックは、1つのLoreブランチにある1つのファイルに対して有効です。

## LoreHubでファイルをロックする

1. リポジトリを開き、**ロック**を選択します。
2. ファイルがあるブランチを選択します。
3. リポジトリのルートからのファイルパスを入力します。
4. **ファイルをロック**を選択します。

ファイルは選択したブランチに存在している必要があります。ロック一覧には、ロックしたユーザーと日時が
表示されます。リポジトリを閲覧できるユーザーはロックを確認できます。ロックの作成には書き込み権限が必要です。

## ロックを解除する

**ロック**を開き、対象ファイルの**ロックを解除**を選択します。ロックした本人に加えて、リポジトリ管理者と
組織所有者も、ほかのユーザーが残したロックを解除できます。

ロック中のファイルを削除または名前変更しても、ロックは残ります。ロックを作成したブランチから解除してください。

## Lore CLIを使う

ログイン済みのLore workspaceで次のコマンドを実行します。

```bash
lore lock acquire Content/Characters/Hero.uasset --branch main
lore lock query --branch main
lore lock status Content/Characters/Hero.uasset --branch main
lore lock release Content/Characters/Hero.uasset --branch main
```

Web画面とCLIはLore Serverにある同じロックを参照します。どちらで行った変更も両方に表示されます。

## 監査記録

LoreHubは、Web画面から成功したロックと解除の操作を組織の監査ログに記録します。記録にはブランチ、
ファイルパス、ロックしたユーザー、ロックした日時が含まれます。
