output "id" {
  description = "Hetzner Cloud server ID."
  value       = hcloud_server.this.id
}

output "ipv4_address" {
  description = "Public IPv4 address of the server."
  value       = hcloud_server.this.ipv4_address
}

output "ipv6_address" {
  description = "First public IPv6 address of the server."
  value       = hcloud_server.this.ipv6_address
}

output "firewall_id" {
  description = "Hetzner Cloud firewall ID."
  value       = hcloud_firewall.this.id
}

output "ssh_key_id" {
  description = "Hetzner Cloud SSH key ID."
  value       = hcloud_ssh_key.this.id
}
