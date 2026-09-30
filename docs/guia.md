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
