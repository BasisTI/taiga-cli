package cli

import "github.com/spf13/cobra"

// swimlaneTextKeys is the text summary of a swimlane; JSON output carries every key.
var swimlaneTextKeys = []string{"id", "name", "order", "is_default"}

// swimlaneCmd only reads: creating, renaming and reordering swimlanes stay in the web UI
// (the first swimlane moves every story, and the order has no version).
func (a *App) swimlaneCmd() *cobra.Command {
	parent := &cobra.Command{Use: "swimlane", Short: "Inspect the swimlanes of the project"}
	list := &cobra.Command{Use: "list", Short: "List the swimlanes in board order", Args: cobra.NoArgs}
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		lanes, err := service.Swimlanes(cmd.Context())
		if err != nil {
			return err
		}
		return a.renderKeys(lanes, swimlaneTextKeys)
	}
	parent.AddCommand(list)
	return parent
}
