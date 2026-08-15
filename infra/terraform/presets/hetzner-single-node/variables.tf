variable "name" {
  description = "Name of the server and related Hetzner Cloud resources."
  type        = string
  default     = "lorehub-single-node"

  validation {
    condition     = length(var.name) > 0 && length(var.name) <= 48 && can(regex("^[A-Za-z0-9][A-Za-z0-9-]*$", var.name))
    error_message = "name must be 1 to 48 characters and use only letters, numbers, and hyphens."
  }
}

variable "server_type" {
  description = "Hetzner Cloud server type."
  type        = string
  default     = "cx32"
}

variable "image" {
  description = "Hetzner Cloud image name."
  type        = string
  default     = "ubuntu-24.04"
}

variable "location" {
  description = "Hetzner Cloud location. Use fsn1 or nbg1 by default."
  type        = string
  default     = "fsn1"
}

variable "ssh_public_key" {
  description = "OpenSSH public key installed on the server."
  type        = string

  validation {
    condition     = length(trimspace(var.ssh_public_key)) > 0
    error_message = "ssh_public_key must not be empty."
  }
}

variable "admin_cidrs" {
  description = "CIDR ranges allowed to connect to SSH."
  type        = list(string)

  validation {
    condition = length(var.admin_cidrs) > 0 && alltrue([
      for cidr in var.admin_cidrs : can(cidrhost(cidr, 0))
    ])
    error_message = "admin_cidrs must contain at least one valid IPv4 or IPv6 CIDR."
  }
}

variable "volume_size_gb" {
  description = "Size of the attached Lore data volume in GB."
  type        = number
  default     = 10

  validation {
    condition     = var.volume_size_gb >= 1 && floor(var.volume_size_gb) == var.volume_size_gb
    error_message = "volume_size_gb must be a positive whole number."
  }
}

variable "repository_url" {
  description = "Git URL used to clone LoreHub."
  type        = string
  default     = "https://github.com/hckaye/LoreHub.git"

  validation {
    condition     = can(regex("^https://[^[:space:]]+$", var.repository_url))
    error_message = "repository_url must be an HTTPS URL without whitespace."
  }
}

variable "repository_ref" {
  description = "Git branch or tag to clone."
  type        = string
  default     = "main"

  validation {
    condition     = length(trimspace(var.repository_ref)) > 0 && !can(regex("[[:space:]]", var.repository_ref))
    error_message = "repository_ref must not be empty or contain whitespace."
  }
}

variable "root_domain" {
  description = "Managed public domain used by LoreHub and Lore Server."
  type        = string

  validation {
    condition = (
      can(regex("^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$", var.root_domain)) &&
      !strcontains(var.root_domain, "..")
    )
    error_message = "root_domain must be a DNS name without whitespace or consecutive dots."
  }
}

variable "public_origin_url" {
  description = "Public HTTPS origin for the LoreHub web application."
  type        = string

  validation {
    condition     = can(regex("^https://[A-Za-z0-9.-]+$", var.public_origin_url))
    error_message = "public_origin_url must be an HTTPS origin without a path or trailing slash."
  }
}

variable "internal_domain" {
  description = "Internal DNS suffix used by the API and Lore Server."
  type        = string
  default     = null

  validation {
    condition = (
      var.internal_domain == null || var.internal_domain == "" ||
      (can(regex("^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$", var.internal_domain)) &&
      !strcontains(var.internal_domain, ".."))
    )
    error_message = "internal_domain must be a DNS name when provided."
  }
}

variable "tls_server_names" {
  description = "TLS certificate SANs. An empty list derives names from public_origin_url, root_domain, and internal_domain."
  type        = list(string)
  default     = []

  validation {
    condition = alltrue([
      for name in var.tls_server_names : can(regex("^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$", name))
    ])
    error_message = "tls_server_names must contain DNS names without whitespace."
  }
}

variable "tls_source_dir" {
  description = "Host directory containing production TLS files consumed by tls-init."
  type        = string
  default     = "/etc/lorehub/tls-source"

  validation {
    condition     = startswith(var.tls_source_dir, "/") && !can(regex("[[:space:]]", var.tls_source_dir))
    error_message = "tls_source_dir must be an absolute path without whitespace."
  }
}

variable "instance_admin_usernames" {
  description = "LoreHub usernames allowed to administer this instance."
  type        = list(string)
  default     = []

  validation {
    condition = alltrue([
      for username in var.instance_admin_usernames :
      length(trimspace(username)) > 0 && !strcontains(username, ",") && !can(regex("[[:space:]]", username))
    ])
    error_message = "instance_admin_usernames must contain non-empty usernames without commas or whitespace."
  }
}

