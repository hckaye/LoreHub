locals {
  internal_domain = (
    var.internal_domain == null || trimspace(var.internal_domain) == "" ?
    "internal.${var.root_domain}" : trimspace(var.internal_domain)
  )
  public_origin_domain = trimprefix(var.public_origin_url, "https://")

  tls_server_names = length(var.tls_server_names) > 0 ? var.tls_server_names : distinct([
    local.public_origin_domain,
    var.root_domain,
    "auth.${var.root_domain}",
    "lore.${var.root_domain}",
    "api.${local.internal_domain}"
  ])

  cloudflared_tunnel_enabled = nonsensitive(trimspace(var.cloudflared_tunnel_token) != "")
  public_ingress_enabled     = var.enable_public_ingress && !local.cloudflared_tunnel_enabled
  keycloak_url               = "https://auth.${var.root_domain}"
  oidc_issuer                = "${local.keycloak_url}/realms/lorehub"
  public_api_url             = "${var.public_origin_url}/api/v3"
  public_graphql_url         = "${var.public_origin_url}/api/graphql"

  production_env = templatefile("${path.module}/templates/production.env.tftpl", {
    root_domain                         = var.root_domain
    internal_domain                     = local.internal_domain
    tls_server_names                    = join(",", local.tls_server_names)
    tls_source_dir                      = var.tls_source_dir
    public_origin_url                   = var.public_origin_url
    public_api_url                      = local.public_api_url
    public_graphql_url                  = local.public_graphql_url
    keycloak_url                        = local.keycloak_url
    oidc_issuer                         = local.oidc_issuer
    instance_admin_usernames            = join(",", var.instance_admin_usernames)
    max_organizations_per_user          = var.max_organizations_per_user
    max_repositories_per_organization   = var.max_repositories_per_organization
    max_repository_size_bytes           = var.max_repository_size_bytes
    keycloak_smtp_host                  = var.keycloak_smtp_host
    keycloak_smtp_port                  = var.keycloak_smtp_port
    keycloak_smtp_from                  = var.keycloak_smtp_from
    keycloak_smtp_auth                  = var.keycloak_smtp_auth
    keycloak_smtp_username              = var.keycloak_smtp_username
    keycloak_smtp_password              = var.keycloak_smtp_password
    keycloak_smtp_starttls              = var.keycloak_smtp_starttls
    keycloak_smtp_ssl                   = var.keycloak_smtp_ssl
    keycloak_smtp_from_display_name     = var.keycloak_smtp_from_display_name
    keycloak_smtp_reply_to              = var.keycloak_smtp_reply_to
    keycloak_smtp_reply_to_display_name = var.keycloak_smtp_reply_to_display_name
  })

  nginx_config = templatefile("${path.module}/templates/nginx.conf.tftpl", {
    root_domain            = var.root_domain
    web_server_names       = join(" ", distinct([local.public_origin_domain, var.root_domain]))
    tls_source_dir         = var.tls_source_dir
    public_ingress_enabled = local.public_ingress_enabled
  })

  cloud_init = templatefile("${path.module}/templates/cloud-init.yaml.tftpl", {
    bootstrap_script = templatefile("${path.module}/templates/bootstrap.sh.tftpl", {
      repository_url             = jsonencode(var.repository_url)
      repository_ref             = jsonencode(var.repository_ref)
      volume_device_path         = jsonencode(hcloud_volume.lore_data.linux_device)
      tls_source_dir             = jsonencode(var.tls_source_dir)
      public_ingress_enabled     = local.public_ingress_enabled
      cloudflared_tunnel_enabled = local.cloudflared_tunnel_enabled
      cloudflared_tunnel_token   = jsonencode(var.cloudflared_tunnel_token)
    })
    compose_override    = file("${path.module}/templates/compose.hetzner.yaml")
    production_env      = local.production_env
    nginx_config        = local.nginx_config
    lorehub_service     = file("${path.module}/templates/lorehub.service")
    cloudflared_service = file("${path.module}/templates/cloudflared.service")
  })
}

resource "hcloud_volume" "lore_data" {
  name     = "${var.name}-lore-data"
  size     = var.volume_size_gb
  location = var.location
  format   = "ext4"
}

module "server" {
  source = "../../modules/hetzner-server"

  name                  = var.name
  server_type           = var.server_type
  image                 = var.image
  location              = var.location
  ssh_key_name          = "${var.name}-ssh"
  ssh_public_key        = var.ssh_public_key
  admin_cidrs           = var.admin_cidrs
  enable_public_ingress = local.public_ingress_enabled
  firewall_name         = "${var.name}-firewall"
  user_data             = local.cloud_init
}

resource "hcloud_volume_attachment" "lore_data" {
  server_id = module.server.id
  volume_id = hcloud_volume.lore_data.id
  automount = false
}
