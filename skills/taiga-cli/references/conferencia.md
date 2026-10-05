# Gravado ou talvez gravado: conferir antes de repetir

Os códigos abaixo querem dizer que a escrita **foi aplicada**, ou **pode** ter sido. A CLI
nunca repete uma escrita sozinha; o agente também não. Para cada um: leia `cause` (que
nomeia candidatas, ids e o estado encontrado), rode a conferência e repita só o que
faltar.

Nenhuma escrita curada sai com exit 7 depois de enviada: rede depois de aberta a conexão,
timeout, 5xx e redirect 3xx (a CLI nunca segue `Location`) viram releitura ou
`*_unconfirmed`. Em criação, o item na releitura não prova que veio deste comando: nunca
autoriza criar outro, e a ausência também não (o servidor pode gravar depois).

| `code` | exit | o que foi gravado | conferência e próximo passo |
|---|---|---|---|
| `story_create_unconfirmed` | 1 | a story pode existir; o `--epic` não foi tentado | ver as candidatas da `cause` com `taiga story get REF` e `taiga story list --search "SUBJECT"`; existindo, vincular o épico com `taiga epic link EPIC REF` em vez de criar outra |
| `task_create_unconfirmed` | 1 | a task pode existir | `taiga task list --story REF` e `taiga task get REF` |
| `comment_unconfirmed` | 1 | o comentário pode ter sido publicado | esperar e conferir com `taiga story comments REF` (ou `taiga task comments REF`) |
| `attachment_unconfirmed` | 1 | o anexo pode ter sido gravado | esperar e conferir com `taiga attachment list REF [--task]` |
| `story_update_unconfirmed`, `task_update_unconfirmed` | 1 | a alteração pode ter sido aplicada | `taiga story get REF` / `taiga task get REF` (no `field set`, `taiga story field list REF` / `taiga task field list REF`); repetir só o que falta: repetir um `--append-description` duplica o texto. No `field set`, valores iguais com este código querem dizer que outra escrita caiu junto e pode ter sido sobrescrita: avise a pessoa |
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

## Exceções que convergem ao repetir

O vínculo de épico é idempotente: o Taiga recusa vínculo duplicado e a CLI confere pela
releitura.

| `code` | exit | próximo passo |
|---|---|---|
| `epic_link_unconfirmed` | 1 | rodar **o mesmo comando** de novo |
| `epic_replace_incomplete` | 1 | rodar **o mesmo comando** de novo para terminar a troca; se o `recovery` disser que o Taiga recusou o `DELETE` (por exemplo sem `modify_epic`), repetir não adianta: conferir com `taiga auth status --diagnose` e avisar a pessoa |

Tabela completa, com `cause` e `recovery` de cada código:
[docs/errors.md](https://github.com/BasisTI/taiga-cli/blob/main/docs/errors.md).
