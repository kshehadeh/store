package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/app"
)

func newList() *cobra.Command {
	var o app.ListOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored items, optionally filtered by tags",
		Long: `List items in the vault that have a store/ tag, without reading their contents.

Each --tag must match (by whole segment, so project matches project/api but not
projectile). Automatic metadata can be filtered too, e.g. meta/type/folder or
meta/host/laptop.`,
		Example: "  store list\n  store list --tag project/api --tag meta/type/file",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := openApp(cmd, app.NewConsole(os.Stdin, os.Stderr))
			if err != nil {
				return err
			}
			return a.List(cmd.Context(), o)
		},
	}
	cmd.Flags().StringSliceVarP(&o.Tags, "tag", "t", nil, "only items with this tag; repeatable, all must match")
	cmd.Flags().BoolVar(&o.JSON, "json", false, "print JSON")
	return cmd
}
