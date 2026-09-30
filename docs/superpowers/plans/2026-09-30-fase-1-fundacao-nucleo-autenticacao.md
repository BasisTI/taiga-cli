# taiga-cli — Fase 1 (fundação, núcleo HTTP e autenticação) — plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Entregar o binário `taiga` com `taiga api` (acesso genérico à API v1) e `taiga auth login|refresh|status|logout`, com testes de unidade e de integração contra um Taiga 6.7 local, CI e release `v0.1.0`.

**Architecture:** Go com cobra. `internal/cli` monta os comandos. `internal/taiga` é o cliente HTTP da API v1, com `version`, paginação e erros. `internal/auth` implementa `taiga.TokenSource`: cache de sessão, refresh sob `flock` e fontes de segredo. `internal/config` resolve URL e projeto com proveniência. `internal/output` cuida da saída texto/JSON, do envelope de erro e dos exit codes. As dependências apontam para dentro: `cli → auth → taiga → output` e `cli → config`.

**Tech Stack:**
- Go 1.27;
- `github.com/spf13/cobra`, `github.com/pelletier/go-toml/v2`, `github.com/godbus/dbus/v5`, `golang.org/x/term`, `golang.org/x/sys`;
- Docker Compose com `taigaio/taiga-back:6.7.3`;
- GitHub Actions, GoReleaser.

**Spec:** `docs/superpowers/specs/2026-09-30-taiga-cli-design.md` (seções 3 a 8, 10 e 11). Leia a spec antes de começar.

**US cobertas (Taiga Infraestrutura, projeto 37, épico #242):** #243 Fundação (tarefas 1 e 2), #245 Núcleo HTTP (tarefas 3 a 8), #244 Autenticação (tarefas 9 a 14).

## Global Constraints

- Caminho do módulo: `github.com/BasisTI/taiga-cli`. Binário: `taiga`. Versão do Go em `go.mod`: `go 1.27.0`.
- Licença: Apache 2.0 (`LICENSE` na raiz).
- Dependências diretas permitidas: cobra, go-toml/v2, godbus/dbus/v5, x/term, x/sys. **Nenhuma outra** sem aprovação.
- Comandos, flags, códigos de erro, mensagens e `README.md` em inglês. Arquivos em `docs/` em PT-BR.
- Exit codes: 0 OK, 1 erro inesperado, 2 uso inválido, 3 autenticação, 4 conflito de `version`, 5 não encontrado, 6 permissão negada, 7 rede ou servidor.
- Envelope de erro: `{"error":{"code","source","stage","cause","recovery"}}`. `code` é estável; `cause` nunca contém segredo.
- JSON sempre por `encoding/json`, nunca montado por concatenação de strings.
- URL: só HTTPS. HTTP é aceito apenas para `localhost`, `127.0.0.1` e `::1`. Sem userinfo, query ou fragmento.
- Arquivos de config, sessão e segredo com permissão `0600`; diretórios com `0700`. Gravação atômica (arquivo temporário + rename).
- Testes **nunca** escrevem em `agile.basis.com.br`. Integração só contra o Taiga local (`TAIGA_TEST_URL`, padrão `http://localhost:8000`).
- Nenhum comando curado faz `DELETE`. `taiga api DELETE` exige `--confirm-delete`.
- Timeout HTTP de 30 s. Nova tentativa só em `GET`, por erro de rede ou 5xx, no máximo 2 novas tentativas.
- Fluxo por US:
  - branch `TG-<ref>` a partir da `main` atualizada, com commits pequenos;
  - PR para `main` com squash, revisado pelo Codex antes do merge;
  - status da story no Taiga: `In progress` ao começar a US e `Ready for test` depois do merge. O agente **nunca** marca `Done`.

## Review Focus

1. **Dois agentes renovando a sessão ao mesmo tempo:** só um chama `/auth/refresh`; o outro relê o cache já renovado. Teste na tarefa 12.
2. **Codex com o state dir somente leitura:** a CLI não queima o refresh e responde `session_expired` com a recuperação "run `taiga auth refresh` outside the sandbox"; com senha no env, faz login em memória e avisa `session_cache_readonly`. Testes na tarefa 12.
3. **`secret_command` preso no pinentry** (gpg sem TTY): encerra em 10 s com `secret_command_timeout`, e a mensagem do stderr com a dica de `GPG_TTY` chega ao usuário. Testes na tarefa 11.
4. **Agente chamando `taiga api DELETE ...`:** sem `--confirm-delete`, sai com exit 2 e nada é enviado. Teste na tarefa 8.
5. **Texto com caracteres de controle, aspas e acentos em `--field`/`--input`:** vai à API como JSON válido e idêntico ao original (o incidente do `jq` na US #177). Teste na tarefa 8.
6. **URL escrita como `https://agile.basis.com.br/api/v1/` ou com `http://`:** normalizada ou recusada com `config_invalid_url`. Teste na tarefa 4.

---

## Pré-requisitos (antes da tarefa 1)

- [ ] **P1: conferir a `main`.** `git log --oneline` deve mostrar o commit da spec e o deste plano. Se o push da `main` ainda não foi feito, peça autorização ao humano e rode `git push -u origin main`.
- [ ] **P2: conferir a toolchain.** Rode `go version` (≥ 1.27) e `docker compose version`. Para o lint local: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`.
- [ ] **P3: iniciar a US #243 no Taiga.** Mude o status da US #243 (projeto 37) para `In progress`. Crie a branch com `git switch -c TG-243`.

---

### Task 1: esqueleto do repositório, `taiga version`, CI e GoReleaser (US #243)

**Files:**
- Create: `go.mod`, `LICENSE`, `.gitignore`, `cmd/taiga/main.go`, `internal/cli/root.go`, `internal/cli/root_test.go`, `.github/workflows/ci.yml`, `.goreleaser.yaml`, `.golangci.yml`, `README.md`

**Interfaces:**
- Produces:
  - `cli.Main(args []string, in io.Reader, out, errOut io.Writer, env func(string) string) int`
  - `cli.Version` (string, preenchida por ldflags)
  - `type App struct` (campos abaixo, ampliados nas tarefas seguintes)

- [ ] **Step 1: inicializar o módulo e a licença**

```bash
go mod init github.com/BasisTI/taiga-cli
go mod edit -go=1.27.0
curl -fsSL https://www.apache.org/licenses/LICENSE-2.0.txt -o LICENSE
printf 'dist/\n/taiga\n*.test\ncoverage.out\n' > .gitignore
go get github.com/spf13/cobra@latest golang.org/x/term@latest
```

- [ ] **Step 2: escrever o teste que falha**

`internal/cli/root_test.go`:

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func run(t *testing.T, env map[string]string, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Main(args, strings.NewReader(""), &out, &errOut, func(k string) string { return env[k] })
	return out.String(), errOut.String(), code
}

func TestVersionPrintsVersion(t *testing.T) {
	Version = "1.2.3-test"
	out, _, code := run(t, nil, "version")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.TrimSpace(out) != "taiga 1.2.3-test" {
		t.Fatalf("out = %q", out)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	_, errOut, code := run(t, nil, "nope")
	if code != 2 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
}
```

- [ ] **Step 3: rodar e ver falhar**

Run: `go test ./internal/cli/...`
Expected: FAIL, com `undefined: Main`.

- [ ] **Step 4: implementar**

`internal/cli/root.go`:

```go
// Package cli wires the taiga command tree.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Version is set at build time with -ldflags "-X github.com/BasisTI/taiga-cli/internal/cli.Version=...".
var Version = "dev"

// App carries the process I/O and environment so commands are testable.
type App struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Env    func(string) string
	ran    bool
	output string
}

var errUsage = errors.New("usage error")

func (a *App) root() *cobra.Command {
	root := &cobra.Command{
		Use:           "taiga",
		Short:         "Command line client for the Taiga REST API",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(*cobra.Command, []string) {
			a.ran = true
		},
	}
	root.PersistentFlags().StringVar(&a.output, "output", "", "output format: json or text (default: text on a terminal, json otherwise)")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %v", errUsage, err)
	})
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the taiga version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(a.Out, "taiga %s\n", Version)
			return err
		},
	})
	return root
}

// Main runs the CLI and returns the process exit code.
func Main(args []string, in io.Reader, out, errOut io.Writer, env func(string) string) int {
	a := &App{In: in, Out: out, Err: errOut, Env: env}
	root := a.root()
	root.SetArgs(args)
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	err := root.Execute()
	if err == nil {
		return 0
	}
	if errors.Is(err, errUsage) || !a.ran {
		fmt.Fprintf(errOut, "error [usage]: %v\n", err)
		return 2
	}
	fmt.Fprintf(errOut, "error: %v\n", err)
	return 1
}
```

`cmd/taiga/main.go`:

```go
package main

import (
	"os"

	"github.com/BasisTI/taiga-cli/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}
```

- [ ] **Step 5: rodar e ver passar**

Run: `go mod tidy && go test ./... && go build -o taiga ./cmd/taiga && ./taiga version`
Expected: PASS e a saída `taiga dev`.

- [ ] **Step 6: CI, lint e release**

`.golangci.yml`:

```yaml
version: "2"
linters:
  default: standard
```

`.github/workflows/ci.yml`:

```yaml
name: ci
on:
  push:
    branches: [main]
  pull_request:
permissions:
  contents: read
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test -race ./...
      - uses: golangci/golangci-lint-action@v8
        with:
          version: latest
```

`.goreleaser.yaml`:

```yaml
version: 2
project_name: taiga
builds:
  - main: ./cmd/taiga
    binary: taiga
    env: [CGO_ENABLED=0]
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X github.com/BasisTI/taiga-cli/internal/cli.Version={{.Version}}
archives:
  - formats: [tar.gz]
    files: [LICENSE, README.md]
checksum:
  name_template: SHA256SUMS
release:
  github:
    owner: BasisTI
    name: taiga-cli
```

Acrescente ao `ci.yml` o workflow de release, num arquivo separado, `.github/workflows/release.yml`:

```yaml
name: release
on:
  push:
    tags: ["v*"]
permissions:
  contents: write
jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

Crie o `README.md` mínimo, completado na tarefa 14:

```markdown
# taiga-cli

Command line client for the [Taiga](https://taiga.io) REST API, built for humans and coding agents.

Status: under development (phase 1). License: Apache-2.0.
```

- [ ] **Step 7: lint local e commit**

Run: `golangci-lint run ./...`
Expected: `0 issues.`

```bash
git add -A
git commit -m "Criar esqueleto do módulo, comando version, CI e GoReleaser"
```

---

### Task 2: Taiga 6.7 local para testes de integração (US #243)

**Files:**
- Create: `compose.test.yml`, `scripts/taiga-seed`, `internal/testtaiga/testtaiga.go`, `internal/testtaiga/smoke_integration_test.go`
- Modify: `.github/workflows/ci.yml` (job `integration`)

**Interfaces:**
- Produces (pacote `testtaiga`, build tag `integration`):
  - `testtaiga.URL() string`: `TAIGA_TEST_URL`, com padrão `http://localhost:8000`;
  - `testtaiga.Login(t *testing.T, username, password string) (authToken, refresh string)`;
  - constantes `AdminUser = "admin"`, `AdminPassword = "admin123"`, `ServiceUser = "svc"`, `ServicePassword = "svc12345"` e `ProjectSlug`, que é definido na seed.

- [ ] **Step 1: escrever o compose de teste**

`compose.test.yml`. Deriva do compose de produção da Basis, mas sem as imagens `-openid`, sem front, gateway e events, e com segredos só de teste. A API fica exposta direto do `taiga-back` na porta 8000.

```yaml
name: taiga-cli-test
x-back-env: &back-env
  POSTGRES_DB: taiga
  POSTGRES_USER: taiga
  POSTGRES_PASSWORD: taiga-test-only
  POSTGRES_HOST: taiga-db
  TAIGA_SECRET_KEY: taiga-cli-test-secret-key-not-for-production
  TAIGA_SITES_SCHEME: http
  TAIGA_SITES_DOMAIN: localhost:8000
  TAIGA_SUBPATH: ""
  EMAIL_BACKEND: django.core.mail.backends.console.EmailBackend
  DEFAULT_FROM_EMAIL: taiga@localhost
  RABBITMQ_USER: taiga
  RABBITMQ_PASS: taiga-test-only
  ENABLE_TELEMETRY: "False"
  PUBLIC_REGISTER_ENABLED: "True"
x-rabbit-env: &rabbit-env
  RABBITMQ_ERLANG_COOKIE: taiga-cli-test-cookie
  RABBITMQ_DEFAULT_USER: taiga
  RABBITMQ_DEFAULT_PASS: taiga-test-only
  RABBITMQ_DEFAULT_VHOST: taiga
services:
  taiga-db:
    image: postgres:12.3
    environment:
      POSTGRES_DB: taiga
      POSTGRES_USER: taiga
      POSTGRES_PASSWORD: taiga-test-only
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U taiga"]
      interval: 2s
      timeout: 15s
      retries: 15
  taiga-events-rabbitmq:
    image: rabbitmq:3.8-management-alpine
    hostname: taiga-events-rabbitmq
    environment: *rabbit-env
  taiga-async-rabbitmq:
    image: rabbitmq:3.8-management-alpine
    hostname: taiga-async-rabbitmq
    environment: *rabbit-env
  taiga-back:
    image: taigaio/taiga-back:6.7.3
    environment: *back-env
    ports:
      - "8000:8000"
    depends_on:
      taiga-db:
        condition: service_healthy
      taiga-events-rabbitmq:
        condition: service_started
      taiga-async-rabbitmq:
        condition: service_started
```

- [ ] **Step 2: escrever o script de seed**

`scripts/taiga-seed`. Precisa ser executável (`chmod +x`).

```bash
#!/usr/bin/env bash
# Seeds the local test Taiga: admin + service account + one project. Test-only credentials.
set -euo pipefail
URL="${TAIGA_TEST_URL:-http://localhost:8000}"
COMPOSE=(docker compose -f "$(dirname "$0")/../compose.test.yml")

echo "waiting for $URL/api/v1/ ..."
for _ in $(seq 1 90); do
  if curl -fsS "$URL/api/v1/" >/dev/null 2>&1; then break; fi
  sleep 2
done
curl -fsS "$URL/api/v1/" >/dev/null

"${COMPOSE[@]}" exec -T taiga-back python manage.py shell -c '
from django.contrib.auth import get_user_model
U = get_user_model()
if not U.objects.filter(username="admin").exists():
    U.objects.create_superuser("admin", "admin@example.com", "admin123")
if not U.objects.filter(username="svc").exists():
    U.objects.create_user("svc", "svc@example.com", "svc12345", full_name="Service Account")
'

TOKEN=$(curl -fsS -X POST "$URL/api/v1/auth" -H 'Content-Type: application/json' \
  -d '{"type":"normal","username":"admin","password":"admin123"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["auth_token"])')
if ! curl -fsS "$URL/api/v1/projects/by_slug?slug=admin-cli-test" -H "Authorization: Bearer $TOKEN" >/dev/null 2>&1; then
  curl -fsS -X POST "$URL/api/v1/projects" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -d '{"name":"cli-test","description":"taiga-cli integration tests","creation_template":1}' >/dev/null
fi
echo "seeded: admin/admin123, svc/svc12345, project admin-cli-test"
```

- [ ] **Step 3: subir a stack e rodar a seed**

Run: `docker compose -f compose.test.yml up -d && scripts/taiga-seed`
Expected: a última linha é `seeded: admin/admin123, svc/svc12345, project admin-cli-test`.

Se o slug criado for diferente de `admin-cli-test`, confira com `curl -s localhost:8000/api/v1/projects -H "Authorization: Bearer <token>" | python3 -m json.tool | grep slug`. Ajuste o slug no script e na constante `ProjectSlug`.

- [ ] **Step 4: escrever o helper e o teste de fumaça**

`internal/testtaiga/testtaiga.go`:

```go
//go:build integration

// Package testtaiga holds helpers for integration tests against the local Taiga from compose.test.yml.
package testtaiga

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

const (
	AdminUser       = "admin"
	AdminPassword   = "admin123"
	ServiceUser     = "svc"
	ServicePassword = "svc12345"
	ProjectSlug     = "admin-cli-test"
)

// URL returns the base URL of the test Taiga (no /api/v1 suffix).
func URL() string {
	if u := os.Getenv("TAIGA_TEST_URL"); u != "" {
		return u
	}
	return "http://localhost:8000"
}

// Login authenticates directly against /api/v1/auth, bypassing the code under test.
func Login(t *testing.T, username, password string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"type": "normal", "username": username, "password": password})
	resp, err := http.Post(URL()+"/api/v1/auth", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	var out struct {
		AuthToken string `json:"auth_token"`
		Refresh   string `json:"refresh"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return out.AuthToken, out.Refresh
}
```

`internal/testtaiga/smoke_integration_test.go`:

```go
//go:build integration

package testtaiga

import (
	"net/http"
	"testing"
)

func TestSmokeServiceAccountSeesAPI(t *testing.T) {
	token, refresh := Login(t, ServiceUser, ServicePassword)
	if token == "" || refresh == "" {
		t.Fatal("empty tokens")
	}
	req, _ := http.NewRequest(http.MethodGet, URL()+"/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("users/me status %d", resp.StatusCode)
	}
}
```

- [ ] **Step 5: rodar**

Run: `go test -tags integration ./internal/testtaiga/...`
Expected: PASS.

Run: `go test ./...`
Expected: PASS. Sem a tag, o pacote é ignorado.

- [ ] **Step 6: job de integração no CI**

Acrescente em `.github/workflows/ci.yml`, dentro de `jobs:`:

```yaml
  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: docker compose -f compose.test.yml up -d
      - run: scripts/taiga-seed
      - run: go test -tags integration -p 1 ./...
      - if: failure()
        run: docker compose -f compose.test.yml logs taiga-back | tail -200
```

- [ ] **Step 7: commit, PR da US #243 e status**

```bash
git add -A
git commit -m "Adicionar Taiga 6.7 local, seed e job de integração"
git push -u origin TG-243
gh pr create --title "TG-243 Fundação do repositório" --body "US #243 (Taiga Infraestrutura). Esqueleto, CI, GoReleaser e Taiga local para integração."
```

Peça a revisão do Codex no PR. Depois do merge com squash:
1. mude a US #243 para `Ready for test`;
2. rode `git switch main && git pull`;
3. mude a US #245 para `In progress`;
4. crie a próxima branch com `git switch -c TG-245`.

---

### Task 3: saída, envelope de erro e exit codes (US #245)

**Files:**
- Create: `internal/output/output.go`, `internal/output/output_test.go`

**Interfaces:**
- Produces:
  - `type Mode int`; `const Text Mode`, `JSON Mode`
  - `func DetectMode(flag string, isTTY bool) (Mode, error)`
  - `type Error struct{ Code, Source, Stage, Cause, Recovery string; Exit int }`, que implementa `error`
  - constantes `ExitOK=0, ExitUnexpected=1, ExitUsage=2, ExitAuth=3, ExitConflict=4, ExitNotFound=5, ExitForbidden=6, ExitNetwork=7`
  - `func AsError(err error) *Error`
  - `func WriteError(w io.Writer, m Mode, e *Error) error`
  - `func WriteJSON(w io.Writer, v any) error`
  - `type Field struct{ Key, Value string }` e `func WriteFields(w io.Writer, fields []Field) error`

- [ ] **Step 1: escrever o teste que falha**

`internal/output/output_test.go`:

```go
package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDetectMode(t *testing.T) {
	cases := []struct {
		flag string
		tty  bool
		want Mode
	}{{"", true, Text}, {"", false, JSON}, {"json", true, JSON}, {"text", false, Text}}
	for _, c := range cases {
		got, err := DetectMode(c.flag, c.tty)
		if err != nil || got != c.want {
			t.Fatalf("DetectMode(%q,%v) = %v,%v", c.flag, c.tty, got, err)
		}
	}
	if _, err := DetectMode("yaml", true); err == nil {
		t.Fatal("expected error for yaml")
	}
}

func TestWriteErrorJSONEnvelope(t *testing.T) {
	var b bytes.Buffer
	e := &Error{Code: "version_conflict", Source: "api", Stage: "patch userstories/1", Cause: "changed", Recovery: "retry", Exit: ExitConflict}
	if err := WriteError(&b, JSON, e); err != nil {
		t.Fatal(err)
	}
	var got map[string]map[string]string
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("not json: %s", b.String())
	}
	if got["error"]["code"] != "version_conflict" || got["error"]["recovery"] != "retry" {
		t.Fatalf("envelope = %v", got)
	}
}

func TestWriteErrorText(t *testing.T) {
	var b bytes.Buffer
	_ = WriteError(&b, Text, &Error{Code: "not_found", Cause: "story 9 not found", Recovery: "check the ref", Exit: ExitNotFound})
	s := b.String()
	if !strings.Contains(s, "error [not_found]: story 9 not found") || !strings.Contains(s, "fix: check the ref") {
		t.Fatalf("text = %q", s)
	}
}

func TestAsErrorWrapsUnknown(t *testing.T) {
	e := AsError(errors.New("boom"))
	if e.Code != "unexpected" || e.Exit != ExitUnexpected || e.Cause != "boom" {
		t.Fatalf("got %+v", e)
	}
	orig := &Error{Code: "x", Exit: 6}
	if AsError(orig) != orig {
		t.Fatal("must return the same *Error")
	}
}

func TestWriteJSONKeepsUnicodeAndControlChars(t *testing.T) {
	var b bytes.Buffer
	_ = WriteJSON(&b, map[string]string{"s": "Ação <b>\u0001\t\"x\""})
	var back map[string]string
	if err := json.Unmarshal(b.Bytes(), &back); err != nil || back["s"] != "Ação <b>\u0001\t\"x\"" {
		t.Fatalf("round trip failed: %s", b.String())
	}
	if strings.Contains(b.String(), "\\u003c") {
		t.Fatal("HTML escaping must be off")
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/output/...`
Expected: FAIL, com `undefined: DetectMode`.

- [ ] **Step 3: implementar**

`internal/output/output.go`:

```go
// Package output renders results and errors as text or JSON and defines exit codes.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Mode int

const (
	Text Mode = iota
	JSON
)

const (
	ExitOK         = 0
	ExitUnexpected = 1
	ExitUsage      = 2
	ExitAuth       = 3
	ExitConflict   = 4
	ExitNotFound   = 5
	ExitForbidden  = 6
	ExitNetwork    = 7
)

// DetectMode picks the output mode: explicit flag wins, otherwise text on a TTY and JSON elsewhere.
func DetectMode(flag string, isTTY bool) (Mode, error) {
	switch flag {
	case "json":
		return JSON, nil
	case "text":
		return Text, nil
	case "":
		if isTTY {
			return Text, nil
		}
		return JSON, nil
	}
	return Text, &Error{Code: "usage", Cause: fmt.Sprintf("invalid --output %q", flag), Recovery: "use --output json or --output text", Exit: ExitUsage}
}

// Error is the stable error envelope. Cause must never contain secrets.
type Error struct {
	Code     string `json:"code"`
	Source   string `json:"source,omitempty"`
	Stage    string `json:"stage,omitempty"`
	Cause    string `json:"cause,omitempty"`
	Recovery string `json:"recovery,omitempty"`
	Exit     int    `json:"-"`
}

func (e *Error) Error() string {
	if e.Cause == "" {
		return e.Code
	}
	return e.Code + ": " + e.Cause
}

// AsError returns err as *Error, wrapping unknown errors as "unexpected".
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: "unexpected", Cause: err.Error(), Exit: ExitUnexpected}
}

// WriteError renders the error envelope.
func WriteError(w io.Writer, m Mode, e *Error) error {
	if m == JSON {
		return WriteJSON(w, map[string]*Error{"error": e})
	}
	s := fmt.Sprintf("error [%s]: %s\n", e.Code, e.Cause)
	if e.Stage != "" {
		s += fmt.Sprintf("  at: %s\n", e.Stage)
	}
	if e.Recovery != "" {
		s += fmt.Sprintf("  fix: %s\n", e.Recovery)
	}
	_, err := io.WriteString(w, s)
	return err
}

// WriteJSON writes v as indented JSON without HTML escaping.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type Field struct{ Key, Value string }

// WriteFields prints aligned "key: value" lines for text mode.
func WriteFields(w io.Writer, fields []Field) error {
	width := 0
	for _, f := range fields {
		if len(f.Key) > width {
			width = len(f.Key)
		}
	}
	for _, f := range fields {
		if _, err := fmt.Fprintf(w, "%-*s  %s\n", width+1, f.Key+":", f.Value); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/output/...`
Expected: PASS.

- [ ] **Step 5: integrar ao `cli.Main`**

Substitua o final de `Main` em `internal/cli/root.go`. O `App` ganha o campo `OutTTY bool`, que o `main` preenche com `term.IsTerminal(int(os.Stdout.Fd()))`. Em testes, o campo fica `false`.

```go
	err := root.Execute()
	if err == nil {
		return output.ExitOK
	}
	mode, merr := output.DetectMode(a.output, a.OutTTY)
	if merr != nil {
		mode = output.Text
	}
	var e *output.Error
	switch {
	case errors.Is(err, errUsage) || !a.ran:
		e = &output.Error{Code: "usage", Cause: err.Error(), Recovery: "run `taiga --help`", Exit: output.ExitUsage}
	default:
		e = output.AsError(err)
	}
	_ = output.WriteError(errOut, mode, e)
	return e.Exit
```

Troque a assinatura para `func Main(args []string, in io.Reader, out, errOut io.Writer, env func(string) string, outTTY bool) int`. Atualize o `cmd/taiga/main.go`:

```go
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, term.IsTerminal(int(os.Stdout.Fd()))))
```

Atualize também o helper `run` em `root_test.go`:

```go
	code := Main(args, strings.NewReader(""), &out, &errOut, func(k string) string { return env[k] }, false)
```

No teste `TestUnknownCommandIsUsageError`, acrescente a verificação de que o stderr traz JSON com `"code": "usage"`:

```go
	if !strings.Contains(errOut, `"code": "usage"`) {
		t.Fatalf("stderr = %s", errOut)
	}
```

- [ ] **Step 6: rodar tudo e commitar**

Run: `go test ./...`
Expected: PASS.

```bash
git add -A
git commit -m "Adicionar saída texto/JSON, envelope de erro e exit codes"
```

---

### Task 4: caminhos, arquivo de config e normalização de URL (US #245)

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: `output.Error`
- Produces:
  - `type Paths struct{ ConfigFile, StateDir, SecretsDir string }`
  - `func DefaultPaths(env func(string) string) (Paths, error)`
  - `type Host struct{ URL, Username, SecretSource string; SecretCommand []string; Project string }`, com as tags toml `url`, `username`, `secret_source`, `secret_command`, `project`
  - `type File struct{ DefaultHost string; Hosts []Host }`, com as tags toml `default_host`, `hosts`
  - `func Load(path string) (File, error)`: arquivo ausente devolve `File{}` sem erro
  - `func Save(path string, f File) error`
  - `func (f File) Host(url string) (Host, bool)`
  - `func (f *File) Upsert(h Host)`
  - `func NormalizeURL(raw string) (string, error)`

- [ ] **Step 1: escrever o teste que falha**

`internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func envMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaultPathsXDG(t *testing.T) {
	p, err := DefaultPaths(envMap(map[string]string{"XDG_CONFIG_HOME": "/c", "XDG_STATE_HOME": "/s", "HOME": "/h"}))
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigFile != "/c/taiga/config.toml" || p.StateDir != "/s/taiga" || p.SecretsDir != "/c/taiga/secrets" {
		t.Fatalf("%+v", p)
	}
}

func TestDefaultPathsOverridesAndHomeFallback(t *testing.T) {
	p, _ := DefaultPaths(envMap(map[string]string{"HOME": "/h", "TAIGA_CONFIG": "/x/cfg.toml", "TAIGA_STATE_DIR": "/y"}))
	if p.ConfigFile != "/x/cfg.toml" || p.StateDir != "/y" {
		t.Fatalf("%+v", p)
	}
	p, _ = DefaultPaths(envMap(map[string]string{"HOME": "/h"}))
	if p.ConfigFile != "/h/.config/taiga/config.toml" || p.StateDir != "/h/.local/state/taiga" {
		t.Fatalf("%+v", p)
	}
}

func TestNormalizeURL(t *testing.T) {
	ok := map[string]string{
		"https://agile.basis.com.br":         "https://agile.basis.com.br",
		"https://agile.basis.com.br/":        "https://agile.basis.com.br",
		"https://agile.basis.com.br/api/v1/": "https://agile.basis.com.br",
		"http://localhost:8000":              "http://localhost:8000",
		"http://127.0.0.1:8000/api/v1":       "http://127.0.0.1:8000",
	}
	for in, want := range ok {
		got, err := NormalizeURL(in)
		if err != nil || got != want {
			t.Fatalf("NormalizeURL(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"http://agile.basis.com.br", "https://u:p@h", "https://h?x=1", "https://h#f", "agile.basis.com.br", ""} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Fatalf("NormalizeURL(%q) must fail", bad)
		}
	}
}

func TestLoadSaveRoundTripAndPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taiga", "config.toml")
	f, err := Load(path)
	if err != nil || len(f.Hosts) != 0 {
		t.Fatalf("missing file must load empty: %v %+v", err, f)
	}
	f.Upsert(Host{URL: "https://a.example", Username: "svc", SecretSource: "keyring", Project: "infra-2025"})
	f.Upsert(Host{URL: "https://a.example", Username: "svc2", SecretSource: "keyring"})
	f.DefaultHost = "https://a.example"
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", info.Mode().Perm())
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := back.Host("https://a.example")
	if !ok || h.Username != "svc2" || len(back.Hosts) != 1 || back.DefaultHost != "https://a.example" {
		t.Fatalf("%+v", back)
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go get github.com/pelletier/go-toml/v2@latest && go test ./internal/config/...`
Expected: FAIL, com `undefined: DefaultPaths`.

- [ ] **Step 3: implementar**

`internal/config/config.go`:

```go
// Package config resolves file locations, the user config file and the Taiga URL/project context.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	toml "github.com/pelletier/go-toml/v2"
)

type Paths struct {
	ConfigFile string
	StateDir   string
	SecretsDir string
}

// DefaultPaths honours TAIGA_CONFIG, TAIGA_STATE_DIR and the XDG base directories.
func DefaultPaths(env func(string) string) (Paths, error) {
	home := env("HOME")
	configHome := env("XDG_CONFIG_HOME")
	if configHome == "" {
		if home == "" {
			return Paths{}, &output.Error{Code: "config_no_home", Source: "env", Cause: "HOME and XDG_CONFIG_HOME are unset", Recovery: "set TAIGA_CONFIG and TAIGA_STATE_DIR", Exit: output.ExitUsage}
		}
		configHome = filepath.Join(home, ".config")
	}
	stateHome := env("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	p := Paths{
		ConfigFile: filepath.Join(configHome, "taiga", "config.toml"),
		StateDir:   filepath.Join(stateHome, "taiga"),
		SecretsDir: filepath.Join(configHome, "taiga", "secrets"),
	}
	if v := env("TAIGA_CONFIG"); v != "" {
		p.ConfigFile = v
		p.SecretsDir = filepath.Join(filepath.Dir(v), "secrets")
	}
	if v := env("TAIGA_STATE_DIR"); v != "" {
		p.StateDir = v
	}
	return p, nil
}

type Host struct {
	URL           string   `toml:"url"`
	Username      string   `toml:"username"`
	SecretSource  string   `toml:"secret_source"`
	SecretCommand []string `toml:"secret_command,omitempty"`
	Project       string   `toml:"project,omitempty"`
}

type File struct {
	DefaultHost string `toml:"default_host,omitempty"`
	Hosts       []Host `toml:"hosts"`
}

// Load reads the config file; a missing file is an empty config.
func Load(path string) (File, error) {
	var f File
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, &output.Error{Code: "config_unreadable", Source: "config", Stage: path, Cause: err.Error(), Recovery: "check the file permissions or set TAIGA_CONFIG", Exit: output.ExitUsage}
	}
	if err := toml.Unmarshal(b, &f); err != nil {
		return f, &output.Error{Code: "config_invalid", Source: "config", Stage: path, Cause: err.Error(), Recovery: "fix the TOML syntax", Exit: output.ExitUsage}
	}
	return f, nil
}

// Save writes the config atomically with mode 0600.
func Save(path string, f File) error {
	b, err := toml.Marshal(f)
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, b)
}

