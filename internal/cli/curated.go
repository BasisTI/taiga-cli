package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/app"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/spf13/cobra"
)

// service builds the per-run application service; credentials come only from App.client.
func (a *App) service(cmd *cobra.Command) (*app.Service, error) {
	rc, err := a.runContext()
	if err != nil {
		return nil, err
	}
	c, err := a.client(cmd.Context(), rc)
	if err != nil {
		return nil, err
	}
	return app.New(cmd.Context(), c, rc.Ctx.Project.Value)
}

// readContent returns the exact bytes of FILE, or of stdin for "-".
func (a *App) readContent(path string) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(a.In)
		return string(b), err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", app.Usage("cannot read input file: " + err.Error())
	}
	return string(b), nil
}

// storyTextKeys is the text summary of a story; JSON output carries the whole object.
var storyTextKeys = []string{"ref", "id", "version", "subject", "status", "is_closed", "tags", "assigned_to", "assigned_users", "is_blocked", "url"}

func (a *App) renderCurated(v any) error { return a.renderKeys(v, storyTextKeys) }

// linkedStoryRefs is the text form of linked stories (epic get): the ref, prefixed with the project
// slug for a story of another project, or the id when the account cannot read the story.
func linkedStoryRefs(links []app.Object) []string {
	out := []string{}
	for _, l := range links {
		switch {
		case l["ref"] == nil:
			out = append(out, fmt.Sprintf("id:%v", l["id"]))
		case l["project_slug"] != nil:
			out = append(out, fmt.Sprintf("%v#%v", l["project_slug"], l["ref"]))
		default:
			out = append(out, fmt.Sprint(l["ref"]))
		}
	}
	return out
}

// searchFlag adds --search to cmd; the returned func checks it was not given blank.
func searchFlag(cmd *cobra.Command, search *string, what string) func() error {
	cmd.Flags().StringVar(search, "search", "", what+" contains this text, ignoring case (accents must match)")
	return func() error {
		if cmd.Flags().Changed("search") && strings.TrimSpace(*search) == "" {
			return app.Usage("--search must not be blank")
		}
		return nil
	}
}

// closedFlag adds --closed to cmd; the returned func gives nil when it was not given.
func closedFlag(cmd *cobra.Command, closed *bool, what string) func() *bool {
	cmd.Flags().BoolVar(closed, "closed", false, "only closed "+what+" (--closed=false: only open ones)")
	return func() *bool {
		if !cmd.Flags().Changed("closed") {
			return nil
		}
		return closed
	}
}

// unsafeRune is a rune that can break or disguise a "key: value" line (output.UnsafeRune).
func unsafeRune(r rune) bool { return output.UnsafeRune(r) }
