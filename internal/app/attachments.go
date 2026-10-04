package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"html"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// attachmentPaths maps the kind of the owner to its attachment endpoint.
var attachmentPaths = map[string]string{"story": "userstories/attachments", "task": "tasks/attachments"}

func attachmentPath(kind string) (string, error) {
	p, ok := attachmentPaths[kind]
	if !ok {
		return "", Usage("attachments belong to a story or a task, not " + kind)
	}
	return p, nil
}

// attachments lists every attachment of owner as Taiga sends it, url included. The list is
// filtered by kind on the server; the object and the project are checked here too.
func (s *Service) attachments(ctx context.Context, path string, owner Object) ([]Object, error) {
	raws, err := s.API.GetAll(ctx, path, url.Values{"project": {s.projectID()}, "object_id": {fmt.Sprint(owner["id"])}})
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	out := []Object{}
	for _, raw := range raws {
		o, err := Decode(raw)
		if err != nil {
			return nil, err
		}
		if ID(o["object_id"]) != ID(owner["id"]) || fmt.Sprint(o["project"]) != s.projectID() {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

// attachmentView is the output form. Signed links (url, preview_url, thumbnail_card_url, or any
// other string carrying a token) are left out: they open the file without authentication while
// the token lasts; `attachment download` is the way to the file.
func attachmentView(o Object) Object {
	out := Object{}
	for k, v := range o {
		if text, ok := v.(string); ok && (strings.HasSuffix(k, "url") || strings.Contains(text, "token=")) {
			continue
		}
		out[k] = v
	}
	return out
}

// Attachments lists the attachments of a story or task (kind "story" or "task").
func (s *Service) Attachments(ctx context.Context, kind string, owner Object) ([]Object, error) {
	path, err := attachmentPath(kind)
	if err != nil {
		return nil, err
	}
	items, err := s.attachments(ctx, path, owner)
	if err != nil {
		return nil, err
	}
	out := make([]Object, 0, len(items))
	for _, o := range items {
		out = append(out, attachmentView(o))
	}
	return out, nil
}

// UploadPlan is what --dry-run prints for an upload: the form fields and the file's
// metadata, never its content.
type UploadPlan struct {
	DryRun bool              `json:"dry_run"`
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Fields map[string]string `json:"fields"`
	File   UploadFile        `json:"file"`
}

type UploadFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	SHA1 string `json:"sha1"`
}

// localFile checks that path is a non-empty regular file (symbolic links followed) and
// returns its name, size and sha1 from a first full read.
func localFile(path string) (UploadFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return UploadFile{}, Usage("cannot read the file: " + err.Error())
	}
	if !info.Mode().IsRegular() {
		return UploadFile{}, Usage(fmt.Sprintf("%s is not a regular file", path))
	}
	f, err := os.Open(path)
	if err != nil {
		return UploadFile{}, Usage("cannot read the file: " + err.Error())
	}
	defer func() { _ = f.Close() }()
	h := sha1.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return UploadFile{}, Usage("cannot read the file: " + err.Error())
	}
	if n == 0 {
		return UploadFile{}, Usage(fmt.Sprintf("%s is empty: Taiga refuses empty attachments", path))
	}
	name := filepath.Base(path)
	if rewritten(name) {
		return UploadFile{}, Usage(fmt.Sprintf("%q: Taiga does not store this file name as sent (control characters, a backslash or an HTML entity); rename the file", name))
	}
	return UploadFile{Name: name, Size: n, SHA1: hex.EncodeToString(h.Sum(nil))}, nil
}

// rewritten is a file name Taiga would store differently from what is sent: the multipart
// encoder escapes CR and LF, and Django keeps only what follows a backslash and applies
// Python's html.unescape, which also takes entities without ";" (report&amp.txt, &#65). The
// stored name must be the local one, or the idempotence by name and sha1 breaks. Go's
// html.UnescapeString follows the same HTML5 rules; any "&#" is refused as well, so a numeric
// form the two treat differently never gets through.
func rewritten(name string) bool {
	return strings.ContainsFunc(name, func(r rune) bool { return unicode.Is(unicode.Cc, r) || r == '\\' }) ||
		html.UnescapeString(name) != name || strings.Contains(name, "&#")
}