// WriteFileAtomic writes via a temp file + rename, creating parent dirs with 0700 and the file with 0600.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (f File) Host(u string) (Host, bool) {
	for _, h := range f.Hosts {
		if h.URL == u {
			return h, true
		}
	}
	return Host{}, false
}

func (f *File) Upsert(h Host) {
	for i := range f.Hosts {
		if f.Hosts[i].URL == h.URL {
			f.Hosts[i] = h
			return
		}
	}
	f.Hosts = append(f.Hosts, h)
}

// NormalizeURL returns scheme://host[:port] without trailing slash or /api/v1 suffix.
func NormalizeURL(raw string) (string, error) {
	fail := func(cause string) (string, error) {
		return "", &output.Error{Code: "config_invalid_url", Source: "config", Cause: fmt.Sprintf("%q: %s", raw, cause), Recovery: "use https://host (http only for localhost)", Exit: output.ExitUsage}
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fail("not an absolute URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fail("userinfo, query and fragment are not allowed")
	}
	host := u.Hostname()
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return fail("scheme must be https")
	}
	p := strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/api/v1")
	if strings.Trim(p, "/") != "" {
		return fail("path prefixes are not supported")
	}
	return u.Scheme + "://" + u.Host, nil
}
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/config/...`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add -A
git commit -m "Adicionar caminhos XDG, arquivo de config e normalização de URL"
```

---

### Task 5: `.taiga.toml` e resolução do contexto com proveniência (US #245)

**Files:**
- Create: `internal/config/context.go`, `internal/config/context_test.go`

**Interfaces:**
- Consumes: `File`, `Host`, `NormalizeURL` (tarefa 4)
- Produces:
  - `type RepoFile struct{ URL, Project string }`, com as tags toml `url` e `project`
  - `func FindRepoFile(startDir string) (RepoFile, string, error)`: sem arquivo, devolve o caminho `""`
  - `type Value struct{ Value, Source string }`, com as tags json `value` e `source`
  - `type Context struct{ URL, Project Value }`, com as tags json `url` e `project`
  - `type Inputs struct{ FlagURL, FlagProject string; Env func(string) string; Cwd string; File File; ConfigPath string }`
  - `func Resolve(in Inputs) (Context, error)`

**Precedência:**
- URL: `--url` > `TAIGA_URL` > `.taiga.toml` > `default_host` da config > erro `config_no_url`.
- Projeto: `--project` > `TAIGA_PROJECT` > `.taiga.toml` > `project` do host na config > vazio, sem erro.
- `Source` vale `flag`, `env:TAIGA_URL`, `env:TAIGA_PROJECT`, `file:<caminho do .taiga.toml>` ou `config:<caminho do config.toml>`.

- [ ] **Step 1: escrever o teste que falha**

`internal/config/context_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRepoFileWalksUp(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	_ = os.MkdirAll(deep, 0o755)
	_ = os.WriteFile(filepath.Join(root, ".taiga.toml"), []byte("url = \"https://t.example\"\nproject = \"infra-2025\"\n"), 0o644)
	rf, path, err := FindRepoFile(deep)
	if err != nil || path != filepath.Join(root, ".taiga.toml") || rf.Project != "infra-2025" {
		t.Fatalf("%+v %q %v", rf, path, err)
	}
	_, path, err = FindRepoFile(t.TempDir())
	if err != nil || path != "" {
		t.Fatalf("no file: %q %v", path, err)
	}
}

func TestResolvePrecedence(t *testing.T) {
	repo := t.TempDir()
	_ = os.WriteFile(filepath.Join(repo, ".taiga.toml"), []byte("url = \"https://repo.example\"\nproject = \"repo-proj\"\n"), 0o644)
	file := File{DefaultHost: "https://cfg.example", Hosts: []Host{{URL: "https://cfg.example", Project: "cfg-proj"}, {URL: "https://repo.example", Project: "host-proj"}}}
	base := Inputs{Env: envMap(nil), Cwd: repo, File: file, ConfigPath: "/c/config.toml"}

	c, err := Resolve(base)
	if err != nil || c.URL.Value != "https://repo.example" || c.URL.Source != "file:"+filepath.Join(repo, ".taiga.toml") || c.Project.Value != "repo-proj" {
		t.Fatalf("repo level: %+v %v", c, err)
	}

	in := base
	in.Env = envMap(map[string]string{"TAIGA_URL": "https://env.example/", "TAIGA_PROJECT": "env-proj"})
	c, _ = Resolve(in)
	if c.URL.Value != "https://env.example" || c.URL.Source != "env:TAIGA_URL" || c.Project.Source != "env:TAIGA_PROJECT" {
		t.Fatalf("env level: %+v", c)
	}

	in.FlagURL, in.FlagProject = "https://flag.example", "flag-proj"
	c, _ = Resolve(in)
	if c.URL.Source != "flag" || c.Project.Value != "flag-proj" {
		t.Fatalf("flag level: %+v", c)
	}

	in = Inputs{Env: envMap(nil), Cwd: t.TempDir(), File: file, ConfigPath: "/c/config.toml"}
	c, _ = Resolve(in)
	if c.URL.Value != "https://cfg.example" || c.URL.Source != "config:/c/config.toml" || c.Project.Value != "cfg-proj" {
		t.Fatalf("config level: %+v", c)
	}
}

func TestResolveNoURL(t *testing.T) {
	_, err := Resolve(Inputs{Env: envMap(nil), Cwd: t.TempDir()})
	if err == nil {
		t.Fatal("expected config_no_url")
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/config/...`
Expected: FAIL, com `undefined: FindRepoFile`.

- [ ] **Step 3: implementar**

`internal/config/context.go`:

