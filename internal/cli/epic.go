package cli

import "github.com/spf13/cobra"

// epicTextKeys and epicGetTextKeys are the text summaries of an epic; JSON output carries
// every key.
var (
	epicTextKeys    = []string{"ref", "subject", "status", "is_closed", "color", "url"}
	epicGetTextKeys = []string{"ref", "id", "version", "subject", "status", "is_closed", "color", "tags", "description", "user_stories", "url"}
)

func (a *App) epicCmd() *cobra.Command {
	parent := &cobra.Command{Use: "epic", Short: "Inspect the epics of the project"}
	var search string
	var closed bool
	list := &cobra.Command{Use: "list", Short: "List the epics of the project", Args: cobra.NoArgs}
	checkSearch := searchFlag(list, &search, "subject")
	closedValue := closedFlag(list, &closed, "epics")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := checkSearch(); err != nil {
			return err
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		items, err := service.Epics(cmd.Context(), search, closedValue())
		if err != nil {
			return err
		}
		return a.renderKeys(items, epicTextKeys)
	}
	get := &cobra.Command{Use: "get REF", Short: "Show an epic and the stories linked to it", Args: cobra.ExactArgs(1)}
	get.RunE = func(cmd *cobra.Command, args []string) error {
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		e, err := service.EpicDetail(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return a.renderKeys(e, epicGetTextKeys)
	}
	parent.AddCommand(list, get)
	return parent
}
