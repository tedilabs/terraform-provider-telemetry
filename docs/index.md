---
page_title: "telemetry Provider"
description: |-
  Best-effort telemetry functions for Terraform execution environments.
---

# telemetry Provider

The Telemetry provider exposes `capture_<destination>(connection, options, extra_data)` functions. It manages no resources or data sources and requires no provider configuration. Terraform 1.8 or later is required.

Currently supported: [`capture_posthog`](functions/capture_posthog.md).

These functions intentionally perform side effects, which differs from Terraform's pure-function design. Delivery and invocation counts are not guaranteed. Use them only for best-effort telemetry.
