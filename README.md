# cloudflare-workers-microprovider-terraform

A small, purpose-built Terraform provider that reads a Cloudflare account's
`workers.dev` subdomain. It is not published to the public Terraform
Registry; instead it is distributed via GitHub Releases plus a static,
self-hosted implementation of the Terraform provider registry protocol
served from this repository's GitHub Pages. Running `terraform init` is all
a consumer needs to do to fetch it, from the CLI or from HCP Terraform.

## Usage

```hcl
terraform {
  required_providers {
    cfworkers = {
      source = "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider"
    }
  }
}

provider "cfworkers" {}

data "cfworkers_subdomain" "this" {
  account_id = var.account_id
}
```

See `docs/provider/usage.md` for authentication details and requirements.

## Documentation

- `docs/provider/usage.md` - provider usage, resources/data sources, auth
- `docs/registry/architecture.md` - how the static registry distribution works
- `docs/registry/setup.md` - one-time setup for a new deployment
- `docs/registry/releasing.md` - how to cut a release
- `docs/registry/troubleshooting.md` - diagnosing init and generator failures
