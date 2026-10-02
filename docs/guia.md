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
taiga swimlane list --output text
taiga story update 246 --swimlane Expedite
taiga story list --no-swimlane
taiga story update 247 --add-assignee me --add-assignee fulano --block "aguardando revisão da B6"
taiga story update 247 --remove-assignee fulano --unblock
taiga story close 246 --status Done
taiga story comment 246 --body-file nota.md
echo "Entregue em staging." | taiga story comment 246 --body-file -
taiga story comments 246 --output text
```

- Toda escrita aceita `--dry-run`, que mostra método, caminho e corpo sem gravar. O `update` envia só os campos que mudam, com o `version` lido; se outra pessoa mudou os mesmos campos no meio, a CLI para com `version_conflict` (exit 4).
- `--tag` substitui as tags; `--add-tag` e `--remove-tag` unem com as atuais. O Taiga grava tags em minúsculas.
- Responsáveis: `--add-assignee` e `--remove-assignee` (username exato, id ou `me`, sempre membro do projeto) unem com os responsáveis atuais, sem substituí-los, e não mexem no responsável principal (`assigned_to`). Para trocá-lo, `--owner-assignee`; para limpá-lo, `--clear-owner-assignee`. Como o Taiga sempre mostra o responsável principal entre os responsáveis, removê-lo exige um desses dois flags no mesmo comando; sem isso a CLI recusa (exit 2) em vez de gravar algo que o Taiga ignoraria. No `create`, `--assignee` (repetível) define os responsáveis iniciais.
- `--remove-assignee` aceita também quem já saiu do projeto. Se outra pessoa alterou a story entre a leitura e a gravação de responsáveis, a CLI para com `version_conflict` (exit 4) sem tentar de novo: rode outra vez para partir da leitura nova.
- O controle de concorrência do Taiga não cobre o responsável principal (`assigned_to`). Por isso a CLI relê os responsáveis logo antes de gravar e confere a story depois. Se o resultado não bate com o pedido, sai `assignees_postcondition_failed` (exit 4): a alteração **foi gravada**, então confira com `taiga story get` em vez de repetir o comando. Uma troca concorrente na janela curta entre essa releitura e a gravação é detectada, mas não evitada (detalhes em `docs/api-notes.md`).
- Bloqueio: `--block "nota"` bloqueia com a nota (obrigatória); `--unblock` desbloqueia e limpa a nota.
- `close` só muda o status para um status fechado: não arquiva nem exclui. No template padrão há dois status fechados (`Done` e `Archived`), então informe `--status`.
- Comentário: `story comment` publica o texto exatamente como veio (`--body`, ou `--body-file` com arquivo ou `-` para stdin), sem mexer na descrição; texto em branco é recusado. O Taiga aceita qualquer `version` antigo para comentário, então não há `--force-version` e o comentário é enviado uma vez só, nunca repetido. Se a resposta se perde (rede ou 5xx), a CLI procura o comentário no histórico: achou, sucesso; não achou ou não conseguiu ler, `comment_unconfirmed` (exit 1), porque o servidor pode gravar depois. Nesse caso, espere e confira com `taiga story comments` antes de publicar de novo. Exit 7 (`network_error`) só quando a conexão nem abriu: nada foi enviado.
- `story comments` lista do mais novo para o mais antigo, inclusive comentários editados e apagados (`edit_comment_date`, `delete_comment_date`). Os comentários automáticos da integração GitLab do Taiga (usuário de sistema `gitlab-<hash>`, inativo, com os textos do push hook) ficam ocultos; `--include-system` os mostra. Comentários da conta de integração dos agentes são humanos e sempre aparecem.
- Swimlanes: `taiga swimlane list` mostra as swimlanes do projeto na ordem do board, com `is_default` na padrão. `--swimlane` (nome ou id) move a story; `--clear-swimlane` tira a story de qualquer swimlane; `story list --swimlane A` e `story list --no-swimlane` filtram por elas. Story criada sem `--swimlane` fica sem swimlane, mesmo com uma padrão no projeto. Criar, renomear e reordenar swimlanes continua na interface web: exige admin, a primeira swimlane do projeto puxa todas as stories e a ordem não tem `version`.
- Vincular story a épico (`--epic`) ainda não é suportado: o Taiga faz o vínculo por um recurso à parte, sem `version`, e trocar o vínculo exige `DELETE`. Use a interface web por enquanto.

## Projetos, membros, milestones e épicos

Comandos só de leitura:

```sh
taiga project list --output text        # projetos de que a conta é membro; não precisa de projeto selecionado
taiga project get --output text         # o projeto selecionado e de onde ele veio (source: flag, env, .taiga.toml, config, ou argument)
taiga user list --search silva          # só membros do projeto, com is_admin e role_name
taiga milestone list --closed=false
taiga epic list --closed=false
taiga epic get 12 --output text         # descrição e stories vinculadas, na ordem do vínculo
```

- `--search` procura o texto nos nomes sem diferenciar maiúsculas, mas **sem** ignorar acento: `agil` não acha `Ágil`.
- `epic get` lista em `user_stories` as stories vinculadas, inclusive de outro projeto (com `project_slug`); uma story que a conta não pode ler aparece só com o `id`.
- Com o módulo de épicos desligado no projeto, `epic list` e `epic get` respondem `not_found` ("the epics module is disabled in this project"), embora a API do Taiga continue servindo os épicos.
- Links assinados de mídia (logo do projeto, foto de usuário) saem com o valor do token escondido, em qualquer texto, tags inclusive, e também quando a chave vem codificada (`%74oken=`, `t&#111;ken=`, `t&amp;#111;ken=`, `access_token=`) ou dentro do valor de outro parâmetro (`link=https://h/a?token=…`). Se o parâmetro de token só aparece depois de decodificar o texto e não dá para recortar o valor, ou se a decodificação de escapes `%` e entidades HTML ainda muda depois de 16 camadas, o texto inteiro vira `[redacted: the text carries a credential]`; texto que só menciona a palavra (`tokenizer=python`, `?q=token`) fica como está; e as credenciais do projeto (`*_csv_uuid`, que abre a exportação CSV sem autenticação, e `transfer_token`) nunca saem.