// sameFile is an attachment with the name and content of file.
func sameFile(o Object, file UploadFile) bool {
	return fmt.Sprint(o["name"]) == file.Name && fmt.Sprint(o["sha1"]) == file.SHA1
}

// Upload attaches the file at path to owner. An attachment with the same name and sha1 already
// there is returned without sending (created: false). The POST is sent once and never
// repeated; its outcome is checked by the answer (sha1, size, object_id), and when the answer is
// lost by a new id with the same name and sha1 in the list.
func (s *Service) Upload(ctx context.Context, kind string, owner Object, path, description string, dry bool) (any, error) {
	endpoint, err := attachmentPath(kind)
	if err != nil {
		return nil, err
	}
	file, err := localFile(path)
	if err != nil {
		return nil, err
	}
	before, err := s.attachments(ctx, endpoint, owner)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, o := range before {
		if sameFile(o, file) {
			return created(o, false), nil
		}
		known[fmt.Sprint(o["id"])] = true
	}
	fields := map[string]string{"project": s.projectID(), "object_id": fmt.Sprint(owner["id"]), "description": description}
	if dry {
		return redactPlan(UploadPlan{true, "POST", endpoint, fields, file})
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, Usage("cannot read the file: " + err.Error())
	}
	defer func() { _ = f.Close() }()
	sent := &countingReader{r: f}
	resp, err := s.API.Upload(ctx, endpoint, fields, "attached_file", file.Name, sent, file.Size)
	if err != nil && sent.n < file.Size && sent.eof {
		// The body ended before its Content-Length: the request was cut, nothing was stored.
		return nil, Usage(fmt.Sprintf("%s changed while it was sent (%d bytes read, %d expected); nothing was stored, send it again", path, sent.n, file.Size))
	}
	var ue *taiga.UnreadableBodyError
	switch {
	case err == nil:
		answer, derr := Decode(resp.Body)
		if derr != nil {
			return s.findUpload(ctx, endpoint, owner, file, known, &taiga.UnreadableBodyError{Method: "POST", Path: endpoint, Status: resp.Status, Err: derr})
		}
		if !sameFile(answer, file) || ID(answer["size"]) != file.Size || ID(answer["object_id"]) != ID(owner["id"]) {
			return nil, &output.Error{Code: "attachment_postcondition_failed", Source: "api", Stage: "POST " + endpoint,
				Cause: fmt.Sprintf("attachment %v was stored, but it differs from the file sent: name %q (sent %q), sha1 %v (sent %s), size %v (sent %d), object %v (sent %v); the file may have changed while it was sent, or Taiga rewrote its name",
					answer["id"], fmt.Sprint(answer["name"]), file.Name, answer["sha1"], file.SHA1, answer["size"], file.Size, answer["object_id"], owner["id"]),
				Recovery: "do not upload again blindly: check the attachment with `taiga attachment list`; it is saved as it arrived",
				Exit:     output.ExitConflict}
		}
		return created(answer, true), nil
	case errors.As(err, &ue):
		return s.findUpload(ctx, endpoint, owner, file, known, ue)
	case !uncertain(err):
		return nil, taiga.ToOutput(err)
	}
	return s.findUpload(ctx, endpoint, owner, file, known, err)
}

// checkTimeout bounds the list read that confirms an upload whose answer was lost.
const checkTimeout = 30 * time.Second

// redactPlan is the printed form of a plan: every string in it, at any depth, goes through
// taiga.RedactSecrets, so a signed URL in a description or a file name never reaches the
// output. Only the printout is redacted; the request keeps the values as given. It is a plain
// map, not an Object, so text output prints every key like other plans.
func redactPlan(plan any) (map[string]any, error) {
	b, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	o, err := Decode(b)
	if err != nil {
		return nil, err
	}
	return map[string]any(redactValue(o).(Object)), nil
}

func redactValue(v any) any {
	switch x := v.(type) {
	case string:
		return taiga.RedactSecrets(x)
	case Object:
		for k, e := range x {
			x[k] = redactValue(e)
		}
	case map[string]any:
		for k, e := range x {
			x[k] = redactValue(e)
		}
	case []any:
		for i, e := range x {
			x[i] = redactValue(e)
		}
	}
	return v
}

