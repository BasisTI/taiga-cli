# Validação da fase 2

Registro do que foi verificado na fase 2 (stories, responsáveis, campos, comentários e projeto como código). Os
contratos da API e suas evidências ficam em [api-notes.md](api-notes.md); aqui ficam a entrega de cada US e o que
continua pendente. Sem tokens nem dados de projetos reais: tudo foi verificado no Taiga local.

## Entregas

| US | Commit na `main` | Contratos (api-notes) |
|---|---|---|
| #246 stories | `e6491c7` | "stories (US #246)" |
| #247 responsáveis e bloqueio | `9f9bfcc` | "responsáveis e bloqueio (US #247)" |
| #248 campos customizados | `db19435` | "campos customizados (US #248)" |
| #250 comentários | `6ebdf64` | "comentários (US #250)" |
| #260 `--unset` em date/checkbox | `0d16d6e` | "Limpar valores: `null` (US #260)" |
| #249 status e campos como código | PR da branch `TG-249` | "status e projeto como código (US #249)" |

## Verificação da US #249 (2026-10-01)

Num Taiga local recém-criado (`docker compose -f compose.test.yml down -v`, `up -d`, `scripts/taiga-seed`;
`taigaio/taiga-back:6.7.3`):

| Comando | Resultado |
|---|---|
| `gofmt -l internal cmd` | sem saída |
| `go vet ./...` e `go vet -tags integration ./...` | sem problemas |
| `golangci-lint run ./...` (v2.14.0, também com `--build-tags integration`) | 0 issues |
| `go test -race ./...` | PASS |
| `go test -tags integration -p 1 ./...` | PASS, inclusive a fase 1 e as US anteriores da fase 2 |

Testes da US #249: `TestProbeStatusContract` (contrato e gate), `TestIntegrationProjectApply` (conta sem admin com e
sem `--dry-run`, dry-run, reorder, drift, ciclo, criação no fim, segunda aplicação sem escrita, status extra
preservado), `TestIntegrationProjectApplyStopsAndResumes` (falha na segunda criação, retomada sem duplicar),
`TestIntegrationProjectApplyConcurrentStatus` (status igual com outra cor criado na corrida: `project_changed`, o do
outro fica), `TestIntegrationProjectApplyConcurrentFieldCase` (campo em outra caixa criado na corrida),
`TestIntegrationProjectApplyBasisExample` (exemplo do fluxo Basis de ponta a ponta, com reordenação),
`TestIntegrationProjectReorderRefusesAMovedOrder` e `TestIntegrationProjectReorderPostcondition` (corridas antes e
depois da escrita da ordem) e os testes de unidade em `internal/app/project_*_test.go` e `internal/cli/project_test.go`.

## Pendências

- **Reordenação de status (US #249):** liberada por decisão humana em 2026-10-01 ("aceitar com conferência"), sem
  OCC: releitura antes e depois de um único `bulk_update_order`. Detecta parte das corridas, não impede (ver
  api-notes). OCC de verdade dependeria de mudança no servidor.
- **Convenção de cores e campos do fluxo Basis:** `docs/examples/taiga-project.toml` reproduz o
  `configurar-taiga-projeto.sh` como exemplo; confirmar a convenção antes de migrar o script (#254).
- Tipos de campo além de `text`, `date` e `checkbox`; valores de campos de task na CLI (#253); vínculo de épico.
