package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/heruujoko/piramid/internal/intake"
	"github.com/spf13/cobra"
)

func newGoalCommand() *cobra.Command {
	var (
		flags   clientFlags
		project string
		yes     bool
	)
	command := &cobra.Command{
		Use:   "goal --project PATH GOAL",
		Short: "Plan and enqueue a natural-language goal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := flags.client()
			if err != nil {
				return err
			}
			draft, err := client.DraftGoal(cmd.Context(), intake.DraftRequest{
				GoalText: args[0], ProjectPath: project,
			})
			if err != nil {
				return connectionError(flags, err)
			}
			printDraftPreview(cmd, draft)
			confirmed := yes
			if !yes {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), "Enqueue this plan? [y/N] ")
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, _ := reader.ReadString('\n')
				answer = strings.ToLower(strings.TrimSpace(answer))
				confirmed = answer == "y" || answer == "yes"
			}
			if !confirmed {
				if err := client.RejectGoal(cmd.Context(), draft.Goal.ID); err != nil {
					return connectionError(flags, err)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Draft rejected; evidence preserved.")
				return nil
			}
			if err := client.ConfirmGoal(cmd.Context(), draft.Goal.ID); err != nil {
				return connectionError(flags, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Enqueued goal %s\n", draft.Goal.ID)
			return err
		},
	}
	command.Flags().StringVar(&project, "project", "", "project clone directory")
	_ = command.MarkFlagRequired("project")
	command.Flags().BoolVar(&yes, "yes", false, "enqueue without confirmation")
	flags.bind(command)
	return command
}

func printDraftPreview(command *cobra.Command, draft intake.Draft) {
	_, _ = fmt.Fprintf(command.OutOrStdout(), "Goal %s\n", draft.Goal.ID)
	for _, task := range draft.Plan.Tasks {
		dependencies := "-"
		if len(task.DependsOn) > 0 {
			dependencies = strings.Join(task.DependsOn, ",")
		}
		_, _ = fmt.Fprintf(
			command.OutOrStdout(),
			"  %s  %s\n    project: %s\n    depends: %s  DOD: %d  attempts: %d  timeout: %s\n",
			task.ID, task.Title, task.ProjectPath, dependencies,
			len(task.DOD), task.MaxAttempts, task.TimeoutText,
		)
	}
}

func connectionError(flags clientFlags, err error) error {
	return fmt.Errorf("Pi-Ramid at %s:%d: %w", flags.host, flags.port, err)
}
