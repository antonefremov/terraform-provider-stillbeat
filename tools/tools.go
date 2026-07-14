//go:build tools

// Package tools pins the code-generation tooling (tfplugindocs) as a module
// dependency so `go generate ./...` and CI use a reproducible version. It is
// never compiled into the provider binary (build tag `tools`).
package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)
