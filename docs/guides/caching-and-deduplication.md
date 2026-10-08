---
page_title: "Caching and Deduplication"
subcategory: ""
description: |-
  How the functions reuse collected metadata and skip duplicate events within a provider process.
---

# Caching and Deduplication

Terraform can evaluate a function call many times in one command,
for example once for each instance of a module created with `count` or `for_each`.
The functions of this provider limit the cost and the number of events with two independent mechanisms:

* The [collection cache](#collection-cache) reuses collected metadata, so commands and network requests are not repeated.
  It does not reduce the number of events.
* [Event deduplication](#event-deduplication) skips events that duplicate an event already sent,
  so repeated calls send fewer events.

Both are enabled by default, and are configured in the `options` argument of each call.

## Provider processes

Both mechanisms keep their state in memory, in the provider process.
Terraform starts provider processes on its own: for example, `terraform apply` can plan with one process
and apply with another. Each process starts with an empty cache and no recorded events,
so metadata is collected again and the same event can be sent once per process.

<a id="collection-cache"></a>
## Collection Cache

By default, each metadata group is collected once per provider process, when a call first needs it,
and later calls reuse it. Unavailable and partial results are reused too.
This avoids repeating the same commands and network requests, for example for each instance of a module.

* The cache is held in memory only, and is not shared between provider processes.
* Cached metadata can become outdated if the environment changes while the provider process runs.
* `extra_data`, connection settings, and events are never cached.
  Caching does not reduce the number of events; see [Event Deduplication](#event-deduplication).

Set `cache_enabled = false` in `options` to collect fresh metadata for a call. This does not update the cache.

<a id="event-deduplication"></a>
## Event Deduplication

By default, a call is skipped when the same provider process has already sent an event with identical properties,
compared after merging `extra_data`. Instances of a module that pass the same `extra_data` therefore send one event
per provider process, however many instances `count` or `for_each` creates.

Set `deduplication_keys` to compare only the selected properties, so that calls that differ in other properties
are still treated as duplicates. For example, the following call sends at most one event per module version,
even when instances pass different `engine` values. The event carries the `engine` of the first instance;
add `engine` to the keys to send one event per engine instead.

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
```

* Paths are relative to the event properties, such as `module` or `machine.os`, without a `properties.` prefix.
  Each dot-separated segment selects an attribute of an object or a key of a map.
  List indexes and keys that contain dots are not supported.
* A path can select a whole object or list. Values are compared with their types.
  The order of object keys does not matter, but the order of list elements does.
* If a selected path is missing, the event is sent without deduplication, and is not recorded.
  A `null` value that is present is compared like any other value.
* Events for different destinations, hosts, or project tokens are never treated as duplicates of each other.
* Only the first event is sent, and later duplicates are skipped even if its delivery failed.
  The properties that `deduplication_keys` does not select come from the first event.
  Among concurrent calls, which one is first is not defined.
* Only hashes of the compared values are held, in memory. They are not shared between provider processes.

Set `deduplication_enabled = false` in `options` to send every event. `cache_enabled` does not affect deduplication.
