# taiga-cli — design do MVP

**Data:** 2026-09-30
**Status:** aprovado em conversa (seções 1 a 4); aguardando revisão da spec escrita
**Épico:** Taiga Infraestrutura (projeto 37, `infra-2025`) #242 — US #243 a #254
**Licença:** Apache 2.0

## 1. Contexto

O MCP do Taiga usado pelos agentes da Basis (fork `BasisTI/taiga-mcp` de `OFFSET3/taiga-mcp`) não tem licença identificada e cobre só o básico. As lacunas são contornadas com `curl` + `jq` pela skill `basis-ci-gitlab`: campos customizados, status, bloqueio, `assigned_users` e comentários. Anexos e swimlanes nunca foram resolvidos. A autenticação é a maior fonte de atrito: no host headless não há Secret Service, e no sandbox do Codex falta TTY, sockets unix e escrita na home.

A `taiga-cli` é uma ferramenta nova, pública, escrita do zero em Go. Não copia código do fork. Reaproveita código da `sgo-cli` (Basis, Apache 2.0) onde couber.

## 2. Objetivos e não objetivos

**Objetivos do MVP:**

- Cobrir as 12 US do épico #242 (seção 11).
- Um agente sem TTY, inclusive o Codex em sandbox, autentica e opera sem intervenção enquanto a sessão estiver válida.
- Toda a API v1 fica acessível desde o primeiro dia por `taiga api`.
- A `basis-ci-gitlab` deixa de chamar a API com `curl`.

**Meta de longo prazo:** comandos curados para toda a API; um MCP fino gerado a partir das mesmas operações, para clientes sem shell.

**Fora do MVP:**
- operações de exclusão (`DELETE`) em comandos curados;
- issues, wiki, webhooks, memberships e roles curados (ficam acessíveis por `taiga api`);
- servidor MCP;
- cache de catálogos em disco;
- Windows.

## 3. Decisões

| Tema | Decisão |
|---|---|
| Linguagem e framework | Go, `github.com/spf13/cobra` |
| Binário | `taiga` (repositório `taiga-cli`) |
| Idioma | Comandos, flags, códigos de erro, mensagens e `README.md` em inglês. `docs/` e a skill em PT-BR |
| Superfície | Comandos recurso-verbo curados onde há semântica, mais o genérico `taiga api` |
| Projeto do contexto | `--project` > `TAIGA_PROJECT` > `.taiga.toml` mais próximo subindo do cwd > projeto padrão da config do usuário |
| Saída | Com TTY: texto. Sem TTY: JSON estável. `--output json\|text` força um dos dois |
| Versionamento | SemVer, a partir de `v0.1.0` (exigido por Go modules e GoReleaser) |
| CI e release | GitHub Actions + GoReleaser; binários Linux e macOS, amd64 e arm64, com `SHA256SUMS`, em GitHub Releases; instalação por `go install` ou `install.sh` |

## 4. Arquitetura

```
cmd/taiga            main: monta a árvore de comandos
internal/cli         comandos cobra, parsing de flags, escolha de saída
internal/app         casos de uso com semântica (version, merges, bloqueio, plano/apply)
internal/taiga       cliente HTTP da API v1, tipos dos recursos, erros tipados
internal/auth        fontes de credencial, cache de sessão, refresh, diagnóstico
internal/config      resolução de URL e projeto, caminhos XDG
internal/output      texto/JSON, envelope de erro, exit codes
```

- **Dependências:** `cli` → `app` → `taiga` → `auth` → `config`. `output` não depende de ninguém.
- **`taiga api`:** usa `internal/taiga` direto, sem passar por `app`.
- **`app`:** depende de uma interface estreita do cliente, para ser testado com fake.
- **Reaproveitamento da `sgo-cli`** (`internal/credenciais`), com a mudança obrigatória de **capturar e sanitizar o stderr** do `secret_command`, que lá é descartado:
  - `keyring_linux.go`: Secret Service por D-Bus (godbus), sem prompt, timeout de 5 s, erros tipados;
  - o executor de `secret_command`: argv sem shell, timeout de 10 s, saída de até 64 KiB.

