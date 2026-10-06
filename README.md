# Terraform Provider Telemetry

Best-effort telemetry functions for Terraform 1.8+. The provider manages no resources or data sources. The first destination is PostHog; additional destinations should expose `capture_<destination>(connection, options, extra_data)` and reuse the common collectors.

## Usage

Terraform functions take **positional** arguments, not named arguments. All three arguments are required; pass `{}` for empty `extra_data`. Set `options.cache_enabled = false` to bypass the metadata cache; omitting it defaults to `true`.

```hcl
terraform {
  required_version = ">= 1.8.0"
  required_providers {
    telemetry = {
      source = "tedilabs/telemetry"
    }
  }
}

variable "posthog_connection" {
  type = object({
    host          = string
    project_token = string
  })
  sensitive = true
}

variable "telemetry_enabled" {
  type    = bool
  default = false
}

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
        workspace = terraform.workspace
        module    = "aws-core"
      }
    ) : true

    error_message = "Telemetry invocation failed."
  }
}
```

This source address is intended for publication; creating this repository does not publish the provider to the Registry. Use a development override for local testing below.

## Connection and event

`connection` has two string attributes:

- `host`: PostHog **ingestion** base URL: `https://us.i.posthog.com`, `https://eu.i.posthog.com`, or a self-hosted base URL. A base path is supported. The function appends `/i/v0/e/`.
- `project_token`: PostHog project token, not a personal API key. It is sent only as the capture payload's `api_key`.

Each non-duplicate invocation attempts to send `terraform_capture` with a fresh UUIDv4 `distinct_id`. It does not create a persistent installation identifier. `$process_person_profile` is `false`, and `$geoip_disable` is `true`. Counts describe retained capture attempts, **not module instance counts, unique people, or successful Terraform operations**.

Collected fields are grouped under `properties.machine`, `properties.network`, etc. User-supplied values are preserved under `properties.extra_data`, avoiding collisions with collectors or PostHog control fields:

```json
{
  "api_key": "<project token>",
  "event": "terraform_capture",
  "distinct_id": "<fresh UUID>",
  "properties": {
    "$process_person_profile": false,
    "$geoip_disable": true,
    "machine": { "os": "linux", "arch": "amd64", "cpu_count": 4 },
    "extra_data": { "workspace": "production", "module": "aws-core" }
  }
}
```

## Collection options

All five collector booleans must be present in the `options` object or map. The `cache_enabled` and `deduplication_enabled` booleans are optional and default to `true`. Optional `deduplication_keys` is a list of property paths; omitted or empty means compare all event properties. Only enabled collectors run. Missing tools, unavailable authentication, or an absent Git repository cause that metadata group to be omitted. A failed collector does not discard data from other collectors.

| Option | Collected fields | Source |
| --- | --- | --- |
| `machine` | `os`, `arch`, `cpu_count` | Go runtime; architecture is the provider binary's architecture |
| `network` | `hostname`, `ips` | Local hostname and non-loopback IPv4/IPv6 interface addresses; no public-IP lookup service |
| `git` | `name`, `branch`, `commit`, `remote` when available | Git repository containing the Terraform process's working directory; `remote` is origin |
| `github` | `login`, `id`, `name`, `html_url` when available | `gh api user` using the CLI's existing authentication and host configuration |
| `github_actions` | Workflow/job/run, repository, actor, ref, and runner metadata | An allowlist of environment variables, only when `GITHUB_ACTIONS=true` |

Git metadata excludes local repository paths and file contents. URL userinfo, query strings, and fragments are removed from origin URLs; SCP-style SSH usernames are removed. Local filesystem remotes are omitted. GitHub metadata excludes tokens and email. No collector runs `gh auth login` or changes Git configuration.

GitHub Actions fields: `workflow`, `workflow_ref`, `workflow_sha`, `job`, `run_id`, `run_number`, `run_attempt`, `repository`, `repository_id`, `repository_owner`, `actor`, `actor_id`, `triggering_actor`, `event_name`, `ref`, `sha`, `head_ref`, `base_ref`, `server_url`, `runner_os`, `runner_arch`, `runner_environment`, and a derived `run_url`. `job` is `GITHUB_JOB`, not a numeric job execution ID; matrix values are not inferred. Pass any additional job context through `extra_data`.

For remote Terraform execution, these describe the runner, not the initiating user's workstation. Setting `network=false` disables explicit network metadata collection; an HTTP connection still necessarily reaches the configured ingestion host.

## Collection cache

Predefined `machine`, `network`, `git`, `github`, and `github_actions` metadata is cached lazily for the lifetime of one provider process. Concurrent calls and separate function instances share these snapshots. Disabled collectors are not read or included, even if their data is already cached. Unavailable or partial results are also cached to avoid repeated failing commands.

Only these explicitly allowlisted groups are cached. `extra_data`, connection information, event IDs, and HTTP delivery are never cached. Collection caching only reduces collection work; the separate event deduplication below reduces PostHog event counts. New collectors must explicitly opt into caching.

Existing calls enable caching. To collect fresh metadata for one invocation, set `cache_enabled = false` in `options`:

```hcl
provider::telemetry::capture_posthog(
  var.posthog_connection,
  { machine = true, network = false, git = true, github = true, github_actions = false, cache_enabled = false },
  { workspace = terraform.workspace }
)
```

A bypass does not replace an earlier snapshot. Cached Git, network, or authentication metadata can become stale if the environment changes while the provider is running; use `cache_enabled = false` when fresh values are needed. The cache is memory-only: no files, Terraform state, or sharing between processes. Planning and applying may start separate provider processes and collect again.

Omitting `cache_enabled` enables caching. An explicit null, unknown, or non-boolean value skips capture and returns `true`. The function accepts exactly three arguments.

