package cli

import (
	"fmt"

	"github.com/heruujoko/piramid/internal/home"
	"github.com/spf13/cobra"
)

func newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize the machine-wide Pi-Ramid home",
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := home.Resolve()
			if err != nil {
				return err
			}
			if err := home.Init(paths); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), paths.Root)
			return err
		},
	}
}
