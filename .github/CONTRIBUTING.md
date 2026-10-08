# Contributing

Thank you for investing your time and energy by contributing to this project: please ensure you are familiar
with the tedilabs [Code of Conduct](https://github.com/tedilabs/.github/blob/main/.github/CODE_OF_CONDUCT.md).

The functions of this provider run inside other people's Terraform configurations and modules,
and send data about the environments running them. Any bug fix and feature has to be considered in that context:
a function must never make a Terraform run fail, and new metadata must stay opt-in. _Stability and privacy over features_.

This provider follows [semantic versioning](https://semver.org/).

## Reporting Vulnerabilities

Please do not open an issue for a security problem. Follow the tedilabs
[security policy](https://github.com/tedilabs/.github/blob/main/.github/SECURITY.md) instead.

## Raising Issues

We welcome bug reports, feature requests, and documentation suggestions.

### Bug Reports

* [ ] **Test against the latest release** of Terraform and the provider; the bug may already be fixed.
* [ ] **Search for duplicates** among the existing issues.
* [ ] **Include steps to reproduce**: the Terraform and provider versions, the function call,
  and the event you expected compared to the event you received.
  Remove project tokens and other identifying data before sharing.

### Feature Requests

* [ ] **Search for duplicates** among the existing issues.
* [ ] **Describe the use case**: why the feature matters, in addition to how it should behave.

## New Pull Request

We are happy to review pull requests without associated issues,
but we **highly recommend** discussing your problem or feature in an issue first.

* [ ] **Tests**: Every change should be covered by tests wherever possible.
  For bug fixes, tests to prove the fix is valid. For features, tests to exercise the new code paths.
* [ ] **Documentation**: Update the documentation sources and [regenerate the documentation](#generating-documentation).
* [ ] **Commit messages**: Follow [Conventional Commits](https://www.conventionalcommits.org/).
  The [pre-commit](https://pre-commit.com/) hooks in this repository check commit messages and formatting.
* [ ] **Go Modules**: Reflect dependency changes in your pull request with `go mod tidy`.
  Routine dependency updates are handled by [Dependabot](https://docs.github.com/en/code-security/dependabot).

## Development

### Requirements

* [Go](https://go.dev/doc/install) (the version declared in `go.mod`)
* [Terraform](https://developer.hashicorp.com/terraform/install) (>= 1.8) for the acceptance tests
* [mise](https://mise.jdx.dev/) (recommended) to install the versions of Go, Terraform,
  and [terraform-plugin-docs](https://github.com/hashicorp/terraform-plugin-docs) pinned in `mise.toml`

### Building

```sh
mise install
go build -o bin/terraform-provider-telemetry .
```

### Testing

```sh
go test -race -timeout 60s ./...
go vet ./...
TF_ACC=1 TF_ACC_TERRAFORM_PATH="$(mise which terraform)" go test -race -run '^TestAcc' -count=1 -timeout 10m ./internal/provider
```

The acceptance tests use `terraform-plugin-testing` and a loopback HTTP server, so no event reaches a real PostHog project.
They build the current provider into a temporary file system mirror, so Terraform launches real provider processes
for each plan and saved-plan apply without downloading a published provider.
This preserves the process boundaries of the collection cache and event deduplication;
in-process provider factories would share those globals across Terraform commands.

Normal `go test` skips the acceptance tests unless `TF_ACC=1` is set.
Set `TF_ACC_TERRAFORM_PATH` to the Terraform binary to test. CI runs them with Terraform 1.8.5 and 1.16.5.

### Using a development build

Create a separate Terraform CLI configuration file with
[development overrides](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers)
that point to the absolute path of the directory containing the built binary:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/tedilabs/telemetry" = "/absolute/path/terraform-provider-telemetry/bin"
  }
  direct {}
}
```

Set `TF_CLI_CONFIG_FILE` to that file when running Terraform, which then uses the development build
and prints a warning about development overrides.
A configuration without other providers or modules can run without `terraform init`.

### Generating documentation

This provider uses [terraform-plugin-docs](https://github.com/hashicorp/terraform-plugin-docs)
to generate the documentation in the `docs/` directory. Do not edit `docs/` directly; edit the sources instead:

* Function definitions in `internal/provider` for the summaries, descriptions, signatures, and arguments.
* `examples/provider/provider.tf` for the example on the provider overview page,
  `examples/functions/<name>/` for the function examples, and `examples/guides/<name>/` for the guide examples.
* `templates/` for the overview, function, and guide pages.
  The nested schemas of function arguments, such as `connection` and `options`, are written by hand in the function templates.

Then regenerate and validate the documentation, and commit `docs/` with the source changes:

```sh
mise exec -- tfplugindocs generate --provider-name telemetry
mise exec -- tfplugindocs validate --provider-name telemetry
```

CI regenerates and validates the documentation, and rejects uncommitted differences or new generated files.
Use the [Terraform Registry Doc Preview Tool](https://registry.terraform.io/tools/doc-preview) to check how a page renders.
The Terraform Registry publishes the documentation of a version from its release tag,
so documentation changes appear on the Registry only after the next release.

### Adding a destination

1. Add `capture_<destination>` in `internal/provider`, keeping the three required positional arguments
   and the optional cache and deduplication settings in `options`. Define the `connection` object of the destination,
   reuse `optionsParameter()`, `collectProperties()`, and the process deduplicator with a distinct destination
   and connection scope, and always return `true`.
2. Add the sender of the destination in `internal/telemetry`; keep transport logic separate from the collectors in `collect.go`.
3. Register the constructor in `TelemetryProvider.Functions`, and add sender tests, a local-only Terraform acceptance scenario,
   a documentation template, and examples. Regenerate the documentation.

No generic backend registry or additional destination is implemented until one is needed.

## Releasing

See the [release guide](RELEASING.md) for releasing a version, verifying it, and replacing the signing key.