## Event deduplication

Deduplication is enabled by default and shared across function instances within one provider process. With no `deduplication_keys` (or `[]`), only events with identical collected metadata and `extra_data` are suppressed. Set `deduplication_enabled = false` to bypass both lookup and recording; this is independent of `cache_enabled`.

To collapse repeated module instances from `for_each` or `count`, select only module-level properties:

```hcl
provider::telemetry::capture_posthog(
  var.posthog_connection,
  {
    machine               = true
    network               = false
    git                   = false
    github                = false
    github_actions        = false
    cache_enabled         = true
    deduplication_enabled = true
    deduplication_keys    = ["extra_data.module", "extra_data.version", "extra_data.workspace"]
  },
  {
    module    = "tedilabs/example/aws"
    version   = "1.2.3"
    workspace = terraform.workspace
  }
)
```

If these values are the same for 100 instances, at most one event is attempted in that provider process. To retain one event for each instance instead, pass an instance identifier in `extra_data` and include its path in `deduplication_keys`. Pass a project identifier too when distinct root configurations need separate identities.

- Paths start at the event properties, e.g. `extra_data.module` or `machine.os`; do not prefix them with `properties.`. Dot-separated paths traverse objects/maps, with no array indexing or escaping for keys containing dots. A path may select a whole object or list.
- All selected paths and their values form the identity. Key order and repeated keys do not matter. Value types and array order matter. The host, project token, and capture destination are always part of the scope, so separate destinations cannot suppress each other's events. Generated event IDs are excluded.
- If any selected path is missing (including metadata from a disabled/unavailable collector), the call sends normally without consulting or updating deduplication state. An explicitly present `null` is a comparable value. Malformed option types or empty path segments skip capture and return `true`.
- The first concurrent caller reserves the identity before sending. Later duplicates return `true` without sending, even if the first attempt fails. There are no retries. Only identity hashes are stored in memory.
- Only the first caller's full properties are sent; changes to unselected properties are ignored. This does not count or aggregate suppressed instances. With concurrent calls, which instance wins is unspecified.
- State lasts for one provider process. A new process starts fresh, including when Terraform restarts the provider between plan and apply. This is not an exactly-once guarantee for a whole Terraform command or for delivery.

## Return value, failures, and execution

- Every invocation handled by the function returns the known boolean `true`, whether delivery succeeds, fails, or is skipped. It never reports capture errors or warnings to Terraform.
- Null connection/options, invalid connection contents, incomplete/unknown arguments, or a non-object/map `extra_data` skip capture. Null `extra_data` is treated as `{}`. Null attributes in connection/options skip capture as well.
- Unknown arguments are explicitly accepted so the function can return `true` without capturing incomplete data. Later evaluation with known values may capture.
- Collection and delivery share a five-second context budget. Each external command and HTTP request has a two-second timeout. There are no retries or HTTP redirects. Process shutdown can add a small delay.
- There are no telemetry logs, files, background senders, resources, or data sources. Successful `check` assertions are quiet in normal CLI output. Terraform still stores the check's pass/fail metadata.
- The Terraform CLI can reject malformed syntax, wrong argument counts/types, or plugin startup failures **before** the function runs. A provider cannot make those failures disappear. `sensitive` redacts output; it does not prevent connection values from appearing in saved plans.

**This function deliberately has side effects.** Terraform expects provider-defined functions to be pure and offline. A `check` assertion is a practical evaluation site, not a guaranteed command hook. Calls can repeat; early failures, unknown arguments, targeting, and other execution paths can prevent a capture. A single plain `apply` includes planning and may produce multiple captures. There is no exactly-once, start-of-command, completion, or success guarantee. Destroy operations do not run normal checks. Do not depend on an unreferenced local value for execution.

References: [Terraform function concepts](https://developer.hashicorp.com/terraform/plugin/framework/functions/concepts), [check blocks](https://developer.hashicorp.com/terraform/language/block/check), [PostHog capture API](https://posthog.com/docs/api/capture).

## Development

Requires Go 1.25+, Terraform 1.8+, and Python 3 for the CLI smoke test.

```sh
go build -o bin/terraform-provider-telemetry .
go test -race -timeout 60s ./...
go vet ./...
python3 scripts/smoke_test.py
```

The smoke test builds in a temporary directory, uses a loopback HTTP server and a synthetic Git repository, and verifies plan, saved-plan apply, no-change apply, HTTP failure handling, typed extra data, Git metadata, the optional `options.cache_enabled` setting, absence of resource state, and deduplication of 100 `count` plus 100 `for_each` module instances (including bypass and per-instance keys). It disables GitHub/network collectors and never sends real telemetry to PostHog.

For manual development, create a separate CLI configuration file with an absolute path to the built binary directory:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/tedilabs/telemetry" = "/absolute/path/terraform-provider-telemetry/bin"
  }
  direct {}
}
```

Set `TF_CLI_CONFIG_FILE` to that file when running Terraform. A function-only example using a dev override can run directly without `terraform init`; initialization would still try to resolve a published version. Terraform prints its normal development override warning in this mode.

## Adding a destination

1. Add `capture_<destination>` in `internal/provider`, retaining three required positional arguments and the optional cache/deduplication settings in `options`. Define that destination's `connection` object, reuse `optionsParameter()`, `collectProperties()`, and the process deduplicator with a distinct destination/connection scope, and always return `true`.
2. Add the destination's sender in `internal/telemetry`; keep transport logic separate from the collectors in `collect.go`.
3. Register the constructor in `TelemetryProvider.Functions`, and add function docs, sender tests, and a local-only Terraform smoke scenario.

No generic backend registry or additional destination is implemented until one is needed.
