package cli

import (
	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/spf13/cobra"
)

// epicTextKeys and epicGetTextKeys are the text summaries of an epic; JSON output carries
// every key.
var (
	epicTextKeys     = []string{"ref", "subject", "status", "is_closed", "color", "url"}
	epicGetTextKeys  = []string{"ref", "id", "version", "subject", "status", "is_closed", "color", "tags", "description", "user_stories", "url"}
	epicLinkTextKeys = []string{"story", "epic", "changed", "linked", "removed", "epics"}
)

func (a *App) epicCmd() *cobra.Command {
	parent := &cobra.Command{Use: "epic", Short: "Inspect the epics of the project and link stories to them"}
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
		if err := validRef(args[0]); err != nil {
			return err
		}
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
	parent.AddCommand(list, get, a.epicLinkCmd())
	return parent
}

func (a *App) epicLinkCmd() *cobra.Command {
	var replace, confirmDelete, dry bool
	cmd := &cobra.Command{Use: "link EPIC_REF STORY_REF", Short: "Link a story to an epic; with --replace, make it the story's only epic", Args: cobra.ExactArgs(2)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		for _, ref := range args {
			if err := validRef(ref); err != nil {
				return err
			}
		}
		if err := checkReplace(replace, confirmDelete, "--replace"); err != nil {
			return err
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		epic, err := service.LinkableEpic(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		story, err := service.Story(cmd.Context(), args[1], 0)
		if err != nil {
			return err
		}
		result, err := service.LinkEpic(cmd.Context(), story, epic, replace, dry)
		if err != nil {
			return err
		}
		return a.renderKeys(result, epicLinkTextKeys)
	}
	f := cmd.Flags()
	f.BoolVar(&replace, "replace", false, "remove the story's other epics after linking this one (requires --confirm-delete)")
	f.BoolVar(&confirmDelete, "confirm-delete", false, "required with --replace: it deletes the other epic links")
	f.BoolVar(&dry, "dry-run", false, "print the requests without sending them")
	return cmd
}

// checkReplace validates the replacement flags before any request.
func checkReplace(replace, confirmDelete bool, flag string) error {
	if replace && !confirmDelete {
		return app.DeleteNotConfirmed(flag)
	}
	if confirmDelete && !replace {
		return app.Usage("--confirm-delete only applies with " + flag)
	}
	return nil
}
