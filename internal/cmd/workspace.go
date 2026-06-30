package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/miabi-io/miabi-cli/internal/config"
	"github.com/spf13/cobra"
)

func init() {
	workspaceCmd.AddCommand(workspaceListCmd, workspaceShowCmd, workspaceSwitchCmd)
	rootCmd.AddCommand(workspaceCmd)
}

var workspaceCmd = &cobra.Command{
	Use:     "workspace",
	Aliases: []string{"ws"},
	Short:   "Manage the active workspace context",
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the workspaces you can access",
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, _, err := newClient()
		if err != nil {
			return err
		}
		ws, err := c.Workspaces(context.Background())
		if err != nil {
			return err
		}
		if flagJSON {
			return printJSON(ws)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tDISPLAY NAME\tROLE")
		for _, w := range ws {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", w.ID, w.Name, w.DisplayName, w.Role)
		}
		return tw.Flush()
	},
}

var workspaceShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the active workspace (from config)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		f, err := config.Load()
		if err != nil {
			return err
		}
		if f.Workspace == nil {
			fmt.Println("No active workspace. Set one with `miabi workspace switch <name>`.")
			return nil
		}
		if flagJSON {
			return printJSON(f.Workspace)
		}
		label := f.Workspace.Name
		if f.Workspace.DisplayName != "" {
			label = fmt.Sprintf("%s (%s)", f.Workspace.DisplayName, f.Workspace.Name)
		}
		fmt.Printf("Active workspace: %s [id %d]\n", label, f.Workspace.ID)
		return nil
	},
}

var workspaceSwitchCmd = &cobra.Command{
	Use:   "switch <name-or-id>",
	Short: "Set the active workspace, persisting it to config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := newClient()
		if err != nil {
			return err
		}
		ws, err := c.Workspaces(context.Background())
		if err != nil {
			return err
		}
		ref := args[0]
		idMatch, _ := strconv.ParseUint(ref, 10, 64)
		for _, w := range ws {
			if w.Name == ref || w.UID == ref || (idMatch != 0 && w.ID == uint(idMatch)) {
				f, err := config.Load()
				if err != nil {
					return err
				}
				f.Workspace = &config.WorkspaceRef{ID: w.ID, Name: w.Name, DisplayName: w.DisplayName}
				if err := config.Save(f); err != nil {
					return err
				}
				label := w.Name
				if w.DisplayName != "" {
					label = fmt.Sprintf("%s (%s)", w.DisplayName, w.Name)
				}
				fmt.Printf("Active workspace is now %s [id %d]\n", label, w.ID)
				return nil
			}
		}
		return fmt.Errorf("workspace %q not found among your workspaces", ref)
	},
}
