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
		Address: "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
