---
name: taiga-cli
description: >-
  Ler e alterar o Taiga pela CLI `taiga`: story, task, comentário, anexo, status,
  swimlane, épico, campo customizado e configuração do board (`taiga project plan|apply`).
  Use quando alguém disser "atualiza a story", "muda o status", "comenta no card", "anexa
  o relatório", "cria a task", "vincula ao épico", "grava o campo", "prepara o board",
  "instalar a taiga-cli"; quando um comando `taiga` falhar com `*_unconfirmed`,
  `write_applied`, `*_postcondition_failed`, `session_expired`, `auth_rejected` ou
  `session_cache_readonly`; e quando a CLI e o MCP do Taiga estiverem disponíveis: a CLI
  tem precedência. Diz o que fazer depois de uma escrita incerta, sem repeti-la às cegas.
---

# Taiga pela CLI `taiga`

A `taiga` fala com a API v1 do Taiga em nome de uma sessão que **uma pessoa** abriu com
`taiga auth login`. O agente usa essa sessão pelo cache no state dir (que a própria CLI
renova quando ele é gravável) e nunca vê nem pede a senha.

Duas regras valem para tudo o que vem abaixo:

1. **Decida pelo `code` do erro, não pelo exit.** Sem terminal, todo erro sai no stderr como
   `{"error":{"code","source","stage","cause","recovery"}}`. O mesmo exit agrupa códigos
   opostos: o exit 4 é tanto `version_conflict` (nada gravado; reler e repetir é seguro)
   quanto `assignees_postcondition_failed` (gravado; repetir não é seguro). Leia `code`,
   `cause` e `recovery`; nos erros de autenticação, a regra desta skill vale mais que o
   `recovery`.
2. **Escrita com resultado incerto é tratada como gravada.** Antes de repetir qualquer
   escrita que falhou, confira o estado com o comando de leitura correspondente.

## Antes de começar

- **CLI antes do MCP.** Com a `taiga` instalada e o MCP do Taiga (`mcp__taiga__*`) também
  disponível, use a CLI e ignore o MCP. Os contratos de escrita diferem (por exemplo,
  `taiga_tasks_archive_or_close` acrescenta a tag `archived-by-mcp`, `taiga task close` só
  muda o status) e quem alterna entre eles perde a garantia de "não repetir às cegas".
  Avise o usuário se encontrar os dois.
- **Projeto.** `taiga project get --output text` mostra o projeto selecionado e de onde
  veio (`source`: `--project`, `TAIGA_PROJECT`, `.taiga.toml` ou config do usuário).
- **Diagnóstico.** `taiga auth status --diagnose` lista credencial, sessão, sandbox,
  permissões que faltam e módulos desligados (épicos). Rode-o antes de um trabalho longo e
  depois de qualquer erro de autenticação.
- **Saída.** Sem terminal no stdout a saída é JSON; force com `--output json|text`.
- **Instalação, Codex, keyring headless, completion:** CLI ausente ou pedido de instalar
  → abra [references/instalacao.md](references/instalacao.md).

## Comandos

Story e task são nomeadas pela **ref** (o número da interface web), nunca pelo id interno.
Status, milestone, swimlane, campo e usuário aceitam nome ou id; usuário aceita `me`.

```
taiga story list [--status S] [--assignee me] [--epic REF] [--search TEXT]; taiga story get REF
taiga story create --subject S [--status S] [--epic REF] ...
taiga story update REF --status S | --add-tag T | --add-assignee U | --milestone M
taiga story comment REF --body TEXTO         # ler: taiga story comments REF
taiga task create --story REF --subject S;  taiga task close REF
```

Anexo, campo customizado, épico, swimlane, `project plan|apply`, `taiga api` e as demais
flags: abra [references/comandos.md](references/comandos.md) antes de montar o comando
(exceto para executar um comando com as flags já escritas em `conferencia.md`; escrita
residual, como um `field set` ou `--unset`, exige `comandos.md` ou o `--help`).
Detalhe de flag: `taiga <comando> --help`.

- **`--dry-run` antes de escrita em lote.** Toda escrita curada aceita `--dry-run`, que
  imprime método, caminho e corpo sem enviar. Antes de um laço de escritas ou de um
  `project apply`, rode uma vez com `--dry-run` e confira o plano.
- **`project apply` exige admin** do projeto (`admin_project_values`) e nunca altera nem
  apaga uma definição existente.
