package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(appsCmd)
}

var appsCmd = &cobra.Command{
	Use:   "apps",
	Short: "List applications in the workspace",
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
		apps, err := c.Apps(ctx, ws)
		if err != nil {
			return err
		}
		if flagJSON {
			return printJSON(apps)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tSLUG\tIMAGE\tTAG\tSTATUS")
		for _, a := range apps {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", a.ID, a.Slug, a.Image, a.Tag, a.Status)
		}
		return tw.Flush()
	},
}
