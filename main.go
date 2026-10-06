package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/tedilabs/terraform-provider-telemetry/internal/provider"
)

var version = "dev"

func main() {
	debug := flag.Bool("debug", false, "Run with debugger support")
	flag.Parse()
	if err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/tedilabs/telemetry",
		Debug:   *debug,
	}); err != nil {
		log.Fatal(err)
	}
}
