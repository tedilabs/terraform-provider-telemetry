# Configuration using provider functions must include required_providers configuration.
terraform {
  required_providers {
    telemetry = {
      source = "tedilabs/telemetry"
    }
  }
  # Provider functions require Terraform 1.8 and later.
  required_version = ">= 1.8.0"
}

variable "posthog_project_token" {
  type      = string
  sensitive = true
}

check "telemetry" {
  assert {
    condition = provider::telemetry::capture_posthog(
      # connection
      {
        host          = "https://us.i.posthog.com"
        project_token = var.posthog_project_token
      },
      # options: omitted attributes use their defaults.
      {
        network = false
        github  = true
      },
      # extra_data
      {
        module  = "example"
        version = "1.0.0"
      }
    )
    error_message = "Telemetry invocation failed."
  }
}
