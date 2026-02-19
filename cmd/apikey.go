package cmd

import (
	"fmt"
	"os"

	"github.com/carloluisito/launchtunnel-cli/client"
	"github.com/carloluisito/launchtunnel-cli/display"
	"github.com/spf13/cobra"
)

func newAPIKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api-key",
		Short: "Manage API keys",
	}

	cmd.AddCommand(
		newAPIKeyCreateCmd(),
		newAPIKeyListCmd(),
		newAPIKeyRevokeCmd(),
	)

	return cmd
}

func newAPIKeyCreateCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey, err := requireAuth()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			c := client.New(cliCfg.APIURL, apiKey)
			key, err := c.CreateAPIKey(name)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			fmt.Printf("API key created: %s  (save this -- it will not be shown again)\n", key.Key)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "human-readable label for this key")
	return cmd
}

func newAPIKeyListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all API keys",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey, err := requireAuth()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			c := client.New(cliCfg.APIURL, apiKey)
			keys, err := c.ListAPIKeys()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			if jsonOutput {
				return display.PrintJSON(os.Stdout, keys)
			}

			if len(keys) == 0 {
				fmt.Println("No API keys.")
				return nil
			}

			tbl := display.NewTable("PREFIX", "NAME", "CREATED", "LAST USED")
			for _, k := range keys {
				lastUsed := "never"
				if k.LastUsedAt != nil {
					lastUsed = k.LastUsedAt.Format("2006-01-02")
				}
				tbl.AddRow(
					k.Prefix,
					k.Name,
					k.CreatedAt.Format("2006-01-02"),
					lastUsed,
				)
			}
			tbl.Render(os.Stdout)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func newAPIKeyRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <key_prefix>",
		Short: "Revoke an API key by its prefix",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey, err := requireAuth()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			c := client.New(cliCfg.APIURL, apiKey)

			// The prefix is the first 8+ characters. We need to find the key ID
			// by listing keys and matching on the prefix.
			keys, err := c.ListAPIKeys()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			prefix := args[0]
			var matchedID string
			for _, k := range keys {
				if k.Prefix == prefix {
					matchedID = k.ID
					break
				}
			}

			if matchedID == "" {
				fmt.Fprintf(os.Stderr, "No API key found with prefix %s.\n", prefix)
				os.Exit(1)
			}

			if err := c.RevokeAPIKey(matchedID); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			fmt.Printf("API key %s... revoked. Active tunnels using this key have been terminated.\n", prefix)
			return nil
		},
	}
}
