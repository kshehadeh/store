package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/config"
	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/providers"
	"github.com/kshehadeh/store/internal/tags"
)

func report(w io.Writer, c provider.Check) {
	mark := "ok  "
	if !c.OK {
		mark = "FAIL"
	}
	fmt.Fprintf(w, "%s %s: %s\n", mark, c.Name, c.Detail)
	if !c.OK && c.Fix != "" {
		fmt.Fprintf(w, "     Fix: %s\n", c.Fix)
	}
}

func newStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check configuration, sign-in, and vault access without changing anything",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fail := func(c provider.Check) error {
				report(out, c)
				fmt.Fprintln(out, "\nstore is not ready.")
				return errReported
			}
			path, err := config.Path()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return fail(provider.Check{Name: "Configuration", Detail: err.Error(), Fix: "run `store setup`"})
			}
			def, ok := providers.Lookup(cfg.Provider)
			if !ok {
				return fail(provider.Check{Name: "Configuration", Detail: fmt.Sprintf("unknown provider %q in %s", cfg.Provider, path), Fix: "run `store setup`"})
			}
			p, err := def.Open(cfg.Settings)
			if err != nil {
				return fail(provider.Check{Name: "Configuration", Detail: err.Error(), Fix: "run `store setup`"})
			}
			report(out, provider.Check{Name: "Configuration", OK: true, Detail: fmt.Sprintf("%s (%s)", def.Describe(cfg.Settings), path)})
			ok = true
			for _, c := range p.Status(cmd.Context()) {
				report(out, c)
				ok = ok && c.OK
			}
			if !ok {
				fmt.Fprintln(out, "\nstore is not ready.")
				return errReported
			}
			items, err := p.ListItems(cmd.Context())
			if err != nil {
				return fail(provider.Check{Name: "Items", Detail: err.Error()})
			}
			n := 0
			for _, it := range items {
				if tags.Discoverable(it.Tags) {
					n++
				}
			}
			report(out, provider.Check{Name: "Items", OK: true, Detail: fmt.Sprintf("%d discoverable (tagged %s/)", n, tags.Root)})
			fmt.Fprintln(out, "\nstore is ready.")
			return nil
		},
	}
}