```go
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BasisTI/taiga-cli/internal/output"
	toml "github.com/pelletier/go-toml/v2"
)

type RepoFile struct {
	URL     string `toml:"url"`
	Project string `toml:"project"`
}

// FindRepoFile walks up from startDir looking for .taiga.toml.
func FindRepoFile(startDir string) (RepoFile, string, error) {
	dir := startDir
	for {
		path := filepath.Join(dir, ".taiga.toml")
		b, err := os.ReadFile(path)
		if err == nil {
			var rf RepoFile
			if err := toml.Unmarshal(b, &rf); err != nil {
				return rf, path, &output.Error{Code: "config_invalid", Source: "file", Stage: path, Cause: err.Error(), Recovery: "fix the TOML syntax", Exit: output.ExitUsage}
			}
			return rf, path, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return RepoFile{}, path, &output.Error{Code: "config_unreadable", Source: "file", Stage: path, Cause: err.Error(), Exit: output.ExitUsage}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return RepoFile{}, "", nil
		}
		dir = parent
	}
}

type Value struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

type Context struct {
	URL     Value `json:"url"`
	Project Value `json:"project"`
}

type Inputs struct {
	FlagURL, FlagProject string
	Env                  func(string) string
	Cwd                  string
	File                 File
	ConfigPath           string
}

// Resolve applies flag > env > .taiga.toml > user config for URL and project.
func Resolve(in Inputs) (Context, error) {
	var c Context
	rf, rpath, err := FindRepoFile(in.Cwd)
	if err != nil {
		return c, err
	}
	pick := func(candidates ...Value) Value {
		for _, v := range candidates {
			if v.Value != "" {
				return v
			}
		}
		return Value{}
	}
	fileSrc := "file:" + rpath
	cfgSrc := "config:" + in.ConfigPath
	c.URL = pick(
		Value{in.FlagURL, "flag"},
		Value{in.Env("TAIGA_URL"), "env:TAIGA_URL"},
		Value{rf.URL, fileSrc},
		Value{in.File.DefaultHost, cfgSrc},
	)
	if c.URL.Value == "" {
		return c, &output.Error{Code: "config_no_url", Source: "config", Cause: "no Taiga URL configured", Recovery: "run `taiga auth login --url https://your.taiga`, set TAIGA_URL or add .taiga.toml", Exit: output.ExitUsage}
	}
	norm, err := NormalizeURL(c.URL.Value)
	if err != nil {
		return c, err
	}
	c.URL.Value = norm
	host, _ := in.File.Host(norm)
	c.Project = pick(
		Value{in.FlagProject, "flag"},
		Value{in.Env("TAIGA_PROJECT"), "env:TAIGA_PROJECT"},
		Value{rf.Project, fileSrc},
		Value{host.Project, cfgSrc},
	)
	return c, nil
}
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/config/...`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add -A
git commit -m "Resolver URL e projeto por flag, env, .taiga.toml e config"
```

---

### Task 6: cliente HTTP da API v1 e mapeamento de erros (US #245)

**Files:**
- Create: `internal/taiga/client.go`, `internal/taiga/errors.go`, `internal/taiga/client_test.go`

**Interfaces:**
- Consumes: `output.Error` e as constantes de exit
- Produces:
  - `type Token struct{ Type, Value string }`: `Type` vale `"Bearer"` ou `"Application"`
  - `type TokenSource interface{ Token(ctx context.Context) (Token, error) }`
  - `type StaticToken Token`, que implementa `TokenSource`
  - `func New(baseURL string, ts TokenSource, opts ...Option) *Client`
  - `func WithHTTPClient(*http.Client) Option` e `func WithRetryWait(time.Duration) Option`
  - `type Request struct{ Method, Path string; Query url.Values; Body any; Header http.Header }`: `Body` nil significa sem corpo; se não for nil, é codificado em JSON
  - `type Response struct{ Status int; Header http.Header; Body []byte }`
  - `func (c *Client) Do(ctx context.Context, r Request) (*Response, error)`
  - `func (c *Client) BaseURL() string`
  - `type APIError struct{ Status int; Method, Path string; Body []byte }`
  - `func (e *APIError) IsVersionConflict() bool`
  - `func ToOutput(err error) *output.Error`

**Mapeamento em `ToOutput`:**

| Erro | `code` | Exit |
|---|---|---|
| Rede ou timeout | `network_error` | 7 |
| 401 | `auth_rejected` | 3 |
| 403 | `forbidden` | 6 |
| 404 | `not_found` | 5 |
| Conflito de `version` | `version_conflict` | 4 |
| Outros 4xx | `invalid_request` | 2 |
| 5xx | `server_error` | 7 |

Em todos os casos, `Source` = `"api"`, `Stage` = método + caminho, e `Cause` = o corpo truncado em 2000 bytes.

- [ ] **Step 1: escrever o teste que falha**

`internal/taiga/client_test.go`:

```go
package taiga

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
)

func TestDoSendsAuthJSONAndPath(t *testing.T) {
	var gotAuth, gotCT, gotPath, gotQuery string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotCT, gotPath, gotQuery = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.URL.Path, r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{Type: "Bearer", Value: "tok"}, WithRetryWait(0))
	resp, err := c.Do(context.Background(), Request{Method: "PATCH", Path: "userstories/7", Query: map[string][]string{"project": {"37"}}, Body: map[string]any{"subject": "Ação\u0001"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotCT != "application/json" || gotPath != "/api/v1/userstories/7" || gotQuery != "project=37" {
		t.Fatalf("auth=%q ct=%q path=%q q=%q", gotAuth, gotCT, gotPath, gotQuery)
	}
	if gotBody["subject"] != "Ação\u0001" || string(resp.Body) != `{"ok":true}` {
		t.Fatalf("body=%v resp=%s", gotBody, resp.Body)
	}
}

func TestDoRetriesGETOn5xxButNotPATCH(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if r.Method == "GET" && n < 3 {
			w.WriteHeader(502)
			return
		}
		if r.Method == "PATCH" {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{Type: "Bearer", Value: "t"}, WithRetryWait(0))
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "projects"}); err != nil {
		t.Fatalf("GET should succeed after retries: %v", err)
	}
	atomic.StoreInt32(&calls, 0)
	_, err := c.Do(context.Background(), Request{Method: "PATCH", Path: "userstories/1", Body: map[string]any{}})
	if err == nil || atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("PATCH must not retry: calls=%d err=%v", calls, err)
	}
}

func TestToOutputMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		code   string
		exit   int
	}{
		{401, `{"detail":"x"}`, "auth_rejected", output.ExitAuth},
		{403, `{}`, "forbidden", output.ExitForbidden},
		{404, `{}`, "not_found", output.ExitNotFound},
		{400, `{"version":"The version parameter is not valid"}`, "version_conflict", output.ExitConflict},
		{409, `{}`, "version_conflict", output.ExitConflict},
		{400, `{"subject":["required"]}`, "invalid_request", output.ExitUsage},
		{500, `oops`, "server_error", output.ExitNetwork},
	}
	for _, c := range cases {
		e := ToOutput(&APIError{Status: c.status, Method: "PATCH", Path: "userstories/1", Body: []byte(c.body)})
		if e.Code != c.code || e.Exit != c.exit || e.Stage != "PATCH userstories/1" || e.Cause != c.body {
			t.Fatalf("%d: %+v", c.status, e)
		}
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/taiga/...`
Expected: FAIL, com `undefined: New`.

- [ ] **Step 3: implementar**

`internal/taiga/errors.go`:

```go
package taiga

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/BasisTI/taiga-cli/internal/output"
)

// APIError is a non-2xx response from Taiga.
type APIError struct {
	Status       int
	Method, Path string
	Body         []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.Status, truncate(e.Body))
}

// IsVersionConflict reports Taiga's optimistic-concurrency rejection (409, or 400 with a "version" key).
func (e *APIError) IsVersionConflict() bool {
	if e.Status == 409 {
		return true
	}
	if e.Status != 400 {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(e.Body, &m) != nil {
		return false
	}
	_, ok := m["version"]
	return ok
}

// NetworkError wraps transport failures.
type NetworkError struct {
	Method, Path string
	Err          error
}

func (e *NetworkError) Error() string { return fmt.Sprintf("%s %s: %v", e.Method, e.Path, e.Err) }
func (e *NetworkError) Unwrap() error { return e.Err }

func truncate(b []byte) string {
	if len(b) > 2000 {
		return string(b[:2000]) + "…"
	}
	return string(b)
}

// ToOutput maps client errors to the stable error envelope.
func ToOutput(err error) *output.Error {
	var oe *output.Error
	if errors.As(err, &oe) {
		return oe
	}
	var ne *NetworkError
	if errors.As(err, &ne) {
		return &output.Error{Code: "network_error", Source: "network", Stage: ne.Method + " " + ne.Path, Cause: ne.Err.Error(), Recovery: "check connectivity to the Taiga URL (sandboxed agents need network access)", Exit: output.ExitNetwork}
	}
	var ae *APIError
	if !errors.As(err, &ae) {
		return output.AsError(err)
	}
	e := &output.Error{Source: "api", Stage: ae.Method + " " + ae.Path, Cause: truncate(ae.Body)}
	switch {
	case ae.Status == 401:
		e.Code, e.Exit, e.Recovery = "auth_rejected", output.ExitAuth, "run `taiga auth status --diagnose`"
	case ae.Status == 403:
		e.Code, e.Exit, e.Recovery = "forbidden", output.ExitForbidden, "the account lacks permission for this operation"
	case ae.Status == 404:
		e.Code, e.Exit = "not_found", output.ExitNotFound
	case ae.IsVersionConflict():
		e.Code, e.Exit, e.Recovery = "version_conflict", output.ExitConflict, "re-read the resource and retry"
	case ae.Status >= 500:
		e.Code, e.Exit = "server_error", output.ExitNetwork
	default:
		e.Code, e.Exit = "invalid_request", output.ExitUsage
	}
	return e
}
```

`internal/taiga/client.go`:

```go
// Package taiga is an HTTP client for the Taiga REST API v1.
package taiga

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Token struct{ Type, Value string }

type TokenSource interface {
	Token(ctx context.Context) (Token, error)
}

// StaticToken is a fixed token (TAIGA_TOKEN, tests).
type StaticToken Token

func (s StaticToken) Token(context.Context) (Token, error) { return Token(s), nil }

type Client struct {
	base      string
	http      *http.Client
	tokens    TokenSource
	retryWait time.Duration
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option    { return func(c *Client) { c.http = h } }
func WithRetryWait(d time.Duration) Option    { return func(c *Client) { c.retryWait = d } }

// New builds a client for baseURL (scheme://host, already normalized).
func New(baseURL string, ts TokenSource, opts ...Option) *Client {
	c := &Client{base: strings.TrimSuffix(baseURL, "/"), http: &http.Client{Timeout: 30 * time.Second}, tokens: ts, retryWait: 500 * time.Millisecond}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) BaseURL() string { return c.base }

type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   any
	Header http.Header
}

type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Do sends one API request. GET is retried up to twice on network errors and 5xx.
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	var payload []byte
	if r.Body != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(r.Body); err != nil {
			return nil, err
		}
		payload = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	}
	tok, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	target := c.base + "/api/v1/" + strings.TrimPrefix(r.Path, "/")
	if len(r.Query) > 0 {
		target += "?" + r.Query.Encode()
	}
	attempts := 1
	if r.Method == http.MethodGet {
		attempts = 3
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 && c.retryWait > 0 {
			select {
			case <-ctx.Done():
				return nil, &NetworkError{Method: r.Method, Path: r.Path, Err: ctx.Err()}
			case <-time.After(c.retryWait * time.Duration(i)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, r.Method, target, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		for k, vs := range r.Header {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		if tok.Value != "" {
			req.Header.Set("Authorization", tok.Type+" "+tok.Value)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = &NetworkError{Method: r.Method, Path: r.Path, Err: err}
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = &NetworkError{Method: r.Method, Path: r.Path, Err: err}
			continue
		}
		if resp.StatusCode >= 500 {
			lastErr = &APIError{Status: resp.StatusCode, Method: r.Method, Path: r.Path, Body: body}
			continue
		}
		if resp.StatusCode >= 300 {
			return nil, &APIError{Status: resp.StatusCode, Method: r.Method, Path: r.Path, Body: body}
		}
		return &Response{Status: resp.StatusCode, Header: resp.Header, Body: body}, nil
	}
	return nil, lastErr
}
```

- [ ] **Step 4: rodar e ver passar**

Run: `gofmt -w internal/taiga && go test -race ./internal/taiga/...`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add -A
git commit -m "Adicionar cliente HTTP da API v1 com erros tipados"
```

---

### Task 7: paginação transparente e escrita com `version` (US #245)

**Files:**
- Create: `internal/taiga/pagination.go`, `internal/taiga/versioned.go`, `internal/taiga/versioned_test.go`, `internal/taiga/pagination_test.go`
- Modify: `internal/taiga/errors.go` (acrescentar `ConflictError` e o mapeamento dele)

**Interfaces:**
- Consumes: `Client.Do`, `Request`, `APIError`
- Produces:
  - `func (c *Client) GetAll(ctx context.Context, path string, q url.Values) ([]json.RawMessage, error)`
  - `func (c *Client) PrepareVersioned(ctx context.Context, path string, patch map[string]any) (body map[string]any, current map[string]json.RawMessage, err error)`
  - `func (c *Client) WriteVersioned(ctx context.Context, method, path string, patch map[string]any, force bool) (*Response, error)`
  - `type ConflictError struct{ Method, Path string; Fields []string }`, mapeado por `ToOutput` para `version_conflict` com exit 4

**Regra de escrita com `version`** (spec, seção 6):
1. lê o recurso;
2. envia o patch com o `version` lido;
3. num conflito, relê uma vez;
4. se algum campo do patch mudou entre as duas leituras e `force` é falso, devolve `ConflictError{Fields}`;
5. senão, repete uma única vez com o novo `version`.

- [ ] **Step 1: escrever os testes que falham**

`internal/taiga/pagination_test.go`:

```go
package taiga

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAllFollowsPagesAndSendsDisableHeader(t *testing.T) {
	var sawHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("x-disable-pagination") == "True"
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			w.Header().Set("x-pagination-next", "http://x/?page=2")
			fmt.Fprint(w, `[{"id":1},{"id":2}]`)
			return
		}
		fmt.Fprint(w, `[{"id":3}]`)
	}))
	defer srv.Close()
	c := New(srv.URL, StaticToken{Type: "Bearer", Value: "t"}, WithRetryWait(0))
	items, err := c.GetAll(context.Background(), "userstories", nil)
	if err != nil || len(items) != 3 || !sawHeader {
		t.Fatalf("items=%d err=%v header=%v", len(items), err, sawHeader)
	}
}

func TestGetAllRejectsNonArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"id":1}`) }))
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	if _, err := c.GetAll(context.Background(), "userstories/1", nil); err == nil {
		t.Fatal("expected error for non-array")
	}
}
```

`internal/taiga/versioned_test.go`:

```go
package taiga

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeStory simulates Taiga OCC: PATCH must carry the current version.
type fakeStory struct {
	mu      sync.Mutex
	version int
	fields  map[string]any
	patches int
}

func (f *fakeStory) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case "GET":
			out := map[string]any{"version": f.version}
			for k, v := range f.fields {
				out[k] = v
			}
			_ = json.NewEncoder(w).Encode(out)
		case "PATCH":
			f.patches++
			b, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(b, &body)
			if int(body["version"].(float64)) != f.version {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"version":"The version doesn't match with the current one"}`))
				return
			}
			for k, v := range body {
				if k != "version" {
					f.fields[k] = v
				}
			}
			f.version++
			_ = json.NewEncoder(w).Encode(map[string]any{"version": f.version})
		}
	})
}

func TestWriteVersionedHappyPath(t *testing.T) {
	f := &fakeStory{version: 3, fields: map[string]any{"status": 1.0}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	if _, err := c.WriteVersioned(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, false); err != nil {
		t.Fatal(err)
	}
	if f.fields["status"] != 2.0 || f.version != 4 || f.patches != 1 {
		t.Fatalf("%+v", f)
	}
}

func TestWriteVersionedRetriesWhenOtherFieldsChanged(t *testing.T) {
	f := &fakeStory{version: 3, fields: map[string]any{"status": 1.0, "subject": "a"}}
	// between our first read and our PATCH someone edits the subject (not our field)
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	origHandler := f.handler()
	var once sync.Once
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			once.Do(func() { f.mu.Lock(); f.fields["subject"] = "b"; f.version = 4; f.mu.Unlock() })
		}
		origHandler.ServeHTTP(w, r)
	})
	if _, err := c.WriteVersioned(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, false); err != nil {
		t.Fatalf("should retry: %v", err)
	}
	if f.fields["status"] != 2.0 || f.fields["subject"] != "b" || f.patches != 2 {
		t.Fatalf("%+v", f)
	}
}

func TestWriteVersionedConflictsWhenOurFieldChanged(t *testing.T) {
	f := &fakeStory{version: 3, fields: map[string]any{"status": 1.0}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	origHandler := f.handler()
	var once sync.Once
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			once.Do(func() { f.mu.Lock(); f.fields["status"] = 5.0; f.version = 4; f.mu.Unlock() })
		}
		origHandler.ServeHTTP(w, r)
	})
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	_, err := c.WriteVersioned(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, false)
	var ce *ConflictError
	if !errors.As(err, &ce) || len(ce.Fields) != 1 || ce.Fields[0] != "status" {
		t.Fatalf("want ConflictError{status}, got %v", err)
	}
	if ToOutput(err).Exit != 4 {
		t.Fatal("exit must be 4")
	}
	// force overrides
	if _, err := c.WriteVersioned(context.Background(), "PATCH", "userstories/1", map[string]any{"status": 2}, true); err != nil {
		t.Fatalf("force: %v", err)
	}
}

func TestPrepareVersionedDoesNotWrite(t *testing.T) {
	f := &fakeStory{version: 9, fields: map[string]any{}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := New(srv.URL, StaticToken{}, WithRetryWait(0))
	body, _, err := c.PrepareVersioned(context.Background(), "userstories/1", map[string]any{"comment": "x"})
	if err != nil || body["version"] != json.Number("9") || f.patches != 0 {
		t.Fatalf("body=%v err=%v patches=%d", body, err, f.patches)
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/taiga/...`
Expected: FAIL, com `undefined: (*Client).GetAll`.

- [ ] **Step 3: implementar**

`internal/taiga/pagination.go`:

```go
package taiga

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// GetAll returns every item of a list endpoint, asking Taiga to disable pagination and following pages if it paginates anyway.
func (c *Client) GetAll(ctx context.Context, path string, q url.Values) ([]json.RawMessage, error) {
	var all []json.RawMessage
	for page := 1; page <= 1000; page++ {
		qq := url.Values{}
		for k, v := range q {
			qq[k] = append([]string(nil), v...)
		}
		if page > 1 {
			qq.Set("page", strconv.Itoa(page))
		}
		resp, err := c.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: qq, Header: http.Header{"x-disable-pagination": {"True"}}})
		if err != nil {
			return nil, err
		}
		var items []json.RawMessage
		if err := json.Unmarshal(resp.Body, &items); err != nil {
			return nil, fmt.Errorf("GET %s: expected a JSON array: %w", path, err)
		}
		all = append(all, items...)
		if resp.Header.Get("x-pagination-next") == "" {
			return all, nil
		}
	}
	return nil, fmt.Errorf("GET %s: more than 1000 pages", path)
}
```

`internal/taiga/versioned.go`:

```go
package taiga

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
)

func (c *Client) getObject(ctx context.Context, path string) (map[string]json.RawMessage, error) {
	resp, err := c.Do(ctx, Request{Method: http.MethodGet, Path: path})
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body, &m); err != nil {
		return nil, fmt.Errorf("GET %s: expected a JSON object: %w", path, err)
	}
	if _, ok := m["version"]; !ok {
		return nil, fmt.Errorf("GET %s: resource has no version field", path)
	}
	return m, nil
}