- **`taiga api` não confere escritas.** Num `POST`/`PATCH`, exit 7 depois do envio também
  pode ter gravado: confira com um `GET` antes de repetir.

## Autenticação: parar e pedir à pessoa

Em todo erro de autenticação (exit 3: `auth_rejected`, `session_expired`,
`session_cache_readonly`, `auth_no_source`, `auth_untrusted_url`, `secret_*`, `keyring_*`,
`*_secret_*`, `password_file_*`), o agente para e pede à pessoa o comando de login ou de
refresh, para ela rodar num terminal fora do sandbox. O agente mexe só no comando que
falhou: credencial, keyring e configuração são da pessoa. Por isso ele nunca:

- roda `taiga auth login`, pede senha ou token;
- define `TAIGA_PASSWORD`, `TAIGA_PASSWORD_FILE` ou `TAIGA_TOKEN`, mesmo quando o
  `recovery` sugere uma dessas variáveis, e mesmo que haja uma senha à vista num arquivo;
- roda `secret-tool`, `gnome-keyring-daemon` ou edita `~/.codex/config.toml`, mesmo
  quando o `recovery` cita esses comandos: repasse a sugestão à pessoa;
- aponta `TAIGA_STATE_DIR` para outro lugar (como `/tmp` ou o workspace) para contornar o
  sandbox. O cache de sessão fica no state dir padrão, `~/.local/state/taiga`.

O que pedir: `session_expired` → `taiga auth refresh` (ou `taiga auth login`, se o refresh
também vencer); os demais → `taiga auth login --url https://<host> --username <conta>`
(a pessoa sabe a conta), repassando `cause` e `recovery` para ela decidir. Antes, em
`auth_rejected`, rode `taiga auth status --diagnose`. Em `session_cache_readonly` recorrente
no Codex, sugira incluir `~/.local/state/taiga` em `writable_roots`
([instalação](references/instalacao.md#codex)). Em `auth_untrusted_url` (URL só do
`.taiga.toml`), peça que ela confirme a URL na config dela.

Erro de autenticação quer dizer que o Taiga recusou o pedido, ou que ele nem saiu: depois
do login, repetir é seguro. Num `project apply` interrompido, o stdout lista o que já
estava em `applied`; o `apply` seguinte replaneja e não duplica.

## Gravado ou talvez gravado: conferir antes de repetir

Os códigos `*_unconfirmed`, `write_applied`, `*_postcondition_failed`, `*_link_failed`,
`project_apply_interrupted`, `project_changed` e `epic_replace_incomplete` querem dizer que
a escrita **foi aplicada** ou **pode** ter sido. A CLI nunca repete uma escrita sozinha; o
agente também não.

**Ao ver qualquer um deles, abra [references/conferencia.md](references/conferencia.md)
antes do próximo comando**: lá estão a conferência e o próximo passo de cada código. Leia
a `cause` (nomeia candidatas, ids e o estado encontrado), confira e repita só o que faltar.

- Criação de story, task, comentário e anexo com resposta inconclusiva sai **sempre** como
  não confirmada, mesmo quando o item aparece na releitura: nada prova que veio deste
  comando. "O item está lá" nunca autoriza criar outro; "não está lá" também não, porque o
  servidor pode gravar depois da conferência. Espere um pouco e confira de novo.
- Só `epic_link_unconfirmed` e `epic_replace_incomplete` convergem ao repetir o **mesmo
  comando**, e o segundo só quando o `recovery` não diz que o Taiga recusou o `DELETE`
  (sem `modify_epic`): nesse caso repetir não adianta; a referência diz o que fazer.

## Seguro repetir

- `version_conflict` (exit 4): nada foi gravado; releia e repita. `--force-version`
  sobrescreve a mudança de outra pessoa: use só quando a pessoa decidir isso.
- Exit 7 num comando curado (`network_error`, `server_error`, `unexpected_redirect`): foi
  leitura, ou a conexão nem abriu. Repetir é seguro. No `taiga api` não é.
- Erros de uso (exit 2), `not_found` (5) e `forbidden` (6): nada foi enviado ou o Taiga
  recusou. Corrija a causa antes de repetir. Exceção: num `project apply`, rode
  `taiga project plan -f ARQUIVO` antes do próximo `apply`.

Tabela completa de códigos, com `cause` e `recovery`:
[docs/errors.md](https://github.com/BasisTI/taiga-cli/blob/main/docs/errors.md).
