package cli

import (
	"strings"
	"unicode/utf8"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/spf13/cobra"
)

// commentCmd publishes a comment on a story or task (kind).
func (a *App) commentCmd(kind string) *cobra.Command {
	var body, file string
	var dry bool
	cmd := &cobra.Command{Use: "comment REF", Short: "Publish a comment on a " + kind, Args: cobra.ExactArgs(1),
		Long: "Publish a comment on a " + kind + ". The comment is sent once and never repeated: if the answer is lost,\n" +
			"the " + kind + " history says whether it was published."}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validRef(args[0]); err != nil {
			return err
		}
		if cmd.Flags().Changed("body") == cmd.Flags().Changed("body-file") {
			return app.Usage("choose exactly one of --body or --body-file")
		}
		if cmd.Flags().Changed("body-file") {
			var err error
			if body, err = a.readContent(file); err != nil {
				return err
			}
		}
		if strings.TrimSpace(body) == "" {
			return app.Usage("comment body must not be blank")
		}
		if !utf8.ValidString(body) {
			return app.Usage("comment body must be valid UTF-8")
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		result, err := service.Comment(cmd.Context(), kind, args[0], body, dry)
		if err != nil {
			return err
		}
		if kind == "task" {
			return a.renderTask(result)
		}
		return a.renderCurated(result)
	}
	cmd.Flags().StringVar(&body, "body", "", "comment text (Markdown)")
	cmd.Flags().StringVar(&file, "body-file", "", "read the comment from FILE, or - for stdin")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request without writing")
	return cmd
}

// commentsCmd lists the comments of a story or task (kind).
func (a *App) commentsCmd(kind string) *cobra.Command {
	var include bool
	cmd := &cobra.Command{Use: "comments REF", Short: "List the comments of a " + kind + ", newest first", Args: cobra.ExactArgs(1),
		Long: "List the comments of a " + kind + ", newest first. Comments written by Taiga's GitLab integration are\n" +
			"hidden unless --include-system; comments of any other author are always listed."}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validRef(args[0]); err != nil {
			return err
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		items, err := service.Comments(cmd.Context(), kind, args[0], include)
		if err != nil {
			return err
		}
		return a.renderComments(items)
	}
	cmd.Flags().BoolVar(&include, "include-system", false, "also list the comments of Taiga's GitLab integration")
	return cmd
}

var commentTextKeys = []string{"id", "created_at", "author", "is_system", "edited_at", "deleted_at", "comment", "url"}

// renderComments writes the history entries as JSON, or in text one block per comment with
// the author's username and the edit/delete dates when there are any.
func (a *App) renderComments(items []app.Object) error {
	items = app.Scrub(items).([]app.Object) // the history user carries a signed photo
	mode, err := output.DetectMode(a.output, a.OutTTY)
	if err != nil {
		return err
	}
	if mode == output.JSON {
		return a.writeJSON(items)
	}
	text := []app.Object{}
	for _, o := range items {
		t := app.Object{"id": o["id"], "created_at": o["created_at"], "is_system": o["is_system"], "comment": o["comment"], "url": o["url"]}
		if user, ok := o["user"].(map[string]any); ok {
			t["author"] = user["username"]
		}
		if o["edit_comment_date"] != nil {
			t["edited_at"] = o["edit_comment_date"]
		}
		if o["delete_comment_date"] != nil {
			t["deleted_at"] = o["delete_comment_date"]
		}
		text = append(text, t)
	}
	return a.renderKeys(text, commentTextKeys)
}
