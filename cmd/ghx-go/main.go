// Command ghx-go syncs all GitHub repositories for an organization.
//
// The binary is named ghx-go so it can coexist on PATH with the Python
// `ghx` during side-by-side testing. Rename cmd/ghx-go/ to cmd/ghx/ once
// you're ready to ship it as the primary `ghx` binary.
package main

import (
	"os"

	"github.com/garyrudolph/ghx/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
