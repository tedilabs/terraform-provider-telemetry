terraform {
  required_providers {
    telemetry = {
      source = "tedilabs/telemetry"
    }
  }
}

provider "telemetry" {}

variable "host" { type = string }

locals {
  connection = { host = var.host, project_token = "acceptance-only" }
  options = {
    machine        = false
    network        = false
    git            = false
    github         = false
    github_actions = false
    hcp_terraform  = false
    terraform      = false
    toolchain      = false
  }
}

# The built-in provider supplies values that are unknown in the initial plan.
resource "terraform_data" "inputs" {
  input = {
    connection = local.connection
    options    = local.options
    extra_data = { test_case = "extra_data", value = "known" }
    value      = "known"
  }
}

locals {
  results = {
    connection = provider::telemetry::capture_posthog(terraform_data.inputs.output.connection, local.options, { test_case = "connection", value = "known" })
    host       = provider::telemetry::capture_posthog(merge(local.connection, { host = terraform_data.inputs.output.connection.host }), local.options, { test_case = "host", value = "known" })
    options    = provider::telemetry::capture_posthog(local.connection, terraform_data.inputs.output.options, { test_case = "options", value = "known" })
    option     = provider::telemetry::capture_posthog(local.connection, merge(local.options, { machine = terraform_data.inputs.output.options.machine }), { test_case = "option", value = "known" })
    extra_data = provider::telemetry::capture_posthog(local.connection, local.options, terraform_data.inputs.output.extra_data)
    nested = provider::telemetry::capture_posthog(local.connection, local.options, {
      test_case = "nested"
      object    = { list = ["known", terraform_data.inputs.output.value] }
    })
  }
}

check "inputs" {
  assert {
    condition     = alltrue(values(local.results))
    error_message = "Unknown inputs must still return true."
  }
}

output "results" { value = local.results }