variable "max_organizations_per_user" {
  description = "Maximum organizations a user may create."
  type        = number
  default     = 1

  validation {
    condition     = var.max_organizations_per_user >= 0 && floor(var.max_organizations_per_user) == var.max_organizations_per_user
    error_message = "max_organizations_per_user must be a non-negative whole number."
  }
}

variable "max_repositories_per_organization" {
  description = "Maximum repositories an organization may create."
  type        = number
  default     = 1

  validation {
    condition     = var.max_repositories_per_organization >= 0 && floor(var.max_repositories_per_organization) == var.max_repositories_per_organization
    error_message = "max_repositories_per_organization must be a non-negative whole number."
  }
}

variable "max_repository_size_bytes" {
  description = "Maximum file-tree size accepted for a pushed revision."
  type        = number
  default     = 10485760

  validation {
    condition     = var.max_repository_size_bytes >= 0 && floor(var.max_repository_size_bytes) == var.max_repository_size_bytes
    error_message = "max_repository_size_bytes must be a non-negative whole number."
  }
}

variable "enable_public_ingress" {
  description = "Allow inbound TCP 80 and 443 when no Cloudflare Tunnel token is set."
  type        = bool
  default     = false
}

variable "cloudflared_tunnel_token" {
  description = "Optional Cloudflare Tunnel token. A non-empty token takes precedence over public 80/443 ingress."
  type        = string
  sensitive   = true
  default     = ""

  validation {
    condition     = !can(regex("[\r\n]", var.cloudflared_tunnel_token))
    error_message = "cloudflared_tunnel_token must not contain a newline."
  }
}

variable "ssh_user" {
  description = "SSH user used by the ssh_command output."
  type        = string
  default     = "root"

  validation {
    condition     = length(trimspace(var.ssh_user)) > 0 && !can(regex("[[:space:]]", var.ssh_user))
    error_message = "ssh_user must be a non-empty name without whitespace."
  }
}

variable "keycloak_smtp_host" {
  description = "SMTP host used by Keycloak for production email verification."
  type        = string

  validation {
    condition     = length(trimspace(var.keycloak_smtp_host)) > 0 && !can(regex("[[:space:]]", var.keycloak_smtp_host))
    error_message = "keycloak_smtp_host must be a non-empty hostname without whitespace."
  }
}

variable "keycloak_smtp_port" {
  description = "SMTP port used by Keycloak."
  type        = number
  default     = 587

  validation {
    condition     = var.keycloak_smtp_port >= 1 && var.keycloak_smtp_port <= 65535 && floor(var.keycloak_smtp_port) == var.keycloak_smtp_port
    error_message = "keycloak_smtp_port must be a valid TCP port."
  }
}

variable "keycloak_smtp_from" {
  description = "From address used by Keycloak."
  type        = string

  validation {
    condition     = length(trimspace(var.keycloak_smtp_from)) > 0 && !can(regex("[[:space:]]", var.keycloak_smtp_from))
    error_message = "keycloak_smtp_from must be a non-empty address without whitespace."
  }
}

variable "keycloak_smtp_auth" {
  description = "Whether Keycloak should authenticate to the SMTP server."
  type        = bool
  default     = false
}

variable "keycloak_smtp_username" {
  description = "SMTP username used when Keycloak SMTP authentication is enabled."
  type        = string
  default     = ""
}

variable "keycloak_smtp_password" {
  description = "SMTP password used when Keycloak SMTP authentication is enabled."
  type        = string
  sensitive   = true
  default     = ""

  validation {
    condition     = !can(regex("[\r\n]", var.keycloak_smtp_password))
    error_message = "keycloak_smtp_password must not contain a newline."
  }
}

variable "keycloak_smtp_starttls" {
  description = "Whether Keycloak should use SMTP STARTTLS."
  type        = bool
  default     = true
}

variable "keycloak_smtp_ssl" {
  description = "Whether Keycloak should use implicit SMTP TLS."
  type        = bool
  default     = false
}

variable "keycloak_smtp_from_display_name" {
  description = "Display name used by Keycloak for outgoing email."
  type        = string
  default     = "LoreHub"
}

variable "keycloak_smtp_reply_to" {
  description = "Optional reply-to address used by Keycloak."
  type        = string
  default     = ""
}

variable "keycloak_smtp_reply_to_display_name" {
  description = "Display name used by Keycloak for the reply-to address."
  type        = string
  default     = "LoreHub"
}
