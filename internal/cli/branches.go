package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newBranchesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ghb <ORG>",
		Short: "Report every branch of every repo in a GitHub organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(*cobra.Command, []string) error {
			return errors.New("not implemented yet")
		},
	}
	setVersion(cmd)
	return cmd
}