## 5. Configuração e contexto

**Arquivos:**

| Caminho | Conteúdo |
|---|---|
| `$XDG_CONFIG_HOME/taiga/config.toml` (ou `TAIGA_CONFIG`) | Uma entrada por host: URL, usuário, fonte do segredo (`keyring`, `secret_command` ou `file`) e projeto padrão. Nunca a senha |
| `$XDG_STATE_HOME/taiga/sessions/<sha256(url\0user)>.json` (ou `TAIGA_STATE_DIR`) | Cache de sessão, permissão 0600 |
| `.taiga.toml` (versionado no repositório do projeto) | `url` e `project` (slug ou id) |

**Variáveis de ambiente:** `TAIGA_URL`, `TAIGA_PROJECT`, `TAIGA_TOKEN`, `TAIGA_TOKEN_TYPE` (`Bearer` por padrão, ou `Application`), `TAIGA_USERNAME`, `TAIGA_PASSWORD`, `TAIGA_PASSWORD_FILE`, `TAIGA_CONFIG`, `TAIGA_STATE_DIR`.

**Proveniência:** URL e projeto sempre informam de onde vieram: flag, env, `.taiga.toml` com o caminho, ou config. `auth status` mostra essa origem.

## 6. Fluxo de uma operação e concorrência

Exemplo: `taiga story update 243 --status "In progress" --add-assignee me`.

1. `config` resolve URL e projeto.
2. `auth` entrega o token (seção 8).
3. `app` resolve nomes para ids:
   - a ref vira o id da story (`userstories/by_ref`, a validar);
   - o nome do status vira o id do status;
   - `me` vira o id do usuário (`users/me`).

   Os catálogos (status, usuários, campos) ficam em memória só durante o processo.
4. `app` lê o recurso e calcula o patch mínimo. Regras:
   - **Responsáveis:** `assigned_users` = atuais ∪ novos − removidos. O `assigned_to` só é alterado por flag explícita.
   - **Campos customizados:** ler `userstories/custom-attributes-values/<id>`, alterar as chaves e gravar o dicionário **inteiro** com o `version` próprio desse recurso.
   - **Tags:** a API devolve pares `[nome, cor]`; normalizar para nomes antes de unir ou remover.
5. `taiga` envia o `PATCH` com o `version` lido. Num conflito (`version` inválido ou desatualizado):
   1. relê o recurso uma vez;
   2. se os campos que o patch altera não mudaram entre as duas leituras, repete com o novo `version`;
   3. caso contrário, devolve `version_conflict` sem repetir. `--force-version` ignora essa verificação.
6. Relê o recurso e imprime o resultado.

**`--dry-run`:** existe em toda escrita, incluindo `taiga api`. Executa os passos 1 a 4 e imprime método, caminho e corpo, sem enviar.

**Robustez:**
- JSON sempre por `encoding/json`, nunca montado como string.
- Timeout HTTP de 30 s.
- Nova tentativa automática só para `GET` em erro de rede ou 5xx, com backoff curto e no máximo duas tentativas.
- Paginação transparente nas listagens: `x-disable-pagination: True` quando suportado; senão, segue as páginas.

## 7. Saída e erros

**Saída:**
- **JSON sem TTY:** objetos da API normalizados, sempre com `ref`, `id`, `url` (montada como `<url>/project/<slug>/us/<ref>` e equivalentes) e `version`. Listagens saem como array.
- **Texto com TTY:** resumo legível com a URL.

**Envelope de erro** no stderr (JSON sem TTY, texto com TTY):

```json
{"error":{"code":"version_conflict","source":"api","stage":"patch userstories/6805",
  "cause":"field status changed by cedriclamalle at 2026-09-29T20:01Z",
  "recovery":"re-run after reviewing the story; use --force-version to override"}}
```

- `code` é estável e documentado em `docs/errors.md`.
- `cause` preserva o corpo de erro da API do Taiga e nunca contém segredo.
- `source` é um de: `config`, `env`, `secret_command`, `keyring`, `file`, `session_cache`, `api`, `network`.

