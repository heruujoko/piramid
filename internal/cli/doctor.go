package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/heruujoko/piramid/internal/doctor"
	"github.com/heruujoko/piramid/internal/home"
	"github.com/spf13/cobra"
)

func newDoctorCommand() *cobra.Command {
	var (
		project string
		smoke   bool
	)
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check Pi-Ramid dependencies without changing the system",
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := home.Resolve()
			if err != nil {
				return err
			}
			results := doctor.New(doctor.Options{
				Paths: paths, ProjectPath: project, SmokeTest: smoke,
			}).Run(cmd.Context())
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			failed := false
			for _, result := range results {
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\n", result.Status, result.Name, result.Message)
				if result.Remediation != "" {
					_, _ = fmt.Fprintf(writer, "\t\t%s\n", result.Remediation)
				}
				failed = failed || result.Status == doctor.Fail
			}
			if err := writer.Flush(); err != nil {
				return err
			}
			if failed {
				return fmt.Errorf("doctor found required checks that failed")
			}
			return nil
		},
	}
	command.Flags().StringVar(&project, "project", "", "optional project clone to inspect")
	command.Flags().BoolVar(&smoke, "smoke-test", false, "run an explicit read-only Pi invocation")
	return command
}
