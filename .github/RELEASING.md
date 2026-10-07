# Publishing tedilabs/telemetry

Source: `github.com/tedilabs/terraform-provider-telemetry` (public).
Registry address: `registry.terraform.io/tedilabs/telemetry`.
Requires Terraform 1.8+; the provider serves protocol 6.

## 1. Merge the preparation stack

Repository foundation (#6) and provider tests (#1) are merged. Merge the remaining PRs in order: Registry packaging (#2) → signed release workflow and this guide (#3) → dependency security fixes (#4).
The packaging PR targets `main`; the remaining PRs target the preceding branch. After merging a parent, retarget the next PR to `main` and verify its diff. Prefer merge commits for this initial stack; squashing a parent requires rebasing descendants to avoid replaying its changes. Do not delete a parent branch until its child has been retargeted.

Wait for all checks to pass, including native Go tests on Linux/macOS/Windows, `terraform-plugin-testing` acceptance tests with Terraform 1.8.5/1.15.6, and the six-platform package check. Review `docs/index.md` and `docs/functions/capture_posthog.md` using the [Registry documentation preview](https://registry.terraform.io/tools/doc-preview). Documentation is read from the released tag.

## 2. Configure the release signing key once

Use a dedicated RSA signing key for the provider, or an existing organization-managed RSA release key. HashiCorp's publishing documentation currently excludes ECC keys. Keep the key's passphrase and backup in the organization's secret management system.

To create a new key interactively, choose RSA (sign only), 4096 bits, the organization's release identity, and an appropriate expiry:

```sh
gpg --full-generate-key
gpg --list-secret-keys --keyid-format LONG
```

Set `KEY_FINGERPRINT` locally to the selected full fingerprint. Export only the public key to a file:

```sh
export KEY_FINGERPRINT='YOUR_FULL_RELEASE_KEY_FINGERPRINT'
gpg --armor --export "$KEY_FINGERPRINT" > telemetry-release-public.asc
```

Sign into [Terraform Registry](https://registry.terraform.io/) with the account that administers `tedilabs`. In signing key settings, select the **tedilabs namespace** and add the ASCII-armored public key. Ensure the Registry's GitHub integration can access this organization and administer the repository's release webhook; organization OAuth/SSO restrictions may require organization approval.

Configure repository Actions secrets without writing the private key to the repository:

```sh
# Run in bash/zsh; do not enable shell tracing.
set -o pipefail
gpg --armor --export-secret-keys "$KEY_FINGERPRINT" \
  | gh secret set GPG_PRIVATE_KEY --repo tedilabs/terraform-provider-telemetry
gh secret set PASSPHRASE --repo tedilabs/terraform-provider-telemetry
```

`PASSPHRASE` prompts for the key's passphrase. GitHub supplies `GITHUB_TOKEN` automatically. The workflow obtains `GPG_FINGERPRINT` from the imported key. Confirm Actions is enabled and permits the pinned actions in `.github/workflows/release.yaml`.

## 3. Publish the first signed GitHub release

After the stack is merged and the signing key is configured, choose the initial version (for example `v0.1.0`):

```sh
git switch main
git pull --ff-only origin main
git tag v0.1.0
git push origin v0.1.0
gh run list --repo tedilabs/terraform-provider-telemetry --workflow release.yaml
```

A `v*` tag triggers the release workflow. It requires the tagged commit to be on `main`, runs tests, builds all six platforms, signs checksums, then publishes a GitHub Release. Do not push a release tag before the required secrets are configured.

For `v0.1.0`, the release must have these nine assets:

- Six `terraform-provider-telemetry_0.1.0_<os>_<arch>.zip` files for Linux, macOS (`darwin`), and Windows, each on amd64 and arm64.
- `terraform-provider-telemetry_0.1.0_manifest.json` with `protocol_versions: ["6.0"]`.
- `terraform-provider-telemetry_0.1.0_SHA256SUMS`, covering the ZIPs and manifest.
- `terraform-provider-telemetry_0.1.0_SHA256SUMS.sig`, a detached GPG signature.

Each ZIP contains the versioned provider binary (with `.exe` on Windows) and `LICENSE`. Download assets into a fresh temporary directory and verify them with the public signing key imported:

```sh
gh release download v0.1.0 --repo tedilabs/terraform-provider-telemetry
gpg --verify terraform-provider-telemetry_0.1.0_SHA256SUMS.sig \
  terraform-provider-telemetry_0.1.0_SHA256SUMS
shasum -a 256 -c terraform-provider-telemetry_0.1.0_SHA256SUMS
```

Do not replace a published version's tag or assets. Release a new patch version for corrections.

## 4. Register the provider once

Open **Publish → Provider** in Terraform Registry and select `tedilabs/terraform-provider-telemetry`. Follow the account/namespace prompts and complete registration after the signed release exists. The Registry creates a GitHub release webhook to ingest future releases.

Confirm version `0.1.0`, protocol 6, all six platform downloads, the provider overview, and the `capture_posthog` function page are visible. If ingestion fails, check the webhook delivery, signing key namespace, signature, checksums, release visibility, and asset names. Use the Registry provider settings' resync facility if needed.

## 5. Verify installation from the public Registry

In a fresh directory, create `main.tf` with no development override:

```hcl
terraform {
  required_version = ">= 1.8.0"
  required_providers {
    telemetry = {
      source  = "tedilabs/telemetry"
      version = "= 0.1.0"
    }
  }
}
```

Use a dedicated CLI configuration containing only `provider_installation { direct {} }`, set `TF_CLI_CONFIG_FILE` to it, then run `terraform init` and `terraform providers schema -json`. Confirm signature verification succeeds and `capture_posthog` appears under the provider's functions. This installation check sends no telemetry. For end-to-end capture, use the repository's example with a test PostHog project.

For later versions, repeat steps 3 and 5; the webhook handles Registry ingestion automatically.

## Local packaging check

```sh
mise install go
goreleaser check
goreleaser release --snapshot --clean --skip=sign,publish
go run ./scripts/verify_release.go
```

Use the Go `toolchain` declared in `go.mod` and GoReleaser 2.17.0, matching CI. Run the commands with mise activated, and set `GOTOOLCHAIN=local` to preserve the selected Go version. Snapshot checks require neither signing secrets nor Registry access; they do not validate the production signing key or perform registration.

References: [HashiCorp publishing requirements](https://developer.hashicorp.com/terraform/registry/providers/publishing), [provider documentation format](https://developer.hashicorp.com/terraform/registry/providers/docs), [GoReleaser configuration](https://goreleaser.com/customization/).