**Exit codes:**

| Código | Significado |
|---|---|
| 0 | OK |
| 1 | Erro inesperado |
| 2 | Uso inválido |
| 3 | Autenticação |
| 4 | Conflito de `version` |
| 5 | Não encontrado |
| 6 | Permissão negada |
| 7 | Rede ou servidor |

## 8. Autenticação

**Comandos:**

- `taiga auth login [--url U] [--username N] [--password-stdin | --secret-command "argv"] [--insecure-storage]` — uso humano:
  1. valida com `POST /auth` e `GET /users/me`;
  2. guarda o segredo de longo prazo no Secret Service, ou registra o `secret_command` na config;
  3. grava o cache de sessão.

  Sem cofre disponível, falha com diagnóstico. `--insecure-storage` grava a senha em arquivo 0600, só por opt-in explícito.
- `taiga auth refresh` renova a sessão. É o que se roda fora do sandbox quando um agente recebe `session_expired`.
- `taiga auth status [--diagnose]` mostra identidade, URL, projeto e a origem de cada um. Com `--diagnose`, testa e reporta cada item como `ok`, `skipped` ou `failed`, com o motivo:
  - as fontes env, `secret_command`, keyring e arquivo;
  - o cache de sessão: validade e se é gravável;
  - o sandbox: `CODEX_SANDBOX_NETWORK_DISABLED`, home gravável ou não;
  - D-Bus (`DBUS_SESSION_BUS_ADDRESS`, `org.freedesktop.secrets`), gpg-agent e TTY;
  - a rede até a URL e `GET /users/me`.
- `taiga auth logout` apaga o cache de sessão e o segredo local. Não revoga nada no servidor.

**Resolução do token**, em ordem:

1. `TAIGA_TOKEN`, usado como está, com o tipo dado por `TAIGA_TOKEN_TYPE`.
2. Cache de sessão válido, considerando uma margem antes do `exp`.
3. Cache expirado com `refresh` válido: `POST /auth/refresh` sob `flock` no arquivo da sessão, para que processos concorrentes não disputem o mesmo refresh; o resultado é gravado atomicamente.
4. Login com a fonte configurada:
   - `TAIGA_USERNAME` + `TAIGA_PASSWORD` ou `TAIGA_PASSWORD_FILE`;
   - senão, `secret_command`;
   - senão, keyring.
5. Falha: erro `auth_*` (por exemplo `auth_no_source`, `session_expired`, `secret_command_timeout`, `keyring_locked`), com a causa e a recuperação.

**Cache sem permissão de gravação (sandbox):** o token renovado fica em memória e sai o aviso `session_cache_readonly`.

- **Condição a validar no Taiga local:** se o refresh for rotacionado e o anterior invalidado, renovar sem conseguir gravar queimaria o refresh salvo.
- **Se isso se confirmar:** em modo somente leitura a CLI não renova e devolve `session_expired`, com a recuperação "run `taiga auth refresh` outside the sandbox".

**Conta de serviço dos agentes:** o humano roda `taiga auth login --username <conta-de-serviço>` uma vez por máquina e repete quando o refresh expira. Se os application tokens do Taiga servirem para conta de serviço, passam a ser uma fonte adicional documentada.

**Keyring em host headless:** o `README.md` (EN) e a skill (PT-BR) trazem a seção "Headless Linux keyring":
- instalar `gnome-keyring` e `libsecret-tools`;
- criar uma unidade `systemd --user` para `gnome-keyring-daemon --components=secrets`;
- desbloquear uma vez por boot com `gnome-keyring-daemon --unlock` pelo stdin;
- verificar com `busctl --user list | grep org.freedesktop.secrets` e com `secret-tool`;
- configurar o Codex: rede ligada (`network_access = true`) e `writable_roots` incluindo o state dir.

O `auth status --diagnose` aponta para essa seção quando não encontra Secret Service. KeePassXC e kwallet não são suportados em headless.

