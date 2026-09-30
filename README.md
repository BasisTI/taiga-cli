# taiga-cli

## What it is

`taiga` is a command line client for the [Taiga](https://taiga.io) REST API v1, built for people and for coding agents. It resolves the Taiga URL and project from flags, environment, a `.taiga.toml` in the repository or the user config, keeps a session cache so agents never handle passwords, and reports every failure as a stable JSON error envelope with an exit code.

`taiga api` reaches the whole API v1, including `version`-checked writes and transparent pagination, so anything without a curated command is still one call away. Licensed under Apache-2.0.

## Install

```sh
go install github.com/BasisTI/taiga-cli/cmd/taiga@latest
```

Or download the tarball for your platform from [GitHub Releases](https://github.com/BasisTI/taiga-cli/releases), together with `SHA256SUMS`, and check it:

```sh
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf taiga_*_linux_amd64.tar.gz taiga && sudo install taiga /usr/local/bin/
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
| `taiga story list [--ref N] [--status S] [--assignee USER\|me] [--epic REF] [--tag T]... [--search TEXT] [--closed[=false]]` | Every matching story, without manual paging. Repeated `--tag` must all match |
| `taiga story get REF` / `taiga story get --id ID` | One story, with its web `url` |
| `taiga story create --subject S [--description-file F\|-] [--status S] [--tag T]... [--swimlane L]` | Create a story |
| `taiga story update REF [--subject S] [--description-file F\|-] [--append-description TEXT] [--status S] [--tag T]... [--add-tag T]... [--remove-tag T]... [--milestone M] [--swimlane L]` | Send only the fields that change, with the story `version` |
| `taiga story close REF [--status S]` | Move to a closed status; never archives or deletes |

- Statuses, milestones and swimlanes take a name or an id of the project; users take an exact username, an id or `me`, and must be project members. A name used twice is `ambiguous_name`.
- `--tag` replaces the tags; `--add-tag`/`--remove-tag` merge with the current ones. Taiga stores tags in lower case, so the CLI sends them that way.
- `close` without `--status` uses the project's only closed status; with several (the default template has `Done` and `Archived`) it asks for one.
- Every write accepts `--dry-run` (prints method, path and body) and, except `create`, `--force-version`.
- `--epic` on `create`/`update` answers `unsupported_operation`: Taiga links epics through a separate, unversioned resource whose replacement needs `DELETE` (see [docs/api-notes.md](docs/api-notes.md)).

```sh
taiga story list --assignee me --closed=false
taiga story update 246 --status "In progress" --add-tag cli --dry-run
taiga story update 246 --description-file notes.md
```

## Output and exit codes

Without a terminal on stdout, `taiga` prints JSON; on a terminal it prints text. Force either with `--output json` or `--output text`. Errors go to stderr as `{"error":{"code","source","stage","cause","recovery"}}`; `code` is stable and `cause` never contains a secret.

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

## Coding agents and sandboxes

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

Integration tests only run against the local Taiga from `compose.test.yml`. Design notes and plans are in [docs/superpowers/specs/](docs/superpowers/specs/) and [docs/superpowers/plans/](docs/superpowers/plans/); Taiga API behaviour observed so far is in [docs/api-notes.md](docs/api-notes.md).
