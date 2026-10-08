# Releasing

Releases are signed GitHub releases that the [Terraform Registry](https://registry.terraform.io/providers/tedilabs/telemetry)
ingests through the release webhook it created on this repository.
The Registry publishes the documentation of each version from its release tag.

## Releasing a version

1. Choose the next version following [semantic versioning](https://semver.org/),
   and confirm that the checks on `main` pass.
2. Tag the release commit on `main` and push the tag:

   ```sh
   git switch main
   git pull --ff-only origin main
   git tag vX.Y.Z
   git push origin vX.Y.Z
   gh run list --repo tedilabs/terraform-provider-telemetry --workflow release.yaml
   ```

   The `v*` tag triggers the release workflow. It requires the tagged commit to be on `main`,
   runs the Go tests, `go vet`, govulncheck, and the acceptance tests, builds the six platform packages,
   signs the checksums, and publishes the GitHub release.
3. [Verify the release assets](#verifying-release-assets).
4. Confirm that the Registry shows the new version with all six platforms and the documentation pages,
   then [verify the installation](#verifying-the-installation).

Never replace the tag or the assets of a published version. Release a new patch version for corrections.

## Verifying release assets

A release `X.Y.Z` has nine assets:

- Six `terraform-provider-telemetry_X.Y.Z_<os>_<arch>.zip` packages for Linux, macOS (`darwin`), and Windows,
  each on `amd64` and `arm64`. Each package contains the versioned provider binary and `LICENSE`.
- `terraform-provider-telemetry_X.Y.Z_manifest.json`, declaring protocol version 6.
- `terraform-provider-telemetry_X.Y.Z_SHA256SUMS`, covering the packages and the manifest.
- `terraform-provider-telemetry_X.Y.Z_SHA256SUMS.sig`, a detached GPG signature of the checksums.

Download the assets into an empty directory and verify them, with the public signing key imported:

```sh
gh release download vX.Y.Z --repo tedilabs/terraform-provider-telemetry
gpg --verify terraform-provider-telemetry_X.Y.Z_SHA256SUMS.sig terraform-provider-telemetry_X.Y.Z_SHA256SUMS
shasum -a 256 -c terraform-provider-telemetry_X.Y.Z_SHA256SUMS
```

## Verifying the installation

In an empty directory, create a configuration that requires the new version:

```hcl
terraform {
  required_providers {
    telemetry = {
      source  = "tedilabs/telemetry"
      version = "= X.Y.Z"
    }
  }
}
```

With a Terraform CLI configuration that contains only `provider_installation { direct {} }` in `TF_CLI_CONFIG_FILE`,
run `terraform init` and `terraform providers schema -json`.
Confirm that the signature verification succeeds and that `capture_posthog` appears under the functions of the provider.
This check sends no telemetry.

## Troubleshooting Registry ingestion

If a release does not appear on the Registry, check the delivery of the release webhook,
the signing key of the `tedilabs` namespace, the signature and checksums, and the asset names.
Use the **Resync** button in the provider settings on the Registry if the webhook is missing or not working.

## Signing key

The release workflow imports the GPG key from the `GPG_PRIVATE_KEY` and `PASSPHRASE` Actions secrets,
and passes its fingerprint to GoReleaser. The public key is registered in the `tedilabs` namespace on the Registry.

To replace the key, for example before it expires:

1. Create an RSA key; the Registry does not accept the default ECC key type.
   Keep the passphrase and a backup in the organization's secret management system.
2. Add the ASCII-armored public key to the `tedilabs` namespace in the signing key settings of the Registry.
3. Replace the Actions secrets without writing the private key to disk:

   ```sh
   # Run in bash or zsh, without shell tracing.
   set -o pipefail
   gpg --armor --export-secret-keys "$KEY_FINGERPRINT" \
     | gh secret set GPG_PRIVATE_KEY --repo tedilabs/terraform-provider-telemetry
   gh secret set PASSPHRASE --repo tedilabs/terraform-provider-telemetry
   ```

## Checking packages locally

The Package workflow builds snapshot packages on every pull request. To reproduce it locally,
use the Go version from `go.mod` with `GOTOOLCHAIN=local`, and the GoReleaser version pinned in the workflows:

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=sign,publish
go run ./scripts/verify_release.go
```

Snapshot packages need neither the signing key nor access to the Registry.

References: [publishing providers](https://developer.hashicorp.com/terraform/registry/providers/publishing),
[GoReleaser configuration](https://goreleaser.com/customization/).
