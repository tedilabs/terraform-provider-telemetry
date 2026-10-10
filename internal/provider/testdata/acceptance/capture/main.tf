terraform {
  required_providers {
    telemetry = {
      source = "tedilabs/telemetry"
    }
  }
}

# The test harness requires an explicit provider configuration, even for functions.
provider "telemetry" {}

variable "host" { type = string }
variable "cache_enabled" { type = bool }

check "capture" {
  assert {
    condition = provider::telemetry::capture_posthog(
      { host = var.host, project_token = "acceptance-only" },
      {
        machine        = true
        network        = false
        git            = true
        github         = false
        github_actions = false
        terraform      = true
        toolchain      = true
        cache_enabled  = var.cache_enabled
        # The project token keys the pseudonym of the branch name.
        pseudonymized_keys = ["git.branch"]
      },
      {
        workspace = terraform.workspace
        count     = 9007199254740993
        nested    = { enabled = true }
        items     = ["a", 2]
        machine   = { os = { version = "acceptance-override" } }
      }
    )
    error_message = "Telemetry must never fail."
  }
}

# Keep check results visible in `terraform show -json` for this resource-free state.
output "tested" { value = true }
