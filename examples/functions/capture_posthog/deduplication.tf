# Send at most one event per module, version, and workspace from a provider
# process, even when the module has many instances through count or for_each.
check "telemetry_deduplicated" {
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
        deduplication_keys = ["module", "version", "workspace"]
      },
      {
        module    = "tedilabs/example/aws"
        version   = "1.2.3"
        workspace = terraform.workspace
      }
    )
    error_message = "Telemetry invocation failed."
  }
}
