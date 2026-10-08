terraform {
  required_providers {
    telemetry = {
      source = "tedilabs/telemetry"
    }
  }
  # Provider functions require Terraform 1.8 and later.
  required_version = ">= 1.8.0"
}

# The provider has no configuration options, so no provider block is needed.

variable "telemetry_enabled" {
  description = "Whether to send usage telemetry."
  type        = bool
  default     = false
}

variable "posthog_connection" {
  description = "PostHog ingestion host and project token."
  type = object({
    host          = string
    project_token = string
  })
  sensitive = true
}

# A check block evaluates the function without affecting any resource.
check "telemetry" {
  assert {
    condition = var.telemetry_enabled ? provider::telemetry::capture_posthog(
      var.posthog_connection,
      {
        machine        = true
        network        = false
        git            = false
        github         = false
        github_actions = false
      },
      {
        module = "example"
      }
    ) : true
    error_message = "Telemetry invocation failed."
  }
}
