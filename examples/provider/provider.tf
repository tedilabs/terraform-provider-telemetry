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
  description = "PostHog ingestion host and project token. Required when telemetry is enabled."
  type = object({
    host          = string
    project_token = string
  })
  default   = null
  sensitive = true
}

# A check block evaluates the function without affecting any resource.
check "telemetry" {
  assert {
    # A null connection disables the call. Wrapping the call in a conditional
    # expression would not, because Terraform evaluates both of its results.
    condition = provider::telemetry::capture_posthog(
      var.telemetry_enabled ? var.posthog_connection : null,
      # Every option is optional. Keep the default collectors except network.
      {
        network = false
      },
      {
        module = "example"
      }
    )
    error_message = "Telemetry invocation failed."
  }
}