## 9. Comandos do MVP

Flags comuns: `--project`, `--output`, `--dry-run` (em escritas).

**Autenticação e acesso genérico:**

| Comando | Faz |
|---|---|
| `taiga auth login\|refresh\|status [--diagnose]\|logout` | Seção 8 |
| `taiga api METHOD PATH [--query k=v]... [--field k=v]... [--input file.json] [--auto-version] [--paginate]` | Chamada genérica. `--auto-version` lê o recurso para obter o `version` antes do `PATCH` ou `PUT` |

**Projeto, usuários e catálogos:**

| Comando | Faz |
|---|---|
| `taiga project list\|get` | Projetos acessíveis e detalhe |
| `taiga user list [--search]` | Usuários do projeto |
| `taiga status list [--kind story\|task]` | Status do projeto |
| `taiga swimlane list` | Swimlanes do projeto |
| `taiga milestone list` | Sprints |

**Stories:**

| Comando | Faz |
|---|---|
| `taiga story list [--status] [--assignee] [--epic] [--tag] [--search] [--closed]` | Listagem sem paginação manual |
| `taiga story get REF\|--id ID` | Detalhe com `url`, campos customizados e bloqueio |
| `taiga story create --subject S [--description-file F] [--status] [--tag]... [--assignee]... [--epic] [--swimlane]` | Criação |
| `taiga story update REF [flags]` | `--subject`, `--description-file`, `--append-description`, `--status`, `--tag`, `--add-tag`, `--remove-tag`, `--epic`, `--milestone`, `--swimlane`, `--add-assignee`, `--remove-assignee`, `--owner-assignee`, `--block NOTE`, `--unblock` |
| `taiga story close REF [--status NAME]` | Move para um status fechado. Não arquiva nem exclui |
| `taiga story field list REF` | Valores dos campos customizados |
| `taiga story field set REF "Nome"=valor...` | Grava com merge |
| `taiga story comment REF --body TEXT\|--body-file F` | `PATCH` com `comment` e `version` |
| `taiga story comments REF [--include-system]` | Lista pelo `history/userstory`, omitindo por padrão os comentários automáticos de integrações |

**Campos, projeto como código e anexos:**

