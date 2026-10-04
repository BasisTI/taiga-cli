package cli

import (
	"sort"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/spf13/cobra"
)

// fieldValuesCmd reads and merges the custom field values of a story or task (kind).
func (a *App) fieldValuesCmd(kind string) *cobra.Command {
	parent := &cobra.Command{Use: "field", Short: "Read and merge " + kind + " custom field values"}
	parent.AddCommand(a.fieldValuesListCmd(kind), a.fieldValuesSetCmd(kind))
	return parent
}

func (a *App) fieldValuesListCmd(kind string) *cobra.Command {
	cmd := &cobra.Command{Use: "list REF", Short: "Read the custom field values of a " + kind, Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validRef(args[0]); err != nil {
			return err
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		item, err := service.Item(cmd.Context(), kind, args[0])
		if err != nil {
			return err
		}
		values, err := service.FieldValues(cmd.Context(), kind, app.ID(item["id"]))
		if err != nil {
			return err
		}
		return a.renderFields(cmd.Context(), service, kind, item, values)
	}
	return cmd
}

func (a *App) fieldValuesSetCmd(kind string) *cobra.Command {
	var dry, force bool
	var unsets []string
	cmd := &cobra.Command{Use: "set REF [Name=value]... [--unset NAME]...", Short: "Merge custom field values into a " + kind, Args: cobra.MinimumNArgs(1),
		Long: "Sets each named field and keeps the others. The name ends at the first \"=\"; text values are taken as is, " +
			"checkbox values are true or false and dates are YYYY-MM-DD. --unset clears a checkbox or date field (stored as null); " +
			"the text \"null\" is never a cleared value, and a text field is set to empty with Name=."}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validRef(args[0]); err != nil {
			return err
		}
		if len(args) == 1 && len(unsets) == 0 {
			return app.Usage("at least one Name=value or --unset NAME is required")
		}
		for _, name := range unsets {
			if name == "" {
				return app.Usage("--unset needs a field name")
			}
		}
		for _, entry := range args[1:] {
			if name, _, ok := strings.Cut(entry, "="); !ok || name == "" {
				return app.Usage("field assignment must be Name=value: " + entry)
			}
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		item, err := service.Item(cmd.Context(), kind, args[0])
		if err != nil {
			return err
		}
		result, err := service.SetFieldValues(cmd.Context(), kind, app.ID(item["id"]), args[1:], unsets, dry, force)
		if err != nil {
			return err
		}
		values, ok := result.(app.Object)
		if !ok {
			return a.renderCurated(result)
		}
		return a.renderFields(cmd.Context(), service, kind, item, values)
	}
	cmd.Flags().StringArrayVar(&unsets, "unset", nil, "clear a checkbox or date field (repeatable)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request without writing")
	cmd.Flags().BoolVar(&force, "force-version", false, "skip the check for a concurrent write of the custom fields")
	return cmd
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
