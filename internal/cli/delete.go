package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/app"
)

func newDelete() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "delete [NAME]",
		Short: "Delete a stored item after confirmation",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			a, err := openApp(cmd, app.NewConsole(os.Stdin, cmd.ErrOrStderr()))
			if err != nil {
				return err
			}
			title, deleted, err := a.Delete(cmd.Context(), name, id)
			if err != nil {
				return err
			}
			if deleted {
				a.Console.Info("Deleted %q.", title)
			} else {
				a.Console.Info("Delete canceled.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "delete the item with this ID")
	return cmd
}
