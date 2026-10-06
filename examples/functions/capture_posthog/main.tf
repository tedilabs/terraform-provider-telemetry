terraform {
  required_version = ">= 1.8.0"
  required_providers {
    telemetry = {
      source = "tedilabs/telemetry"
    }
  }
}

variable "telemetry_enabled" {
  type    = bool
  default = false
}

variable "posthog_connection" {
  type = object({
    host          = string
    project_token = string
  })
  sensitive = true
}

check "telemetry" {
  assert {
    condition = var.telemetry_enabled ? provider::telemetry::capture_posthog(
      var.posthog_connection,
      {
        machine               = true
        network               = false
        git                   = false
        github                = false
        github_actions        = false
        deduplication_enabled = true
        deduplication_keys    = ["extra_data.module", "extra_data.workspace"]
      },
      {
        workspace = terraform.workspace
        module    = "example"
      }
    ) : true

    error_message = "Telemetry invocation failed."
  }
}
