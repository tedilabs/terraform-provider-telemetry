---
page_title: "capture_posthog function - terraform-provider-telemetry"
description: |-
  Collect selected execution metadata, attempt a PostHog capture, and return true.
---

# function: capture_posthog

Sends a `terraform_capture` event to PostHog. Every function invocation returns `true`; this does not indicate delivery success. The function deliberately performs network side effects and does not guarantee exactly-once execution.

## Signature

```text
capture_posthog(connection object, options object, extra_data dynamic) bool
```

All three arguments are positional and required. The optional `options.cache_enabled` attribute defaults to `true`.

## Arguments

1. `connection`: `{ host = string, project_token = string }`. Use an ingestion base URL such as `https://us.i.posthog.com`. The function appends `/i/v0/e/`. Connection configuration is supplied here, not in a `provider` block.
2. `options`: An object or map with required boolean attributes `machine`, `network`, `git`, `github`, and `github_actions`. Enable only the desired collectors. Optional `cache_enabled` defaults to `true`; set it to `false` to bypass metadata cache reads and writes.
3. `extra_data`: An object or map with arbitrary JSON-compatible values. Nested structures, strings, numbers, booleans, and null are preserved under `properties.extra_data`. Pass `{}` when empty.

## Example

```hcl
check "telemetry" {
  assert {
    condition = provider::telemetry::capture_posthog(
      { host = "https://us.i.posthog.com", project_token = var.posthog_project_token },
      { machine = true, network = false, git = false, github = false, github_actions = false },
      { workspace = terraform.workspace }
    )
    error_message = "Telemetry invocation failed."
  }
}
```

Declare `tedilabs/telemetry` in `required_providers` and declare the referenced token variable before using this example. Mark the token variable sensitive.

## Collected data

| Option | Properties |
| --- | --- |
| `machine` | OS, provider architecture, CPU count |
| `network` | Hostname, local non-loopback interface IPs |
| `git` | Repository basename, branch, commit, sanitized origin URL |
| `github` | Authenticated `gh` user's login, ID, name, profile URL |
| `github_actions` | Workflow, job key, run identifiers, repository, actor, refs, runner information, run URL |

Collectors use the Terraform process's execution environment. Disabled collectors do not run. Unavailable Git repositories/tools/authentication are skipped. GitHub Actions metadata is only included when `GITHUB_ACTIONS=true`. No public-IP lookup, GitHub login, token collection, or full environment dump is performed.

## Behavior

Each capture has a fresh UUIDv4 `distinct_id`; person profile processing and GeoIP enrichment are disabled. No stable user identity is inferred. Collected metadata and extra data occupy separate namespaces.

Null connection/options, unknown data, invalid connection contents, and non-object extra data skip capture and return true. Null `extra_data` becomes `{}`. Terraform itself can still reject malformed argument types or fail to load the provider.

There is a five-second collection/delivery budget and two-second timeouts for commands and HTTP requests. No retry or redirect is performed. Capture failures do not produce function errors or warning diagnostics. A passing check produces no normal check-specific CLI output; check status metadata remains in Terraform state.

## Collection cache

The explicitly allowlisted `machine`, `network`, `git`, `github`, and `github_actions` groups are collected lazily once per provider process by default, including unavailable/partial results. Concurrent calls and separate function instances share the cache. Disabled groups are neither collected nor included from cache. Cached metadata is a snapshot and may become stale if the execution environment changes.

`extra_data`, connection information, event IDs, and event delivery are never cached. Every eligible call still attempts an event; this feature does not reduce PostHog event counts. Cache storage is memory-only and separate provider processes do not share it, including when Terraform restarts providers between plan and apply.

Set `cache_enabled = false` inside `options` to bypass the cache for a call. This does not replace cached values. Omitting `cache_enabled` enables caching; an explicit null, unknown, or non-boolean value skips capture and returns `true`. No fourth argument is accepted.
