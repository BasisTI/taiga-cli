# Notas de comportamento da API do Taiga 6.7

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-09-30.

| Ponto | Resultado | Teste |
|---|---|---|
| Vida do `auth_token` | ≈ 24 h (claim `exp` do JWT) | `TestProbeTokenLifetimes` |
| Vida do `refresh` | ≈ 8 dias (192 h, claim `exp` do JWT) | `TestProbeTokenLifetimes` |
| Refresh rotaciona | sim: `POST /auth/refresh` devolve um `refresh` novo | `TestProbeRefreshRotationInvalidatesPrevious` |
| Refresh anterior invalidado | sim: reusar o `refresh` antigo é recusado. Por isso `refreshInvalidatesPrevious = true` e, com o cache somente leitura, a CLI não renova e devolve `session_expired` | `TestProbeRefreshRotationInvalidatesPrevious` |
| `PATCH` com `version` desatualizado | `400 {"version": "The version doesn't match with the current one"}` **só quando o PATCH envia um campo alterado desde aquela versão**; sem sobreposição, é aceito (ver "OCC por campo") | sondagem manual da US #245; `TestIntegrationAutoVersionOnStory`; `TestProbeOCCIsPerField` |
| `PATCH` sem `version` | `400 {"version": "The version parameter is not valid"}`, também tratado como conflito | sondagem manual da US #245 |
| `x-disable-pagination` respeitado | sim (lista sem `x-pagination-next`) | `TestIntegrationAPIProjectsAndUsersMe` |
| Paginação | `?page=N`; o próximo vem em `X-Pagination-Next` | sondagem manual da US #245 |
| Application tokens para conta de serviço | não validado na fase 1: exige cadastrar uma Application pelo admin do Django; fica para quando houver demanda | — |
| `userstories/by_ref?ref=&project=` | validado na fase 2; ver seção abaixo | `TestProbeStoryByRef` |
| Swimlanes: catálogo, primeira swimlane, `swimlane` na story, filtro da listagem, permissão | validado na fase 3; ver "swimlanes" | `TestProbeSwimlaneContract` |
| Anexos: upload multipart, nomes, arquivo vazio, listagem, objeto de outro projeto, duplicata, permissão, caminho do download | validado na fase 3, com o download pelo gateway do `compose.test.yml`; ver "anexos" | `TestProbeAttachmentContract` |
| Comentários (`PATCH {comment, version}`, `history/userstory`, integração GitLab) | validado na fase 2; ver "comentários" | `TestProbeCommentContract`, `TestProbeCommentHistoryPages`, `TestIntegrationStoryComments` |
| Relação `assigned_to` × `assigned_users`, bloqueio | validado na fase 2; ver "responsáveis e bloqueio" | `TestProbeStoryAssignees`, `TestProbeStoryBlock` |
| Campos customizados (definições e valores) de story e task | validado na fase 2; ver "campos customizados" | `TestProbeFieldDefinitions`, `TestProbeFieldValues`, `TestProbeTaskFieldValues`, `TestProbeFieldValuesUnset` |
| Status de story/task, permissão `admin_project_values`, ordem sem OCC (reordenação conferida antes/depois) | validado na fase 2; ver "status e projeto como código" | `TestProbeStatusContract` |

## Fase 2 — stories (US #246)

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-09-30, com a conta `admin`
nos projetos descartáveis `cli-test` e `cli-test-probe-b` (este com épicos ativados pelo teste).
Testes em `internal/taiga/stories_probe_integration_test.go`:
`go test -tags integration -run '^TestProbe(Story|Users)' -v ./internal/taiga`. Os itens marcados com
*(manual)* foram observados com `curl` no mesmo Taiga local e não são reafirmados pelos testes.

### Resolução por ref

| Requisição | Resultado |
|---|---|
| `GET userstories/by_ref?project=<id>&ref=<n>` | 200 com o objeto completo da story (`id`, `ref`, `project`, `version`, `tags` como pares `[nome, cor]`) |
| mesma ref em dois projetos | cada `project` devolve a story do próprio projeto; sem vazamento entre projetos |
| ref inexistente, projeto inexistente ou ref não numérica | 404 `{"_error_message": ...}` — indistinguível de rota inexistente; por isso a CLI valida a ref antes |
| sem `project` | 400 `"project or project__slug param is needed"` |

### Escrita de vínculos

| Campo | Resultado |
|---|---|
| `milestone`, `swimlane`, `status`, `tags` no POST e no PATCH | gravados; `null` limpa milestone/swimlane; PATCH exige `version` |
| milestone, swimlane ou status de outro projeto | 403 `PermissionDenied` ("You don't have permissions to set this ... to this user story") |
| `epics` no PATCH | **ignorado em silêncio**: responde 200, `version` sobe, `epics` continua igual |
| vínculo de épico | `POST epics/<id>/related_userstories {"epic", "user_story"}` → 201, **sem `version`** e sem alterar a `version` da story; uma story aceita vários épicos; trocar ou remover o vínculo exige `DELETE` (`PATCH` no vínculo respondeu 500 *(manual)*). Contrato completo em "Fase 3 — vínculo de épico (US #253, PR 253-2)" |
| POST de story com `version` | aceito e gravado (a story nasce com a versão enviada); sem `version` nasce com 1. A CLI não envia `version` na criação |
| tags | o servidor grava em minúsculas; sem duplicatas (`["Dup","dup"]` → `[["dup",null]]`) *(manual)* |

Consequência na fase 2: `--epic` em `story create`/`story update` ficou bloqueado com `unsupported_operation`
(exit 2) até decisão humana (vínculo sem OCC e substituição por `DELETE`). A decisão veio em 2026-10-01 e o vínculo
entrou no PR 253-2 (seção "Fase 3 — vínculo de épico").
`--milestone` e `--swimlane` são campos da story e usam a `version` normal.

### Filtros da listagem `GET userstories?project=<id>`

| Parâmetro | Resultado |
|---|---|
| `status`, `assigned_users`, `epic`, `status__is_closed` (também `is_closed` *(manual)*), `q` | respeitados |
| `tags=a,b` | respeitado, com semântica OU; repetir o parâmetro vale só o último *(manual)* |
| `ref`, `swimlane` | **ignorados** (lista inteira); também `subject` e parâmetros desconhecidos *(manual)* |
| projeto inexistente | 200 `[]` *(manual)* |

A listagem traz `tags`, `assigned_users`, `epics` (objetos com `id`/`ref`), `is_closed` e `status`, o suficiente
para conferir localmente cada filtro depois do `GetAll`.

### Usuários do projeto

`GET users?project=<id>` lista também quem **não** é membro do projeto (observado com `svc` fora de `cli-test`).
`GET memberships?project=<id>` delimita os membros (`user` = id), mas não traz `username`. A CLI resolve o
username em `users?project=` e exige que o id esteja em `memberships`. O servidor aceita `assigned_users`
com usuário de fora do projeto (PATCH 200), então a checagem é da CLI.

## Fase 2 — responsáveis e bloqueio (US #247)

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-09-30, com a conta `admin` no projeto
descartável `cli-test-probe-assign`, onde `admin` e `svc` são membros (o `cli-test` mantém `svc` de fora para
`TestProbeUsersCatalogScope`). Testes em `internal/taiga/assignees_probe_integration_test.go`:
`go test -tags integration -run '^TestProbeStory(Assignees|Block)$' -v ./internal/taiga`.

### `assigned_to` e `assigned_users`

A resposta **não** mostra a lista gravada: o serializer devolve `assigned_users` = lista gravada ∪ {`assigned_to`}
(`taiga/projects/userstories/serializers.py`, `get_assigned_users`). A lista gravada fica invisível pela API.