## Diagnóstico do projeto

`taiga auth status --diagnose` termina com o check `project`: lê o projeto selecionado (só leitura) e mostra `id`, `slug`, a origem da seleção, se a conta é membro e admin, as permissões que faltam entre as que os comandos usam (`view_us`, `modify_us`, `add_us`, `comment_us`, `view_tasks`, `add_task`, `modify_task`, `modify_epic`, `admin_project_values`), os módulos de épicos, kanban e backlog e o número de swimlanes. Fica `skipped` sem projeto selecionado ou quando a identidade (`users/me`) falhou, e `failed` quando o projeto não pode ser lido: não existe, a conta não o vê, ou outro erro (rede, servidor) interrompeu a leitura. Permissão faltando não reprova o check; só avisa: sem `modify_epic`, vincular épico vai dar `forbidden`; sem `admin_project_values`, `project apply` também.

## Fotos e outros links assinados

Toda saída dos comandos curados (todos menos `taiga api`, que mostra a resposta do Taiga como veio) esconde o valor dos parâmetros de token, mantendo as chaves e o resto da URL. As fotos de usuário (`photo` e `big_photo` em `owner_extra_info`, `assigned_to_extra_info` e no autor dos comentários), o logo do projeto e os links de anexo são URLs assinadas de mídia que abrem o arquivo sem autenticação enquanto o token vale. Em `story get`, a foto sai como `"photo": "https://…/media/user/…?token=…"`.

## Anexos

```sh
taiga attachment upload 246 relatorio.pdf --description "execução em staging"
taiga attachment list 246 --output text
taiga attachment download 246 3121 --to ~/Downloads/
taiga attachment upload 18 log.txt --task
taiga attachment download 18 3122 --task --to - | less
```

