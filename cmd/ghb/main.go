// Command ghb reports every branch of every repository in a GitHub
// organization, read-only, from the GitHub API.
package main

import (
	"os"

	"github.com/lolay/ghx/internal/cli"
)

func main() {
	os.Exit(cli.ExecuteBranches())
}
