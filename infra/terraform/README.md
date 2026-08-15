# Terraform layouts

The Terraform configuration is organized by deployment preset. A preset is a complete choice of cloud provider and
service placement. Shared resources that are used by more than one preset belong under `modules/`.

## Directory layout

| Path       | Purpose                                                        |
| ---------- | -------------------------------------------------------------- |
| `presets/` | Complete deployment choices that can be applied independently. |
| `modules/` | Reusable Terraform resources used by presets.                  |

## Available presets

| Preset                | Provider      | Intended audience and scale                                                                                                                  |
| --------------------- | ------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `hetzner-single-node` | Hetzner Cloud | A small public test deployment with Postgres, Lore Server, web, API, Keycloak, and runners on one `cx32` server. It is not highly available. |

Presets share `modules/` where reuse makes sense. The current `hetzner-single-node` preset uses
`modules/hetzner-server` for the SSH key, firewall, and server resources. A future multi-instance or non-Hetzner
preset can add modules for the resources that it actually shares with an existing preset.

## Apply a preset

1. Read the README in the selected preset directory.
2. Copy its `terraform.tfvars.example` to `terraform.tfvars` and set the required values.
3. Export `HCLOUD_TOKEN` for Hetzner Cloud.
4. Run `terraform init`, `terraform validate`, and `terraform apply` from the preset directory.
