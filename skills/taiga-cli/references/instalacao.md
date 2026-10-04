# Instalação, login, Codex e keyring

Passos de uma **pessoa**, feitos uma vez por máquina. O agente não instala nada, não faz
login e não edita a config do Codex: quando faltar algum destes passos, ele para e pede.

## Binário em `~/.local/bin`

Os comandos usam a `v0.3.0`, que só existe depois de publicada a release; antes disso,
use a última tag de [Releases](https://github.com/BasisTI/taiga-cli/releases).

Caminho principal: o tar.gz da release, conferido pelo `SHA256SUMS`. Ajuste `P` para a
plataforma (`linux_amd64`, `linux_arm64`, `darwin_amd64` ou `darwin_arm64`). O bloco roda
num subshell com `set -eu`: qualquer passo que falhe (download, checksum ausente ou
`FAILED`, extração) interrompe antes de instalar, sem fechar o seu shell. No macOS, sem
`sha256sum`, ele usa `shasum -a 256`.

```sh
(
  set -eu
  V=0.3.0
  P=linux_amd64   # linux_arm64, darwin_amd64 ou darwin_arm64
  F="taiga_${V}_${P}.tar.gz"
  cd "$(mktemp -d)"
  gh release download "v$V" -R BasisTI/taiga-cli -p "$F" -p SHA256SUMS
  awk -v f="$F" '$2 == f' SHA256SUMS > checksum
  test -s checksum || { echo "$F is not in SHA256SUMS" >&2; exit 1; }
  if command -v sha256sum >/dev/null; then sha256sum -c checksum; else shasum -a 256 -c checksum; fi
  tar -xzf "$F" taiga
  mkdir -p "$HOME/.local/bin"
  install -m 0755 taiga "$HOME/.local/bin/taiga"
  "$HOME/.local/bin/taiga" version
)
```

Sem `gh`, baixe o tar.gz e o `SHA256SUMS` pela página de releases para um diretório
vazio, entre nele e rode o mesmo bloco sem as linhas do `cd` e do `gh`.

Alternativa, com Go instalado:

```sh
GOBIN="$HOME/.local/bin" go install github.com/BasisTI/taiga-cli/cmd/taiga@v0.3.0
```

`~/.local/bin` precisa estar no `PATH`.

## Skill

```sh
npx skills add BasisTI/taiga-cli --global   # instala esta skill para todos os projetos
npx skills update --global --yes            # a cópia instalada não se atualiza sozinha
```

## Login

```sh
taiga auth login --url https://agile.basis.com.br --username <conta>
taiga auth status --diagnose --output text
```

A senha vai para o keyring (Secret Service); `--secret-command "pass show taiga"` lê de um
gerenciador de senhas, e `--insecure-storage` grava num arquivo `0600`. A sessão fica no
state dir, `~/.local/state/taiga` por padrão (`$XDG_STATE_HOME/taiga` quando
`XDG_STATE_HOME` está definido). O refresh dura cerca de 8 dias: depois disso,
`taiga auth login` de novo; antes, `taiga auth refresh` renova.

## Codex

O agente no sandbox do Codex precisa de rede e de escrita no state dir, para renovar a
sessão. Em `~/.codex/config.toml`:

```toml
[sandbox_workspace_write]
network_access = true
writable_roots = ["/home/<usuário>/.local/state/taiga"]   # caminho absoluto do state dir
```

Sem o state dir gravável, a CLI usa a sessão enquanto ela vale e depois responde
`session_expired` ou `session_cache_readonly`; a recuperação é rodar `taiga auth refresh`
ou `taiga auth login` num terminal fora do Codex. O state dir fica no lugar padrão: um
`TAIGA_STATE_DIR` em `/tmp` ou no workspace separa a sessão do agente da sessão da pessoa.
`taiga auth status --diagnose` mostra no check `sandbox` se a rede está desligada
(`CODEX_SANDBOX_NETWORK_DISABLED=1`).

## Keyring em servidor sem desktop

Em Linux sem sessão gráfica, o keyring é o GNOME Keyring rodando só como Secret Service,
desbloqueado uma vez por boot antes de qualquer uso; o desbloqueio é o que inicia o
daemon. A receita completa (pacotes, `loginctl enable-linger`, o desbloqueio, a
conferência com `secret-tool` e a recuperação de `keyring_no_default`) está na seção
[Headless Linux keyring](https://github.com/BasisTI/taiga-cli/blob/main/README.md#headless-linux-keyring)
do README. Se desbloquear a cada boot não for aceitável, use `--secret-command`.

## Completion do shell

`taiga completion bash|zsh|fish|powershell` imprime o script; ele nunca chama o Taiga. O
script gerado não grava o debug do próprio shell: com `BASH_COMP_DEBUG_FILE` definida, o
arquivo recebe só o log do binário (linhas `[Debug]`, erros do completion inclusive),
com os valores de token escondidos. Um script salvo por uma versão anterior à `v0.3.0` ainda
grava a linha digitada, token incluído: gere-o de novo.
