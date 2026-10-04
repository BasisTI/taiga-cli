---
name: taiga-cli
description: >-
  Ler e alterar o Taiga pela CLI `taiga`: story, task, comentário, anexo, status,
  swimlane, épico, campo customizado e configuração do board (`taiga project plan|apply`).
  Use quando alguém disser "atualiza a story", "muda o status", "comenta no card", "anexa
  o relatório", "cria a task", "vincula ao épico", "grava o campo", "prepara o board",
  "instalar a taiga-cli"; quando um comando `taiga` falhar com `*_unconfirmed`,
  `write_applied`, `*_postcondition_failed`, `session_expired`, `auth_rejected` ou
  `session_cache_readonly`; e quando a CLI e o MCP do Taiga estiverem disponíveis: a CLI
  tem precedência. Diz o que fazer depois de uma escrita incerta, sem repeti-la às cegas.
---

# Taiga pela CLI `taiga`

A `taiga` fala com a API v1 do Taiga em nome de uma sessão que **uma pessoa** abriu com
`taiga auth login`. O agente só lê esse cache de sessão: nunca vê nem pede a senha.

Duas regras valem para tudo o que vem abaixo:

1. **Decida pelo `code` do erro, não pelo exit.** Sem terminal, todo erro sai no stderr como
   `{"error":{"code","source","stage","cause","recovery"}}`. O mesmo exit agrupa códigos
   opostos: o exit 4 é tanto `version_conflict` (nada gravado; reler e repetir é seguro)
   quanto `assignees_postcondition_failed` (gravado; repetir não é seguro). Leia `code`,
   `cause` e `recovery`.