func withVersion(patch map[string]any, version json.RawMessage) map[string]any {
	body := make(map[string]any, len(patch)+1)
	for k, v := range patch {
		body[k] = v
	}
	body["version"] = json.Number(bytes.TrimSpace(version))
	return body
}

// PrepareVersioned reads the resource and returns the body that would be sent (for --dry-run).
func (c *Client) PrepareVersioned(ctx context.Context, path string, patch map[string]any) (map[string]any, map[string]json.RawMessage, error) {
	cur, err := c.getObject(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	return withVersion(patch, cur["version"]), cur, nil
}

// WriteVersioned applies patch with optimistic concurrency and one guarded retry.
func (c *Client) WriteVersioned(ctx context.Context, method, path string, patch map[string]any, force bool) (*Response, error) {
	body, first, err := c.PrepareVersioned(ctx, path, patch)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(ctx, Request{Method: method, Path: path, Body: body})
	var ae *APIError
	if err == nil || !errors.As(err, &ae) || !ae.IsVersionConflict() {
		return resp, err
	}
	second, err := c.getObject(ctx, path)
	if err != nil {
		return nil, err
	}
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

func changedKeys(patch map[string]any, a, b map[string]json.RawMessage) []string {
	var out []string
	for k := range patch {
		if k == "version" {
			continue
		}
		if !jsonEqual(a[k], b[k]) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func jsonEqual(x, y json.RawMessage) bool {
	var bx, by bytes.Buffer
	if len(x) > 0 && json.Compact(&bx, x) != nil {
		return false
	}
	if len(y) > 0 && json.Compact(&by, y) != nil {
		return false
	}
	return bytes.Equal(bx.Bytes(), by.Bytes())
}
```

Acrescente em `internal/taiga/errors.go`:

```go
// ConflictError means another writer changed the fields we are updating.
type ConflictError struct {
	Method, Path string
	Fields       []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s %s: version conflict on %v", e.Method, e.Path, e.Fields)
}
```

Em `ToOutput`, depois do bloco de `NetworkError`:

```go
	var ce *ConflictError
	if errors.As(err, &ce) {
		cause := "resource changed concurrently"
		if len(ce.Fields) > 0 {
			cause = fmt.Sprintf("fields changed by someone else: %v", ce.Fields)
		}
		return &output.Error{Code: "version_conflict", Source: "api", Stage: ce.Method + " " + ce.Path, Cause: cause, Recovery: "re-read the resource and retry; use --force-version to override", Exit: output.ExitConflict}
	}
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test -race ./internal/taiga/...`
Expected: PASS.


- [ ] **Step 5: commit**

```bash
git add -A
git commit -m "Adicionar paginação transparente e escrita com version"
```

---

### Task 8: comando `taiga api` (US #245)

**Files:**
- Create: `internal/cli/api.go`, `internal/cli/api_test.go`, `internal/cli/context.go`, `internal/cli/api_integration_test.go`
- Modify: `internal/cli/root.go` (flags globais `--url` e `--project`, registro do comando, campos do `App`)

**Interfaces:**
- Consumes:
  - `config.DefaultPaths`, `config.Load`, `config.Resolve`
  - `taiga.New`, `Client.Do/GetAll/WriteVersioned/PrepareVersioned`, `taiga.ToOutput`, `taiga.StaticToken`
  - `output.WriteJSON`
- Produces:
  - campos novos em `App`: `Cwd string`, `HTTP *http.Client`, `flagURL`, `flagProject string` e `TokenSource func(ctx context.Context, rc *RunContext) (taiga.TokenSource, error)`
  - `type RunContext struct{ Paths config.Paths; File config.File; Ctx config.Context }`
  - `func (a *App) runContext() (*RunContext, error)`
  - `func (a *App) client(ctx context.Context, rc *RunContext) (*taiga.Client, error)`

Nesta tarefa, `App.TokenSource` usa só `TAIGA_TOKEN` (com `TAIGA_TOKEN_TYPE`). A tarefa 12 troca pelo resolver de autenticação.

**Uso do comando:**

```
taiga api METHOD PATH [--query k=v]... [--field k=v]... [--raw-field k=v]... [--input FILE|-]
                      [--paginate] [--auto-version] [--force-version] [--dry-run] [--confirm-delete]
```

**Regras:**
- `--field`: o valor é interpretado como JSON se for JSON válido (número, `true`, `null`, array, objeto, string entre aspas); senão, vira string.
- `--raw-field`: o valor é sempre string.
- `--input` lê um objeto JSON de arquivo, ou do stdin com `-`. Os `--field` se sobrepõem às chaves dele.
- `--paginate` só vale para `GET` e usa `GetAll`.
- `--auto-version` só vale para `PATCH` e `PUT` e usa `WriteVersioned`.
- `DELETE` sem `--confirm-delete` é recusado com exit 2 (`delete_not_confirmed`).
- `--dry-run`:
  - com `--auto-version`, lê o recurso para obter o `version`;
  - imprime `{"dry_run":true,"method","url","query","body"}`;
  - não envia a escrita. Um `GET` com `--dry-run` também não é enviado.
- Saída: o corpo da resposta como JSON indentado (ou array com `--paginate`), independente do TTY. Corpo vazio não imprime nada.

- [ ] **Step 1: escrever os testes que falham**

`internal/cli/api_test.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recorded struct {
	method, path, query, auth string
	body                      map[string]any
}

func fakeTaiga(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]recorded) {
	t.Helper()
	var calls []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		calls = append(calls, recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), body})
		handle(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func runIn(t *testing.T, env map[string]string, stdin string, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	if env == nil {
		env = map[string]string{}
	}
	if env["HOME"] == "" {
		env["HOME"] = t.TempDir()
	}
	a := &App{In: strings.NewReader(stdin), Out: &out, Err: &errOut, Env: func(k string) string { return env[k] }, Cwd: t.TempDir()}
	code := a.Run(args)
	return out.String(), errOut.String(), code
}

func TestAPIGetWithQuery(t *testing.T) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[{"id":1}]`)) })
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	out, errOut, code := runIn(t, env, "", "api", "GET", "userstories", "--query", "project=37")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	c := (*calls)[0]
	if c.path != "/api/v1/userstories" || c.query != "project=37" || c.auth != "Bearer tok" {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(out, `"id": 1`) {
		t.Fatalf("out = %s", out)
	}
}

func TestAPIFieldTypingAndControlChars(t *testing.T) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	weird := "linha1\nlinha2\t\u0001 \"aspas\" ação"
	_, errOut, code := runIn(t, env, "", "api", "POST", "userstories",
		"--field", "project=37", "--field", "tags=[\"a\",\"b\"]", "--field", "subject="+weird, "--raw-field", "ref=12", "--field", "note=12 abc")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	b := (*calls)[0].body
	if b["project"] != 37.0 || b["subject"] != weird || b["ref"] != "12" || b["note"] != "12 abc" || len(b["tags"].([]any)) != 2 {
		t.Fatalf("body = %#v", b)
	}
}

func TestAPIDeleteNeedsConfirmation(t *testing.T) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	_, errOut, code := runIn(t, env, "", "api", "DELETE", "userstories/1")
	if code != 2 || len(*calls) != 0 || !strings.Contains(errOut, "delete_not_confirmed") {
		t.Fatalf("code=%d calls=%d err=%s", code, len(*calls), errOut)
	}
	_, _, code = runIn(t, env, "", "api", "DELETE", "userstories/1", "--confirm-delete")
	if code != 0 || len(*calls) != 1 {
		t.Fatalf("confirmed delete: code=%d calls=%d", code, len(*calls))
	}
}

func TestAPIDryRunAutoVersionDoesNotWrite(t *testing.T) {
	srv, calls := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"version":7,"subject":"x"}`)) })
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	out, errOut, code := runIn(t, env, "", "api", "PATCH", "userstories/1", "--field", "comment=oi", "--auto-version", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, c := range *calls {
		if c.method != "GET" {
			t.Fatalf("dry-run sent %s", c.method)
		}
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	body := got["body"].(map[string]any)
	if got["dry_run"] != true || body["version"] != 7.0 || body["comment"] != "oi" {
		t.Fatalf("out = %s", out)
	}
}

func TestAPIMapsHTTPErrorsToExitCodes(t *testing.T) {
	srv, _ := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404); w.Write([]byte(`{"_error_message":"No UserStory matches"}`)) })
	env := map[string]string{"TAIGA_URL": srv.URL, "TAIGA_TOKEN": "tok"}
	_, errOut, code := runIn(t, env, "", "api", "GET", "userstories/999")
	if code != 5 || !strings.Contains(errOut, `"code": "not_found"`) || !strings.Contains(errOut, "No UserStory matches") {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}

func TestAPIWithoutURLIsUsageError(t *testing.T) {
	_, errOut, code := runIn(t, map[string]string{"TAIGA_TOKEN": "t"}, "", "api", "GET", "projects")
	if code != 2 || !strings.Contains(errOut, "config_no_url") {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}
```

Os testes chamam `a.Run(args)`. Extraia de `Main` um método `func (a *App) Run(args []string) int` que faça o trabalho, e deixe `Main` montar o `App` a partir dos argumentos e chamar `Run`. Com isso, o `root_test.go` continua valendo.

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/cli/...`
Expected: FAIL, com `a.Run undefined` e `unknown command "api"`.

- [ ] **Step 3: implementar**

`internal/cli/context.go`:

```go
package cli

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/BasisTI/taiga-cli/internal/config"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

type RunContext struct {
	Paths config.Paths
	File  config.File
	Ctx   config.Context
}

func (a *App) runContext() (*RunContext, error) {
	paths, err := config.DefaultPaths(a.Env)
	if err != nil {
		return nil, err
	}
	file, err := config.Load(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	ctx, err := config.Resolve(config.Inputs{FlagURL: a.flagURL, FlagProject: a.flagProject, Env: a.Env, Cwd: a.Cwd, File: file, ConfigPath: paths.ConfigFile})
	if err != nil {
		return nil, err
	}
	return &RunContext{Paths: paths, File: file, Ctx: ctx}, nil
}

// envTokenSource is the phase-1 bootstrap source; task 12 replaces App.TokenSource with the auth resolver.
func envTokenSource(a *App) func(context.Context, *RunContext) (taiga.TokenSource, error) {
	return func(context.Context, *RunContext) (taiga.TokenSource, error) {
		tok := a.Env("TAIGA_TOKEN")
		if tok == "" {
			return nil, &output.Error{Code: "auth_no_source", Source: "env", Cause: "TAIGA_TOKEN is not set", Recovery: "set TAIGA_TOKEN", Exit: output.ExitAuth}
		}
		typ := a.Env("TAIGA_TOKEN_TYPE")
		if typ == "" {
			typ = "Bearer"
		}
		return taiga.StaticToken{Type: typ, Value: tok}, nil
	}
}

func (a *App) client(ctx context.Context, rc *RunContext) (*taiga.Client, error) {
	ts, err := a.TokenSource(ctx, rc)
	if err != nil {
		return nil, err
	}
	return taiga.New(rc.Ctx.URL.Value, ts, taiga.WithHTTPClient(a.httpClient())), nil
}

func (a *App) httpClient() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: httpTimeout}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
```

Em `root.go`:
- declare `const httpTimeout = 30 * time.Second`;
- acrescente ao `App` os campos `Cwd string`, `HTTP *http.Client`, `flagURL`, `flagProject string` e `TokenSource func(context.Context, *RunContext) (taiga.TokenSource, error)`;
- registre as flags persistentes `--url` (em `a.flagURL`) e `--project` (em `a.flagProject`);
- adicione `root.AddCommand(a.apiCmd())`;
- em `Run`, se `a.TokenSource == nil`, faça `a.TokenSource = envTokenSource(a)`; se `a.Cwd == ""`, use `os.Getwd()`.

`Main` passa a ser:

```go
func Main(args []string, in io.Reader, out, errOut io.Writer, env func(string) string, outTTY bool) int {
	a := &App{In: in, Out: out, Err: errOut, Env: env, OutTTY: outTTY}
	return a.Run(args)
}
```

`Run` contém o corpo antigo de `Main`: monta o root, executa e escreve o erro.

`internal/cli/api.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

func (a *App) apiCmd() *cobra.Command {
	var queries, fields, rawFields []string
	var input string
	var paginate, autoVersion, forceVersion, dryRun, confirmDelete bool
	cmd := &cobra.Command{
		Use:   "api METHOD PATH",
		Short: "Call any Taiga API v1 endpoint (PATH is relative to /api/v1/)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			method, path := strings.ToUpper(args[0]), strings.TrimPrefix(args[1], "/")
			usage := func(code, cause string) error {
				return &output.Error{Code: code, Source: "flag", Cause: cause, Exit: output.ExitUsage}
			}
			if method == "DELETE" && !confirmDelete {
				return usage("delete_not_confirmed", "DELETE requires --confirm-delete")
			}
			if paginate && method != "GET" {
				return usage("usage", "--paginate only applies to GET")
			}
			if autoVersion && method != "PATCH" && method != "PUT" {
				return usage("usage", "--auto-version only applies to PATCH and PUT")
			}
			q := url.Values{}
			for _, kv := range queries {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return usage("usage", "--query expects key=value: "+kv)
				}
				q.Add(k, v)
			}
			body, err := a.buildBody(input, fields, rawFields)
			if err != nil {
				return err
			}
			rc, err := a.runContext()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			c, err := a.client(ctx, rc)
			if err != nil {
				return err
			}
			if dryRun {
				if autoVersion {
					if body, _, err = c.PrepareVersioned(ctx, path, body); err != nil {
						return taiga.ToOutput(err)
					}
				}
				target := c.BaseURL() + "/api/v1/" + path
				return output.WriteJSON(a.Out, map[string]any{"dry_run": true, "method": method, "url": target, "query": q, "body": body})
			}
			var resp *taiga.Response
			switch {
			case paginate:
				items, err := c.GetAll(ctx, path, q)
				if err != nil {
					return taiga.ToOutput(err)
				}
				return output.WriteJSON(a.Out, items)
			case autoVersion:
				resp, err = c.WriteVersioned(ctx, method, path, body, forceVersion)
			default:
				var reqBody any
				if body != nil {
					reqBody = body
				}
				resp, err = c.Do(ctx, taiga.Request{Method: method, Path: path, Query: q, Body: reqBody})
			}
			if err != nil {
				return taiga.ToOutput(err)
			}
			if len(bytes.TrimSpace(resp.Body)) == 0 {
				return nil
			}
			var pretty any
			if json.Unmarshal(resp.Body, &pretty) != nil {
				_, err = a.Out.Write(resp.Body)
				return err
			}
			return output.WriteJSON(a.Out, pretty)
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&queries, "query", nil, "query parameter key=value (repeatable)")
	f.StringArrayVarP(&fields, "field", "F", nil, "body field key=value; value parsed as JSON when valid (repeatable)")
	f.StringArrayVarP(&rawFields, "raw-field", "f", nil, "body field key=value; value always a string (repeatable)")
	f.StringVar(&input, "input", "", "read the JSON body from a file, or - for stdin")
	f.BoolVar(&paginate, "paginate", false, "GET every page and print one JSON array")
	f.BoolVar(&autoVersion, "auto-version", false, "read the current version and send it (PATCH/PUT)")
	f.BoolVar(&forceVersion, "force-version", false, "with --auto-version, retry even if the same fields changed concurrently")
	f.BoolVar(&dryRun, "dry-run", false, "print the request instead of sending it")
	f.BoolVar(&confirmDelete, "confirm-delete", false, "required to send DELETE")
	return cmd
}

func (a *App) buildBody(input string, fields, rawFields []string) (map[string]any, error) {
	var body map[string]any
	if input != "" {
		var r io.Reader
		if input == "-" {
			r = a.In
		} else {
			f, err := os.Open(input)
			if err != nil {
				return nil, &output.Error{Code: "usage", Source: "flag", Cause: err.Error(), Exit: output.ExitUsage}
			}
			defer f.Close()
			r = f
		}
		dec := json.NewDecoder(r)
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			return nil, &output.Error{Code: "usage", Source: "flag", Cause: "--input must be a JSON object: " + err.Error(), Exit: output.ExitUsage}
		}
	}
	set := func(kv string, raw bool) error {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return &output.Error{Code: "usage", Source: "flag", Cause: "field expects key=value: " + kv, Exit: output.ExitUsage}
		}
		if body == nil {
			body = map[string]any{}
		}
		if !raw && json.Valid([]byte(v)) {
			dec := json.NewDecoder(strings.NewReader(v))
			dec.UseNumber()
			var parsed any
			if dec.Decode(&parsed) == nil {
				body[k] = parsed
				return nil
			}
		}
		body[k] = v
		return nil
	}
	for _, kv := range fields {
		if err := set(kv, false); err != nil {
			return nil, err
		}
	}
	for _, kv := range rawFields {
		if err := set(kv, true); err != nil {
			return nil, err
		}
	}
	return body, nil
}
```


- [ ] **Step 4: rodar e ver passar**

Run: `go test -race ./internal/cli/...`
Expected: PASS.

- [ ] **Step 5: teste de integração do `taiga api`**

`internal/cli/api_integration_test.go`:

```go
//go:build integration

package cli

import (
	"encoding/json"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func TestIntegrationAPIProjectsAndUsersMe(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token}
	out, errOut, code := runIn(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+testtaiga.ProjectSlug)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(out), &p); err != nil || p["slug"] != testtaiga.ProjectSlug {
		t.Fatalf("project: %s", out)
	}
	out, _, code = runIn(t, env, "", "api", "GET", "projects", "--paginate")
	var list []any
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list) == 0 {
		t.Fatalf("paginate: code=%d out=%s", code, out)
	}
	_, errOut, code = runIn(t, env, "", "api", "GET", "userstories/999999")
	if code != 5 {
		t.Fatalf("not found must exit 5: %d %s", code, errOut)
	}
}

func TestIntegrationAutoVersionOnStory(t *testing.T) {
	token, _ := testtaiga.Login(t, testtaiga.AdminUser, testtaiga.AdminPassword)
	env := map[string]string{"TAIGA_URL": testtaiga.URL(), "TAIGA_TOKEN": token}
	out, _, _ := runIn(t, env, "", "api", "GET", "projects/by_slug", "--query", "slug="+testtaiga.ProjectSlug)
	var p map[string]any
	_ = json.Unmarshal([]byte(out), &p)
	pid := strconv.Itoa(int(p["id"].(float64)))
	out, errOut, code := runIn(t, env, "", "api", "POST", "userstories", "--field", "project="+pid, "--field", "subject=auto-version")
	if code != 0 {
		t.Fatalf("create story: %d %s", code, errOut)
	}
	var us map[string]any
	_ = json.Unmarshal([]byte(out), &us)
	sid := strconv.Itoa(int(us["id"].(float64)))
	desc := "integração \"auto-version\"\ncom quebra\t\u0001"
	_, errOut, code = runIn(t, env, "", "api", "PATCH", "userstories/"+sid, "--field", "description="+desc, "--auto-version")
	if code != 0 {
		t.Fatalf("patch exit %d: %s", code, errOut)
	}
	out, _, _ = runIn(t, env, "", "api", "GET", "userstories/"+sid)
	_ = json.Unmarshal([]byte(out), &us)
	if us["description"] != desc {
		t.Fatalf("description = %q", us["description"])
	}
}
```

Acrescente `"strconv"` aos imports do arquivo. Os `--field` com texto que não é JSON válido viram string, então a descrição chega intacta.

Run: `go test -tags integration -p 1 ./internal/cli/...`
Expected: PASS.

- [ ] **Step 6: commit, PR da US #245 e status**

```bash
gofmt -l . && go vet ./... && golangci-lint run ./...
git add -A
git commit -m "Adicionar comando taiga api com dry-run, paginação e auto-version"
git push -u origin TG-245
gh pr create --title "TG-245 Núcleo do cliente HTTP" --body "US #245 (Taiga Infraestrutura). Cliente da API v1, version, paginação, envelope de erro e taiga api."
```

Peça a revisão do Codex. Depois do merge com squash:
1. mude a US #245 para `Ready for test`;
2. rode `git switch main && git pull`;
3. mude a US #244 para `In progress`;
4. crie a próxima branch com `git switch -c TG-244`.

---

### Task 9: cache de sessão, expiração do JWT e `flock` (US #244)

**Files:**
- Create: `internal/auth/session.go`, `internal/auth/session_test.go`

**Interfaces:**
- Consumes: `config.WriteFileAtomic`
- Produces:
  - `type Session struct{ URL, Username, AuthToken, Refresh string; UserID int64; Expiry time.Time }`, com as tags json `url`, `username`, `auth_token`, `refresh`, `user_id`, `exp`
  - `func SessionRef(url, username string) string`: sha256 hex de `url + "\x00" + username`
  - `type Store struct{ Dir string }`, onde `Dir` é o state dir
  - `func (s Store) Path(ref string) string`: `<Dir>/sessions/<ref>.json`
  - `func (s Store) Load(ref string) (Session, error)`: arquivo ausente devolve um erro que satisfaz `errors.Is(err, fs.ErrNotExist)`
  - `func (s Store) Save(ref string, sess Session) error`: diretório somente leitura devolve um erro que satisfaz `errors.Is(err, ErrReadOnly)`
  - `func (s Store) Lock(ctx context.Context, ref string) (unlock func(), err error)`: `flock` exclusivo em `<Dir>/sessions/<ref>.lock`; somente leitura devolve `ErrReadOnly`
  - `func (s Store) Delete(ref string) error`
  - `func (s Store) Writable() bool`
  - `var ErrReadOnly = errors.New("session store is read-only")`
  - `func JWTExpiry(token string) (time.Time, error)`

- [ ] **Step 1: escrever o teste que falha**

`internal/auth/session_test.go`:

```go
package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fakeJWT(exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + itoa64(exp.Unix()) + `}`))
	return "eyJhbGciOiJIUzI1NiJ9." + payload + ".sig"
}

func TestJWTExpiry(t *testing.T) {
	exp := time.Unix(1790000000, 0)
	got, err := JWTExpiry(fakeJWT(exp))
	if err != nil || !got.Equal(exp) {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := JWTExpiry("not-a-jwt"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSessionRefIsStable(t *testing.T) {
	if SessionRef("https://a", "u") != SessionRef("https://a", "u") || SessionRef("https://a", "u") == SessionRef("https://a", "v") {
		t.Fatal("ref must depend on url and user only")
	}
}

func TestStoreSaveLoadPermissions(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	ref := SessionRef("https://a", "u")
	if _, err := s.Load(ref); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	in := Session{URL: "https://a", Username: "u", AuthToken: "t", Refresh: "r", UserID: 7, Expiry: time.Unix(1790000000, 0).UTC()}
	if err := s.Save(ref, in); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(s.Path(ref))
	dinfo, _ := os.Stat(filepath.Dir(s.Path(ref)))
	if info.Mode().Perm() != 0o600 || dinfo.Mode().Perm() != 0o700 {
		t.Fatalf("perm file=%v dir=%v", info.Mode().Perm(), dinfo.Mode().Perm())
	}
	out, err := s.Load(ref)
	if err != nil || out != in {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestStoreReadOnly(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o700)
	_ = os.Chmod(filepath.Join(dir, "sessions"), 0o500)
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "sessions"), 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	s := Store{Dir: dir}
	if err := s.Save("x", Session{}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("save: %v", err)
	}
	if _, err := s.Lock(context.Background(), "x"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("lock: %v", err)
	}
	if s.Writable() {
		t.Fatal("Writable must be false")
	}
}

func TestLockIsExclusive(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	var inside, maxInside int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := s.Lock(context.Background(), "r")
			if err != nil {
				t.Error(err)
				return
			}
			n := atomic.AddInt32(&inside, 1)
			for {
				m := atomic.LoadInt32(&maxInside)
				if n <= m || atomic.CompareAndSwapInt32(&maxInside, m, n) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&inside, -1)
			unlock()
		}()
	}
	wg.Wait()
	if maxInside != 1 {
		t.Fatalf("max concurrent holders = %d", maxInside)
	}
}
```

