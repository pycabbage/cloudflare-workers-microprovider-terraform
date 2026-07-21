terraform {
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
    cfworkers = {
      source = "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider"
    }
  }
}

provider "cloudflare" {}
provider "cfworkers" {}

variable "account_id" {
  type = string
}

data "cfworkers_subdomain" "this" {
  account_id = var.account_id
}

output "worker_url" {
  value = "https://hello-world.${data.cfworkers_subdomain.this.subdomain}.workers.dev"
}
