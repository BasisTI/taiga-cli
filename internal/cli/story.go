package cli

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/spf13/cobra"
)

func validRef(ref string) error {
	n, err := strconv.ParseInt(ref, 10, 64)
	if err != nil || n <= 0 {
		return app.Usage("reference must be a positive integer")
	}
	return nil
}

// tagNames validates tag flags and returns them as Taiga stores them (lower case).
func tagNames(flag string, tags []string) ([]string, error) {
	out := []string{}
	for _, t := range tags {
		if strings.TrimSpace(t) == "" {
			return nil, app.Usage(flag + " cannot be blank")
		}
		out = append(out, app.TagName(t))
	}
	return out, nil
}

// errEpicLink blocks --epic on writes: the probe showed "epics" is ignored by PATCH and the link
// is an unversioned POST whose replacement needs DELETE (docs/api-notes.md).
var errEpicLink = app.Unsupported("linking a story to an epic is not supported yet: Taiga links epics through epics/<id>/related_userstories, without version, and replacing a link requires DELETE",
	"link the story in the Taiga web UI, or with `taiga api POST epics/<id>/related_userstories`")

func (a *App) storyCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "story", Short: "Read and update user stories"}
	cmd.AddCommand(a.storyListCmd(), a.storyGetCmd(), a.storyWriteCmd(false), a.storyWriteCmd(true), a.storyCloseCmd())
	return cmd
}

