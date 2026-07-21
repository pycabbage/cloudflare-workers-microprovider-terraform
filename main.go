package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/pycabbage/cloudflare-workers-microprovider-terraform/internal/provider"
)

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with debugger support")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{
		// terraform init で参照するアドレス。GitHub Pages 上の静的レジストリ
		// (Terraform provider registry protocol) で配布しているため、
		// 利用側 required_providers の source と完全に一致させる必要がある
		Address: "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
