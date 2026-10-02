package cli

import "github.com/spf13/cobra"

// userTextKeys is the text summary of a member; JSON output carries every key.
var userTextKeys = []string{"username", "full_name", "id", "is_admin", "role_name"}

func (a *App) userCmd() *cobra.Command {
	parent := &cobra.Command{Use: "user", Short: "Inspect the members of the project"}
	var search string
	list := &cobra.Command{Use: "list", Short: "List the members of the project with their role", Args: cobra.NoArgs}
	checkSearch := searchFlag(list, &search, "username or full name")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := checkSearch(); err != nil {
			return err
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		users, err := service.Users(cmd.Context(), search)
		if err != nil {
			return err
		}
		return a.renderKeys(users, userTextKeys)
	}
	parent.AddCommand(list)
	return parent
}
