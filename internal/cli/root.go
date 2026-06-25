package cli

import "github.com/spf13/cobra"

func NewRootCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "piramid",
		Short:         "Pi-Ramid AI work orchestrator",
		Long:          "Pi-Ramid schedules, executes, verifies, and records AI work delegated to Pi.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
}