- Valem para story e, com `--task`, para task. Não há edição nem exclusão de anexo pela CLI.
- Idempotência pelo conteúdo: se a story (ou task) já tem um anexo com o mesmo nome e o mesmo `sha1`, `upload` não envia nada e devolve esse anexo com `"created": false`. Rodar o mesmo comando duas vezes não duplica o anexo; o mesmo conteúdo com outro nome é um anexo novo.
- O upload é enviado uma vez só, nunca repetido. A resposta é conferida (`sha1`, tamanho, objeto); se não bate, sai `attachment_postcondition_failed` (exit 4) e o anexo **foi gravado**. Se a resposta se perde (rede ou 5xx), a CLI procura na lista um anexo novo com o mesmo nome e `sha1`: achou, sucesso; não achou, `attachment_unconfirmed` (exit 1). Confira com `taiga attachment list` antes de enviar de novo.
- Nomes que o Taiga gravaria diferente (caracteres de controle, barra invertida, entidade HTML como `&amp;`, com ou sem `;`, ou qualquer `&#`) são recusados: renomeie o arquivo. A idempotência compara o nome gravado.
- A CLI não limita o tamanho. O proxy da Basis aceita até **50 MB** (`client_max_body_size 50M`); acima disso sai `payload_too_large` (exit 2). Arquivo vazio é recusado, porque o Taiga recusa.
- Por que o `url` não aparece: o Taiga devolve no anexo um `url` com token assinado que abre o arquivo **sem autenticação** por alguns minutos. A CLI nunca o imprime, nem no JSON. O caminho para o arquivo é `attachment download`, que lê um `url` novo, só baixa da mesma origem da URL do Taiga, sem `Authorization` e sem seguir redirect.
- `download` grava em `--to` (arquivo ou diretório existente), no stdout com `--to -`, ou no diretório atual. O nome vindo do servidor é tratado como não confiável: só o último componente, com controles, caracteres bidi e ponto inicial trocados por `_` (um anexo nunca vira arquivo oculto como `.bashrc`). Arquivo existente só é substituído com `--overwrite`. Os bytes vão para um temporário (0600) no mesmo diretório, que ao ser salvo recebe o umask do usuário, e só viram o destino depois de conferidos tamanho e `sha1`; se não batem, sai `attachment_download_mismatch` (exit 7) e nada é salvo.
- `--dry-run` mostra os campos e nome, tamanho e `sha1` do arquivo, nunca o conteúdo; em todo valor do plano (descrição, nome do arquivo) a query de URLs e os valores de `token=` aparecem ocultos. O envio real manda os valores como foram dados.
- `--to -`: os bytes vão direto para o stdout, e a conferência roda no fim. Se falhar, o erro sai no stderr com exit 7, e quem leu deve descartar o que recebeu. Nesse modo nenhum JSON de resultado é impresso.
- `--timeout` (padrão `10m`) limita a transferência inteira; as chamadas comuns da API continuam com 30 s. Interromper (Ctrl-C, SIGTERM) cancela de forma limpa: o download apaga o temporário, e o upload ainda confere a lista (com prazo próprio, mesmo depois do `--timeout`).
- Repetir o upload com outra `--description` devolve o anexo existente sem alterá-lo.

## Campos customizados

```sh
taiga field list --kind story
taiga field create --kind story --name "Testado em staging" --type checkbox
taiga field create --kind task --name "Horas" --type text --description "estimativa"
taiga story field list 246
taiga story field set 246 "Testado em staging=true" "Data de entrega=2026-10-15" "Notas=texto com = no meio"
taiga story field set 246 --unset "Testado em staging" --unset "Data de entrega"
```

- `field create` pode ser repetido: se já existe definição com o mesmo nome (sensível a maiúsculas) e o mesmo tipo, ela é devolvida sem alteração; com outro tipo, ou outra descrição quando `--description` é informado, sai `field_definition_conflict` (exit 2). A CLI nunca altera nem apaga definições.
- Tipos aceitos por enquanto: `text`, `date` (`AAAA-MM-DD`) e `checkbox` (`true`/`false`). O nome vai até o primeiro `=`; o texto `null` é gravado como texto.
- `--unset NOME` (repetível, nome ou id) limpa um campo `checkbox` ou `date`: o Taiga guarda `null` na chave do campo, que continua no dicionário mesmo quando é o último (o Taiga recusa dicionário vazio). Campo sem valor, ou já `null`, não gera escrita. Campo `text` não se limpa (erro de uso); para esvaziar, `"Notas="`. No JSON o valor limpo aparece como `null`; no texto, vazio, como campo sem valor.
- `story field set` junta os campos informados aos atuais e grava o dicionário inteiro com o `version` do recurso de valores, que é outro, independente do `version` da story. `story get` mostra os dois: o da story no topo e o dos valores em `custom_attributes`.
- O Taiga não recusa `version` antigo nos valores de campos customizados. A CLI confere a resposta: se outra gravação caiu junto da nossa, sai `field_values_postcondition_failed` (exit 4) e a alteração **foi gravada**; confira com `taiga story field list` antes de repetir.
- Campos de task: as definições já têm `field list/create --kind task`; o comando de valores de task vem com a #253.