Em `internal/auth/helpers_test.go`: `func itoa64(i int64) string { return strconv.FormatInt(i, 10) }`.

Observação: `flock` é por descritor de arquivo. Cada `Lock` abre um descritor novo, então funciona entre goroutines e entre processos.

- [ ] **Step 2: rodar e ver falhar**

Run: `go get golang.org/x/sys@latest && go test ./internal/auth/...`
Expected: FAIL, com `undefined: JWTExpiry`.

- [ ] **Step 3: implementar**

`internal/auth/session.go`:

```go
// Package auth resolves Taiga credentials: env, session cache, refresh and secret sources.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/BasisTI/taiga-cli/internal/config"
	"golang.org/x/sys/unix"
)

var ErrReadOnly = errors.New("session store is read-only")

type Session struct {
	URL       string    `json:"url"`
	Username  string    `json:"username"`
	AuthToken string    `json:"auth_token"`
	Refresh   string    `json:"refresh"`
	UserID    int64     `json:"user_id"`
	Expiry    time.Time `json:"exp"`
}

func SessionRef(url, username string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimRight(url, "/")+"\x00"+username)))
}

type Store struct{ Dir string }

func (s Store) Path(ref string) string { return filepath.Join(s.Dir, "sessions", ref+".json") }

func (s Store) Load(ref string) (Session, error) {
	var sess Session
	b, err := os.ReadFile(s.Path(ref))
	if err != nil {
		return sess, err
	}
	if err := json.Unmarshal(b, &sess); err != nil {
		return sess, fmt.Errorf("session cache %s is corrupt: %w", s.Path(ref), err)
	}
	return sess, nil
}

func isReadOnly(err error) bool {
	return errors.Is(err, syscall.EROFS) || errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EPERM)
}

func (s Store) Save(ref string, sess Session) error {
	b, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(s.Path(ref), b); err != nil {
		if isReadOnly(err) {
			return fmt.Errorf("%w: %v", ErrReadOnly, err)
		}
		return err
	}
	return nil
}

func (s Store) Delete(ref string) error {
	err := os.Remove(s.Path(ref))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Writable reports whether the sessions directory accepts new files.
func (s Store) Writable() bool {
	dir := filepath.Join(s.Dir, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// Lock takes an exclusive flock so concurrent processes do not refresh the same session twice.
func (s Store) Lock(ctx context.Context, ref string) (func(), error) {
	dir := filepath.Join(s.Dir, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		if isReadOnly(err) {
			return nil, fmt.Errorf("%w: %v", ErrReadOnly, err)
		}
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ref+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		if isReadOnly(err) {
			return nil, fmt.Errorf("%w: %v", ErrReadOnly, err)
		}
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, fmt.Errorf("timed out waiting for session lock: %w", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// JWTExpiry reads the exp claim without verifying the signature (the server verifies it).
func JWTExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("token is not a JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, err
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(b, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, errors.New("token has no exp claim")
	}
	return time.Unix(claims.Exp, 0).UTC(), nil
}
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test -race ./internal/auth/...`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add -A
git commit -m "Adicionar cache de sessão com flock e leitura do exp do JWT"
```

---

### Task 10: login e refresh HTTP, com sondagens no Taiga local (US #244)

**Files:**
- Create: `internal/auth/api.go`, `internal/auth/api_test.go`, `internal/auth/probe_integration_test.go`, `docs/api-notes.md`

**Interfaces:**
- Consumes: `output.Error`
- Produces:
  - `type LoginResult struct{ AuthToken, Refresh string; UserID int64; Username string }`
  - `func Login(ctx context.Context, hc *http.Client, baseURL, username string, password []byte) (LoginResult, error)`: `POST /api/v1/auth` com `{"type":"normal","username","password"}`
  - `func RefreshToken(ctx context.Context, hc *http.Client, baseURL, refresh string) (authToken, newRefresh string, err error)`: `POST /api/v1/auth/refresh` com `{"refresh"}`

**Erros:**

| Situação | `code` | Exit | `source` |
|---|---|---|---|
| Login com 400 ou 401 | `auth_invalid_credentials` | 3 | `api` |
| Refresh com 400 ou 401 | `session_expired` | 3 | `session_cache` |
| Rede | `network_error` | 7 | `network` |

Nos três casos, `stage` é `login` ou `refresh`.

- [ ] **Step 1: escrever o teste que falha**

`internal/auth/api_test.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/output"
)

func TestLoginAndRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/api/v1/auth":
			if body["type"] != "normal" || body["password"] != "pw" {
				w.WriteHeader(401)
				w.Write([]byte(`{"_error_message":"bad"}`))
				return
			}
			w.Write([]byte(`{"auth_token":"a1","refresh":"r1","id":166,"username":"svc"}`))
		case "/api/v1/auth/refresh":
			if body["refresh"] != "r1" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"auth_token":"a2","refresh":"r2"}`))
		}
	}))
	defer srv.Close()
	res, err := Login(context.Background(), srv.Client(), srv.URL, "svc", []byte("pw"))
	if err != nil || res.AuthToken != "a1" || res.UserID != 166 {
		t.Fatalf("%+v %v", res, err)
	}
	_, err = Login(context.Background(), srv.Client(), srv.URL, "svc", []byte("no"))
	if e := output.AsError(err); e.Code != "auth_invalid_credentials" || e.Exit != 3 {
		t.Fatalf("%+v", e)
	}
	a, r, err := RefreshToken(context.Background(), srv.Client(), srv.URL, "r1")
	if err != nil || a != "a2" || r != "r2" {
		t.Fatalf("%s %s %v", a, r, err)
	}
	_, _, err = RefreshToken(context.Background(), srv.Client(), srv.URL, "old")
	if e := output.AsError(err); e.Code != "session_expired" {
		t.Fatalf("%+v", e)
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/auth/...`
Expected: FAIL, com `undefined: Login`.

- [ ] **Step 3: implementar**

`internal/auth/api.go`:

```go
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/BasisTI/taiga-cli/internal/output"
)

type LoginResult struct {
	AuthToken string `json:"auth_token"`
	Refresh   string `json:"refresh"`
	UserID    int64  `json:"id"`
	Username  string `json:"username"`
}

func post(ctx context.Context, hc *http.Client, url string, payload any) (int, []byte, error) {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, body, err
}

func netErr(stage string, err error) error {
	return &output.Error{Code: "network_error", Source: "network", Stage: stage, Cause: err.Error(), Recovery: "check connectivity to the Taiga URL (sandboxed agents need network access)", Exit: output.ExitNetwork}
}

// Login exchanges username/password for tokens. The password never appears in errors.
func Login(ctx context.Context, hc *http.Client, baseURL, username string, password []byte) (LoginResult, error) {
	var res LoginResult
	status, body, err := post(ctx, hc, baseURL+"/api/v1/auth", map[string]string{"type": "normal", "username": username, "password": string(password)})
	if err != nil {
		return res, netErr("login", err)
	}
	if status == 400 || status == 401 {
		return res, &output.Error{Code: "auth_invalid_credentials", Source: "api", Stage: "login", Cause: string(body), Recovery: "check the username and the secret source", Exit: output.ExitAuth}
	}
	if status != 200 {
		return res, &output.Error{Code: "server_error", Source: "api", Stage: "login", Cause: string(body), Exit: output.ExitNetwork}
	}
	if err := json.Unmarshal(body, &res); err != nil || res.AuthToken == "" {
		return res, &output.Error{Code: "server_error", Source: "api", Stage: "login", Cause: "unexpected login response", Exit: output.ExitNetwork}
	}
	return res, nil
}

// RefreshToken renews the session; Taiga may rotate the refresh token.
func RefreshToken(ctx context.Context, hc *http.Client, baseURL, refresh string) (string, string, error) {
	status, body, err := post(ctx, hc, baseURL+"/api/v1/auth/refresh", map[string]string{"refresh": refresh})
	if err != nil {
		return "", "", netErr("refresh", err)
	}
	if status == 400 || status == 401 {
		return "", "", &output.Error{Code: "session_expired", Source: "session_cache", Stage: "refresh", Cause: string(body), Recovery: "run `taiga auth login` (or `taiga auth refresh` outside the sandbox)", Exit: output.ExitAuth}
	}
	var out struct {
		AuthToken string `json:"auth_token"`
		Refresh   string `json:"refresh"`
	}
	if status != 200 || json.Unmarshal(body, &out) != nil || out.AuthToken == "" {
		return "", "", &output.Error{Code: "server_error", Source: "api", Stage: "refresh", Cause: string(body), Exit: output.ExitNetwork}
	}
	if out.Refresh == "" {
		out.Refresh = refresh
	}
	return out.AuthToken, out.Refresh, nil
}
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/auth/...`
Expected: PASS.

- [ ] **Step 5: sondagens no Taiga local**

`internal/auth/probe_integration_test.go`:

```go
//go:build integration

package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

// These probes pin down Taiga 6.7 behaviour that the resolver depends on (spec §10).
func TestProbeTokenLifetimes(t *testing.T) {
	res, err := Login(context.Background(), http.DefaultClient, testtaiga.URL(), testtaiga.ServiceUser, []byte(testtaiga.ServicePassword))
	if err != nil {
		t.Fatal(err)
	}
	exp, err := JWTExpiry(res.AuthToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FINDING auth_token lifetime ≈ %s", time.Until(exp).Round(time.Minute))
	if rexp, err := JWTExpiry(res.Refresh); err == nil {
		t.Logf("FINDING refresh lifetime ≈ %s", time.Until(rexp).Round(time.Hour))
	}
}

// Conservative expectation: after a refresh, the previous refresh token is rejected.
// If this test fails, set refreshInvalidatesPrevious = false in resolver.go and flip the assertion.
func TestProbeRefreshRotationInvalidatesPrevious(t *testing.T) {
	ctx := context.Background()
	res, err := Login(ctx, http.DefaultClient, testtaiga.URL(), testtaiga.ServiceUser, []byte(testtaiga.ServicePassword))
	if err != nil {
		t.Fatal(err)
	}
	_, r2, err := RefreshToken(ctx, http.DefaultClient, testtaiga.URL(), res.Refresh)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FINDING refresh rotated = %v", r2 != res.Refresh)
	_, _, err = RefreshToken(ctx, http.DefaultClient, testtaiga.URL(), res.Refresh)
	if err == nil {
		t.Fatal("previous refresh token still works: set refreshInvalidatesPrevious = false")
	}
}
```

Run: `go test -tags integration -run Probe -v ./internal/auth/...`
Expected: as linhas `FINDING` no log. `TestProbeRefreshRotationInvalidatesPrevious` passa se o Taiga invalida o refresh anterior.

- [ ] **Step 6: registrar os achados**

Crie `docs/api-notes.md` em PT-BR, com os valores observados no Step 5:

```markdown
# Notas de comportamento da API do Taiga 6.7

Observado no Taiga local (`compose.test.yml`, `taigaio/taiga-back:6.7.3`) em <data>.

| Ponto | Resultado | Teste |
|---|---|---|
| Vida do `auth_token` | <valor da linha FINDING> | `TestProbeTokenLifetimes` |
| Vida do `refresh` | <valor da linha FINDING> | `TestProbeTokenLifetimes` |
| Refresh rotaciona | <sim/não> | `TestProbeRefreshRotationInvalidatesPrevious` |
| Refresh anterior invalidado | <sim/não> | `TestProbeRefreshRotationInvalidatesPrevious` |
| `x-disable-pagination` respeitado | sim (lista sem `x-pagination-next`) | `TestIntegrationAPIProjectsAndUsersMe` |
| Application tokens para conta de serviço | não validado na fase 1: exige cadastrar uma Application pelo admin do Django; fica para quando houver demanda | — |
| `userstories/by_ref` | fase 2 (US #246) | — |
| Escrita de swimlane, upload de anexo, comentários no histórico | fases 2 e 3 | — |
```

Substitua cada `<…>` pelos valores reais do log antes do commit. O arquivo não pode ser commitado com `<…>`.

- [ ] **Step 7: commit**

```bash
git add -A
git commit -m "Adicionar login e refresh HTTP e registrar comportamento dos tokens"
```

---

### Task 11: fontes de segredo: env, arquivo, `secret_command` e keyring (US #244)

**Files:**
- Create: `internal/auth/secrets.go`, `internal/auth/secrets_test.go`, `internal/auth/keyring_linux.go`, `internal/auth/keyring_other.go`, `internal/auth/keyring_linux_test.go`

**Interfaces:**
- Consumes: `output.Error`
- Produces:
  - `type SecretSource interface{ Name() string; Password(ctx context.Context) ([]byte, error) }`
  - `type EnvPassword struct{ Env func(string) string }`: `TAIGA_PASSWORD`, depois `TAIGA_PASSWORD_FILE`. `Name()` = `"env"`
  - `func (e EnvPassword) Configured() bool`
  - `type CommandSecret struct{ Args []string }`: `Name()` = `"secret_command"`
  - `type FileSecret struct{ Path string }`: `Name()` = `"file"`, com `func (f FileSecret) Put(pw []byte) error`
  - `type Keyring struct{ Ref string }`: `Name()` = `"keyring"`, com `Password`, `Put(ctx, []byte) error`, `Delete(ctx) error` e `Available(ctx) error`
  - `type FirstOf []SecretSource`, que tenta cada fonte em ordem e devolve o primeiro sucesso ou o último erro

**Erros de `CommandSecret`** (todos com exit 3 e `source` = `secret_command`):

| Situação | `code` | Observação |
|---|---|---|
| Timeout de 10 s | `secret_command_timeout` | — |
| Executável inexistente | `secret_command_not_found` | — |
| Exit diferente de 0 | `secret_command_failed` | `cause` = `exit status N: <stderr sanitizado>` |
| Saída vazia | `secret_command_empty` | — |

**Sanitização do stderr:** até 4 KiB, remove caracteres de controle (exceto `\n`), mantém as 3 últimas linhas não vazias e trunca em 300 caracteres. Se o stderr citar `pinentry`, `inappropriate ioctl`, `no tty` ou `gpg:` (sem distinguir maiúsculas), o `recovery` passa a ser:

> run `export GPG_TTY=$(tty); <comando> >/dev/null` in an interactive terminal outside the sandbox to unlock gpg-agent, then retry

- [ ] **Step 1: escrever o teste que falha**

`internal/auth/secrets_test.go`:

