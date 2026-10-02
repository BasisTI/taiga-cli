# taiga-cli — Fase 3 (anexos, swimlanes e paridade com o MCP) — plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans para implementar este plano tarefa por tarefa. Delegação depende da escolha do humano. Steps usam checkboxes (`- [ ]`) para acompanhamento.

**Goal:** Entregar anexos de story e task (listar, enviar, baixar), swimlanes (listar e usar), e a paridade com o MCP do Taiga — projetos, usuários, milestones, épicos (listar, obter, vincular e trocar o vínculo), tasks (listar, obter, criar, atualizar, fechar, campos, comentários) e diagnóstico do projeto —, mantendo as regras de escrita que a fase 2 provou necessárias.

**Architecture:** a mesma da fase 2. `internal/cli` faz parsing e renderização; `internal/app` resolve projeto, refs e nomes e calcula as escritas; `internal/taiga` cuida de HTTP, paginação e concorrência. Esta fase acrescenta ao cliente dois caminhos que não são JSON: upload multipart e download em streaming. A autenticação continua entrando só por `App.client(ctx, rc)` e `taiga.TokenSource`.

**Tech Stack:** Go 1.27.0; cobra; go-toml/v2; `encoding/json`; `mime/multipart`; `crypto/sha1` (só para conferir o `sha1` que o Taiga calcula, não como segurança); httptest; Taiga local 6.7.3 (`compose.test.yml`).

**Spec:** `docs/superpowers/specs/2026-09-30-taiga-cli-design.md`, seções 6, 7, 9–11. Base: `docs/superpowers/plans/2026-09-30-fase-2-stories-e-campos.md` e o código em `main` `07c3f7a` (v0.2.0).

**Fontes lidas:**
- Código em `07c3f7a`.
- Descrições da #251 (id 6813), #252 (6814), #253 (6815) e #254 (6816), lidas no projeto 37 sem escrita.
- `docs/api-notes.md`, `docs/errors.md`, `docs/guia.md`, `README.md`.
- ai-memory `perso/vault-assistente`: `decisions/taiga-cli-substitui-mcp-do-taiga.md` e `notes/taiga-mcp-funcionalidades-e-limitacoes.md`.
- Catálogo do MCP em uso: `taiga_projects_list/get`, `taiga_users_list`, `taiga_milestones_list`, `taiga_epics_list/get/add_user_story`, `taiga_tasks_list/get/create/update/archive_or_close`, `taiga_diagnostics`.

**Leitura feita na redação (2026-10-01), só leitura:** código do `taiga-back` 6.7.3 dentro do container local e `GET`/`OPTIONS` no Taiga local já em execução. Nada foi criado, alterado nem reiniciado. Os achados abaixo são **hipóteses fortes**, lidas no código do servidor; cada US começa por uma sondagem que as confirma antes do código curado.

## Achados da leitura do servidor (a confirmar nas sondagens)

| Recurso | Achado | Onde |
|---|---|---|
| Anexo | sem `version` (modelo sem `OCCModelMixin`); `PATCH` parcial permitido, `PUT` recusado | `taiga/projects/attachments/{models,api}.py` |
| Anexo | `size`, `name` (basename do arquivo enviado) e `sha1` são calculados pelo servidor; `sha1` é somente leitura | `api.py` (`pre_save`), `validators.py` |
| Anexo | filtros `project` e `object_id`; o objeto precisa ser do mesmo projeto (400 senão) | `api.py` |
| Anexo | `url` = `MEDIA_URL` + caminho + `#fragmento`; no deploy padrão, `MEDIA_URL` = `<TAIGA_URL>/media/` com storage protegido (`?token=`), servido pelo `taiga-protected` (`MAX_AGE` 360 s no compose) | `serializers.py`, `settings/config.py`, `compose.test.yml` |
| Anexo | o servidor não limita o tamanho no código; o limite vem do proxy (`client_max_body_size` do nginx) | `settings/` |
| Swimlane | sem `version`; `unique_together (project, name)`; `order` padrão = timestamp em ms (uma nova cai no fim); criar, alterar e reordenar exigem admin, listar é livre | `taiga/projects/models.py`, `permissions.py` |
| Swimlane | ao criar a **primeira** swimlane de um projeto, o servidor move **todas** as stories para ela; a swimlane padrão não pode ser apagada | `taiga/projects/api.py` (`SwimlaneViewSet.post_save`/`pre_delete`) |
| Swimlane | `GET swimlanes?project=` devolve `id`, `name`, `order`, `project` e `statuses` (com `wip_limit` por swimlane); `GET projects/<id>` traz `swimlanes` e `default_swimlane` | Taiga local, `cli-test` |
| Vínculo de épico | `RelatedUserStory` sem `version`, com `unique_together (user_story, epic)`: duplicata é recusada pelo banco; uma story aceita vários épicos | `taiga/projects/epics/models.py` |
| Vínculo de épico | criar, alterar e apagar exigem `modify_epic`; ler exige `view_epics` | `epics/permissions.py` |
| Task | `OCCModelMixin`, `BlockedMixin`, `TaggedMixin`, `DueDateMixin`; **um** responsável (`assigned_to`), sem `assigned_users`; `user_story` opcional no modelo | `taiga/projects/tasks/models.py` |
| Épico | `OCCModelMixin` | `taiga/projects/epics/models.py` |

## Global Constraints

