package cli

import (
	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/spf13/cobra"
)

// fieldTextKeys is the text summary of a definition; JSON output carries every key.
var fieldTextKeys = []string{"id", "name", "type", "description", "order"}

func (a *App) fieldCmd() *cobra.Command {
	parent := &cobra.Command{Use: "field", Short: "Manage custom field definitions of stories and tasks"}
	parent.AddCommand(a.fieldListCmd(), a.fieldCreateCmd())
	return parent
}

func (a *App) fieldListCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{Use: "list", Short: "List the custom field definitions", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if _, err := app.FieldPath(kind); err != nil {
			return err
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		fields, err := service.Fields(cmd.Context(), kind)
		if err != nil {
			return err
		}
		return a.renderKeys(fields, fieldTextKeys)
	}
	cmd.Flags().StringVar(&kind, "kind", "", "story or task (required)")
	return cmd
}

func (a *App) fieldCreateCmd() *cobra.Command {
	var kind, name, typ, description string
	var dry bool
	cmd := &cobra.Command{Use: "create", Short: "Create a custom field definition unless it already exists", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if _, err := app.FieldPath(kind); err != nil {
			return err
		}
		if err := app.ValidateField(name, typ); err != nil {
			return err
		}
		var desc *string
		if cmd.Flags().Changed("description") {
			desc = &description
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		result, err := service.CreateField(cmd.Context(), kind, name, typ, desc, dry)
		if err != nil {
			return err
		}
		return a.renderKeys(result, fieldTextKeys)
	}
	cmd.Flags().StringVar(&kind, "kind", "", "story or task (required)")
	cmd.Flags().StringVar(&name, "name", "", "field name, case-sensitive (required)")
	cmd.Flags().StringVar(&typ, "type", "", "text, date or checkbox (required)")
	cmd.Flags().StringVar(&description, "description", "", "field description; when given, an existing field must match it")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request without writing")
	return cmd
}
