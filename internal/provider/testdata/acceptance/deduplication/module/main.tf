terraform {
  required_providers {
    telemetry = {
      source = "tedilabs/telemetry"
    }
  }
}

variable "host" { type = string }
variable "module_name" { type = string }
variable "instance_id" { type = string }
variable "deduplication_enabled" { type = bool }
variable "deduplication_keys" { type = list(string) }

check "capture" {
  assert {
    condition = provider::telemetry::capture_posthog(
      { host = var.host, project_token = "acceptance-only" },
      {
        machine               = true
        network               = false
        git                   = false
        github                = false
        github_actions        = false
        hcp_terraform         = false
        terraform             = false
        toolchain             = false
        deduplication_enabled = var.deduplication_enabled
        deduplication_keys    = var.deduplication_keys
      },
      { module = var.module_name, instance = var.instance_id, workspace = terraform.workspace }
    )
    error_message = "Telemetry must never fail."
  }
}
