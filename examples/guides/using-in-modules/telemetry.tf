terraform {
  required_providers {
    telemetry = {
      source  = "tedilabs/telemetry"
      version = ">= 0.1.3"
    }
  }
  # Provider functions require Terraform 1.8 and later.
  required_version = ">= 1.8.0"
}

variable "telemetry_enabled" {
  description = "Whether to send usage telemetry of this module to its maintainers."
  type        = bool
  default     = false
  nullable    = false
}

locals {
  telemetry = {
    connection = {
      host          = "https://us.i.posthog.com"
      project_token = "<project-token>"
    }
    # Update together with each release of the module.
    module  = "tedilabs/example/aws"
    version = "1.2.3"
  }
}

check "telemetry" {
  assert {
    condition = provider::telemetry::capture_posthog(
      # A null connection disables the call.
      var.telemetry_enabled ? local.telemetry.connection : null,
      # Keep only the machine and toolchain metadata.
      {
        network        = false
        git            = false
        github_actions = false
        hcp_terraform  = false
        terraform      = false
      },
      {
        module  = local.telemetry.module
        version = local.telemetry.version
      }
    )
    error_message = "Telemetry invocation failed."
  }
}
