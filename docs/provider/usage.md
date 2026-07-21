# Provider usage

## Requiring the provider

```hcl
terraform {
  required_providers {
    cfworkers = {
      source = "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider"
    }
  }
}

provider "cfworkers" {}
```

The local name `cfworkers` is arbitrary (you can choose any name in
`required_providers`), but the resource/data source prefix documented below
always matches the provider's TypeName, `cfworkers`, regardless of the
local name you pick.

Note that the registry type name in the source address
(`cloudflare-workers-microprovider`) intentionally differs from the
provider's TypeName (`cfworkers`), and therefore from the resource/data
source prefix. The type in a provider source address and a provider's
TypeName are independent concepts in Terraform, and this provider
deliberately uses different values for each.

## Resources and data sources

- All resources and data sources use the prefix `cfworkers_`.
- Currently there is one data source: `cfworkers_subdomain`.

```hcl
data "cfworkers_subdomain" "this" {
  account_id = var.account_id
}
```

Input: `account_id` (required) - the Cloudflare account identifier.

Output: `subdomain` (computed) - the account's `workers.dev` subdomain (the
`octocat` in `hello-world.octocat.workers.dev`). Internally this calls
`GET /accounts/{account_id}/workers/subdomain`.

## Authentication

Neither the `cloudflare` provider nor `cfworkers` needs any configuration
in the `provider` block - both blocks can be empty:

```hcl
provider "cloudflare" {}
provider "cfworkers" {}
```

Authentication is resolved from environment variables using exactly the same
rules as the official `cloudflare` provider, because this provider calls
`cloudflare.NewClient()` from the same Cloudflare Go SDK, which reads:

- `CLOUDFLARE_API_TOKEN`, or
- `CLOUDFLARE_API_KEY` together with `CLOUDFLARE_EMAIL`, or
- `CLOUDFLARE_API_USER_SERVICE_KEY`

The SDK itself decides which of these to use; this provider does not
implement any of that branching logic itself.

## Requirements

- Terraform >= 1.1 (this provider implements plugin protocol version 6,
  which requires Terraform 1.1 or later).

## Implementation note

In `main.go`, `providerserver.ServeOpts.Address` is set to
`pycabbage.github.io/pycabbage/cloudflare-workers-microprovider`. This value
must always exactly match the `source` used in consumers'
`required_providers` blocks, since Terraform uses it to validate that the
plugin binary it launched actually serves the provider it asked for.
