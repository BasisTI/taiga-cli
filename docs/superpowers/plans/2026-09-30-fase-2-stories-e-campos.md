# taiga-cli — Fase 2 (stories e campos) — plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans para implementar este plano tarefa por tarefa. Delegação depende da escolha do humano. Steps usam checkboxes (`- [ ]`) para acompanhamento.

**Goal:** Entregar stories, responsáveis, bloqueio, definições e valores de campos customizados, comentários e configuração declarativa de status/campos, com concorrência otimista, dry-run e saída estável.

**Architecture:** `internal/cli` faz parsing e renderização; `internal/app` resolve o projeto e os nomes e calcula alterações; `internal/taiga` preserva HTTP, paginação e concorrência. Cada comando cria um serviço por execução, com catálogos em memória. A autenticação entra exclusivamente por `App.client(ctx, rc)` e `taiga.TokenSource`; não acessar senha, Store ou Resolver diretamente.

**Tech Stack:** Go 1.27.0; cobra; go-toml/v2; encoding/json; httptest; Taiga local 6.7.3 para integração futura.

**Spec:** `docs/superpowers/specs/2026-09-30-taiga-cli-design.md`, seções 6, 7, 9–12. Base: `docs/superpowers/plans/2026-09-30-fase-1-fundacao-nucleo-autenticacao.md`, Tasks 9–13 e “Depois da fase 1”.

**Fontes lidas:** código em `fc51fc3203fbe0896a32a6ece57fee730e57f34c`; descrições de #246 (id 6808), #247 (6809), #248 (6810), #250 (6812), #249 (6811), consultadas somente em leitura no projeto 37. ai-memory `perso/vault-assistente`: `decisions/taiga-cli-substitui-mcp-do-taiga.md`, `decisions/taiga-cli-autenticacao-cache-de-sessao-e-keyring-headless.md`, `notes/taiga-mcp-funcionalidades-e-limitacoes.md`. A spec prevalece sobre divergências históricas de caminho do cache e idioma das flags. A [documentação oficial da API](https://docs.taiga.io/api.html) serve como catálogo de hipóteses, não como evidência de comportamento da instalação 6.7.

## Global Constraints

- Módulo `github.com/BasisTI/taiga-cli`; binário `taiga`; Go `1.27.0`; Apache 2.0. Sem novas dependências.
- Comandos, flags, mensagens, códigos e README em inglês; `docs/` em PT-BR.
- Nenhum comando curado faz DELETE. Close muda apenas status fechado; não arquiva e não acrescenta tag de arquivo.
- Toda escrita oferece `--dry-run`; toda alteração de recurso existente usa sua `version` observada. POST de criação não tem versão anterior: não inventar `version: 0`. Registrar na sondagem se aceita/exige versão inicial. Reordenação sem OCC é um bloqueio de implementação, descrito na Task 9.
- Reusar `Client.GetAll`, `PrepareVersioned`, `WriteVersioned`, `RunContext`, `output.DetectMode/WriteJSON/WriteFields`, `taiga.ToOutput`. A Task 2 estende o núcleo sem quebrar assinaturas existentes.
- `cli → app → taiga`; `app` depende de interface estreita. Não depender de símbolos da #244 além do contrato da fase 1: `App.TokenSource func(context.Context, *RunContext) (taiga.TokenSource, error)` e `TokenSource.Token(context.Context) (taiga.Token, error)`.
- Retry HTTP apenas para GET (até duas novas tentativas); PATCH só pode repetir uma vez após conflito explícito de versão. Nunca repetir POST ou comentário depois de timeout/5xx.
- Exit 0 OK, 1 inesperado, 2 uso, 3 autenticação, 4 conflito, 5 não encontrado, 6 permissão, 7 rede/servidor. Erros passam por `taiga.ToOutput`, sem token/senha na saída.
- Preservar números com `json.Number`, chaves desconhecidas e tags vindas como `[nome, cor]`; não concatenar JSON. Arrays vazios saem como `[]`.
- Projeto: flag > env > `.taiga.toml` > config, via RunContext. Ref nunca é id; ref exige projeto, e id explícito também é conferido contra o projeto selecionado.
- Branch `TG-<ref>` e um PR por US, squash para main. Executar sequencialmente #246 → #247 → #248 → #250 → #249; cada próxima branch nasce da main contendo a anterior.
- Este documento não autoriza push, PR, merge ou mudança de status. Durante sua redação: só este arquivo e commit local na TG-246; não tocar `../taiga-cli`, não iniciar `compose.test.yml`, não alterar Taiga real.
- Na execução futura, integração usa `testtaiga.URL()` (loopback obrigatório), projeto descartável e credenciais de teste. Só iniciar/usar compose após terminar a execução concorrente da #244 e combinar a posse das portas. Não limpar fixtures por DELETE: descartar o ambiente de teste em janela própria.

## Review Focus

1. **Leitura para merge seguida de releitura de versão:** impedir que uma mudança em tags/assigned_users/attributes_values seja sobrescrita com versão nova. Testes Task 2, 4 e 6.
2. **Ref/id e catálogos de outro projeto; nomes repetidos:** rejeitar antes de escrever; não escolher o primeiro resultado nem procurar globalmente. Task 3.
3. **Texto com aspas, acentos, controles e valor vazio explícito:** preservar conteúdo de arquivo/stdin e distinguir flag ausente de vazia. Tasks 3, 6 e 7.
4. **Histórico com comentário humano, integração e diff sem comentário:** conservar humanos e desconhecidos; ocultar somente integração identificada, incluir tudo com flag. Task 7.
5. **Apply parcial, arquivo cíclico ou divergência de catálogo:** validar tudo antes da primeira escrita, preservar itens extras e retomar sem duplicatas; sem falsa promessa de transação. Tasks 8–9.

## Mapa de arquivos e dependências

| Área | Arquivos | Responsabilidade |
|---|---|---|
| Núcleo | `internal/taiga/versioned.go` | Escrita ancorada à leitura usada no merge |
| Aplicação | `internal/app/service.go`, `stories.go`, `story_patch.go` | Contexto, resolução, stories e patches |
| Responsáveis | `internal/app/assignees.go` | Set merge e bloqueio |
| Campos | `internal/app/fields.go`, `field_values.go` | Definições e dicionário versionado |
| Comentários | `internal/app/comments.go` | Publicação e leitura do histórico |
| Projeto declarativo | `internal/app/project_spec.go`, `project_plan.go`, `project_apply.go` | TOML, ações determinísticas, execução |
| CLI | `internal/cli/curated.go`, `story.go`, `story_fields.go`, `field.go`, `comments.go`, `project.go`, `status.go` | Flags, roteamento e saída |
| Evidência | `docs/api-notes.md`, testes `*_integration_test.go` | Contratos locais; sem afirmar sondagem não executada |

**Convenção dos blocos:** testes e algoritmos abaixo são código completo para a unidade indicada, sem reticências. Os blocos acrescentados ao mesmo arquivo compartilham os imports indicados; integrar os blocos sem duplicar package/import. Não deixar stubs, TODO ou panic de implementação. As sondagens são gates: ajustar o adapter ao contrato observado, sem transformar hipótese em fato.

## US #246 — Stories

**Branch:** `TG-246`. **PR:** `Implementar os comandos de stories TG-246`.
**Descrição consultada:** listar por status/ref/responsável/épico/tags, sem paginação manual; obter por ref/id com URL; criar; atualizar status, épico, sprint e tags normalizando os pares da API.
**Entregas:** Tasks 1–3. Preparar interfaces para #247/#248/#250, sem acrescentar seus comandos antes das respectivas US.

### Task 1: validar `userstories/by_ref` no Taiga local e registrar o contrato

