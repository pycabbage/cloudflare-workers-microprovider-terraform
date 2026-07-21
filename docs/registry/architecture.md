# Registry architecture

This provider is not published to the public Terraform Registry
(registry.terraform.io). Instead it is distributed via GitHub Releases plus a
static implementation of the Terraform provider registry protocol served over
GitHub Pages.

## Resolution flow

```
terraform init
    |
    | 1. service discovery
    v
https://pycabbage.github.io/.well-known/terraform.json
    (user-site repo: pycabbage/pycabbage.github.io)
    |
    | 2. read the "providers.v1" base URL
    v
https://pycabbage.github.io/cloudflare-workers-microprovider-terraform/v1/providers/...
    (this repo's GitHub Pages: static JSON for the versions / download endpoints)
    |
    | 3. read download_url
    v
zip on GitHub Releases
    (terraform-provider-cloudflare-workers-microprovider_{VERSION}_{os}_{arch}.zip)
```

`terraform init` performs service discovery against the hostname in the
provider source address (`pycabbage.github.io`), which is why the
`.well-known/terraform.json` document must live in the `pycabbage.github.io`
user-site repository rather than in this repository. That document simply
tells Terraform where the `providers.v1` API for this specific provider type
lives, which is this repository's own GitHub Pages site.

This works identically whether Terraform is invoked from the CLI or from HCP
Terraform (Terraform Cloud), since both implement the same provider registry
protocol client.

## Registry protocol details

- Only protocol version `6.0` is advertised (`protocols: ["6.0"]`), matching
  the version implemented by `terraform-plugin-framework`.
- The registry site is fully regenerated from the complete list of GitHub
  Releases on every generator run. The GitHub Pages site itself is stateless:
  there is no incremental update and no persisted state between runs. Every
  run reads all releases from the GitHub API and rebuilds every JSON file
  from scratch.
- Each release is verified end to end: the generator downloads the
  `SHA256SUMS` asset for that release and cross-checks it against the
  platform zip assets before including the version. See
  `docs/registry/troubleshooting.md` for the exact skip/fail rules.
- Releases are signed with a GPG detached signature: `SHA256SUMS.sig` is a
  detached signature of `SHA256SUMS`, produced by GoReleaser during release
  and verified by Terraform against the public key advertised in the
  `signing_keys` field of each download response.
- The generator also writes an empty `.nojekyll` file into its own output
  directory (`_site`), alongside the `versions` and `download` JSON files.
  This is necessary because those JSON files are served without a file
  extension, and GitHub Pages' Jekyll build would otherwise exclude or
  mishandle extensionless files. This is separate from the `.nojekyll` file
  discussed in `docs/registry/setup.md` and
  `docs/registry/troubleshooting.md`, which must be placed manually in the
  `pycabbage.github.io` user-site repository for its own `.well-known/`
  directory.
- The generator does not produce a human-readable index page. The site's
  root page is a separate Next.js application in `web/`, built by CI and
  merged into `_site` before the generator runs; see
  `docs/registry/releasing.md` for how the two outputs are combined.

## Naming contract

- GoReleaser `project_name` (and therefore the asset name prefix) is
  `terraform-provider-cloudflare-workers-microprovider`. This differs from
  the repository name (`cloudflare-workers-microprovider-terraform`)
  because Terraform's naming convention for provider binaries and archives
  requires the `terraform-provider-` prefix.
- Platform archive: `{name}_{version}_{os}_{arch}.zip`, e.g.
  `terraform-provider-cloudflare-workers-microprovider_0.1.0_linux_amd64.zip`.
- Binary inside the archive: `{name}_v{version}`, e.g.
  `terraform-provider-cloudflare-workers-microprovider_v0.1.0`. This exact
  naming is required by Terraform's plugin discovery.
- Checksums and signature: `{name}_{version}_SHA256SUMS` and
  `{name}_{version}_SHA256SUMS.sig`.

## Target platforms

Exactly 5 platform combinations are built and published:

- linux/amd64
- linux/arm64
- darwin/amd64
- darwin/arm64
- windows/amd64

(`windows/arm64` is intentionally excluded.)

## Why terraform-registry-manifest.json is absent

The `terraform-registry-manifest.json` file (and the corresponding
GoReleaser `terraform_provider_extra_files` / manifest wiring some
scaffolds include) is only relevant when publishing to the official public
Terraform Registry, which parses that manifest during provider
registration. Since this provider is never registered there and is served
entirely through a self-hosted static implementation of the registry
protocol, that file would be inert and is intentionally omitted from both
the GoReleaser configuration and the release assets.

## Key ID format

The `key_id` published in each download response's `signing_keys` block is
the last 16 hex characters of the GPG key fingerprint, upper-cased (a GPG
"long key ID"). The generator accepts either a 16-hex key ID or a 40-hex
fingerprint as input and normalizes it to this form; see
`docs/registry/troubleshooting.md` for details.