2. **Escrita com resultado incerto é tratada como gravada.** Antes de repetir qualquer
   escrita que falhou, confira o estado com o comando de leitura da
   [tabela de conferência](#gravado-ou-talvez-gravado-conferir-antes-de-repetir).

Instalação, Codex, keyring headless e completion: [references/instalacao.md](references/instalacao.md).

## Antes de começar

- **CLI antes do MCP.** Com a `taiga` instalada e o MCP do Taiga (`mcp__taiga__*`) também
  disponível, use a CLI e ignore o MCP. Ter os dois não é aconselhado: os contratos de
  escrita diferem (por exemplo, `taiga_tasks_archive_or_close` acrescenta a tag
  `archived-by-mcp`, `taiga task close` só muda o status) e um agente que alterna entre
  eles perde a garantia de "não repetir às cegas". Avise o usuário se encontrar os dois.
- **Projeto.** A CLI resolve URL e projeto por `--url`/`--project`, `TAIGA_URL`/`TAIGA_PROJECT`,
  o `.taiga.toml` do repositório e a config do usuário. `taiga project get --output text`
  mostra qual projeto está selecionado e de onde veio (`source`).
- **Diagnóstico.** `taiga auth status --diagnose` testa as fontes de credencial, o cache
  de sessão, o sandbox, o D-Bus, a identidade (`users/me`) e o projeto: lista as permissões que faltam (`modify_epic`, `admin_project_values`…) e os
  módulos desligados (épicos). Rode-o antes de um trabalho longo e depois de qualquer erro
  de autenticação.
- **Saída.** Sem terminal no stdout a saída é JSON; force com `--output json|text`.

## Autenticação: parar e pedir à pessoa

O agente nunca roda `taiga auth login`, nunca pede senha e nunca define `TAIGA_PASSWORD`
ou `TAIGA_PASSWORD_FILE`. O cache de sessão fica no state dir padrão,
`~/.local/state/taiga`; não aponte `TAIGA_STATE_DIR` para outro lugar (como `/tmp` ou o
workspace) para contornar o sandbox.

| `code` | o que fazer |
|---|---|
| `auth_rejected` (exit 3) | rodar `taiga auth status --diagnose`; parar e pedir à pessoa `taiga auth login` fora do sandbox |
| `session_expired` (exit 3) | parar e pedir à pessoa `taiga auth refresh` fora do sandbox (ou `taiga auth login`, se o refresh também vencer); depois repetir o comando que falhou, se ele não escreveu nada |
| `session_cache_readonly` (exit 3) | o state dir está somente leitura: parar e pedir à pessoa `taiga auth login` fora do sandbox, ou incluir `~/.local/state/taiga` em `writable_roots` do Codex ([instalação](references/instalacao.md#codex)) |
| `auth_no_source`, `secret_missing`, `keyring_*`, `secret_command_*` | sem sessão utilizável: parar e pedir à pessoa `taiga auth login` (o `recovery` diz o resto) |
| `auth_untrusted_url` | a URL veio só do `.taiga.toml`; pedir à pessoa que confirme a URL na config dela (`taiga auth login --url …`) |

Um erro de autenticação quer dizer que o Taiga recusou o pedido, ou que ele nem saiu:
depois do login, repetir é seguro. Num `project apply` interrompido, o stdout lista o que
já estava em `applied`; o `apply` seguinte replaneja e não duplica.

## Comandos

Story e task são nomeadas pela **ref** (o número da interface web), nunca pelo id interno.
Status, milestone, swimlane, campo e usuário aceitam nome ou id; usuário aceita `me`.

| tarefa | comando |
|---|---|
| ler stories | `taiga story list [--status S] [--assignee me] [--epic REF] [--tag T] [--search TEXT] [--closed=false]`, `taiga story get REF` |
| criar story | `taiga story create --subject S [--description-file F] [--status S] [--tag T] [--swimlane L] [--assignee U] [--epic REF]` |
| mudar status, tags, responsáveis, sprint, bloqueio | `taiga story update REF --status S`, `--add-tag T`, `--add-assignee U`, `--milestone M`, `--block NOTA`/`--unblock`, `--append-description TEXTO` |
| fechar | `taiga story close REF [--status S]` (só muda o status; não arquiva nem apaga) |
| swimlane | `taiga swimlane list`; `taiga story update REF --swimlane L` ou `--clear-swimlane` |
| comentário | `taiga story comment REF --body TEXTO` ou `--body-file F`; ler com `taiga story comments REF` |
| campos customizados | `taiga field list --kind story`, `taiga story field list REF`, `taiga story field set REF "Nome=valor"... [--unset NOME]`; definição nova: `taiga field create --kind story --name N --type text\|date\|checkbox` |
| task | `taiga task list --story REF`, `taiga task get REF`, `taiga task create --story REF --subject S`, `taiga task update REF ...`, `taiga task close REF`, `taiga task comment REF --body TEXTO`, `taiga task field set REF ...` |
| anexo | `taiga attachment upload REF ARQUIVO [--task] [--description TEXTO]`, `taiga attachment list REF [--task]`, `taiga attachment download REF ID [--task] [--to CAMINHO]` |
| épico | `taiga epic list`, `taiga epic get REF`, `taiga epic link EPIC_REF STORY_REF` (acrescenta); troca: `taiga epic link EPIC_REF STORY_REF --replace --confirm-delete` |
| status e board | `taiga status list [--kind story\|task]`; `taiga project plan -f ARQUIVO`, `taiga project apply -f ARQUIVO` |
| projeto e membros | `taiga project list`, `taiga project get`, `taiga user list`, `taiga milestone list` |

Detalhes de cada flag: `taiga <comando> --help`. Comportamento de cada comando:
[README](https://github.com/BasisTI/taiga-cli/blob/main/README.md).

- **`--dry-run` antes de escrita em lote.** Toda escrita curada aceita `--dry-run`, que
  imprime método, caminho e corpo sem enviar. Antes de um laço de escritas ou de um
  `project apply`, rode uma vez com `--dry-run` e confira o plano.
- **Board da Basis.** O arquivo canônico do fluxo da `basis-ci-gitlab` (status `In revision`
  e `Waiting for deployment` e os 6 campos de registro) é o
  [`docs/examples/taiga-project.toml`](https://github.com/BasisTI/taiga-cli/blob/main/docs/examples/taiga-project.toml).
  `taiga project plan` só lê; o `apply` exige admin do projeto (`admin_project_values`) e
  nunca altera nem apaga o que já existe.
- **Escrita com `version`.** `story update`, `task update`, `close` e `field set` mandam a
  `version` lida. `version_conflict` (exit 4) quer dizer que nada foi gravado: releia e
  repita.
- **`taiga api` é a saída de emergência** para o que não tem comando curado:
  `taiga api GET|POST|PATCH|PUT|DELETE CAMINHO` (caminho relativo a `/api/v1/`), com
  `--query`, `--field`/`--raw-field`, `--input`, `--paginate`, `--auto-version`,
  `--dry-run`; `DELETE` exige `--confirm-delete`. Ele imprime a resposta do Taiga como
  veio, sem redigir links assinados, e **não confere** escritas: num `POST`/`PATCH` do
  `taiga api`, exit 7 depois do envio também pode ter gravado. Confira com um `GET` antes
  de repetir.

## Gravado ou talvez gravado: conferir antes de repetir

Os códigos abaixo querem dizer que a escrita **foi aplicada**, ou **pode** ter sido. A CLI
nunca repete uma escrita sozinha; o agente também não. Para cada um: leia `cause` (que
nomeia candidatas, ids e o estado encontrado), rode a conferência e repita só o que
faltar.

Desde a US #274, nenhuma escrita curada sai com exit 7 depois de enviada: rede depois de
aberta a conexão, timeout, 5xx e redirect 3xx (a CLI nunca segue `Location`) viram
releitura ou `*_unconfirmed`. Criação de story, task, comentário e anexo com resposta
inconclusiva sai **sempre** como não confirmada, mesmo quando o item aparece na
releitura: nada prova que ele veio deste comando (opção B, contrato visível). Por isso,
"o item está lá" nunca autoriza a criar outro; e "não está lá" também não, porque o
servidor pode gravar depois da conferência. Espere um pouco e confira de novo.

| `code` | exit | o que foi gravado | conferência e próximo passo |
|---|---|---|---|
| `story_create_unconfirmed` | 1 | a story pode existir; o `--epic` não foi tentado | ver as candidatas da `cause` com `taiga story get REF` e `taiga story list --search "SUBJECT"`; existindo, vincular o épico com `taiga epic link EPIC REF` em vez de criar outra |
| `task_create_unconfirmed` | 1 | a task pode existir | `taiga task list --story REF` e `taiga task get REF` |
| `comment_unconfirmed` | 1 | o comentário pode ter sido publicado | esperar e conferir com `taiga story comments REF` (ou `taiga task comments REF`) |
| `attachment_unconfirmed` | 1 | o anexo pode ter sido gravado | esperar e conferir com `taiga attachment list REF [--task]` |
| `story_update_unconfirmed`, `task_update_unconfirmed` | 1 | a alteração pode ter sido aplicada | `taiga story get REF` / `taiga task get REF` (no `field set`, `taiga story field list REF` / `taiga task field list REF`); repetir só o que falta: repetir um `--append-description` duplica o texto |
| `write_applied` | 1 | **gravado** (status HTTP 2xx), mas sem resposta legível nem releitura | não repetir; conferir com `taiga story get REF`, `taiga story list`, `taiga task get REF` ou `taiga task list --story REF`; no `project apply`, `taiga project plan -f ARQUIVO` |
| `story_created_link_failed` | 1 | a story **foi criada**; o vínculo com o épico falhou | não repetir o `create`; vincular a ref da `cause` com `taiga epic link EPIC REF` (se o `recovery` apontar permissão ou épicos mudados, conferir antes com `taiga auth status --diagnose` ou `taiga story get REF`) |
| `story_updated_link_failed` | 1 | os campos do `update` **foram gravados**; o vínculo falhou | não repetir o `update`; seguir o `recovery`: em geral `taiga epic link EPIC REF` (com `--replace --confirm-delete` se era troca), ou `taiga story get REF` quando outra pessoa mexeu nos épicos |
| `field_create_unconfirmed` | 1 | a definição pode existir | `taiga field list --kind story\|task`; o Taiga recusa nome repetido, então rodar de novo não duplica |
| `status_create_unconfirmed` | 1 | o status pode existir | `taiga project plan -f ARQUIVO`; um novo `apply` replaneja e não duplica |
| `status_order_unconfirmed` | 1 | a ordem pode ter sido gravada | `taiga status list` e `taiga project plan -f ARQUIVO`; decidir antes de aplicar de novo |
| `project_apply_interrupted` | 1 | parte do plano **foi aplicada** (`applied` no stdout) | `taiga project plan -f ARQUIVO`, depois `apply` de novo |
| `assignees_postcondition_failed` | 4 | **gravado**, mas o resultado não bate com o pedido | `taiga story get REF` e corrigir o que for preciso |
| `field_values_postcondition_failed` | 4 | **gravado**; outra escrita caiu junto | `taiga story field list REF` (ou `taiga task field list REF`) e gravar o que faltar |
| `epic_links_postcondition_failed` | 4 | o vínculo novo pode estar gravado (`cause` diz) | `taiga story get REF` e decidir |
| `attachment_postcondition_failed` | 4 | o anexo **foi gravado**, com hash ou tamanho diferentes | `taiga attachment list REF [--task]` e decidir |
| `status_order_postcondition_failed` | 4 | a ordem **foi gravada**; outra mudança caiu junto | `taiga status list` e `taiga project plan -f ARQUIVO` |
| `project_changed` | 4 | parte do plano pode ter sido aplicada (`applied`) | `taiga project plan -f ARQUIVO` e revisar antes de aplicar |

**Exceções que convergem ao repetir.** O vínculo de épico é idempotente: o Taiga recusa
vínculo duplicado e a CLI confere pela releitura.

| `code` | exit | próximo passo |
|---|---|---|
| `epic_link_unconfirmed` | 1 | rodar **o mesmo comando** de novo |
| `epic_replace_incomplete` | 1 | rodar **o mesmo comando** de novo para terminar a troca; se o `recovery` disser que o Taiga recusou o `DELETE` (por exemplo sem `modify_epic`), repetir não adianta: conferir com `taiga auth status --diagnose` e avisar a pessoa |

Tabela completa, com `cause` e `recovery` de cada código:
[docs/errors.md](https://github.com/BasisTI/taiga-cli/blob/main/docs/errors.md).

## Seguro repetir

- `version_conflict` (exit 4): nada foi gravado; releia e repita (ou `--force-version`,
  se a mudança da outra pessoa pode ser sobrescrita).
- Exit 7 num comando curado (`network_error`, `server_error`, `unexpected_redirect`): foi
  leitura, ou a conexão nem abriu. Repetir é seguro. No `taiga api` não é: veja acima.
- Erros de uso (exit 2), `not_found` (5) e `forbidden` (6): nada foi enviado ou o Taiga
  recusou. Corrija a causa antes de repetir.
