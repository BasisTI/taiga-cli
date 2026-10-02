package cli

import (
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/spf13/cobra"
)

func (a *App) projectCmd() *cobra.Command {
	parent := &cobra.Command{Use: "project", Short: "Inspect projects; plan and apply the statuses and custom fields declared in a TOML file"}
	parent.AddCommand(a.projectListCmd(), a.projectGetCmd())
	for _, apply := range []bool{false, true} {
		var file string
		var dry bool
		use, short := "plan", "Compare the project with the file without writing"
		if apply {
			use, short = "apply", "Create the missing statuses and fields declared in the file"
		}
		cmd := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs}
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return app.Usage("-f is required (a TOML file, or - for stdin)")
			}
			content, err := a.readContent(file)
			if err != nil {
				return err
			}
			spec, err := app.ParseProjectSpec(strings.NewReader(content))
			if err != nil {
				return err
			}
			service, err := a.service(cmd)
			if err != nil {
				return err
			}
			if !apply {
				plan, err := service.ProjectPlan(cmd.Context(), spec)
				if err != nil {
					return err
				}
				return a.renderCurated(plan)
			}
			result, applyErr := service.ApplyProject(cmd.Context(), spec, dry)
			if err := a.renderCurated(result); err != nil {
				return err
			}
			return applyErr // the partial result is on stdout, the error and its exit code on stderr
		}
		cmd.Flags().StringVarP(&file, "file", "f", "", "project TOML file, or - for stdin (required)")
		if apply {
			cmd.Flags().BoolVar(&dry, "dry-run", false, "print the requests without writing")
		}
		parent.AddCommand(cmd)
	}
	return parent
}

// projectTextKeys and projectGetTextKeys are the text summaries of a project; JSON output
// carries every key (signed logo URLs hidden).
var (
	projectTextKeys    = []string{"id", "slug", "name", "is_private", "i_am_admin"}
	projectGetTextKeys = []string{"id", "slug", "name", "source", "is_private", "i_am_member", "i_am_admin", "is_epics_activated", "is_kanban_activated", "is_backlog_activated"}
)

func (a *App) projectListCmd() *cobra.Command {
	var search string
	cmd := &cobra.Command{Use: "list", Short: "List the projects you are a member of (no project needed)", Args: cobra.NoArgs}
	checkSearch := searchFlag(cmd, &search, "name or slug")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := checkSearch(); err != nil {
			return err
		}
		rc, err := a.runContext()
		if err != nil {
			return err
		}
		c, err := a.client(cmd.Context(), rc)
		if err != nil {
			return err
		}
		items, err := app.Projects(cmd.Context(), c, search)
		if err != nil {
			return err
		}
		return a.renderKeys(items, projectTextKeys)
	}
	return cmd
}

// projectGetCmd shows the project given as argument, or the selected one with where it was
// selected (flag, env, .taiga.toml or the config file), as auth status does.
func (a *App) projectGetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "get [SLUG|ID]", Short: "Show a project (default: the selected one, with where it was selected)", Args: cobra.MaximumNArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		rc, err := a.runContext()
		if err != nil {
			return err
		}
		selected := rc.Ctx.Project
		if len(args) == 1 {
			if a.flagProject != "" {
				return app.Usage("give the project as argument or with --project, not both")
			}
			if strings.TrimSpace(args[0]) == "" {
				return app.Usage("project must not be blank")
			}
			selected.Value, selected.Source = args[0], "argument"
		}
		if selected.Value == "" {
			return &output.Error{Code: "usage", Cause: "no project selected", Recovery: "pass SLUG or ID, --project, set TAIGA_PROJECT or add project to .taiga.toml", Exit: output.ExitUsage}
		}
		c, err := a.client(cmd.Context(), rc)
		if err != nil {
			return err
		}
		service, err := app.New(cmd.Context(), c, selected.Value)
		if err != nil {
			return err
		}
		view := service.ProjectView()
		view["source"] = selected.Source
		return a.renderKeys(view, projectGetTextKeys)
	}
	return cmd
}
