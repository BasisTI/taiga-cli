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
	cmd.AddCommand(a.storyListCmd(), a.storyGetCmd(), a.storyWriteCmd(false), a.storyWriteCmd(true), a.storyCloseCmd(), a.storyFieldCmd())
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
		values, err := service.FieldValues(cmd.Context(), "story", app.ID(raw["id"]))
		if err != nil {
			return err
		}
		view["custom_attributes"] = values
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

// assigneeFlags are the assignee and block flags: create takes the initial assignees, update
// merges into the current ones and changes the main assignee only when asked.
func assigneeFlags(cmd *cobra.Command, update bool) {
	f := cmd.Flags()
	if !update {
		f.StringArray("assignee", nil, "initial assignee (project username, id or me), repeatable")
		return
	}
	f.StringArray("add-assignee", nil, "add an assignee (project username, id or me), repeatable")
	f.StringArray("remove-assignee", nil, "remove an assignee (project username, id or me), repeatable")
	f.String("owner-assignee", "", "set the main assignee (assigned_to): project username, id or me")
	f.Bool("clear-owner-assignee", false, "clear the main assignee (assigned_to)")
	f.String("block", "", "block the story with a note")
	f.Bool("unblock", false, "unblock the story and clear its note")
}

// stringArray returns the values of a StringArray flag. pflag's GetStringArray re-parses the
// flag's String() form and turns a single empty value into no value at all.
func stringArray(cmd *cobra.Command, name string) []string {
	if flag := cmd.Flags().Lookup(name); flag != nil {
		if v, ok := flag.Value.(interface{ GetSlice() []string }); ok {
			return v.GetSlice()
		}
	}
	return nil
}

// checkAssigneeFlags rejects blank selectors and contradictory flags before any request.
func checkAssigneeFlags(cmd *cobra.Command, update bool) error {
	f := cmd.Flags()
	lists := []string{"assignee"}
	if update {
		lists = []string{"add-assignee", "remove-assignee"}
	}
	for _, name := range lists {
		for _, v := range stringArray(cmd, name) {
			if strings.TrimSpace(v) == "" {
				return app.Usage("--" + name + " cannot be blank")
			}
		}
	}
	if !update {
		return nil
	}
	owner, _ := f.GetString("owner-assignee")
	if f.Changed("owner-assignee") && strings.TrimSpace(owner) == "" {
		return app.Usage("--owner-assignee cannot be blank")
	}
	clearOwner, _ := f.GetBool("clear-owner-assignee")
	if f.Changed("owner-assignee") && clearOwner {
		return app.Usage("choose --owner-assignee or --clear-owner-assignee")
	}
	note, _ := f.GetString("block")
	unblock, _ := f.GetBool("unblock")
	if f.Changed("block") && unblock {
		return app.Usage("choose --block or --unblock")
	}
	if f.Changed("block") && strings.TrimSpace(note) == "" {
		return app.Usage("--block requires a note")
	}
	remove := stringArray(cmd, "remove-assignee")
	for _, r := range remove {
		for _, x := range append(stringArray(cmd, "add-assignee"), owner) {
			if x == r {
				return app.Usage("assignee cannot be added and removed together: " + r)
			}
		}
	}
	return nil
}

// resolveAssignees turns every assignee flag into project member ids in patch, before any write.
func resolveAssignees(cmd *cobra.Command, service *app.Service, patch *app.Patch, update bool) error {
	f := cmd.Flags()
	resolve := func(name string) ([]int64, error) {
		// Adding requires a member; removing does not, so a former member can be cleaned up.
		find := service.Member
		if name == "remove-assignee" {
			find = service.User
		}
		ids := []int64{}
		for _, v := range stringArray(cmd, name) {
			user, err := find(cmd.Context(), v)
			if err != nil {
				return nil, err
			}
			ids = append(ids, app.ID(user["id"]))
		}
		return app.MergeIDs(nil, ids, nil), nil
	}
	if !update {
		if f.Changed("assignee") {
			ids, err := resolve("assignee")
			if err != nil {
				return err
			}
			patch.Set["assigned_users"] = ids
		}
		return nil
	}
	var err error
	if patch.AddAssignees, err = resolve("add-assignee"); err != nil {
		return err
	}
	if patch.RemoveAssignees, err = resolve("remove-assignee"); err != nil {
		return err
	}
	if f.Changed("owner-assignee") {
		value, _ := f.GetString("owner-assignee")
		user, err := service.Member(cmd.Context(), value)
		if err != nil {
			return err
		}
		id := app.ID(user["id"])
		patch.Owner = &id
	}
	patch.ClearOwner, _ = f.GetBool("clear-owner-assignee")
	if f.Changed("block") {
		note, _ := f.GetString("block")
		patch.Block = &note
	}
	patch.Unblock, _ = f.GetBool("unblock")
	return nil
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
		if err := checkAssigneeFlags(cmd, update); err != nil {
			return err
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
		if err := resolveAssignees(cmd, service, &patch, update); err != nil {
			return err
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
	assigneeFlags(cmd, update)
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
