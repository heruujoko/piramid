package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newInspectCommand() *cobra.Command {
	var flags clientFlags
	command := &cobra.Command{
		Use:   "inspect TASK_ID",
		Short: "Inspect a task and its attempts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := flags.client()
			if err != nil {
				return err
			}
			task, err := client.GetTask(cmd.Context(), args[0])
			if err != nil {
				return connectionError(flags, err)
			}
			content, err := yaml.Marshal(task)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), string(content))
			return err
		},
	}
	flags.bind(command)
	return command
}
