# Guia de uso da taiga-cli na Basis

Guia curto para a equipe. A referência completa está no [README](../README.md) (em inglês) e os códigos de erro em [errors.md](errors.md).

## Login com a conta de serviço

Em cada máquina onde agentes vão usar o Taiga, uma pessoa faz o login uma vez com a conta de serviço:

```sh
taiga auth login --url https://agile.basis.com.br --username <conta-de-serviço>
taiga auth status
```

A senha vai para o keyring (Secret Service). Em servidor sem desktop, prepare antes o keyring conforme a seção "Headless Linux keyring" do README. Alternativas:

- `--secret-command "pass show taiga/servico"`: a senha é lida de um gerenciador de senhas a cada login;
- `--insecure-storage`: grava a senha num arquivo `0600`. Use só quando nenhuma das outras opções servir.

O refresh da sessão dura cerca de 8 dias (ver [api-notes.md](api-notes.md)). Depois disso, repita `taiga auth login`, ou rode `taiga auth refresh` antes do vencimento.

## `.taiga.toml` nos repositórios

Na raiz do repositório:

```toml
url = "https://agile.basis.com.br"
project = "<slug-do-projeto>"
```

A CLI procura o arquivo subindo a partir do diretório atual. Flags (`--url`, `--project`) e variáveis (`TAIGA_URL`, `TAIGA_PROJECT`) têm precedência. `TAIGA_TOKEN` e `TAIGA_PASSWORD` só são enviados a uma URL que venha de flag, env ou da config do usuário; se a URL vier só do `.taiga.toml`, a CLI recusa com `auth_untrusted_url`.

## Stories

Com o projeto no `.taiga.toml`, as stories são tratadas pela ref, o número que aparece na interface web:

```sh
taiga story list --assignee me --closed=false
taiga story get 246
taiga story create --subject "Nova story" --tag cli --description-file descricao.md
taiga story update 246 --status "In progress" --add-tag cli --milestone "Sprint 12"
taiga story update 246 --append-description "Entregue em staging."
taiga story update 247 --add-assignee me --add-assignee fulano --block "aguardando revisão da B6"
taiga story update 247 --remove-assignee fulano --unblock
taiga story close 246 --status Done
```

- Toda escrita aceita `--dry-run`, que mostra método, caminho e corpo sem gravar. O `update` envia só os campos que mudam, com o `version` lido; se outra pessoa mudou os mesmos campos no meio, a CLI para com `version_conflict` (exit 4).
- `--tag` substitui as tags; `--add-tag` e `--remove-tag` unem com as atuais. O Taiga grava tags em minúsculas.
- Responsáveis: `--add-assignee` e `--remove-assignee` (username exato, id ou `me`, sempre membro do projeto) unem com os responsáveis atuais, sem substituí-los, e não mexem no responsável principal (`assigned_to`). Para trocá-lo, `--owner-assignee`; para limpá-lo, `--clear-owner-assignee`. Como o Taiga sempre mostra o responsável principal entre os responsáveis, removê-lo exige um desses dois flags no mesmo comando; sem isso a CLI recusa (exit 2) em vez de gravar algo que o Taiga ignoraria. No `create`, `--assignee` (repetível) define os responsáveis iniciais.
- `--remove-assignee` aceita também quem já saiu do projeto. Se outra pessoa alterou a story entre a leitura e a gravação de responsáveis, a CLI para com `version_conflict` (exit 4) sem tentar de novo: rode outra vez para partir da leitura nova.
- O controle de concorrência do Taiga não cobre o responsável principal (`assigned_to`). Por isso a CLI relê os responsáveis logo antes de gravar e confere a story depois. Se o resultado não bate com o pedido, sai `assignees_postcondition_failed` (exit 4): a alteração **foi gravada**, então confira com `taiga story get` em vez de repetir o comando. Uma troca concorrente na janela curta entre essa releitura e a gravação é detectada, mas não evitada (detalhes em `docs/api-notes.md`).
- Bloqueio: `--block "nota"` bloqueia com a nota (obrigatória); `--unblock` desbloqueia e limpa a nota.
- `close` só muda o status para um status fechado: não arquiva nem exclui. No template padrão há dois status fechados (`Done` e `Archived`), então informe `--status`.
- Vincular story a épico (`--epic`) ainda não é suportado: o Taiga faz o vínculo por um recurso à parte, sem `version`, e trocar o vínculo exige `DELETE`. Use a interface web por enquanto.

## `taiga api` para o que ainda não tem comando próprio

Enquanto os comandos curados das próximas fases não chegam, `taiga api` cobre as receitas que hoje estão na skill `basis-ci-gitlab`. O caminho é relativo a `/api/v1/`, e `--auto-version` lê o `version` atual antes de gravar.

```sh
# comentário na story (o texto vai para a API como JSON válido, com acentos e quebras de linha)
taiga api PATCH userstories/<id> --raw-field comment="$(cat nota.md)" --auto-version

# bloquear com motivo
taiga api PATCH userstories/<id> --field is_blocked=true --raw-field blocked_note="aguarda B6" --auto-version

# campos customizados: ler e gravar o dicionário inteiro
taiga api GET userstories/custom-attributes-values/<id>
taiga api PATCH userstories/custom-attributes-values/<id> --input valores.json --auto-version
```

`--field` manda o valor como JSON quando ele é JSON válido (`true`, `false`, `null`, números, listas, objetos) e como texto nos demais casos; `--raw-field` manda sempre texto. Para conferir sem gravar, use `--dry-run`. `DELETE` exige `--confirm-delete`.

## No Codex

- Ative a rede e libere o state dir no `~/.codex/config.toml`:

  ```toml
  [sandbox_workspace_write]
  network_access = true
  writable_roots = ["/home/<usuário>/.local/state/taiga"]
  ```

- Sem o state dir gravável, a CLI usa a sessão enquanto ela for válida. Quando vence, responde `session_expired` com a recuperação "run `taiga auth refresh` outside the sandbox". Rode `taiga auth refresh` num terminal fora do Codex e repita o comando. A CLI não gasta o refresh guardado quando não consegue gravar o novo, porque o Taiga invalida o anterior.
- Com o state dir somente leitura, a CLI nunca usa o keyring, o `secret_command` nem o arquivo do `--insecure-storage`, haja sessão ou não. Só `TAIGA_PASSWORD`/`TAIGA_PASSWORD_FILE` fazem login, com o token mantido em memória. Sem sessão e sem senha no env, o erro é `session_cache_readonly`: rode `taiga auth login` fora do Codex.
- Com `CODEX_SANDBOX_NETWORK_DISABLED=1`, nem rede nem D-Bus funcionam. `taiga auth status --diagnose` mostra isso no check `sandbox`.
- Em caso de dúvida, `taiga auth status --diagnose` testa cada fonte de credencial, o cache de sessão, o sandbox, o D-Bus e o TTY.
