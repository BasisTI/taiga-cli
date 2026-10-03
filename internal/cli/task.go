package cli

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/spf13/cobra"
)

// taskTextKeys is the text summary of a task; JSON output carries the whole object.
var taskTextKeys = []string{"ref", "id", "version", "subject", "user_story", "status", "is_closed", "tags", "assigned_to", "is_blocked", "due_date", "url"}

func (a *App) renderTask(v any) error { return a.renderKeys(v, taskTextKeys) }

func (a *App) taskCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Read and update tasks"}
	cmd.AddCommand(a.taskListCmd(), a.taskGetCmd(), a.taskWriteCmd(false), a.taskWriteCmd(true), a.taskCloseCmd())
	return cmd
}

// resolveTaskStatus turns a task status name or id into its id in the project.
func resolveTaskStatus(cmd *cobra.Command, service *app.Service, selector string) (any, error) {
	items, err := service.Catalog(cmd.Context(), "task-statuses")
	if err != nil {
		return nil, err
	}
	found, err := app.Resolve(items, selector, "name")
	if err != nil {
		return nil, err
	}
	return found["id"], nil
}

func (a *App) taskGetCmd() *cobra.Command {
	var id int64
	cmd := &cobra.Command{Use: "get [REF]", Short: "Get a task by reference or id", Args: cobra.MaximumNArgs(1)}
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
		raw, err := service.Task(cmd.Context(), ref, id)
		if err != nil {
			return err
		}
		view, err := service.TaskView(raw)
		if err != nil {
			return err
		}
		values, err := service.FieldValues(cmd.Context(), "task", app.ID(raw["id"]))
		if err != nil {
			return err
		}
		view["custom_attributes"] = values
		return a.renderTask(view)
	}
	cmd.Flags().Int64Var(&id, "id", 0, "internal task id, exclusive with REF")
	return cmd
}

func (a *App) taskListCmd() *cobra.Command {
	var story, status, assignee, search string
	var tags []string
	var closed bool
	cmd := &cobra.Command{Use: "list", Short: "List every matching task", Args: cobra.NoArgs}
	isClosed := closedFlag(cmd, &closed, "tasks")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if cmd.Flags().Changed("story") {
			if err := validRef(story); err != nil {
				return app.Usage("--story must be a story reference (a positive integer)")
			}
		}
		for _, name := range []string{"status", "assignee", "search"} {
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
		if search != "" {
			q.Set("search", search)
		}
		if c := isClosed(); c != nil {
			q.Set("closed", strconv.FormatBool(*c))
		}
		if story != "" {
			s, err := service.Story(ctx, story, 0)
			if err != nil {
				return err
			}
			q.Set("story", strconv.FormatInt(app.ID(s["id"]), 10))
		}
		if status != "" {
			id, err := resolveTaskStatus(cmd, service, status)
			if err != nil {
				return err
			}
			q.Set("status", strconv.FormatInt(app.ID(id), 10))
		}
		if assignee != "" {
			user, err := service.Member(ctx, assignee)
			if err != nil {
				return err
			}
			q.Set("assignee", strconv.FormatInt(app.ID(user["id"]), 10))
		}
		result, err := service.Tasks(ctx, q)
		if err != nil {
			return err
		}
		return a.renderTask(result)
	}
	f := cmd.Flags()
	f.StringVar(&story, "story", "", "story reference")
	f.StringVar(&status, "status", "", "task status name or id")
	f.StringVar(&assignee, "assignee", "", "project member: username, id or me")
	f.StringArrayVar(&tags, "tag", nil, "required tag, repeatable (all must match)")
	f.StringVar(&search, "search", "", "case-insensitive text in the subject")
	return cmd
}

// validDate checks a YYYY-MM-DD date.
func validDate(flag, v string) error {
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return app.Usage(flag + " must be a valid YYYY-MM-DD date: " + v)
	}
	return nil
}

