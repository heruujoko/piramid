package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newEnqueueCommand() *cobra.Command {
	var flags clientFlags
	command := &cobra.Command{
		Use:   "enqueue TASK.yaml",
		Short: "Enqueue a structured task or plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			plan, err := decodeEnqueueYAML(content)
			if err != nil {
				return err
			}
			client, err := flags.client()
			if err != nil {
				return err
			}
			if err := client.Enqueue(cmd.Context(), plan); err != nil {
				return connectionError(flags, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Enqueued %d task(s) for %s\n",
				len(plan.Tasks), plan.GoalID)
			return err
		},
	}
	flags.bind(command)
	return command
}

func decodeEnqueueYAML(content []byte) (domain.Plan, error) {
	var header map[string]any
	if err := yaml.Unmarshal(content, &header); err != nil {
		return domain.Plan{}, err
	}
	if _, isPlan := header["tasks"]; isPlan {
		var plan domain.Plan
		if err := strictYAML(bytes.NewReader(content), &plan); err != nil {
			return domain.Plan{}, err
		}
		if err := domain.ValidatePlan(&plan); err != nil {
			return domain.Plan{}, err
		}
		return plan, nil
	}
	var task domain.Task
	if err := strictYAML(bytes.NewReader(content), &task); err != nil {
		return domain.Plan{}, err
	}
	plan := domain.Plan{Version: 1, GoalID: "DIRECT-" + task.ID, Tasks: []domain.Task{task}}
	if err := domain.ValidatePlan(&plan); err != nil {
		return domain.Plan{}, err
	}
	return plan, nil
}

func strictYAML(reader io.Reader, target any) error {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("YAML must contain exactly one document")
	}
	return nil
}
