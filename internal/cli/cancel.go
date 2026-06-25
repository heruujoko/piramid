package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCancelCommand() *cobra.Command {
	var flags clientFlags
	command := &cobra.Command{
		Use:   "cancel TASK_ID",
		Short: "Cancel active or pending work",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := flags.client()
			if err != nil {
				return err
			}
			if err := client.CancelTask(cmd.Context(), args[0]); err != nil {
				return connectionError(flags, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Cancelled %s\n", args[0])
			return err
		},
	}
	flags.bind(command)
	return command
}
