//go:build tools
package tools

import (
  _ "golang.org/x/tools/cmd/goimports"
  _ "github.com/golangci/golangci-lint/cmd/golangci-lint"
  _ "gotest.tools/gotestsum"
  _ "golang.org/x/vuln/cmd/govulncheck"
)
