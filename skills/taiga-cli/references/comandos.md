# Comandos da `taiga`

| tarefa | comando |
|---|---|
| ler stories | `taiga story list [--status S] [--assignee me] [--epic REF] [--tag T] [--search TEXT] [--closed=false]`, `taiga story get REF` |
| criar story | `taiga story create --subject S [--description-file F] [--status S] [--tag T] [--swimlane L] [--assignee U] [--owner-assignee U] [--epic REF]` (`--assignee` só preenche a lista de responsáveis; o responsável principal, que aparece no card, é `--owner-assignee`) |
| mudar status, tags, responsáveis, sprint, bloqueio | `taiga story update REF --status S`, `--add-tag T`, `--add-assignee U`, `--owner-assignee U`, `--milestone M`, `--block NOTA`/`--unblock`, `--append-description TEXTO` |
| fechar | `taiga story close REF [--status S]` (só muda o status; não arquiva nem apaga) |
| swimlane | `taiga swimlane list`; `taiga story update REF --swimlane L` ou `--clear-swimlane` |
| comentário | `taiga story comment REF --body TEXTO` ou `--body-file F`; ler com `taiga story comments REF` |
| campos customizados | `taiga field list --kind story`, `taiga story field list REF`, `taiga story field set REF "Nome=valor"...` (texto vazio: `"Nome="`; `--unset NOME` só limpa `checkbox` e `date`); definição nova: `taiga field create --kind story --name N --type text\|date\|checkbox` |
| task | `taiga task list --story REF`, `taiga task get REF`, `taiga task create --story REF --subject S`, `taiga task update REF ...`, `taiga task close REF`, `taiga task comment REF --body TEXTO`, `taiga task field set REF ...` |
| anexo | `taiga attachment upload REF ARQUIVO [--task] [--description TEXTO]`, `taiga attachment list REF [--task]`, `taiga attachment download REF ID [--task] [--to CAMINHO]` |
| épico | `taiga epic list`, `taiga epic get REF`, `taiga epic link EPIC_REF STORY_REF` (acrescenta); troca: `taiga epic link EPIC_REF STORY_REF --replace --confirm-delete` |
| status e board | `taiga status list [--kind story\|task]`; `taiga project plan -f ARQUIVO`, `taiga project apply -f ARQUIVO` |
| projeto e membros | `taiga project list`, `taiga project get`, `taiga user list`, `taiga milestone list` |

Detalhes de cada flag: `taiga <comando> --help`. Comportamento de cada comando:
[README](https://github.com/BasisTI/taiga-cli/blob/main/README.md).

## Escrita com `version`

`story update`, `task update`, `close` e `field set` mandam a `version` lida.
`version_conflict` (exit 4) quer dizer que nada foi gravado: releia e repita.
`--force-version` muda de sentido conforme o comando (veja o `--help`).

## Board da Basis

O arquivo canônico do fluxo da `basis-ci-gitlab` (status `In revision` e
`Waiting for deployment` e os 6 campos de registro) é o
[`docs/examples/taiga-project.toml`](https://github.com/BasisTI/taiga-cli/blob/main/docs/examples/taiga-project.toml).
`taiga project plan` só lê; o `apply` exige admin do projeto (`admin_project_values`),
cria só os status e campos que faltam e nunca altera nem apaga uma definição existente
(nome, cor, `closed`, tipo, descrição). A ordem dos status existentes pode mudar: quando
o `after` exige, o `apply` grava a ordem inteira numa requisição `bulk_update_order`.
Rode `--dry-run`/`plan` antes de aplicar.

## `taiga api`: saída de emergência

Para o que não tem comando curado: `taiga api GET|POST|PATCH|PUT|DELETE CAMINHO` (caminho
relativo a `/api/v1/`), com `--query`, `--field`/`--raw-field`, `--input`, `--paginate`,
`--auto-version`, `--dry-run`; `DELETE` exige `--confirm-delete`. Imprime a resposta do
Taiga como veio, sem redigir links assinados, e **não confere** escritas (exit 7 num
`POST`/`PATCH`: confira com um `GET` antes de repetir).
