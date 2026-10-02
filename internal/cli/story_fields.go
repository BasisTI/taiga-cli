package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) storyFieldCmd() *cobra.Command {
	parent := &cobra.Command{Use: "field", Short: "Read and merge story custom field values"}
	parent.AddCommand(a.storyFieldListCmd(), a.storyFieldSetCmd())
	return parent
}

func (a *App) storyFieldListCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list REF", Short: "Read the custom field values of a story", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validRef(args[0]); err != nil {
			return err
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		story, err := service.Story(cmd.Context(), args[0], 0)
		if err != nil {
			return err
		}
		values, err := service.FieldValues(cmd.Context(), "story", app.ID(story["id"]))
		if err != nil {
			return err
		}
		return a.renderStoryFields(cmd.Context(), service, story, values)
	}
	return cmd
}

func (a *App) storyFieldSetCmd() *cobra.Command {
	var dry, force bool
	var unsets []string
	cmd := &cobra.Command{Use: "set REF [Name=value]... [--unset NAME]...", Short: "Merge custom field values into a story", Args: cobra.MinimumNArgs(1),
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
		story, err := service.Story(cmd.Context(), args[0], 0)
		if err != nil {
			return err
		}
		result, err := service.SetFieldValues(cmd.Context(), "story", app.ID(story["id"]), args[1:], unsets, dry, force)
		if err != nil {
			return err
		}
		values, ok := result.(app.Object)
		if !ok {
			return a.renderCurated(result)
		}
		return a.renderStoryFields(cmd.Context(), service, story, values)
	}
	cmd.Flags().StringArrayVar(&unsets, "unset", nil, "clear a checkbox or date field (repeatable)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request without writing")
	cmd.Flags().BoolVar(&force, "force-version", false, "skip the check for a concurrent write of the custom fields")
	return cmd
}

// renderStoryFields prints the values of a story next to their definitions. version is the
// version of the values resource, not the story's.
func (a *App) renderStoryFields(ctx context.Context, service *app.Service, story, values app.Object) error {
	defs, err := service.Fields(ctx, "story")
	if err != nil {
		return err
	}
	view, err := service.StoryView(story)
	if err != nil {
		return err
	}
	fields := app.FieldsView(values, defs)
	result := app.Scrub(app.Object{"id": story["id"], "ref": story["ref"], "url": view["url"], "version": values["version"],
		"attributes_values": values["attributes_values"], "fields": fields}).(app.Object)
	fields, _ = result["fields"].([]app.Object)
	mode, err := output.DetectMode(a.output, a.OutTTY)
	if err != nil {
		return err
	}
	if mode == output.JSON {
		return a.writeJSON(result)
	}
	lines := []output.Field{{Key: "ref", Value: fmt.Sprint(story["ref"])}, {Key: "id", Value: fmt.Sprint(story["id"])},
		{Key: "version", Value: fmt.Sprint(values["version"])}, {Key: "url", Value: fmt.Sprint(view["url"])}}
	known := map[string]bool{}
	for _, f := range fields {
		known[fmt.Sprint(f["id"])] = true
		value := ""
		if v, ok := f["value"]; ok {
			value = jsonValue(v)
		}
		lines = append(lines, output.Field{Key: fmt.Sprintf("%s (%s)", jsonValue(f["name"]), jsonValue(f["type"])), Value: value})
	}
	stored, _ := result["attributes_values"].(map[string]any)
	for _, id := range sortedKeys(stored) {
		if !known[id] {
			lines = append(lines, output.Field{Key: "#" + id + " (no definition)", Value: jsonValue(stored[id])})
		}
	}
	return a.writeFields(lines)
}

// jsonValue prints plain text as is; empty text, text with control or format characters (line
// breaks and bidi overrides included) and any other value print as JSON, so each field stays on one line and a stored ""
// differs from a field without value. A cleared value (null) prints empty, like a field without
// value, so it never reads as the text "null"; JSON output keeps the null.
func jsonValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok && s != "" && !strings.ContainsFunc(s, unsafeRune) {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return escapeJSON(string(b))
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