| Comando | Faz |
|---|---|
| `taiga field list\|create --kind story\|task --name N --type text\|date\|checkbox\|...` | Definições de campos customizados; criação idempotente pelo nome |
| `taiga project plan\|apply -f taiga-project.toml` | Status e campos como código (US #249) |
| `taiga attachment list\|upload\|download REF [--task]` | Anexos de story (e de task) |

**Épicos e tasks:**

| Comando | Faz |
|---|---|
| `taiga epic list\|get\|link EPIC_REF STORY_REF` | Épicos e vínculo com stories |
| `taiga task list [--story REF] \| get \| create --story REF ... \| update \| close` | Tasks com os mesmos padrões de responsáveis, status, tags, `version` e campos |

**Projeto como código:**
- `plan` compara o arquivo com o projeto e imprime as ações.
- `apply` executa. Idempotente.
- Só cria e reordena. Itens do projeto ausentes do arquivo são listados, nunca removidos.
- Exige `admin_project_values`. Sem essa permissão, sai com exit 6 e informa a permissão que falta.
- Formato:

```toml
[[story_status]]
name = "In revision"
color = "#8E44AD"
closed = false
after = "In progress"

[[story_field]]
name = "Testado em staging"
type = "checkbox"
description = "Registro de teste da versão entregue"
```

## 10. Testes

1. **Unidade (`go test ./...`):**
   - `app` com cliente fake: merges, `assigned_users`, a regra de nova tentativa de `version`, plano/apply, resolução de ref, status e usuário;
   - `config` e `auth` com diretórios temporários: precedência, permissão 0600, `flock`, modo somente leitura, `secret_command` com timeout e stderr;
   - o keyring com um `dbus-daemon` isolado.
2. **Integração (tag de build `integration`) contra o Taiga 6.7 local:**
   - `compose.test.yml` com `taigaio/taiga-back:6.7.3`, `taigaio/taiga-events:6.7.0`, `taigaio/taiga-front:6.7.7`, `taigaio/taiga-protected:6.7.0`, Postgres 12 e RabbitMQ 3.8, sem as imagens `-openid` e com segredos só de teste;
   - `scripts/taiga-seed` cria o superusuário, a conta de serviço e o projeto de teste pela API;
   - executa os comandos curados e `taiga api` de ponta a ponta.

   No CI, o mesmo compose roda no GitHub Actions.
3. **Contrato de saída:** golden files do JSON e do texto dos comandos principais e dos envelopes de erro.

Os testes nunca escrevem na instância `agile.basis.com.br`. Um smoke test manual e somente leitura (`auth status`, `story get`) é permitido.

**Pontos a validar primeiro no Taiga local:**
- rotação e invalidação do refresh;
- tempos de vida do `auth_token` e do refresh;
- application tokens para conta de serviço;
- `userstories/by_ref`;
- escrita de swimlane numa story;
- upload multipart de anexo;
- identificação de comentários no `history/userstory`;
- suporte a `x-disable-pagination`.

## 11. Entregas

| Fase | US | Conteúdo |
|---|---|---|
| 1 | #243 | Fundação: módulo, `LICENSE`, estrutura, CI, GoReleaser, compose de teste e seed |
| 1 | #245 | Núcleo HTTP: cliente, `version`, paginação, erros, saída, `--dry-run`, `taiga api` |
| 1 | #244 | Autenticação (seção 8), com a seção do keyring headless no README |
| 2 | #246 | Stories: list, get, create, update, close |
| 2 | #247 | Responsáveis e bloqueio |
| 2 | #248 | Campos customizados: definições e valores |
| 2 | #250 | Comentários |
| 2 | #249 | Projeto como código (status e campos) |
| 3 | #251 | Anexos |
| 3 | #252 | Swimlanes |
| 3 | #253 | Paridade com o MCP: projetos, usuários, milestones, épicos, tasks |
| 4 | #254 | Skill `skills/taiga-cli/SKILL.md` e migração da `basis-ci-gitlab` |

**Fluxo de trabalho:**
- Uma branch `TG-<ref>` e um PR por US, com squash no merge.
- A story no Taiga acompanha os status do fluxo.
- Implementação com Claude Code + Opus 5.5 (effort medium); revisão do PR com Codex + GPT-6-Sol (effort medium).
- `v0.1.0` sai ao fim da fase 1; o MVP fecha em `v0.x` ao fim da fase 4.

## 12. Documentação

| Arquivo | Idioma | Conteúdo |
|---|---|---|
| `README.md` | Inglês | Instalação, quickstart, autenticação, keyring headless, `.taiga.toml` |
| `docs/` | PT-BR | Guia de uso, esta spec, planos |
| `docs/errors.md` | — | Códigos de erro e exit codes |
| `skills/taiga-cli/SKILL.md` | PT-BR | Uso por agentes, recuperação de erros de autenticação, seção do keyring. Instalável pelo skills CLI |

## 13. Referências

- Documentação da API: https://docs.taiga.io/api.html
- Levantamento das lacunas e contornos (ai-memory `perso/vault-assistente`):
  - `notes/taiga-mcp-funcionalidades-e-limitacoes`;
  - `notes/taiga-api-v1-catalogo-de-rotas`;
  - `notes/credenciais-de-cli-em-host-headless-e-sandbox-do-codex`.
- Decisões (mesmo projeto):
  - `decisions/taiga-cli-substitui-mcp-do-taiga`;
  - `decisions/taiga-cli-autenticacao-cache-de-sessao-e-keyring-headless`.
- Referências de terceiros com licença MIT (consulta, preservando aviso se houver reuso): `nephila/python-taiga`, `illodev/taiga-mcp`, `TETRA-2023/pytaiga-mcp`.
- `sgo-cli` (`basis/convey`, pasta `sgo-cli/`): credenciais, `secret_command` e keyring.
