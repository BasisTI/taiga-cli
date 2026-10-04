# Instalação, login, Codex e keyring

Passos de uma **pessoa**, feitos uma vez por máquina. O agente não instala nada, não faz
login e não edita a config do Codex: quando faltar algum destes passos, ele para e pede.

## Binário em `~/.local/bin`

Caminho principal: o tar.gz da release, conferido pelo `SHA256SUMS`. Exemplo para Linux
amd64 (troque `linux_amd64` por `linux_arm64`, `darwin_amd64` ou `darwin_arm64`):

```sh
V=0.3.0
cd "$(mktemp -d)"
gh release download "v$V" -R BasisTI/taiga-cli -p "taiga_${V}_linux_amd64.tar.gz" -p SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing      # macOS: grep "taiga_${V}_darwin_arm64" SHA256SUMS | shasum -a 256 -c
tar -xzf "taiga_${V}_linux_amd64.tar.gz" taiga
mkdir -p ~/.local/bin && install -m 0755 taiga ~/.local/bin/taiga
taiga version
```

Sem `gh`, baixe os dois arquivos pela página
[Releases](https://github.com/BasisTI/taiga-cli/releases) e siga a partir do `sha256sum`.
Uma linha `FAILED` no `sha256sum` encerra a instalação: não use o binário.

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