func (a *App) taskWriteCmd(update bool) *cobra.Command {
	var fl itemFlags
	var story, assignee, block, due string
	var clearAssignee, unblock, clearDue bool
	use, short := "create", "Create a task in a story"
	args := cobra.NoArgs
	if update {
		use, short, args = "update REF", "Update a task", cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{Use: use, Short: short, Args: args}
	cmd.RunE = func(cmd *cobra.Command, argv []string) error {
		f := cmd.Flags()
		if update {
			if err := validRef(argv[0]); err != nil {
				return err
			}
		} else {
			if !f.Changed("story") {
				return app.Usage("--story is required: a task belongs to a story (a task without story only through `taiga api`)")
			}
			if err := validRef(story); err != nil {
				return app.Usage("--story must be a story reference (a positive integer)")
			}
		}
		if f.Changed("assignee") && strings.TrimSpace(assignee) == "" {
			return app.Usage("--assignee cannot be blank")
		}
		if f.Changed("assignee") && clearAssignee {
			return app.Usage("choose --assignee or --clear-assignee")
		}
		if f.Changed("block") && unblock {
			return app.Usage("choose --block or --unblock")
		}
		if f.Changed("block") && strings.TrimSpace(block) == "" {
			return app.Usage("--block requires a note")
		}
		if f.Changed("due-date") && clearDue {
			return app.Usage("choose --due-date or --clear-due-date")
		}
		if f.Changed("due-date") {
			if err := validDate("--due-date", due); err != nil {
				return err
			}
		}
		patch, err := fl.patch(a, cmd, update)
		if err != nil {
			return err
		}
		if f.Changed("due-date") {
			patch.Set["due_date"] = due
		}
		if clearDue {
			patch.Set["due_date"] = nil
		}
		if clearAssignee {
			patch.Set["assigned_to"] = nil
		}
		if f.Changed("block") {
			patch.Block = &block
		}
		patch.Unblock = unblock
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		if f.Changed("status") {
			if patch.Set["status"], err = resolveTaskStatus(cmd, service, fl.status); err != nil {
				return err
			}
		}
		if f.Changed("assignee") {
			user, err := service.Member(ctx, assignee)
			if err != nil {
				return err
			}
			patch.Set["assigned_to"] = user["id"]
		}
		var result any
		if update {
			result, err = service.UpdateTask(ctx, argv[0], patch, fl.dry, fl.force)
		} else {
			parent, serr := service.Story(ctx, story, 0)
			if serr != nil {
				return serr
			}
			result, err = service.CreateTask(ctx, parent, patch.Set, fl.dry)
		}
		if err != nil {
			return err
		}
		return a.renderTask(result)
	}
	fl.register(cmd, "task", update)
	f := cmd.Flags()
	f.StringVar(&assignee, "assignee", "", "the assignee: project username, id or me")
	f.StringVar(&due, "due-date", "", "due date, YYYY-MM-DD")
	if update {
		f.BoolVar(&clearAssignee, "clear-assignee", false, "remove the assignee")
		f.StringVar(&block, "block", "", "block the task with a note")
		f.BoolVar(&unblock, "unblock", false, "unblock the task and clear its note")
		f.BoolVar(&clearDue, "clear-due-date", false, "remove the due date")
	} else {
		f.StringVar(&story, "story", "", "reference of the story the task belongs to (required)")
	}
	return cmd
}

func (a *App) taskCloseCmd() *cobra.Command {
	var status string
	var dry, force bool
	cmd := &cobra.Command{Use: "close REF", Short: "Move a task to a closed status (never tags, archives or deletes)", Args: cobra.ExactArgs(1)}
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
		result, err := service.CloseTask(cmd.Context(), args[0], status, dry, force)
		if err != nil {
			return err
		}
		return a.renderTask(result)
	}
	cmd.Flags().StringVar(&status, "status", "", "closed status name or id (default: the project's only closed status)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request without sending it")
	cmd.Flags().BoolVar(&force, "force-version", false, "on a version conflict, retry even if the same fields changed")
	return cmd
}
