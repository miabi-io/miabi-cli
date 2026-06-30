package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

var (
	statusApp        string
	statusDeployment uint
)

func init() {
	f := statusCmd.Flags()
	f.StringVar(&statusApp, "app", "", "application slug or id (required)")
	f.UintVar(&statusDeployment, "deployment", 0, "a specific deployment id (default: the app's current status)")
	_ = statusCmd.MarkFlagRequired("app")
	rootCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status --app <slug> [--deployment <id>]",
	Short: "Show an application's status, or a specific deployment's status",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := context.Background()
		c, eff, err := newClient()
		if err != nil {
			return err
		}
		ws, err := workspaceRef(ctx, c, eff)
		if err != nil {
			return err
		}
		appID, err := c.ResolveAppID(ctx, ws, statusApp)
		if err != nil {
			return err
		}

		if statusDeployment != 0 {
			dep, err := c.Deployment(ctx, ws, appID, statusDeployment)
			if err != nil {
				return err
			}
			if flagJSON {
				return printJSON(dep)
			}
			fmt.Printf("Deployment #%d: %s\n", dep.ID, dep.Status)
			if dep.Error != "" {
				fmt.Printf("Error: %s\n", dep.Error)
			}
			return nil
		}

		app, err := c.App(ctx, ws, appID)
		if err != nil {
			return err
		}
		if flagJSON {
			return printJSON(app)
		}
		fmt.Printf("%s (%s): %s — %s:%s\n", app.Name, app.Slug, app.Status, app.Image, app.Tag)
		return nil
	},
}
