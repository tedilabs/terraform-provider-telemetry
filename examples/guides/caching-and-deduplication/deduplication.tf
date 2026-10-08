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

variable "engine" {
  description = "Database engine, which can differ between instances of the module."
  type        = string
}

# Send at most one event per module and version from a provider process,
# even when instances of the module pass different engine values.
check "telemetry" {
  assert {
    condition = provider::telemetry::capture_posthog(
      {
        host          = "https://us.i.posthog.com"
        project_token = var.posthog_project_token
      },
      {
        machine            = true
        network            = false
        git                = false
        github             = false
        github_actions     = false
        deduplication_keys = ["module", "version"]
      },
      {
        module  = "tedilabs/example/aws"
        version = "1.2.3"
        engine  = var.engine
      }
    )
    error_message = "Telemetry invocation failed."
  }
}
