# taiga-cli — Fase 4 (skill da CLI e migração da basis-ci-gitlab) — plano

> **For agentic workers:** cada PR abaixo é executado por um agente a partir de um pacote do coordenador (Assistente Global), que define escopo, autorizações e devolução. Este documento registra o plano aprovado e as decisões; não autoriza push, PR, merge nem mudança de status no Taiga.

**Goal:** Fechar a US #254 ("Skill da taiga-cli e migração da basis-ci-gitlab"): publicar uma skill que ensina agentes a usar a CLI, lançar a `v0.3.0` e trocar o MCP do Taiga e os scripts `curl` da skill `basis-ci-gitlab` pela CLI.

**Spec:** `docs/superpowers/specs/2026-09-30-taiga-cli-design.md` ("MVP fecha em `v0.x`"). Base: `docs/superpowers/plans/2026-10-01-fase-3-anexos-swimlanes-paridade.md`, seção "Fase 4 (#254) — registro de escopo".

**Fontes lidas no planejamento (2026-10-04, só leitura):** os repositórios `BasisTI/taiga-cli` e `BasisTI/skills`; o projeto 37 (`infra-2025`) pelo MCP (`taiga_projects_get`); um build local da CLI. O `project plan` não rodou por falta de config e login na máquina do planejamento.

## Situação

| PR | US | Estado |
|---|---|---|
| PR 1 | #273, redigir tokens na saída do completion | mesclado: #17, `369bf02` |
| PR 2 | #274, redirect em escritas como resultado incerto | mesclado: #16, `557596d` |
| PR 3 | #254 parte A, skill no taiga-cli | este PR (branch `TG-254`) |
| — | release `v0.3.0` | pendente (Cedric) |
| PR 4 | #254 parte B, `BasisTI/skills` | pendente, depois da `v0.3.0` |

## Achados que mudaram o plano

1. **O board 37 não está configurado.** Os status de US são New, Ready, In progress, Ready for test, Done e Archived: faltam `In revision` e `Waiting for deployment`. O único campo customizado de US é `Ocorrência SGO`; nenhum dos 6 campos de registro existe. Antes de "`project plan` sem diferenças" valer alguma coisa, é preciso um `taiga project apply` no 37, rodado pelo Cedric, que é admin (`admin_project_values`). Será o primeiro apply real contra produção.
2. **O projeto 37 está com o módulo de épicos desligado** (`is_epics_activated: false`), embora o épico #242 seja citado. Não afeta a #254; fica para o coordenador conferir.
3. **O state dir padrão é `~/.local/state/taiga`**, não `~/.local/share`. O `~/.codex/config.toml` da máquina do Cedric tem `network_access = true`, mas não tem `writable_roots` para o state dir.
4. **O `go install …@latest` só serve depois da `v0.3.0`.** No planejamento ele resolvia para a `v0.2.0`, sem a fase 3. A release usa goreleaser: tar.gz linux/darwin amd64/arm64 e `SHA256SUMS`.

## Global Constraints

- Docs e skill em PT-BR; README, comandos, flags e mensagens em inglês.
- A skill só cita comando, flag e código que existem no `main`, conferidos contra `docs/errors.md` e o `--help` de um binário compilado da branch.
- Nunca escrever em `agile.basis.com.br` a partir dos PRs; `auth login` e `project apply` no board 37 são passos humanos.
- Desligar o MCP do Taiga nas configs dos agentes fica fora da #254.

## PR 1 — #273 (branch `TG-273`)

Redigir tokens na saída do completion.
**Aceite:** nenhum token ou segredo na saída do completion, com teste que prove; CI `ci` verde.

## PR 2 — #274 (branch `TG-274`)

Tratar redirect em escritas como resultado incerto.
**Aceite:** uma escrita que recebe 3xx sai com código "gravado, ou talvez gravado: não repetir às cegas", documentado em `docs/errors.md`, com teste; CI verde.

