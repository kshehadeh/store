package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/app"
)

func newTUI() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Browse stored items and run actions in an interactive interface",
		Long: `Browse stored items in a two-pane terminal interface. Running store with no
command also opens this browser when stdin is a terminal.

Type to filter items, use the arrow keys to move through the list, press Enter
to pull the selected item, Ctrl-E to edit its description and store tags, or
Ctrl-R to rename it.`,
		Args: cobra.NoArgs,
		RunE: runTUI,
	}
}

func runTUI(cmd *cobra.Command, _ []string) error {
	a, err := openApp(app.NewConsole(os.Stdin, os.Stderr))
	if err != nil {
		return err
	}
	return a.TUI(cmd.Context())
}
