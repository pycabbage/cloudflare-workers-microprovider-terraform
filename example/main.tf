terraform {
  required_version = ">= 1.15"
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5"
    }
    cfworkers = {
      source  = "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider"
      version = "0.0.0-alpha.2"
    }
  }
}

provider "cloudflare" {}
provider "cfworkers" {}

data "cloudflare_account" "this" {
  filter = {}
}

data "cfworkers_subdomain" "this" {
  account_id = data.cloudflare_account.this.account_id
}

output "account_id" {
  value = data.cloudflare_account.this.account_id
}
output "subdomain" {
  value = data.cfworkers_subdomain.this.subdomain
}
