package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var releasesApp string

func init() {
	releasesCmd.Flags().StringVar(&releasesApp, "app", "", "application slug or id (required)")
	_ = releasesCmd.MarkFlagRequired("app")
	rootCmd.AddCommand(releasesCmd)
}

var releasesCmd = &cobra.Command{
	Use:   "releases --app <slug>",
	Short: "List an application's releases (newest first)",
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
		appID, err := c.ResolveAppID(ctx, ws, releasesApp)
		if err != nil {
			return err
		}
		rels, err := c.Releases(ctx, ws, appID)
		if err != nil {
			return err
		}
		if flagJSON {
			return printJSON(rels)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "VERSION\tID\tIMAGE\tACTIVE\tCREATED")
		for _, r := range rels {
			active := ""
			if r.Active {
				active = "✓"
			}
			fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\n", r.Version, r.ID, r.Image, active, r.CreatedAt.Format("2006-01-02 15:04"))
		}
		return tw.Flush()
	},
}
