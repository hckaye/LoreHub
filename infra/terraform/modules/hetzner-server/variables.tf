variable "name" {
  description = "Name of the Hetzner Cloud server."
  type        = string

  validation {
    condition     = length(var.name) > 0 && length(var.name) <= 63
    error_message = "name must contain between 1 and 63 characters."
  }
}

variable "server_type" {
  description = "Hetzner Cloud server type."
  type        = string
}

variable "image" {
  description = "Image name or ID used for the server."
  type        = string
}

variable "location" {
  description = "Hetzner Cloud location for the server."
  type        = string
}

variable "ssh_key_name" {
  description = "Name of the SSH key resource."
  type        = string
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

variable "enable_public_ingress" {
  description = "Whether the firewall allows the public ingress ports."
  type        = bool
  default     = false
}

variable "public_ingress_ports" {
  description = "TCP ports allowed when public ingress is enabled."
  type        = set(string)
  default     = ["80", "443"]
}

variable "firewall_name" {
  description = "Name of the Hetzner Cloud firewall resource."
  type        = string
}

variable "user_data" {
  description = "Cloud-init user data for the server."
  type        = string
}