## Status e campos do projeto como código

Substitui o `configurar-taiga-projeto.sh` da skill basis-ci-gitlab. O arquivo TOML declara status de story e campos
customizados de story; [docs/examples/taiga-project.toml](examples/taiga-project.toml) traz o equivalente ao script
(`In revision`, `Waiting for deployment` e os seis campos de registro). Nomes, cores e descrições do exemplo são
exemplo, não configuração embutida na CLI.

```sh
taiga status list --output text
taiga project plan -f taiga-project.toml          # só lê; qualquer membro roda
taiga project apply -f taiga-project.toml --dry-run
taiga project apply -f taiga-project.toml          # exige admin do projeto
```

- Só o que o arquivo declara é gerenciado, e nada é alterado nem apagado. O que existe e não está no arquivo aparece
  em `unmanaged` e fica como está. Declarado que já existe com outra cor, outro `closed`, outro tipo ou outra
  descrição — ou com nome igual a menos de maiúsculas — é `drift`: o `plan` mostra e o `apply` recusa
  (`definition_drift`, exit 2) antes de gravar. Quem decide é você: corrigir o arquivo ou o projeto.
- Nome é sensível a maiúsculas (o script comparava sem diferenciar; agora a diferença só de caixa vira drift, em vez
  de virar um segundo status).
- Status novo entra depois do último, na ordem do arquivo. `after` põe o status depois de outro (existente ou do
  próprio arquivo); ciclo, autorreferência e status inexistente são erro de uso. Quando o `after` exige mover status,
  o `apply` grava a ordem inteira no fim, numa única requisição `bulk_update_order` — é o caso do exemplo
  (`In revision` depois de `In progress`).
- **Reordenação conferida, não protegida.** O Taiga 6.7 não tem controle de concorrência na ordem dos status (não há
  `version`; ver `docs/api-notes.md`). Por decisão de 2026-10-01 ("aceitar com conferência"), o `apply` relê a ordem
  logo antes de gravar e recusa se algo mudou desde o plano (`project_changed`, exit 4, nada enviado); logo depois,
  relê e exige a ordem pretendida, ou sai `status_order_postcondition_failed` (exit 4): a ordem **foi gravada** e
  outra mudança caiu junto — confira com `taiga status list` em vez de repetir. Detecta parte das corridas, não
  impede: uma mudança entre a última leitura e a escrita pode ser sobrescrita. Nunca há repetição automática.
- Dois status ou dois campos do arquivo com nomes iguais a menos de maiúsculas são recusados antes de qualquer
  leitura. Antes de cada criação o catálogo é relido; status ou campo que apareceu com outros valores, ou com o nome
  em outra caixa, interrompe com `project_changed`.
- O `apply` confere `admin_project_values` antes de tudo, inclusive no `--dry-run` (`forbidden`, exit 6). A conta de
  serviço normalmente não tem: rode com a de um admin do projeto.
- Falha no meio: o resultado (`applied`, `remaining`, `complete: false`) sai no stdout e o erro no stderr; nada é
  desfeito nem repetido. Rodar de novo relê o projeto e continua sem duplicar. `complete: true` só depois de uma
  releitura sem pendências; se o projeto mudou durante o apply, sai `project_changed` (exit 4).

## `taiga api` para o que ainda não tem comando próprio

Enquanto os comandos curados das próximas fases não chegam, `taiga api` cobre as receitas que hoje estão na skill `basis-ci-gitlab`. O caminho é relativo a `/api/v1/`, e `--auto-version` lê o `version` atual antes de gravar.

```sh
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
