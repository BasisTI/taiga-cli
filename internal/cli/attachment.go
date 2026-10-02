package cli

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/spf13/cobra"
)

// attachmentTextKeys is the text summary of an attachment. The signed url never appears, in
// text or JSON: it opens the file without authentication while its token lasts.
var attachmentTextKeys = []string{"id", "name", "size", "sha1", "description", "created_date", "owner", "is_deprecated", "created", "path"}

// transferTimeout bounds a whole upload or download, which can outlast the 30 s of an API call.
const transferTimeout = 10 * time.Minute

// attachmentCmd lists, uploads and downloads attachments of stories and tasks. Editing and
// deleting attachments stay in the web UI.
func (a *App) attachmentCmd() *cobra.Command {
	parent := &cobra.Command{Use: "attachment", Short: "List, upload and download attachments of stories and tasks"}
	parent.AddCommand(a.attachmentListCmd(), a.attachmentUploadCmd(), a.attachmentDownloadCmd())
	return parent
}

// transferContext bounds a transfer by timeout and cancels it on SIGINT or SIGTERM, so the
// command can clean up (a download removes its temporary file; an upload checks the list)
// before exiting, instead of being killed mid-way.
func transferContext(parent context.Context, timeout time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	return ctx, func() { stop(); cancel() }
}

// attachmentOwner resolves REF as a story, or as a task with --task.
func attachmentOwner(ctx context.Context, service *app.Service, task bool, ref string) (string, app.Object, error) {
	if task {
		o, err := service.Task(ctx, ref, 0)
		return "task", o, err
	}
	o, err := service.Story(ctx, ref, 0)
	return "story", o, err
}

func (a *App) attachmentListCmd() *cobra.Command {
	var task bool
	cmd := &cobra.Command{Use: "list REF", Short: "List the attachments of a story (or of a task with --task)", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validRef(args[0]); err != nil {
			return err
		}
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		kind, owner, err := attachmentOwner(cmd.Context(), service, task, args[0])
		if err != nil {
			return err
		}
		items, err := service.Attachments(cmd.Context(), kind, owner)
		if err != nil {
			return err
		}
		return a.renderKeys(items, attachmentTextKeys)
	}
	cmd.Flags().BoolVar(&task, "task", false, "REF is a task, not a story")
	return cmd
}

func (a *App) attachmentUploadCmd() *cobra.Command {
	var task, dry bool
	var description string
	var timeout time.Duration
	cmd := &cobra.Command{Use: "upload REF FILE", Short: "Attach a local file to a story (or to a task with --task)", Args: cobra.ExactArgs(2),
		Long: "Attach a local file to a story (or to a task with --task). The upload is sent once and never\n" +
			"repeated. If the story already has an attachment with the same name and content (sha1), nothing\n" +
			"is sent and the existing one is returned with \"created\": false. The CLI has no size limit; the\n" +
			"proxy in front of Taiga has one (50 MB at Basis), and above it the upload fails with\n" +
			"payload_too_large. Empty files are refused, as Taiga refuses them."}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validRef(args[0]); err != nil {
			return err
		}
		if timeout <= 0 {
			return app.Usage("--timeout must be positive")
		}
		ctx, cancel := transferContext(cmd.Context(), timeout)
		defer cancel()
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		kind, owner, err := attachmentOwner(ctx, service, task, args[0])
		if err != nil {
			return err
		}
		result, err := service.Upload(ctx, kind, owner, args[1], description, dry)
		if err != nil {
			return err
		}
		return a.renderKeys(result, attachmentTextKeys)
	}
	cmd.Flags().BoolVar(&task, "task", false, "REF is a task, not a story")
	cmd.Flags().StringVar(&description, "description", "", "description of the attachment")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request (form fields and the file's name, size and sha1) without sending")
	cmd.Flags().DurationVar(&timeout, "timeout", transferTimeout, "maximum time for the whole upload")
	return cmd
}

func (a *App) attachmentDownloadCmd() *cobra.Command {
	var task, overwrite bool
	var to string
	var timeout time.Duration
	cmd := &cobra.Command{Use: "download REF ATTACHMENT_ID", Short: "Download an attachment of a story (or of a task with --task)", Args: cobra.ExactArgs(2),
		Long: "Download an attachment of a story (or of a task with --task) to --to: a file, an existing\n" +
			"directory (the attachment's name is used, made safe), or - for stdout. The default is the\n" +
			"working directory. An existing file is replaced only with --overwrite. The file is saved only\n" +
			"after its size and sha1 match the attachment; with --to -, a mismatch is reported at the end\n" +
			"(exit 7) and what was written must be discarded."}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validRef(args[0]); err != nil {
			return err
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || id <= 0 {
			return app.Usage("attachment id must be a positive integer")
		}
		if to == "-" && overwrite {
			return app.Usage("--overwrite has no effect with --to -")
		}
		if timeout <= 0 {
			return app.Usage("--timeout must be positive")
		}
		dest := to
		if dest == "" {
			dest = a.Cwd
		}
		ctx, cancel := transferContext(cmd.Context(), timeout)
		defer cancel()
		service, err := a.service(cmd)
		if err != nil {
			return err
		}
		kind, owner, err := attachmentOwner(ctx, service, task, args[0])
		if err != nil {
			return err
		}
		result, err := service.DownloadAttachment(ctx, kind, owner, id, dest, overwrite, a.Out)
		if err != nil || to == "-" {
			return err // with --to -, stdout carries only the file
		}
		return a.renderKeys(result, attachmentTextKeys)
	}
	cmd.Flags().BoolVar(&task, "task", false, "REF is a task, not a story")
	cmd.Flags().StringVar(&to, "to", "", "destination: a file, an existing directory, or - for stdout (default: the working directory)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing file")
	cmd.Flags().DurationVar(&timeout, "timeout", transferTimeout, "maximum time for the whole download")
	return cmd
}
