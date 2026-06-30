package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/miabi-io/miabi-cli/internal/config"
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
		if flagJSON {
			return printJSON(me)
		}
		fmt.Printf("User:    %s <%s>\n", me.Name, me.Email)
		if me.Username != "" {
			fmt.Printf("Username: %s\n", me.Username)
		}
		fmt.Printf("Auth:    %s\n", me.Auth.Method)
		if len(me.Auth.Scopes) > 0 {
			fmt.Printf("Scopes:  %s\n", strings.Join(me.Auth.Scopes, ", "))
		}
		if me.Auth.WorkspaceID != nil {
			fmt.Printf("Bound workspace: %d\n", *me.Auth.WorkspaceID)
		}
		return nil
	},
}
