# Notas de comportamento da API do Taiga 6.7

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em 2026-09-30.

| Ponto | Resultado | Teste |
|---|---|---|
| Vida do `auth_token` | ≈ 24 h (claim `exp` do JWT) | `TestProbeTokenLifetimes` |
| Vida do `refresh` | ≈ 8 dias (192 h, claim `exp` do JWT) | `TestProbeTokenLifetimes` |
| Refresh rotaciona | sim: `POST /auth/refresh` devolve um `refresh` novo | `TestProbeRefreshRotationInvalidatesPrevious` |
| Refresh anterior invalidado | sim: reusar o `refresh` antigo é recusado. Por isso `refreshInvalidatesPrevious = true` e, com o cache somente leitura, a CLI não renova e devolve `session_expired` | `TestProbeRefreshRotationInvalidatesPrevious` |
| `PATCH` com `version` desatualizado | `400 {"version": "The version doesn't match with the current one"}` | sondagem manual da US #245; `TestIntegrationAutoVersionOnStory` |
| `PATCH` sem `version` | `400 {"version": "The version parameter is not valid"}`, também tratado como conflito | sondagem manual da US #245 |
| `x-disable-pagination` respeitado | sim (lista sem `x-pagination-next`) | `TestIntegrationAPIProjectsAndUsersMe` |
| Paginação | `?page=N`; o próximo vem em `X-Pagination-Next` | sondagem manual da US #245 |
| Application tokens para conta de serviço | não validado na fase 1: exige cadastrar uma Application pelo admin do Django; fica para quando houver demanda | — |
| `userstories/by_ref?ref=&project=` | validado na fase 2; ver seção abaixo | `TestProbeStoryByRef` |
| Escrita de swimlane, upload de anexo, comentários no histórico | fases 2 e 3 | — |

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