| Requisição | Resultado |
|---|---|
| POST com `assigned_to` fora de `assigned_users` | 201; a resposta mostra os dois |
| POST sem responsáveis | `assigned_users: []`, `assigned_to: null` |
| PATCH `assigned_users` sem o `assigned_to` atual | 200, `version` sobe, **o responsável principal continua na resposta** (remoção ignorada em silêncio na leitura) |
| PATCH `assigned_to: null` + `assigned_users` sem ele | os dois saem |
| PATCH só `assigned_to` (troca ou `null`) | o responsável principal anterior **some** de `assigned_users` quando nunca esteve na lista gravada (entrou só por `assigned_to`); fica quando estava gravado |
| PATCH `assigned_to` + `assigned_users` com a lista completa | a lista enviada fica, mais o novo principal |
| usuário de fora do projeto em `assigned_users` | aceito pelo servidor (já registrado na #246); a CLI confere em `memberships` |

Consequências na CLI:

- `--add-assignee`/`--remove-assignee` enviam a lista lida (a da resposta) mais/menos os ids pedidos. Enviar a lista
  lida grava explicitamente quem estava só implícito.
- Remover o responsável principal sem trocar `assigned_to` no mesmo comando é recusado (`usage`, exit 2): o PATCH
  seria aceito e a leitura continuaria mostrando a pessoa. Não há sincronização implícita: a troca é explícita com
  `--owner-assignee` ou `--clear-owner-assignee`.
- Toda mudança de `assigned_to` envia também `assigned_users` com a lista completa, para não perder um responsável
  principal anterior que o Taiga descartaria.
- Um PATCH com `assigned_users` ou `assigned_to` não é repetido depois de conflito de versão: como a lista gravada
  não aparece na resposta, a comparação campo a campo pode não ver a mudança de outra pessoa. Sai `version_conflict`
  (exit 4), a menos que haja `--force-version`. Os demais campos seguem com a repetição única já existente.
- `--remove-assignee` aceita qualquer usuário (username, id ou `me`), mesmo fora do projeto, para limpar ex-membros;
  `--add-assignee`, `--assignee` e `--owner-assignee` exigem membro.

### Bloqueio

| Requisição | Resultado |
|---|---|
| `is_blocked: true` + `blocked_note` | gravados; aspas, quebra de linha e acentos preservados |
| `blocked_note` com a story desbloqueada | **descartado** (continua `""`) |
| `blocked_note` com a story bloqueada | gravado |
| `is_blocked: false` sozinho | limpa também `blocked_note` |
| `is_blocked: true` sem nota | aceito pelo servidor; a CLI exige nota em `--block` |

### OCC por campo

Observado em `TestProbeOCCIsPerField` (mesmo projeto descartável) e lido em `taiga/projects/occ/mixins.py` do
`taiga-back` 6.7.3:

```python
            if current_version != param_version:
                diff_versions = current_version - param_version

                modifying_fields = set(self.request.DATA.keys())
                if "version" in modifying_fields:
                    modifying_fields.remove("version")

                modified_fields = set(get_modified_fields(obj, diff_versions))
                if "version" in modifying_fields:
                    modified_fields.remove("version")

                both_modified = modifying_fields & modified_fields

                if both_modified:
                    raise exc.WrongArguments({"version": _("The version doesn't match with the current one")})
```

`get_modified_fields` junta as chaves do `diff` das últimas `diff_versions` entradas do histórico. O conflito só
existe quando as chaves enviadas cruzam as chaves alteradas desde a versão enviada; com `version` antiga e
campos disjuntos, o PATCH é aceito e a `version` sobe. O histórico grava `assigned_users` a partir da lista
**armazenada** (`userstory_freezer`), não da resposta, e **não grava `assigned_to`**: em
`taiga/projects/history/services.py`, `_deprecated_fields = {"userstories.userstory": frozenset(["assigned_to"])}`
tira o campo do `diff`. Há uma exceção no freezer: com a lista armazenada **vazia**, o snapshot usa
`[assigned_to]` como `assigned_users`
(`if us.assigned_to_id and not assigned_users: assigned_users = [us.assigned_to_id]`). Então:

- lista armazenada vazia: trocar ou limpar `assigned_to` muda o `assigned_users` do snapshot, gera entrada no
  histórico e um PATCH antigo que envie `assigned_users` dá conflito;
- lista armazenada com alguém: uma troca só de `assigned_to` não gera diff nenhum (nem entrada no histórico) e
  nenhum PATCH antigo conflita por causa dela.

Os dois casos estão em `TestProbeOCCIgnoresAssignedTo`.

| Requisição (com `version` antiga, depois de outra pessoa gravar `blocked_note`) | Resultado |
|---|---|
| `{"subject"}` | 200 |
| `{"is_blocked": false}` | 200, e apaga a nota da outra pessoa |
| `{"is_blocked", "blocked_note"}` | 400 conflito de versão |

Regra da CLI: **todo PATCH envia cada campo de que o cálculo dependeu**, porque só esses são protegidos:

- bloqueio: `--block` e `--unblock` enviam sempre o par `is_blocked` + `blocked_note`;
- tags, `--append-description`, status, milestone e swimlane já enviam o próprio campo de que dependem.

Na escrita versionada da #246 (`WriteVersionedFrom`), a repetição única após conflito compara só as chaves do
patch entre a leitura inicial e a releitura, e o servidor também só confere as chaves enviadas. A premissa
"version antiga sempre conflita" só fazia diferença para campos de que o patch depende sem enviá-los; o único
caso era o bloqueio (`--unblock` mandava só `is_blocked`, `--block` podia mandar só `blocked_note`), corrigido
com o envio do par. Um PATCH com responsáveis continua sem repetição (a lista armazenada não aparece na
releitura).

**Limitação do Taiga — `assigned_to`:** com a lista armazenada não vazia, nenhum PATCH é protegido pelo OCC
contra uma troca concorrente do responsável principal, nem mandando `assigned_to` junto (testado: aceito).
A CLI mitiga em `story update` com responsáveis (decisão humana de 2026-09-30: "mitigar e documentar"):

1. **releitura antes do PATCH:** relê a story logo antes de gravar; se `assigned_to` ou `assigned_users` mudaram
   desde a leitura que calculou o patch, sai com `version_conflict` (exit 4) sem gravar. Não recalcula: o resto do
   patch (tags, bloqueio, descrição) foi calculado da mesma leitura, e recalcular mudaria o resultado sem o
   usuário ver;
2. **pós-condição depois do PATCH:** relê e confere contra o pedido: quem devia entrar está, quem devia sair não
   está, o responsável principal é o pedido (ou o de antes), e ninguém da releitura sumiu sem ser removido. Também
   exige que a resposta do PATCH seja a `version` seguinte à da releitura (se a resposta não decodificar, vale a
   `version` da releitura pós-escrita, com a mesma exigência; a checagem nunca é pulada); um salto indica outra
   escrita no meio, inclusive uma troca de `assigned_to` que não deixa rastro. Se algo não bate, sai com
   `assignees_postcondition_failed` (exit 4), dizendo que a escrita **foi aplicada**, com o estado encontrado. Não
   há repetição automática.

`--force-version` pula as duas conferências. **Janela residual:** entre a releitura final e o PATCH, uma troca
concorrente de `assigned_to` ainda é gravada pelo Taiga e pode ser desfeita pelo nosso PATCH; a CLI então detecta
e avisa (exit 4), mas não evita. Um falso alarme também é possível: qualquer escrita de outra pessoa nessa janela,
mesmo em outro campo, faz a `version` saltar e gera o erro com a escrita aplicada.


## Fase 2 — campos customizados (US #248)

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-09-30, com `admin` no projeto
descartável `cli-test-probe-fields`, onde `svc` é membro sem ser admin. Testes em
`internal/taiga/fields_probe_integration_test.go`:
`go test -tags integration -run '^TestProbe(Field|TaskField)' -v ./internal/taiga`. Código lido no container:
`taiga/projects/custom_attributes/` (`models.py`, `validators.py`, `api.py`, `choices.py`).

### Definições

| Requisição | Resultado |
|---|---|
| `GET userstory-custom-attributes?project=<id>` e `task-custom-attributes?project=<id>` | lista com `id`, `name`, `description`, `type`, `order`, `project`, `extra`, `created_date`, `modified_date` |
| `POST` com `project`, `name`, `type` (e `description` opcional) | 201; sem `description` nasce com `""` |
| `version` em definição | **não existe**: nem na resposta, nem no modelo (sem `OCCModelMixin`) |
| mesmo nome no mesmo projeto | 400 `{"name": ["Already exists one with the same name."], "project": [...]}`; `unique_together = ("project", "name")` no banco garante unicidade também numa corrida |
| mesmo nome com outra caixa | aceito (201): o nome é sensível a caixa |
| nome com 65 caracteres | 400 em `name` (`max_length=64`) |
| tipo desconhecido | 400 em `type`. O catálogo do servidor (`choices.py`) tem `text`, `multiline`, `richtext`, `date`, `url`, `dropdown`, `checkbox`, `number`; a CLI aceita só os três validados aqui (`text`, `date`, `checkbox`) |
| membro sem admin (`svc`) | lê a lista; `POST` 403 (exige `admin_project_values`) |
| não membro de projeto privado | `GET ...?project=<id>` responde **200 `[]`**, sem erro *(manual)*. A CLI não chega aí: `projects/by_slug` já recusa o projeto antes |

Consequência na CLI: `field create` é idempotente pela leitura do catálogo antes do `POST`; se outro processo criar a
mesma definição entre a leitura e o `POST`, o 400 em `name` faz a CLI reler e aceitar a definição só se for
compatível (mesmo tipo e, se `--description` foi dado, mesma descrição). O `POST` nunca é repetido. Limite não
sondado: se os dois `POST` passarem juntos pelo validador, quem recusa é o `unique_together` do banco, o que pode
virar 500 (`server_error`, exit 7) em vez do 400; a unicidade continua garantida e repetir o comando encontra a
definição. A CLI também recusa `=` no nome, porque `story field set` corta a atribuição no primeiro `=`. Definições
existentes nunca são alteradas nem apagadas.

### Valores

| Requisição | Resultado |
|---|---|
| `GET userstories/custom-attributes-values/<story id>` e `tasks/custom-attributes-values/<task id>` | `{"attributes_values": {...}, "version": N, "user_story"\|"task": <id>}`; story ou task nova já tem o recurso, com `{}` e `version` 1 |
| chaves de `attributes_values` | id da definição **em string**; ids de story e de task são de tabelas diferentes e se repetem |
| `PATCH` com `attributes_values` + `version` | 200 e `version` + 1; **substitui o dicionário inteiro** (chaves não enviadas somem); a releitura confirma a persistência |
| `version` dos valores × da story/task | independentes: gravar valores não muda a `version` da story nem da task, e vice-versa |
| `version` maior que a atual, ou ausente | 400 `{"version": "The version parameter is not valid"}` |
| **`version` antiga**, com o dicionário alterado desde então | **aceito (200)**, sobrescrevendo o que outra pessoa gravou: o OCC deste recurso nunca acusa conflito (abaixo) |
| `null` em date/checkbox, `""` em date, `"yes"` em checkbox, `"30/09/2026"` em date | aceitos e gravados como enviados: o servidor **não valida o valor pelo tipo** |
| `attributes_values: {}` | 400 `"This field cannot be blank."` — não dá para esvaziar o dicionário |
| id que não é definição do projeto | 400 `"It contains invalid custom fields."`. Chave órfã não surge pelo caminho normal: apagar uma definição dispara `clean_key_in_custom_attributes_values`, que tira a chave de todos os valores (`custom_attributes/migrations/0003_triggers_on_delete_customattribute.py`). A CLI preserva chaves sem definição no merge; se uma existir, o servidor recusa o `PATCH` (`invalid_request`, exit 2) |
| membro sem admin (`svc`) | grava valores (`modify_us`); não membro de projeto privado: 403 |
| `tasks/by_ref?project=<id>&ref=<n>` | 200 com a task do projeto, como em stories |

### Por que o OCC dos valores nunca dispara

O `OCCResourceMixin` (trecho em "OCC por campo", acima) só recusa quando as chaves enviadas cruzam
`get_modified_fields(obj, diff_versions)`, que procura o histórico pela chave do **próprio objeto**
(`make_key_from_model_object(obj)`). O viewset dos valores grava o histórico na story ou task
(`BaseCustomAttributesValuesViewSet.get_object_for_snapshot` devolve `obj.user_story`/`obj.task`), então não há
entrada nenhuma com a chave do objeto de valores. Conferido no banco do container para uma story sondada:

```
values key: custom_attributes.userstorycustomattributesvalues:66 entries: 0
story key: userstories.userstory:66 entries: 7
modified since v1: []
```

O conjunto de campos alterados é sempre vazio; o dicionário é um campo só e não há nada por chave. Qualquer
`version` entre 0 e a atual é aceita.

Consequências na CLI (`story field set`, e o serviço de valores de task):

- merge local: lê os valores, aplica só os campos pedidos e envia o dicionário inteiro com a `version` do recurso de
  valores (nunca a da story), num `PATCH` próprio; `custom_attributes` nunca vai no `PATCH` da story;
- a `version` não protege nada, então a CLI confere a resposta: ela tem de ser a `version` seguinte à da leitura que
  calculou o merge e trazer exatamente o dicionário enviado. Se não bate, outra escrita caiu entre a leitura e o
  `PATCH` (e pode ter sido desfeita pelo nosso): sai `field_values_postcondition_failed` (exit 4), com a escrita
  **aplicada** e o estado encontrado; sem repetição. Se a resposta não decodificar, vale a releitura, com a mesma
  exigência; se nenhuma das duas der, `write_applied`. Resposta **truncada** depois do 200 (conexão caída no meio
  do corpo) conta como resposta que não decodifica: o cliente devolve `UnreadableBodyError` em vez de erro de rede,
  sem repetir, e o fluxo relê e confere (`TestIntegrationTruncatedValuesAnswerIsDetected`, com proxy local);
- não há releitura extra antes do `PATCH`, como em responsáveis: a leitura dos valores já é a última requisição
  antes da escrita. A janela entre essa leitura e o `PATCH` é detectada, não evitada. `--force-version` pula a
  conferência;
- como o servidor não valida valores, a CLI valida: `checkbox` só `true`/`false`, `date` só `AAAA-MM-DD` válida.
  O texto `null` continua texto; limpar é `--unset` (abaixo).

### Limpar valores: `null` (US #260)

Observado no Taiga local (`taigaio/taiga-back:6.7.3`) em 2026-10-01, em story e em task, no projeto
`cli-test-probe-fields`: `go test -tags integration -run '^TestProbeFieldValuesUnset$' -v ./internal/taiga`.

| Requisição | Resultado |
|---|---|
| `PATCH` com `{date: null, checkbox: true}` | 200, `version` + 1; o `GET` devolve a chave com `null` (não some) |
| `PATCH` com só `{checkbox: null}` (o último campo) | 200: um dicionário só com `null` é aceito, ao contrário de `{}` (400) |
| `null` numa chave que não existia | 200, `version` + 1, e a chave passa a existir com `null`: **não** é no-op no servidor |
| o mesmo dicionário de novo | 200, `version` + 1: o servidor não compara com o atual |

Consequências na CLI (`story field set --unset NOME`, e o serviço de valores de task):

- limpar é gravar `null` na chave, nunca tirar a chave: o dicionário nunca fica vazio, e `null` é o que o Taiga
  devolve na leitura;
- chave ausente e `null` significam "sem valor"; limpar um campo nessa situação não envia nada (o servidor
  acrescentaria a chave e subiria a `version` à toa). Por isso o no-op é da CLI, não do servidor;
- só `checkbox` e `date`. `text` é recusado com erro de uso, embora o servidor aceite `null` em qualquer tipo:
  texto já tem o seu "vazio" (`Nome=`) e um segundo estado sem valor só criaria ambiguidade na leitura;
- `--unset` entra no mesmo merge, com a mesma `version` do recurso de valores, a mesma pós-condição
  (`field_values_postcondition_failed`, comparando `null` com `null`), a mesma releitura para resposta ilegível e o
  mesmo `--dry-run`; o mesmo campo atribuído e limpo no mesmo comando é `duplicate field assignment`;
- exibição: no JSON de `story get` (`custom_attributes.attributes_values`) e de `story field list`
  (`attributes_values` e `fields[].value`) o valor limpo sai `null`, distinto do campo sem chave (sem `value`); no
  texto de `story field list` sai vazio, como campo sem valor, para nunca se confundir com o texto `null`.
  `story get` em texto não mostra campos customizados.


## Fase 2 — comentários (US #250)

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-10-01, com `admin` e `svc` (membro)
no projeto descartável `cli-test-probe-comments`. Testes em `internal/taiga/comments_probe_integration_test.go`
(`go test -tags integration -run '^TestProbeComment' -v ./internal/taiga`) e
`internal/cli/comments_integration_test.go`. Código lido no container: `taiga/projects/history/api.py` e
`serializers.py`, `taiga/hooks/event_hooks.py`, `taiga/hooks/gitlab/` (`api.py`, `event_hooks.py`,
`migrations/0001_initial.py`).

### Publicação: `PATCH userstories/<id> {comment, version}`

| Requisição | Resultado |
|---|---|
| `comment` + `version` atual | 200; a `version` da story sobe 1; a resposta traz `comment: ""` (o texto não volta na story) |
| `comment` + `version` **antiga** (inclusive 1, depois de outras escritas) | **aceito (200)**: o OCC compara as chaves enviadas com as chaves do `diff` do histórico (ver "OCC por campo"), e `comment` nunca é chave de `diff` |
| `comment` sem `version`, ou com `version` maior que a atual | 400 `{"version": "The version parameter is not valid"}` |
| `comment: "   "` (só espaços) | aceito e gravado como entrada de comentário |
| aspas, acentos, quebras de linha, Markdown | gravados como enviados; `comment_html` renderizado pelo servidor |
| membro sem admin (`svc`) | publica; o autor da entrada é `svc` |

Consequências na CLI (`story comment`):

- a `version` não protege nada: não há conflito possível, nem `--force-version`. A CLI envia a `version` lida (o
  servidor exige uma válida) e nunca repete o `PATCH`;
- texto em branco é recusado antes da autenticação (o servidor gravaria a entrada);
- texto que não é UTF-8 válido é recusado (exit 2): o encoder JSON trocaria os bytes por U+FFFD, o texto gravado não
  bateria com o enviado e a conferência no histórico não acharia o comentário publicado;
- a listagem mostra também o comentário só de espaços, que o Taiga gravou como comentário (só a integração GitLab é
  ocultada);
- resposta 2xx ilegível (`UnreadableBodyError`, corpo truncado) segue o caminho de releitura das outras escritas: o
  status já confirma a gravação; se a releitura falhar, `write_applied`;
- erro sem resposta conclusiva (rede depois de aberta a conexão, timeout ou 5xx): antes do `PATCH` a CLI guarda os
  ids dos comentários do próprio usuário (`users/me`) com exatamente o mesmo texto; depois da falha relê o histórico.
  Um id novo com o texto = publicado (sucesso, story relida). Nenhum, ou histórico ilegível = `comment_unconfirmed`
  (exit 1): a ausência não prova que o `PATCH` falhou, porque um gateway pode responder 503 enquanto o servidor ainda
  grava, e o comentário aparece depois da conferência (achado da revisão do PR #7: um script que repetia o exit 7
  criou duas cópias). O erro de rede original (exit 7, repetível) só sai quando a conexão nem abriu (DNS, conexão
  recusada).

### Leitura: `GET history/userstory/<id>`

| Ponto | Resultado |
|---|---|
| ordem | `-created_at`: **mais novo primeiro**. A CLI preserva a ordem do servidor, sem reordenar por texto de data |
| paginação | 30 por página por padrão, com `x-pagination-next`; `x-disable-pagination: True` é respeitado (lista inteira). `GetAll` cobre os dois |
| `?type=comment` | só entradas com `comment` diferente de `""` (inclui o comentário só de espaços); `?type=activity` só `diff` sem comentário. A CLI usa `type=comment` para encurtar e confere o texto localmente |
| entrada | `id` (UUID), `user` (`pk`, `username`, `name`, `photo`, `is_active`, `gravatar_id`), `created_at`, `type` (1 em todos os casos observados, inclusive comentário só), `key`, `diff`, `values_diff`, `snapshot`, `values`, `comment`, `comment_html`, `edit_comment_date`, `delete_comment_date`, `delete_comment_user`, `is_hidden`, `is_snapshot` |
| comentário sem alteração | `diff` e `values_diff` vazios; alteração sem comentário: `comment: ""` |
| `POST history/userstory/<id>/edit_comment?id=<uuid> {comment}` | troca o texto, preenche `edit_comment_date`; **não** muda a `version` da story |
| `POST history/userstory/<id>/delete_comment?id=<uuid>` | preenche `delete_comment_date`/`delete_comment_user`; o texto **continua** na resposta; não muda a `version` |
| autor inativo comum | pela leitura do serializer (não sondado): `username` nulo e `name` o gravado na entrada; só usuário ativo ou de sistema (`is_system`) tem o `username` exposto |

A CLI lista comentários editados e apagados (com as datas), porque esconder é decisão que o plano não tomou; a saída
de texto mostra `edited_at` e `deleted_at`.

### Comentários da integração GitLab

A integração nativa do Taiga (`gitlab-hook`, ativada por `PATCH projects/<id>/modules {"gitlab": {"secret"}}`) grava
como o usuário de sistema criado na migração: `username` = `gitlab-` + 32 hex (`uuid4().hex`), `name` "GitLab",
`is_active: false`, `is_system: true`. No push hook o autor do commit nunca é ligado a um usuário do Taiga
(`user_id: None`), então toda entrada do hook usa esse usuário. Gerado no Taiga local com dois commits:

- menção: `This user story has been mentioned by Dev Exemplo in the [GitLab commit](<url> "See commit '<id> - <msg>'") "<msg>"` (sem `diff`);
- troca de status (`TG-<ref> #<slug>`): `Dev Exemplo changed the status from [GitLab commit](<url> ...)\n\n  - Status: **New** → **Ready for test**` (com `diff` de `status`).

O registro anterior (memória, 2026-09-29) descreve no Taiga da Basis o mesmo usuário `gitlab-…` e o texto "This user
story has been mentioned…".

Regra da CLI (`SystemComment`), conservadora: é comentário de sistema só quando **as duas** coisas batem — autor
`gitlab-<32 hex minúsculos>` com `is_active: false` explícito, **e** texto num dos modelos do hook ("This user story has
been mentioned by … in the [GitLab commit](", "This issue has been mentioned in the GitLab commit ", "… changed the
status from [GitLab commit](", "Changed status from GitLab commit."). Não basta `type`, `diff` vazio, conta de serviço
nem um nome parecido (`gitlab-bot`). Todo o resto, inclusive autor desconhecido, aparece. A fixture
`internal/app/testdata/history_userstory.json` é o histórico real do Taiga local (fotos e gravatar sanitizados).
Os modelos passam por tradução (`_()`) no servidor; no Taiga local saíram em inglês. Se uma instância os gravar
traduzidos, a CLI os deixa visíveis (lado seguro).

## Fase 2 — status e projeto como código (US #249)

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-10-01, com `admin` no projeto
descartável `cli-test-probe-statuses`, onde `svc` é membro sem ser admin. Teste em
`internal/taiga/statuses_probe_integration_test.go`: `go test -tags integration -run '^TestProbeStatusContract$' -v
./internal/taiga`. Código lido no container: `taiga/projects/models.py` (`UserStoryStatus`), `api.py`
(`UserStoryStatusViewSet`), `permissions.py`, `validators.py`, `mixins/ordering.py` e
`services/bulk_update_order.py`.

### Catálogo e criação

| Requisição | Resultado |
|---|---|
| `GET userstory-statuses?project=<id>` | lista com `id`, `name`, `slug`, `order`, `is_closed`, `is_archived`, `color`, `wip_limit`, `project`, já ordenada por `order` e depois `name` (`Meta.ordering`) |
| `GET task-statuses?project=<id>` | mesmos campos, sem `is_archived` e sem `wip_limit` |
| `version` em status | **não existe**: nem na resposta, nem no modelo (sem `OCCModelMixin`). O projeto também não tem: `version` vem `null` |
| `POST userstory-statuses` sem `order` | 201 com `order` **10** (default do modelo), não no fim: num board com mais de dez status, o novo cai no meio |
| `POST` com `order` | aceito como enviado |
| `POST` com `version` | aceito e ignorado: não existe versão inicial a respeitar |
| mesmo nome no mesmo projeto | 400 `{"name": ["Duplicated name"]}`; `unique_together (project, name)` e um `advisory_lock` na criação garantem unicidade também numa corrida |
| mesmo nome com outra caixa | aceito (201), com outro `slug`: o nome é sensível a caixa |
| `color` fora de `#RRGGBB` | aceito e gravado como enviado: o servidor não valida a cor |

Consequências na CLI (`project apply`): nomes declarados iguais a menos de maiúsculas são recusados no TOML, e o
catálogo (status e campos) é relido antes de cada criação. O status novo vai com `order` = maior `order` atual + 1, relido antes de cada
`POST`, para entrar no fim e na ordem do arquivo; a CLI valida a cor (`#RRGGBB`) antes de qualquer requisição. O `POST`
nunca é repetido: 400 em `name` (outra execução criou antes) e resposta perdida depois do 2xx são resolvidos relendo o
catálogo — status igual, sucesso; diferente, `project_changed`; ausente depois de resposta perdida, `write_applied`.
Nome diferente só em maiúsculas de um existente é tratado como drift (o `configurar-taiga-projeto.sh` comparava sem
diferenciar caixa), para não criar um quase-duplicado.

### Permissão

| Conta | Resultado |
|---|---|
| admin do projeto (`membership.is_admin`) e superusuário, mesmo sem ser membro *(superusuário: manual)* | `my_permissions` de `GET projects/<id>` inclui `admin_project_values` (e `i_am_admin: true`) |
| membro sem admin (`svc`) | `my_permissions` sem `admin_project_values`; **lê** o catálogo (200); `POST`, `PATCH` e `bulk_update_order` respondem **403** |

O servidor decide por `IsProjectAdmin` (`is_project_admin`: superusuário ou membro admin), não pela lista; a lista
reflete o mesmo critério nos dois casos sondados. A CLI usa a lista como pré-checagem (ausente ou sem a permissão =
`forbidden`, exit 6, também no `--dry-run`); o 403 do servidor continua valendo para cada escrita. `project plan`
só lê e roda com qualquer membro.

### Ordem: sem OCC (gate da Task 9)

| Requisição | Resultado |
|---|---|
| `PATCH userstory-statuses/<id>` `{order, version: 12345}` | **200**, ordem gravada: a `version` é ignorada |
| `PATCH` `{order}` sem `version` | **200**, ordem gravada |
| `POST userstory-statuses/bulk_update_order` `{project, bulk_userstory_statuses: [[id, order], ...]}` | **204**; aceita qualquer chave extra (`version` inclusive) e não confere nada; roda numa transação (`@transaction.atomic`), um `UPDATE` por par |
| `bulk_update_order` com id de status de outro projeto | 204 sem efeito: o `UPDATE` filtra por projeto, em silêncio |

Nenhum dos dois caminhos tem controle de concorrência verificável: não há `version` no status nem no projeto, e o
servidor aceita qualquer valor. Uma reordenação calculada sobre uma leitura antiga sobrescreve, sem aviso, a ordem que
outra pessoa acabou de definir.

**Decisão humana (2026-10-01): "aceitar com conferência".** A CLI reordena sem OCC, mitigando e documentando, no
mesmo padrão dos responsáveis (US #247). Constante `statusOrderCheckedWrite = true` em
`internal/app/project_status_writer.go`:

- **mecanismo: `bulk_update_order`**, e não `PATCH` por status. É uma requisição só, executada numa transação no
  servidor: a janela entre a releitura e a escrita é de um round-trip, e o board nunca mostra metade da nova ordem.
  Com `PATCH` individual seriam N requisições, N janelas e estados intermediários visíveis; nenhum dos dois tem
  `version`. O corpo leva todos os status do projeto com posições 1..N (os não declarados mantêm a ordem relativa);
- **antes:** relê o catálogo logo antes do bulk; se algum status apareceu, sumiu, mudou de nome, de id ou de `order`
  desde a leitura do plano (mais os criados pelo próprio apply), recusa com `project_changed` (exit 4) sem enviar;
- **depois:** relê e exige exatamente a ordem pretendida. Se não bate, `status_order_postcondition_failed` (exit 4),
  dizendo que a escrita **foi aplicada** (ou teve resultado incerto, em rede/5xx) e para não repetir às cegas. 4xx do
  bulk é recusa: o erro dele, sem conferência. Resposta perdida depois do 2xx: vale a releitura;
- nunca há repetição automática do bulk. Como o bulk grava posições absolutas, rodar o apply de novo depois de
  conferir é seguro: ele replaneja e confere outra vez;
- limite: uma mudança que cai entre a releitura e o bulk é sobrescrita; só é detectada se deixar outra ordem na
  releitura posterior. A conferência detecta parte das corridas, não impede.

Testes: `TestStatusWriterReorder*` (unidade), `TestIntegrationProjectApplyBasisExample`,
`TestIntegrationProjectReorderRefusesAMovedOrder` e `TestIntegrationProjectReorderPostcondition` (corridas via
proxy). OCC de verdade continua dependendo do servidor (por exemplo `version` em status ou no projeto, respeitado
pelo `bulk_update_order`).

## Fase 3 — swimlanes (US #252)

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-10-02, com `admin` em dois
projetos descartáveis criados pelo teste (`cli-test-probe-swimlanes-<sufixo>` e `cli-test-probe-swimlanes-other-<sufixo>`),
com `svc` como membro sem admin, convidado por e-mail. Teste em
`internal/taiga/swimlanes_probe_integration_test.go`: `go test -tags integration -run '^TestProbeSwimlaneContract$' -v
./internal/taiga`. Código lido no container: `taiga/projects/api.py` (`SwimlaneViewSet`), `signals.py`,
`userstories/api.py` e `userstories/filters.py`.

### Catálogo

| Requisição | Resultado |
|---|---|
| `GET swimlanes?project=<id>` | lista com `id`, `name`, `order`, `project`, `statuses` (com `wip_limit` por swimlane), ordenada por `order`; **sem `version`** |
| projeto sem swimlanes | lista vazia; em `GET projects/<id>`, `swimlanes` e `default_swimlane` vêm `null` (não `[]`) |
| `POST swimlanes` | `order` padrão cresce (timestamp): a nova vai para o fim; mesmo nome no mesmo projeto → 400 |
| **primeira** swimlane do projeto | vira `default_swimlane` e **todas** as stories do projeto passam para ela, por `UPDATE` em lote: a `version` das stories **não muda** |
| segunda swimlane em diante | não muda a padrão nem as stories |
| membro sem admin (`svc`) | lê o catálogo (200); `POST swimlanes` → 403 |

### `swimlane` na story

| Requisição | Resultado |
|---|---|
| story criada sem `swimlane` num projeto que já tem swimlanes | fica com `swimlane: null`; a padrão **não** é aplicada na criação |
| `PATCH {swimlane: <id>, version}` e `{swimlane: null, version}` | gravados, `version` +1; a releitura confirma |
| membro sem admin | move a story de swimlane (é campo da story, não escrita de swimlane) |
| swimlane de outro projeto | 403 (`You don't have permissions to set this swimlane`) |
| id inexistente | 400 em `swimlane` |
| `version` antiga com campo disjunto alterado desde então | aceito; com `swimlane` alterado desde então, 400 em `version` (OCC por campo, como nos demais) |

### Filtro da listagem

| Parâmetro de `GET userstories?project=<id>` | Resultado |
|---|---|
| `swimlane=<id>` ou `swimlane=null` | **ignorado** (lista tudo), como já visto na #246 |
| `swimnlane=<id>` | respeitado: o filtro do servidor (`SwimlanesFilter`) lê o parâmetro com esse erro de digitação |
| `swimnlane=null` | respeitado: só as stories sem swimlane |

Consequência na CLI: `story list --swimlane`/`--no-swimlane` mandam `swimnlane` para encurtar a lista e **sempre**
filtram localmente depois do `GetAll`; se uma versão futura corrigir o nome do parâmetro, o servidor passa a ignorá-lo e
o resultado continua certo. `--no-swimlane` é uma flag própria, sem valor mágico (`none` pode ser nome real).
`--clear-swimlane` em `story update` envia `{swimlane: null, version}` pelo caminho versionado normal. Criar, renomear,
reordenar e apagar swimlanes ficam fora da CLI (decisão de 2026-10-01): exigem admin, a primeira move todas as stories
sem passar pelo OCC, e a ordem não tem `version`.

## Fase 3 — anexos (US #251)

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-10-02, com `admin` em dois
projetos descartáveis criados pelo teste (`cli-test-probe-attachments-<sufixo>` e
`cli-test-probe-attachments-other-<sufixo>`, ambos privados), com `svc` como membro sem admin do primeiro, convidado por
e-mail. O `POST` multipart é montado no próprio teste (`mime/multipart`), sem o código do cliente. Teste em
`internal/taiga/attachments_probe_integration_test.go`: `go test -tags integration -run '^TestProbeAttachmentContract$'
-v ./internal/taiga`. Código lido no container: `settings/config.py`, `settings/common.py`, `taiga/urls.py` e o
`server.py` do `taigaio/taiga-protected:6.7.0`.

### Upload: `POST <userstories|tasks>/attachments` (multipart)

Campos `project`, `object_id`, `description` e o arquivo em `attached_file`.

| Caso | Resultado |
|---|---|
| sucesso | 201 com `id`, `name`, `size`, `sha1`, `url`, `object_id`, `project`, `order` (0), `is_deprecated` (false), `from_comment` (false), `description`, `created_date`, `owner`; **sem `version`**. `size` e `sha1` batem com os calculados localmente |
| nome com acento, espaço ou aspas (`relatório final.txt`, `with "quotes".txt`) | `name` guardado como enviado; só o caminho do `url` é normalizado (`relatorio-final.txt`, `with-quotes.txt`) |
| nome com diretório (`../x`, `dir/sub/inner.txt`) | `name` vira o basename (`x`, `inner.txt`) |
| nome com CR/LF, barra invertida ou entidade HTML | gravado **diferente** do enviado: o Go codifica CR/LF (`%0D%0A`), o Django guarda só o que vem depois da `\` e aplica o `html.unescape` do Python, que aceita entidades **sem `;`** (`report&amp.txt` → `report&.txt`, `report&#65.txt` e `report&#x41.txt` → `reportA.txt`; revisão independente da PR #12). Como a idempotência compara nome e `sha1`, a CLI recusa esses nomes (`usage`, renomear o arquivo) |
| arquivo vazio (0 byte) | **400** `{"attached_file": ["The submitted file is empty."]}` |
| `object_id` de story de outro projeto, com `project` deste | 400 `Project ID does not match between object and project` (`WrongArguments`) |
| `object_id` inexistente | 400 `Object id issue doesn't exist` |
| o mesmo arquivo duas vezes | dois anexos (sem unicidade no servidor) |
| `version` da story depois do upload | **não muda** (1 → 1) |
| membro sem admin (`svc`) | envia (201, `owner` = svc) e lista |
| não membro de projeto privado | upload 403; `GET` do anexo 403; a listagem responde 200 **vazia** (o filtro esconde, não recusa) |

O tamanho não tem limite no código do servidor; o limite vem do proxy. **No proxy da Basis, `client_max_body_size
50M`** (informado pelo Cedric em 2026-10-02). Acima disso o nginx responde 413 com página HTML própria (o gateway do
`compose.test.yml` usa o mesmo limite e reproduz o 413); a CLI traduz para `payload_too_large` e não impõe limite próprio.

### Leitura

| Requisição | Resultado |
|---|---|
| `GET userstories/attachments?project=&object_id=` | só os anexos daquele objeto, em ordem de `id`; paginada (`x-paginated: true`, `x-pagination-count`) |
| `GET tasks/attachments?project=&object_id=` | idem para a task; anexos de story não aparecem |
| sem `object_id` | todos os anexos daquele tipo no projeto |
| `GET <tipo>/attachments/<id>` | o anexo, com `url` assinado na hora (token novo a cada leitura em segundo diferente). `preview_url` e, em imagem, `thumbnail_card_url` também levam token e abrem o arquivo (ou a miniatura) sem autenticação (revisão adversarial da #251); a CLI tira da saída toda chave terminada em `url` e todo texto com `token=` |
| `GET tasks/attachments/<id de anexo de story>` | **200** com o anexo da story: o detalhe **não confere o tipo**. Conferir só o `object_id` não basta (id de story e de task podem coincidir); a CLI confere que o id está na **lista** do objeto, que filtra pelo tipo |

### Download: o `url`

`url` = `MEDIA_URL` + caminho + `?token=<assinatura>` + `#_taiga-refresh=<tipo>:<id>`. `MEDIA_URL` =
`<TAIGA_SITES_SCHEME>://<TAIGA_SITES_DOMAIN>/media/`, a mesma origem da API. O storage é
`taiga_contrib_protected.storage.ProtectedFileSystemStorage`: o token é um `TimestampSigner` (sal `taiga-protected`)
sobre o caminho, conferido pelo `taiga-protected` com `MAX_AGE` (360 s no compose; 3600 s se ausente), que responde
200 vazio com `X-Accel-Redirect: /_protected/<caminho>`. Quem entrega os bytes é o nginx do gateway, a partir do volume
de mídia. O fragmento nunca vai ao servidor.

| Onde | `GET <url>` | Resultado |
|---|---|---|
| só `taiga-back` publicado (o `compose.test.yml` até a #251) | com token, sem token, token adulterado, com e sem `Authorization`, `/static/...` | **404** (página `Not Found` do Django) em todos: `DEBUG=False`, e o `taiga/urls.py` só serve `/media/` com `DEBUG`. O arquivo existia em `/taiga-back/media/attachments/...` |
| gateway do `compose.test.yml` (desde a #251) | token válido, com ou sem `Authorization` | 200, `text/plain`, bytes iguais ao enviado |
| idem | token do upload (outro, mais antigo, dentro do `MAX_AGE`) | 200 |
| idem | sem token, com ou sem `Authorization` | 403 (o `Authorization` não substitui o token) |
| idem | token adulterado | 403 |

Token expirado (`MAX_AGE`) não foi forçado: o `taiga-protected` recusa com o mesmo 403 da assinatura inválida
(`BadData` do `itsdangerous`, lido no `server.py`).

**Gate do download (Task 3, Step 3), decidido em 2026-10-02 (Cedric, opção B):** o `compose.test.yml` ganhou, no
default, o `taiga-protected` e um gateway nginx (`taiga-gateway`) em `127.0.0.1:8000`, como o gateway do
`taiga-docker`: `/api/` e `/admin/` vão para o `taiga-back`, que deixou de publicar porta; `/media/` vai para o
`taiga-protected`; `/_protected/` é `internal` e serve o volume `taiga-media`, compartilhado com o `taiga-back`.
`client_max_body_size 50M`, igual ao proxy da Basis. A URL de teste continua `http://localhost:8000`, e a CI cobre o
download e o 413.

## Fase 3 — catálogos e épicos (US #253)

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-10-02, com `admin` (superusuário,
id 5 no seed atual) e `svc` (membro sem admin, convidado por e-mail) em projetos descartáveis criados pelo teste
(`cli-test-probe-catalogs-<sufixo>`, `-alone-`, `-public-` e um criado pelo próprio `svc`). Teste em
`internal/taiga/catalogs_probe_integration_test.go`: `go test -tags integration -run '^TestProbeCatalogs$' -v
./internal/taiga`. Código lido no container: `taiga/projects/api.py` e `filters.py`, `milestones/api.py`,
`epics/api.py`.

### Projetos

| Requisição | Resultado |
|---|---|
| `GET projects?member=<id de users/me>` | só os projetos de que a conta é membro, também para o superusuário. Na leitura da redação, `member=1` devolveu `[]` porque o id do `admin` é 5, não 1 |
| `GET projects` sem `member` | tudo o que a conta **vê**: para `svc`, os projetos de que é membro **mais os públicos**; para o superusuário, **todos**, inclusive privados sem membership |
| `GET projects/<id>` de projeto privado sem acesso | 403 |
| `GET projects/by_slug?slug=` de projeto privado sem acesso | 404 |
| `q=` | busca de texto completo (`to_tsquery` sobre nome, tags e descrição), não substring |
| `GET projects/<id>` como membro | traz `i_am_member`, `i_am_admin`, `my_permissions`, `is_epics_activated`, `is_kanban_activated`, `is_backlog_activated`, `swimlanes`, `default_swimlane` |
| `my_permissions` de `svc` com o papel padrão do template | `view_project`, `view_us`/`add_us`/`modify_us`/`comment_us`/`delete_us`, o mesmo para task, epic, issue, milestone e wiki; **sem** `admin_project_values` nem `admin_roles` |

| `GET projects/<id>` como admin (inclusive superusuário sem membership) | traz `epics_csv_uuid`, `userstories_csv_uuid`, `tasks_csv_uuid`, `issues_csv_uuid` e `transfer_token`. Com um `*_csv_uuid`, `GET <tipo>/csv?uuid=` serve a exportação **sem autenticação** (`csv_perms = AllowAny`, lido no código; revisão adversarial da #253) |

Consequência na CLI: `project list` manda `member=<id de users/me>` (a lista do superusuário seria o servidor inteiro)
confere `i_am_member` localmente, e `--search` filtra localmente por nome e slug, sem diferenciar maiúsculas; o `q` do servidor não é enviado, porque
não é substring e esconderia resultados. `project list` e `project get` nunca imprimem os `*_csv_uuid` nem o
`transfer_token`.

### Paginação

`x-disable-pagination: True` é respeitado por `projects`, `milestones` e `epics` (sem `x-pagination-next`).
`Client.GetAll` continua seguindo o `x-pagination-next` se um dia vier.

### Milestones

| Requisição | Resultado |
|---|---|
| `GET milestones?project=` | `id`, `name`, `slug`, `estimated_start`, `estimated_finish`, `closed`, `order`, `owner`, `project`, `project_extra_info`, `total_points`, `closed_points`, `disponibility`, `created_date`, `modified_date` e `user_stories` (as stories inteiras do sprint; vazio aqui) |
| `closed=true` / `closed=false` | respeitado |
| `project__slug=` | respeitado |

Consequência na CLI: `milestone list --closed` envia `closed` e confere localmente; a saída de texto mostra só as
colunas curtas, o JSON leva o objeto inteiro.

### Épicos

| Requisição | Resultado |
|---|---|
| `GET epics?project=` | `id`, `ref`, `subject`, `status`, `status_extra_info` (`name`, `color`, `is_closed`), `is_closed`, `color`, `tags` (pares), `assigned_to`, `user_stories_counts`, `version`, sem `description` |
| `GET epics/by_ref?project=&ref=` | o detalhe: os mesmos campos mais `description`, `description_html`, `comment`, `neighbors` |
| `epics/by_ref` com o ref de uma **story** | 404 (ref de outro tipo não é épico) |
| `GET epics/<id>/related_userstories` | lista com `user_story` (id), `epic` (id) e `order` (timestamp em ms) |
| story vinculada | `GET userstories/<id>` traz `epics` como lista de `{id, ref, subject, color, project{id,name,slug}}` |
| `status__is_closed=true\|false` | respeitado |
| `q=` | respeitado, mas é busca de texto (ref ou palavras do assunto), não substring |
| **módulo desligado** (`is_epics_activated: false`) | `epics`, `epics/by_ref` e `epics/<id>` continuam respondendo **200**, para `admin` e para `svc`: a chave só esconde o módulo na interface web; a API não a confere |

Consequência na CLI: `epic list --closed` envia `status__is_closed` e confere localmente; `--search` filtra só
localmente pelo assunto (o `q` do servidor não é substring). Como o servidor não recusa nada com o módulo desligado,
`epic list` e `epic get` conferem `is_epics_activated` do projeto e recusam com `not_found` e a recuperação "the epics
module is disabled in this project", que é o comportamento combinado no plano; `taiga api` continua lendo.

### Usuários

`GET memberships?project=` traz `user`, `full_name`, `role`, `role_name`, `is_admin`, `is_owner`, `is_user_active`,
`user_email`, `email`, mas **não** o `username`. `GET users?project=` traz `username`, `full_name`,
`full_name_display`, `is_active`, `roles`, mas lista também não membros (Fase 2). `user list` cruza os dois: só quem
tem membership, com o `username` vindo de `users`.

**Fotos de usuário.** `photo` e `big_photo` (em `users`, `owner_extra_info`, `assigned_to_extra_info`,
`assigned_users_extra_info`, o `user` do histórico) são URLs assinadas de mídia,
`<TAIGA_URL>/media/user/…/<arquivo>.80x80_q85_crop.jpg?token=<assinatura>` (e `300x300` na `big_photo`), que abrem o
arquivo **sem autenticação**: confirmado em produção pelo Cedric em 2026-10-02 (`story get 251`, só leitura) e no
Taiga local, onde o seed não tem foto e o teste `TestIntegrationStoryOutputsHideUserPhotos` põe um avatar no `svc`
(`POST users/change_avatar`, multipart `avatar`) e o tira no fim (`POST users/remove_avatar`). Toda saída curada passa
por `app.Scrub`: as chaves e a URL ficam, o valor do token vira `…`.

## Fase 3 — vínculo de épico (US #253, PR 253-2)

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-10-02, com `admin` e `svc` (membro
convidado por e-mail) em projetos descartáveis (`cli-test-probe-epic-links-<sufixo>` e `-other-`). Teste em
`internal/taiga/epic_links_probe_integration_test.go`: `go test -tags integration -run '^TestProbeEpicLinkContract$'
-v ./internal/taiga`.

| Requisição | Resultado |
|---|---|
| `POST epics/<id>/related_userstories {"epic", "user_story"}` | 201 `{"epic", "user_story", "order"}`; a `version` da story **não muda**; `GET userstories/<id>` já traz o épico em `epics` |
| o mesmo `POST` de novo | **400** `{"__all__": ["Related user story with this User story and Epic already exists."]}`; nada muda |
| segundo épico na mesma story | 201; `epics` com os dois |
| `epic` do corpo diferente do `<id>` do caminho | vale o **corpo**: `POST epics/<E3>/related_userstories {"epic": E2}` respondeu o 400 de duplicata de E2 |
| `DELETE epics/<id>/related_userstories/<story id>` | 204 sem corpo; `epics` relido já sem o épico; `version` da story igual |
| o mesmo `DELETE` de novo | **404** `{"_error_message": ""}` |
| `GET epics/<id>/related_userstories/<story id>` | 200 com o vínculo (o mesmo formato do `POST`) |
| épico de **outro projeto** | **201**: o servidor vincula (como `admin`) e a story passa a listar o épico estrangeiro |
| `svc` com o papel padrão de membro | tem `modify_epic`; `POST` 201 |
| `svc` sem `modify_epic` no papel | `POST` e `DELETE` → **403** `PermissionDenied` |

Consequências na CLI:

- O vínculo não tem `version` nem altera a da story. A conferência é da CLI: os `epics` da story são relidos antes do
  `POST` e qualquer mudança desde a primeira leitura é `version_conflict` (nada enviado); depois da escrita, a
  pós-condição relê a story.
- O 400 de duplicata e o 404 do `DELETE` repetido são o que torna o rerun seguro: rodar o mesmo comando de novo não
  cria duplicata e não falha por um vínculo já removido. Mesmo assim, o 400 só conta como sucesso depois da releitura
  mostrar o vínculo, porque outro 400 (corpo inválido) tem o mesmo status.
- O `epic` do corpo é sempre o mesmo id do caminho.
- O servidor não confere o projeto do épico; a CLI resolve o épico só no projeto selecionado (`epics/by_ref` pelo
  catálogo do projeto), então `epic link` nunca vincula um épico de outro projeto. Na troca, um épico de outro projeto
  já vinculado à story conta como antigo e é removido.
- Sem `modify_epic`, `epic link` sai com `forbidden` (exit 6); o check `project` do `auth status --diagnose` já lista
  a permissão ausente.

## Fase 3 — tasks (US #253, PR 253-3)

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-10-03, com `admin` e `svc` (membro
convidado por e-mail) em projetos descartáveis (`cli-test-probe-tasks-<sufixo>` e `-other-`). Teste em
`internal/taiga/tasks_probe_integration_test.go`: `go test -tags integration -run '^TestProbeTaskContract$' -v
./internal/taiga`.

### Criação e leitura

| Requisição | Resultado |
|---|---|
| `POST tasks {project, user_story, subject, status, tags, assigned_to, due_date}` | 201; `version` 1; tags em pares `[nome, cor]`, minúsculas; **sem `assigned_users`** (um responsável só, `assigned_to`); traz `owner`, `user_story`, `milestone` (o da story), `is_closed`, `due_date`, `due_date_reason`, `due_date_status`, `is_blocked`, `blocked_note`, `created_date` |
| `ref` da task | **mesma sequência das stories** do projeto (stories 1 e 2, task 3) |
| `POST` mínimo (`project`, `user_story`, `subject`) | `description` `""`, `tags` `[]`, `due_date` e `assigned_to` `null`, `status` = `default_task_status` do projeto (`GET projects/<id>`), `is_blocked` `false`, `blocked_note` `""`, `attachments` `[]`, `is_closed` `false`, `version` 1 (também na releitura). Registro de contrato: a CLI não usa esses padrões para decidir um `task create` incerto, que é sempre `task_create_unconfirmed` (decisão de 2026-10-03) |
| sprint da task | **herda a da story**: task criada numa story da sprint 1 nasce com `milestone` 1; a story passa para a sprint 2 e a task acompanha. Por isso a CLI não tem `--milestone` em task |
| `POST tasks` sem `user_story` | 201, `user_story: null` (a CLI exige `--story`; task solta só por `taiga api`) |
| `user_story` de outro projeto | **400** `WrongArguments` "You don't have permissions to set this user story to this task." |
| `due_date` fora de `AAAA-MM-DD` (`31/12/2026`, `2026-02-30`) | **400** "Date has wrong format" |
| `tasks/by_ref?project=<este>&ref=<ref>` com ref que só existe em outro projeto, ou que é uma **story** deste | **404**; `userstories/by_ref` com ref de task também dá 404 |

### Filtros da listagem `GET tasks?project=<id>`

| Parâmetro | Resultado |
|---|---|
| `user_story`, `status`, `assigned_to`, `tags`, `q`, `status__is_closed` | respeitados |
| `ref`, `user_story__isnull` | **ignorados** (lista inteira) |

A CLI manda os respeitados e confere todos localmente depois do `GetAll` (Review Focus 6).

### OCC e `assigned_to`

Ao contrário da story, `assigned_to` **entra no `diff` do histórico da task**, e o OCC por campo o protege:

| Requisição | Resultado |
|---|---|
| outra escrita troca `assigned_to`; depois `PATCH {version antiga, assigned_to}` | **400** "The version doesn't match with the current one" |
| a mesma `version` antiga com só `subject` | 200 (campo não alterado desde então) |

Consequência: a task **não** precisa da releitura antes e da pós-condição de responsáveis da #247. `assigned_to` da
task segue o caminho normal de `WriteVersionedFrom` (uma repetição só se nenhum campo enviado mudou), porque a
resposta mostra o valor gravado inteiro.

### Bloqueio, `due_date`, status fechado

- Bloqueio igual ao da story: `blocked_note` sem `is_blocked` é descartada; `is_blocked: false` limpa a nota.
- `due_date: null` limpa; `due_date_reason` é texto livre e independente (a CLI não o expõe).
- `task-statuses` do template padrão: `New`, `In progress`, `Ready for test`, `Needs Info` abertos e **um só fechado**, `Closed`. Mudar para o status fechado põe
  `is_closed: true` e `finished_date`.

### Comentários

- `PATCH tasks/<id> {comment, version antiga}` é aceito (o comentário não é campo da task), como na story.
- `GET history/task/<id>?type=comment` tem o mesmo formato e a mesma ordem (mais novo primeiro) de
  `history/userstory/<id>`.
- Texto da integração GitLab para task: sem evidência; o modelo "This issue has been mentioned…" já está em
  `SystemComment`. Pendente se aparecer outro modelo.
