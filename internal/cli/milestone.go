package cli

import "github.com/spf13/cobra"

// milestoneTextKeys is the text summary of a sprint; JSON output carries every key, including
// the stories of the sprint.
var milestoneTextKeys = []string{"id", "name", "slug", "estimated_start", "estimated_finish", "closed"}

func (a *App) milestoneCmd() *cobra.Command {
	parent := &cobra.Command{Use: "milestone", Short: "Inspect the milestones (sprints) of the project"}
	var search string
	var closed bool
	list := &cobra.Command{Use: "list", Short: "List the milestones of the project", Args: cobra.NoArgs}
	checkSearch := searchFlag(list, &search, "name")
	closedValue := closedFlag(list, &closed, "milestones")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := checkSearch(); err != nil {
			return err
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		items, err := service.Milestones(cmd.Context(), search, closedValue())
		if err != nil {
			return err
		}
		return a.renderKeys(items, milestoneTextKeys)
	}
	parent.AddCommand(list)
	return parent
}
