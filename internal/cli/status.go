package cli

import (
	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/spf13/cobra"
)

// statusTextKeys is the text summary of a status; JSON output carries every key.
var statusTextKeys = []string{"id", "name", "order", "is_closed", "color"}

func (a *App) statusCmd() *cobra.Command {
	parent := &cobra.Command{Use: "status", Short: "Inspect the statuses of the project"}
	var kind string
	list := &cobra.Command{Use: "list", Short: "List the statuses in board order", Args: cobra.NoArgs}
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		path := "userstory-statuses"
		switch kind {
		case "story":
		case "task":
			path = "task-statuses"
		default:
			return app.Usage("--kind must be story or task")
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		statuses, err := service.Catalog(cmd.Context(), path)
		if err != nil {
			return err
		}
		statuses = append(make([]app.Object, 0, len(statuses)), statuses...)
		app.SortStatuses(statuses)
		return a.renderKeys(statuses, statusTextKeys)
	}
	list.Flags().StringVar(&kind, "kind", "story", "story or task")
	parent.AddCommand(list)
	return parent
}
