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
		// terraform init で参照するアドレス。レジストリに公開しないなら
		// 適当なホスト名 + 自分の名前空間で問題ない
		Address: "registry.terraform.io/pycabbage/cloudflare-workers-microprovider",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
