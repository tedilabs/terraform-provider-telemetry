# Terraform Provider: Telemetry

The Telemetry provider offers best-effort telemetry functions for Terraform configurations and modules.
Each function collects metadata about the environment running Terraform, such as the machine,
the Git repository, or the GitHub Actions run, and attempts to send it as an event to a telemetry destination.
The provider currently offers the [`capture_posthog`](docs/functions/capture_posthog.md) function
for [PostHog](https://posthog.com/).

The provider manages no resources or data sources, and has no configuration options.

## Documentation, questions and discussions

Official documentation on how to use this provider can be found on the
[Terraform Registry](https://registry.terraform.io/providers/tedilabs/telemetry/latest/docs),
including examples, the collected metadata, the limitations of side-effecting functions,
and a guide for adding telemetry to modules.
For questions, bug reports, or feature requests, please open an
[issue](https://github.com/tedilabs/terraform-provider-telemetry/issues).

We also provide:

* [Contributing](.github/CONTRIBUTING.md) guidelines in case you want to help this project
* [Releasing](.github/RELEASING.md) guide for maintainers who publish new versions
* [Security policy](https://github.com/tedilabs/.github/blob/main/.github/SECURITY.md) for reporting vulnerabilities

## Requirements

* [Terraform](https://developer.hashicorp.com/terraform/install) >= 1.8

## Usage

```hcl
terraform {
  required_providers {
    telemetry = {
      source = "tedilabs/telemetry"
    }
  }
  # Provider functions require Terraform 1.8 and later.
  required_version = ">= 1.8.0"
}

variable "telemetry_enabled" {
  type    = bool
  default = false
}

variable "posthog_project_token" {
  type      = string
  default   = null
  sensitive = true
}

check "telemetry" {
  assert {
    condition = provider::telemetry::capture_posthog(
      # connection: a null connection disables the call.
      var.telemetry_enabled ? {
        host          = "https://us.i.posthog.com"
        project_token = var.posthog_project_token
      } : null,
      # options: every attribute is optional, shown here with its default value.
      {
        # Metadata collectors.
        machine        = true
        network        = true
        git            = true
        github         = false
        github_actions = true
        hcp_terraform  = true
        terraform      = true
        toolchain      = true

        # Caching and deduplication.
        cache_enabled         = true
        deduplication_enabled = true
        deduplication_keys    = []
      },
      # extra_data: additional event properties.
      {
        module  = "example"
        version = "1.0.0"
      }
    )
    error_message = "Telemetry invocation failed."
  }
}
```

The function always returns `true`, whether the event is sent, skipped, or fails to deliver.
Do not wrap the whole call in a conditional expression to disable it:
Terraform evaluates both results of a conditional expression, so the function would still run.

## License

[Apache License 2.0](LICENSE)