func created(o Object, isNew bool) Object {
	out := attachmentView(o)
	out["created"] = isNew
	return out
}

// findUpload looks for the attachment of a POST without a conclusive answer: a new id with the
// name and sha1 sent. A 2xx whose body was lost is applied, so the new one is the answer and not
// finding it is write_applied. An unknown outcome (network after the connection opened, 5xx,
// 3xx) is always attachment_unconfirmed, exit 1, naming the new ones: another process of the
// same account may have attached the same file, so none of them is proof (decision of
// 2026-10-03, option B), and none found proves no absence, because the POST may still be
// running on the server.
func (s *Service) findUpload(ctx context.Context, endpoint string, owner Object, file UploadFile, known map[string]bool, sendErr error) (any, error) {
	// The upload may have failed because its context is done (--timeout, interrupt): the check
	// gets a short deadline of its own, or it could never confirm a stored file.
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
	defer cancel()
	after, err := s.attachments(check, endpoint, owner)
	found := []Object{}
	if err == nil {
		for _, o := range after {
			if !known[fmt.Sprint(o["id"])] && sameFile(o, file) {
				found = append(found, o)
			}
		}
	}
	var ue *taiga.UnreadableBodyError
	if errors.As(sendErr, &ue) {
		if len(found) > 0 {
			return created(found[0], true), nil
		}
		if err == nil {
			err = fmt.Errorf("the new attachment is not in the list of %v", owner["id"])
		}
		return nil, WriteApplied("POST", endpoint, ue.Status, err)
	}
	checked := "the list does not show it yet, but the request may still be running on the server"
	switch {
	case err != nil:
		checked = "the list could not be read to check: " + output.AsError(err).Error()
	case len(found) > 0:
		ids := []string{}
		for _, o := range found {
			ids = append(ids, fmt.Sprint(o["id"]))
		}
		checked = fmt.Sprintf("the list shows %d new attachment(s) with this name and sha1 (id %s), but nothing proves this command attached them", len(found), strings.Join(ids, ", "))
	}
	return nil, &output.Error{Code: "attachment_unconfirmed", Source: output.AsError(taiga.ToOutput(sendErr)).Source, Stage: "POST " + endpoint,
		Cause:    fmt.Sprintf("the file may have been attached: POST %s failed (%v) and %s", endpoint, sendErr, checked),
		Recovery: fmt.Sprintf("do not upload again blindly: it would attach the file twice, and an attachment not found yet may still be saved; check with `taiga attachment list %v`", owner["ref"]),
		Exit:     output.ExitUnexpected}
}

// countingReader counts the bytes read and whether the source ended.
type countingReader struct {
	r   io.Reader
	n   int64
	eof bool
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if err == io.EOF {
		c.eof = true
	}
	return n, err
}

// safeName makes the attachment name from the server a local file name: only its last path
// element, with controls, format characters (bidi) and a leading dot replaced by "_"; ".",
// ".." and an empty name become attachment-<id>.
func safeName(name string, id int64) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r == 0 || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return fmt.Sprintf("attachment-%d", id)
	}
	// A leading dot would plant a hidden file (.bashrc, .envrc) in the destination directory.
	if strings.HasPrefix(name, ".") {
		name = "_" + name[1:]
	}
	return name
}

