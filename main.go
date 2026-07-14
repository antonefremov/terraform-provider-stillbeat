// terraform-provider-stillbeat is the Stillbeat Terraform provider entry
// point. It serves the provider over the plugin protocol; Terraform launches it.
package main

// Generate the registry documentation under docs/ from the provider schema +
// examples/. Run with `go generate ./...` or `make docs` (needs terraform).
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name stillbeat

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/antonefremov/terraform-provider-stillbeat/internal/provider"
)

// version is set by the release build (GoReleaser ldflags); "dev" locally.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/antonefremov/stillbeat",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
