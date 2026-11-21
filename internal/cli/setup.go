package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/app"
	"github.com/kshehadeh/store/internal/config"
	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/providers"
)

func newSetup() *cobra.Command {
	var providerName, account, vault string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Choose the vault provider, account, and vault for this computer",
		Long: `Choose the vault provider, account, and vault for this computer.

Run setup on every computer that shares items and select the same vault on
each. Only identifiers are saved; signing in is handled by the provider's own
tools (for 1Password, the op CLI and the 1Password app).`,
		Example: "  store setup\n  store setup --vault Shared",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			console := app.NewConsole(os.Stdin, os.Stderr)
			defs := providers.All()
			var def provider.Definition
			switch {
			case providerName != "":
				d, ok := providers.Lookup(providerName)
				if !ok {
					return fmt.Errorf("unknown provider %q; available: %s", providerName, providerNames(defs))
				}
				def = d
			case len(defs) == 1:
				def = defs[0]
			default:
				labels := make([]string, len(defs))
				for i, d := range defs {
					labels[i] = d.DisplayName
				}
				idx, err := console.Choose("Select a provider:", labels)
				if err != nil {
					return err
				}
				def = defs[idx]
			}
			console.Info("Provider: %s", def.DisplayName)
			preset := map[string]string{}
			if account != "" {
				preset["account"] = account
			}
			if vault != "" {
				preset["vault"] = vault
			}
			settings, err := def.Setup(cmd.Context(), console, preset)
			if err != nil {
				return err
			}
			path, err := config.Path()
			if err != nil {
				return err
			}
			if err := config.Save(path, &config.Config{Version: 1, Provider: def.Name, Settings: settings}); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Saved setup for %s to %s\n", def.Describe(settings), path)
			fmt.Fprintln(out, "Run `store status` at any time to check connectivity.")
			return nil
		},
	}
	cmd.Flags().StringVar(&providerName, "provider", "", "provider to use (available: "+providerNames(providers.All())+")")
	cmd.Flags().StringVar(&account, "account", "", "account to use (1Password: sign-in address, email, or account ID)")
	cmd.Flags().StringVar(&vault, "vault", "", "vault name or ID")
	return cmd
}

func providerNames(defs []provider.Definition) string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return strings.Join(names, ", ")
}
