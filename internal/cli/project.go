package cli

import (
	"strings"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/spf13/cobra"
)

func (a *App) projectCmd() *cobra.Command {
	parent := &cobra.Command{Use: "project", Short: "Plan and apply the statuses and custom fields declared in a TOML file"}
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
