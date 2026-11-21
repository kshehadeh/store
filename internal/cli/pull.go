package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/app"
)

func newPull() *cobra.Command {
	var o app.PullOptions
	cmd := &cobra.Command{
		Use:   "pull [NAME]",
		Short: "Choose a recent item to pull, or retrieve by name, tags, or ID",
		Long: `Retrieve a stored item by name, tags, or ID.

Text, notes, and fields are written to stdout exactly as stored. Files and
folders are saved to a new path: --to, or a path you are asked for. Folders are
restored as a directory tree unless --as-zip is given. store never overwrites
an existing file or folder.

With no name, tags, or ID, choose from all stored items, most recently updated
first. Items kept again move to the top; manually tagged items are included.
If several items match an explicit query, you choose one from a list. When
not running in a terminal, specify a name, tags, or --id instead.`,
		Example: `  store pull
  store pull root-env --to ~/.env
  store pull region
  store pull --tag project/api
  store pull "API Login" --field password`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				o.Name = args[0]
			}
			a, err := openApp(app.NewConsole(os.Stdin, os.Stderr))
			if err != nil {
				return err
			}
			return a.Pull(cmd.Context(), o)
		},
	}
	cmd.Flags().StringSliceVarP(&o.Tags, "tag", "t", nil, "only items with this tag; repeatable, all must match")
	cmd.Flags().StringVar(&o.ID, "id", "", "pull the item with this ID")
	cmd.Flags().StringVar(&o.To, "to", "", "destination path (an existing directory places the item inside it)")
	cmd.Flags().BoolVar(&o.AsZip, "as-zip", false, "save a folder as its ZIP archive instead of extracting it")
	cmd.Flags().StringVar(&o.Field, "field", "", "field to pull from a structured item such as a login (label, section.label, or ID)")
	return cmd
}
