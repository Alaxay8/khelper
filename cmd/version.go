package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version   = "0.1.0"
	BuildDate = "04 Mar 2026"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "version: %s\ncommit: %s\nbuildDate: %s\n", Version, Commit, BuildDate)
			return nil
		},
	}
}
