# Terraform の構成

Terraform の設定はデプロイ方法ごとの preset に分けています。preset は、クラウド provider とサービスの配置を
まとめて選ぶ単位です。複数の preset で使うリソースは `modules/` に置きます。

## ディレクトリ構成

| パス       | 役割                                           |
| ---------- | ---------------------------------------------- |
| `presets/` | 個別に適用できるデプロイ方法です。             |
| `modules/` | preset から再利用する Terraform リソースです。 |

## 利用できる preset

- `hetzner-single-node`(Hetzner Cloud): Postgres、Lore Server、web、API、Keycloak、runner を 1 台の `cx32` に
  置く小規模な公開テスト用です。高可用構成ではありません。

preset 間で再利用できるものは `modules/` に置きます。現在の `hetzner-single-node` は、SSH key、firewall、server を
`modules/hetzner-server` から使います。将来、複数インスタンス構成や Hetzner 以外の preset を追加するときは、
既存 preset と実際に共有するリソースだけを module に切り出します。

## preset の適用

1. 選択した preset の README を読みます。
2. `terraform.tfvars.example` を `terraform.tfvars` にコピーし、必要な値を設定します。
3. Hetzner Cloud 用に `HCLOUD_TOKEN` を export します。
4. preset ディレクトリで `terraform init`、`terraform validate`、`terraform apply` を実行します。
