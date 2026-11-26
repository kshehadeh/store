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
to pull the selected item, Ctrl-N to add a file, folder, or text, Ctrl-E to edit
the selected item's description and store tags, Ctrl-R to rename it, or Ctrl-D to delete it after confirmation.`,
		Args: cobra.NoArgs,
		RunE: runTUI,
	}
}

func runTUI(cmd *cobra.Command, _ []string) error {
	a, err := openApp(cmd, app.NewConsole(os.Stdin, os.Stderr))
	if err != nil {
		return err
	}
	return a.TUI(cmd.Context())
}