```go
package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
)

func TestEnvPassword(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "pw")
	_ = os.WriteFile(pf, []byte("from-file\n"), 0o600)
	e := EnvPassword{Env: func(k string) string { return map[string]string{"TAIGA_PASSWORD_FILE": pf}[k] }}
	pw, err := e.Password(context.Background())
	if err != nil || string(pw) != "from-file" || !e.Configured() {
		t.Fatalf("%q %v", pw, err)
	}
	e = EnvPassword{Env: func(k string) string { return map[string]string{"TAIGA_PASSWORD": "direct", "TAIGA_PASSWORD_FILE": pf}[k] }}
	if pw, _ := e.Password(context.Background()); string(pw) != "direct" {
		t.Fatalf("TAIGA_PASSWORD must win: %q", pw)
	}
}

func TestCommandSecretSuccessAndErrors(t *testing.T) {
	ctx := context.Background()
	pw, err := CommandSecret{Args: []string{"/bin/sh", "-c", "printf 'segredo\\n'"}}.Password(ctx)
	if err != nil || string(pw) != "segredo" {
		t.Fatalf("%q %v", pw, err)
	}
	_, err = CommandSecret{Args: []string{"/bin/sh", "-c", "echo 'gpg: decryption failed: Inappropriate ioctl for device' >&2; exit 2"}}.Password(ctx)
	e := output.AsError(err)
	if e.Code != "secret_command_failed" || !strings.Contains(e.Cause, "exit status 2") || !strings.Contains(e.Cause, "Inappropriate ioctl") || !strings.Contains(e.Recovery, "GPG_TTY") {
		t.Fatalf("%+v", e)
	}
	_, err = CommandSecret{Args: []string{"/nonexistent/helper"}}.Password(ctx)
	if output.AsError(err).Code != "secret_command_not_found" {
		t.Fatalf("%v", err)
	}
	_, err = CommandSecret{Args: []string{"/bin/true"}}.Password(ctx)
	if output.AsError(err).Code != "secret_command_empty" {
		t.Fatalf("%v", err)
	}
}

func TestCommandSecretTimeout(t *testing.T) {
	old := commandTimeout
	commandTimeout = 200 * time.Millisecond
	defer func() { commandTimeout = old }()
	start := time.Now()
	_, err := CommandSecret{Args: []string{"/bin/sh", "-c", "sleep 5"}}.Password(context.Background())
	if output.AsError(err).Code != "secret_command_timeout" || time.Since(start) > 3*time.Second {
		t.Fatalf("%v after %s", err, time.Since(start))
	}
}

func TestFileSecretRequiresPrivateMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secrets", "ref")
	f := FileSecret{Path: p}
	if err := f.Put([]byte("pw")); err != nil {
		t.Fatal(err)
	}
	if pw, err := f.Password(context.Background()); err != nil || string(pw) != "pw" {
		t.Fatalf("%q %v", pw, err)
	}
	_ = os.Chmod(p, 0o644)
	if _, err := f.Password(context.Background()); output.AsError(err).Code != "file_secret_insecure_permissions" {
		t.Fatalf("%v", err)
	}
}

type failing struct{ name string }

func (f failing) Name() string { return f.name }
func (f failing) Password(context.Context) ([]byte, error) {
	return nil, &output.Error{Code: "x_" + f.name, Exit: 3}
}

func TestFirstOf(t *testing.T) {
	pw, err := FirstOf{failing{"a"}, CommandSecret{Args: []string{"/bin/echo", "ok"}}}.Password(context.Background())
	if err != nil || string(pw) != "ok" {
		t.Fatalf("%q %v", pw, err)
	}
	_, err = FirstOf{failing{"a"}, failing{"b"}}.Password(context.Background())
	if output.AsError(err).Code != "x_b" {
		t.Fatalf("%v", err)
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/auth/...`
Expected: FAIL, com `undefined: EnvPassword`.

- [ ] **Step 3: implementar `secrets.go`**

`internal/auth/secrets.go`:

```go
package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/BasisTI/taiga-cli/internal/config"
	"github.com/BasisTI/taiga-cli/internal/output"
)

type SecretSource interface {
	Name() string
	Password(ctx context.Context) ([]byte, error)
}

func authErr(code, source, cause, recovery string) *output.Error {
	return &output.Error{Code: code, Source: source, Cause: cause, Recovery: recovery, Exit: output.ExitAuth}
}

type EnvPassword struct{ Env func(string) string }

func (EnvPassword) Name() string { return "env" }
func (e EnvPassword) Configured() bool {
	return e.Env("TAIGA_PASSWORD") != "" || e.Env("TAIGA_PASSWORD_FILE") != ""
}
func (e EnvPassword) Password(context.Context) ([]byte, error) {
	if v := e.Env("TAIGA_PASSWORD"); v != "" {
		return []byte(v), nil
	}
	path := e.Env("TAIGA_PASSWORD_FILE")
	if path == "" {
		return nil, authErr("auth_no_source", "env", "TAIGA_PASSWORD and TAIGA_PASSWORD_FILE are unset", "")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, authErr("password_file_unreadable", "env", fmt.Sprintf("TAIGA_PASSWORD_FILE %s: %v", path, err), "check the path and permissions")
	}
	b = bytes.TrimRight(b, "\r\n")
	if len(b) == 0 {
		return nil, authErr("password_file_empty", "env", "TAIGA_PASSWORD_FILE is empty", "")
	}
	return b, nil
}

var commandTimeout = 10 * time.Second

type CommandSecret struct{ Args []string }

func (CommandSecret) Name() string { return "secret_command" }

type capped struct {
	bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.Len(); room > 0 {
		if len(p) > room {
			c.Buffer.Write(p[:room])
		} else {
			c.Buffer.Write(p)
		}
	}
	return len(p), nil
}

var ctrl = regexp.MustCompile(`[\x00-\x09\x0b-\x1f\x7f]`)
var gpgHint = regexp.MustCompile(`(?i)pinentry|inappropriate ioctl|no tty|gpg:`)

func sanitizeStderr(b []byte) string {
	lines := strings.Split(ctrl.ReplaceAllString(string(b), ""), "\n")
	var kept []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, strings.TrimSpace(l))
		}
	}
	if len(kept) > 3 {
		kept = kept[len(kept)-3:]
	}
	s := strings.Join(kept, " | ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func (c CommandSecret) Password(ctx context.Context) ([]byte, error) {
	if len(c.Args) == 0 {
		return nil, authErr("secret_command_not_found", "secret_command", "secret_command is empty", "set secret_command in the config")
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Args[0], c.Args[1:]...)
	stdout := &capped{max: 64 << 10}
	stderr := &capped{max: 4 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	cmdline := strings.Join(c.Args, " ")
	recovery := "run the secret command by hand to see what it needs"
	if gpgHint.Match(stderr.Bytes()) {
		recovery = fmt.Sprintf("run `export GPG_TTY=$(tty); %s >/dev/null` in an interactive terminal outside the sandbox to unlock gpg-agent, then retry", cmdline)
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, authErr("secret_command_timeout", "secret_command", fmt.Sprintf("no answer after %s: %s", commandTimeout, sanitizeStderr(stderr.Bytes())), recovery)
	case errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist):
		return nil, authErr("secret_command_not_found", "secret_command", err.Error(), "fix secret_command in the config (absolute path, no shell)")
	case err != nil:
		var ee *exec.ExitError
		code := -1
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return nil, authErr("secret_command_failed", "secret_command", fmt.Sprintf("exit status %d: %s", code, sanitizeStderr(stderr.Bytes())), recovery)
	}
	out := bytes.TrimSuffix(bytes.TrimSuffix(stdout.Bytes(), []byte("\n")), []byte("\r"))
	if len(out) == 0 {
		return nil, authErr("secret_command_empty", "secret_command", "the secret command printed nothing", recovery)
	}
	return out, nil
}

type FileSecret struct{ Path string }

func (FileSecret) Name() string { return "file" }
func (f FileSecret) Put(pw []byte) error { return config.WriteFileAtomic(f.Path, pw) }
func (f FileSecret) Password(context.Context) ([]byte, error) {
	info, err := os.Stat(f.Path)
	if err != nil {
		return nil, authErr("file_secret_missing", "file", err.Error(), "run `taiga auth login --insecure-storage` again")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, authErr("file_secret_insecure_permissions", "file", fmt.Sprintf("%s has mode %v", f.Path, info.Mode().Perm()), "chmod 600 the file")
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, authErr("file_secret_missing", "file", err.Error(), "")
	}
	return b, nil
}

// FirstOf tries each source in order.
type FirstOf []SecretSource

func (f FirstOf) Name() string {
	names := make([]string, len(f))
	for i, s := range f {
		names[i] = s.Name()
	}
	return strings.Join(names, ",")
}
func (f FirstOf) Password(ctx context.Context) ([]byte, error) {
	var last error = authErr("auth_no_source", "config", "no secret source configured", "run `taiga auth login`")
	for _, s := range f {
		pw, err := s.Password(ctx)
		if err == nil {
			return pw, nil
		}
		last = err
	}
	return nil, last
}
```

- [ ] **Step 4: portar o keyring da `sgo-cli`**

Copie de `~/Projetos/Basis/convey/sgo-cli/internal/credenciais/` (Apache 2.0, Basis):

```bash
SRC=~/Projetos/Basis/convey/sgo-cli/internal/credenciais
cp $SRC/keyring_linux.go internal/auth/keyring_linux.go
cp $SRC/keyring_other.go internal/auth/keyring_other.go
cp $SRC/keyring_linux_test.go internal/auth/keyring_linux_test.go
go get github.com/godbus/dbus/v5@latest
```

Aplique nos três arquivos:

1. `package credenciais` → `package auth`. Remova o import `"sgo-cli/internal/saida"` e importe `"github.com/BasisTI/taiga-cli/internal/output"`.
2. Acrescente no topo, abaixo do build tag: `// Adapted from sgo-cli (Basis Tecnologia da Informação, Apache-2.0).`
3. O tipo passa a ser `type Keyring struct{ Ref string }`. Os métodos perdem o parâmetro `ref` e usam `k.Ref`:
   - `Password(ctx) ([]byte, error)`, antigo `Get`;
   - `Put(ctx, value []byte) error`;
   - `Delete(ctx) error`;
   - `Name() string`, que devolve `"keyring"`.
4. Toda chamada `saida.Fail(code, msg, 3)` vira `authErr(code, "keyring", msg, keyringRecovery)`, com a constante:

   `const keyringRecovery = "see README section \"Headless Linux keyring\"; or use --secret-command"`

   Em `storeError`, troque `saida.Fail(...)` + `e.Recovery = recovery` por `authErr(code, "keyring", fmt.Sprintf("%s (stage: %s)", message, stage), recovery)`.
5. As mensagens passam para o inglês:

   | Original | Novo |
   |---|---|
   | "Não foi possível acessar o serviço de cofre." | "cannot reach the Secret Service" |
   | "O serviço de cofre não respondeu no prazo." | "the Secret Service did not answer in time" |
   | "A sessão não oferece a interface Secret Service (org.freedesktop.secrets)." | "no Secret Service (org.freedesktop.secrets) on this session bus" |
   | "O serviço de cofre recusou o acesso." | "the Secret Service denied access" |
   | "A coleção do Secret Service está bloqueada; desbloqueie o cofre da sessão." | "the Secret Service collection is locked; unlock it (gnome-keyring-daemon --unlock)" |
   | "O cofre exige autorização interativa; nenhuma janela foi aberta automaticamente." | "the keyring requires an interactive prompt; taiga never opens one" |
   | "Referência de credencial ausente." | "missing credential reference" |
   | "Mais de uma credencial encontrada para a referência." | "more than one credential for this reference" |
   | "Credencial não encontrada no cofre." / "O cofre retornou credencial vazia." | "no credential stored in the keyring" |
   | "Secret Service está disponível, mas não há coleção padrão configurada no cofre." | "the Secret Service has no default collection" |

   O `recovery` de `keyring_service_unavailable` passa a ser `keyringRecovery`.
6. `attributes := map[string]string{"service": "sgo-cli", "account": ref}` → `{"service": "taiga-cli", "account": k.Ref}`. O label `"SGO CLI"` → `"taiga-cli"`.
7. Acrescente `Available(ctx) error`: conecta ao session bus, chama `org.freedesktop.DBus.NameHasOwner` com `org.freedesktop.secrets` e devolve `keyring_service_unavailable` se o nome não tiver dono. Use o mesmo timeout de 5 s.
8. `keyring_other.go`: mantenha o build tag `!linux`. Os métodos devolvem `authErr("keyring_unsupported", "keyring", "Secret Service is only supported on Linux", "use --secret-command")`.
9. `keyring_linux_test.go`: faça as mesmas substituições de package e import. Adapte as chamadas para `Keyring{Ref: "r"}.Put(ctx, v)`, `.Password(ctx)` e `.Delete(ctx)`. Troque as asserções de `saida.Error` por `output.AsError(err).Code`. Mantenha o `dbus-daemon` isolado. Se `dbus-daemon` não existir no PATH, `t.Skip`.

- [ ] **Step 5: rodar e ver passar**

Run: `go vet ./internal/auth/... && go test -race ./internal/auth/...`
Expected: PASS. Os testes de keyring passam, ou são pulados se não houver `dbus-daemon`. No CI (ubuntu-latest), instale-o antes dos testes, acrescentando ao job `unit` do `ci.yml` o passo `- run: sudo apt-get update && sudo apt-get install -y dbus`.

- [ ] **Step 6: commit**

```bash
git add -A
git commit -m "Adicionar fontes de segredo: env, arquivo, secret_command e keyring"
```

---

### Task 12: resolver de token e ligação ao CLI (US #244)

**Files:**
- Create: `internal/auth/resolver.go`, `internal/auth/resolver_test.go`, `internal/cli/tokens.go`
- Modify: `internal/cli/root.go` (em `Run`, `a.TokenSource` passa a apontar para `a.resolverTokenSource`)

**Interfaces:**
- Consumes:
  - `Store`, `Session`, `SessionRef`, `JWTExpiry`, `Login`, `RefreshToken` e `SecretSource` (tarefas 9 a 11)
  - `taiga.Token`
- Produces:
  - `type Resolver struct{ URL, Username string; Env func(string) string; Store Store; Secret SecretSource; HTTP *http.Client; Now func() time.Time; Warn func(code, message string) }`
  - `func (r *Resolver) Token(ctx context.Context) (taiga.Token, error)`, que implementa `taiga.TokenSource`
  - `func (r *Resolver) ForceRefresh(ctx context.Context) (Session, error)`
  - `func (r *Resolver) LoginWith(ctx context.Context, password []byte) (Session, error)`
  - `const refreshInvalidatesPrevious` (valor definido pela tarefa 10)
  - `func (a *App) resolverTokenSource(ctx context.Context, rc *RunContext) (taiga.TokenSource, error)`
  - `func (a *App) resolver(rc *RunContext) (*auth.Resolver, error)`

**Algoritmo de `Token`** (spec, seção 8), com margem de 60 s antes do `exp`:
1. `TAIGA_TOKEN` definido: devolve `{Type: TAIGA_TOKEN_TYPE ou "Bearer", Value}`.
2. `Username` vazio: `auth_no_source`, com a recuperação "run `taiga auth login` or set TAIGA_TOKEN".
3. Sessão carregada e válida: devolve o token da sessão.
4. Tenta `Store.Lock`:
   - **deu certo:** relê a sessão; se já estiver válida (outro processo renovou), devolve. Se houver refresh, chama `RefreshToken`, grava e devolve. Se o refresh falhar com `session_expired`, segue para o passo 5.
   - **`ErrReadOnly`** (sandbox): se `refreshInvalidatesPrevious` for verdadeiro, **não** renova. Senão, renova só em memória e chama `Warn("session_cache_readonly", ...)`.
5. Login com `Secret`:
   - `Secret` nil ou falhando: se o store é somente leitura e havia sessão, devolve `session_expired`, com a recuperação "run `taiga auth refresh` outside the sandbox"; senão, devolve o erro da fonte.
   - login OK: grava a sessão. Se a gravação der `ErrReadOnly`, chama `Warn("session_cache_readonly", ...)` e devolve o token em memória.

- [ ] **Step 1: escrever o teste que falha**

`internal/auth/resolver_test.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
)

type fakeAuth struct {
	srv       *httptest.Server
	logins    int32
	refreshes int32
	valid     sync.Map // refresh tokens currently valid
	exp       time.Time
}

func newFakeAuth(t *testing.T) *fakeAuth {
	f := &fakeAuth{exp: time.Now().Add(24 * time.Hour)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/api/v1/auth":
			n := atomic.AddInt32(&f.logins, 1)
			if body["password"] != "pw" {
				w.WriteHeader(401)
				return
			}
			rt := "r-login-" + itoa64(int64(n))
			f.valid.Store(rt, true)
			_ = json.NewEncoder(w).Encode(map[string]any{"auth_token": fakeJWT(f.exp), "refresh": rt, "id": 166, "username": "svc"})
		case "/api/v1/auth/refresh":
			if _, ok := f.valid.LoadAndDelete(body["refresh"]); !ok {
				w.WriteHeader(401)
				return
			}
			n := atomic.AddInt32(&f.refreshes, 1)
			time.Sleep(20 * time.Millisecond)
			rt := "r-refresh-" + itoa64(int64(n))
			f.valid.Store(rt, true)
			_ = json.NewEncoder(w).Encode(map[string]any{"auth_token": fakeJWT(f.exp), "refresh": rt})
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type staticSecret string

func (staticSecret) Name() string                                  { return "test" }
func (s staticSecret) Password(context.Context) ([]byte, error)  { return []byte(s), nil }

func newResolver(f *fakeAuth, dir string, secret SecretSource) *Resolver {
	return &Resolver{URL: f.srv.URL, Username: "svc", Env: func(string) string { return "" }, Store: Store{Dir: dir}, Secret: secret, HTTP: f.srv.Client(), Now: time.Now, Warn: func(string, string) {}}
}

func TestTokenEnvWins(t *testing.T) {
	r := &Resolver{Env: func(k string) string { return map[string]string{"TAIGA_TOKEN": "x", "TAIGA_TOKEN_TYPE": "Application"}[k] }}
	tok, err := r.Token(context.Background())
	if err != nil || tok.Value != "x" || tok.Type != "Application" {
		t.Fatalf("%+v %v", tok, err)
	}
}

func TestTokenLoginsOnceThenUsesCache(t *testing.T) {
	f := newFakeAuth(t)
	r := newResolver(f, t.TempDir(), staticSecret("pw"))
	for i := 0; i < 3; i++ {
		if _, err := r.Token(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if f.logins != 1 {
		t.Fatalf("logins = %d", f.logins)
	}
}

func TestExpiredSessionRefreshesOnceAcrossConcurrentCallers(t *testing.T) {
	f := newFakeAuth(t)
	dir := t.TempDir()
	st := Store{Dir: dir}
	ref := SessionRef(f.srv.URL, "svc")
	f.valid.Store("r0", true)
	_ = st.Save(ref, Session{URL: f.srv.URL, Username: "svc", AuthToken: fakeJWT(time.Now().Add(-time.Hour)), Refresh: "r0", Expiry: time.Now().Add(-time.Hour)})
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := newResolver(f, dir, nil).Token(context.Background())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.refreshes != 1 || f.logins != 0 {
		t.Fatalf("refreshes=%d logins=%d", f.refreshes, f.logins)
	}
}

func readOnlyStateDir(t *testing.T, sess *Session, ref string) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	dir := t.TempDir()
	if sess != nil {
		_ = Store{Dir: dir}.Save(ref, *sess)
	}
	_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o700)
	_ = os.Chmod(filepath.Join(dir, "sessions"), 0o500)
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "sessions"), 0o700) })
	return dir
}

func TestReadOnlyExpiredSessionWithoutSecretIsSessionExpired(t *testing.T) {
	f := newFakeAuth(t)
	ref := SessionRef(f.srv.URL, "svc")
	f.valid.Store("r0", true)
	dir := readOnlyStateDir(t, &Session{URL: f.srv.URL, Username: "svc", AuthToken: "old", Refresh: "r0", Expiry: time.Now().Add(-time.Hour)}, ref)
	_, err := newResolver(f, dir, nil).Token(context.Background())
	e := output.AsError(err)
	if e.Code != "session_expired" || e.Exit != 3 {
		t.Fatalf("%+v", e)
	}
	if refreshInvalidatesPrevious && f.refreshes != 0 {
		t.Fatal("must not burn the persisted refresh token in read-only mode")
	}
}

func TestReadOnlyWithEnvPasswordLogsInInMemoryAndWarns(t *testing.T) {
	f := newFakeAuth(t)
	dir := readOnlyStateDir(t, nil, "")
	var warned string
	r := newResolver(f, dir, staticSecret("pw"))
	r.Warn = func(code, _ string) { warned = code }
	if _, err := r.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if warned != "session_cache_readonly" || f.logins != 1 {
		t.Fatalf("warned=%q logins=%d", warned, f.logins)
	}
}

func TestNoUsernameIsNoSource(t *testing.T) {
	r := &Resolver{Env: func(string) string { return "" }}
	if output.AsError(func() error { _, err := r.Token(context.Background()); return err }()).Code != "auth_no_source" {
		t.Fatal("want auth_no_source")
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/auth/...`
Expected: FAIL, com `undefined: Resolver`.

