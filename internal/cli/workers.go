package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newWorkersCommand() *cobra.Command {
	var flags clientFlags
	command := &cobra.Command{
		Use:   "workers",
		Short: "List active Pi workers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := flags.client()
			if err != nil {
				return err
			}
			workers, err := client.ListWorkers(cmd.Context())
			if err != nil {
				return connectionError(flags, err)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "WORKER\tSTATUS\tTASK\tATTEMPT")
			for _, worker := range workers {
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%d\n",
					worker.ID, worker.Status, worker.TaskID, worker.AttemptID)
			}
			return writer.Flush()
		},
	}
	flags.bind(command)
	return command
}