**Files:**
- Create: `internal/taiga/stories_probe_integration_test.go`
- Create ou Modify: `docs/api-notes.md` (preservar sondagens da #244)

**Interfaces:** Consumes `testtaiga.URL/Login`, `taiga.New/Do`; Produces fixture do by_ref (id, ref, project, version), endpoint e parâmetros confirmados.

- [ ] **Step 1: escrever a sondagem, antes de código curado.** Arquivo completo:

```go
//go:build integration

package taiga

import (
    "context"
    "encoding/json"
    "fmt"
    "net/url"
    "testing"

    "github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func TestProbeStoryByRef(t *testing.T) {
    base := testtaiga.URL() // gate loopback antes de qualquer login/escrita
    token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
    c := New(base, StaticToken{Type: "Bearer", Value: token}, WithRetryWait(0))
    ctx := context.Background()
    p, err := c.Do(ctx, Request{Method: "GET", Path: "projects/by_slug",
        Query: url.Values{"slug": {testtaiga.ProjectSlug}}})
    if err != nil { t.Fatal(err) }
    var project struct { ID int64 `json:"id"` }
    if err := json.Unmarshal(p.Body, &project); err != nil || project.ID <= 0 {
        t.Fatalf("project decode: %v", err)
    }
    created, err := c.Do(ctx, Request{Method: "POST", Path: "userstories",
        Body: map[string]any{"project": project.ID, "subject": "phase-2 by-ref probe"}})
    if err != nil { t.Fatal(err) }
    var story struct { ID, Ref, Project, Version int64 }
    if err := json.Unmarshal(created.Body, &story); err != nil || story.ID <= 0 {
        t.Fatalf("story decode: %v", err)
    }
    q := url.Values{"project": {fmt.Sprint(project.ID)}, "ref": {fmt.Sprint(story.Ref)}}
    got, err := c.Do(ctx, Request{Method: "GET", Path: "userstories/by_ref", Query: q})
    if err != nil { t.Fatalf("hypothesis userstories/by_ref failed: %v", err) }
    var resolved struct { ID, Ref, Project, Version int64 }
    if err := json.Unmarshal(got.Body, &resolved); err != nil { t.Fatal(err) }
    if resolved.ID != story.ID || resolved.Ref != story.Ref || resolved.Project != project.ID {
        t.Fatalf("resolved=%+v created=%+v", resolved, story)
    }
    q.Set("ref", "9223372036854775807")
    _, err = c.Do(ctx, Request{Method: "GET", Path: "userstories/by_ref", Query: q})
    if err == nil { t.Fatal("missing ref unexpectedly resolved") }
    t.Logf("FINDING by_ref returns story object; version=%d; missing-ref=%v", resolved.Version, err)
}
```

- [ ] **Step 2: executar somente na janela local futura.**

Run: `go test -tags integration -run '^TestProbeStoryByRef$' -v ./internal/taiga`
Expected: PASS com FINDING. Se 404 da rota ou formato diferente, parar a Task 3 e validar no Taiga local nesta tarefa a alternativa `resolver` documentada oficialmente. Uma ref inexistente não é evidência de rota inexistente. Confirmar também mesma ref em dois projetos e resposta a projeto inválido (fixtures isoladas), antes de aceitar o contrato.

- [ ] **Step 3: registrar evidência sanitizada.** Acrescentar seção `Fase 2 — by_ref` a `docs/api-notes.md`: data, versão da imagem, teste, método/path/query sem token, formato, status para ref inexistente e isolamento por projeto. Nesta redação a evidência permanece **pendente**; nunca preencher como validada sem executar. Se o contrato divergir, substituir a rota apenas no adapter da Task 3 e adicionar fixture do formato real.

Run: `git diff --check`
Expected: sem whitespace e sem segredos; somente notas verificadas na execução futura.

```bash
git add internal/taiga/stories_probe_integration_test.go docs/api-notes.md
git commit -m "Validar resolução de story por ref no Taiga local"
```

### Task 2: ancorar escritas versionadas à leitura que calculou o patch

**Files:**
- Modify: `internal/taiga/versioned.go`
- Create: `internal/taiga/versioned_from_test.go`

**Interfaces:** Produces `WriteVersionedFrom(ctx context.Context, method, path string, patch map[string]any, first map[string]json.RawMessage, force bool) (*Response, error)`; mantém `WriteVersioned` e `PrepareVersioned` compatíveis.

- [ ] **Step 1: escrever regressão de alteração entre merge e envio.**

```go
package taiga

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestWriteVersionedFromPreservesMergeBaseline(t *testing.T) {
    for _, field := range []string{"tags", "assigned_users", "attributes_values", "description"} {
        t.Run(field, func(t *testing.T) {
            writes := 0
            srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.Method == "GET" {
                    _ = json.NewEncoder(w).Encode(map[string]any{"version": 4, field: "concurrent"})
                    return
                }
                writes++
                var body map[string]any
                if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Error(err) }
                if body["version"] != float64(3) { t.Errorf("version=%v", body["version"]) }
                w.WriteHeader(409)
                _, _ = fmt.Fprint(w, `{}`)
            }))
            defer srv.Close()
            c := New(srv.URL, StaticToken{}, WithRetryWait(0))
            first := map[string]json.RawMessage{"version": json.RawMessage(`3`), field: json.RawMessage(`"old"`)}
            _, err := c.WriteVersionedFrom(context.Background(), "PATCH", "userstories/1",
                map[string]any{field: "merged"}, first, false)
            var conflict *ConflictError
            if !errors.As(err, &conflict) || writes != 1 { t.Fatalf("writes=%d err=%v", writes, err) }
        })
    }
}

func TestWriteVersionedFromRetriesOnlyUnchangedFields(t *testing.T) {
    writes := 0
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method == "GET" { _, _ = fmt.Fprint(w, `{"version":4,"status":1,"subject":"new"}`); return }
        writes++
        var b map[string]any
        if err := json.NewDecoder(r.Body).Decode(&b); err != nil { t.Error(err) }
        want := float64(3)
        if writes == 2 { want = 4 }
        if b["version"] != want { t.Errorf("body=%v", b) }
        if writes == 1 { w.WriteHeader(409); return }
        _, _ = fmt.Fprint(w, `{"version":5}`)
    }))
    defer srv.Close()
    c := New(srv.URL, StaticToken{}, WithRetryWait(0))
    _, err := c.WriteVersionedFrom(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2},
        map[string]json.RawMessage{"version": json.RawMessage(`3`), "status": json.RawMessage(`1`)}, false)
    if err != nil || writes != 2 { t.Fatalf("writes=%d err=%v", writes, err) }
}
```

- [ ] **Step 2: rodar vermelho.**

Run: `go test ./internal/taiga -run WriteVersionedFrom`
Expected: FAIL `WriteVersionedFrom undefined`.

- [ ] **Step 3: substituir `WriteVersioned` e adicionar o método abaixo.** Imports existentes já atendem; reaproveitar `withVersion`, `getObject`, `changedKeys`.

```go
func (c *Client) WriteVersioned(ctx context.Context, method, path string, patch map[string]any, force bool) (*Response, error) {
    _, first, err := c.PrepareVersioned(ctx, path, patch)
    if err != nil { return nil, err }
    return c.WriteVersionedFrom(ctx, method, path, patch, first, force)
}

func (c *Client) WriteVersionedFrom(ctx context.Context, method, path string, patch map[string]any,
    first map[string]json.RawMessage, force bool) (*Response, error) {
    version, ok := first["version"]
    var n int64
    if !ok || json.Unmarshal(version, &n) != nil || n < 0 {
        return nil, fmt.Errorf("GET %s: resource has no valid version", stagePath(path))
    }
    resp, err := c.Do(ctx, Request{Method: method, Path: path, Body: withVersion(patch, version)})
    var ae *APIError
    if err == nil || !errors.As(err, &ae) || !ae.IsVersionConflict() { return resp, err }
    second, err := c.getObject(ctx, stagePath(path))
    if err != nil { return nil, err }
    if !force {
        if changed := changedKeys(patch, first, second); len(changed) > 0 {
            return nil, &ConflictError{Method: method, Path: path, Fields: changed}
        }
    }
    resp, err = c.Do(ctx, Request{Method: method, Path: path, Body: withVersion(patch, second["version"])})
    if err != nil && errors.As(err, &ae) && ae.IsVersionConflict() {
        return nil, &ConflictError{Method: method, Path: path}
    }
    return resp, err
}
```

- [ ] **Step 4: testar também force, segundo conflito e ausência de version.** Force permite uma segunda escrita mesmo com campo alterado; segundo conflito sai 4; sem version não envia; erro 503/timeout não repete PATCH. Acrescentar casos aos testes existentes `versioned_test.go`, reutilizando seu fake.

Run: `go test -race ./internal/taiga ./internal/cli`
Expected: PASS inclusive `taiga api --auto-version`, sem alteração de assinatura pública.

```bash
git add internal/taiga/versioned.go internal/taiga/versioned_from_test.go internal/taiga/versioned_test.go
git commit -m "Ancorar patches versionados ao snapshot usado no merge"
```

### Task 3: serviço de stories, catálogos, CLI e saída

**Files:**
- Create: `internal/app/service.go`, `internal/app/stories.go`, `internal/app/story_patch.go`, `internal/app/stories_test.go`, `internal/cli/curated.go`, `internal/cli/story.go`, `internal/cli/story_test.go`, `internal/cli/story_integration_test.go`
- Modify: `internal/cli/root.go` (somente registrar `a.storyCmd()`)
- Modify ou Create: `README.md`, `docs/guia.md`, `docs/errors.md`

**Interfaces:**
- Consumes núcleo da Task 2, RunContext e output.
- Produces `Service{API API; Project Object}`, `Object map[string]any`, `New(ctx, api, project string) (*Service, error)`, `Story(ctx, ref string, id int64) (Object, error)`, `Stories(ctx, filters url.Values) ([]Object, error)`, `CreateStory(ctx, body Object, dry bool) (any, error)`, `UpdateStory(ctx, ref string, patch Patch, dry, force bool) (any, error)`, `CloseStory(ctx, ref, status string, dry, force bool) (any, error)`.
- `Patch` registra presença: `Set Object; AddTags, RemoveTags []string; Append *string`. #247 estende com merge de responsáveis.

**Superfície e regras exatas:**

| Comando | Flags e semântica |
|---|---|
| `story list` | `--ref` (requisito da descrição da US), `--status`, `--assignee`, `--epic`, `--tag` repetível, `--search`, `--closed` booleano trivalente (Changed), paginação GetAll |
| `story get [REF]` | alternativa exclusiva `--id ID`; exatamente um identificador positivo; URL `<base>/project/<slug>/us/<ref>`; bloqueio e campos próprios (Task 6 completa o enriquecimento) |
| `story create` | `--subject` obrigatório e não branco; `--description-file FILE|-`, `--status`, `--tag` repetível; #247 acrescenta `--assignee`; `--epic`/`--swimlane` só após gate abaixo |
| `story update REF` | `--subject`, `--description-file FILE|-`, `--append-description TEXT`, `--status`, `--tag` substitui só quando presente, `--add-tag`, `--remove-tag`, `--milestone`, `--epic`, `--swimlane`; patch mínimo; arquivo e append exclusivos |
| `story close REF` | `--status NAME|ID`; omitido: único status com `is_closed=true`, senão `ambiguous_name` exit 2; status explícito aberto: uso inválido; já fechado no destino: nenhuma escrita |

Todas as três escritas têm `--dry-run` e `--force-version` (force só faz sentido em alteração, rejeitar em criação). Tags de substituição e merge são exclusivas. Adicionar e remover a mesma tag é erro 2. Status/usuários por id também devem pertencer ao projeto. Username exato, não substring; nomes duplicados são `ambiguous_name`, ausência `not_found`. `--assignee` na listagem corresponde a assigned_users; **validar no Taiga local na Task 1** o filtro aceito, ou filtrar localmente após GetAll, mantendo o mesmo resultado.

**Gate de vínculos:** validar no Taiga local na Task 1 os filtros e escrita de milestone/swimlane/épico. Não supor que `epic` no PATCH funciona: pode exigir `epics/<id>/related_userstories`. Se houver criação de vínculo sem version ou substituição exigir DELETE, a flag fica bloqueada com `unsupported_operation` exit 2 e recuperação explícita até decisão humana. Não inventar PATCH nem violar a restrição de DELETE. #252/#253 entregam catálogos/épicos curados; a resolução interna mínima por ref/id nesta US não antecipa esses comandos.

- [ ] **Step 1: escrever testes de contrato (arquivo completo do caso mínimo).**

```go
package cli

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "testing"
)

func TestStoryUpdateDryRunPreservesTagsAndVersion(t *testing.T) {
    srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/api/v1/projects/37":
            _, _ = fmt.Fprint(w, `{"id":37,"slug":"infra-2025"}`)
        case "/api/v1/userstories/by_ref", "/api/v1/userstories/6808":
            _, _ = fmt.Fprint(w, `{"id":6808,"ref":246,"project":37,"version":7,"subject":"a","tags":[["old",null]]}`)
        default:
            t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
            w.WriteHeader(404)
        }
    })
    out, stderr, code := runIn(t, map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}, "",
        "story", "update", "246", "--project", "37", "--add-tag", "new", "--dry-run")
    if code != 0 { t.Fatalf("%d: %s", code, stderr) }
    var plan struct { Method, Path string; Body map[string]any }
    if err := json.Unmarshal([]byte(out), &plan); err != nil { t.Fatal(err) }
    if plan.Method != "PATCH" || plan.Path != "userstories/6808" || plan.Body["version"] != float64(7) {
        t.Fatalf("%s", out)
    }
    b, _ := json.Marshal(plan.Body["tags"])
    if string(b) != `["old","new"]` { t.Fatalf("tags=%s", b) }
    for _, call := range *calls { if call.method != "GET" { t.Fatalf("dry-run: %+v", call) } }
}

func TestStoryRejectsInvalidSelectorsAndConflictingFlags(t *testing.T) {
    for _, args := range [][]string{
        {"story", "get", "246", "--id", "6808"},
        {"story", "get", "0"},
        {"story", "update", "246", "--tag", "a", "--add-tag", "b"},
        {"story", "update", "246", "--add-tag", "a", "--remove-tag", "a"},
        {"story", "create", "--subject", " "},
    } {
        _, stderr, code := runIn(t, nil, "", args...)
        if code != 2 || !strings.Contains(stderr, "usage") { t.Fatalf("%v: %d %s", args, code, stderr) }
    }
}
```

Acrescentar testes com fake para: paginação 2 páginas sem duplicatas, `[]` vazio, todos os filtros simultâneos incluindo closed=false explícito; slug/id; me via users/me; status inexistente e duplicado; id de story de outro projeto (zero escritas); URL com slug escapado; descrição com `\n\t\u0001`/aspas/acentos; append em descrição vazia e não vazia; patch no-op sem PATCH; GET depois de POST/PATCH; erro 401/403/404/409/503 com exit esperado. Criar golden files `internal/cli/testdata/story_get.json`, `story_list.json`, `story_get.txt`, `story_dry_run.json`; assertar saída integral, sem atualizar goldens automaticamente.

Run: `go test ./internal/cli -run Story`
Expected: FAIL `unknown command "story"`.

- [ ] **Step 2: criar contrato comum completo em `service.go`.**

```go
package app

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/url"
    "strconv"

    "github.com/BasisTI/taiga-cli/internal/output"
    "github.com/BasisTI/taiga-cli/internal/taiga"
)

type Object map[string]any

type API interface {
    Do(context.Context, taiga.Request) (*taiga.Response, error)
    GetAll(context.Context, string, url.Values) ([]json.RawMessage, error)
    WriteVersioned(context.Context, string, string, map[string]any, bool) (*taiga.Response, error)
    WriteVersionedFrom(context.Context, string, string, map[string]any, map[string]json.RawMessage, bool) (*taiga.Response, error)
    BaseURL() string
}

type Service struct {
    API API
    Project Object
    catalogs map[string][]Object
}

func Decode(b []byte) (Object, error) {
    var out Object
    d := json.NewDecoder(bytes.NewReader(b))
    d.UseNumber()
    if err := d.Decode(&out); err != nil { return nil, err }
    if out == nil { return nil, fmt.Errorf("expected JSON object") }
    return out, nil
}

func ID(v any) int64 {
    n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
    return n
}

func Read(ctx context.Context, api API, path string, q url.Values) (Object, error) {
    r, err := api.Do(ctx, taiga.Request{Method: "GET", Path: path, Query: q})
    if err != nil { return nil, taiga.ToOutput(err) }
    return Decode(r.Body)
}

func New(ctx context.Context, api API, project string) (*Service, error) {
    if project == "" { return nil, Usage("project is required") }
    path, q := "projects/by_slug", url.Values{"slug": {project}}
    if n, err := strconv.ParseInt(project, 10, 64); err == nil && n > 0 {
        path, q = fmt.Sprintf("projects/%d", n), nil
    }
    p, err := Read(ctx, api, path, q)
    if err != nil { return nil, err }
    if ID(p["id"]) <= 0 || p["slug"] == nil { return nil, fmt.Errorf("invalid project response") }
    return &Service{API: api, Project: p, catalogs: map[string][]Object{}}, nil
}

func Usage(cause string) error {
    return &output.Error{Code: "usage", Cause: cause, Exit: output.ExitUsage}
}

func (s *Service) Catalog(ctx context.Context, path string) ([]Object, error) {
    if items, ok := s.catalogs[path]; ok { return items, nil }
    raws, err := s.API.GetAll(ctx, path, url.Values{"project": {fmt.Sprint(s.Project["id"])}})
    if err != nil { return nil, taiga.ToOutput(err) }
    items := []Object{}
    for _, raw := range raws {
        obj, err := Decode(raw)
        if err != nil { return nil, err }
        items = append(items, obj)
    }
    s.catalogs[path] = items
    return items, nil
}

func Resolve(items []Object, selector, nameKey string) (Object, error) {
    matches := []Object{}
    for _, o := range items {
        if fmt.Sprint(o[nameKey]) == selector || (ID(o["id"]) > 0 && fmt.Sprint(o["id"]) == selector) {
            matches = append(matches, o)
        }
    }
    if len(matches) == 0 {
        return nil, &output.Error{Code: "not_found", Cause: "catalog entry not found: " + selector, Exit: output.ExitNotFound}
    }
    if len(matches) != 1 {
        return nil, &output.Error{Code: "ambiguous_name", Cause: "multiple catalog entries: " + selector, Exit: output.ExitUsage}
    }
    return matches[0], nil
}

func Snapshot(o Object) (map[string]json.RawMessage, error) {
    b, err := json.Marshal(o)
    if err != nil { return nil, err }
    var raw map[string]json.RawMessage
    err = json.Unmarshal(b, &raw)
    return raw, err
}

type WritePlan struct {
    DryRun bool `json:"dry_run"`
    Method string `json:"method"`
    Path string `json:"path"`
    Body Object `json:"body"`
}

func (s *Service) Write(ctx context.Context, path string, before, patch Object, dry, force bool) (any, error) {
    if len(patch) == 0 { return before, nil }
    if _, ok := before["version"]; !ok { return nil, fmt.Errorf("resource has no version: %s", path) }
    if dry {
        body := Object{}
        for k, v := range patch { body[k] = v }
        body["version"] = before["version"]
        return WritePlan{true, "PATCH", path, body}, nil
    }
    raw, err := Snapshot(before)
    if err != nil { return nil, err }
    if _, err := s.API.WriteVersionedFrom(ctx, "PATCH", path, patch, raw, force); err != nil {
        return nil, taiga.ToOutput(err)
    }
    return Read(ctx, s.API, path, nil)
}
```

**Importante:** catálogos de usuários podem exigir memberships/projeto em vez de `users?project`; **validar no Taiga local na Task 1**. `Catalog` é adapter; não assumir que parâmetro ignorado delimita escopo. Conferir ids contra memberships quando necessário.

- [ ] **Step 3: implementar algoritmos de patch em `story_patch.go`.**

```go
package app

import (
    "encoding/json"
    "fmt"
)

type Patch struct {
    Set Object
    AddTags, RemoveTags []string
    Append *string
}

func Names(v any) ([]string, error) {
    if v == nil { return []string{}, nil }
    xs, ok := v.([]any)
    if !ok { return nil, fmt.Errorf("invalid tags array") }
    out := []string{}
    for _, x := range xs {
        if pair, ok := x.([]any); ok {
            if len(pair) == 0 { return nil, fmt.Errorf("empty tag pair") }
            x = pair[0]
        }
        n, ok := x.(string)
        if !ok { return nil, fmt.Errorf("invalid tag name") }
        out = append(out, n)
    }
    return out, nil
}

func MergeNames(current, add, remove []string) []string {
    removed, seen := map[string]bool{}, map[string]bool{}
    for _, x := range remove { removed[x] = true }
    out := []string{}
    for _, xs := range [][]string{current, add} {
        for _, x := range xs {
            if !removed[x] && !seen[x] { out = append(out, x); seen[x] = true }
        }
    }
    return out
}

func equal(a, b any) bool {
    x, ex := json.Marshal(a)
    y, ey := json.Marshal(b)
    return ex == nil && ey == nil && string(x) == string(y)
}

func BuildPatch(before Object, p Patch) (Object, error) {
    out := Object{}
    for k, v := range p.Set {
        old := before[k]
        if k == "tags" {
            var err error
            old, err = Names(before[k])
            if err != nil { return nil, err }
        }
        if !equal(old, v) { out[k] = v }
    }
    if p.Append != nil {
        old, _ := before["description"].(string)
        next := old
        if old != "" && *p.Append != "" { next += "\n\n" }
        next += *p.Append
        if next != old { out["description"] = next }
    }
    if len(p.AddTags)+len(p.RemoveTags) > 0 {
        old, err := Names(before["tags"])
        if err != nil { return nil, err }
        next := MergeNames(old, p.AddTags, p.RemoveTags)
        if !equal(old, next) { out["tags"] = next }
    }
    return out, nil
}
```

No-op de `--tag` compara nomes normalizados, preservando a leitura bruta passada a `WriteVersionedFrom`. `--append-description ""` não escreve. #247 usa `Patch.Set` para assigned_users/is_blocked/blocked_note.

- [ ] **Step 4: implementar `stories.go` e adapter curado.**

`stories.go` completo abaixo usa a hipótese by_ref, condicionada ao gate da Task 1. `Story` mantém tags brutas para OCC; `StoryView` normaliza somente a saída. Rotas auxiliares de filtros são decididas pela sondagem; este corpo usa pós-filtro local para não depender de parâmetro ignorado.

```go
package app

import (
    "context"
    "fmt"
    "net/url"
    "strconv"
    "strings"

    "github.com/BasisTI/taiga-cli/internal/output"
    "github.com/BasisTI/taiga-cli/internal/taiga"
)

func positive(selector string) (int64, error) {
    n, err := strconv.ParseInt(selector, 10, 64)
    if err != nil || n <= 0 { return 0, Usage("reference must be a positive integer") }
    return n, nil
}

func (s *Service) Story(ctx context.Context, ref string, id int64) (Object, error) {
    if (ref == "") == (id == 0) { return nil, Usage("choose REF or --id") }
    path, q := "userstories/by_ref", url.Values{}
    var expected int64
    if ref != "" {
        var err error
        expected, err = positive(ref)
        if err != nil { return nil, err }
        q.Set("project", fmt.Sprint(s.Project["id"]))
        q.Set("ref", fmt.Sprint(expected))
    } else {
        if id <= 0 { return nil, Usage("id must be positive") }
        path, q = fmt.Sprintf("userstories/%d", id), nil
    }
    o, err := Read(ctx, s.API, path, q)
    if err != nil { return nil, err }
    if ID(o["project"]) != ID(s.Project["id"]) { return nil, Usage("story belongs to another project") }
    if ID(o["id"]) <= 0 || ID(o["ref"]) <= 0 { return nil, fmt.Errorf("invalid story identity") }
    if expected > 0 && ID(o["ref"]) != expected { return nil, fmt.Errorf("by_ref returned another reference") }
    if id > 0 && ID(o["id"]) != id { return nil, fmt.Errorf("GET returned another id") }
    return o, nil
}

func (s *Service) StoryView(o Object) (Object, error) {
    out := Object{}
    for k, v := range o { out[k] = v }
    names, err := Names(o["tags"])
    if err != nil { return nil, err }
    out["tags"] = names
    out["url"] = s.API.BaseURL() + "/project/" + url.PathEscape(fmt.Sprint(s.Project["slug"])) + "/us/" + fmt.Sprint(o["ref"])
    return out, nil
}

func (s *Service) Stories(ctx context.Context, filters url.Values) ([]Object, error) {
    raws, err := s.API.GetAll(ctx, "userstories", url.Values{"project": {fmt.Sprint(s.Project["id"])}})
    if err != nil { return nil, taiga.ToOutput(err) }
    out := []Object{}
    for _, raw := range raws {
        o, err := Decode(raw)
        if err != nil { return nil, err }
        if ID(o["project"]) != ID(s.Project["id"]) { return nil, fmt.Errorf("list returned another project") }
        if filters.Get("ref") != "" && fmt.Sprint(o["ref"]) != filters.Get("ref") { continue }
        if filters.Get("status") != "" && fmt.Sprint(o["status"]) != filters.Get("status") { continue }
        if closed, present := filters["closed"]; present && fmt.Sprint(o["is_closed"]) != closed[0] { continue }
        if search := filters.Get("search"); search != "" && !strings.Contains(strings.ToLower(fmt.Sprint(o["subject"])), strings.ToLower(search)) { continue }
        if assignee := filters.Get("assignee"); assignee != "" {
            matched := false
            users, _ := o["assigned_users"].([]any)
            for _, u := range users { if fmt.Sprint(u) == assignee { matched = true } }
            if !matched { continue }
        }
        if epic := filters.Get("epic"); epic != "" {
            matched := false
            epics, _ := o["epics"].([]any)
            for _, e := range epics {
                if obj, ok := e.(map[string]any); ok && fmt.Sprint(obj["id"]) == epic { matched = true }
            }
            if !matched { continue }
        }
        view, err := s.StoryView(o)
        if err != nil { return nil, err }
        names, _ := view["tags"].([]string)
        allTags := true
        for _, wanted := range filters["tag"] {
            present := false
            for _, n := range names { if n == wanted { present = true } }
            if !present { allTags = false }
        }
        if allTags { out = append(out, view) }
    }
    return out, nil
}

func (s *Service) CreateStory(ctx context.Context, body Object, dry bool) (any, error) {
    subject, _ := body["subject"].(string)
    if strings.TrimSpace(subject) == "" { return nil, Usage("--subject is required") }
    body["project"] = s.Project["id"]
    if dry { return WritePlan{true, "POST", "userstories", body}, nil }
    r, err := s.API.Do(ctx, taiga.Request{Method: "POST", Path: "userstories", Body: body})
    if err != nil { return nil, taiga.ToOutput(err) }
    created, err := Decode(r.Body)
    if err != nil { return nil, err }
    raw, err := s.Story(ctx, "", ID(created["id"]))
    if err != nil { return nil, err }
    return s.StoryView(raw)
}

func (s *Service) UpdateStory(ctx context.Context, ref string, p Patch, dry, force bool) (any, error) {
    before, err := s.Story(ctx, ref, 0)
    if err != nil { return nil, err }
    patch, err := BuildPatch(before, p)
    if err != nil { return nil, err }
    result, err := s.Write(ctx, fmt.Sprintf("userstories/%d", ID(before["id"])), before, patch, dry, force)
    if err != nil { return nil, err }
    if o, ok := result.(Object); ok { return s.StoryView(o) }
    return result, nil
}

func (s *Service) CloseStory(ctx context.Context, ref, selector string, dry, force bool) (any, error) {
    statuses, err := s.Catalog(ctx, "userstory-statuses")
    if err != nil { return nil, err }
    var chosen Object
    if selector != "" {
        chosen, err = Resolve(statuses, selector, "name")
        if err != nil { return nil, err }
        if chosen["is_closed"] != true { return nil, Usage("close requires a closed status") }
    } else {
        closed := []Object{}
        for _, st := range statuses { if st["is_closed"] == true { closed = append(closed, st) } }
        if len(closed) == 0 { return nil, &output.Error{Code: "not_found", Cause: "no closed status", Exit: output.ExitNotFound} }
        if len(closed) > 1 { return nil, &output.Error{Code: "ambiguous_name", Cause: "choose a closed status with --status", Exit: output.ExitUsage} }
        chosen = closed[0]
    }
    return s.UpdateStory(ctx, ref, Patch{Set: Object{"status": chosen["id"]}}, dry, force)
}
```

Filtros de search/assignee/epic acima são semântica local explícita. Confirmar shape dos objetos listados na Task 1; se a listagem não devolver dados suficientes, hidratar detalhes por GET ou usar o filtro remoto **comprovado**, nunca descartar silenciosamente resultados. #247 pode usar UserID compartilhado para me.

Código completo do adapter comum (`internal/cli/curated.go`):

```go
package cli

import (
    "encoding/json"
    "fmt"
    "sort"
    "io"
    "os"

    "github.com/BasisTI/taiga-cli/internal/app"
    "github.com/BasisTI/taiga-cli/internal/output"
    "github.com/spf13/cobra"
)

func (a *App) service(cmd *cobra.Command) (*app.Service, error) {
    rc, err := a.runContext()
    if err != nil { return nil, err }
    c, err := a.client(cmd.Context(), rc)
    if err != nil { return nil, err }
    return app.New(cmd.Context(), c, rc.Ctx.Project.Value)
}

func (a *App) readContent(path string) (string, error) {
    if path == "-" {
        b, err := io.ReadAll(a.In)
        return string(b), err
    }
    b, err := os.ReadFile(path)
    if err != nil { return "", app.Usage("cannot read input file: " + err.Error()) }
    return string(b), nil
}

func (a *App) renderCurated(v any) error {
    mode, err := output.DetectMode(a.output, a.OutTTY)
    if err != nil { return err }
    if mode == output.JSON { return output.WriteJSON(a.Out, v) }
    switch x := v.(type) {
    case app.Object:
        keys := []string{"ref", "id", "subject", "url"}
        if _, isComment := x["story_ref"]; isComment { keys = []string{"story_ref", "user", "created_at", "comment", "url"} }
        if _, isField := x["name"]; isField { keys = []string{"id", "name", "type", "description", "is_closed", "order", "version"} }
        if _, isValues := x["attributes_values"]; isValues { keys = []string{"ref", "id", "version", "attributes_values", "fields", "url"} }
        fields := []output.Field{}
        for _, k := range keys {
            if val, ok := x[k]; ok { fields = append(fields, output.Field{Key: k, Value: fmt.Sprint(val)}) }
        }
        return output.WriteFields(a.Out, fields)
    case []app.Object:
        for _, o := range x { if err := a.renderCurated(o); err != nil { return err } }
        return nil
    default:
        // Dry-run and project results have nested structures; text emits key/value fields.
        b, err := json.Marshal(v)
        if err != nil { return err }
        var object map[string]json.RawMessage
        if err := json.Unmarshal(b, &object); err != nil { return err }
        keys := []string{}
        for k := range object { keys = append(keys, k) }
        sort.Strings(keys)
        fields := []output.Field{}
        for _, k := range keys { fields = append(fields, output.Field{Key: k, Value: string(object[k])}) }
        return output.WriteFields(a.Out, fields)
    }
}
```

`story.go` completo para a superfície confirmada da #246. As flags epic/swimlane/milestone dependem do gate; não enviar o corpo hipotético abaixo enquanto a Task 1 não confirmar cada propriedade/rota. `resolveStoryValue` é o adapter que deve ser ajustado à evidência local, incluindo vínculo de épico quando não for campo da story.

```go
package cli

import (
    "fmt"
    "net/url"
    "strconv"
    "strings"

    "github.com/BasisTI/taiga-cli/internal/app"
    "github.com/spf13/cobra"
)

func validRef(ref string) error {
    n, err := strconv.ParseInt(ref, 10, 64)
    if err != nil || n <= 0 { return app.Usage("reference must be a positive integer") }
    return nil
}

func (a *App) storyCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "story", Short: "Read and update user stories"}
    cmd.AddCommand(a.storyListCmd(), a.storyGetCmd(), a.storyWriteCmd(false), a.storyWriteCmd(true), a.storyCloseCmd())
    return cmd
}

func (a *App) storyGetCmd() *cobra.Command {
    var id int64
    cmd := &cobra.Command{Use: "get [REF]", Short: "Get a story by reference or id", Args: cobra.MaximumNArgs(1)}
    cmd.RunE = func(cmd *cobra.Command, args []string) error {
        hasRef, hasID := len(args) == 1, cmd.Flags().Changed("id")
        if hasRef == hasID || (hasID && id <= 0) { return app.Usage("choose REF or a positive --id") }
        ref := ""
        if hasRef { ref = args[0]; if err := validRef(ref); err != nil { return err } }
        service, err := a.service(cmd)
        if err != nil { return err }
        raw, err := service.Story(cmd.Context(), ref, id)
        if err != nil { return err }
        view, err := service.StoryView(raw)
        if err != nil { return err }
        return a.renderCurated(view)
    }
    cmd.Flags().Int64Var(&id, "id", 0, "internal story id, exclusive with REF")
    return cmd
}

func (a *App) storyListCmd() *cobra.Command {
    var ref, status, assignee, epic, search string
    var tags []string
    var closed bool
    cmd := &cobra.Command{Use: "list", Short: "List every matching story", Args: cobra.NoArgs}
    cmd.RunE = func(cmd *cobra.Command, _ []string) error {
        if ref != "" { if err := validRef(ref); err != nil { return err } }
        service, err := a.service(cmd)
        if err != nil { return err }
        q := url.Values{}
        if ref != "" { q.Set("ref", ref) }
        if search != "" { q.Set("search", search) }
        if cmd.Flags().Changed("closed") { q.Set("closed", strconv.FormatBool(closed)) }
        for _, tag := range tags { q.Add("tag", tag) }
        if status != "" {
            items, err := service.Catalog(cmd.Context(), "userstory-statuses")
            if err != nil { return err }
            st, err := app.Resolve(items, status, "name")
            if err != nil { return err }
            q.Set("status", fmt.Sprint(st["id"]))
        }
        if assignee != "" {
            selector := assignee
            if selector == "me" {
                me, err := app.Read(cmd.Context(), service.API, "users/me", nil)
                if err != nil { return err }
                selector = fmt.Sprint(me["id"])
            }
            users, err := service.Catalog(cmd.Context(), "users")
            if err != nil { return err }
            user, err := app.Resolve(users, selector, "username")
            if err != nil { return err }
            q.Set("assignee", fmt.Sprint(user["id"]))
        }
        if epic != "" {
            items, err := service.Catalog(cmd.Context(), "epics")
            if err != nil { return err }
            e, err := app.Resolve(items, epic, "ref")
            if err != nil { return err }
            q.Set("epic", fmt.Sprint(e["id"]))
        }
        result, err := service.Stories(cmd.Context(), q)
        if err != nil { return err }
        return a.renderCurated(result)
    }
    f := cmd.Flags()
    f.StringVar(&ref, "ref", "", "story reference")
    f.StringVar(&status, "status", "", "status name or id")
    f.StringVar(&assignee, "assignee", "", "project username, id or me")
    f.StringVar(&epic, "epic", "", "epic reference")
    f.StringArrayVar(&tags, "tag", nil, "required tag, repeatable")
    f.StringVar(&search, "search", "", "subject search")
    f.BoolVar(&closed, "closed", false, "filter closed state, true or false")
    return cmd
}

func (a *App) resolveStoryValue(cmd *cobra.Command, service *app.Service, field, selector string) (any, error) {
    path, key := "", "name"
    switch field {
    case "status": path = "userstory-statuses"
    case "milestone": path = "milestones"
    case "swimlane": path = "swimlanes"
    case "epic": path, key = "epics", "ref"
    default: return nil, app.Usage("unsupported story field")
    }
    items, err := service.Catalog(cmd.Context(), path)
    if err != nil { return nil, err }
    found, err := app.Resolve(items, selector, key)
    if err != nil { return nil, err }
    return found["id"], nil
}

func (a *App) storyWriteCmd(update bool) *cobra.Command {
    var subject, descriptionFile, appendText, status, epic, milestone, swimlane string
    var tags, addTags, removeTags []string
    var dry, force bool
    use, short := "create", "Create a story"
    args := cobra.NoArgs
    if update { use, short, args = "update REF", "Update a story", cobra.ExactArgs(1) }
    cmd := &cobra.Command{Use: use, Short: short, Args: args}
    cmd.RunE = func(cmd *cobra.Command, argv []string) error {
        f := cmd.Flags()
        if update { if err := validRef(argv[0]); err != nil { return err } }
        if (!update || f.Changed("subject")) && strings.TrimSpace(subject) == "" { return app.Usage("--subject is required") }
        if !update && force { return app.Usage("--force-version is not valid for creation") }
        if f.Changed("description-file") && f.Changed("append-description") { return app.Usage("choose description file or append") }
        if f.Changed("tag") && (len(addTags)+len(removeTags) > 0) { return app.Usage("choose tag replacement or merge") }
        for _, add := range addTags { for _, remove := range removeTags { if add == remove { return app.Usage("tag cannot be added and removed together") } } }
        patch := app.Patch{Set: app.Object{}, AddTags: addTags, RemoveTags: removeTags}
        if f.Changed("subject") { patch.Set["subject"] = subject }
        if f.Changed("description-file") {
            text, err := a.readContent(descriptionFile)
            if err != nil { return err }
            patch.Set["description"] = text
        }
        if f.Changed("append-description") { patch.Append = &appendText }
        if f.Changed("tag") { patch.Set["tags"] = app.MergeNames(nil, tags, nil) }
        service, err := a.service(cmd)
        if err != nil { return err }
        for _, pair := range [][2]string{{"status", status}, {"epic", epic}, {"milestone", milestone}, {"swimlane", swimlane}} {
            if !f.Changed(pair[0]) { continue }
            value, err := a.resolveStoryValue(cmd, service, pair[0], pair[1])
            if err != nil { return err }
            patch.Set[pair[0]] = value
        }
        var result any
        if update { result, err = service.UpdateStory(cmd.Context(), argv[0], patch, dry, force) } else {
            result, err = service.CreateStory(cmd.Context(), patch.Set, dry)
        }
        if err != nil { return err }
        return a.renderCurated(result)
    }
    f := cmd.Flags()
    f.StringVar(&subject, "subject", "", "story subject")
    f.StringVar(&descriptionFile, "description-file", "", "read description from FILE or -")
    f.StringVar(&status, "status", "", "project status name or id")
    f.StringVar(&epic, "epic", "", "epic reference, requires validated link contract")
    f.StringVar(&swimlane, "swimlane", "", "swimlane name or id")
    f.StringArrayVar(&tags, "tag", nil, "replace tags, repeatable")
    if update {
        f.StringVar(&appendText, "append-description", "", "append text separated by a blank line")
        f.StringVar(&milestone, "milestone", "", "milestone name or id")
        f.StringArrayVar(&addTags, "add-tag", nil, "add a tag")
        f.StringArrayVar(&removeTags, "remove-tag", nil, "remove a tag")
    }
    f.BoolVar(&dry, "dry-run", false, "print the request without writing")
    f.BoolVar(&force, "force-version", false, "override a version conflict once")
    return cmd
}

func (a *App) storyCloseCmd() *cobra.Command {
    var status string
    var dry, force bool
    cmd := &cobra.Command{Use: "close REF", Short: "Move a story to a closed status", Args: cobra.ExactArgs(1)}
    cmd.RunE = func(cmd *cobra.Command, args []string) error {
        if err := validRef(args[0]); err != nil { return err }
        service, err := a.service(cmd)
        if err != nil { return err }
        result, err := service.CloseStory(cmd.Context(), args[0], status, dry, force)
        if err != nil { return err }
        return a.renderCurated(result)
    }
    cmd.Flags().StringVar(&status, "status", "", "closed status name or id")
    cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request without writing")
    cmd.Flags().BoolVar(&force, "force-version", false, "override a version conflict once")
    return cmd
}
```

**Antes de executar esse bloco:** epic é apenas hipótese de campo. Enquanto o gate não confirmar PATCH/POST direto, interceptar `--epic` com `unsupported_operation` antes da rede. Se o vínculo exigir operação separada, CreateStory/UpdateStory devem preparar todas as ações antes da primeira escrita, mostrar todas no dry-run e reportar falha parcial (sem DELETE); não esconder vínculo numa escrita que ignora a flag. O gate exige código e teste do adapter confirmado no PR #246, ou pendência humana explicitamente registrada; não considerar #246 entregue sem resolver esse requisito.

Flags inválidas são verificadas antes da autenticação; usar Changed para vazio explícito, StringArray para tags com vírgula. Atualização de subject vazio explícito deve ser recusada por Usage, como create. **RunContext não é substituído**. Registro completo a acrescentar em root: `root.AddCommand(a.storyCmd())`, preservando api/auth/version.

Run: `gofmt -w internal/app internal/cli/curated.go internal/cli/story.go`
Expected: arquivos completos formatados e sem imports ociosos.

- [ ] **Step 5: integrar, testar e documentar os comandos.**

Run: `go test -race ./...`
Expected: PASS nos contratos desta tarefa e base #243/#245; testes usam TokenSource injetada, sem depender de home/keyring.

Na janela futura: `go test -tags integration -p 1 -run 'Story|ProbeStory' ./internal/cli ./internal/taiga`
Expected: criar fixture → get por ref/id → filtrar → atualizar tags/status/milestone → close → reler; dry-run de cada escrita deixa version/conteúdo iguais. Um teste após merge da #244 faz story get só com sessão, sem TAIGA_TOKEN; falha de sessão chega intacta exit 3, sem login próprio em app.

Atualizar guia/README com comandos da tabela e docs/errors com `ambiguous_name`/`unsupported_operation`. Gate de PR futuro: gofmt sem saída, go vet, lint, unidade e integração da US, contrato de saída revisado. Abrir somente após autorização futura; título exato desta seção, um PR #246, sem publicar agora.

```bash
git add internal/app internal/cli/curated.go internal/cli/story.go internal/cli/story_test.go internal/cli/story_integration_test.go internal/cli/testdata internal/cli/root.go README.md docs/guia.md docs/errors.md
git commit -m "Implementar listagem, leitura e escrita de stories"
```

## US #247 — Responsáveis e bloqueio

**Branch:** `TG-247`, depois do merge da #246. **PR:** `Implementar responsáveis e bloqueio TG-247`.
**Descrição consultada:** acrescentar/remover assigned_users preservando os atuais; bloquear/desbloquear por is_blocked e blocked_note.

### Task 4: merge de responsáveis e bloqueio explícito

**Files:**
- Create: `internal/app/assignees.go`, `internal/app/assignees_test.go`, `internal/cli/assignees_test.go`
- Modify: `internal/cli/story.go`, `internal/app/stories.go`, `internal/cli/story_integration_test.go`, `README.md`, `docs/guia.md`

**Interfaces:** Consumes `Patch.Set`, `Service.Catalog/Write`, `WriteVersionedFrom`; Produces `MergeIDs(current, add, remove []int64) []int64`, `AssignmentPatch(before Object, add, remove []int64, owner *int64, block *string, unblock bool) (Object, error)`.

- [ ] **Step 1: escrever testes completos.**

```go
package app

import (
    "encoding/json"
    "reflect"
    "testing"
)

func TestAssignmentPatchPreservesOwnerAndCurrentAssignees(t *testing.T) {
    before := Object{"assigned_to": json.Number("6"), "assigned_users": []any{json.Number("6"), json.Number("166")}}
    got, err := AssignmentPatch(before, []int64{20, 20}, []int64{166}, nil, nil, false)
    if err != nil { t.Fatal(err) }
    if !reflect.DeepEqual(got["assigned_users"], []int64{6, 20}) { t.Fatalf("%v", got) }
    if _, exists := got["assigned_to"]; exists { t.Fatal("implicit owner change") }
    if !reflect.DeepEqual(before["assigned_users"], []any{json.Number("6"), json.Number("166")}) {
        t.Fatal("input mutated")
    }
}

func TestAssignmentPatchBlockUnblockAndInvalidOverlap(t *testing.T) {
    note := "waiting for B6\n\"review\""
    got, err := AssignmentPatch(Object{}, nil, nil, nil, &note, false)
    if err != nil || got["is_blocked"] != true || got["blocked_note"] != note { t.Fatalf("%v %v", got, err) }
    got, err = AssignmentPatch(Object{"is_blocked": true, "blocked_note": note}, nil, nil, nil, nil, true)
    if err != nil || got["is_blocked"] != false || got["blocked_note"] != "" { t.Fatalf("%v %v", got, err) }
    if _, err := AssignmentPatch(Object{}, []int64{6}, []int64{6}, nil, nil, false); err == nil { t.Fatal("overlap") }
    if _, err := AssignmentPatch(Object{}, nil, nil, nil, &note, true); err == nil { t.Fatal("block and unblock") }
    owner := int64(20)
    got, err = AssignmentPatch(Object{}, nil, nil, &owner, nil, false)
    if err != nil || got["assigned_to"] != owner { t.Fatalf("%v %v", got, err) }
}
```

Run: `go test ./internal/app -run Assignment`
Expected: FAIL undefined AssignmentPatch.

- [ ] **Step 2: implementar o arquivo completo.**

```go
package app

import "strings"

func MergeIDs(current, add, remove []int64) []int64 {
    removed, seen := map[int64]bool{}, map[int64]bool{}
    for _, id := range remove { removed[id] = true }
    out := []int64{}
    for _, xs := range [][]int64{current, add} {
        for _, id := range xs {
            if !removed[id] && !seen[id] { out = append(out, id); seen[id] = true }
        }
    }
    return out
}

func AssignmentPatch(before Object, add, remove []int64, owner *int64, block *string, unblock bool) (Object, error) {
    removed := map[int64]bool{}
    for _, id := range remove {
        if id <= 0 { return nil, Usage("assignee must be a positive id") }
        removed[id] = true
    }
    for _, id := range add {
        if id <= 0 || removed[id] { return nil, Usage("assignee cannot be added and removed together") }
    }
    if block != nil && (unblock || strings.TrimSpace(*block) == "") {
        return nil, Usage("--block requires a note and cannot be combined with --unblock")
    }
    out := Object{}
    if len(add)+len(remove) > 0 {
        current := []int64{}
        if value := before["assigned_users"]; value != nil {
            xs, ok := value.([]any)
            if !ok { return nil, Usage("invalid assigned_users response") }
            for _, x := range xs {
                id := ID(x)
                if id <= 0 { return nil, Usage("invalid assignee id in response") }
                current = append(current, id)
            }
        }
        next := MergeIDs(current, add, remove)
        if !equal(current, next) { out["assigned_users"] = next }
    }
    if owner != nil {
        if *owner <= 0 { return nil, Usage("owner assignee must be a positive id") }
        if ID(before["assigned_to"]) != *owner { out["assigned_to"] = *owner }
    }
    if block != nil {
        if before["is_blocked"] != true { out["is_blocked"] = true }
        if before["blocked_note"] != *block { out["blocked_note"] = *block }
    }
    if unblock {
        if before["is_blocked"] != false { out["is_blocked"] = false }
        if before["blocked_note"] != "" { out["blocked_note"] = "" }
    }
    return out, nil
}
```

- [ ] **Step 3: ligar às flags e resolver usuário.** `story create --assignee` repetível define assigned_users, preservando assigned_to padrão da API; `story update --add-assignee/--remove-assignee` repetíveis e `--owner-assignee` explícito; `--block NOTE` exclusivo de `--unblock`. Resolver me por GET users/me e exigir associação ao projeto; nomes por username exato, id positivo conferido contra membros. Não usar nome completo ambíguo como atalho. Não sincronizar implicitamente assigned_to ao remover de assigned_users; **validar no Taiga local nesta Task 4** se a API impõe associação entre ambos, e documentar eventual bloqueio de combinação inconsistente.

Código completo do resolver a acrescentar em `assignees.go` (acrescentar imports context/fmt):

```go
func (s *Service) UserID(ctx context.Context, selector string) (int64, error) {
    if selector == "me" {
        me, err := Read(ctx, s.API, "users/me", nil)
        if err != nil { return 0, err }
        selector = fmt.Sprint(me["id"])
    }
    users, err := s.Catalog(ctx, "users")
    if err != nil { return 0, err }
    user, err := Resolve(users, selector, "username")
    if err != nil { return 0, err }
    return ID(user["id"]), nil
}
```

A rota `users` acima depende do adapter de memberships validado na Task 1. Não aceitar uma lista global retornada ao ignorar project. Mesclar AssignmentPatch ao patch da Task 3 usando a **mesma leitura bruta**, antes de Service.Write. Validar todas as flags/nomes antes de qualquer escrita.

Extensão completa de `Patch` (substituir sua declaração em story_patch.go na #247):

```go
type Patch struct {
    Set Object
    AddTags, RemoveTags []string
    Append *string
    AddAssignees, RemoveAssignees []int64
    Owner *int64
    Block *string
    Unblock bool
}
```

Acrescentar ao final de BuildPatch, antes do return:

```go
    assignments, err := AssignmentPatch(before, p.AddAssignees, p.RemoveAssignees, p.Owner, p.Block, p.Unblock)
    if err != nil { return nil, err }
    for k, v := range assignments { out[k] = v }
```

Helpers completos da CLI, acrescentados em `story.go`:

```go
func assignmentFlags(cmd *cobra.Command, update bool) {
    if update {
        cmd.Flags().StringArray("add-assignee", nil, "add a project username, id or me")
        cmd.Flags().StringArray("remove-assignee", nil, "remove a project username, id or me")
        cmd.Flags().String("owner-assignee", "", "set the main assignee explicitly")
        cmd.Flags().String("block", "", "block with a note")
        cmd.Flags().Bool("unblock", false, "clear block and note")
    } else { cmd.Flags().StringArray("assignee", nil, "initial assignee, repeatable") }
}

func parseAssignments(cmd *cobra.Command, service *app.Service, patch *app.Patch, update bool) error {
    f := cmd.Flags()
    resolve := func(flag string) ([]int64, error) {
        values, err := f.GetStringArray(flag)
        if err != nil { return nil, err }
        ids := []int64{}
        for _, value := range values {
            id, err := service.UserID(cmd.Context(), value)
            if err != nil { return nil, err }
            ids = append(ids, id)
        }
        return app.MergeIDs(nil, ids, nil), nil
    }
    if !update {
        if f.Changed("assignee") {
            ids, err := resolve("assignee")
            if err != nil { return err }
            patch.Set["assigned_users"] = ids
        }
        return nil
    }
    add, err := resolve("add-assignee")
    if err != nil { return err }
    remove, err := resolve("remove-assignee")
    if err != nil { return err }
    patch.AddAssignees, patch.RemoveAssignees = add, remove
    if f.Changed("owner-assignee") {
        value, _ := f.GetString("owner-assignee")
        id, err := service.UserID(cmd.Context(), value)
        if err != nil { return err }
        patch.Owner = &id
    }
    if f.Changed("block") { value, _ := f.GetString("block"); patch.Block = &value }
    patch.Unblock, _ = f.GetBool("unblock")
    if patch.Block != nil && patch.Unblock { return app.Usage("choose --block or --unblock") }
    return nil
}
```

Na construção de storyWriteCmd, antes do return cmd: `assignmentFlags(cmd, update)`. No RunE, depois de resolver todos os nomes e antes de CreateStory/UpdateStory: `if err := parseAssignments(cmd, service, &patch, update); err != nil { return err }`. Validar block vazio/overlap antes de qualquer escrita. O BuildPatch usa a mesma leitura já obtida em UpdateStory.

- [ ] **Step 4: regressões CLI, integração e documentação.** Testar dry-run combinando tags, executor e bloqueio, sem tocar assigned_to; remover último executor envia `[]`; repetição gera no-op; usuário inexistente/fora do projeto sai 5; owner explícito só altera assigned_to. Forçar conflito de assigned_users entre leitura e PATCH: exit 4 e nenhum segundo PATCH; conflito só em subject permite uma repetição. Mesma regra para blocked_note. A API pode normalizar campos no GET final, que é a saída.

Run: `go test -race ./...`
Expected: PASS.

Na janela local: `go test -tags integration -p 1 -run Story ./internal/cli`
Expected: add/remove de dois usuários, preservar owner, block/unblock e dry-run com releitura sem mudanças. Acrescentar exemplos no README/guia e achados em api-notes.

```bash
git add internal/app/assignees.go internal/app/assignees_test.go internal/app/stories.go internal/cli/story.go internal/cli/assignees_test.go internal/cli/story_integration_test.go README.md docs/guia.md docs/api-notes.md
git commit -m "Implementar merge de responsáveis e bloqueio de stories"
```

**Gate de PR futuro:** gofmt/go vet/lint, unidade e integração; título `Implementar responsáveis e bloqueio TG-247`; uma única US e squash. Nenhum push/status neste trabalho de planejamento.

## US #248 — Campos customizados

**Branch:** `TG-248`, depois do merge da #247. **PR:** `Implementar definições e valores de campos customizados TG-248`.
**Descrição consultada:** listar/criar definições idempotentemente; ler/gravar dicionário inteiro com merge e versão própria, para stories e tasks.

### Task 5: sondagem e definições idempotentes de story/task

**Files:**
- Create: `internal/app/fields.go`, `internal/app/fields_test.go`, `internal/cli/field.go`, `internal/cli/field_test.go`, `internal/cli/fields_probe_integration_test.go`
- Modify: `internal/cli/root.go` (registrar field), `docs/api-notes.md`

**Interfaces:** Produces `FieldPath(kind string) (string, error)`, `Service.Fields(ctx, kind string) ([]Object, error)`, `Service.CreateField(ctx, kind, name, typ string, description *string, dry bool) (any, error)`.

**Gate:** **validar no Taiga local na Task 5** `userstory-custom-attributes`, `task-custom-attributes`, métodos/corpos, tipos e nome case-sensitive, permissões e existência de version em definições. Testar mesma definição criada duas vezes, incompatível com mesmo nome, dois nomes iguais e corrida de criação. GET/POST são hipóteses até api-notes registrar. Testar admin e conta sem admin_project_values. Não inferir suporte a tipos além de text/date/checkbox; os outros entram só depois de enumerar e validar o catálogo local.

- [ ] **Step 1: teste completo da seleção de rota e erro de tipo.**

```go
package app

import "testing"

func TestFieldPathsAndValidation(t *testing.T) {
    for kind, want := range map[string]string{"story": "userstory-custom-attributes", "task": "task-custom-attributes"} {
        got, err := FieldPath(kind)
        if err != nil || got != want { t.Fatalf("%s: %s %v", kind, got, err) }
    }
    if _, err := FieldPath("issue"); err == nil { t.Fatal("invalid kind accepted") }
    if err := ValidateField("Testado em staging", "checkbox"); err != nil { t.Fatal(err) }
    for _, tc := range [][2]string{{"", "text"}, {"x", "unknown"}, {" ", "date"}} {
        if err := ValidateField(tc[0], tc[1]); err == nil { t.Fatalf("accepted %v", tc) }
    }
}
```

Run: `go test ./internal/app -run FieldPaths`
Expected: FAIL undefined FieldPath.

- [ ] **Step 2: implementar `fields.go` completo, após confirmar rotas.**

```go
package app

import (
    "context"
    "fmt"
    "strings"

    "github.com/BasisTI/taiga-cli/internal/output"
    "github.com/BasisTI/taiga-cli/internal/taiga"
)

func FieldPath(kind string) (string, error) {
    switch kind {
    case "story": return "userstory-custom-attributes", nil
    case "task": return "task-custom-attributes", nil
    }
    return "", Usage("--kind must be story or task")
}

func ValidateField(name, typ string) error {
    if strings.TrimSpace(name) == "" { return Usage("field name is required") }
    switch typ {
    case "text", "date", "checkbox": return nil
    }
    return Usage("unsupported field type: " + typ)
}

func (s *Service) Fields(ctx context.Context, kind string) ([]Object, error) {
    path, err := FieldPath(kind)
    if err != nil { return nil, err }
    return s.Catalog(ctx, path)
}

func (s *Service) CreateField(ctx context.Context, kind, name, typ string, description *string, dry bool) (any, error) {
    if err := ValidateField(name, typ); err != nil { return nil, err }
    path, err := FieldPath(kind)
    if err != nil { return nil, err }
    fields, err := s.Fields(ctx, kind)
    if err != nil { return nil, err }
    matches := []Object{}
    for _, field := range fields { if field["name"] == name { matches = append(matches, field) } }
    if len(matches) > 1 {
        return nil, &output.Error{Code: "ambiguous_name", Cause: "duplicate field name: " + name, Exit: output.ExitUsage}
    }
    if len(matches) == 1 {
        if matches[0]["type"] != typ || (description != nil && fmt.Sprint(matches[0]["description"]) != *description) {
            return nil, &output.Error{Code: "field_definition_conflict", Cause: "existing field differs: " + name,
                Recovery: "review the existing definition; no changes were made", Exit: output.ExitUsage}
        }
        return matches[0], nil
    }
    body := Object{"project": s.Project["id"], "name": name, "type": typ}
    if description != nil { body["description"] = *description }
    if dry { return WritePlan{true, "POST", path, body}, nil }
    r, err := s.API.Do(ctx, taiga.Request{Method: "POST", Path: path, Body: body})
    if err != nil { return nil, taiga.ToOutput(err) }
    delete(s.catalogs, path)
    created, err := Decode(r.Body)
    if err != nil { return nil, err }
    return Read(ctx, s.API, fmt.Sprintf("%s/%d", path, ID(created["id"])), nil)
}
```

Descrição omitida compara somente type/name; a CLI passa nil quando ausente. Não alterar definições existentes silenciosamente. Cache invalidado após POST. Corrida: nenhuma repetição cega de POST; se o Taiga rejeitar duplicata de nome, reler e aceitar somente definição compatível; se permitir duplicata, documentar que idempotência não garante unicidade entre processos, e recusar futuros matches ambíguos.

- [ ] **Step 3: implementar CLI, sem comandos de tasks antecipados.** `field list --kind story|task` e `field create --kind story|task --name NAME --type TYPE [--description TEXT] [--dry-run]`. Kind obrigatório e validado antes da rede. Renderizar array de definições com id/name/type/description/order e demais campos da API; definição não tem ref/URL de story. Texto usa `WriteFields` com name/id/type, não renderer de story. Registrar `a.fieldCmd()` em root.

Código completo de `field.go` (imports e comandos):

```go
package cli

import (
    "github.com/BasisTI/taiga-cli/internal/app"
    "github.com/spf13/cobra"
)

func (a *App) fieldCmd() *cobra.Command {
    parent := &cobra.Command{Use: "field", Short: "Manage custom field definitions"}
    for _, create := range []bool{false, true} {
        var kind, name, typ, description string
        var dry bool
        use, short := "list", "List custom fields"
        if create { use, short = "create", "Create a custom field idempotently" }
        cmd := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs}
        cmd.RunE = func(cmd *cobra.Command, _ []string) error {
            if _, err := app.FieldPath(kind); err != nil { return err }
            if create { if err := app.ValidateField(name, typ); err != nil { return err } }
            service, err := a.service(cmd)
            if err != nil { return err }
            var result any
            if create {
                var desc *string
                if cmd.Flags().Changed("description") { desc = &description }
                result, err = service.CreateField(cmd.Context(), kind, name, typ, desc, dry)
            } else { result, err = service.Fields(cmd.Context(), kind) }
            if err != nil { return err }
            return a.renderCurated(result)
        }
        cmd.Flags().StringVar(&kind, "kind", "", "story or task, required")
        if create {
            cmd.Flags().StringVar(&name, "name", "", "field name, required")
            cmd.Flags().StringVar(&typ, "type", "", "validated field type, required")
            cmd.Flags().StringVar(&description, "description", "", "field description")
            cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request without writing")
        }
        parent.AddCommand(cmd)
    }
    return parent
}
```

Teste fake deve assertar: GET de catálogo com project, zero POST em idempotência, zero POST em incompatibilidade, um POST na criação e GET de confirmação, dry-run sem POST, task usa rota de task.

Run: `go test -race ./internal/app ./internal/cli`
Expected: PASS.

Run na janela futura: `go test -tags integration -p 1 -run FieldsProbe -v ./internal/cli`
Expected: achados de tipos/rotas/version/permissões registrados; segundo create não altera catálogo.

```bash
git add internal/app/fields.go internal/app/fields_test.go internal/cli/field.go internal/cli/field_test.go internal/cli/fields_probe_integration_test.go internal/cli/root.go docs/api-notes.md
git commit -m "Implementar definições idempotentes de campos de story e task"
```

### Task 6: valores de campos com merge, tipos e version independente

**Files:**
- Create: `internal/app/field_values.go`, `internal/app/field_values_test.go`, `internal/cli/story_fields.go`, `internal/cli/story_fields_test.go`, `internal/cli/field_values_integration_test.go`
- Modify: `internal/cli/story.go` (registrar field), `internal/app/stories.go` (enriquecimento get), `README.md`, `docs/guia.md`, `docs/errors.md`, `docs/api-notes.md`

**Interfaces:** Produces `ValuePath(kind string, id int64) (string, error)`, `ParseFieldValue(typ, raw string) (any, error)`, `MergeValues(current Object, updates Object) Object`, `Service.FieldValues(ctx, kind string, id int64) (Object, error)`, `Service.SetFieldValues(ctx, kind string, id int64, entries []string, dry, force bool) (any, error)`.

**Gate:** **validar no Taiga local na Task 6** GET/PATCH `userstories/custom-attributes-values/<id>` e `tasks/custom-attributes-values/<id>`; propriedade attributes_values, version próprio, chaves ids em string, valor vazio/null/false/date e leitura posterior. Nunca mandar custom_attributes dentro do PATCH de story. Confirmar que task resolve ref pelo projeto e que version do dicionário é diferente do version da story/task. Task field CLI curada fica para #253, mas o serviço e integração de valores de task são entregues aqui.

- [ ] **Step 1: testes completos de tipos e preservação do dicionário.**

```go
package app

import (
    "reflect"
    "testing"
)

func TestParseFieldValuesAndMerge(t *testing.T) {
    for _, tc := range []struct { typ, raw string; want any }{
        {"text", "12", "12"}, {"text", "", ""}, {"text", "null", "null"},
        {"checkbox", "false", false}, {"checkbox", "true", true}, {"date", "2026-09-30", "2026-09-30"},
    } {
        got, err := ParseFieldValue(tc.typ, tc.raw)
        if err != nil || !reflect.DeepEqual(got, tc.want) { t.Fatalf("%+v: %v %v", tc, got, err) }
    }
    for _, tc := range [][2]string{{"checkbox", "yes"}, {"date", "2026-02-30"}, {"date", "30/09/2026"}} {
        if _, err := ParseFieldValue(tc[0], tc[1]); err == nil { t.Fatalf("accepted %v", tc) }
    }
    current := Object{"27": "old", "28": false, "999": Object{"unknown": true}}
    got := MergeValues(current, Object{"27": "new"})
    if got["27"] != "new" || got["28"] != false || !reflect.DeepEqual(got["999"], current["999"]) {
        t.Fatalf("%v", got)
    }
    if current["27"] != "old" { t.Fatal("mutated input") }
}
```

Run: `go test ./internal/app -run ParseField`
Expected: FAIL undefined ParseFieldValue.

- [ ] **Step 2: implementar algoritmos completos.**

```go
package app

import (
    "context"
    "fmt"
    "strings"
    "time"
)

func ValuePath(kind string, id int64) (string, error) {
    if id <= 0 { return "", Usage("resource id must be positive") }
    switch kind {
    case "story": return fmt.Sprintf("userstories/custom-attributes-values/%d", id), nil
    case "task": return fmt.Sprintf("tasks/custom-attributes-values/%d", id), nil
    }
    return "", Usage("--kind must be story or task")
}

func ParseFieldValue(typ, raw string) (any, error) {
    switch typ {
    case "text": return raw, nil
    case "checkbox":
        switch raw { case "true": return true, nil; case "false": return false, nil }
        return nil, Usage("checkbox value must be true or false")
    case "date":
        if _, err := time.Parse("2006-01-02", raw); err != nil { return nil, Usage("date value must be YYYY-MM-DD") }
        return raw, nil
    }
    return nil, Usage("unsupported field type: " + typ)
}

func MergeValues(current Object, updates Object) Object {
    out := Object{}
    for k, v := range current { out[k] = v }
    for k, v := range updates { out[k] = v }
    return out
}

func (s *Service) FieldValues(ctx context.Context, kind string, id int64) (Object, error) {
    path, err := ValuePath(kind, id)
    if err != nil { return nil, err }
    resource := "userstories"
    if kind == "task" { resource = "tasks" }
    owner, err := Read(ctx, s.API, fmt.Sprintf("%s/%d", resource, id), nil)
    if err != nil { return nil, err }
    if ID(owner["project"]) != ID(s.Project["id"]) { return nil, Usage("resource belongs to another project") }
    return Read(ctx, s.API, path, nil)
}

func (s *Service) SetFieldValues(ctx context.Context, kind string, id int64, entries []string, dry, force bool) (any, error) {
    if len(entries) == 0 { return nil, Usage("at least one field assignment is required") }
    defs, err := s.Fields(ctx, kind)
    if err != nil { return nil, err }
    updates := Object{}
    for _, entry := range entries {
        name, raw, ok := strings.Cut(entry, "=")
        if !ok || name == "" { return nil, Usage("field assignment must be Name=value") }
        def, err := Resolve(defs, name, "name")
        if err != nil { return nil, err }
        key := fmt.Sprint(def["id"])
        if _, exists := updates[key]; exists { return nil, Usage("duplicate field assignment: " + name) }
        value, err := ParseFieldValue(fmt.Sprint(def["type"]), raw)
        if err != nil { return nil, err }
        updates[key] = value
    }
    before, err := s.FieldValues(ctx, kind, id)
    if err != nil { return nil, err }
    current, ok := before["attributes_values"].(map[string]any)
    if !ok { return nil, fmt.Errorf("invalid attributes_values response") }
    merged := MergeValues(Object(current), updates)
    patch := Object{}
    if !equal(current, merged) { patch["attributes_values"] = merged }
    path, err := ValuePath(kind, id)
    if err != nil { return nil, err }
    return s.Write(ctx, path, before, patch, dry, force)
}
```

No-op não escreve. No conflito em qualquer chave de attributes_values, abortar exit 4 em vez de sobrescrever o dicionário; `--force-version` opt-in permite sobrescrita explícita. Não implementar retry por chave nesta US: a semântica da spec compara o campo alterado, que é o dicionário completo. Não oferecer limpeza null de date/checkbox sem validar representação local; text vazio e checkbox false são valores válidos. Se necessário, decidir sintaxe de unset com humano e documentar, sem interpretar texto `null` como exclusão.

- [ ] **Step 3: CLI de valores e get enriquecido.** `story field list REF` lê story, definições e valores e imprime objeto `{id,ref,url,version,attributes_values,fields}`; aqui version é **do recurso de valores**, não da story. `fields` liga name/type/id ao valor sem descartar ids desconhecidos. `story field set REF "Nome"=valor...` corta no primeiro `=`, resolve todos antes da escrita; flags dry-run/force-version. `story get` acrescenta `custom_attributes` com version próprio e attributes_values, mantendo version da story no topo. Falha de permissão na leitura não vira campos vazios: devolver erro. Valores de task usam serviço acima por id; #253 acrescentará a seleção de ref e comando task field.

Código completo de `story_fields.go`:

```go
package cli

import (
    "fmt"

    "github.com/BasisTI/taiga-cli/internal/app"
    "github.com/spf13/cobra"
)

func (a *App) storyFieldCmd() *cobra.Command {
    parent := &cobra.Command{Use: "field", Short: "Read and merge story custom fields"}
    for _, set := range []bool{false, true} {
        var dry, force bool
        use, short, args := "list REF", "Read story custom field values", cobra.ExactArgs(1)
        if set { use, short, args = "set REF Name=value...", "Merge story custom field values", cobra.MinimumNArgs(2) }
        cmd := &cobra.Command{Use: use, Short: short, Args: args}
        cmd.RunE = func(cmd *cobra.Command, argv []string) error {
            if err := validRef(argv[0]); err != nil { return err }
            service, err := a.service(cmd)
            if err != nil { return err }
            story, err := service.Story(cmd.Context(), argv[0], 0)
            if err != nil { return err }
            var result any
            if set { result, err = service.SetFieldValues(cmd.Context(), "story", app.ID(story["id"]), argv[1:], dry, force) } else {
                values, readErr := service.FieldValues(cmd.Context(), "story", app.ID(story["id"]))
                if readErr != nil { return readErr }
                defs, readErr := service.Fields(cmd.Context(), "story")
                if readErr != nil { return readErr }
                view, viewErr := service.StoryView(story)
                if viewErr != nil { return viewErr }
                result = app.Object{"id": story["id"], "ref": story["ref"], "url": view["url"],
                    "version": values["version"], "attributes_values": values["attributes_values"], "fields": defs}
            }
            if err != nil { return err }
            // For set, return the values resource, preserving its own version.
            if values, ok := result.(app.Object); ok && set {
                view, viewErr := service.StoryView(story)
                if viewErr != nil { return viewErr }
                values["id"], values["ref"], values["url"] = story["id"], story["ref"], view["url"]
                values["resource"] = fmt.Sprintf("userstories/custom-attributes-values/%d", app.ID(story["id"]))
            }
            return a.renderCurated(result)
        }
        if set {
            cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request without writing")
            cmd.Flags().BoolVar(&force, "force-version", false, "override a version conflict once")
        }
        parent.AddCommand(cmd)
    }
    return parent
}
```

Registrar `cmd.AddCommand(a.storyFieldCmd())` em storyCmd. Enriquecimento completo no storyGetCmd, depois de StoryView e antes de renderCurated:

```go
        values, err := service.FieldValues(cmd.Context(), "story", app.ID(raw["id"]))
        if err != nil { return err }
        view["custom_attributes"] = values
```

Não acrescentar GET de valores à seleção interna de story usada por update/comment: não são dependências dessas escritas.

Golden files: `story_fields.json`, `story_fields_dry_run.json`, `story_get_with_fields.json`. Testar text com `=` e controles; várias atribuições; desconhecido/duplicado sem escrita; campos desconhecidos preservados; version 19 de valores versus 7 de story; conflito no dicionário, force, dry-run e releitura efetiva. Integração precisa comprovar que os valores realmente persistiram, evitando o bug do MCP de PATCH ignorado.

Run: `go test -race ./...`
Expected: PASS.

Run futuro: `go test -tags integration -p 1 -run 'Fields|FieldValues' ./internal/cli`
Expected: story e task, todos os tipos validados, dry-run sem alteração, definições e chaves não fornecidas preservadas.

```bash
git add internal/app/field_values.go internal/app/field_values_test.go internal/app/stories.go internal/cli/story.go internal/cli/story_fields.go internal/cli/story_fields_test.go internal/cli/field_values_integration_test.go internal/cli/testdata README.md docs/guia.md docs/errors.md docs/api-notes.md
git commit -m "Implementar valores de campos com merge e version independente"
```

**Gate de PR futuro:** checagens comuns mais integração story/task; título `Implementar definições e valores de campos customizados TG-248`; um PR contendo Tasks 5–6.

## US #250 — Comentários

**Branch:** `TG-250`, depois do merge da #248. **PR:** `Implementar publicação e leitura de comentários TG-250`.
**Descrição consultada:** PATCH comment/version confirmado historicamente; listar history/userstory, separando automáticos do GitLab.

### Task 7: publicar sem repetição cega e listar histórico filtrado

**Files:**
- Create: `internal/app/comments.go`, `internal/app/comments_test.go`, `internal/cli/comments.go`, `internal/cli/comments_test.go`, `internal/cli/comments_integration_test.go`
- Modify: `internal/cli/story.go`, `internal/cli/curated.go` (renderer de comentários), `docs/api-notes.md`, `docs/errors.md`, `README.md`, `docs/guia.md`

**Interfaces:** Produces `Service.Comment(ctx, ref, body string, dry, force bool) (any, error)`, `Service.Comments(ctx, ref string, includeSystem bool) ([]Object, error)`, `SystemComment(Object) bool`.

**Gate:** **validar no Taiga local na Task 7** GET history/userstory/<id>, paginação, estrutura user/id/created_at/comment/type/diff, comentário sem diff, edição/remoção de comentário, identificador de integração e PATCH comment/version. Não inferir “system” somente por type 1, diff vazio ou autor ser conta de serviço; os agentes também publicam comentários humanos com essa conta. A heurística abaixo é provisória para o padrão GitLab já registrado na memória, devendo ser fixada em fixture local sanitizada. Desconhecidos permanecem visíveis.

- [ ] **Step 1: teste completo do filtro conservador.**

```go
package app

import "testing"

func TestSystemCommentConservative(t *testing.T) {
    for _, tc := range []struct { author, body string; want bool }{
        {"gitlab-bot", "This user story has been mentioned in merge request 10", true},
        {"cedric.integration", "Decisão do PO: aceitar o risco", false},
        {"gitlab-bot", "Manual review completed", false},
        {"alice", "This user story has been mentioned in a discussion", false},
        {"", "Unknown integration text", false},
    } {
        o := Object{"user": map[string]any{"username": tc.author}, "comment": tc.body}
        if got := SystemComment(o); got != tc.want { t.Fatalf("%+v: %v", tc, got) }
    }
}
```

Run: `go test ./internal/app -run SystemComment`
Expected: FAIL undefined SystemComment.

- [ ] **Step 2: implementar arquivo completo.**

```go
package app

import (
    "context"
    "fmt"
    "strings"

    "github.com/BasisTI/taiga-cli/internal/taiga"
)

func SystemComment(o Object) bool {
    user, _ := o["user"].(map[string]any)
    username, _ := user["username"].(string)
    body, _ := o["comment"].(string)
    return strings.HasPrefix(username, "gitlab-") && strings.HasPrefix(body, "This user story has been mentioned")
}

func (s *Service) Comment(ctx context.Context, ref, body string, dry, force bool) (any, error) {
    if strings.TrimSpace(body) == "" { return nil, Usage("comment body must not be blank") }
    story, err := s.Story(ctx, ref, 0)
    if err != nil { return nil, err }
    path := fmt.Sprintf("userstories/%d", ID(story["id"]))
    result, err := s.Write(ctx, path, story, Object{"comment": body}, dry, force)
    if err != nil { return nil, err }
    if raw, ok := result.(Object); ok { return s.StoryView(raw) }
    return result, nil
}

func (s *Service) Comments(ctx context.Context, ref string, includeSystem bool) ([]Object, error) {
    story, err := s.Story(ctx, ref, 0)
    if err != nil { return nil, err }
    raws, err := s.API.GetAll(ctx, fmt.Sprintf("history/userstory/%d", ID(story["id"])), nil)
    if err != nil { return nil, taiga.ToOutput(err) }
    out := []Object{}
    for _, raw := range raws {
        o, err := Decode(raw)
        if err != nil { return nil, err }
        body, _ := o["comment"].(string)
        if strings.TrimSpace(body) == "" { continue }
        system := SystemComment(o)
        if system && !includeSystem { continue }
        o["is_system"] = system
        view, err := s.StoryView(story)
        if err != nil { return nil, err }
        o["story_id"], o["story_ref"], o["url"] = story["id"], story["ref"], view["url"]
        out = append(out, o)
    }
    return out, nil
}
```

`Comment` usa a story bruta da Task 3 como baseline; montar a view somente depois da escrita. Se GET story não devolve comment, tratar ausência conforme resposta real e testar conflito. OCC rejeitado é a única condição que permite reaplicar uma vez; rede/5xx nunca. Publicação retorna story relida; leitura retorna array de entradas de histórico com id UUID, user, created_at, comment, is_system e url. Não inventar ref/version para uma entrada de histórico; story_ref e story_id identificam seu pai. Preservar ordem observada e documentá-la; não fazer sort textual de timestamp de formatos diferentes.

- [ ] **Step 3: CLI completo para publicação e leitura.**

```go
package cli

import (
    "strings"
    "github.com/BasisTI/taiga-cli/internal/app"
    "github.com/spf13/cobra"
)

func (a *App) commentCmd() *cobra.Command {
    var body, file string
    var dry, force bool
    cmd := &cobra.Command{Use: "comment REF", Short: "Publish a story comment", Args: cobra.ExactArgs(1)}
    cmd.RunE = func(cmd *cobra.Command, args []string) error {
        hasBody, hasFile := cmd.Flags().Changed("body"), cmd.Flags().Changed("body-file")
        if hasBody == hasFile { return app.Usage("choose exactly one of --body or --body-file") }
        if hasFile {
            var err error
            body, err = a.readContent(file)
            if err != nil { return err }
        }
        if err := validRef(args[0]); err != nil { return err }
        if strings.TrimSpace(body) == "" { return app.Usage("comment body must not be blank") }
        s, err := a.service(cmd)
        if err != nil { return err }
        result, err := s.Comment(cmd.Context(), args[0], body, dry, force)
        if err != nil { return err }
        return a.renderCurated(result)
    }
    cmd.Flags().StringVar(&body, "body", "", "comment text")
    cmd.Flags().StringVar(&file, "body-file", "", "read comment from a file or - for stdin")
    cmd.Flags().BoolVar(&dry, "dry-run", false, "print the request without writing")
    cmd.Flags().BoolVar(&force, "force-version", false, "override a version conflict once")
    return cmd
}

func (a *App) commentsCmd() *cobra.Command {
    var include bool
    cmd := &cobra.Command{Use: "comments REF", Short: "List story comments", Args: cobra.ExactArgs(1)}
    cmd.RunE = func(cmd *cobra.Command, args []string) error {
        if err := validRef(args[0]); err != nil { return err }
        if strings.TrimSpace(body) == "" { return app.Usage("comment body must not be blank") }
        s, err := a.service(cmd)
        if err != nil { return err }
        result, err := s.Comments(cmd.Context(), args[0], include)
        if err != nil { return err }
        return a.renderCurated(result)
    }
    cmd.Flags().BoolVar(&include, "include-system", false, "include recognized integration comments")
    return cmd
}
```

Registrar no storyCmd. Renderer texto distingue comentário de story por `story_ref` e imprime author/created_at/comment/url com WriteFields; JSON preserva campos. Validar selector/body em branco **antes** da autenticação para erro 2 consistente. Golden `comments.json`, `comments.txt`, `comment_dry_run.json`.

- [ ] **Step 4: testes de transporte e integração.** Fake: comentário não mexe em description; body-file e stdin preservam conteúdo; escolha inválida/arquivo ausente/blank não escreve; version aparece no dry-run; 503 gera um PATCH só; 409 com campo comment alterado não repete, alteração só de subject permite uma repetição; lista paginada combina humanos/integração/diff sem texto/autor desconhecido; include-system acrescenta somente os ocultados; todos vazios retornam `[]`.

Run: `go test -race ./...`
Expected: PASS.

Run futuro: `go test -tags integration -p 1 -run Comments -v ./internal/cli`
Expected: publicar comentário único e achá-lo exatamente uma vez no histórico; markdown, multiline e autor preservados. Dry-run não cria entrada. Registrar filtro observado em api-notes.

```bash
git add internal/app/comments.go internal/app/comments_test.go internal/cli/comments.go internal/cli/comments_test.go internal/cli/comments_integration_test.go internal/cli/story.go internal/cli/curated.go internal/cli/testdata README.md docs/guia.md docs/errors.md docs/api-notes.md
git commit -m "Implementar comentários de stories e filtro de integrações"
```

**Gate de PR futuro:** checagens comuns e histórico local; título `Implementar publicação e leitura de comentários TG-250`; um PR.

## US #249 — Projeto como código: status e campos

**Branch:** `TG-249`, depois do merge da #250. **PR:** `Implementar plano e aplicação de status e campos TG-249`.
**Descrição consultada:** listar/criar/reordenar status de story, com plan/apply idempotentes; absorver status In revision/Waiting for deployment e campos do fluxo usados pelo configurar-taiga-projeto.sh. Não executar esse script nem consultar suas credenciais.

### Task 8: TOML estrito e plano determinístico sem remoção

**Files:**
- Create: `internal/app/project_spec.go`, `internal/app/project_plan.go`, `internal/app/project_plan_test.go`, `internal/cli/project.go`, `internal/cli/project_test.go`, `docs/examples/taiga-project.toml`
- Modify: `internal/cli/root.go` (registrar project), `docs/errors.md`

**Interfaces:** Produces `ProjectSpec`, `StatusSpec`, `FieldSpec`, `ParseProjectSpec(io.Reader) (ProjectSpec, error)`, `BuildProjectPlan(spec ProjectSpec, statuses, fields []Object) (ProjectPlan, error)`; `ProjectPlan{Actions []Action; Unmanaged []Object; Drift []Object}`; `Action{Kind, Name string; Body Object}`. O plano é semântico, sem ids fictícios para status novos; Task 9 materializa ids e versions reais.

**Política:** só criar e reordenar. Definição existente de mesmo nome com color/closed/type divergente é drift explícito, nunca update implícito. Apply recusa drift no alvo declarado, exit 2; é o humano quem decide reconciliar ou corrigir TOML. Itens existentes ausentes do arquivo ficam em Unmanaged e mantêm ordem relativa. Nome case-sensitive; duplicados locais/remotos são erro. `after` pode apontar a item existente fora do arquivo ou novo do próprio arquivo; referência inexistente, ciclo e self-reference são erro antes da escrita. Sem after não reposicionar existente; novo sem after vai ao fim na ordem do arquivo. Duas declarações depois do mesmo predecessor preservam ordem do TOML.

- [ ] **Step 1: teste de TOML e ordenação completo.**

```go
package app

import (
    "reflect"
    "strings"
    "testing"
)

func TestProjectSpecStrictAndStableOrder(t *testing.T) {
    input := `[[story_status]]
name = "In revision"
color = "#8E44AD"
closed = false
after = "In progress"
[[story_field]]
name = "Testado em staging"
type = "checkbox"
description = "Registro de teste da versão entregue"
`
    spec, err := ParseProjectSpec(strings.NewReader(input))
    if err != nil { t.Fatal(err) }
    got, err := OrderStatuses([]string{"New", "In progress", "Done", "Extra"}, spec.StoryStatus)
    want := []string{"New", "In progress", "In revision", "Done", "Extra"}
    if err != nil || !reflect.DeepEqual(got, want) { t.Fatalf("%v %v", got, err) }
    if _, err := ParseProjectSpec(strings.NewReader("unknown = true")); err == nil { t.Fatal("unknown key") }
    bad := []StatusSpec{{Name: "A", After: "B"}, {Name: "B", After: "A"}}
    if _, err := OrderStatuses(nil, bad); err == nil { t.Fatal("cycle") }
    if _, err := OrderStatuses([]string{"A"}, []StatusSpec{{Name: "B", After: "missing"}}); err == nil {
        t.Fatal("missing predecessor")
    }
}
```

Run: `go test ./internal/app -run ProjectSpec`
Expected: FAIL undefined ParseProjectSpec.

- [ ] **Step 2: implementar parser completo em `project_spec.go`.**

```go
package app

import (
    "fmt"
    "io"
    "regexp"
    "strings"

    toml "github.com/pelletier/go-toml/v2"
)

type StatusSpec struct {
    Name string `toml:"name"`
    Color string `toml:"color"`
    Closed bool `toml:"closed"`
    After string `toml:"after"`
}

type FieldSpec struct {
    Name string `toml:"name"`
    Type string `toml:"type"`
    Description string `toml:"description"`
}

type ProjectSpec struct {
    StoryStatus []StatusSpec `toml:"story_status"`
    StoryField []FieldSpec `toml:"story_field"`
}

func ParseProjectSpec(r io.Reader) (ProjectSpec, error) {
    var spec ProjectSpec
    d := toml.NewDecoder(r).DisallowUnknownFields()
    if err := d.Decode(&spec); err != nil { return spec, Usage("invalid project TOML: " + err.Error()) }
    seen := map[string]bool{}
    color := regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
    for _, st := range spec.StoryStatus {
        if strings.TrimSpace(st.Name) == "" || !color.MatchString(st.Color) || st.Name == st.After {
            return spec, Usage("invalid status definition: " + st.Name)
        }
        if seen[st.Name] { return spec, Usage("duplicate status: " + st.Name) }
        seen[st.Name] = true
    }
    seen = map[string]bool{}
    for _, f := range spec.StoryField {
        if err := ValidateField(f.Name, f.Type); err != nil { return spec, err }
        if seen[f.Name] { return spec, Usage("duplicate field: " + f.Name) }
        seen[f.Name] = true
    }
    return spec, nil
}

func OrderStatuses(current []string, specs []StatusSpec) ([]string, error) {
    order := append([]string(nil), current...)
    exists, declared := map[string]bool{}, map[string]bool{}
    for _, name := range order {
        if exists[name] { return nil, Usage("duplicate remote status: " + name) }
        exists[name] = true
    }
    for _, st := range specs {
        if declared[st.Name] { return nil, Usage("duplicate status: " + st.Name) }
        declared[st.Name] = true
        if !exists[st.Name] { order = append(order, st.Name); exists[st.Name] = true }
    }
    after := map[string]string{}
    for _, st := range specs {
        if st.After != "" {
            if !exists[st.After] { return nil, Usage("unknown predecessor: " + st.After) }
            after[st.Name] = st.After
        }
    }
    mark := map[string]int{}
    var check func(string) error
    check = func(name string) error {
        if mark[name] == 1 { return Usage("cycle in status order: " + name) }
        if mark[name] == 2 { return nil }
        mark[name] = 1
        if prev := after[name]; prev != "" { if err := check(prev); err != nil { return err } }
        mark[name] = 2
        return nil
    }
    for name := range after { if err := check(name); err != nil { return nil, err } }
    // Stable DFS: roots follow existing order; explicit siblings follow TOML order.
    children := map[string][]string{}
    for _, st := range specs {
        if st.After != "" { children[st.After] = append(children[st.After], st.Name) }
    }
    out := []string{}
    var emit func(string)
    emit = func(name string) {
        out = append(out, name)
        for _, child := range children[name] { emit(child) }
    }
    for _, name := range order { if after[name] == "" { emit(name) } }
    if len(out) != len(order) { return nil, fmt.Errorf("invalid status ordering") }
    return out, nil
}
```

- [ ] **Step 3: implementar plano completo em `project_plan.go`.** Status recebido já vem ordenado por order numérico/id (não pela ordem de rede); defs e status são catálogos confirmados da Task 9.

```go
package app

import (
    "fmt"
    "reflect"
)

type Action struct {
    Kind string `json:"kind"`
    Name string `json:"name"`
    Body Object `json:"body"`
}

type ProjectPlan struct {
    Actions []Action `json:"actions"`
    Unmanaged []Object `json:"unmanaged"`
    Drift []Object `json:"drift"`
}

func indexNames(items []Object) (map[string]Object, error) {
    out := map[string]Object{}
    for _, item := range items {
        name, ok := item["name"].(string)
        if !ok || name == "" { return nil, Usage("invalid catalog name") }
        if _, exists := out[name]; exists { return nil, Usage("duplicate catalog name: " + name) }
        out[name] = item
    }
    return out, nil
}

func BuildProjectPlan(spec ProjectSpec, statuses, fields []Object) (ProjectPlan, error) {
    plan := ProjectPlan{Actions: []Action{}, Unmanaged: []Object{}, Drift: []Object{}}
    sts, err := indexNames(statuses)
    if err != nil { return plan, err }
    fs, err := indexNames(fields)
    if err != nil { return plan, err }
    wantedSt, wantedF := map[string]bool{}, map[string]bool{}
    current := []string{}
    for _, st := range statuses { current = append(current, fmt.Sprint(st["name"])) }
    order, err := OrderStatuses(current, spec.StoryStatus)
    if err != nil { return plan, err }
    for _, st := range spec.StoryStatus {
        wantedSt[st.Name] = true
        body := Object{"name": st.Name, "color": st.Color, "is_closed": st.Closed}
        old, found := sts[st.Name]
        if !found {
            plan.Actions = append(plan.Actions, Action{"create_status", st.Name, body})
        } else if old["color"] != st.Color || old["is_closed"] != st.Closed {
            plan.Drift = append(plan.Drift, Object{"kind": "status", "name": st.Name, "actual": old, "desired": body})
        }
    }
    for _, f := range spec.StoryField {
        wantedF[f.Name] = true
        body := Object{"name": f.Name, "type": f.Type, "description": f.Description}
        old, found := fs[f.Name]
        if !found {
            plan.Actions = append(plan.Actions, Action{"create_field", f.Name, body})
        } else if old["type"] != f.Type || old["description"] != f.Description {
            plan.Drift = append(plan.Drift, Object{"kind": "field", "name": f.Name, "actual": old, "desired": body})
        }
    }
    projected := append([]string{}, current...)
    for _, st := range spec.StoryStatus {
        if _, exists := sts[st.Name]; !exists { projected = append(projected, st.Name) }
    }
    if !reflect.DeepEqual(projected, order) {
        plan.Actions = append(plan.Actions, Action{"reorder_statuses", "", Object{"names": order}})
    }
    for _, st := range statuses {
        if !wantedSt[fmt.Sprint(st["name"])] { plan.Unmanaged = append(plan.Unmanaged, Object{"kind": "status", "value": st}) }
    }
    for _, f := range fields {
        if !wantedF[fmt.Sprint(f["name"])] { plan.Unmanaged = append(plan.Unmanaged, Object{"kind": "field", "value": f}) }
    }
    return plan, nil
}
```

TOML de exemplo completo (`docs/examples/taiga-project.toml`):

```toml
[[story_status]]
name = "In revision"
color = "#8E44AD"
closed = false
after = "In progress"

[[story_status]]
name = "Waiting for deployment"
color = "#3498DB"
closed = false
after = "In revision"

[[story_field]]
name = "Testado em staging"
type = "checkbox"
description = "Registro de teste da versão entregue"
```

Esses nomes/valores são exemplo explícito de configuração, não valores obrigatórios embutidos na aplicação. Conferir com humano a convenção desejada para cores e demais campos do fluxo antes de migrar o script na #254.

- [ ] **Step 4: testes e CLI de plan.** `project plan -f FILE|-` lê TOML via parser estrito; projeto via RunContext; lê status/campos; imprime ProjectPlan, sem POST/PATCH. Plan exige somente leitura, **validar no Taiga local na Task 9** se a API permite ler catálogos sem admin_project_values. `project apply -f` pode ser registrado aqui, mas implementação é Task 9; não entregar stub em PR. Plan não aceita `--force-version`: não altera recurso. Mesmo arquivo aplicado de novo deve produzir actions=[] preservando unmanaged. Ordem de status estável por order e id; não reorder se arquivo só declara campos.

Testes adicionais: after para novo posterior no TOML, chain com três, dois filhos, cycle, self-reference, predecessor externo, catálogo remoto duplicado, unknown key, color inválida, arquivo ausente, closed=false, drift de type/color/closed/description, preservação de extras, TOML vazio → zero ações. Golden `project_plan.json`/`project_plan.txt` com actions/unmanaged/drift explícitos.

Run: `go test -race ./internal/app ./internal/cli`
Expected: PASS e nenhuma escrita em plan.

```bash
git add internal/app/project_spec.go internal/app/project_plan.go internal/app/project_plan_test.go internal/cli/project.go internal/cli/project_test.go internal/cli/root.go internal/cli/testdata docs/examples/taiga-project.toml docs/errors.md
git commit -m "Implementar plano determinístico de status e campos do projeto"
```

### Task 9: validar status/permissões/OCC e executar apply retomável

**Files:**
- Create: `internal/app/project_apply.go`, `internal/app/project_status_writer.go`, `internal/app/project_apply_test.go`, `internal/cli/status.go`, `internal/cli/project_integration_test.go`, `internal/cli/project_probe_integration_test.go`
- Modify: `internal/cli/project.go`, `internal/cli/root.go`, `docs/api-notes.md`, `docs/errors.md`, `README.md`, `docs/guia.md`

**Interfaces:** Produces `Service.ProjectPlan(ctx context.Context, spec ProjectSpec) (ProjectPlan, error)`, `Service.ApplyProject(ctx context.Context, spec ProjectSpec, dry, force bool) (ApplyResult, error)`, `ApplyResult{Plan ProjectPlan; Applied []Action; Remaining []Action; Complete bool}` e `StatusWriter` abaixo. Produces CLI `status list --kind story|task` e project apply.

**Gate obrigatório:** **validar no Taiga local na Task 9**:

1. `userstory-statuses?project=ID`, `task-statuses?project=ID`, GET id e POST; nomes de propriedades is_closed/color/order, versão ou ausência dela, ordenação e incremento observado.
2. `admin_project_values`: onde aparece a permissão efetiva (project.my_permissions ou equivalente), admin e membro sem permissão, erro real 403. O cliente preflight nunca substitui o 403 do servidor. Ausência do campo de permissão não significa permissão concedida.
3. Reordenação por PATCH individual/order ou rota bulk_update_order; atomicidade e OCC real. Não assumir version só porque ela existe em story. Provar conflito por escrita concorrente em status e se version de projeto protege o catálogo.
4. Criação de status/campo: não existe versão anterior; registrar contrato e evitar repetição em erro de transporte. Sem unicidade remota, idempotência é sequencial; corridas produzem erro claro, não escolhas silenciosas.

**Decisão que pode bloquear esta tarefa:** se a reordenação do Taiga 6.7 não tiver OCC verificável, não inventar version nem marcar apply concluído. Manter plan e criação disponíveis, recusar reorder com `unsupported_operation` e pedir ao humano decisão concreta: aceitar operação não versionada com checagem antes/depois (não garante atomicidade), implementar suporte de OCC no servidor, ou adiar reordenação. Até decisão, nenhuma implementação remove a proteção de version. Este gate exige humano somente se a sondagem demonstrar a limitação; no planejamento atual isso é pendência futura, não pedido de autorização para implementar.

- [ ] **Step 1: escrever teste de aplicação sem escrita em drift/dry-run.** Fake StatusWriter mantém estado em memória e conta chamadas. Contrato completo do resultado e adapter em `project_apply.go`:

```go
package app

import "context"

type ApplyResult struct {
    Plan ProjectPlan `json:"plan"`
    Applied []Action `json:"applied"`
    Remaining []Action `json:"remaining"`
    Complete bool `json:"complete"`
    Requests []WritePlan `json:"requests"`
    Deferred []Action `json:"deferred"`
}

type StatusWriter interface {
    // These methods use contracts proved by Task 9, never guessed paths/bodies.
    CheckPermission(context.Context) error
    ProjectID() any
    Load(context.Context) (statuses, fields []Object, err error)
    CheckReorderSupport(context.Context) error
    CreateStatus(context.Context, Object) error
    CreateField(context.Context, Object) error
    Reorder(context.Context, []string, bool) error
}

func Apply(ctx context.Context, writer StatusWriter, spec ProjectSpec, dry, force bool) (ApplyResult, error) {
    result := ApplyResult{Applied: []Action{}, Remaining: []Action{}, Requests: []WritePlan{}, Deferred: []Action{}}
    if err := writer.CheckPermission(ctx); err != nil { return result, err }
    statuses, fields, err := writer.Load(ctx)
    if err != nil { return result, err }
    plan, err := BuildProjectPlan(spec, statuses, fields)
    result.Plan = plan
    result.Remaining = append(result.Remaining, plan.Actions...)
    if err != nil { return result, err }
    if len(plan.Drift) > 0 { return result, Usage("project definitions differ; review plan drift before applying") }
    for _, action := range plan.Actions {
        if action.Kind == "reorder_statuses" {
            if err := writer.CheckReorderSupport(ctx); err != nil { return result, err }
        }
    }
    result.Requests, result.Deferred, err = PreviewProject(plan, statuses, writer.ProjectID())
    if err != nil { return result, err }
    if dry { return result, nil }
    for i, action := range plan.Actions {
        switch action.Kind {
        case "create_status": err = writer.CreateStatus(ctx, action.Body)
        case "create_field": err = writer.CreateField(ctx, action.Body)
        case "reorder_statuses":
            names, ok := action.Body["names"].([]string)
            if !ok { return result, Usage("invalid reorder plan") }
            err = writer.Reorder(ctx, names, force)
        default: return result, Usage("unknown project action: " + action.Kind)
        }
        if err != nil { return result, err }
        result.Applied = append(result.Applied, action)
        result.Remaining = append([]Action{}, plan.Actions[i+1:]...)
    }
    statuses, fields, err = writer.Load(ctx)
    if err != nil { return result, err }
    after, err := BuildProjectPlan(spec, statuses, fields)
    if err != nil { return result, err }
    result.Remaining = after.Actions
    result.Complete = len(after.Actions) == 0 && len(after.Drift) == 0
    if !result.Complete { return result, Usage("project changed during apply; review a new plan") }
    return result, nil
}
```

Teste completo de erro antes da primeira escrita (`project_apply_test.go`):

```go
package app

import (
    "context"
    "errors"
    "testing"
)

type applyFake struct { writes int; denied bool; statuses, fields []Object }
func (f *applyFake) CheckPermission(context.Context) error {
    if f.denied { return errors.New("admin_project_values required") }
    return nil
}
func (f *applyFake) ProjectID() any { return 37 }
func (f *applyFake) CheckReorderSupport(context.Context) error { return nil }
func (f *applyFake) Load(context.Context) ([]Object, []Object, error) { return f.statuses, f.fields, nil }
func (f *applyFake) CreateStatus(context.Context, Object) error { f.writes++; return nil }
func (f *applyFake) CreateField(context.Context, Object) error { f.writes++; return nil }
func (f *applyFake) Reorder(context.Context, []string, bool) error { f.writes++; return nil }

func TestApplyPreflightAndDryRunNeverWrite(t *testing.T) {
    spec := ProjectSpec{StoryStatus: []StatusSpec{{Name: "In revision", Color: "#8E44AD"}}}
    for _, dry := range []bool{true, false} {
        f := &applyFake{statuses: []Object{{"name": "In revision", "color": "#000000", "is_closed": false}}}
        if _, err := Apply(context.Background(), f, spec, dry, false); err == nil || f.writes != 0 {
            t.Fatalf("drift: writes=%d err=%v", f.writes, err)
        }
    }
    f := &applyFake{}
    got, err := Apply(context.Background(), f, spec, true, false)
    if err != nil || f.writes != 0 || len(got.Plan.Actions) != 1 || got.Complete {
        t.Fatalf("dry: %+v writes=%d err=%v", got, f.writes, err)
    }
    f = &applyFake{denied: true}
    if _, err := Apply(context.Background(), f, spec, false, false); err == nil || f.writes != 0 {
        t.Fatalf("permission: %d %v", f.writes, err)
    }
}
```

Run antes de adicionar Apply: `go test ./internal/app -run Apply`
Expected: FAIL undefined Apply.

Materialização completa da prévia a acrescentar em `project_apply.go` (imports fmt): `ApplyResult` ganha `Requests []WritePlan` e `Deferred []Action` com tags json `requests`/`deferred`. Depois do preflight e antes de `if dry`, chamar `PreviewProject`, atribuir as duas listas e devolver erro se houver. `statuses` é a leitura que gerou plan. Requests contém apenas requests cujo corpo/path já podem ser conhecidos; Deferred explica as operações que precisam do id e version criados pelo POST. Não enviar nada para produzir uma prévia.

```go
func PreviewProject(plan ProjectPlan, statuses []Object, projectID any) ([]WritePlan, []Action, error) {
    requests, deferred := []WritePlan{}, []Action{}
    index, err := indexNames(statuses)
    if err != nil { return nil, nil, err }
    for _, action := range plan.Actions {
        switch action.Kind {
        case "create_status", "create_field":
            path := "userstory-statuses"
            if action.Kind == "create_field" { path = "userstory-custom-attributes" }
            body := Object{"project": projectID}
            for k, v := range action.Body { body[k] = v }
            requests = append(requests, WritePlan{true, "POST", path, body})
        case "reorder_statuses":
            names, ok := action.Body["names"].([]string)
            if !ok { return nil, nil, Usage("invalid reorder plan") }
            for i, name := range names {
                st, exists := index[name]
                if !exists {
                    deferred = append(deferred, Action{"reorder_created_status", name, Object{"order": i, "depends_on": "create_status"}})
                    continue
                }
                if ID(st["order"]) == int64(i) { continue }
                version, ok := st["version"]
                if !ok { return nil, nil, Usage("status ordering requires a version") }
                requests = append(requests, WritePlan{true, "PATCH", fmt.Sprintf("userstory-statuses/%d", ID(st["id"])),
                    Object{"order": i, "version": version}})
            }
        }
    }
    return requests, deferred, nil
}
```

Acrescentar `ProjectID() any` a StatusWriter, implementado no HTTP por `return w.service.Project["id"]` e no fake por `return 37`. Integração completa em Apply, depois de CheckReorderSupport e antes de `if dry`:

```go
    result.Requests, result.Deferred, err = PreviewProject(plan, statuses, writer.ProjectID())
    if err != nil { return result, err }
```

Esse trecho usa o mesmo gate de order/rota/version da Task 9; se a sondagem exigir bulk versionado, substituir também PreviewProject para manter a prévia fiel ao adapter. Criação ao fim sem `after` explícito não deve gerar reorder artificial: comparar a ordem desejada à ordem projetada após os POSTs, e confirmar a ordem efetiva na releitura.

- [ ] **Step 2: implementar adapter HTTP depois do gate.** `StatusWriter` deve:

- `CheckPermission`: verificar a permissão efetiva comprovada, ou consulta equivalente; retornar `output.Error{Code:"forbidden", Source:"api", Cause:"admin_project_values permission is required", Exit:6}`. Executar também em dry-run de apply; plan permanece leitura.
- `Load`: invalidar caches e GetAll de status/definições, ordenar numericamente por order/id, sem GET global; detectar duplicatas antes da primeira escrita.
- `CreateStatus`: reler catálogo e conferir definição/nome antes do POST, acrescentar project ao body, GET de confirmação após sucesso, nunca repetir POST por rede. Se existente compatível, no-op; incompatível/ambíguo, erro.
- `CreateField`: reutilizar CreateField da Task 5, com descrição explícita do TOML; não duplicar lógica.
- `Reorder`: reler catálogo inteiro, resolver nomes → ids reais, conferir itens extras e versões contra snapshot inicial; aplicar o mecanismo comprovado no gate. Se PATCH individual versionado funciona, escrever só order por `WriteVersionedFrom`, usando snapshot completo de cada status; uma repetição guardada e no máximo uma por alteração. Se bulk é único mecanismo e não tem OCC, bloquear conforme decisão acima. Não passar version do projeto arbitrariamente.
- Reordenar também os extras quando for necessário deslocar order, mantendo sua ordem relativa e valores color/closed intactos; só altera order. Falha parcial não desfaz POST/PATCH com DELETE ou rollback cego. Interromper, informar ações já aplicadas e pendentes; nova invocação relê e recalcula.

Adapter completo **condicional** em `internal/app/project_status_writer.go`. Criar esse arquivo e incluí-lo no commit da Task 9. As hipóteses de rota/permissão são marcadas no gate; se divergem, alterar somente este adapter e seus testes. A constante fica false até prova local de PATCH order com version; não “corrigir” testes ligando-a sem evidência. Não há stub de sucesso: o caminho não comprovado retorna unsupported_operation. Sem comprovação, a US permanece pendente de decisão humana.

```go
package app

import (
    "context"
    "fmt"
    "sort"

    "github.com/BasisTI/taiga-cli/internal/output"
    "github.com/BasisTI/taiga-cli/internal/taiga"
)

// Gate Task 9: set true only after local evidence confirms versioned PATCH order.
const versionedStatusOrderValidated = false

type statusHTTPWriter struct {
    service *Service
    baseline map[string]Object
}

func (w *statusHTTPWriter) ProjectID() any { return w.service.Project["id"] }

func (w *statusHTTPWriter) CheckPermission(ctx context.Context) error {
    project, err := Read(ctx, w.service.API, fmt.Sprintf("projects/%d", ID(w.service.Project["id"])), nil)
    if err != nil { return err }
    permissions, ok := project["my_permissions"].([]any) // validate this shape in Task 9
    if ok {
        for _, p := range permissions { if p == "admin_project_values" { return nil } }
    }
    return &output.Error{Code: "forbidden", Source: "api", Cause: "admin_project_values permission is required", Exit: output.ExitForbidden}
}

func (w *statusHTTPWriter) Load(ctx context.Context) ([]Object, []Object, error) {
    delete(w.service.catalogs, "userstory-statuses")
    delete(w.service.catalogs, "userstory-custom-attributes")
    statuses, err := w.service.Catalog(ctx, "userstory-statuses")
    if err != nil { return nil, nil, err }
    sort.SliceStable(statuses, func(i, j int) bool {
        a, b := ID(statuses[i]["order"]), ID(statuses[j]["order"])
        if a == b { return ID(statuses[i]["id"]) < ID(statuses[j]["id"]) }
        return a < b
    })
    fields, err := w.service.Fields(ctx, "story")
    if err != nil { return nil, nil, err }
    if w.baseline == nil {
        w.baseline = map[string]Object{}
        for _, st := range statuses { w.baseline[fmt.Sprint(st["name"])] = st }
    }
    return statuses, fields, nil
}

func (w *statusHTTPWriter) CheckReorderSupport(ctx context.Context) error {
    if !versionedStatusOrderValidated {
        return &output.Error{Code: "unsupported_operation", Cause: "versioned status ordering has not been validated",
            Recovery: "validate the Taiga local contract before applying status ordering", Exit: output.ExitUsage}
    }
    for _, st := range w.baseline {
        if _, exists := st["version"]; !exists {
            return &output.Error{Code: "unsupported_operation", Cause: "status resource has no version", Exit: output.ExitUsage}
        }
    }
    return nil
}

func (w *statusHTTPWriter) CreateStatus(ctx context.Context, desired Object) error {
    statuses, _, err := w.Load(ctx)
    if err != nil { return err }
    if _, err := indexNames(statuses); err != nil { return err }
    for _, st := range statuses {
        if st["name"] != desired["name"] { continue }
        if st["color"] != desired["color"] || st["is_closed"] != desired["is_closed"] {
            return Usage("status definition changed concurrently")
        }
        return nil
    }
    body := Object{"project": w.service.Project["id"]}
    for k, v := range desired { body[k] = v }
    r, err := w.service.API.Do(ctx, taiga.Request{Method: "POST", Path: "userstory-statuses", Body: body})
    if err != nil { return taiga.ToOutput(err) }
    created, err := Decode(r.Body)
    if err != nil { return err }
    confirmed, err := Read(ctx, w.service.API, fmt.Sprintf("userstory-statuses/%d", ID(created["id"])), nil)
    if err != nil { return err }
    w.baseline[fmt.Sprint(confirmed["name"])] = confirmed
    return nil
}

func (w *statusHTTPWriter) CreateField(ctx context.Context, desired Object) error {
    description, _ := desired["description"].(string)
    _, err := w.service.CreateField(ctx, "story", fmt.Sprint(desired["name"]), fmt.Sprint(desired["type"]), &description, false)
    return err
}

func (w *statusHTTPWriter) Reorder(ctx context.Context, names []string, force bool) error {
    if err := w.CheckReorderSupport(ctx); err != nil { return err }
    statuses, _, err := w.Load(ctx)
    if err != nil { return err }
    index, err := indexNames(statuses)
    if err != nil { return err }
    if len(index) != len(names) { return &taiga.ConflictError{Method: "PATCH", Path: "userstory-statuses", Fields: []string{"catalog"}} }
    for _, name := range names {
        current, exists := index[name]
        old, known := w.baseline[name]
        if !exists || !known || ID(current["id"]) != ID(old["id"]) {
            return &taiga.ConflictError{Method: "PATCH", Path: "userstory-statuses", Fields: []string{"catalog"}}
        }
        if !force && (!equal(old["order"], current["order"]) || !equal(old["color"], current["color"]) || !equal(old["is_closed"], current["is_closed"])) {
            return &taiga.ConflictError{Method: "PATCH", Path: "userstory-statuses", Fields: []string{"order", "color", "is_closed"}}
        }
    }
    for i, name := range names {
        old := w.baseline[name]
        if ID(old["order"]) == int64(i) { continue } // Task 9 proves order base/spacing
        raw, err := Snapshot(old)
        if err != nil { return err }
        _, err = w.service.API.WriteVersionedFrom(ctx, "PATCH", fmt.Sprintf("userstory-statuses/%d", ID(old["id"])),
            map[string]any{"order": i}, raw, force)
        if err != nil { return taiga.ToOutput(err) }
    }
    return nil
}

func (s *Service) ProjectPlan(ctx context.Context, spec ProjectSpec) (ProjectPlan, error) {
    writer := &statusHTTPWriter{service: s}
    statuses, fields, err := writer.Load(ctx)
    if err != nil { return ProjectPlan{}, err }
    return BuildProjectPlan(spec, statuses, fields)
}

func (s *Service) ApplyProject(ctx context.Context, spec ProjectSpec, dry, force bool) (ApplyResult, error) {
    result, err := Apply(ctx, &statusHTTPWriter{service: s}, spec, dry, force)
    if err != nil { return result, taiga.ToOutput(err) }
    return result, nil
}
```

**Limites desse corpo:** order zero-based e atualização individual só entram após comprovação da Task 9; se Taiga exigir bulk/outra propriedade, substituir Reorder pelo contrato provado sem perder OCC. Entre preflight e cada PATCH, a comparação do núcleo protege order; uma mudança só em color/closed depois do preflight é preservada porque o patch só altera order. O resultado completo é confirmado por releitura/novo plan. Quando o catálogo remoto permite nomes duplicados, CreateStatus deve chamar indexNames antes do loop e tratar a corrida conforme gate; nunca aceitar primeiro match de um catálogo ambíguo.

- [ ] **Step 3: CLI de apply/status e erro parcial.** `project apply -f FILE|- [--dry-run] [--force-version]`; resultado JSON com plan/applied/remaining/complete; em falha parcial imprimir resultado no stdout **e** erro no stderr com exit original. Nenhuma mensagem “complete” antes de releitura confirmar actions=[] e drift=[]. Dry-run imprime plano de ações com método/caminho e corpo versionado para recursos existentes; acrescentar materialização da prévia abaixo ao ApplyResult; status novo aparece como referência simbólica, sem id/version fictícios, e sem POST para obter ids. Não tratar dry-run de apply como plano executável persistido: apply recalcula a cada invocação, não aceita aplicar um JSON antigo.

`status list --kind story|task` lê o catálogo validado; saída com id/name/is_closed/color/order/version quando fornecida, texto por WriteFields. Registrar root.AddCommand(a.statusCmd()). `project list|get` permanece #253; comando project nesta US contém plan/apply.

Código completo de `project.go`; consome os métodos Service.ProjectPlan/ApplyProject definidos nesta tarefa. Ambos constroem o adapter validado e chamam BuildProjectPlan/Apply; não consultar auth diretamente.

```go
package cli

import (
    "strings"

    "github.com/BasisTI/taiga-cli/internal/app"
    "github.com/spf13/cobra"
)

func (a *App) projectCmd() *cobra.Command {
    parent := &cobra.Command{Use: "project", Short: "Plan and apply project configuration"}
    for _, apply := range []bool{false, true} {
        var file string
        var dry, force bool
        use, short := "plan", "Compare project configuration without writing"
        if apply { use, short = "apply", "Apply project configuration idempotently" }
        cmd := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs}
        cmd.RunE = func(cmd *cobra.Command, _ []string) error {
            if file == "" { return app.Usage("-f is required") }
            content, err := a.readContent(file)
            if err != nil { return err }
            spec, err := app.ParseProjectSpec(strings.NewReader(content))
            if err != nil { return err }
            service, err := a.service(cmd)
            if err != nil { return err }
            if !apply {
                plan, err := service.ProjectPlan(cmd.Context(), spec)
                if err != nil { return err }
                return a.renderCurated(plan)
            }
            result, applyErr := service.ApplyProject(cmd.Context(), spec, dry, force)
            if err := a.renderCurated(result); err != nil { return err }
            return applyErr // keeps partial result on stdout and original exit/error on stderr
        }
        cmd.Flags().StringVarP(&file, "file", "f", "", "project TOML file or - for stdin")
        if apply {
            cmd.Flags().BoolVar(&dry, "dry-run", false, "print actions without writing")
            cmd.Flags().BoolVar(&force, "force-version", false, "override a version conflict once")
        }
        parent.AddCommand(cmd)
    }
    return parent
}
```

Na Task 8 registrar só plan enquanto o apply ainda não está implementado: retirar `true` do slice, sem entregar stub. Na Task 9 usar o bloco integral. Métodos completos para catálogo de status em `status.go`:

```go
package cli

import (
    "github.com/BasisTI/taiga-cli/internal/app"
    "github.com/spf13/cobra"
)

func (a *App) statusCmd() *cobra.Command {
    parent := &cobra.Command{Use: "status", Short: "Inspect project statuses"}
    var kind string
    list := &cobra.Command{Use: "list", Short: "List project statuses", Args: cobra.NoArgs}
    list.RunE = func(cmd *cobra.Command, _ []string) error {
        path := "userstory-statuses"
        switch kind {
        case "story":
        case "task": path = "task-statuses"
        default: return app.Usage("--kind must be story or task")
        }
        service, err := a.service(cmd)
        if err != nil { return err }
        statuses, err := service.Catalog(cmd.Context(), path)
        if err != nil { return err }
        return a.renderCurated(statuses)
    }
    list.Flags().StringVar(&kind, "kind", "story", "story or task")
    parent.AddCommand(list)
    return parent
}
```

Run: `go test -race ./...`
Expected: PASS.

- [ ] **Step 4: integração, idempotência e falha parcial.** Testar:

- conta sem admin: exit 6 e zero POST/PATCH, inclusive dry-run;
- admin: criar In revision/Waiting for deployment e checkbox, ordenar, reler;
- segunda aplicação: nenhuma escrita, actions=[];
- item extra ausente no TOML continua existindo e na ordem relativa;
- criação interrompida na segunda ação: informar primeira aplicada, nenhuma repetição/rollback; após restaurar API, nova aplicação completa sem duplicar;
- concorrência em nome/ordem/closed/cor: não sobrescrever sem force; catálogo novo inserido durante apply força novo plan;
- dry-run, ciclo e drift não alteram o projeto;
- esquema de status sem version é recusado no gate, teste explícito.

Run futuro: `go test -tags integration -p 1 -run 'Project|Status' -v ./internal/cli`
Expected: PASS somente com contrato OCC resolvido; caso não resolvido, a entrega de reorder permanece bloqueada e é reportada como pendência, não como concluída.

```bash
git add internal/app/project_apply.go internal/app/project_status_writer.go internal/app/project_apply_test.go internal/cli/project.go internal/cli/status.go internal/cli/root.go internal/cli/project_integration_test.go internal/cli/project_probe_integration_test.go internal/cli/testdata README.md docs/guia.md docs/errors.md docs/api-notes.md
git commit -m "Implementar apply retomável e catálogos de status do projeto"
```

### Task 10: documentação de fase 2 e gate do PR #249

**Files:**
- Modify: `README.md`, `docs/guia.md`, `docs/errors.md`, `docs/api-notes.md`
- Create: `docs/fase-2-validacao.md`

- [ ] **Step 1: documentar superfície final e validações.** README inglês e guia PT-BR: todos os comandos/flags de fase 2, JSON/texto, URL, assigned_users versus assigned_to, block/unblock, dicionário completo/version separado, arquivo/stdin, comentário automático, plan/apply/idempotência/falha parcial, sem delete. Exemplos usam `taiga.example.com` e refs de exemplo; testes usam loopback. Errors inclui usage/ambiguous_name/not_found/field_definition_conflict/unsupported_operation/version_conflict/forbidden e os envelopes existentes. Não mudar a receita de auth/headless da #244.

Registrar `docs/fase-2-validacao.md` com US/commit/testes/resultados/contratos pendentes, sem tokens ou dados de projetos reais. api-notes distingue “confirmado localmente”, “evidência histórica” e “pendente”; nunca transcrever senha, headers Authorization ou URLs de mídia com tokens.

- [ ] **Step 2: rodar verificação comum de todas as US.**

```bash
gofmt -l internal cmd
go vet ./...
golangci-lint run ./...
go test -race ./...
go build -o /tmp/taiga-phase2 ./cmd/taiga
/tmp/taiga-phase2 story --help
/tmp/taiga-phase2 field --help
/tmp/taiga-phase2 project --help
/tmp/taiga-phase2 status --help
```

Expected: gofmt sem saída; vet/lint sem problemas; testes PASS; help em inglês e sem comando delete. Flags de escrita dry-run/force-version onde aplicável. Nenhuma dependência extra em go.mod/go.sum.

Na janela local futura, depois de combinar portas e o término da #244:

Run: `go test -tags integration -p 1 ./...`
Expected: PASS incluindo autenticação por sessão e a fase 2. Falha da API incerta é gate para resolver contrato; não “corrigir” teste desativando proteção/asserção. CI deve executar essa suíte com seu próprio ambiente, sem portas do checkout principal.

- [ ] **Step 3: revisar escopo e preparar um único PR #249 na execução futura.**

```bash
git diff --check
git add README.md docs/guia.md docs/errors.md docs/api-notes.md docs/fase-2-validacao.md
git commit -m "Documentar fase 2 e seus contratos de API"
```

Expected: PR reúne Tasks 8–10, título `Implementar plano e aplicação de status e campos TG-249`. Publicação/PR/merge/status são ações futuras sujeitas à autorização daquela execução. Não executar agora. #246/#247/#248/#250 têm seus gates nas respectivas seções, sem concentrar revisão de toda a fase num PR só.

## Pontos a validar e decisões humanas

| Ponto | Tarefa | Critério / decisão |
|---|---|---|
| by_ref e isolamento de projeto | 1 | Rota/parâmetros/resposta, 404 de ref versus rota, ref igual em projetos distintos |
| filtros, membro do projeto, me | 1/3/4 | Filtros reais ou pós-filtro local; users/memberships delimitados; relação owner/executores |
| vínculo de épico e milestone | 1/3 | Endpoint real e OCC; não prometer PATCH epic nem substituir vínculo por DELETE |
| swimlane na criação/edição | 1/3 | Validar escrita local antes de expor; comandos de catálogo ficam #252 |
| definições e valores de story/task | 5/6 | Tipos, rotas, corpo, version próprio e persistência por releitura |
| campos date/checkbox vazios/null | 6 | Validar representação; eventual sintaxe de unset exige escolha humana, text “null” continua texto |
| histórico e identificação GitLab | 7 | Fixture local; unknown/humano preservado; ordem e paginação observadas |
| catálogo/permissões/status/reorder/OCC | 9 | Se não houver OCC, humano decide aceitar limitação, estender servidor ou adiar; não inventar version |
| divergência de definição existente | 8/9 | Plano mostra drift e apply recusa; humano decide reconciliar, sem alteração silenciosa |
| ports/compose e integração | Todas | Humano coordena fim da #244 e posse do Taiga local; nenhum compose nesta sessão |
| cores e campos do fluxo Basis | 8/10 | Exemplo não impõe config corporativa; confirmar antes da migração #254 |

**Dependência externa:** merge da #244 antes de validar autenticação real de fase 2. A redação e testes com token injetado não dependem de símbolos ainda inexistentes de auth. Reconciliar registro do root por acréscimo de comandos, sem sobrescrever o registro de auth/api/version.

**Revisão deste plano:** cobertura por US e ordem conferidas contra spec e descrições; riscos de merge, projeto errado, texto, histórico e apply parcial ligados a tarefas. Nenhuma sondagem local executada nesta sessão, nenhuma escrita no Taiga real. As Tasks 1/5/6/7/9 estão explicitamente pendentes de execução local. O documento é plano, não evidência de testes de produção ou de entrega das US.