- [ ] **Step 3: implementar**

`internal/auth/resolver.go`:

```go
package auth

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// refreshInvalidatesPrevious reflects TestProbeRefreshRotationInvalidatesPrevious (docs/api-notes.md).
// When true, a read-only session store must not refresh: the rotated token could not be persisted.
const refreshInvalidatesPrevious = true

const expiryMargin = 60 * time.Second

type Resolver struct {
	URL, Username string
	Env           func(string) string
	Store         Store
	Secret        SecretSource
	HTTP          *http.Client
	Now           func() time.Time
	Warn          func(code, message string)
}

func (r *Resolver) ref() string { return SessionRef(r.URL, r.Username) }

func (r *Resolver) valid(s Session) bool {
	return s.AuthToken != "" && r.Now().Add(expiryMargin).Before(s.Expiry)
}

func bearer(s Session) taiga.Token { return taiga.Token{Type: "Bearer", Value: s.AuthToken} }

func (r *Resolver) Token(ctx context.Context) (taiga.Token, error) {
	if v := r.Env("TAIGA_TOKEN"); v != "" {
		typ := r.Env("TAIGA_TOKEN_TYPE")
		if typ == "" {
			typ = "Bearer"
		}
		return taiga.Token{Type: typ, Value: v}, nil
	}
	if r.Username == "" {
		return taiga.Token{}, authErr("auth_no_source", "config", "no username configured for "+r.URL, "run `taiga auth login` or set TAIGA_TOKEN")
	}
	sess, loadErr := r.Store.Load(r.ref())
	if loadErr == nil && r.valid(sess) {
		return bearer(sess), nil
	}
	unlock, lockErr := r.Store.Lock(ctx, r.ref())
	readOnly := errors.Is(lockErr, ErrReadOnly)
	if lockErr != nil && !readOnly {
		return taiga.Token{}, lockErr
	}
	if unlock != nil {
		defer unlock()
		if s2, err := r.Store.Load(r.ref()); err == nil {
			sess, loadErr = s2, nil
			if r.valid(sess) {
				return bearer(sess), nil
			}
		}
	}
	hadSession := loadErr == nil
	if hadSession && sess.Refresh != "" && (!readOnly || !refreshInvalidatesPrevious) {
		if s, err := r.refresh(ctx, sess); err == nil {
			return bearer(s), nil
		} else if output.AsError(err).Code != "session_expired" {
			return taiga.Token{}, err
		}
	}
	if r.Secret == nil {
		if hadSession && readOnly {
			return taiga.Token{}, &output.Error{Code: "session_expired", Source: "session_cache", Stage: r.Store.Path(r.ref()), Cause: "session expired and the session cache is read-only", Recovery: "run `taiga auth refresh` outside the sandbox", Exit: output.ExitAuth}
		}
		if hadSession {
			return taiga.Token{}, authErr("session_expired", "session_cache", "session expired and no secret source is configured", "run `taiga auth login`")
		}
		return taiga.Token{}, authErr("auth_no_source", "config", "no session and no secret source for "+r.Username, "run `taiga auth login`")
	}
	pw, err := r.Secret.Password(ctx)
	if err != nil {
		if hadSession && readOnly {
			e := output.AsError(err)
			e.Recovery = "run `taiga auth refresh` outside the sandbox (" + e.Recovery + ")"
			return taiga.Token{}, e
		}
		return taiga.Token{}, err
	}
	s, err := r.LoginWith(ctx, pw)
	if err != nil {
		return taiga.Token{}, err
	}
	return bearer(s), nil
}

func (r *Resolver) persist(s Session) {
	if err := r.Store.Save(r.ref(), s); err != nil && r.Warn != nil {
		if errors.Is(err, ErrReadOnly) {
			r.Warn("session_cache_readonly", "session cache is read-only; token kept in memory for this run")
		} else {
			r.Warn("session_cache_write_failed", err.Error())
		}
	}
}

func (r *Resolver) refresh(ctx context.Context, sess Session) (Session, error) {
	tok, rt, err := RefreshToken(ctx, r.HTTP, r.URL, sess.Refresh)
	if err != nil {
		return Session{}, err
	}
	sess.AuthToken, sess.Refresh = tok, rt
	if exp, err := JWTExpiry(tok); err == nil {
		sess.Expiry = exp
	} else {
		sess.Expiry = r.Now().Add(time.Hour)
	}
	r.persist(sess)
	return sess, nil
}

// LoginWith authenticates with a password and stores the new session.
func (r *Resolver) LoginWith(ctx context.Context, password []byte) (Session, error) {
	res, err := Login(ctx, r.HTTP, r.URL, r.Username, password)
	if err != nil {
		return Session{}, err
	}
	s := Session{URL: r.URL, Username: r.Username, AuthToken: res.AuthToken, Refresh: res.Refresh, UserID: res.UserID}
	if exp, err := JWTExpiry(res.AuthToken); err == nil {
		s.Expiry = exp
	} else {
		s.Expiry = r.Now().Add(time.Hour)
	}
	r.persist(s)
	return s, nil
}

// ForceRefresh renews the stored session regardless of its expiry (taiga auth refresh).
func (r *Resolver) ForceRefresh(ctx context.Context) (Session, error) {
	unlock, err := r.Store.Lock(ctx, r.ref())
	if err != nil {
		return Session{}, err
	}
	defer unlock()
	sess, err := r.Store.Load(r.ref())
	if err == nil && sess.Refresh != "" {
		if s, err := r.refresh(ctx, sess); err == nil {
			return s, nil
		}
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Session{}, err
	}
	if r.Secret == nil {
		return Session{}, authErr("session_expired", "session_cache", "no valid session and no secret source", "run `taiga auth login`")
	}
	pw, err := r.Secret.Password(ctx)
	if err != nil {
		return Session{}, err
	}
	return r.LoginWith(ctx, pw)
}
```

`internal/cli/tokens.go`:

```go
package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/BasisTI/taiga-cli/internal/auth"
	"github.com/BasisTI/taiga-cli/internal/config"
	"github.com/BasisTI/taiga-cli/internal/taiga"
)

// configuredSecret builds the secret source recorded for the host, with env passwords first.
func configuredSecret(env func(string) string, host config.Host, paths config.Paths, ref string) auth.SecretSource {
	var chain auth.FirstOf
	if e := (auth.EnvPassword{Env: env}); e.Configured() {
		chain = append(chain, e)
	}
	switch host.SecretSource {
	case "secret_command":
		chain = append(chain, auth.CommandSecret{Args: host.SecretCommand})
	case "keyring":
		chain = append(chain, auth.Keyring{Ref: ref})
	case "file":
		chain = append(chain, auth.FileSecret{Path: filepath.Join(paths.SecretsDir, ref)})
	}
	if len(chain) == 0 {
		return nil
	}
	return chain
}

func (a *App) resolver(rc *RunContext) (*auth.Resolver, error) {
	host, _ := rc.File.Host(rc.Ctx.URL.Value)
	username := a.Env("TAIGA_USERNAME")
	if username == "" {
		username = host.Username
	}
	ref := auth.SessionRef(rc.Ctx.URL.Value, username)
	return &auth.Resolver{
		URL:      rc.Ctx.URL.Value,
		Username: username,
		Env:      a.Env,
		Store:    auth.Store{Dir: rc.Paths.StateDir},
		Secret:   configuredSecret(a.Env, host, rc.Paths, ref),
		HTTP:     a.httpClient(),
		Now:      time.Now,
		Warn: func(code, msg string) {
			fmt.Fprintf(a.Err, "warning [%s]: %s\n", code, msg)
		},
	}, nil
}

func (a *App) resolverTokenSource(_ context.Context, rc *RunContext) (taiga.TokenSource, error) {
	return a.resolver(rc)
}
```

Em `Run` (`root.go`), troque `a.TokenSource = envTokenSource(a)` por `a.TokenSource = a.resolverTokenSource` e apague `envTokenSource` do `context.go`. O resolver já trata `TAIGA_TOKEN`, então os testes da tarefa 8 continuam valendo.

- [ ] **Step 4: rodar e ver passar**

Run: `go test -race ./...`
Expected: PASS, inclusive os testes do `taiga api` da tarefa 8.

- [ ] **Step 5: commit**

```bash
git add -A
git commit -m "Adicionar resolver de token com refresh sob flock e modo somente leitura"
```

---

### Task 13: comandos `taiga auth login|refresh|status|logout` e diagnóstico (US #244)

**Files:**
- Create: `internal/cli/auth.go`, `internal/cli/auth_test.go`, `internal/auth/diagnose.go`, `internal/auth/diagnose_test.go`, `internal/cli/auth_integration_test.go`

**Interfaces:**
- Consumes: `Resolver`, `Keyring`, `FileSecret`, `CommandSecret`, `config.Save/Upsert`, `output.WriteJSON/WriteFields`
- Produces:
  - `type Check struct{ Name, Status, Detail string }`, com as tags json `name`, `status`, `detail`. `Status` vale `ok`, `skipped` ou `failed`
  - `type DiagnoseInput struct{ Env func(string) string; Store Store; Ref string; Secret SecretSource; KeyringProbe func(context.Context) error; StdinTTY bool; Now func() time.Time }`
  - `func Diagnose(ctx context.Context, in DiagnoseInput) []Check`

**Comandos:**

- `taiga auth login --url U --username N [--password-stdin] [--secret-command "ARGV"] [--insecure-storage] [--project P]`:
  1. obtém a senha: de `--secret-command` (roda a fonte); senão de `--password-stdin` (lê `a.In` até EOF e remove o `\n` final); senão, com TTY no stdin, pede sem eco (`term.ReadPassword`); sem TTY e sem flag, erro `usage` com a recuperação "use --password-stdin or --secret-command";
  2. `resolver.LoginWith(pw)`;
  3. guarda o segredo conforme a fonte:
     - `--secret-command`: `SecretSource = "secret_command"` e `SecretCommand = strings.Fields(arg)`;
     - `--insecure-storage`: `FileSecret{SecretsDir/ref}.Put(pw)` e `SecretSource = "file"`;
     - nenhum dos dois: `Keyring{ref}.Put(ctx, pw)` e `SecretSource = "keyring"`. Se falhar, **desfaz** a sessão gravada (`Store.Delete`) e devolve o erro do keyring com a recuperação "see README \"Headless Linux keyring\", or re-run with --secret-command or --insecure-storage";
  4. `Upsert` do host; define `default_host` se estiver vazio;
  5. `config.Save`;
  6. imprime o mesmo resultado de `auth status`.
- `taiga auth refresh`: `resolver.ForceRefresh` e depois a saída de status.
- `taiga auth status [--diagnose]`:
  - chama `GET users/me` com o resolver e mostra URL e projeto com as fontes, usuário e id, validade e caminho da sessão e se o cache é gravável;
  - se o `users/me` falhar, sai com o exit do erro (3 para autenticação);
  - `--diagnose` acrescenta `checks` e sempre imprime os checks, mesmo quando a identidade falha;
  - texto em TTY, JSON sem TTY.
- `taiga auth logout`: apaga a sessão e o segredo local (keyring ou arquivo; `secret_command` não é tocado), mantém o host na config e imprime `logged out of <url> (<user>)`.

**Checks do `Diagnose`, nesta ordem:**

| Check | Resultado |
|---|---|
| `env_token` | `ok` se `TAIGA_TOKEN` estiver definido; senão `skipped` |
| `env_password` | `ok` ou `skipped` |
| `secret_source` | Executa a fonte configurada (`ok`, ou `failed` com `code: cause`) |
| `keyring` | `KeyringProbe`; `skipped` se nil |
| `session_cache` | `ok` com "valid until …"; `failed` com "expired at …" ou "missing" |
| `session_cache_writable` | `ok` ou `failed` com "read-only (sandbox?)" |
| `sandbox` | `failed` com "CODEX_SANDBOX_NETWORK_DISABLED=1: unix sockets and network are blocked" se a variável estiver definida; senão `ok` |
| `dbus` | `skipped` se `DBUS_SESSION_BUS_ADDRESS` estiver vazio; senão `ok`, com o endereço |
| `tty` | `ok` ou `skipped` ("no TTY: pinentry prompts cannot work") |

O check de rede e identidade é o próprio `users/me` do status.

- [ ] **Step 1: escrever os testes que falham**

`internal/auth/diagnose_test.go`:

```go
package auth

import (
	"context"
	"testing"
	"time"
)

func TestDiagnoseReportsSandboxAndMissingSession(t *testing.T) {
	env := map[string]string{"CODEX_SANDBOX_NETWORK_DISABLED": "1"}
	checks := Diagnose(context.Background(), DiagnoseInput{Env: func(k string) string { return env[k] }, Store: Store{Dir: t.TempDir()}, Ref: "x", Now: time.Now})
	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if byName["sandbox"].Status != "failed" || byName["session_cache"].Status != "failed" || byName["env_token"].Status != "skipped" || byName["tty"].Status != "skipped" {
		t.Fatalf("%+v", checks)
	}
}
```

`internal/cli/auth_test.go`:

```go
package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/taiga-cli/internal/auth"
)

func jwtFor(exp time.Time) string { return testJWT(exp) }

func authServer(t *testing.T) string {
	srv, _ := fakeTaiga(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth":
			_ = json.NewEncoder(w).Encode(map[string]any{"auth_token": jwtFor(time.Now().Add(24 * time.Hour)), "refresh": "r1", "id": 166, "username": "svc"})
		case "/api/v1/users/me":
			w.Write([]byte(`{"id":166,"username":"svc","full_name":"Service"}`))
		}
	})
	return srv.URL
}

func TestLoginWithInsecureStorageThenStatus(t *testing.T) {
	url := authServer(t)
	home := t.TempDir()
	env := map[string]string{"HOME": home}
	_, errOut, code := runIn(t, env, "pw\n", "auth", "login", "--url", url, "--username", "svc", "--password-stdin", "--insecure-storage")
	if code != 0 {
		t.Fatalf("login exit %d: %s", code, errOut)
	}
	ref := auth.SessionRef(url, "svc")
	secret := filepath.Join(home, ".config", "taiga", "secrets", ref)
	if info, err := os.Stat(secret); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret file: %v", err)
	}
	cfg, _ := os.ReadFile(filepath.Join(home, ".config", "taiga", "config.toml"))
	if !strings.Contains(string(cfg), "secret_source = 'file'") && !strings.Contains(string(cfg), `secret_source = "file"`) {
		t.Fatalf("config: %s", cfg)
	}
	if strings.Contains(string(cfg), "pw") {
		t.Fatal("password leaked into config")
	}
	out, errOut, code := runIn(t, env, "", "auth", "status")
	if code != 0 {
		t.Fatalf("status exit %d: %s", code, errOut)
	}
	var st map[string]any
	if err := json.Unmarshal([]byte(out), &st); err != nil || st["user"].(map[string]any)["username"] != "svc" {
		t.Fatalf("status: %s", out)
	}
}

func TestLoginWithoutPasswordSourceOffTTYIsUsage(t *testing.T) {
	url := authServer(t)
	_, errOut, code := runIn(t, nil, "", "auth", "login", "--url", url, "--username", "svc")
	if code != 2 || !strings.Contains(errOut, "--password-stdin") {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}

func TestStatusDiagnoseAlwaysPrintsChecks(t *testing.T) {
	env := map[string]string{"TAIGA_URL": "http://127.0.0.1:1", "TAIGA_USERNAME": "svc", "CODEX_SANDBOX_NETWORK_DISABLED": "1"}
	out, _, code := runIn(t, env, "", "auth", "status", "--diagnose")
	if code == 0 {
		t.Fatal("status without session must fail")
	}
	if !strings.Contains(out, `"sandbox"`) || !strings.Contains(out, `"session_cache"`) {
		t.Fatalf("checks missing: %s", out)
	}
}

func TestLogoutRemovesSessionAndFileSecret(t *testing.T) {
	url := authServer(t)
	home := t.TempDir()
	env := map[string]string{"HOME": home}
	runIn(t, env, "pw\n", "auth", "login", "--url", url, "--username", "svc", "--password-stdin", "--insecure-storage")
	_, errOut, code := runIn(t, env, "", "auth", "logout")
	if code != 0 {
		t.Fatalf("logout: %d %s", code, errOut)
	}
	ref := auth.SessionRef(url, "svc")
	if _, err := os.Stat(filepath.Join(home, ".local", "state", "taiga", "sessions", ref+".json")); !os.IsNotExist(err) {
		t.Fatal("session still present")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "taiga", "secrets", ref)); !os.IsNotExist(err) {
		t.Fatal("secret still present")
	}
}
```

Em `internal/cli/helpers_test.go`, acrescente:

```go
func testJWT(exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp.Unix(), 10) + `}`))
	return "eyJhbGciOiJIUzI1NiJ9." + payload + ".sig"
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/...`
Expected: FAIL, com `undefined: Diagnose` e `unknown command "auth"`.

- [ ] **Step 3: implementar `Diagnose`**

`internal/auth/diagnose.go`:

