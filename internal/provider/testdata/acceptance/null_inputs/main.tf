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

locals {
  results = {
    null_connection       = provider::telemetry::capture_posthog(null, local.options, {})
    null_host             = provider::telemetry::capture_posthog(merge(local.connection, { host = null }), local.options, {})
    null_project_token    = provider::telemetry::capture_posthog(merge(local.connection, { project_token = null }), local.options, {})
    null_options          = provider::telemetry::capture_posthog(local.connection, null, {})
    null_collector_option = provider::telemetry::capture_posthog(local.connection, merge(local.options, { machine = null }), {})
    null_setting_option   = provider::telemetry::capture_posthog(local.connection, merge(local.options, { cache_enabled = null }), {})
    null_key              = provider::telemetry::capture_posthog(local.connection, merge(local.options, { deduplication_keys = [null] }), {})
    misspelled_option     = provider::telemetry::capture_posthog(local.connection, merge(local.options, { netwrok = false }), { test_case = "misspelled_option" })
    null_extra_data       = provider::telemetry::capture_posthog(local.connection, local.options, null)
    known                 = provider::telemetry::capture_posthog(local.connection, local.options, { test_case = "known", value = "known" })
    nested_nulls = provider::telemetry::capture_posthog(local.connection, local.options, {
      test_case = "nested_nulls"
      object    = { value = null }
      list      = tolist([null, "known"])
      map       = tomap({ empty = null, known = "known" })
      set       = toset([null])
      tuple     = [null, 1, true]
    })
  }
}

check "inputs" {
  assert {
    condition     = alltrue(values(local.results))
    error_message = "Null inputs must still return true."
  }
}

output "results" { value = local.results }