// DownloadAttachment saves attachment id of owner to dest: a file, an existing directory (the
// server's name, made safe), "" for the working directory, or "-" for out. The attachment must
// be in the owner's list, which is filtered by kind (the detail endpoint is not). An existing
// file is replaced only with overwrite. The bytes go to a temporary file in the same directory
// and become dest only after their size and sha1 match the attachment.
func (s *Service) DownloadAttachment(ctx context.Context, kind string, owner Object, id int64, dest string, overwrite bool, out io.Writer) (Object, error) {
	endpoint, err := attachmentPath(kind)
	if err != nil {
		return nil, err
	}
	items, err := s.attachments(ctx, endpoint, owner)
	if err != nil {
		return nil, err
	}
	var listed Object
	for _, o := range items {
		if ID(o["id"]) == id {
			listed = o
		}
	}
	if listed == nil {
		return nil, &output.Error{Code: "not_found", Cause: fmt.Sprintf("attachment %d is not attached to %s %v", id, kind, owner["ref"]), Recovery: "list them with `taiga attachment list`", Exit: output.ExitNotFound}
	}
	// A fresh read signs a new token for the url.
	a, err := Read(ctx, s.API, fmt.Sprintf("%s/%d", endpoint, id), nil)
	if err != nil {
		return nil, err
	}
	if ID(a["id"]) != id || ID(a["object_id"]) != ID(owner["id"]) {
		return nil, &output.Error{Code: "not_found", Cause: fmt.Sprintf("attachment %d is not attached to %s %v", id, kind, owner["ref"]), Exit: output.ExitNotFound}
	}
	rawURL, _ := a["url"].(string)
	if rawURL == "" {
		return nil, fmt.Errorf("attachment %d has no url", id)
	}
	result := attachmentView(a)
	if dest == "-" {
		h := sha1.New()
		n, err := s.API.Download(ctx, rawURL, io.MultiWriter(out, h))
		if err != nil {
			return nil, taiga.ToOutput(err)
		}
		if err := checkDownload(a, n, h); err != nil {
			return nil, err
		}
		return result, nil
	}
	target, err := destination(dest, safeName(fmt.Sprint(a["name"]), id))
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(target); err == nil && !overwrite {
		return nil, Usage(fmt.Sprintf("%s already exists; pass --overwrite to replace it", target))
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".taiga-download-*")
	if err != nil {
		return nil, taiga.ToOutput(&taiga.LocalWriteError{Err: err})
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	h := sha1.New()
	n, err := s.API.Download(ctx, rawURL, io.MultiWriter(tmp, h))
	if cerr := tmp.Close(); err == nil && cerr != nil {
		return nil, taiga.ToOutput(&taiga.LocalWriteError{Err: cerr})
	}
	if err != nil {
		return nil, taiga.ToOutput(err)
	}
	if err := checkDownload(a, n, h); err != nil {
		return nil, err
	}
	// The temporary file is 0600 while unchecked; the saved one gets the user's umask, like any
	// file the user creates.
	if err := os.Chmod(tmp.Name(), 0o666&^currentUmask()); err != nil {
		return nil, taiga.ToOutput(&taiga.LocalWriteError{Err: err})
	}
	if overwrite {
		err = os.Rename(tmp.Name(), target)
	} else {
		// A link fails if the destination appeared meanwhile; rename would replace it.
		err = os.Link(tmp.Name(), target)
		if errors.Is(err, fs.ErrExist) {
			return nil, Usage(fmt.Sprintf("%s already exists; pass --overwrite to replace it", target))
		}
	}
	if err != nil {
		return nil, taiga.ToOutput(&taiga.LocalWriteError{Err: err})
	}
	result["path"] = target
	return result, nil
}

// destination is dest itself, or dest/name when dest is a directory ("" is the working one).
func destination(dest, name string) (string, error) {
	if dest == "" {
		dest = "."
	}
	if info, err := os.Stat(dest); err == nil && info.IsDir() {
		return filepath.Join(dest, name), nil
	}
	if info, err := os.Stat(filepath.Dir(dest)); err != nil || !info.IsDir() {
		return "", Usage(fmt.Sprintf("the directory of %s does not exist", dest))
	}
	return dest, nil
}

// checkDownload compares the bytes received with the attachment's size and sha1.
func checkDownload(a Object, n int64, h hash.Hash) error {
	got := hex.EncodeToString(h.Sum(nil))
	if n == ID(a["size"]) && got == fmt.Sprint(a["sha1"]) {
		return nil
	}
	return &output.Error{Code: "attachment_download_mismatch", Source: "network", Stage: fmt.Sprintf("GET attachment %v", a["id"]),
		Cause:    fmt.Sprintf("the bytes received do not match attachment %v: %d bytes with sha1 %s, expected %v bytes with sha1 %v", a["id"], n, got, a["size"], a["sha1"]),
		Recovery: "nothing was saved; download again (with --to -, discard what was read)",
		Exit:     output.ExitNetwork}
}