```go
package auth

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/BasisTI/taiga-cli/internal/output"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type DiagnoseInput struct {
	Env          func(string) string
	Store        Store
	Ref          string
	Secret       SecretSource
	KeyringProbe func(context.Context) error
	StdinTTY     bool
	Now          func() time.Time
}

func Diagnose(ctx context.Context, in DiagnoseInput) []Check {
	var out []Check
	add := func(name, status, detail string) { out = append(out, Check{name, status, detail}) }
	if in.Env("TAIGA_TOKEN") != "" {
		add("env_token", "ok", "TAIGA_TOKEN is set and takes precedence")
	} else {
		add("env_token", "skipped", "")
	}
	if (EnvPassword{Env: in.Env}).Configured() {
		add("env_password", "ok", "")
	} else {
		add("env_password", "skipped", "")
	}
	if in.Secret == nil {
		add("secret_source", "skipped", "none configured")
	} else if _, err := in.Secret.Password(ctx); err != nil {
		e := output.AsError(err)
		add("secret_source", "failed", in.Secret.Name()+": "+e.Code+": "+e.Cause)
	} else {
		add("secret_source", "ok", in.Secret.Name())
	}
	if in.KeyringProbe == nil {
		add("keyring", "skipped", "")
	} else if err := in.KeyringProbe(ctx); err != nil {
		e := output.AsError(err)
		add("keyring", "failed", e.Code+": "+e.Cause)
	} else {
		add("keyring", "ok", "org.freedesktop.secrets is available")
	}
	sess, err := in.Store.Load(in.Ref)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		add("session_cache", "failed", "missing: "+in.Store.Path(in.Ref))
	case err != nil:
		add("session_cache", "failed", err.Error())
	case in.Now().After(sess.Expiry):
		add("session_cache", "failed", "expired at "+sess.Expiry.Format(time.RFC3339))
	default:
		add("session_cache", "ok", "valid until "+sess.Expiry.Format(time.RFC3339))
	}
	if in.Store.Writable() {
		add("session_cache_writable", "ok", in.Store.Dir)
	} else {
		add("session_cache_writable", "failed", "read-only (sandbox?): "+in.Store.Dir)
	}
	if in.Env("CODEX_SANDBOX_NETWORK_DISABLED") != "" {
		add("sandbox", "failed", "CODEX_SANDBOX_NETWORK_DISABLED=1: unix sockets and network are blocked")
	} else {
		add("sandbox", "ok", "")
	}
	if a := in.Env("DBUS_SESSION_BUS_ADDRESS"); a == "" {
		add("dbus", "skipped", "DBUS_SESSION_BUS_ADDRESS is empty")
	} else {
		add("dbus", "ok", a)
	}
	if in.StdinTTY {
		add("tty", "ok", "")
	} else {
		add("tty", "skipped", "no TTY: pinentry prompts cannot work")
	}
	return out
}
```

- [ ] **Step 4: implementar os comandos**

`internal/cli/auth.go`:

```go
package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BasisTI/taiga-cli/internal/auth"
	"github.com/BasisTI/taiga-cli/internal/config"
	"github.com/BasisTI/taiga-cli/internal/output"
	"github.com/BasisTI/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (a *App) authCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Log in, inspect and refresh Taiga credentials"}
	cmd.AddCommand(a.authLoginCmd(), a.authRefreshCmd(), a.authStatusCmd(), a.authLogoutCmd())
	return cmd
}

func (a *App) stdinIsTTY() bool {
	f, ok := a.In.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (a *App) authLoginCmd() *cobra.Command {
	var username, secretCmd string
	var passwordStdin, insecure bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store the secret (keyring, secret command or --insecure-storage)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if username == "" {
				return &output.Error{Code: "usage", Source: "flag", Cause: "--username is required", Exit: output.ExitUsage}
			}
			rc, err := a.runContext()
			if err != nil {
				return err
			}
			var pw []byte
			switch {
			case secretCmd != "":
				if pw, err = (auth.CommandSecret{Args: strings.Fields(secretCmd)}).Password(ctx); err != nil {
					return err
				}
			case passwordStdin:
				b, err := io.ReadAll(io.LimitReader(a.In, 64<<10))
				if err != nil {
					return err
				}
				pw = bytes.TrimRight(b, "\r\n")
			case a.stdinIsTTY():
				io.WriteString(a.Err, "Password: ")
				b, err := term.ReadPassword(int(a.In.(*os.File).Fd()))
				io.WriteString(a.Err, "\n")
				if err != nil {
					return err
				}
				pw = b
			default:
				return &output.Error{Code: "usage", Source: "flag", Cause: "no password source and stdin is not a terminal", Recovery: "use --password-stdin or --secret-command", Exit: output.ExitUsage}
			}
			host, _ := rc.File.Host(rc.Ctx.URL.Value)
			host.URL, host.Username = rc.Ctx.URL.Value, username
			if a.flagProject != "" {
				host.Project = a.flagProject
			}
			r, _ := a.resolver(rc)
			r.Username = username
			ref := auth.SessionRef(host.URL, username)
			if _, err := r.LoginWith(ctx, pw); err != nil {
				return err
			}
			switch {
			case secretCmd != "":
				host.SecretSource, host.SecretCommand = "secret_command", strings.Fields(secretCmd)
			case insecure:
				if err := (auth.FileSecret{Path: filepath.Join(rc.Paths.SecretsDir, ref)}).Put(pw); err != nil {
					return err
				}
				host.SecretSource, host.SecretCommand = "file", nil
			default:
				if err := (auth.Keyring{Ref: ref}).Put(ctx, pw); err != nil {
					_ = r.Store.Delete(ref)
					e := output.AsError(err)
					e.Recovery = "see README \"Headless Linux keyring\", or re-run with --secret-command or --insecure-storage"
					return e
				}
				host.SecretSource, host.SecretCommand = "keyring", nil
			}
			rc.File.Upsert(host)
			if rc.File.DefaultHost == "" {
				rc.File.DefaultHost = host.URL
			}
			if err := config.Save(rc.Paths.ConfigFile, rc.File); err != nil {
				return err
			}
			a.Env = overlayEnv(a.Env, map[string]string{"TAIGA_USERNAME": username})
			return a.printStatus(ctx, false)
		},
	}
	f := cmd.Flags()
	f.StringVar(&username, "username", "", "Taiga username (required)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin")
	f.StringVar(&secretCmd, "secret-command", "", "command that prints the password (split on spaces, no shell)")
	f.BoolVar(&insecure, "insecure-storage", false, "store the password in a 0600 file instead of the keyring")
	return cmd
}

func overlayEnv(base func(string) string, over map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := over[k]; ok {
			return v
		}
		return base(k)
	}
}

func (a *App) authRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Renew the stored session now (run outside sandboxes)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, err := a.runContext()
			if err != nil {
				return err
			}
			r, _ := a.resolver(rc)
			if _, err := r.ForceRefresh(cmd.Context()); err != nil {
				return err
			}
			return a.printStatus(cmd.Context(), false)
		},
	}
}

func (a *App) authStatusCmd() *cobra.Command {
	var diagnose bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show identity, URL/project provenance and session state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.printStatus(cmd.Context(), diagnose)
		},
	}
	cmd.Flags().BoolVar(&diagnose, "diagnose", false, "test every credential source and the environment")
	return cmd
}

type statusView struct {
	URL      config.Value `json:"url"`
	Project  config.Value `json:"project"`
	User     *userView    `json:"user,omitempty"`
	Session  sessionView  `json:"session"`
	Checks   []auth.Check `json:"checks,omitempty"`
	Error    *output.Error `json:"error,omitempty"`
}
type userView struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}
type sessionView struct {
	Path      string `json:"path"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Writable  bool   `json:"writable"`
}

func (a *App) printStatus(ctx context.Context, diagnose bool) error {
	rc, err := a.runContext()
	if err != nil {
		return err
	}
	r, _ := a.resolver(rc)
	ref := auth.SessionRef(r.URL, r.Username)
	v := statusView{URL: rc.Ctx.URL, Project: rc.Ctx.Project, Session: sessionView{Path: r.Store.Path(ref), Writable: r.Store.Writable()}}
	var identityErr *output.Error
	c := taiga.New(r.URL, r, taiga.WithHTTPClient(a.httpClient()))
	resp, err := c.Do(ctx, taiga.Request{Method: http.MethodGet, Path: "users/me"})
	if err != nil {
		identityErr = taiga.ToOutput(err)
		v.Error = identityErr
	} else {
		var u userView
		_ = jsonUnmarshal(resp.Body, &u)
		v.User = &u
	}
	if s, err := r.Store.Load(ref); err == nil {
		v.Session.ExpiresAt = s.Expiry.Format(time.RFC3339)
	}
	if diagnose {
		var probe func(context.Context) error
		host, _ := rc.File.Host(r.URL)
		if host.SecretSource == "keyring" || host.SecretSource == "" {
			probe = auth.Keyring{Ref: ref}.Available
		}
		v.Checks = auth.Diagnose(ctx, auth.DiagnoseInput{Env: a.Env, Store: r.Store, Ref: ref, Secret: r.Secret, KeyringProbe: probe, StdinTTY: a.stdinIsTTY(), Now: time.Now})
	}
	mode, _ := output.DetectMode(a.output, a.OutTTY)
	if mode == output.JSON {
		if err := output.WriteJSON(a.Out, v); err != nil {
			return err
		}
	} else {
		fields := []output.Field{{"url", v.URL.Value + " (" + v.URL.Source + ")"}, {"project", v.Project.Value + " (" + v.Project.Source + ")"}}
		if v.User != nil {
			fields = append(fields, output.Field{"user", v.User.Username})
		}
		fields = append(fields, output.Field{"session", v.Session.ExpiresAt + " " + v.Session.Path})
		for _, ch := range v.Checks {
			fields = append(fields, output.Field{"check " + ch.Name, ch.Status + " " + ch.Detail})
		}
		if err := output.WriteFields(a.Out, fields); err != nil {
			return err
		}
	}
	if identityErr != nil {
		identityErr.Recovery = "run `taiga auth status --diagnose`; " + identityErr.Recovery
		return identityErr
	}
	return nil
}

func (a *App) authLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete the local session and stored secret",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, err := a.runContext()
			if err != nil {
				return err
			}
			r, _ := a.resolver(rc)
			ref := auth.SessionRef(r.URL, r.Username)
			if err := r.Store.Delete(ref); err != nil {
				return err
			}
			host, _ := rc.File.Host(r.URL)
			switch host.SecretSource {
			case "keyring":
				_ = auth.Keyring{Ref: ref}.Delete(cmd.Context())
			case "file":
				_ = os.Remove(filepath.Join(rc.Paths.SecretsDir, ref))
			}
			_, err = io.WriteString(a.Out, "logged out of "+r.URL+" ("+r.Username+")\n")
			return err
		},
	}
}
```

O helper `jsonUnmarshal` já existe em `context.go` (tarefa 8). Registre `root.AddCommand(a.authCmd())` em `root.go`.

No teste `TestStatusDiagnoseAlwaysPrintsChecks`, o `stdout` deve trazer o JSON de status com `checks` **e** o comando deve sair com código diferente de zero. O erro vai para o stderr, pelo `Run`. O `printStatus` já escreve a view antes de devolver o erro.

- [ ] **Step 5: rodar e ver passar**

Run: `go test -race ./...`
Expected: PASS.

- [ ] **Step 6: teste de integração do fluxo de autenticação**

`internal/cli/auth_integration_test.go`:

```go
//go:build integration

package cli

import (
	"strings"
	"testing"

	"github.com/BasisTI/taiga-cli/internal/testtaiga"
)

func TestIntegrationLoginApiRefreshLogout(t *testing.T) {
	env := map[string]string{"HOME": t.TempDir()}
	_, errOut, code := runIn(t, env, testtaiga.ServicePassword+"\n", "auth", "login", "--url", testtaiga.URL(), "--username", testtaiga.ServiceUser, "--password-stdin", "--insecure-storage")
	if code != 0 {
		t.Fatalf("login: %d %s", code, errOut)
	}
	out, errOut, code := runIn(t, env, "", "api", "GET", "users/me")
	if code != 0 || !strings.Contains(out, testtaiga.ServiceUser) {
		t.Fatalf("api via session: %d %s %s", code, out, errOut)
	}
	if _, errOut, code = runIn(t, env, "", "auth", "refresh"); code != 0 {
		t.Fatalf("refresh: %d %s", code, errOut)
	}
	if _, errOut, code = runIn(t, env, "", "auth", "logout"); code != 0 {
		t.Fatalf("logout: %d %s", code, errOut)
	}
	// with the file secret gone and no session, api must fail with an auth error
	_, _, code = runIn(t, env, "", "api", "GET", "users/me")
	if code != 3 {
		t.Fatalf("after logout exit = %d, want 3", code)
	}
}
```

Run: `go test -tags integration -p 1 ./...`
Expected: PASS.

- [ ] **Step 7: commit**

```bash
gofmt -l . && go vet ./... && golangci-lint run ./...
git add -A
git commit -m "Adicionar comandos auth login, refresh, status e logout com diagnóstico"
```

---

### Task 14: documentação, PR da US #244 e release `v0.1.0`

**Files:**
- Modify: `README.md`
- Create: `docs/errors.md`, `docs/guia.md`

- [ ] **Step 1: README em inglês**

Substitua o `README.md` por um documento com estas seções, nesta ordem, e todos os comandos copiáveis:

1. **What it is**: dois parágrafos (cliente da API v1 para pessoas e agentes; `taiga api` cobre toda a API; licença Apache-2.0).
2. **Install**: `go install github.com/BasisTI/taiga-cli/cmd/taiga@latest`, ou baixar o tarball do GitHub Releases e conferir com `sha256sum -c SHA256SUMS --ignore-missing`.
3. **Quickstart**:

```sh
taiga auth login --url https://taiga.example.com --username me
echo 'url = "https://taiga.example.com"'  > .taiga.toml
echo 'project = "my-project"'           >> .taiga.toml
taiga api GET users/me
taiga api GET userstories --query project=37 --paginate
taiga api PATCH userstories/123 --field comment="Deployed to staging" --auto-version
```

4. **Output and exit codes**: JSON sem TTY, texto com TTY, `--output`; tabela dos exit codes 0–7; link para `docs/errors.md`.
5. **Authentication**:
   - ordem de resolução: `TAIGA_TOKEN` → sessão em cache → refresh → env password / `secret_command` / keyring / file;
   - variáveis `TAIGA_URL`, `TAIGA_PROJECT`, `TAIGA_TOKEN`, `TAIGA_TOKEN_TYPE`, `TAIGA_USERNAME`, `TAIGA_PASSWORD`, `TAIGA_PASSWORD_FILE`, `TAIGA_CONFIG`, `TAIGA_STATE_DIR`;
   - `taiga auth status --diagnose`.
6. **Coding agents and sandboxes**:
   - o humano faz login, os agentes só leem o cache de sessão;
   - `session_expired` dentro do sandbox → rode `taiga auth refresh` fora dele;
   - Codex: `network_access = true` e `writable_roots` contendo `~/.local/state/taiga`.
7. **Headless Linux keyring**, com o texto e os comandos abaixo, **literalmente**:

```sh
# Debian/Ubuntu
sudo apt-get install -y gnome-keyring libsecret-tools dbus-user-session

mkdir -p ~/.config/systemd/user
cat > ~/.config/systemd/user/gnome-keyring-secrets.service <<'EOF'
[Unit]
Description=GNOME Keyring (Secret Service only)

[Service]
Type=simple
ExecStart=/usr/bin/gnome-keyring-daemon --foreground --components=secrets
Restart=on-failure

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user enable --now gnome-keyring-secrets.service
loginctl enable-linger "$USER"   # keep the user manager running without an open session

# once per boot: unlock (the first unlock creates the "login" collection with this password)
read -rs KP && printf '%s' "$KP" | gnome-keyring-daemon --unlock --components=secrets >/dev/null; unset KP

# check
busctl --user list | grep org.freedesktop.secrets
secret-tool store --label=probe test probe <<<"ok" && secret-tool lookup test probe && secret-tool clear test probe
taiga auth status --diagnose
```

Acrescente três frases:
- o mesmo keyring serve para outras ferramentas que usam libsecret (por exemplo `sonar auth login`);
- KeePassXC e KWallet não funcionam sem sessão gráfica;
- se desbloquear a cada boot for inaceitável, use `--secret-command` com um gerenciador de senhas.

Antes de commitar, rode o bloco numa VM ou container Ubuntu limpo, ou na própria máquina de trabalho, se o humano autorizar instalar os pacotes. Se algum comando exigir ajuste (por exemplo, a unidade já existir no pacote), corrija o texto para o que funcionou e registre no PR o ambiente em que foi validado.

8. **Development**:
   - `go test ./...`;
   - `docker compose -f compose.test.yml up -d && scripts/taiga-seed && go test -tags integration -p 1 ./...`;
   - link para `docs/superpowers/specs/`.

- [ ] **Step 2: `docs/errors.md` e `docs/guia.md`**

`docs/errors.md`:
- tabela com todos os `code` emitidos pelo código, com colunas `code | exit | source | quando ocorre | recuperação`. Gere a lista com `grep -rhoE 'Code: +"[a-z_]+"|authErr\("[a-z_]+"' internal | sort -u` e complete a partir das definições;
- tabela dos exit codes.

`docs/guia.md` (PT-BR): guia curto de uso para a equipe Basis:
- login com a conta de serviço;
- `.taiga.toml` nos repositórios;
- `taiga api` para as operações que ainda não têm comando curado, com os exemplos de comentário, bloqueio e campos customizados vindos das receitas atuais da `basis-ci-gitlab`:

```sh
taiga api PATCH userstories/<id> --raw-field comment="$(cat nota.md)" --auto-version
taiga api PATCH userstories/<id> --field is_blocked=true --raw-field blocked_note="aguarda B6" --auto-version
taiga api GET userstories/custom-attributes-values/<id>
taiga api PATCH userstories/custom-attributes-values/<id> --input valores.json --auto-version
```
- como proceder no Codex.

- [ ] **Step 3: verificação final**

Run: `gofmt -l . && go vet ./... && golangci-lint run ./... && go test -race ./... && go test -tags integration -p 1 ./...`
Expected: tudo PASS e `0 issues.`

Run: `go build -o taiga ./cmd/taiga && ./taiga --help && ./taiga auth status --diagnose --output text || true`
Expected: o help lista `api`, `auth` e `version`, e o diagnóstico é impresso.

- [ ] **Step 4: commit, PR da US #244 e status**

```bash
git add -A
git commit -m "Documentar instalação, autenticação, keyring headless e códigos de erro"
git push -u origin TG-244
gh pr create --title "TG-244 Autenticação" --body "US #244 (Taiga Infraestrutura). Cache de sessão, refresh sob flock, fontes de segredo, taiga auth e documentação do keyring headless."
```

Peça a revisão do Codex. Depois do merge com squash, mude a US #244 para `Ready for test`.

- [ ] **Step 5: release `v0.1.0`, com autorização explícita do humano**

Só com o "pode publicar" do humano:

```bash
git switch main && git pull
git tag -a v0.1.0 -m "taiga-cli v0.1.0: taiga api e taiga auth"
git push origin v0.1.0
gh run watch "$(gh run list --workflow release --limit 1 --json databaseId -q '.[0].databaseId')"
gh release view v0.1.0
```

Expected: o release traz os tarballs `taiga_0.1.0_{linux,darwin}_{amd64,arm64}.tar.gz` e o `SHA256SUMS`. Instale numa máquina limpa com `go install github.com/BasisTI/taiga-cli/cmd/taiga@v0.1.0` e rode `taiga version`.

---

## Depois da fase 1

Os planos das fases 2 a 4 (US #246 a #254) serão escritos a partir desta base. Eles reaproveitam:
- `Client.WriteVersioned`, para as escritas com `version`;
- `GetAll`, para as listagens;
- o `RunContext`, para o projeto;
- `output`, para texto e JSON.

A primeira tarefa da fase 2 valida `userstories/by_ref` no Taiga local e registra o resultado em `docs/api-notes.md`.
