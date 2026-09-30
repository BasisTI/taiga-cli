# Códigos de erro da taiga-cli

Todo erro sai no stderr como `{"error":{"code","source","stage","cause","recovery"}}` (ou em texto, num terminal). O `code` é estável e pode ser usado em scripts; o `cause` nunca contém segredo. `source` e `stage` são omitidos quando não se aplicam.

## Exit codes

| Exit | Significado |
|---|---|
| 0 | OK |
| 1 | erro inesperado |
| 2 | uso inválido |
| 3 | autenticação |
| 4 | conflito de `version` |
| 5 | não encontrado |
| 6 | permissão negada |
| 7 | rede ou servidor |

## Códigos

| code | exit | source | quando ocorre | recuperação |
|---|---|---|---|---|
| `usage` | 2 | — | flag, argumento ou combinação inválida; comando desconhecido | `taiga --help` |
| `delete_not_confirmed` | 2 | — | `taiga api DELETE` sem `--confirm-delete`; nada é enviado | repetir com `--confirm-delete` |
| `invalid_request` | 2 | `api` | a API respondeu 4xx que não é 401, 403, 404 nem conflito de `version` | corrigir o corpo ou a query |
| `config_no_home` | 2 | `env` | `HOME` e `XDG_*` ausentes ou relativos | definir `TAIGA_CONFIG` e `TAIGA_STATE_DIR` |
| `config_unreadable` | 2 | `config` ou `file` | a config ou o `.taiga.toml` não pôde ser lido | conferir permissões |
| `config_invalid` | 2 | `config` ou `file` | TOML inválido na config ou no `.taiga.toml` | corrigir a sintaxe |
| `config_no_url` | 2 | `config` | nenhuma URL em flag, env, `.taiga.toml` ou config | `taiga auth login --url …`, `TAIGA_URL` ou `.taiga.toml` |
| `config_invalid_url` | 2 | `config` | URL sem HTTPS (fora de localhost), com userinfo, query ou fragmento | usar `https://host` |
| `auth_no_source` | 3 | `config` ou `env` | sem `TAIGA_TOKEN`, sem sessão e sem fonte de segredo | `taiga auth login` ou `TAIGA_TOKEN` |
| `auth_untrusted_url` | 3 | `env` ou `file` | `TAIGA_TOKEN`/`TAIGA_PASSWORD`/`TAIGA_PASSWORD_FILE` definidos, ou `taiga auth login` sem `--url`, e a URL veio só do `.taiga.toml`, sem host correspondente na config; nada é enviado | `TAIGA_URL`/`--url` com essa URL, ou `taiga auth login --url …` |
| `auth_invalid_credentials` | 3 | `api` | `POST /auth` respondeu 400 ou 401 | conferir usuário e fonte de segredo |
| `auth_rejected` | 3 | `api` | a API respondeu 401 a uma chamada autenticada | `taiga auth status --diagnose` |
| `session_expired` | 3 | `session_cache` | refresh recusado, ou sessão vencida com o cache somente leitura (sandbox) | `taiga auth refresh` fora do sandbox, ou `taiga auth login` |
| `session_cache_readonly` | 3 | `session_cache` | `taiga auth refresh` com o state dir somente leitura; ou nenhuma sessão, cache somente leitura e sem `TAIGA_PASSWORD`/`TAIGA_PASSWORD_FILE` (keyring, `secret_command` e arquivo não são usados no sandbox). Como aviso (stderr, sem falhar): token obtido por login com senha do env mantido só em memória | rodar fora do sandbox; ou incluir o state dir em `writable_roots` |
| `session_cache_write_failed` | — | — | aviso: a sessão nova não pôde ser gravada por outro motivo | conferir o state dir |
| `password_file_unreadable` | 3 | `env` | `TAIGA_PASSWORD_FILE` não pôde ser lido | conferir caminho e permissões |
| `password_file_empty` | 3 | `env` | `TAIGA_PASSWORD_FILE` vazio | preencher o arquivo |
| `secret_command_timeout` | 3 | `secret_command` | o comando não respondeu em 10 s (por exemplo gpg esperando o pinentry) | se o stderr citar gpg/pinentry: rodar o comando uma vez com `export GPG_TTY=$(tty)` num terminal fora do sandbox |
| `secret_command_not_found` | 3 | `secret_command` | executável inexistente ou `secret_command` vazio | corrigir o caminho (sem shell) |
| `secret_command_failed` | 3 | `secret_command` | exit diferente de 0; `cause` = `exit status N;` seguido só dos padrões conhecidos do stderr (gpg, pinentry, `inappropriate ioctl`, `no tty`). O texto do stderr e o argv nunca são copiados, porque podem conter o segredo | rodar o comando à mão; dica de `GPG_TTY` se for gpg |
| `secret_command_empty` | 3 | `secret_command` | o comando não imprimiu nada | conferir o comando |
| `file_secret_missing` | 3 | `file` | arquivo do `--insecure-storage` ausente ou ilegível | `taiga auth login --insecure-storage` de novo |
| `file_secret_insecure_permissions` | 3 | `file` | arquivo do segredo legível por grupo ou outros | `chmod 600` |
| `keyring_unavailable` | 3 | `keyring` | não foi possível falar com o Secret Service | seção "Headless Linux keyring" do README, ou `--secret-command` |
| `keyring_service_unavailable` | 3 | `keyring` | nenhum `org.freedesktop.secrets` no session bus (ou a ativação falhou) | idem |
| `keyring_timeout` | 3 | `keyring` | o Secret Service não respondeu em 5 s | idem |
| `keyring_access_denied` | 3 | `keyring` | o Secret Service recusou o acesso | idem |
| `keyring_locked` | 3 | `keyring` | coleção bloqueada | `gnome-keyring-daemon --unlock` (README) |
| `keyring_prompt_required` | 3 | `keyring` | o cofre pediu interação; a CLI nunca abre prompt | desbloquear antes, ou `--secret-command` |
| `keyring_no_default` | 3 | `keyring` | Secret Service sem coleção padrão (daemon iniciado antes do unlock) | `pkill -x gnome-keyring-d` e desbloquear de novo (README) |
| `keyring_unsupported` | 3 | `keyring` | keyring fora do Linux | `--secret-command` |
| `secret_missing` | 3 | `keyring` | nenhuma credencial guardada para esta URL e usuário | `taiga auth login` |
| `secret_ambiguous` | 3 | `keyring` | mais de uma credencial para a mesma referência | apagar as duplicadas com `secret-tool clear service taiga-cli` |
| `version_conflict` | 4 | `api` | `PATCH`/`PUT` com `version` desatualizado ou ausente; com `--auto-version`, conflito em campo alterado por outra pessoa | reler e repetir; `--auto-version` ou `--force-version` |
| `not_found` | 5 | `api` | a API respondeu 404 | conferir o caminho e o id |
| `forbidden` | 6 | `api` | a API respondeu 403 | a conta não tem permissão |
| `network_error` | 7 | `network` | falha de rede, DNS, TLS ou timeout de 30 s | conferir a conectividade (agentes em sandbox precisam de rede) |
| `server_error` | 7 | `api` | 5xx, ou resposta inesperada do login/refresh | tentar de novo mais tarde |
| `unexpected_redirect` | 7 | `api` | a API respondeu 3xx; a CLI nunca segue redirects | usar a URL canônica `https://` |
| `unexpected` | 1 | — | erro não previsto | abrir issue com o comando e o stderr |
