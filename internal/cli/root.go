package cli

import "github.com/spf13/cobra"

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "piramid",
		Short:         "Pi-Ramid AI work orchestrator",
		Long:          "Pi-Ramid schedules, executes, verifies, and records AI work delegated to Pi.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newInitCommand())
	cmd.AddCommand(newStartCommand())
	cmd.AddCommand(newGoalCommand())
	cmd.AddCommand(newEnqueueCommand())
	cmd.AddCommand(newQueueCommand())
	cmd.AddCommand(newWorkersCommand())
	cmd.AddCommand(newInspectCommand())
	cmd.AddCommand(newRetryCommand())
	cmd.AddCommand(newCancelCommand())
	cmd.AddCommand(newDoctorCommand())
	cmd.AddCommand(newTUICommand())
	return cmd
}