func (a *App) storyGetCmd() *cobra.Command {
	var id int64
	cmd := &cobra.Command{Use: "get [REF]", Short: "Get a story by reference or id", Args: cobra.MaximumNArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		hasRef, hasID := len(args) == 1, cmd.Flags().Changed("id")
		if hasRef == hasID || (hasID && id <= 0) {
			return app.Usage("choose REF or a positive --id")
		}
		ref := ""
		if hasRef {
			ref = args[0]
			if err := validRef(ref); err != nil {
				return err
			}
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		raw, err := service.Story(cmd.Context(), ref, id)
		if err != nil {
			return err
		}
		view, err := service.StoryView(raw)
		if err != nil {
			return err
		}
		return a.renderCurated(view)
	}
	cmd.Flags().Int64Var(&id, "id", 0, "internal story id, exclusive with REF")
	return cmd
}

func (a *App) storyListCmd() *cobra.Command {
	var ref, status, assignee, epic, search string
	var tags []string
	var closed bool
	cmd := &cobra.Command{Use: "list", Short: "List every matching story", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if cmd.Flags().Changed("ref") {
			if err := validRef(ref); err != nil {
				return err
			}
		}
		for _, name := range []string{"status", "assignee", "epic", "search"} {
			if cmd.Flags().Changed(name) && strings.TrimSpace(cmd.Flags().Lookup(name).Value.String()) == "" {
				return app.Usage("--" + name + " cannot be blank")
			}
		}
		names, err := tagNames("--tag", tags)
		if err != nil {
			return err
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		q := url.Values{"tag": names}
		if ref != "" {
			q.Set("ref", ref)
		}
		if search != "" {
			q.Set("search", search)
		}
		if cmd.Flags().Changed("closed") {
			q.Set("closed", strconv.FormatBool(closed))
		}
		if status != "" {
			items, err := service.Catalog(ctx, "userstory-statuses")
			if err != nil {
				return err
			}
			st, err := app.Resolve(items, status, "name")
			if err != nil {
				return err
			}
			q.Set("status", strconv.FormatInt(app.ID(st["id"]), 10))
		}
		if assignee != "" {
			user, err := service.Member(ctx, assignee)
			if err != nil {
				return err
			}
			q.Set("assignee", strconv.FormatInt(app.ID(user["id"]), 10))
		}
		if epic != "" {
			e, err := service.Epic(ctx, epic)
			if err != nil {
				return err
			}
			q.Set("epic", strconv.FormatInt(app.ID(e["id"]), 10))
		}
		result, err := service.Stories(ctx, q)
		if err != nil {
			return err
		}
		return a.renderCurated(result)
	}
	f := cmd.Flags()
	f.StringVar(&ref, "ref", "", "story reference")
	f.StringVar(&status, "status", "", "status name or id")
	f.StringVar(&assignee, "assignee", "", "project member: username, id or me")
	f.StringVar(&epic, "epic", "", "epic reference")
	f.StringArrayVar(&tags, "tag", nil, "required tag, repeatable (all must match)")
	f.StringVar(&search, "search", "", "case-insensitive text in the subject")
	f.BoolVar(&closed, "closed", false, "only closed stories; --closed=false for open ones")
	return cmd
}

// resolveStoryValue turns a status, milestone or swimlane name or id into its id in the project.
func (a *App) resolveStoryValue(cmd *cobra.Command, service *app.Service, field, selector string) (any, error) {
	path := map[string]string{"status": "userstory-statuses", "milestone": "milestones", "swimlane": "swimlanes"}[field]
	items, err := service.Catalog(cmd.Context(), path)
	if err != nil {
		return nil, err
	}
	found, err := app.Resolve(items, selector, "name")
	if err != nil {
		return nil, err
	}
	return found["id"], nil
}

func (a *App) storyWriteCmd(update bool) *cobra.Command {
	var subject, descriptionFile, appendText, status, epic, milestone, swimlane string
	var tags, addTags, removeTags []string
	var dry, force bool
	use, short := "create", "Create a story"
	args := cobra.NoArgs
	if update {
		use, short, args = "update REF", "Update a story", cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{Use: use, Short: short, Args: args}
	cmd.RunE = func(cmd *cobra.Command, argv []string) error {
		f := cmd.Flags()
		if update {
			if err := validRef(argv[0]); err != nil {
				return err
			}
		}
		if (!update || f.Changed("subject")) && strings.TrimSpace(subject) == "" {
			return app.Usage("--subject cannot be blank")
		}
		if !update && force {
			return app.Usage("--force-version is not valid for creation")
		}
		if f.Changed("description-file") && f.Changed("append-description") {
			return app.Usage("choose --description-file or --append-description")
		}
		if f.Changed("tag") && (len(addTags)+len(removeTags) > 0) {
			return app.Usage("choose --tag (replace) or --add-tag/--remove-tag (merge)")
		}
		replace, err := tagNames("--tag", tags)
		if err != nil {
			return err
		}
		add, err := tagNames("--add-tag", addTags)
		if err != nil {
			return err
		}
		remove, err := tagNames("--remove-tag", removeTags)
		if err != nil {
			return err
		}
		for _, x := range add {
			for _, y := range remove {
				if x == y {
					return app.Usage("tag cannot be added and removed together: " + x)
				}
			}
		}
		for _, name := range []string{"status", "milestone", "swimlane"} {
			if f.Changed(name) && strings.TrimSpace(f.Lookup(name).Value.String()) == "" {
				return app.Usage("--" + name + " cannot be blank")
			}
		}
		if f.Changed("epic") {
			return errEpicLink
		}
		patch := app.Patch{Set: app.Object{}, AddTags: add, RemoveTags: remove}
		if f.Changed("subject") {
			patch.Set["subject"] = subject
		}
		if f.Changed("description-file") {
			text, err := a.readContent(descriptionFile)
			if err != nil {
				return err
			}
			patch.Set["description"] = text
		}
		if f.Changed("append-description") {
			patch.Append = &appendText
		}
		if f.Changed("tag") {
			patch.Set["tags"] = app.MergeNames(nil, replace, nil)
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		for _, pair := range [][2]string{{"status", status}, {"milestone", milestone}, {"swimlane", swimlane}} {
			if !f.Changed(pair[0]) {
				continue
			}
			value, err := a.resolveStoryValue(cmd, service, pair[0], pair[1])
			if err != nil {
				return err
			}
			patch.Set[pair[0]] = value
		}
		var result any
		if update {
			result, err = service.UpdateStory(cmd.Context(), argv[0], patch, dry, force)
		} else {
			result, err = service.CreateStory(cmd.Context(), patch.Set, dry)
		}
		if err != nil {
			return err
		}
		return a.renderCurated(result)
	}
	f := cmd.Flags()
	f.StringVar(&subject, "subject", "", "story subject")
	f.StringVar(&descriptionFile, "description-file", "", "read the description from FILE, or - for stdin")
	f.StringVar(&status, "status", "", "status name or id")
	f.StringVar(&swimlane, "swimlane", "", "swimlane name or id")
	f.StringVar(&epic, "epic", "", "epic reference (not supported yet: see docs/api-notes.md)")
	f.StringArrayVar(&tags, "tag", nil, "set the tags, repeatable (replaces the current ones)")
	if update {
		f.StringVar(&appendText, "append-description", "", "append text to the description, after a blank line")
		f.StringVar(&milestone, "milestone", "", "milestone (sprint) name or id")
		f.StringArrayVar(&addTags, "add-tag", nil, "add a tag, repeatable")
		f.StringArrayVar(&removeTags, "remove-tag", nil, "remove a tag, repeatable")
	}
	f.BoolVar(&dry, "dry-run", false, "print the request without sending it")
	f.BoolVar(&force, "force-version", false, "on a version conflict, retry even if the same fields changed")
	return cmd
}

func (a *App) storyCloseCmd() *cobra.Command {
	var status string
	var dry, force bool
	cmd := &cobra.Command{Use: "close REF", Short: "Move a story to a closed status (never archives or deletes)", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validRef(args[0]); err != nil {
			return err
		}
		if cmd.Flags().Changed("status") && strings.TrimSpace(status) == "" {
			return app.Usage("--status cannot be blank")
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		result, err := service.CloseStory(cmd.Context(), args[0], status, dry, force)
		if err != nil {
			return err
		}
		return a.renderCurated(result)
	}
	cmd.Flags().StringVar(&status, "status", "", "closed status name or id (default: the project's only closed status)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request without sending it")
	cmd.Flags().BoolVar(&force, "force-version", false, "on a version conflict, retry even if the same fields changed")
	return cmd
}
