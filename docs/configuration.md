# Configuração local

`chatwoot-mcp` usa um arquivo local compartilhado pelos comandos `panel` e `mcp`. O painel é a forma recomendada de definir e validar os dados de conexão.

## Primeira configuração

1. Compile o executável com Go 1.25 ou posterior:

   ```sh
   go build -o ./bin/chatwoot-mcp ./cmd/chatwoot-mcp
   ```

2. Abra o painel:

   ```sh
   ./bin/chatwoot-mcp panel
   ```

3. Informe a URL base da instalação Chatwoot, o ID numérico da conta e um token pessoal de usuário. Use HTTPS para instalações normais. HTTP é permitido apenas para desenvolvimento em endereço loopback.
4. Salve depois que o teste de conexão for aprovado.
5. Configure o cliente MCP local para iniciar o mesmo executável com o argumento `mcp` e reinicie o cliente. Exemplos estão no [README](../README.md#conectar-um-cliente-mcp-local).

O painel inicial contém conexão, estado da conta, configuração do cliente local e catálogo das ferramentas. Ele não substitui a caixa de entrada do Chatwoot. O painel deve permanecer acessível apenas em `127.0.0.1`; não o exponha por proxy ou túnel de rede.

## Onde os dados ficam

Por padrão, a configuração fica em `chatwoot-mcp/config.json` dentro do diretório de configuração do usuário retornado pelo sistema operacional (em Unix, normalmente sob `~/.config`). O arquivo contém URL base, `account_id` e token. Em Unix, o diretório é criado com modo restrito e o arquivo usa permissão `0600`; o aplicativo rejeita arquivo existente com permissões mais abertas e não aceita arquivo de configuração que seja link simbólico.

O token nunca é devolvido pelo painel, incluído nos exemplos MCP nem mostrado em mensagens de erro. O armazenamento é local ao usuário do sistema operacional; proteja também a conta do sistema e os backups desse diretório.

## Rotação do token

1. Crie ou selecione o novo token pessoal na instalação Chatwoot, com as permissões necessárias.
2. No painel local, substitua o valor do token e salve após o teste de conexão.
3. Reinicie ou recarregue os clientes MCP conectados para que o processo `chatwoot-mcp mcp` releia a configuração.
4. Revogue o token anterior no Chatwoot e confirme a conexão com o novo token.

Se a credencial tiver sido exposta, revogue-a no Chatwoot imediatamente e repita a configuração com um token novo. Não tente corrigir a exposição apenas apagando o valor de um arquivo ou log.

## Escopo e limites

Uma configuração representa uma instalação Chatwoot e um `account_id`. O token determina as contas, caixas de entrada e operações permitidas. Todas as caixas de entrada visíveis ao usuário podem aparecer, mas a capacidade de responder depende do canal e do estado da conversa.

`send_reply`, `send_attachment`, `send_template` e `create_conversation` fazem uma única tentativa. Se houver timeout ou falha de rede sem confirmação, o resultado pode ser incerto: confira a conversa no Chatwoot antes de repetir para evitar duplicidade. Uma resposta de sucesso informa aceitação pela API e os dados retornados pelo Chatwoot, não a entrega ao cliente.

## Clientes MCP

O transporte local usa `stdio`. O cliente deve iniciar o executável com o argumento `mcp`, usando um caminho absoluto e estável. O bloco do cliente não contém o token: o processo lê o mesmo arquivo usado pelo painel. Consulte os exemplos para Claude Desktop e Codex no [README](../README.md#conectar-um-cliente-mcp-local).

## Ferramentas disponíveis

O servidor MCP expõe vinte e duas ferramentas, agrupadas por finalidade:

| Grupo | Ferramentas |
|---|---|
| Consulta e resposta | `check_connection`, `list_conversations`, `get_conversation`, `search_contacts`, `get_contact_conversations`, `send_reply` |
| Conversa | `add_private_note`, `set_conversation_status`, `set_priority` |
| Organização e roteamento | `list_inboxes`, `list_agents`, `list_teams`, `assign_conversation`, `get_conversation_labels`, `add_conversation_labels`, `remove_conversation_labels` |
| Mensagens ricas | `send_attachment`, `list_message_templates`, `send_template` |
| Contatos e novas conversas | `get_contact`, `update_contact`, `create_conversation` |

Mensagens de clientes são dados não confiáveis. A leitura de uma conversa não envia mensagens nem altera estado; cada mutação exige uma chamada explícita com ID positivo e devolve o estado informado pelo Chatwoot.

### Restrições por canal

- `send_reply` e `send_attachment` respeitam `can_reply`, que reflete a janela de mensagens do canal (por exemplo, 24 horas no WhatsApp). Fora da janela, use um modelo aprovado com `send_template`.
- `send_attachment` aceita apenas caminho absoluto de arquivo local; URL remota, caminho relativo e diretório são recusados. Tamanho e MIME são verificados antes do upload e cada canal tem seus limites.
- `list_message_templates` e `send_template` funcionam somente em caixas do WhatsApp (nativo ou Twilio WhatsApp). Em outros canais retornam um erro específico.
- `create_conversation` só funciona nos canais que permitem iniciação (Website, API, Email e SMS/Phone). Nos canais em que o contato precisa escrever primeiro, retorna erro específico.

### Dependências de versão

Os endpoints usados são os da Application API documentada. `list_message_templates` depende de uma versão que exponha `GET /inboxes/{id}/message_templates`, e `send_template` depende do campo `template_params` em `POST /conversations/{id}/messages`. Em versões anteriores, a operação retorna erro em vez de presumir sucesso.
