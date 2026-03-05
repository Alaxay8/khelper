package cmd

import "github.com/spf13/cobra"

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate shell completion script",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "bash",
			Short: "Generate bash completion script",
			RunE: func(cmd *cobra.Command, args []string) error {
				return writeShellCompletion(cmd.Root(), "bash", cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "zsh",
			Short: "Generate zsh completion script",
			RunE: func(cmd *cobra.Command, args []string) error {
				return writeShellCompletion(cmd.Root(), "zsh", cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "fish",
			Short: "Generate fish completion script",
			RunE: func(cmd *cobra.Command, args []string) error {
				return writeShellCompletion(cmd.Root(), "fish", cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "powershell",
			Short: "Generate PowerShell completion script",
			RunE: func(cmd *cobra.Command, args []string) error {
				return writeShellCompletion(cmd.Root(), "powershell", cmd.OutOrStdout())
			},
		},
	)

	return cmd
}
