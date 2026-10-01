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

Um exit code agrupa vários `code`. Automação deve decidir pelo `code`, nunca só pelo exit: o exit 4 vale tanto para `version_conflict` (nada foi gravado, ou a gravação foi recusada; reler e repetir é seguro) quanto para `assignees_postcondition_failed` e `field_values_postcondition_failed` (a gravação **foi aplicada**; repetir às cegas não é seguro) e para `project_changed` (parte do plano pode ter sido aplicada; o resultado no stdout diz o quê).

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
| `field_definition_conflict` | 2 | — | `field create` com um nome que já existe no projeto com outro tipo, ou com outra descrição quando `--description` foi informado; nada é enviado | rever a definição existente (a CLI não altera definições) |
| `ambiguous_name` | 2 | — | nome de status, milestone, swimlane, usuário ou campo customizado repetido no projeto; `story close` sem `--status` num projeto com mais de um status fechado; em `project plan`/`apply`, dois status ou dois campos com o mesmo nome no Taiga | usar o id, ou escolher com `--status`; em `project`, renomear a duplicata no Taiga |
| `definition_drift` | 2 | — | `project apply`: um status ou campo declarado já existe com outra cor, outro `closed`, outro tipo ou outra descrição, ou com o nome diferente só em maiúsculas; nada é enviado. O `plan` lista cada caso em `drift` | corrigir o arquivo ou o projeto (a CLI nunca altera definições existentes) |
| `unsupported_operation` | 2 | — | a flag depende de um contrato do Taiga que a CLI não cumpre com segurança; hoje, `--epic` em `story create`/`update` (vínculo sem `version`, troca exige `DELETE`) e, em `project apply`, um `after` que exige reordenar status (a ordem dos status não tem `version` no Taiga 6.7); nada é enviado | vincular pela interface web ou por `taiga api POST epics/<id>/related_userstories`; em `project apply`, tirar o `after` ou apontá-lo para o último status e ordenar pela interface |
| `version_conflict` | 4 | `api` | `PATCH`/`PUT` com `version` desatualizado ou ausente (400 com chave `version`, 409 ou 412); com `--auto-version`, conflito em campo alterado por outra pessoa; em `story update` com responsáveis, `assigned_to`/`assigned_users` mudaram entre a leitura e a releitura antes do `PATCH` (nada é enviado); em `story field set`, `version` recusado pelo servidor (maior que o atual) | reler e repetir; `--auto-version` ou `--force-version` |
| `assignees_postcondition_failed` | 4 | `api` | `story update` que mexe em responsáveis: o `PATCH` foi **aplicado**, mas a releitura não bate com o pedido (quem devia entrar não está, quem devia sair continua, o responsável principal não é o pedido, alguém sumiu sem ser removido) ou a `version` pulou (outra escrita caiu junto do `PATCH`): vale a `version` da resposta do `PATCH` ou, se ela não decodificar, a da releitura depois da escrita, que precisa ser exatamente a seguinte à da releitura anterior; a checagem nunca é pulada. O Taiga não detecta troca concorrente de `assigned_to` (ver `docs/api-notes.md`); `cause` traz o estado encontrado. Nunca há repetição automática | **não** repetir às cegas: conferir com `taiga story get` e corrigir o que for preciso |
| `field_values_postcondition_failed` | 4 | `api` | `story field set` (com ou sem `--unset`): o `PATCH` foi **aplicado**, mas a resposta não é a `version` seguinte à da leitura que calculou o merge, ou não traz exatamente o dicionário enviado (se a resposta não decodificar, vale a releitura). Significa que outra escrita caiu junto: o Taiga aceita `version` antigo nesse recurso e não acusa conflito (ver `docs/api-notes.md`). `cause` traz os valores encontrados. Nunca há repetição automática; `--force-version` pula a checagem | **não** repetir às cegas: conferir com `taiga story field list` e gravar o que faltar |
| `project_changed` | 4 | — | `project apply`: o projeto mudou durante a execução — um status ou campo com o mesmo nome apareceu com outros valores, ou um com nome igual a menos de maiúsculas apareceu, entre o plano e a criação (o catálogo é relido antes de cada criação), ou a releitura final ainda encontra ações ou drift (inclusive dois nomes iguais a menos de maiúsculas). O que já foi aplicado está em `applied`; nada é desfeito nem repetido | rodar `taiga project plan` de novo e revisar antes de aplicar |
| `write_applied` | 1 | a da releitura (`api` ou `network`) | a escrita (`POST`/`PATCH`/`PUT`) foi **confirmada** pelo status HTTP, mas nem a resposta dela decodificou (ou chegou inteira: conexão caída no meio do corpo depois do 2xx) nem a releitura funcionou; `cause` traz o status da escrita e a falha da releitura. Em `taiga api` e na criação (`story create`, `field create`), que não têm como reler sem a resposta, o corpo truncado depois do 2xx já dá `write_applied`. O cliente nunca repete essa escrita. Sai com 1, e não com 7, para que scripts que repetem erros de rede não repitam a escrita | **não** repetir o comando: a alteração já está gravada; conferir com `taiga story get` ou `taiga story list` |
| `comment_unconfirmed` | 1 | a do `PATCH` (`api` ou `network`) | `story comment`: o `PATCH` não teve resposta conclusiva (rede depois de aberta a conexão, timeout ou 5xx) e o histórico não mostra o comentário novo, ou não pôde ser lido. O comentário **pode** ter sido publicado: um gateway pode responder 5xx enquanto o `PATCH` ainda roda no servidor e grava depois da conferência. Nunca há repetição automática. Sai com 1, e não com 7, para que scripts que repetem erros de rede não publiquem duas vezes. Quando o histórico mostra o comentário novo, o comando termina com sucesso; `network_error` (exit 7) só sai quando a conexão nem abriu (DNS, conexão recusada), ou seja, nada chegou ao Taiga | esperar, conferir com `taiga story comments REF` e publicar de novo só se ainda faltar |
| `not_found` | 5 | `api` ou — | a API respondeu 404; ou nome, ref de épico ou usuário inexistente no projeto (usuário fora de `memberships` também) | conferir o caminho, a ref ou o nome |
| `forbidden` | 6 | `api` | a API respondeu 403; ou `project apply` (também com `--dry-run`) sem `admin_project_values` no projeto, conferido antes de qualquer escrita | a conta não tem permissão; em `project apply`, usar a conta de um admin do projeto |
| `network_error` | 7 | `network` | falha de rede, DNS, TLS ou timeout de 30 s, sem status 2xx de escrita (esse caso é `write_applied`, ou segue para releitura e pós-condição) | conferir a conectividade (agentes em sandbox precisam de rede) |
| `server_error` | 7 | `api` | 5xx, ou resposta inesperada do login/refresh | tentar de novo mais tarde |
| `unexpected_redirect` | 7 | `api` | a API respondeu 3xx; a CLI nunca segue redirects | usar a URL canônica `https://` |
| `unexpected` | 1 | — | erro não previsto | abrir issue com o comando e o stderr |