## PR 3 — #254 parte A (branch `TG-254`)

Arquivos:

- `skills/taiga-cli/SKILL.md` (PT-BR), novo, e `skills/taiga-cli/references/instalacao.md`:
  - instalação: binário em `~/.local/bin`; principal, o tar.gz da release conferido pelo `SHA256SUMS`; alternativa, `go install github.com/BasisTI/taiga-cli/cmd/taiga@v0.3.0` (D7, D12); skill por `npx skills add BasisTI/taiga-cli`;
  - autenticação: a pessoa faz `taiga auth login`, o agente lê o cache; Codex com o state dir em `writable_roots` e rede (D9); `auth_rejected`, `session_expired` e `session_cache_readonly` = parar e pedir à pessoa login ou refresh fora do sandbox (D8);
  - keyring headless: resumo com link para o README (decisão de 2026-09-29);
  - uso por agentes: comandos curados (story, task, comentário, anexo, status, swimlane, épico, `field`, `project plan|apply`, `auth status --diagnose`), `taiga api` como saída de emergência, decisão pelo `code` e não pelo exit, `--dry-run` antes de escrita em lote;
  - os códigos "gravado, ou talvez gravado: não repetir às cegas", derivados de `docs/errors.md`, cada um com a conferência; as exceções que convergem (`epic_link_unconfirmed`, `epic_replace_incomplete`);
  - precedência sobre o MCP e o aviso de que não é aconselhado ter os dois (D6);
  - completion: o script gerado não grava o debug do shell (#17).
- `docs/examples/taiga-project.toml`: cabeçalho conforme a linha 9 da tabela D5; dados inalterados.
- `README.md` e `docs/guia.md`: apontar para a skill e para a instalação em `~/.local/bin`; a receita do keyring continua no README.
- Este plano.

**Aceite:** todo comando e código citado na skill existe no `main`; a instalação por `npx skills add BasisTI/taiga-cli` é conferida pelo Cedric depois do merge (D14); links internos resolvem; CI verde.

## Passo humano 1 — release `v0.3.0` (Cedric)

Depois dos PRs 1 a 3 mesclados: tag `v0.3.0` no `main`; o workflow `release.yml` publica os tar.gz e o `SHA256SUMS`.
**Aceite:** release `v0.3.0` com 4 tar.gz e `SHA256SUMS`; `go install …@latest` resolve para `v0.3.0`.

## Passo humano 2 — instalação e board (Cedric)

1. Instalar o `taiga` `v0.3.0` em `~/.local/bin` e a skill pelo skills CLI (global).
2. `taiga auth login --url https://agile.basis.com.br`.
3. Acrescentar `writable_roots = ["/home/cedric/.local/state/taiga"]` ao `[sandbox_workspace_write]` do `~/.codex/config.toml`.
4. `taiga project plan -f docs/examples/taiga-project.toml --project 37`, revisar, depois `taiga project apply` no board 37 (cria 2 status e 6 campos).

**Aceite:** `taiga auth status --diagnose` ok, inclusive dentro do Codex; `project plan` sem ações nem drift no 37.

## PR 4 — #254 parte B, `BasisTI/skills` (branch `TG-254`)

Arquivos em `skills/basis-ci-gitlab/`:

- remover `references/taiga-mcp.md`, `scripts/taiga-env.sh` e `scripts/configurar-taiga-projeto.sh`;
- criar `references/taiga.md` enxuto (D10): receitas do fluxo com comandos `taiga`, mantendo as âncoras `#atualizar-o-status`, `#a-story-pode-começar`, `#retomar-story-bloqueada`, `#mudança-de-configuração-tag-config`, `#revisão-global-tag-review` e as demais citadas; preparar o board passa a ser `taiga project apply` com o TOML canônico do taiga-cli;
- `SKILL.md`: trocar a seção "## 2. O Taiga pelo MCP" por "O Taiga pela CLI", que exige a skill `taiga-cli` e a CLI instaladas e dá precedência à CLI sobre o MCP; tirar os dois scripts da tabela; trocar o link de `taiga-mcp.md` por `taiga.md`;
- `references/ciclo-da-mr.md`, `revisao-da-mr.md`, `worktree.md` e `cadeia-de-entrega.md`: trocar `taiga-mcp.md#…` por `taiga.md#…`.

**Aceite:** `grep -rn "taiga-mcp\|taiga-env\|configurar-taiga-projeto\|mcp__taiga\|curl" skills/basis-ci-gitlab` sem ocorrências ligadas ao Taiga; todos os links e âncoras resolvem; nenhuma instrução da skill pede curl/jq para o Taiga.

## Prova de aceite da #254

Depois do PR 4 mesclado e do `npx skills update --global --yes`, um agente (Codex no sandbox, D13), seguindo a `basis-ci-gitlab` migrada e a skill `taiga-cli`, numa US de teste no board 37:

1. cria a US;
2. comenta;
3. anexa um arquivo;
4. muda o status (por exemplo New → In progress → In revision) e grava os campos de registro de início (`Início da implementação`, `Executor`, `Worktree`);
5. não usa curl, jq nem o MCP do Taiga;
6. o Cedric confere na interface e apaga a US.

Também: `taiga project plan` sem diferenças no 37.

## Dependências

- PR 3 depende de #273 e #274 mesclados (D4); a skill cita os códigos novos da #274.
- `v0.3.0` depende dos PRs 1 a 3.
- PR 4 depende da `v0.3.0`, da skill instalável a partir do `main` do `BasisTI/taiga-cli` e do passo humano 2 (D2).
- A prova depende do PR 4 mesclado e do `npx skills update --global --yes`: a cópia instalada não se atualiza sozinha.

## Decisões

### Decisões tomadas (com origem)

Origem de D1–D14: Cedric, conversa de planejamento de 2026-10-04, encaminhada pelo Assistente Global.

| # | Decisão |
|---|---|
| D1 | A skill mora no repositório taiga-cli (`skills/taiga-cli/SKILL.md`), versionada com o binário e instalável pelo skills CLI a partir de `BasisTI/taiga-cli`. |
| D2 | Ordem: primeiro o PR no taiga-cli, depois o PR no `BasisTI/skills`, este só após a CLI e a skill instaladas. |
| D3 | Release **`v0.3.0`**, normal (não RC), antes da migração. A tag é criada pelo Cedric. Descartadas `v1.0.0-rc.1` e a mudança para 1.0; o spec ("MVP fecha em `v0.x`") segue valendo. |
| D4 | A #273 e a #274 são implementadas **antes** da skill, e portanto antes da `v0.3.0`. |
| D5 | Tabela de diferenças TOML × script **aprovada inteira** (abaixo), inclusive a mudança do comentário do TOML. Cumpre a exigência da decisão de 2026-10-01. |
| D6 | O que é do MCP antigo é **removido** da `basis-ci-gitlab`. Se o MCP e a CLI estiverem instalados, **a CLI tem precedência**; a skill diz que não é aconselhado ter os dois. Desligar o MCP das configs continua fora da #254. |
| D7 | O binário é instalado em `~/.local/bin`, como o `sgo`. |
| D8 | Em erro de autenticação, o agente para e pede ao usuário que refaça o login (`taiga auth login`, ou `taiga auth refresh` quando é `session_expired`) fora do sandbox. O agente nunca pede senha nem usa `TAIGA_PASSWORD`. |
| D9 | `TAIGA_STATE_DIR` no Codex: **opção a**. Fica o padrão `~/.local/state/taiga`, incluído em `[sandbox_workspace_write] writable_roots`, com `network_access = true`. Descartados o cache somente leitura como modo normal e o state dir em `/tmp` ou no workspace. |
| D10 | A `basis-ci-gitlab` mantém um `references/taiga.md` enxuto com as receitas do fluxo (status, início, bloqueio, tags config/review), agora com comandos `taiga`, **preservando as âncoras** usadas por `ciclo-da-mr.md`, `revisao-da-mr.md`, `worktree.md` e `cadeia-de-entrega.md`. O uso geral da CLI fica na skill taiga-cli. |
| D11 | Prova de aceite: um agente cria, comenta, anexa e muda o status de uma US de teste no Taiga; o Cedric apaga a US pela interface no fim. |
| D12 | Instalação na skill: o tar.gz da release, conferido pelo `SHA256SUMS`, é o caminho principal; o `go install …@v0.3.0` fica como alternativa. |
| D13 | A prova é feita pelo Codex dentro do sandbox, para validar a D9. |
| D14 | A instalação da skill a partir do `BasisTI/taiga-cli` é testada pelo Cedric com `npx skills add` depois do merge do PR 3. A estrutura `skills/<nome>/SKILL.md` é a mesma do `BasisTI/skills`; o teste faz parte do passo humano 2. |

Decisões tomadas durante a execução dos PRs 1 e 2 (origem: Cedric, 2026-10-04, via Assistente Global):

| PR | Decisão |
|---|---|
| #16 (#274) | **Opção B estendida a comentário e anexo.** `story comment`, `task comment` e `attachment upload` com resposta inconclusiva (rede depois de aberta a conexão, timeout, 5xx ou redirect 3xx) saem **sempre** com `comment_unconfirmed` ou `attachment_unconfirmed` (exit 1), nomeando as candidatas, e nunca com sucesso, mesmo que o item apareça na releitura. Muda o contrato anterior ("achou = sucesso") e passa a ser visível para quem automatiza. |
| #17 (#273) | **Completion: sobrescrever o escritor de debug em vez de filtrar.** Os scripts gerados por `taiga completion` redefinem o `__taiga_debug` do Cobra para não fazer nada, em vez de filtrar a linha digitada; o `BASH_COMP_DEBUG_FILE` recebe só o log do binário, já redigido. |

### Tabela aprovada (D5): `docs/examples/taiga-project.toml` × `configurar-taiga-projeto.sh`

Os dados são idênticos: 2 status (mesmos nomes e cores, `closed=false`) e 6 campos (mesmos nomes, tipos e descrições).

| # | Ponto | Script | CLI/TOML | Decisão |
|---|---|---|---|---|
| 1 | Âncora do `after` | slug (`in-progress`, `ready-for-test`) | nome (`In progress`, `Ready for test`) | aceitar |
| 2 | Comparação de nome | ignora maiúsculas | diferencia; nome igual só na caixa é drift | aceitar |
| 3 | Item existente com outros valores | mantém e segue | drift: o `plan` mostra, o `apply` recusa (exit 2) | aceitar |
| 4 | Âncora ausente | exit 4 | erro de uso, exit 2 | aceitar |
| 5 | Sem admin | exit 3 | `forbidden`, exit 6, também no `--dry-run` | aceitar |
| 6 | `is_archived` | `false` explícito | omitido (padrão do Taiga, `false`) | aceitar |
| 7 | Reordenação | sem conferência | relê antes e depois (`project_changed`, `status_order_postcondition_failed`) | aceitar |
| 8 | Credencial | `TAIGA_TOKEN` ou usuário/senha no env | sessão do `taiga auth login` | aceitar |
| 9 | Comentário do TOML | — | "Nomes, cores e descrições são exemplo…" | **mudar**: declarar que é a convenção canônica do fluxo da `basis-ci-gitlab` |

### Propostas ainda pendentes

- **Status da US de teste na prova.** A sequência exata (só In progress, ou passar também por In revision) fica com o executor; o mínimo é uma troca de status e os 3 campos de início.
- **Épicos desligados no projeto 37** (achado 2): conferir se o #242 é de fato épico do 37. Não foi discutido com o Cedric.
