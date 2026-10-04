# taiga-cli

## What it is

`taiga` is a command line client for the [Taiga](https://taiga.io) REST API v1, built for people and for coding agents. It resolves the Taiga URL and project from flags, environment, a `.taiga.toml` in the repository or the user config, keeps a session cache so agents never handle passwords, and reports every failure as a stable JSON error envelope with an exit code.

`taiga api` reaches the whole API v1, including `version`-checked writes and transparent pagination, so anything without a curated command is still one call away. Licensed under Apache-2.0.

## Install

Install the binary in `~/.local/bin` (it must be on your `PATH`). Download the tarball for your platform from [GitHub Releases](https://github.com/BasisTI/taiga-cli/releases), together with `SHA256SUMS`, and check it before installing:

```sh
V=0.3.0
gh release download "v$V" -R BasisTI/taiga-cli -p "taiga_${V}_linux_amd64.tar.gz" -p SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf "taiga_${V}_linux_amd64.tar.gz" taiga
mkdir -p ~/.local/bin && install -m 0755 taiga ~/.local/bin/taiga
```

Or, with Go:

```sh
GOBIN="$HOME/.local/bin" go install github.com/BasisTI/taiga-cli/cmd/taiga@v0.3.0
```

### Agent skill

[`skills/taiga-cli/SKILL.md`](skills/taiga-cli/SKILL.md) teaches coding agents to use the CLI (in Portuguese): which command to use, how to stop and ask a person on authentication errors, and what to check after an uncertain write instead of repeating it. Install it with the [skills CLI](https://github.com/vercel-labs/skills):

```sh
npx skills add BasisTI/taiga-cli
```

## Quickstart

```sh
taiga auth login --url https://taiga.example.com --username me
echo 'url = "https://taiga.example.com"'  > .taiga.toml
echo 'project = "my-project"'           >> .taiga.toml
taiga api GET users/me
taiga api GET userstories --query project=37 --paginate
taiga api PATCH userstories/123 --field comment="Deployed to staging" --auto-version
```

## Stories

`taiga story` works on the user stories of the selected project (`--project`, `TAIGA_PROJECT`, `.taiga.toml` or the config). A story is named by its reference (`REF`, the number shown in the web UI), never by its internal id; `story get --id` takes the id and still checks the project.

| Command | What it does |
|---|---|
| `taiga story list [--ref N] [--status S] [--assignee USER\|me] [--epic REF] [--swimlane L\|--no-swimlane] [--tag T]... [--search TEXT] [--closed[=false]]` | Every matching story, without manual paging. Repeated `--tag` must all match |
| `taiga story get REF` / `taiga story get --id ID` | One story, with its web `url`; JSON output also carries `custom_attributes` (values with their own `version`) |
| `taiga story create --subject S [--description-file F\|-] [--status S] [--tag T]... [--swimlane L] [--assignee USER]... [--epic REF]` | Create a story; `--epic` links it to an epic after creating it |
| `taiga story update REF [--subject S] [--description-file F\|-] [--append-description TEXT] [--status S] [--tag T]... [--add-tag T]... [--remove-tag T]... [--milestone M] [--swimlane L\|--clear-swimlane] [--add-assignee USER]... [--remove-assignee USER]... [--owner-assignee USER\|--clear-owner-assignee] [--block NOTE\|--unblock] [--epic REF\|--replace-epic REF --confirm-delete]` | Send only the fields that change, with the story `version`; then add the epic link, or make it the only one |
| `taiga story close REF [--status S]` | Move to a closed status; never archives or deletes |
| `taiga story field list REF` | The story's custom field values next to their definitions |
| `taiga story field set REF ["Name=value"]... [--unset NAME]...` | Merge custom field values: named fields change, the others stay |
| `taiga story comment REF --body TEXT\|--body-file F\|-` | Publish a comment (Markdown); sent once, never repeated |
| `taiga story comments REF [--include-system]` | The story's comments, newest first |
| `taiga swimlane list` | The project's swimlanes in board order, with `is_default` |

- Statuses, milestones and swimlanes take a name or an id of the project; users take an exact username, an id or `me`, and must be project members. A name used twice is `ambiguous_name`.
- `--tag` replaces the tags; `--add-tag`/`--remove-tag` merge with the current ones. Taiga stores tags in lower case, so the CLI sends them that way.
- `--add-assignee`/`--remove-assignee` merge with the current assignees (`assigned_users`) and never change the main assignee (`assigned_to`); that takes `--owner-assignee` or `--clear-owner-assignee`. Taiga always shows the main assignee among the assignees, so removing them needs one of those two flags in the same command. Changing the main assignee keeps the others.
- `--remove-assignee` also takes users who left the project. A write that changes assignees is not retried after a version conflict (exit 4; run it again, or use `--force-version`).
- Taiga's concurrency check does not cover the main assignee (`assigned_to`). The CLI re-reads the assignees right before writing (a change since the first read is `version_conflict`) and checks the story after writing; a mismatch is `assignees_postcondition_failed` (exit 4): the write **was applied**, so check the story instead of re-running. A concurrent change in the short window between that re-read and the write is detected, not prevented. See [docs/api-notes.md](docs/api-notes.md).
- `--block NOTE` sets `is_blocked` with the note (required); `--unblock` clears both.
- Swimlanes: `--swimlane` moves the story to a swimlane, `--clear-swimlane` takes it out of any; `story list --swimlane L` and `--no-swimlane` filter by it (always checked locally: Taiga ignores the documented `swimlane` parameter). A story created without `--swimlane` stays without one, even when the project has a default swimlane. Creating, renaming and reordering swimlanes stay in the web UI: the first swimlane of a project takes every story, and the order has no `version`.
- `close` without `--status` uses the project's only closed status; with several (the default template has `Done` and `Archived`) it asks for one.
- Every write accepts `--dry-run` (prints method, path and body) and, except `create`, `--force-version`.
- A write whose answer is inconclusive after the request left (network error, 5xx, or a redirect, which the CLI never follows: a proxy may have passed the request on) never exits 7. `update`, `close` and `field set` re-read the story: the change is there, success; otherwise `story_update_unconfirmed` (exit 1). `create` always exits with `story_create_unconfirmed` (exit 1), naming the new stories of this account with the same subject, if any (another process of the same account may have created them); it never links `--epic`. Do **not** re-run blindly: check with `taiga story get REF` or `taiga story list` first. Exit 7 is left for reads and for a connection that never opened.
- `comment` sends the text exactly as given (quotes, accents, line breaks) and never touches the description. A blank comment is refused. Taiga accepts any past `version` for a comment, so the version protects nothing there and there is no `--force-version`: the comment is sent once. If the answer is lost (network error, 5xx or a redirect, which the CLI never follows), the command always exits with `comment_unconfirmed` (exit 1), naming the new comments of this account with the same text found in the history, if any: none of them proves it came from this command (another process of the same account may have published the same text), and a request still running on the server can land later. Check `story comments` before publishing again. Exit 7 (`network_error`) means the connection never opened, so nothing was sent.
- `comments` lists every comment, including edited and deleted ones (`edit_comment_date`, `delete_comment_date`). Comments written by Taiga's own GitLab integration (its inactive `gitlab-<hash>` user, with the push hook templates "This user story has been mentioned by …" and "… changed the status from [GitLab commit]…") are hidden unless `--include-system`; every other author, service accounts included, is always shown. Each entry carries `is_system`, `story_ref` and the story `url`.
- `--epic` and `--replace-epic`: see "Linking stories to epics" below.

```sh
taiga story list --assignee me --closed=false
taiga story update 246 --status "In progress" --add-tag cli --dry-run
taiga story update 246 --description-file notes.md
taiga story update 247 --add-assignee me --remove-assignee jdoe --block "waiting for the B6 review"
taiga story comment 246 --body-file note.md
taiga story comments 246 --output text
```

## Tasks

`taiga task` works on the tasks of the selected project. Like a story, a task is named by its reference (`REF`; stories and tasks share the same sequence of references in a project), and `task get --id` takes the internal id and still checks the project. A task belongs to a story: `task create` requires `--story` (a task without a story only through `taiga api`), and its sprint is the story's.

| Command | What it does |
|---|---|
| `taiga task list [--story REF] [--status S] [--assignee USER\|me] [--tag T]... [--search TEXT] [--closed[=false]]` | Every matching task, without manual paging; every filter is also checked locally |
| `taiga task get REF` / `taiga task get --id ID` | One task, with its web `url`; JSON output also carries `custom_attributes` |
| `taiga task create --story REF --subject S [--description-file F\|-] [--status S] [--tag T]... [--assignee USER] [--due-date YYYY-MM-DD]` | Create a task in the story |
| `taiga task update REF [--subject S] [--description-file F\|-] [--append-description TEXT] [--status S] [--tag T]... [--add-tag T]... [--remove-tag T]... [--assignee USER\|--clear-assignee] [--block NOTE\|--unblock] [--due-date YYYY-MM-DD\|--clear-due-date]` | Send only the fields that change, with the task `version` |
| `taiga task close REF [--status S]` | Move to a closed status; nothing else changes |
| `taiga task field list REF` / `taiga task field set REF ["Name=value"]... [--unset NAME]...` | Custom field values, like `story field` |
| `taiga task comment REF --body TEXT\|--body-file F\|-` / `taiga task comments REF [--include-system]` | Comments, like `story comment`/`comments` (entries carry `task_ref`) |

- Statuses come from the task statuses of the project (`taiga status list --kind task`). A task has a single assignee (`assigned_to`): `--assignee` sets it (a project member: username, id or `me`), `--clear-assignee` removes it. Taiga's concurrency check covers it on tasks, so a concurrent change is a `version_conflict` (exit 4), unlike the main assignee of a story.
- `--block NOTE` sets `is_blocked` with the note (required); `--unblock` clears both. `--due-date` takes `YYYY-MM-DD`; `--clear-due-date` removes it.
- `close` only changes the status. Without `--status` it uses the project's only closed status (the default template has one task status, `Closed`); with several it asks for one. Unlike the MCP's `archive_or_close`, it never adds an "archived" tag.
- Every write accepts `--dry-run` and, except `create`, `--force-version`.
- A write whose answer is lost after the request left (network error, 5xx or a redirect) never exits 7: `create` always exits with `task_create_unconfirmed` (exit 1), even when the task was saved, since nothing proves that a task found in the story came from this command (another process of the same account may have created the same one); the error names the candidates (new tasks of the story by this account with the same subject, by ref and id) or says none was found, and an empty or unreadable list does not prove the task is missing either; and `update`, `close` and `field set` re-read the task (the change is there, success; otherwise `task_update_unconfirmed`, exit 1). In both cases do **not** re-run blindly: check with `taiga task list --story REF` or `taiga task get REF` first, since a re-run could create a second task or append the description twice.

```sh
taiga task list --story 246 --closed=false
taiga task create --story 246 --subject "Write the tests" --assignee me --due-date 2026-10-31
taiga task update 250 --status "In progress" --block "waiting for the B6 review"
taiga task close 250
taiga task comment 250 --body "Done in staging."
```

## Projects, members, milestones and epics

Read-only commands. Every string they print, nested ones and tags included, has the value of any token parameter hidden (`token=`, `access_token=`, and the same key spelled with percent escapes or HTML entities, such as `%74oken=`, and inside the value of another parameter, as in `link=https://h/a?token=…`): signed media links (project logos, user photos) open files without authentication. A text in which a token parameter only shows up once decoded, where its value cannot be cut out, or whose percent escapes and HTML entities are still changing after 16 decodings, is replaced whole by `[redacted: the text carries a credential]`. Text that merely mentions the word (`tokenizer=python`, `?q=token`) is kept.

| Command | What it does |
|---|---|
| `taiga project list [--search TEXT]` | The projects you are a member of (checked locally too); needs no selected project |
| `taiga project get [SLUG\|ID]` | A project; without an argument, the selected one, with `source` saying where it was selected (flag, env, `.taiga.toml`, config; `argument` when given). Credentials of the project (`*_csv_uuid`, `transfer_token`) are never printed |
| `taiga user list [--search TEXT]` | The project members (never other users), with `is_admin` and `role_name` from the membership |
| `taiga milestone list [--search TEXT] [--closed[=false]]` | The project's milestones (sprints); JSON output carries each sprint's stories |
| `taiga epic list [--search TEXT] [--closed[=false]]` | The project's epics, with their web `url` |
| `taiga epic get REF` | One epic, with its description and `user_stories`: the linked stories in link order (`id`, `ref`, `subject`, `project`, plus `project_slug` for a story of another project; a story you cannot read keeps only its `id`) |

- `--search` looks for the text in names (project name or slug; username or full name; milestone name; epic subject), ignoring case but not accents: `agil` does not find `Ágil`. It is checked locally; Taiga's own `q` is a word search that would hide partial matches.
- `--closed` is sent to Taiga, which honours it, and checked again locally.
- Taiga keeps serving the epics of a project whose epics module is off; `epic list` and `epic get` refuse it with `not_found` ("the epics module is disabled in this project"). `taiga api` still reads them.

```sh
taiga project list --output text
taiga project get --output text
taiga user list --search silva
taiga milestone list --closed=false
taiga epic get 12 --output text
```

### Linking stories to epics

| Command | What it does |
|---|---|
| `taiga epic link EPIC_REF STORY_REF [--dry-run]` | Add the epic to the story's epics; the story keeps the others (Taiga allows several) |
| `taiga epic link EPIC_REF STORY_REF --replace --confirm-delete [--dry-run]` | Make the epic the story's only one: link it, then remove every other link |
| `taiga story create ... --epic REF` / `taiga story update REF ... --epic REF` | Same as `epic link`, after the story write |
| `taiga story update REF ... --replace-epic REF --confirm-delete` | Same as `epic link --replace`, after the story write |

- Replacing creates the new link **before** removing the old ones, so the story never ends without an epic. It is the only curated command that sends `DELETE`, and it requires `--confirm-delete`, also with `--dry-run` (which lists the `POST` and every `DELETE`). Without it the command exits 2 (`delete_not_confirmed`) and sends nothing.
- A link has no `version` in Taiga. The CLI re-reads the story's epics right before the `POST` (a change since the first read is `version_conflict`, nothing sent) and checks them after the writes (`epic_links_postcondition_failed`, exit 4, when they do not match). An epic linked by someone else between the `POST` and the `DELETE`s stops the replacement before any `DELETE`.
- Running the same command again is safe: Taiga refuses a duplicate link and answers a repeated `DELETE` with 404, and the CLI treats both as done after re-reading the story. A link already there is a no-op (`changed: false`). When an answer is lost, the command exits 1 with `epic_link_unconfirmed` or `epic_replace_incomplete` (with what was linked, removed and remaining): run the same command again to finish.
- `story create --epic`: the story exists once created. If the link then fails, the command exits 1 with `story_created_link_failed`, naming the new story; link it with `taiga epic link EPIC REF` instead of creating again. With other fields, `story update` writes them first; a failed link then exits 1 with `story_updated_link_failed` (the link's own code in the cause), saying they are saved and pointing to `epic link`: do not re-run the update.
- The epic is resolved by ref in the selected project only, before any write. Taiga itself would link an epic of another project.
- Linking needs the `modify_epic` permission (`forbidden`, exit 6, without it); `auth status --diagnose` lists it when missing.

```sh
taiga epic link 12 246
taiga epic link 15 246 --replace --confirm-delete --dry-run
taiga story create --subject "Export CSV" --epic 12
taiga story update 246 --replace-epic 15 --confirm-delete
```

## Attachments

| Command | What it does |
|---|---|
| `taiga attachment list REF [--task]` | The attachments of a story (or of a task): `id`, `name`, `size`, `sha1`, `description`, `created_date`, `owner`, `is_deprecated` |
| `taiga attachment upload REF FILE [--task] [--description TEXT] [--dry-run] [--timeout 10m]` | Attach a local file |
| `taiga attachment download REF ATTACHMENT_ID [--task] [--to PATH\|-] [--overwrite] [--timeout 10m]` | Save an attachment, checked against its size and sha1 |

- `upload` is idempotent by content: if the story or task already has an attachment with the same name and sha1, nothing is sent and that attachment comes back with `"created": false`. The same content under another name is a new attachment.
- The upload is sent once and never repeated. Its answer is checked (sha1, size, object); a mismatch is `attachment_postcondition_failed` (exit 4): the file **was stored**. If the answer is lost after the request left (network error, 5xx or a redirect), the command always exits with `attachment_unconfirmed` (exit 1), naming any new attachment with that name and sha1 (another process of the same account may have attached it). A lost body after a 2xx is applied: the new attachment is the answer. Check `attachment list` before uploading again.
- File names Taiga would store differently (control characters, a backslash, an HTML entity such as `&amp;`, with or without `;`, or any `&#`) are refused: rename the file. The idempotence compares the stored name.
- The CLI has no size limit. The proxy in front of Taiga has one (50 MB at Basis): above it the upload fails with `payload_too_large` (exit 2). Empty files are refused, as Taiga refuses them.
- `--dry-run` prints the form fields and the file's name, size and sha1, never its content; every value of the plan (the description, the file name) is printed with URL queries and `token=` values hidden. The real upload sends them as given.
- Taiga's attachment `url` carries a signed token that opens the file without authentication for a few minutes, so it is never printed. `download` reads a fresh one and fetches it only from the same origin as the Taiga URL, without the `Authorization` header and without following redirects.
- `download` saves to `--to` (a file or an existing directory), to stdout with `--to -`, or to the working directory. The server's file name is treated as untrusted: only its last element is used, with control and bidi characters and a leading dot replaced by `_` (an attachment never becomes a hidden file such as `.bashrc`). An existing file is replaced only with `--overwrite`. The bytes go to a temporary file (0600) in the same directory, which gets the user's umask when saved, and become the destination only after their size and sha1 match (`attachment_download_mismatch`, exit 7, otherwise). With `--to -`, a mismatch is reported at the end and what was written must be discarded.
- `--timeout` (default `10m`) bounds the whole transfer. An interrupt (Ctrl-C, SIGTERM) cancels it cleanly: a download removes its temporary file, an upload still checks the list.
- A repeated upload with another `--description` returns the existing attachment unchanged.
- Editing and deleting attachments stay in the web UI.

```sh
taiga attachment upload 246 report.pdf --description "staging run"
taiga attachment list 246 --output text
taiga attachment download 246 3121 --to ~/Downloads/
taiga attachment download 18 3122 --task --to - | less
```

## Custom fields

| Command | What it does |
|---|---|
| `taiga field list --kind story\|task` | The project's custom field definitions |
| `taiga field create --kind story\|task --name NAME --type text\|date\|checkbox [--description TEXT] [--dry-run]` | Create a definition unless one with that name exists |
| `taiga story field list REF` / `taiga story field set REF ["Name=value"]... [--unset NAME]... [--dry-run] [--force-version]` | Read and merge a story's values |

- `field create` is idempotent: an existing definition with the same name (case-sensitive) and type is returned unchanged; one with another type, or another description when `--description` is given, is `field_definition_conflict` (exit 2). Definitions are never changed or deleted. An inconclusive answer (network error after the request left, 5xx, redirect) is `field_create_unconfirmed` (exit 1), naming a definition with that name if one is there; Taiga refuses a second definition with the same name, so it is never created twice.
- Only `text`, `date` and `checkbox` are supported so far. Values: text as is (`null` is text), checkbox `true`/`false`, date `YYYY-MM-DD`. The name ends at the first `=`.
- `--unset NAME` (repeatable, name or id) clears a `checkbox` or `date` field: Taiga stores `null` under its key, which stays even when it is the last field (Taiga refuses an empty dictionary). A field without a value, or already `null`, writes nothing. Text fields cannot be unset (usage error); set them to empty with `Name=`. JSON output shows the cleared value as `null`; text output shows it empty, like a field without value. The same merge, check and `--dry-run` apply.
- `story field set` reads the values, merges the named fields and writes the whole dictionary with the `version` of the values resource, not the story's. Unchanged values write nothing. Values of fields without a definition are kept.
- Taiga never refuses an old `version` on custom field values, so it cannot stop a concurrent write from being overwritten. The CLI checks the answer instead: if another write landed next to ours, it exits with `field_values_postcondition_failed` (exit 4) and the write **was applied**; check with `story field list` instead of re-running. `--force-version` skips the check. See [docs/api-notes.md](docs/api-notes.md).

```sh
taiga field create --kind story --name "Tested in staging" --type checkbox
taiga story field set 246 "Tested in staging=true" "Delivery=2026-10-15" "Notes=a=b is fine"
taiga story field set 246 --unset "Tested in staging" --unset "Delivery"
```

## Project configuration

| Command | What it does |
|---|---|
| `taiga status list [--kind story\|task]` | The project's statuses in board order |
| `taiga project plan -f FILE\|-` | Compare the project with a TOML file; reads only |
| `taiga project apply -f FILE\|- [--dry-run]` | Create the statuses and story fields the file declares and the project lacks |

The file declares story statuses and story custom fields ([docs/examples/taiga-project.toml](docs/examples/taiga-project.toml)); unknown keys are errors:

```toml
[[story_status]]
name = "Waiting for deployment"
color = "#40A8E4"      # #RRGGBB, required
closed = false         # default false
after = "Ready for test"

[[story_field]]
name = "Tested in staging"
type = "checkbox"      # text, date or checkbox
description = ""       # default ""
```

- Only what the file declares is managed and nothing is ever updated or deleted. Statuses and fields missing from the file are listed as `unmanaged` and kept. A declared one that exists with another color, `closed`, type or description — or a name that differs only by case — is `drift`: `plan` shows it and `apply` refuses (`definition_drift`, exit 2) before writing anything.
- Names are case-sensitive. Two statuses or fields with the same name in Taiga are refused (`ambiguous_name`).
- New statuses are created after the last status, in file order. `after` places a status behind another one, existing or declared; a cycle, a self-reference or an unknown status is a usage error. When `after` requires moving statuses, `apply` writes the whole order at the end, in one `bulk_update_order` request.
- **Reordering is checked, not protected.** Taiga 6.7 has no optimistic concurrency for status order (no `version`). Right before the write, `apply` re-reads the order and refuses if anything moved since the plan (`project_changed`, exit 4, nothing sent); right after, it re-reads and requires the intended order, or exits with `status_order_postcondition_failed` (exit 4): the write **was applied** and someone else's change landed next to it, so check with `taiga status list` instead of re-running. This detects part of the races, it does not prevent them: a change between the last read and the write can be overwritten. The write is never repeated.
- `apply` needs `admin_project_values` (a project admin) and checks it first, also with `--dry-run` (`forbidden`, exit 6); `plan` only reads. `--dry-run` prints the requests without sending them.
- `apply` validates everything before the first write, never repeats a write and never undoes one. Its JSON result has `plan`, `applied`, `remaining` and `complete`; `complete` is `true` only when a new read finds nothing left. If it stops halfway, the result still goes to stdout with the error on stderr; run it again and it re-plans from Taiga without duplicating anything. If Taiga changed under it, it exits with `project_changed` (exit 4). Declarations whose names differ only by case are refused before anything is read; the catalogs are re-read before each creation.

```sh
taiga status list --output text
taiga project plan -f taiga-project.toml
taiga project apply -f taiga-project.toml --dry-run
taiga project apply -f taiga-project.toml
```

## Output and exit codes

Without a terminal on stdout, `taiga` prints JSON; on a terminal it prints text. Force either with `--output json` or `--output text`. Errors go to stderr as `{"error":{"code","source","stage","cause","recovery"}}`; `code` is stable and `cause` never contains a secret. In text mode, any value with control or bidi characters (often the server's own text) is printed Go-quoted, on one line.

Every curated command (all but `taiga api`, which prints Taiga's answer as is) hides the value of token parameters in everything it prints, on stdout and on stderr (errors and warnings), `--dry-run` plans included, keeping the keys and the rest of the URL (only the printout changes: a real write sends the values as given and as stored): user photos (`photo`, `big_photo` in `owner_extra_info`, `assigned_to_extra_info`, the comment author), project logos and attachment links are signed media URLs that open the file without authentication while the token lasts. `story get` shows `"photo": "https://…/media/user/…/photo.png?token=…"`.

Shell completion (`taiga completion bash|zsh|fish|powershell`) never calls Taiga: it completes command and flag names and leaves arguments and flag values to the shell (file names). When it cannot (an unknown command, an unknown flag, a bad flag value), the error it writes to stderr and, with `BASH_COMP_DEBUG_FILE` set, to that file has the typed words with token values hidden and control and bidi characters escaped (`\u202e`); a flag redacted whole (`[redacted: the text carries a credential]`) no longer reads as a flag, so no flag error is printed for it. The scripts that `taiga completion` prints are Cobra's, with one block added: Cobra's debug writer (`__taiga_debug`, which appends the typed command line, tokens included, to `BASH_COMP_DEBUG_FILE`) is redefined, before any completion runs, to do nothing. With the variable set, the file only gets the binary's redacted errors; what the scripts complete is Cobra's. A script saved by an earlier version still logs the command line: generate it again.

| Exit | Meaning |
|---|---|
| 0 | OK |
| 1 | unexpected error |
| 2 | invalid usage |
| 3 | authentication |
| 4 | `version` conflict |
| 5 | not found |
| 6 | permission denied |
| 7 | network or server |

Every error code is listed in [docs/errors.md](docs/errors.md).

## Authentication

The token for each call is resolved in this order:

1. `TAIGA_TOKEN`, used as is, with the type from `TAIGA_TOKEN_TYPE` (default `Bearer`);
2. the cached session, while it is valid;
3. a refresh of the cached session, under a file lock so concurrent processes refresh once;
4. a login with the configured secret: `TAIGA_PASSWORD` or `TAIGA_PASSWORD_FILE`, then `secret_command`, the keyring, or the `--insecure-storage` file.

`TAIGA_TOKEN`, `TAIGA_PASSWORD` and `TAIGA_PASSWORD_FILE` are only sent to a URL given by `--url`, `TAIGA_URL` or the user config. A URL that comes only from a repository's `.taiga.toml` is refused with `auth_untrusted_url`, and so is `taiga auth login` without `--url` in that case, so a cloned repository cannot redirect your credentials.

| Variable | Use |
|---|---|
| `TAIGA_URL` | Taiga base URL (`https://host`; `http` only for localhost) |
| `TAIGA_PROJECT` | project slug |
| `TAIGA_TOKEN` | token used as is |
| `TAIGA_TOKEN_TYPE` | `Authorization` scheme for `TAIGA_TOKEN` (default `Bearer`) |
| `TAIGA_USERNAME` | username for the session and for password logins |
| `TAIGA_PASSWORD` | password for a login without the keyring |
| `TAIGA_PASSWORD_FILE` | file holding the password |
| `TAIGA_CONFIG` | config file (default `~/.config/taiga/config.toml`) |
| `TAIGA_STATE_DIR` | state dir holding the session cache (default `~/.local/state/taiga`) |

`taiga auth login` stores the password in the Secret Service keyring. Use `--secret-command "pass show taiga"` to read it from a password manager instead (the command is split on spaces and runs without a shell), or `--insecure-storage` to keep it in a `0600` file. `taiga auth refresh` renews the session, `taiga auth logout` deletes the local session and secret, and `taiga auth status --diagnose` tests every source and the environment:

```sh
taiga auth status --diagnose --output text
```

The last check, `project`, reads the selected project (it only reads) and reports its `id`, `slug` and where it was selected; whether you are a member and an admin; the permissions the curated commands need that you lack (`view_us`, `modify_us`, `add_us`, `comment_us`, `view_tasks`, `add_task`, `modify_task`, `modify_epic`, `admin_project_values`); the epics, kanban and backlog modules; and the number of swimlanes. JSON output also carries them under `data`. It is `skipped` without a selected project or when the identity check (`users/me`) failed, and `failed` when the project cannot be read: it does not exist, the account cannot see it, or another error (network, server) stopped the read. Missing permissions do not fail it: they limit some commands (`project apply` needs `admin_project_values`).

## Coding agents and sandboxes

The [agent skill](#agent-skill) carries these rules for agents.

- A person runs `taiga auth login` once per machine; agents only read the session cache and never see the password.
- An agent that gets `session_expired` inside a sandbox cannot renew the session there: run `taiga auth refresh` outside the sandbox and retry. `taiga` never spends the stored refresh token when it cannot save the new one, and with a read-only cache it logs in only with `TAIGA_PASSWORD`/`TAIGA_PASSWORD_FILE` (keeping that token in memory), never with the keyring, `secret_command` or the `--insecure-storage` file, whether or not a session exists. Without a session and without an env password it fails with `session_cache_readonly`: run `taiga auth login` outside the sandbox.
- Codex: enable `network_access = true` and add the state dir to `writable_roots`:

```toml
[sandbox_workspace_write]
network_access = true
writable_roots = ["/home/you/.local/state/taiga"]   # absolute path to the state dir
```

## Headless Linux keyring

On a server without a desktop session, run GNOME Keyring for the Secret Service only. The unlock must be what starts the daemon: a daemon that is already running (started by a unit or by D-Bus activation) stays without an unlocked `login` collection.

```sh
# Debian/Ubuntu
sudo apt-get install -y gnome-keyring libsecret-tools dbus-user-session
loginctl enable-linger "$USER"   # keep the user manager and its D-Bus running without an open session

# once per boot, before anything uses the keyring: unlock
# (the first unlock creates the "login" collection with this password)
read -rs KP && printf '%s' "$KP" | gnome-keyring-daemon --unlock --components=secrets >/dev/null; unset KP

# check
busctl --user list | grep org.freedesktop.secrets
secret-tool store --label=probe test probe <<<"ok" && secret-tool lookup test probe && secret-tool clear test probe
taiga auth status --diagnose
```

If something reached the keyring before the unlock, `secret-tool` fails with `Object does not exist at path /org/freedesktop/secrets/collection/login` and `taiga` reports `keyring_no_default`, `keyring_locked` or `secret_missing`: stop that daemon with `pkill -x gnome-keyring-d` and unlock again. Run these commands in bash, in the user's own login session (for example over SSH), so that `DBUS_SESSION_BUS_ADDRESS` points to the user bus.

The same keyring serves other tools that use libsecret, such as `sonar auth login`. KeePassXC and KWallet do not work without a graphical session. If unlocking at every boot is not acceptable, use `--secret-command` with a password manager.

## Development

```sh
go test ./...
docker compose -f compose.test.yml up -d && scripts/taiga-seed && go test -tags integration -p 1 ./...
```

Integration tests only run against the local Taiga from `compose.test.yml`. It is served on `127.0.0.1:8000` by an nginx gateway, as in a production deployment: `/api/` goes to `taiga-back`, attachment URLs (`/media/`) are checked by `taiga-protected`, and uploads above 50 MB are refused with 413, the limit of the Basis proxy. Changing the compose file needs `docker compose -f compose.test.yml down -v` before `up -d`. Design notes and plans are in [docs/superpowers/specs/](docs/superpowers/specs/) and [docs/superpowers/plans/](docs/superpowers/plans/); Taiga API behaviour observed so far is in [docs/api-notes.md](docs/api-notes.md).
