resource "hcloud_ssh_key" "this" {
  name       = var.ssh_key_name
  public_key = var.ssh_public_key
}

resource "hcloud_firewall" "this" {
  name = var.firewall_name

  rule {
    description = "SSH from administrator networks"
    direction   = "in"
    protocol    = "tcp"
    port        = "22"
    source_ips  = var.admin_cidrs
  }

  dynamic "rule" {
    for_each = var.enable_public_ingress ? var.public_ingress_ports : toset([])

    content {
      description = "Public HTTP ingress"
      direction   = "in"
      protocol    = "tcp"
      port        = rule.value
      source_ips  = ["0.0.0.0/0", "::/0"]
    }
  }
}

resource "hcloud_server" "this" {
  name        = var.name
  server_type = var.server_type
  image       = var.image
  location    = var.location
  user_data   = var.user_data

  firewall_ids = [hcloud_firewall.this.id]
  ssh_keys     = [hcloud_ssh_key.this.id]

  public_net {
    ipv4_enabled = true
    ipv6_enabled = true
  }

  shutdown_before_deletion = true

  lifecycle {
    # cloud-init only runs on first boot. Changing user_data or image must not
    # replace the server and destroy data on the included disk.
    ignore_changes = [user_data, image]
    # prevent_destroy cannot reference variables. Keep this true; edit it
    # deliberately before terraform destroy.
    prevent_destroy = true
  }
}
