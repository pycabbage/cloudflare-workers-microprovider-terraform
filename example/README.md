# Example usage

```shell
$ CLOUDFLARE_API_TOKEN=$(bunx wrangler auth token --json | jq -r '.token') terraform plan
data.cloudflare_account.this: Reading...
data.cloudflare_account.this: Still reading... [00m10s elapsed]
data.cloudflare_account.this: Read complete after 12s [id=00000000000000000000000000000000]
data.cfworkers_subdomain.this: Reading...
data.cfworkers_subdomain.this: Read complete after 0s

Changes to Outputs:
  + account_id = "00000000000000000000000000000000"
  + subdomain  = "octocat"

You can apply this plan to save these new output values to the Terraform state, without changing any real infrastructure.

───────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

Note: You didn't use the -out option to save this plan, so Terraform can't guarantee to take exactly these actions if you run "terraform apply" now.
```
