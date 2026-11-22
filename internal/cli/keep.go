package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/app"
)

func newKeep() *cobra.Command {
	var o app.KeepOptions
	var description string
	cmd := &cobra.Command{
		Use:   "keep [PATH]",
		Short: "Create or update a stored file, folder, or text",
		Long: `Create or update a stored file, folder, or text.

A file is stored byte-for-byte. A folder is stored as a ZIP, including dotfiles
and empty directories; symlinks and special files are refused. With --text,
text is read from stdin, or from a hidden prompt when run in a terminal.
Use --description to set a custom text field named Description in 1Password.
When updating an item, its description changes only if --description is given.

If an item with --name exists and store created it, its content is replaced and
its labels are kept. store refuses to touch an item it did not create.`,
		Example: `  store keep ~/.env --name root-env --description "Local API settings" --tag project/api
  store keep ./config --name app-config
  pbpaste | store keep --text --name api-token
  store keep --text --name region`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Description = nil
			if cmd.Flags().Changed("description") {
				o.Description = &description
			}
			if len(args) == 1 {
				o.Path = args[0]
			}
			a, err := openApp(app.NewConsole(os.Stdin, os.Stderr))
			if err != nil {
				return err
			}
			return a.Keep(cmd.Context(), o)
		},
	}
	cmd.Flags().StringVarP(&o.Name, "name", "n", "", "name of the item in the vault (required)")
	cmd.Flags().BoolVar(&o.Text, "text", false, "keep text from stdin or a hidden prompt instead of a path")
	cmd.Flags().StringSliceVarP(&o.Tags, "tag", "t", nil, "label to add, e.g. project/api (stored as store/project/api); repeatable")
	cmd.Flags().StringVar(&description, "description", "", "description saved as a custom text field in 1Password")
	return cmd
}
