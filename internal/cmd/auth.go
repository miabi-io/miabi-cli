package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/miabi-io/miabi-cli/internal/config"
	"github.com/miabi-io/miabi-cli/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(loginCmd, whoamiCmd)
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Store the panel URL and API token in ~/.miabi/config.yaml",
	Long:  "Validates the token against GET /me and persists url+token to the config file.\nCI should set MIABI_URL/MIABI_TOKEN instead of logging in.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, eff, err := newClient()
		if err != nil {
			return err
		}
		me, err := c.Me(context.Background())
		if err != nil {
			return fmt.Errorf("token rejected: %w", err)
		}
		f, err := config.Load()
		if err != nil {
			return err
		}
		f.URL, f.Token = eff.URL, eff.Token
		// Persist the identity (display name + username) for offline display.
		f.User = &config.Identity{Name: me.Name, Username: me.Username, Email: me.Email}
		if err := config.Save(f); err != nil {
			return err
		}
		who := me.Name
		if me.Username != "" {
			who = fmt.Sprintf("%s (@%s)", me.Name, me.Username)
		}
		fmt.Printf("Logged in as %s <%s> at %s\n", who, me.Email, eff.URL)
		fmt.Printf("Config saved to %s\n", config.Path())
		return nil
	},
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the authenticated identity, scopes, and bound workspace",
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, _, err := newClient()
		if err != nil {
			return err
		}
		me, err := c.Me(context.Background())
		if err != nil {
			return err
		}
		if structured() {
			return emit(me)
		}
		fmt.Printf("%s   %s <%s>\n", ui.Dim("User: "), me.Name, me.Email)
		if me.Username != "" {
			fmt.Printf("%s   @%s\n", ui.Dim("Name: "), me.Username)
		}
		fmt.Printf("%s   %s\n", ui.Dim("Auth: "), me.Auth.Method)
		if len(me.Auth.Scopes) > 0 {
			fmt.Printf("%s %s\n", ui.Dim("Scopes:"), strings.Join(me.Auth.Scopes, ", "))
		}
		// Local context (workspace + bound app) from the config file.
		if f, ferr := config.Load(); ferr == nil {
			if f.Workspace != nil {
				fmt.Printf("%s    %s\n", ui.Dim("Space:"), f.Workspace.Name)
			}
			if f.App != nil {
				fmt.Printf("%s      %s\n", ui.Dim("App:"), f.App.Name)
			}
		}
		return nil
	},
}
