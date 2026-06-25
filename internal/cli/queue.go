package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newQueueCommand() *cobra.Command {
	var flags clientFlags
	command := &cobra.Command{
		Use:   "queue",
		Short: "List queued and historical tasks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := flags.client()
			if err != nil {
				return err
			}
			tasks, err := client.ListTasks(cmd.Context())
			if err != nil {
				return connectionError(flags, err)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "TASK\tSTATUS\tATTEMPTS\tPROJECT")
			for _, task := range tasks {
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%d/%d\t%s\n",
					task.ID, task.Status, task.AttemptCount, task.MaxAttempts, task.ProjectPath)
			}
			return writer.Flush()
		},
	}
	flags.bind(command)
	return command
}
