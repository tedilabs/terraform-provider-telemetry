# Terraform Provider: Telemetry

The Telemetry provider offers best-effort telemetry functions for Terraform configurations and modules.
Each function collects opt-in metadata about the environment running Terraform, such as the machine,
the Git repository, or the GitHub Actions run, and attempts to send it as an event to a telemetry destination.
The provider currently offers the [`capture_posthog`](docs/functions/capture_posthog.md) function
for [PostHog](https://posthog.com/).

The provider manages no resources or data sources, and has no configuration options.

## Documentation, questions and discussions

Official documentation on how to use this provider can be found on the
[Terraform Registry](https://registry.terraform.io/providers/tedilabs/telemetry/latest/docs).
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
  required_version = ">= 1.8.0"
}

check "telemetry" {
  assert {
    condition = provider::telemetry::capture_posthog(
      { host = "https://us.i.posthog.com", project_token = var.posthog_project_token },
      { machine = true, network = false, git = false, github = false, github_actions = false },
      { module = "example" }
    )
    error_message = "Telemetry invocation failed."
  }
}
```

Read the [provider documentation](https://registry.terraform.io/providers/tedilabs/telemetry/latest/docs)
for the collected metadata, the limitations of side-effecting functions, and privacy considerations.

## License

[Apache License 2.0](LICENSE)
