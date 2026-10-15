//go:build tools

// Tool dependencies tracked for `go generate` - never built into the binary.
package main

import (
	_ "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
)
