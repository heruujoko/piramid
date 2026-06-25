package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRetryCommand() *cobra.Command {
	var (
		flags    clientFlags
		override bool
	)
	command := &cobra.Command{
		Use:   "retry TASK_ID",
		Short: "Retry a failed task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := flags.client()
			if err != nil {
				return err
			}
			if err := client.RetryTask(cmd.Context(), args[0], override); err != nil {
				return connectionError(flags, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Retry queued for %s\n", args[0])
			return err
		},
	}
	command.Flags().BoolVar(&override, "override", false, "allow one attempt beyond the configured limit")
	flags.bind(command)
	return command
}
