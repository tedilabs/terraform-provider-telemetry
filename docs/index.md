---
page_title: "Provider: Telemetry"
description: |-
  The Telemetry provider offers best-effort telemetry functions for Terraform configurations and modules.
---

# Telemetry Provider

The Telemetry provider offers best-effort telemetry functions for Terraform configurations and modules.
Each function collects metadata about the environment running Terraform, such as the machine,
the Git repository, or the GitHub Actions run, and attempts to send it as an event to a telemetry destination.
[PostHog](https://posthog.com/) is currently the only destination.

The provider manages no resources or data sources, and has no configuration options:
connection settings are passed to each function call instead.
Provider-defined functions require Terraform 1.8 or later.

Use the navigation to the left to read about the available functions and guides.

## Example Usage

```terraform
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
```

~> **Note:** To disable a call, pass a `null` connection as in the example above. Do not wrap the call in a conditional expression such as `var.telemetry_enabled ? provider::telemetry::capture_posthog(...) : true`: Terraform evaluates both results of a conditional expression, so the function still runs and sends the event.

<a id="limitations"></a>
## Limitations

### Side effects

Terraform expects provider-defined functions to be pure: to return the same result for the same arguments,
without side effects. The functions of this provider deliberately break this expectation.
They run local commands, read local files, and send network requests.
To keep plans stable, they always return the known value `true`, regardless of the outcome.

### No delivery or execution guarantees

Terraform decides when and how often a function call is evaluated,
so the number of events is not a reliable count of Terraform runs, module instances, or users.

* A single command can evaluate a call more than once. For example, `terraform apply` plans and then applies,
  and can send an event in each phase. These events share the same `terraform.command_id`.
* A call might not be evaluated at all, for example when an earlier error stops Terraform,
  when its arguments are unknown, when `-target` excludes it, or, for calls in `check` blocks, during destroy operations.
* Delivery failures are ignored and never retried.

Use these functions only for best-effort usage telemetry, never for auditing or billing.

### Collected data and privacy

!> **Warning:** By default, the functions collect metadata that identifies people or machines, such as the hostname, the public IP address, and the Git remote URL. Disable the collectors you do not need. The [Collected Metadata](https://registry.terraform.io/providers/tedilabs/telemetry/latest/docs/guides/collected-metadata) guide lists every property and its source.

Every metadata collector except `github` is enabled by default. Set a collector to `false` in `options` to disable it.
To count distinct hosts, repositories, or users without receiving the values, replace the identifying properties
with pseudonyms instead; see [Pseudonymization](https://registry.terraform.io/providers/tedilabs/telemetry/latest/docs/functions/capture_posthog#pseudonymization).
PostHog does not attribute events to the same user unless `identity_keys` is set in `options`.
To add telemetry to a module that others use, follow the
[Using Telemetry in Modules](https://registry.terraform.io/providers/tedilabs/telemetry/latest/docs/guides/using-in-modules) guide.

Connection settings, such as a project token, are function arguments.
Marking a variable as `sensitive` hides its value in Terraform output, but saved plan files still contain it.
