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
- There is currently one data source: `cfworkers_subdomain`.

```hcl
data "cfworkers_subdomain" "this" {
  account_id = var.account_id
}
```

Input: `account_id` (required) - the Cloudflare account identifier.

Output: `subdomain` (computed) - the account's `workers.dev` subdomain (the
`octocat` in `hello-world.octocat.workers.dev`). Internally this calls
`GET /accounts/{account_id}/workers/subdomain`.

## Functions

See the "Requirements" section below for the Terraform CLI version needed
to call provider-defined functions such as `jsonc_decode`.

### `jsonc_decode`

`provider::cfworkers::jsonc_decode(content)` parses JSON-with-comments
(JSONC) text - the format Cloudflare's `wrangler.jsonc` uses, which extends
JSON with `//` line comments, `/* */` block comments, and trailing commas -
and returns a genuine decoded value, just like the built-in `jsondecode()`
does for plain JSON. Unlike the removed `cfworkers_jsonc` data source this
replaced, there's no intermediate JSON string and no second `jsondecode()`
call: the function's return value is already an object/array/string/
number/bool/null tree you can navigate directly with `.attr` / `[index]`
syntax.

This function does no file I/O of its own - pass it text you've already
read, typically with Terraform's own built-in `file()` function:

```hcl
locals {
  wrangler_config = provider::cfworkers::jsonc_decode(file("${path.module}/wrangler.jsonc"))
}

output "worker_name" {
  value = local.wrangler_config.name
}
```

Parameter: `content` (required, string) - JSONC text to decode.

Return: a dynamic value - the decoded JSON structure. JSON objects become
Terraform object values and JSON arrays become Terraform tuple values
rather than lists, since JSON arrays may mix element types positionally
(the same reason `jsondecode()`/`cty` model JSON arrays as tuples
internally); this is an implementation detail rather than something you
typically need to think about when just reading fields back out.

This function needs no authentication at all - unlike `cfworkers_subdomain`
above, it makes no API calls, requires no credentials of any kind, and
(being a function rather than a data source) persists nothing to Terraform
state.

## Authentication

Neither the `cloudflare` provider nor `cfworkers` needs any configuration
in the `provider` block - both blocks can be empty. (Only `cfworkers_subdomain`
above actually uses Cloudflare credentials; `jsonc_decode` needs none, as
noted above.)

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
  which requires Terraform 1.1 or later) to use `cfworkers_subdomain`.
- A sufficiently recent Terraform CLI to call the `jsonc_decode` provider
  function - provider-defined functions are a newer Terraform CLI feature
  than plugin protocol 6 itself. This repository's own `example/main.tf`
  already requires `>= 1.15`, so building/testing against that example is a
  safe floor; if you're integrating `jsonc_decode` into other
  configurations, check your Terraform CLI's release notes for
  provider-defined function support if you need to support an older
  version.

## Implementation note

In `main.go`, `providerserver.ServeOpts.Address` is set to
`pycabbage.github.io/pycabbage/cloudflare-workers-microprovider`. This value
must always exactly match the `source` used in consumers'
`required_providers` blocks, since Terraform uses it to validate that the
plugin binary it launched actually serves the provider it asked for.
