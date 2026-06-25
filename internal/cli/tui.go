package cli

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/heruujoko/piramid/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICommand() *cobra.Command {
	var flags clientFlags
	command := &cobra.Command{
		Use:   "tui",
		Short: "Open the Pi-Ramid operational terminal interface",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := flags.client()
			if err != nil {
				return err
			}
			program := tea.NewProgram(
				tui.NewModel(client),
				tea.WithAltScreen(),
				tea.WithContext(cmd.Context()),
			)
			_, err = program.Run()
			return err
		},
	}
	flags.bind(command)
	return command
}
