---
page_title: "Collected Metadata"
subcategory: ""
description: |-
  The metadata that each collector adds to an event, where it comes from, and which properties identify people or machines.
---

# Collected Metadata

The `options` argument of each function selects metadata collectors.
Each enabled collector adds a metadata group, named after its option, to the event properties.
This guide lists the properties of each group and where they come from.

| Option | Default | Source | Identifying properties |
| --- | --- | --- | --- |
| [`machine`](#machine) | `true` | Operating system and provider binary | None |
| [`network`](#network) | `true` | Operating system and [ipify](https://www.ipify.org/) | `hostname`, `public_ip` |
| [`git`](#git) | `true` | Git repository of the working directory | `remote`, and the repository and branch names |
| [`github`](#github) | `false` | GitHub CLI | `login`, `id`, `name`, `html_url` |
| [`github_actions`](#github_actions) | `true` | GitHub Actions environment variables | `actor`, `actor_id`, `triggering_actor`, and the repository |
| [`terraform`](#terraform) | `true` | Terraform CLI workspace selection | The workspace name |
| [`toolchain`](#toolchain) | `true` | Version commands of tools in `PATH` | None |

Set a collector to `false` in `options` to disable it, and disable the collectors you do not need.
Metadata describes the environment running Terraform: with remote execution, such as HCP Terraform
or a CI/CD pipeline, it describes the remote runner, not the workstation of the person who started the run.

Values that are not available are omitted, for example when a command is not installed,
authentication is not configured, or the working directory is not in a Git repository.
A failing collector does not affect the other collectors.
Collectors never log in, prompt, or change the configuration of Git or the GitHub CLI.

<a id="machine"></a>
## machine

- `os.name` (String) `macOS`, `Windows`, or the `NAME` of the Linux distribution from `os-release`.
  Other platforms report the Go operating system name, such as `freebsd`.
- `os.version` (String) macOS product version, Linux `VERSION_ID`, or Windows `major.minor.build` version.
- `arch` (String) CPU architecture of the provider binary, such as `amd64` or `arm64`.
- `cpu_count` (Number) Number of logical CPUs.
- `memory_size` (Number) Total physical memory in MiB, rounded down. This is not free memory or a container memory limit.

<a id="network"></a>
## network

- `hostname` (String) Host name reported by the operating system.
- `public_ip` (String) Public IPv4 or IPv6 address of the outbound connection, as observed by [ipify](https://www.ipify.org/).

The public IP address is looked up with an unauthenticated HTTPS request to `https://api64.ipify.org`,
which carries no event properties or credentials. The result reflects NAT, VPNs, and proxies,
and can differ from the address PostHog observes. Local network interfaces are never enumerated.

<a id="git"></a>
## git

Collected from the Git repository that contains the working directory of Terraform.

- `name` (String) Name of the repository root directory.
- `branch` (String) Current branch.
- `commit` (String) Current commit SHA.
- `remote` (String) URL of the `origin` remote, without user information, query, or fragment.
  Remotes on the local file system are omitted.

<a id="github"></a>
## github

Collected with the GitHub CLI (`gh api user`) and its existing authentication.
Tokens and email addresses are never collected.

- `login` (String) Login of the authenticated user.
- `id` (Number) ID of the authenticated user.
- `name` (String) Display name of the authenticated user.
- `html_url` (String) Profile URL of the authenticated user.
- `account_type` (String) Account type, such as `User` or `Bot`.

<a id="github_actions"></a>
## github_actions

Collected only when the `GITHUB_ACTIONS` environment variable is `true`.
Each property is read from a default environment variable of GitHub Actions:

| Property | Environment variable |
| --- | --- |
| `workflow` | `GITHUB_WORKFLOW` |
| `workflow_ref` | `GITHUB_WORKFLOW_REF` |
| `workflow_sha` | `GITHUB_WORKFLOW_SHA` |
| `job` | `GITHUB_JOB` |
| `run_id` | `GITHUB_RUN_ID` |
| `run_number` | `GITHUB_RUN_NUMBER` |
| `run_attempt` | `GITHUB_RUN_ATTEMPT` |
| `repository` | `GITHUB_REPOSITORY` |
| `repository_id` | `GITHUB_REPOSITORY_ID` |
| `repository_owner` | `GITHUB_REPOSITORY_OWNER` |
| `actor` | `GITHUB_ACTOR` |
| `actor_id` | `GITHUB_ACTOR_ID` |
| `triggering_actor` | `GITHUB_TRIGGERING_ACTOR` |
| `event_name` | `GITHUB_EVENT_NAME` |
| `ref` | `GITHUB_REF` |
| `sha` | `GITHUB_SHA` |
| `head_ref` | `GITHUB_HEAD_REF` |
| `base_ref` | `GITHUB_BASE_REF` |
| `server_url` | `GITHUB_SERVER_URL` |
| `runner_os` | `RUNNER_OS` |
| `runner_arch` | `RUNNER_ARCH` |
| `runner_environment` | `RUNNER_ENVIRONMENT` |

`run_url` is built from `GITHUB_SERVER_URL`, `GITHUB_REPOSITORY`, and `GITHUB_RUN_ID`.
`job` is the job ID in the workflow file, not a numeric ID. Matrix values are not collected;
pass them in `extra_data` when needed.

<a id="terraform"></a>
## terraform

- `command_id` (String) Identifier of the Terraform or OpenTofu command that started the provider process:
  a hash of the ID and start time of the parent process, which does not reveal either.
  The planning and applying phases of one `terraform apply` share it, while separate commands,
  such as `terraform plan -out` and the `terraform apply` of the saved plan, have different identifiers.
  Group events by it to count commands instead of events. Omitted on platforms other than Linux, macOS, and Windows.
- `workspace` (String) Selected Terraform CLI workspace.
- `workspace_source` (String) Where the workspace was read from: `environment` for the `TF_WORKSPACE` environment variable,
  `data_directory` for the `environment` file in the data directory (`TF_DATA_DIR`, or `.terraform` by default),
  or `default` when the data directory has no `environment` file.

This is the workspace selected in the local CLI, which is not necessarily the HCP Terraform workspace.
To send the value of `terraform.workspace` instead, pass it in `extra_data`.
State, backend configuration, and variable values are never read.

<a id="toolchain"></a>
## toolchain

- `terraform` (String) Version reported by `terraform version -json`.
- `opentofu` (String) Version reported by `tofu version -json`.
- `git` (String) Version reported by `git --version`.
- `github_cli` (String) Version reported by `gh --version`.
- `telemetry_provider` (String) Version of this provider.

The versions are of the tools found in `PATH`, which might differ from the executable running this provider.
They do not show whether the current run uses Terraform or OpenTofu.
The version commands run with `CHECKPOINT_DISABLE=1`, which disables the update checks of HashiCorp tools.
