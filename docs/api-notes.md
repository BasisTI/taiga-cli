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
| Escrita de swimlane, upload de anexo, comentários no histórico | fases 2 e 3 | — |
| Relação `assigned_to` × `assigned_users`, bloqueio | validado na fase 2; ver "responsáveis e bloqueio" | `TestProbeStoryAssignees`, `TestProbeStoryBlock` |

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
| vínculo de épico | `POST epics/<id>/related_userstories {"epic", "user_story"}` → 201, **sem `version`** e sem alterar a `version` da story; uma story aceita vários épicos *(manual)*; trocar ou remover o vínculo exige `DELETE` (`PATCH` no vínculo respondeu 500 *(manual)*) |
| POST de story com `version` | aceito e gravado (a story nasce com a versão enviada); sem `version` nasce com 1. A CLI não envia `version` na criação |
| tags | o servidor grava em minúsculas; sem duplicatas (`["Dup","dup"]` → `[["dup",null]]`) *(manual)* |

Consequência: `--epic` em `story create`/`story update` fica bloqueado com `unsupported_operation`
(exit 2) até decisão humana, conforme o gate do plano (vínculo sem OCC e substituição por `DELETE`).
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
   exige que a resposta do PATCH seja a `version` seguinte à da releitura; um salto indica outra escrita no meio,
   inclusive uma troca de `assigned_to` que não deixa rastro. Se algo não bate, sai com
   `assignees_postcondition_failed` (exit 4), dizendo que a escrita **foi aplicada**, com o estado encontrado. Não
   há repetição automática.

`--force-version` pula as duas conferências. **Janela residual:** entre a releitura final e o PATCH, uma troca
concorrente de `assigned_to` ainda é gravada pelo Taiga e pode ser desfeita pelo nosso PATCH; a CLI então detecta
e avisa (exit 4), mas não evita. Um falso alarme também é possível: qualquer escrita de outra pessoa nessa janela,
mesmo em outro campo, faz a `version` saltar e gera o erro com a escrita aplicada.

