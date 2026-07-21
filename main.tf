terraform {
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
    cfsubdomain = {
      source = "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider"
    }
  }
}

# どちらのproviderも設定ブロックは空でよい。
# 認証は両者とも CLOUDFLARE_API_TOKEN 等の環境変数から同じ規則で解決される。
provider "cloudflare" {}
provider "cfsubdomain" {}

variable "account_id" {
  type = string
}

data "cfsubdomain_workers_subdomain" "this" {
  account_id = var.account_id
}

output "worker_url" {
  value = "https://hello-world.${data.cfsubdomain_workers_subdomain.this.subdomain}.workers.dev"
}