- Módulo `github.com/BasisTI/taiga-cli`; binário `taiga`; Go `1.27.0`; Apache 2.0. Sem dependências novas: multipart e hash vêm da biblioteca padrão.
- Comandos, flags, mensagens, códigos e README em inglês; `docs/` em PT-BR.
- **DELETE em comando curado: só a troca de épico** (`--replace-epic`/`epic link --replace`), e só com `--confirm-delete`, por decisão humana de 2026-10-01. Nenhum outro comando curado faz DELETE. `close` de task só muda o status, sem tag de arquivo (o MCP acrescenta `archived-by-mcp`; a CLI não).
- Toda escrita oferece `--dry-run` (método, caminho e corpo; no upload, os campos do formulário e nome, tamanho e `sha1` do arquivo, sem o conteúdo).
- **OCC:** toda alteração de recurso versionado (task, story) usa a `version` lida e **envia todo campo de que o cálculo dependeu** (lição da #247: o OCC do Taiga é por campo). Recursos sem `version` (anexo, vínculo de épico, swimlane) seguem o padrão "mitigar e documentar": releitura antes, pós-condição depois, erro específico e nenhuma repetição.
- **Pós-condição** pela resposta da escrita; se ela não decodificar (inclusive `UnreadableBodyError`, corpo truncado depois do 2xx), pela releitura; se nem a releitura der, `write_applied` (exit 1).
- **Nunca repetir escrita.** Retry HTTP só para `GET`. `POST` de anexo, de vínculo, de task e de comentário nunca é repetido depois de timeout, rede ou 5xx.
- **Resultado incerto nunca sai com exit repetível (7).** Exit 7 só quando a conexão nem abriu (DNS, conexão recusada) ou numa leitura pura (download).
- Exit 0 OK, 1 inesperado/incerto, 2 uso, 3 autenticação, 4 conflito ou pós-condição, 5 não encontrado, 6 permissão, 7 rede/servidor. Códigos novos entram em `docs/errors.md` na mesma PR que os cria.
- Saída de texto escapa controles e bidi (`escapeJSON`/`unsafeRune` de `internal/cli/curated.go`), inclusive nome e descrição de anexo, nome de swimlane, épico, milestone e projeto.
- Ref nunca é id; ref exige projeto; id explícito é conferido contra o projeto selecionado. Task por ref usa `tasks/by_ref` (validado na #248).
- Reusar `Client.GetAll`, `WriteVersionedFrom`, `Service.Write`, `Service.Catalog`, `Resolve`, `BuildPatch`, `MergeNames`, `reread`, `confirmed`, `WriteApplied`, a resolução de responsáveis e o fluxo de comentário (`ownComments`/`findComment`), generalizando por tipo (`story`/`task`) em vez de copiar.
- `cli → app → taiga`; `app` depende da interface estreita `API` (ampliada aqui com `Upload` e `Download`, Task 4).
- **Testes de integração autossuficientes:** cada teste cria a partir do seed (`scripts/taiga-seed`: `admin`, `svc`, `cli-test`) o próprio projeto descartável, com sufixo único. Membros entram por convite **por e-mail** (`svc@example.com`), como na fase 2. Nenhum teste depende de dado deixado por outro. Antes de abrir cada PR, a suíte roda num Taiga **recém-criado** (`docker compose -f compose.test.yml down -v && up -d`, seed, testes), em janela combinada com o Cedric, porque outro agente pode estar usando o Taiga local.
- Nunca escrever em `agile.basis.com.br`. Smoke manual permitido lá só em leitura (`attachment list`, `swimlane list`, `epic list`, `task list`).
- Branch e PR conforme cada US abaixo, squash para `main`. Ordem: #252 → #251 → #253 (leitura) → #253 (épicos) → #253 (tasks). Cada branch nasce da `main` com a anterior mergeada.
- Este documento não autoriza push, PR, merge nem mudança de status no Taiga.

## Review Focus

1. **Escritas sem `version` (anexo, vínculo de épico):** releitura antes, pós-condição depois, erro específico com o estado encontrado, nenhuma repetição. Tasks 5, 10.
2. **Troca de épico com DELETE:** o vínculo novo é criado **antes** de apagar os antigos (a story nunca fica sem épico); `--confirm-delete` obrigatório, também no `--dry-run` para mostrar o DELETE; a falha no meio não é repetida, e rodar o mesmo comando de novo converge. Task 10.
3. **`story create --epic` / `task create`:** a criação já aconteceu quando o vínculo falha; o erro não pode convidar a repetir a criação (duplicaria a story). Tasks 10, 12.
4. **Download:** a URL vem do servidor e carrega token; só a mesma origem do `--url`, sem `Authorization`, sem seguir redirect, nome de arquivo do servidor tratado como não confiável, nunca sobrescrever sem `--overwrite`, conferência de tamanho e `sha1`, arquivo temporário renomeado só depois de conferido. Tasks 4, 5.
5. **Responsável de task:** `assigned_to` é campo único; a sondagem precisa confirmar que, ao contrário da story, ele entra no `diff` do histórico da task (o OCC protege). Se não entrar, o padrão de releitura e pós-condição da #247 vale aqui também. Task 11.
6. **Filtros que o servidor ignora** (como `swimlane` e `ref` em stories): filtrar localmente depois do `GetAll` e nunca confiar só no parâmetro. Tasks 2, 7, 12.

## Mapa de arquivos e dependências

| Área | Arquivos | Responsabilidade |
|---|---|---|
| Núcleo | `internal/taiga/client.go`, `internal/taiga/transfer.go` (novo), `internal/taiga/errors.go` | `Upload` multipart sem retry; `Download` em streaming com checagem de origem; `413` → `payload_too_large` |
| Swimlanes | `internal/app/swimlanes.go` (novo), `internal/cli/swimlane.go` (novo), `internal/cli/story.go` | Catálogo, filtro local, `--clear-swimlane` |
| Anexos | `internal/app/attachments.go` (novo), `internal/cli/attachment.go` (novo) | Listar, enviar com pós-condição, baixar com conferência |
| Leitura | `internal/app/catalogs.go` (novo), `internal/cli/project.go`, `user.go`, `milestone.go`, `epic.go` (novos) | Projetos, usuários, milestones, épicos |
| Diagnóstico | `internal/auth/diagnose.go`, `internal/cli/auth.go` | Check `project` |
| Vínculo de épico | `internal/app/epic_links.go` (novo), `internal/cli/epic.go`, `internal/cli/story.go` | Acrescentar e trocar com confirmação |
| Tasks | `internal/app/tasks.go` (novo), `internal/app/comments.go`, `internal/app/field_values.go`, `internal/cli/task.go`, `task_fields.go`, `task_comments.go` (novos) | Tasks, campos e comentários |
| Evidência | `docs/api-notes.md`, `internal/taiga/*_probe_integration_test.go`, `internal/cli/*_integration_test.go` | Contratos observados no Taiga local |
| Documentação | `README.md`, `docs/guia.md`, `docs/errors.md`, spec seção 9 | Por PR, junto com o código |

**Convenção:** cada Task traz os casos de teste que precisam existir (nomes e asserções), as interfaces e os comandos de verificação. O código segue os padrões já existentes nos arquivos citados; sem stubs, TODO ou panic de implementação. As sondagens são gates: o código se ajusta ao contrato observado, e um achado que contradiga este plano para a Task e volta para decisão humana se mudar comportamento visível.

---

## US #252 — Swimlanes

**Branch:** `TG-252`. **PR:** `Implementar listagem e uso de swimlanes TG-252`.
**Descrição consultada:** listar swimlanes do projeto e definir a swimlane de uma story; validar antes no Taiga local.
**Escopo decidido (2026-10-01):** só listar e usar. Sem criar, renomear nem reordenar (criação é admin, a primeira swimlane move todas as stories, ordem sem OCC). O `--swimlane` de `story create/update` já existe desde a #246; esta US o confirma, acrescenta a listagem, o filtro e a limpeza.

### Task 1: sondar swimlanes no Taiga local

**Files:**
- Create: `internal/taiga/swimlanes_probe_integration_test.go`
- Modify: `docs/api-notes.md` (seção "Fase 3 — swimlanes (US #252)")

**Interfaces:** Consumes `testtaiga.URL/Login`, `taiga.New/Do`. Produces o contrato de `GET swimlanes?project=`, de `swimlane` na story e do filtro de listagem.

- [ ] **Step 1: escrever `TestProbeSwimlaneContract`.** Num projeto descartável próprio (`cli-test-probe-swimlanes-<sufixo>`), com `admin`:
  1. criar duas stories **antes** de qualquer swimlane; criar a swimlane `A` e conferir que as duas passaram para `A` (achado do `post_save`) e que `default_swimlane` do projeto passou a ser `A`;
  2. criar `B`; conferir `order` crescente e a ordenação de `GET swimlanes?project=` (por `order`, depois `name`);
  3. criar uma story sem `swimlane` com `B` existente e registrar onde ela cai (`null` ou a padrão);
  4. `PATCH` com `swimlane: B` e `version`; depois `swimlane: null`; registrar a resposta e a releitura;
  5. `GET userstories?project=&swimlane=<B>`: registrar se o filtro é respeitado (a #246 observou que é ignorado);
  6. `svc` (membro sem admin, por e-mail) lê `GET swimlanes?project=` (200) e recebe 403 no `POST swimlanes`;
  7. swimlane de outro projeto no `PATCH` da story: registrar o status (a #246 viu 403);
  8. registrar se o `PATCH` de `swimlane` com `version` antiga e campo disjunto é aceito (OCC por campo, como nos demais).
- [ ] **Step 2: rodar.**

Run: `go test -tags integration -run '^TestProbeSwimlaneContract$' -v ./internal/taiga`
Expected: PASS com linhas `FINDING`. Se a story criada sem swimlane cair na padrão, documentar; se o filtro `swimlane` for respeitado, ainda assim filtrar localmente (Task 2).

- [ ] **Step 3: registrar em `docs/api-notes.md`** a tabela com data, imagem e teste, no formato das seções da fase 2. Atualizar a linha "Escrita de swimlane, upload de anexo" da tabela inicial.

Run: `git diff --check`
Expected: limpo.

```bash
git add internal/taiga/swimlanes_probe_integration_test.go docs/api-notes.md
git commit -m "Validar o contrato de swimlanes no Taiga local"
```

### Task 2: `swimlane list`, `story list --swimlane` e `--clear-swimlane`

**Files:**
- Create: `internal/app/swimlanes.go`, `internal/app/swimlanes_test.go`, `internal/cli/swimlane.go`, `internal/cli/swimlane_test.go`, `internal/cli/swimlane_integration_test.go`
- Modify: `internal/cli/root.go` (registrar `swimlane`), `internal/cli/story.go` (filtro e flag), `internal/app/stories.go` (`matches`), `internal/app/story_patch.go`

**Interfaces:** Produces `(*Service).Swimlanes(ctx) ([]Object, error)` (usa `Catalog("swimlanes")`, marca `is_default` comparando com `Project["default_swimlane"]`).

- [ ] **Step 1: testes de unidade primeiro.**
  - `TestSwimlanesMarksDefault`: fake com duas swimlanes e `default_swimlane`; só a padrão sai com `is_default: true`.
  - `TestStoryListSwimlaneFiltersLocally`: o fake **ignora** o parâmetro e devolve stories de duas swimlanes; `story list --swimlane A` mostra só as de `A`. Nome, id e nome repetido (`ambiguous_name`) resolvidos por `Resolve`.
  - `TestStoryListNoSwimlane`: `--no-swimlane` lista as stories com `swimlane: null` (não usar um valor mágico como `none`, que pode ser nome real); `--swimlane` e `--no-swimlane` juntos é `usage`.
  - `TestStoryUpdateClearSwimlane`: `--clear-swimlane` gera `{"swimlane": null, "version"}`; com `--swimlane` junto é `usage`; story já sem swimlane não gera escrita.
  - `TestSwimlaneTextEscapesControls`: nome com `\x1b[31m` e U+202E sai escapado no texto.
  - Projeto sem swimlanes: `swimlane list` devolve `[]`; `--swimlane X` responde `not_found` (exit 5).
- [ ] **Step 2: implementar.** `swimlane list` com colunas `id`, `name`, `order`, `is_default`; JSON é a lista da API com `is_default` acrescentado. O filtro local roda depois do `GetAll` em `matches`, como os demais. `--clear-swimlane` entra no `Patch` e passa pelo `BuildPatch`/`Write` existentes (envia o próprio campo; OCC por campo).
- [ ] **Step 3: integração** `TestIntegrationSwimlaneListAndUse`: projeto descartável com duas swimlanes criadas pelo teste via `taiga api` com `admin`; `story create --swimlane A`, `story update --swimlane B`, `story list --swimlane B`, `--clear-swimlane`, `--no-swimlane`.
- [ ] **Step 4: documentação.** README (tabela "Project, users and catalogs"), `docs/guia.md` (seção curta "Swimlanes", dizendo que criar e reordenar ficam na interface web), spec seção 9 (`swimlane list` já consta; acrescentar `--swimlane`/`--no-swimlane` em `story list` e `--clear-swimlane`).

Run: `go test ./... && go vet ./... && go test -tags integration -run 'Swimlane' -v ./internal/...`
Expected: tudo PASS.

```bash
git add internal docs README.md
git commit -m "Implementar listagem e uso de swimlanes TG-252"
```

---

## US #251 — Anexos

**Branch:** `TG-251`. **PR:** `Implementar anexos de stories e tasks TG-251`.
**Descrição consultada:** upload multipart de arquivo local, listagem e download de anexos de stories (e tasks, se couber).
**Escopo decidido (2026-10-01):** story **e** task; `list`, `upload`, `download`; **sem limite de tamanho na CLI** (o limite é o do proxy: `413` vira `payload_too_large`). Sem editar nem apagar anexo.

Comandos (spec, seção 9):

```
taiga attachment list REF [--task]
taiga attachment upload REF FILE [--task] [--description TEXT] [--dry-run]
taiga attachment download REF ATTACHMENT_ID [--task] [--to PATH|-] [--overwrite]
```

### Task 3: sondar anexos e o caminho de download no Taiga local

**Files:**
- Create: `internal/taiga/attachments_probe_integration_test.go`
- Modify: `docs/api-notes.md` (seção "Fase 3 — anexos (US #251)"); possivelmente `compose.test.yml` (Step 3)

- [ ] **Step 1: `TestProbeAttachmentContract`** (projeto descartável próprio, story e task criadas no teste), com um `POST` multipart montado no teste (`mime/multipart`, campos `project`, `object_id`, `attached_file`, `description`):
  1. resposta 201: registrar `id`, `name`, `size`, `sha1`, `url`, `object_id`, `project`, `order`, `is_deprecated`, `from_comment`; conferir `sha1` e `size` contra os calculados localmente;
  2. nome com acento, espaço, aspas e `../x`: registrar o `name` gravado (esperado: basename sanitizado pelo storage);
  3. arquivo vazio (0 byte): registrar a resposta;
  4. `GET userstories/attachments?project=&object_id=` e `tasks/attachments?...`: só os do objeto; paginação e `x-disable-pagination`;
  5. `object_id` de story de **outro** projeto com `project` deste: registrar o 400;
  6. o mesmo arquivo duas vezes: dois anexos (sem unicidade no servidor);
  7. `svc` (membro, por e-mail) envia e lista; não membro de projeto privado recebe 403/404;
  8. registrar se a `version` da story muda depois do upload (o anexo grava histórico na story via `get_object_for_snapshot`).
- [ ] **Step 2: rodar.**

Run: `go test -tags integration -run '^TestProbeAttachmentContract$' -v ./internal/taiga`
Expected: PASS com `FINDING`.

- [ ] **Step 3: gate do download.** O `url` aponta para `<TAIGA_URL>/media/...?token=...`. O compose de teste não tem gateway: o `taiga-protected` fica no profile `full` e nada roteia `/media/`. Sondar `GET` no `url` devolvido:
  - **se o `taiga-back` servir `/media/` direto** (modo de desenvolvimento), registrar e seguir;
  - **senão**, acrescentar ao `compose.test.yml`, no profile `full`, um gateway nginx mínimo que encaminhe `/api` e `/admin` para o `taiga-back` e `/media/` para o `taiga-protected`, publicado em `127.0.0.1:8000`. Isso exige mover a porta do `taiga-back` para a rede interna. **Mudar o compose afeta o outro agente: combinar com o Cedric antes**, e manter o default (sem `full`) funcionando como hoje. Registrar o resultado de `GET` com token válido, sem token, com token expirado (`MAX_AGE` 360 s: forçar com token adulterado) e com `Authorization` presente ou não.
- [ ] **Step 4: registrar em `docs/api-notes.md`** a tabela da sondagem e o caminho do download. Atualizar a linha "Escrita de swimlane, upload de anexo".

```bash
git add internal/taiga/attachments_probe_integration_test.go docs/api-notes.md compose.test.yml
git commit -m "Validar o contrato de anexos no Taiga local"
```

### Task 4: `Upload` multipart e `Download` em streaming no cliente

**Files:**
- Create: `internal/taiga/transfer.go`, `internal/taiga/transfer_test.go`
- Modify: `internal/taiga/errors.go`, `internal/app/service.go` (interface `API`)

**Interfaces:**

```go
// Upload sends one multipart POST. Never retried. The file is streamed from r.
func (c *Client) Upload(ctx context.Context, path string, fields map[string]string,
    fileField, fileName string, r io.Reader) (*Response, error)

// Download streams GET rawURL into w. rawURL must have the client's scheme, host and port;
// no Authorization header is sent and redirects are not followed. Retried only before
// the first byte is written to w.
func (c *Client) Download(ctx context.Context, rawURL string, w io.Writer) (int64, error)
```

- [ ] **Step 1: testes com `httptest`.**
  - `TestUploadSendsMultipartOnce`: o servidor confere `Content-Type: multipart/form-data`, os campos, o arquivo byte a byte e `Authorization`; com 500 o cliente **não** repete (1 requisição).
  - `TestUploadTruncatedAnswerIsUnreadable`: 201 com corpo cortado → `*UnreadableBodyError`, nunca `NetworkError`.
  - `TestUploadConnectionRefusedIsNotSent`: conexão recusada → `NetworkError` com indicação de "não enviado" (mesmo critério de `notSent` em `app/comments.go`; mover o critério para `taiga` e reusar).
  - `TestUpload413IsPayloadTooLarge`: `413` → `ToOutput` dá `payload_too_large`, exit 2, `cause` com o corpo do proxy.
  - `TestDownloadRejectsOtherOrigin`: URL com outro host, outra porta ou outro esquema → erro `attachment_url_untrusted` sem nenhuma requisição.
  - `TestDownloadSendsNoAuthorization` e `TestDownloadDropsFragment`.
  - `TestDownloadDoesNotFollowRedirect`: 302 → `unexpected_redirect`.
  - `TestDownloadRetriesOnlyBeforeFirstByte`: 502 e depois 200 → sucesso; corpo cortado no meio → erro de rede sem nova tentativa (o chamador descarta o temporário).
- [ ] **Step 2: implementar.** O upload usa `io.Pipe` com `multipart.Writer`, sem carregar o arquivo em memória. O timeout HTTP de 30 s não serve para transferência longa: upload e download usam uma cópia do `http.Client` sem `Timeout` total, com `ResponseHeaderTimeout` de 30 s depois do envio e um prazo total vindo do contexto (`--timeout`, padrão `10m`, na Task 5). Escrita da interface `API` de `app` ganha `Upload` e `Download`; os fakes dos testes existentes ganham implementações que falham se chamadas.

Run: `go test ./internal/taiga/ ./internal/app/ && go vet ./...`
Expected: PASS.

```bash
git add internal/taiga internal/app/service.go
git commit -m "Acrescentar upload multipart e download conferido ao cliente"
```

### Task 5: serviço e comandos de anexos

**Files:**
- Create: `internal/app/attachments.go`, `internal/app/attachments_test.go`, `internal/app/tasks_ref.go` (só a resolução de task por ref, reusada na #253), `internal/cli/attachment.go`, `internal/cli/attachment_test.go`, `internal/cli/attachment_integration_test.go`
- Modify: `internal/cli/root.go`, `docs/errors.md`

**Interfaces:**

```go
func (s *Service) Task(ctx context.Context, ref string, id int64) (Object, error) // tasks/by_ref, conferindo o projeto
func (s *Service) Attachments(ctx context.Context, kind string, owner Object) ([]Object, error)
func (s *Service) Upload(ctx context.Context, kind string, owner Object, path, description string, dry bool) (any, error)
func (s *Service) DownloadAttachment(ctx context.Context, kind string, owner Object, id int64, dest string, overwrite bool) (Object, error)
```

`kind` é `story` (`userstories/attachments`) ou `task` (`tasks/attachments`).

**Regras do upload:**

1. Abre o arquivo, recusa diretório, dispositivo e link quebrado (`usage`), calcula `size` e `sha1` numa primeira passada. Não há limite de tamanho (decisão de 2026-10-01).
2. Lista os anexos do objeto. **Se já existe um com o mesmo `name` e o mesmo `sha1`, devolve esse anexo sem enviar** (idempotência pelo conteúdo, como `field create`; proposta do plano, ver "Pontos a validar"). O resultado diz `"created": false`.
3. `--dry-run`: imprime `POST <kind>/attachments`, os campos (`project`, `object_id`, `description`) e `file: {name, size, sha1}`.
4. Envia uma vez. **Pós-condição pela resposta:** `sha1`, `size` e `object_id` iguais aos enviados. Diferença → `attachment_postcondition_failed` (exit 4, anexo **gravado**, por exemplo porque o arquivo mudou entre o hash e o envio). Resposta ilegível → vale a releitura da lista: um anexo novo com o mesmo `sha1` e `name`.
5. **Sem resposta conclusiva** (rede depois de aberta a conexão, timeout ou 5xx): relê a lista e procura um id novo (fora do conjunto lido antes) com o mesmo `sha1`. Achou → sucesso. Não achou, ou a lista não pôde ser lida → `attachment_unconfirmed` (exit 1), com a recuperação "check with `taiga attachment list` before uploading again". Exit 7 só quando a conexão nem abriu.

**Regras do download:**

1. Relê o anexo (`GET <kind>/attachments/<id>`) para ter um `url` com token novo e confere que o `object_id` é o da story/task pedida (senão `not_found`, exit 5).
2. Destino: `--to PATH` (arquivo ou diretório existente), `--to -` (stdout), ou o diretório atual. O nome vindo do servidor é **não confiável**: só o basename, sem `/`, `\`, NUL, controles e bidi (trocados por `_`), e `.`/`..`/vazio viram `attachment-<id>`.
3. Nunca sobrescreve: destino existente sem `--overwrite` → `usage` antes de qualquer requisição de conteúdo.
4. Grava num temporário no mesmo diretório (`os.CreateTemp`, `0600`), calculando tamanho e `sha1` enquanto grava. Confere com o anexo: diferença → apaga o temporário e sai `attachment_download_mismatch` (exit 7; leitura, repetir é seguro). Bate → `chmod` com `0666 &^ umask` (revisão da PR #12: respeitar o umask) e renomeia; sem `--overwrite`, cria o destino com `os.Link` (falha se apareceu um arquivo no meio) e remove o temporário.
5. Com `--to -`, os bytes vão direto ao stdout; a conferência roda no fim e, se falhar, o erro sai no stderr com exit 7 (o consumidor deve descartar o que leu). O JSON de resultado não é impresso no stdout nesse modo.

**Saída:** `attachment list` mostra `id`, `name`, `size`, `sha1`, `description`, `created_date`, `owner` e `is_deprecated`. **O `url` com token não sai** nem no JSON nem no texto: ele dá acesso ao arquivo sem autenticação enquanto valer; o caminho para obter o arquivo é `attachment download`. O upload devolve o anexo, sem `url`, mais `created`.

- [ ] **Step 1: testes de unidade (fake).**
  - `TestUploadPostconditionFromAnswer` (sucesso) e `TestUploadPostconditionMismatch` (resposta com outro `sha1` → `attachment_postcondition_failed`, exit 4, `cause` com os dois hashes).
  - `TestUploadUnreadableAnswerUsesReread`, `TestUploadUnreadableAnswerAndRereadFails` (→ `write_applied`).
  - `TestUploadLostAnswerFoundInList` (sucesso), `TestUploadLostAnswerNotFound` (→ `attachment_unconfirmed`, exit 1), `TestUploadNotSentIsNetworkError` (exit 7).
  - `TestUploadSameNameAndSha1IsNoop` (nenhum POST; `created: false`); mesmo `sha1` com outro nome envia.
  - `TestUploadDryRunShowsFileMetadataOnly` (sem bytes do arquivo na saída).
  - `TestUploadRefusesDirectory`.
  - `TestDownloadSanitizesServerName` (tabela: `../../etc/passwd`, `a/b`, `\x1b]0;x\a`, U+202E, `.`, `..`, vazio).
  - `TestDownloadRefusesExistingWithoutOverwrite` (nenhum GET de conteúdo), `TestDownloadMismatchRemovesTemp`, `TestDownloadChecksObjectID`.
  - `TestTaskByRefChecksProject`.
- [ ] **Step 2: implementar** serviço e CLI; `--task` troca o `kind` e resolve a ref por `Service.Task`. `--timeout` (padrão `10m`) só nos comandos de transferência.
- [ ] **Step 3: códigos novos em `docs/errors.md`:** `payload_too_large` (2), `attachment_postcondition_failed` (4), `attachment_unconfirmed` (1), `attachment_download_mismatch` (7), `attachment_url_untrusted` (1: a URL devolvida pelo servidor não é da mesma origem; não repetir, conferir a configuração `MEDIA_URL` da instância). Atualizar o parágrafo introdutório que lista os códigos "gravado, não repetir".
- [ ] **Step 4: integração** `TestIntegrationAttachmentsStoryAndTask`: upload em story e em task, `list`, `download` com conferência de bytes, upload repetido (no-op), `--overwrite`. Com o proxy local já usado em `TestIntegrationTruncatedValuesAnswerIsDetected`: `TestIntegrationUploadTruncatedAnswer` (201 truncado → releitura e sucesso) e `TestIntegrationUploadLostAnswer` (proxy fecha a conexão depois de repassar o POST → sucesso pela lista). O download roda só se o gate da Task 3 deixou `/media/` acessível; senão o teste faz `t.Skip` com a razão, e o plano registra a pendência.
- [ ] **Step 5: documentação.** README (seção "Attachments"), `docs/guia.md` (seção "Anexos": idempotência por conteúdo, por que o `url` não aparece, `--to -`), spec seção 9 (linha de anexos com `--to`/`--overwrite`/`--description`).

Run: `go test ./... && go vet ./... && go test -tags integration -run 'Attachment|Upload' -v ./internal/...`
Expected: PASS (download pulado só se o gate o exigir, com a razão registrada).

```bash
git add internal docs README.md
git commit -m "Implementar anexos de stories e tasks TG-251"
```

---

## US #253 — Paridade com o MCP do Taiga

**Descrição consultada:** projetos, usuários, milestones, épicos (listar, obter, vincular story), tasks (listar, obter, criar, atualizar, fechar) e diagnóstico, cobrindo tudo o que o MCP oferece. Decisão de 2026-09-30: o vínculo story↔épico veio da #246 para cá. Também veio para cá o `task field`.

**Divisão decidida (2026-10-01): três PRs sequenciais, todos com `TG-253` no título.** A story só avança no fluxo quando o terceiro PR for mergeado.

| PR | Branch | Título |
|---|---|---|
| 253-1 | `TG-253-leitura` | `Implementar leitura de projetos, usuários, milestones e épicos TG-253` |
| 253-2 | `TG-253-epicos` | `Implementar vínculo e troca de épico de stories TG-253` |
| 253-3 | `TG-253-tasks` | `Implementar comandos de tasks TG-253` |

Paridade com o MCP, ferramenta por ferramenta:

| MCP | CLI | PR |
|---|---|---|
| `taiga_projects_list` / `get` | `taiga project list [--search]` / `taiga project get [SLUG\|ID]` | 253-1 |
| `taiga_users_list` | `taiga user list [--search]` | 253-1 |
| `taiga_milestones_list` | `taiga milestone list [--search] [--closed[=false]]` | 253-1 |
| `taiga_epics_list` / `get` | `taiga epic list [--search] [--closed[=false]]` / `taiga epic get REF` | 253-1 |
| `taiga_diagnostics` | `taiga auth status --diagnose` com o check `project` | 253-1 |
| `taiga_epics_add_user_story` | `taiga epic link EPIC STORY`; `--epic` em `story create/update`; troca com `--replace-epic`/`--replace` + `--confirm-delete` | 253-2 |
| `taiga_tasks_list/get/create/update` | `taiga task list/get/create/update` | 253-3 |
| `taiga_tasks_archive_or_close` | `taiga task close REF [--status S]` (sem tag de arquivo) | 253-3 |
| `taiga_stories_*` | já cobertos na fase 2 | — |
| — (além do MCP) | `taiga task field list/set`, `taiga task comment/comments`, bloqueio e `due_date` em task | 253-3 |

### PR 253-1 — leitura e diagnóstico

#### Task 6: sondar projetos, usuários, milestones e épicos

**Files:**
- Create: `internal/taiga/catalogs_probe_integration_test.go`
- Modify: `docs/api-notes.md` (seção "Fase 3 — catálogos e épicos (US #253)")

- [ ] **Step 1: `TestProbeCatalogs`** (projetos descartáveis próprios; `svc` convidado por e-mail em um deles):
  1. `GET projects?member=<id de users/me>` com `svc`: só os projetos de que é membro; com `admin` (superusuário): registrar se aparecem projetos sem membership. Na leitura da redação, `admin` com `member=1` devolveu `[]`: confirmar o id certo antes de concluir;
  2. `x-disable-pagination` em `projects`, `milestones`, `epics`;
  3. `GET milestones?project=&closed=true|false`: filtro respeitado? Tamanho do objeto (traz `user_stories` inteiras): registrar para decidir as colunas;
  4. `GET epics?project=` e `GET epics/by_ref?project=&ref=`: formato; projeto com o módulo de épicos **desligado** (`is_epics_activated: false`): registrar status e corpo;
  5. `GET epics/<id>/related_userstories`: formato (`user_story`, `epic`, `order`);
  6. filtros de épico: `status__is_closed`, `q`; registrar os ignorados.
- [ ] **Step 2:** `go test -tags integration -run '^TestProbeCatalogs$' -v ./internal/taiga` → PASS com `FINDING`; registrar em `docs/api-notes.md`.

```bash
git commit -m "Validar catálogos de projetos, milestones e épicos no Taiga local"
```

#### Task 7: `project list/get`, `user list`, `milestone list`, `epic list/get`

**Files:**
- Create: `internal/app/catalogs.go`, `internal/app/catalogs_test.go`, `internal/cli/user.go`, `internal/cli/milestone.go`, `internal/cli/epic.go` e testes
- Modify: `internal/cli/project.go` (hoje só `plan`/`apply`), `internal/app/stories.go` (`Epic` passa a reusar o catálogo), `internal/cli/root.go`

- [ ] **Step 1: testes primeiro.**
  - `project list` não exige projeto no contexto; `project get` sem argumento usa o projeto do contexto e mostra a origem (flag, env, `.taiga.toml`, config), como `auth status`.
  - `user list` = membros (`memberships` ∩ `users?project=`), com `username`, `full_name`, `id`, `is_admin` (da membership), `role_name`. `--search` filtra localmente por `username` e `full_name`, sem diferenciar maiúsculas e sem normalizar acentos (não prometer o que não se testa).
  - `milestone list`: `id`, `name`, `slug`, `estimated_start`, `estimated_finish`, `closed`; `--closed` filtra localmente.
  - `epic list`: `ref`, `subject`, `status`, `is_closed`, `color`, `url` (`<url>/project/<slug>/epic/<ref>`); `epic get REF` com `description`, `tags` normalizadas, `user_stories` (refs dos vínculos).
  - Texto com controles e bidi escapado em todos.
  - Projeto com épicos desligados: erro claro conforme a sondagem (provável `not_found` com recuperação "the epics module is disabled in this project").
- [ ] **Step 2: implementar** reusando `Catalog`, `Resolve` e `renderKeys`.
- [ ] **Step 3: integração** `TestIntegrationCatalogCommands` em projeto descartável.

Run: `go test ./... && go test -tags integration -run 'Catalog' -v ./internal/...`
Expected: PASS.

```bash
git commit -m "Implementar leitura de projetos, usuários, milestones e épicos"
```

#### Task 8: check `project` no `auth status --diagnose`

**Files:**
- Modify: `internal/auth/diagnose.go`, `internal/auth/diagnose_test.go`, `internal/cli/auth.go`

**Escopo decidido (2026-10-01):** check `project` dentro do diagnóstico existente, sem comando novo.

- [ ] **Step 1: testes.** O check `project` reporta, como os demais (`ok`/`skipped`/`failed` e motivo):
  - `skipped` sem projeto no contexto, ou quando a autenticação falhou antes;
  - projeto resolvido: `id`, `slug`, origem;
  - membro ou não (`i_am_member`), admin ou não (`i_am_admin`);
  - presença de `view_us`, `modify_us`, `add_us`, `comment_us`, `view_tasks`, `add_task`, `modify_task`, `modify_epic`, `admin_project_values` em `my_permissions`, listando as ausentes;
  - módulos: `is_epics_activated`, `is_kanban_activated`, `is_backlog_activated`; número de swimlanes.
  - `failed` se o projeto não existe ou a conta não o vê (`not_found`/`forbidden`), com a recuperação adequada.
  O diagnóstico continua só lendo.
- [ ] **Step 2: implementar; documentar** no README ("auth status --diagnose") e no guia.
- [ ] **Step 3: docs do PR 253-1:** README, guia, spec seção 9 (tabela "Projeto, usuários e catálogos").

Run: `go test ./... && go vet ./...`
Expected: PASS.

```bash
git commit -m "Acrescentar o check de projeto ao diagnóstico TG-253"
```

### PR 253-2 — vínculo e troca de épico

**Decisão (2026-10-01):** "criar e trocar". `--epic E` **acrescenta** o vínculo. A troca exige `--replace-epic E` (em `story update`) ou `taiga epic link E STORY --replace`, sempre com `--confirm-delete`: **cria o vínculo novo e só então apaga os antigos**, para a story nunca ficar sem épico.

Comandos:

```
taiga epic link EPIC_REF STORY_REF [--replace --confirm-delete] [--dry-run]
taiga story create ... [--epic EPIC_REF]
taiga story update REF [--epic EPIC_REF | --replace-epic EPIC_REF --confirm-delete]
```

#### Task 9: sondar o vínculo de épico

**Files:**
- Modify: `internal/taiga/catalogs_probe_integration_test.go` (ou `epic_links_probe_integration_test.go`), `docs/api-notes.md`

- [ ] **Step 1: `TestProbeEpicLinkContract`** (projeto descartável com épicos ativados):
  1. `POST epics/<id>/related_userstories {epic, user_story}` → 201, formato da resposta; `version` da story inalterada e `epics` da story relida inclui o épico;
  2. o mesmo `POST` de novo: status e corpo (esperado 400 pelo `unique_together`; se vier 500, registrar e tratar como incerto);
  3. segundo épico na mesma story: aceito, `epics` com os dois;
  4. `DELETE epics/<id>/related_userstories/<story id>` → 204; de novo → 404;
  5. épico de outro projeto: status;
  6. `svc` sem `modify_epic` (role sem a permissão, se o seed permitir configurar) → 403; com a permissão padrão de membro, registrar se `svc` tem `modify_epic`;
  7. `GET userstories/<id>` depois de cada passo: a lista `epics` reflete o vínculo imediatamente?
- [ ] **Step 2:** rodar, registrar em `docs/api-notes.md` (substitui o "*(manual)*" da seção "Escrita de vínculos" da #246 pelos resultados do teste).

```bash
git commit -m "Validar o vínculo de épico no Taiga local"
```

#### Task 10: acrescentar e trocar o vínculo

**Files:**
- Create: `internal/app/epic_links.go`, `internal/app/epic_links_test.go`, `internal/cli/epic_link_integration_test.go`
- Modify: `internal/cli/epic.go`, `internal/cli/story.go` (remover `errEpicLink`; flags novas), `docs/errors.md` (tirar `--epic` de `unsupported_operation`)

**Interfaces:**

```go
// LinkEpic makes epic one of the story's epics; with replace, the only one.
func (s *Service) LinkEpic(ctx context.Context, story, epic Object, replace, dry bool) (any, error)
```

**Algoritmo:**

1. `before` = `epics` da story lida. Se `epic` já está e (`!replace` ou é o único) → no-op, `changed: false`.
2. `--dry-run`: imprime o `POST` (se faltar o vínculo) e, com `replace`, um `DELETE epics/<old>/related_userstories/<story id>` por épico antigo. Sem `--confirm-delete` com `replace`, o `--dry-run` também recusa (`delete_not_confirmed`, exit 2), para o script descobrir a falta da flag antes de rodar de verdade.
3. **Releitura antes:** relê a story; se `epics` mudou desde `before` → `version_conflict` (exit 4), nada enviado (a story não tem `version` no vínculo; a conferência é da CLI, como em responsáveis).
4. `POST` do vínculo, uma vez. 400 de duplicata → relê; se o vínculo está lá, segue. Resposta ilegível ou sem resposta conclusiva → relê a story: vínculo presente, segue; ausente → `epic_link_unconfirmed` (exit 1; recuperação: "linking is idempotent: run the same command again", porque a unicidade do servidor impede duplicata).
5. Com `replace`: relê de novo; se apareceu um épico que não estava em `before` nem é o novo → para sem apagar, `epic_links_postcondition_failed` (exit 4, vínculo novo **gravado**). Senão, `DELETE` de cada antigo, uma vez cada. 404 = já removido, segue. Outra falha → `epic_replace_incomplete` (exit 1), com o estado em `cause` (`linked`, `removed`, `remaining`); recuperação: "run the same command again: it re-reads and finishes the replacement".
6. **Pós-condição:** relê e exige `epic` presente; com `replace`, exatamente `{epic}`. Senão `epic_links_postcondition_failed` (exit 4, estado em `cause`).

**`story create --epic E`:** cria a story (fluxo atual) e então chama `LinkEpic`. Se o vínculo falhar, o erro **inclui a ref da story criada** e a recuperação "the story was created as #<ref>; run `taiga epic link E <ref>`", **nunca** "repetir o comando" (repetir criaria outra story). Exit do erro do vínculo, mas com `code` `story_created_link_failed` (exit 1) para scripts distinguirem. `--replace-epic` não existe no `create`.

**`story update` com `--epic`/`--replace-epic` e outros campos:** primeiro o `PATCH` dos campos (fluxo atual), depois o vínculo. Falha no vínculo depois do `PATCH` aplicado → mesmo erro do vínculo, com `cause` dizendo que os campos **foram gravados**.

- [ ] **Step 1: testes de unidade** (fake com `epics` controlado por passo):
  - `TestLinkEpicAddsOnce`, `TestLinkEpicAlreadyLinkedIsNoop`, `TestLinkEpicDuplicate400IsSuccess`;
  - `TestLinkEpicLostAnswerLinked` / `TestLinkEpicLostAnswerNotLinked` (`epic_link_unconfirmed`, exit 1, nunca 7);
  - `TestLinkEpicRefusesChangedLinksBeforePost` (nada enviado);
  - `TestReplaceEpicPostsBeforeDelete` (ordem das requisições no fake);
  - `TestReplaceEpicRequiresConfirmDelete` (também no dry-run);
  - `TestReplaceEpicStopsOnNewForeignLink` (nenhum DELETE);
  - `TestReplaceEpicDelete404IsDone`, `TestReplaceEpicDeleteFailsIsIncomplete` (exit 1, `cause` com o estado);
  - `TestReplaceEpicRerunConverges` (segundo run com os dois épicos só apaga o antigo);
  - `TestStoryCreateEpicLinkFailureNamesTheStory`;
  - `TestEpicFromOtherProjectIsNotFound` (o `Epic` existente já resolve pelo projeto).
- [ ] **Step 2: implementar.** `epic link` e as flags da story usam `LinkEpic`. Atualizar `docs/errors.md`: `delete_not_confirmed` passa a valer também para `--replace-epic`/`--replace`; códigos novos `epic_link_unconfirmed` (1), `epic_replace_incomplete` (1), `epic_links_postcondition_failed` (4), `story_created_link_failed` (1). Tirar `--epic` do `unsupported_operation`.
- [ ] **Step 3: spec.** Seção 2 ("Fora do MVP"): "operações de exclusão em comandos curados, **exceto** a remoção do vínculo story↔épico na troca, com `--confirm-delete` (decisão de 2026-10-01)". Seção 9: linha de `epic link` com `--replace`.
- [ ] **Step 4: integração** `TestIntegrationEpicLinkAndReplace`: link, link repetido (no-op), `--epic` no create, `--replace-epic` sem e com `--confirm-delete`, rerun. Com o proxy: `TestIntegrationReplaceEpicDeleteLost` (proxy derruba a resposta do DELETE → `epic_replace_incomplete`; rerun converge).
- [ ] **Step 5: docs.** README e guia: tirar a frase "vincular a épico não é suportado"; explicar acrescentar × trocar e que a troca cria antes de apagar.

Run: `go test ./... && go test -tags integration -run 'Epic' -v ./internal/...`
Expected: PASS.

```bash
git commit -m "Implementar vínculo e troca de épico de stories TG-253"
```

### PR 253-3 — tasks

**Escopo decidido (2026-10-01):** paridade com o MCP mais `task field`, **bloqueio**, **comentários** e **due date**. Sem `--milestone` em task (a task herda da story).

```
taiga task list [--story REF] [--status S] [--assignee USER|me] [--tag T]... [--search TEXT] [--closed[=false]]
taiga task get REF | --id ID
taiga task create --story REF --subject S [--description-file F|-] [--status S] [--tag T]... [--assignee USER] [--due-date AAAA-MM-DD] [--dry-run]
taiga task update REF [--subject S] [--description-file F|-] [--append-description TEXT] [--status S]
                      [--tag T]... [--add-tag T]... [--remove-tag T]... [--assignee USER|--clear-assignee]
                      [--block NOTE|--unblock] [--due-date AAAA-MM-DD|--clear-due-date] [--dry-run] [--force-version]
taiga task close REF [--status S] [--dry-run] [--force-version]
taiga task field list REF
taiga task field set REF ["Nome=valor"]... [--unset NOME]... [--dry-run] [--force-version]
taiga task comment REF --body TEXT|--body-file F|-
taiga task comments REF [--include-system]
```

#### Task 11: sondar tasks

**Files:**
- Create: `internal/taiga/tasks_probe_integration_test.go`
- Modify: `docs/api-notes.md` (seção "Fase 3 — tasks (US #253)")

- [ ] **Step 1: `TestProbeTaskContract`** (projeto descartável; `svc` por e-mail):
  1. `POST tasks {project, user_story, subject, status, tags, assigned_to, due_date}` → 201; `ref` é da mesma sequência das stories? (registrar);
  2. `GET tasks?project=&user_story=&status=&assigned_to=&tags=&q=&status__is_closed=`: quais filtros são respeitados (filtrar localmente os ignorados);
  3. **OCC de `assigned_to`:** outra escrita troca `assigned_to`; um `PATCH` com `version` antiga mandando `assigned_to` → conflito? E mandando só `subject` → aceito? (decide se a task precisa da releitura/pós-condição da #247; ver Review Focus 5);
  4. bloqueio: `is_blocked`/`blocked_note` com o mesmo comportamento da story (nota descartada quando desbloqueada; `is_blocked: false` limpa a nota);
  5. `due_date`: formato aceito, `null` limpa; `due_date_reason`;
  6. comentário: `PATCH tasks/<id> {comment, version}` com `version` antiga aceito? `GET history/task/<id>?type=comment`: mesmo formato e ordem de `history/userstory`;
  7. `task-statuses`: `is_closed`; mais de um status fechado no template padrão?;
  8. task de outro projeto por `tasks/by_ref` com o `project` deste: 404.
- [ ] **Step 2:** rodar e registrar.

```bash
git commit -m "Validar o contrato de tasks no Taiga local"
```

#### Task 12: `task list/get/create/update/close`

**Files:**
- Create: `internal/app/tasks.go`, `internal/app/tasks_test.go`, `internal/cli/task.go`, `internal/cli/task_test.go`, `internal/cli/task_integration_test.go`
- Modify: `internal/app/story_patch.go` (generalizar `Patch`/`BuildPatch` para os campos comuns), `internal/cli/root.go`

- [ ] **Step 1: testes primeiro.**
  - Resolução: `task get REF` por `tasks/by_ref`; `--id` conferido contra o projeto; `--story REF` resolvido por `Service.Story`.
  - `create` exige `--story` (paridade com o MCP; task solta fica para `taiga api`). Sem `version` no POST. Resposta ilegível → `write_applied` (não há como reler sem o id). Sem resposta conclusiva (rede depois de aberta a conexão, timeout ou 5xx), procura por `subject` + `user_story` + `owner` = `users/me` criada depois da leitura inicial; achou uma → sucesso; nenhuma ou várias → `task_create_unconfirmed` (exit 1; "check `taiga task list --story REF` before creating again").
  - `update`: tags com `MergeNames` (minúsculas), `--append-description`, status por nome, `--assignee` exige membro, `--clear-assignee`, `--block` exige nota e envia o par `is_blocked`+`blocked_note`, `--unblock` envia o par, `--due-date` valida `AAAA-MM-DD`, `--clear-due-date` envia `null`. Toda flag envia o campo de que depende (OCC por campo).
  - Se a Task 11 mostrar que `assigned_to` **não** é protegido pelo OCC na task: aplicar a releitura antes e a pós-condição da #247 (`assignees_postcondition_failed`); se for protegido, só a repetição única de `WriteVersionedFrom`. Escrever os dois testes e manter o que corresponder ao achado.
  - `close` como `story close`: um status fechado; vários sem `--status` → `ambiguous_name`; nunca acrescenta tag.
  - `list --story` filtra também localmente pelo `user_story`.
  - Texto escapado; `url` = `<url>/project/<slug>/task/<ref>`.
- [ ] **Step 2: implementar** generalizando o código de story por `kind`, sem duplicar os merges.
- [ ] **Step 3: integração** `TestIntegrationTaskLifecycle`.

Run: `go test ./... && go test -tags integration -run 'Task' -v ./internal/...`
Expected: PASS.

```bash
git commit -m "Implementar list, get, create, update e close de tasks"
```

#### Task 13: `task field` e `task comment/comments`

**Files:**
- Create: `internal/cli/task_fields.go`, `internal/cli/task_comments.go` e testes
- Modify: `internal/app/comments.go` (`historyPath` e `Comment`/`Comments` por `kind`), `internal/app/field_values.go` (já aceita `task`), `docs/errors.md` (os códigos de valores e comentário passam a citar task)

- [ ] **Step 1: testes.** `task field set` reusa `SetFieldValues("task", ...)`: mesma `version` própria, mesma pós-condição (`field_values_postcondition_failed`), mesmo `--unset`. `task comment` reusa o fluxo de `comment_unconfirmed` com `history/task/<id>` e `PATCH tasks/<id>`. A regra `SystemComment` vale igual (o modelo "This issue has been mentioned…" já está na lista; conferir se a integração GitLab menciona task com outro texto e registrar como pendente se não houver evidência).
- [ ] **Step 2: implementar.**
- [ ] **Step 3: integração** `TestIntegrationTaskFieldsAndComments`, com o caso de resposta truncada via proxy para o `PATCH` de valores de task.

Run: `go test ./... && go test -tags integration -run 'TaskField|TaskComment' -v ./internal/...`
Expected: PASS.

```bash
git commit -m "Implementar campos e comentários de tasks"
```

#### Task 14: documentação e gate do PR 253-3

- [ ] README (seção "Tasks"), `docs/guia.md` (seção "Tasks", com a diferença do `close` em relação ao MCP), `docs/errors.md`, spec seção 9 (tabela "Épicos e tasks" com as flags finais) e seção 11 (marcar a fase 3).
- [ ] Rodar a suíte inteira num Taiga recém-criado, em janela combinada:

Run: `docker compose -f compose.test.yml down -v && docker compose -f compose.test.yml up -d && scripts/taiga-seed && go test ./... && go test -tags integration ./...`
Expected: PASS em tudo, sem depender de dado anterior.

```bash
git commit -m "Implementar comandos de tasks TG-253"
```

---

## Fase 4 (#254) — registro de escopo

Não detalhado aqui; plano próprio depois da fase 3. Escopo decidido (2026-10-01):

- `skills/taiga-cli/SKILL.md` (PT-BR, instalável pelo skills CLI): uso por agentes, recuperação de erros (sobretudo os códigos "gravado, não repetir"), keyring headless.
- Migração da `basis-ci-gitlab`: trocar `taiga-env.sh`, `configurar-taiga-projeto.sh` e `references/taiga-mcp.md` pela CLI.
- `docs/examples/taiga-project.toml` vira o arquivo canônico do fluxo Basis. **Na própria #254**, conferir nomes, cores e campos contra o `configurar-taiga-projeto.sh` atual e levar as diferenças ao Cedric antes de publicar.
- Desligar o MCP das configs dos agentes não entra na #254.

## Pontos a validar e decisões humanas

### Decisões tomadas (com origem)

| Data | Decisão | Origem |
|---|---|---|
| 2026-09-30 | Vínculo de épico adiado da #246 para a #253 (opção a) | Cedric, via Assistente Global (registrado na #253) |
| 2026-09-30 | "Mitigar e documentar" para corridas sem OCC (responsáveis) | Cedric, #247 |
| 2026-10-01 | "Aceitar com conferência" na reordenação de status | Cedric, #249 |
| 2026-10-01 | Limpeza de checkbox/date (`--unset`) | Cedric; entregue na #260 |
| 2026-10-01 | **Épico: criar e trocar.** `--epic` acrescenta; a troca exige `--replace-epic`/`--replace` + `--confirm-delete` e faz `POST` do novo **antes** do `DELETE` dos antigos. É a única exceção à regra "nenhum DELETE em comando curado" | Cedric, nesta conversa de planejamento (encaminhada pelo Assistente Global) |
| 2026-10-01 | **Anexos:** story e task; `list`, `upload`, `download`; sem limite de tamanho na CLI (`413` → `payload_too_large`) | Cedric, nesta conversa |
| 2026-10-02 | **Limite de upload do nginx da Basis:** `client_max_body_size 50M`. A CLI não impõe limite próprio; o `413` vira `payload_too_large` | Cedric, via Assistente Global (pacote da #251) |
| 2026-10-01 | **Swimlanes:** só listar e usar; sem criar, renomear nem reordenar | Cedric, nesta conversa |
| 2026-10-01 | **Diagnóstico:** check `project` no `auth status --diagnose`, sem comando novo | Cedric, nesta conversa |
| 2026-10-01 | **PRs:** ordem #252 → #251 → #253; a #253 em três PRs (`TG-253-leitura`, `TG-253-epicos`, `TG-253-tasks`) | Cedric, nesta conversa |
| 2026-10-01 | **Tasks:** paridade do MCP + `task field` + bloqueio + comentários + due date; sem milestone | Cedric, nesta conversa |
| 2026-10-01 | **Fase 4:** skill + migração; o TOML de exemplo vira canônico após conferência com o script atual, aprovada pelo Cedric na própria #254 | Cedric, nesta conversa |

### Propostas do plano (aprovadas pelo Cedric na revisão do plano, 2026-10-01)

1. **Upload idempotente por conteúdo:** mesmo `name` e mesmo `sha1` já anexados → devolve o existente sem enviar. Alternativa: sempre enviar (o Taiga aceita duplicatas).
2. **`url` assinado fora da saída** de `attachment list`/`upload` (dá acesso sem autenticação por alguns minutos). Alternativa: incluir no JSON.
3. **`--clear-swimlane`** e **`--no-swimlane`** em story (#252), no padrão de `--clear-owner-assignee`.
4. **`task create` exige `--story`**; task sem story só por `taiga api`.
5. **`story create --epic` com falha no vínculo** sai com `story_created_link_failed` (exit 1), nomeando a story criada.
6. **`user list --search`** sem normalização de acento.

### Gates e pendências

- **Download no Taiga local (Task 3, Step 3):** o compose de teste não roteia `/media/`. Pode ser preciso um gateway nginx no profile `full`, o que muda o `compose.test.yml` usado também por outro agente: **combinar com o Cedric antes**. Sem isso, o teste de integração do download fica pulado e o download é validado só pela unidade e por um smoke manual de leitura no Taiga da Basis.
- **OCC de `assigned_to` em task (Task 11):** decide se a task precisa da releitura e pós-condição da #247.
- **Texto da integração GitLab para task:** sem evidência; se aparecer outro modelo, entra em `SystemComment`.
- **`modify_epic` da conta de serviço na Basis:** se faltar, `epic link` dá `forbidden` (exit 6); o check `project` do diagnóstico mostra isso antes.
- **Branch do plano × ordem das US:** este plano foi commitado na `TG-251`, mas a primeira US a implementar é a #252. Antes de criar a `TG-252`, o plano precisa chegar à `main` (PR próprio de documentação ou cherry-pick, a critério do Cedric); a `TG-251` da implementação nasce depois da `main` com a #252.
- **Janela do Taiga local recém-criado** antes de cada PR: combinar com o Cedric, porque outro agente pode estar usando o compose.
