output "server_ipv4" {
  description = "Public IPv4 address of the LoreHub server."
  value       = module.server.ipv4_address
}

output "server_ipv6" {
  description = "First public IPv6 address of the LoreHub server."
  value       = module.server.ipv6_address
}

output "volume_device_path" {
  description = "Stable Linux device path of the attached Lore data volume."
  value       = hcloud_volume.lore_data.linux_device
}

output "ssh_command" {
  description = "SSH command for the configured server user."
  value       = "ssh ${var.ssh_user}@${module.server.ipv4_address}"
}

output "url_summary" {
  description = "Rendered URLs configured by this preset."
  value       = <<-EOT
    Web: ${var.public_origin_url}
    API: ${var.public_origin_url}/api/v3
    GraphQL: ${var.public_origin_url}/api/graphql
    Keycloak: https://auth.${var.root_domain}
    OIDC issuer: https://auth.${var.root_domain}/realms/lorehub
    Lore public endpoint: lores://${var.root_domain}:41337
    Lore auth endpoint: ucs-auth://auth.${var.root_domain}:8443
    Lore JWKS: ${var.public_origin_url}/.well-known/jwks.json
  EOT
}
