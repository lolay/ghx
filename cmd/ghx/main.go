// Command ghx syncs all GitHub repositories for an organization.
package main

import (
	"os"

	"github.com/lolay/ghx/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
