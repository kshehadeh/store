package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/app"
	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/providers"
)

func newSetup() *cobra.Command {
	var o connectOptions
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Add a vault connection for this computer",
		Long: `Choose the vault provider, account, and vault for this computer.

Run setup on every computer that shares items and select the same vault on
each. Only identifiers are saved; signing in is handled by the provider's own
tools (for 1Password, the op CLI and the 1Password app).

Each run adds one named connection; the first becomes the default. Manage
connections with ` + "`store connection list`" + `, ` + "`store connection default`" + `, and
` + "`store connection remove`" + `, and select a non-default one with the global
--connection (-c) flag.`,
		Example: "  store setup\n  store setup --vault Shared\n  store setup --as work --vault Shared --account my.1password.com",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return addConnection(cmd, app.NewConsole(os.Stdin, os.Stderr), o)
		},
	}
	cmd.Flags().StringVar(&o.as, "as", "", "short id for this connection (default: derived from the vault name)")
	cmd.Flags().StringVar(&o.provider, "provider", "", "provider to use (available: "+providerNames(providers.All())+")")
	cmd.Flags().StringVar(&o.account, "account", "", "account to use (1Password: sign-in address, email, or account ID)")
	cmd.Flags().StringVar(&o.vault, "vault", "", "vault name or ID")
	return cmd
}

func providerNames(defs []provider.Definition) string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return strings.Join(names, ", ")
}
