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
| `userstories/by_ref?ref=&project=` | responde 200 numa sondagem manual da US #245; a validação formal fica para a fase 2 (US #246) | — |
| Escrita de swimlane, upload de anexo, comentários no histórico | fases 2 e 3 | — |
