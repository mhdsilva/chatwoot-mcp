# Chatwoot MCP — analytics e fila de atenção

## Objetivo

Permitir que uma pessoa pergunte ao agente sobre atrasos, tempo de resposta,
tempo de resolução, volume e desempenho operacional sem obrigar o agente a
ler conversa por conversa ou reconstruir métricas que o próprio Chatwoot já
calcula.

A primeira entrega deve responder com dados estruturados a quatro tarefas:

1. quais conversas estão esperando atendimento há mais tempo;
2. como a operação se comportou em um período;
3. quais tempos foram registrados em uma conversa específica;
4. como agentes, equipes, caixas e canais se comparam entre dois períodos.

O agente interpreta os resultados e produz os insights em linguagem natural.
O MCP coleta, valida, limita e identifica a completude dos dados; ele não gera
uma narrativa fixa nem mantém um banco analítico próprio.

## Escopo

### Incluído

- fila atual de conversas aguardando resposta;
- médias de primeira resposta e resolução;
- volumes de conversas, mensagens e resoluções;
- eventos oficiais de primeira resposta, resposta e resolução por conversa;
- valores corridos e em horário comercial, quando fornecidos pelo Chatwoot;
- comparação por agente, equipe, caixa de entrada e tipo de canal;
- resumo de um único escopo de conta, agente, equipe, caixa ou etiqueta;
- comparação automática com o período imediatamente anterior de igual duração;
- resultados limitados, indicação de truncamento e indicação explícita de
  varredura parcial.

### Não incluído

- armazenamento local, data warehouse ou sincronização por webhook;
- alertas agendados ou notificações automáticas;
- dashboards no painel local;
- CSAT, análise de sentimento ou classificação do conteúdo das mensagens;
- comparação agrupada por etiqueta; uma etiqueta individual continua aceita
  como escopo do resumo;
- estimativas por mensagens quando uma rota oficial de relatório recusar a
  requisição;
- alteração de conversa como efeito colateral de uma consulta analítica.

## Princípios do desenho

1. **Métrica oficial antes de reconstrução local.** Primeira resposta,
   resolução e tempos de resposta vêm das rotas de relatórios do Chatwoot.
2. **Fila em tempo real a partir da conversa.** A fila usa `waiting_since` e
   metadados da listagem de conversas, pois não é uma série histórica.
3. **Interface pequena e profunda.** Quatro ferramentas representam tarefas
   do usuário e escondem paginação, autenticação, normalização de datas,
   agregação e compatibilidade.
4. **Parcial nunca parece completo.** Toda varredura limitada informa se
   alcançou o fim dos dados e como continuar.
5. **Permissões não são contornadas.** Um `403` é devolvido como `forbidden`;
   o MCP não substitui a métrica por uma aproximação menos confiável.
6. **Somente leitura.** As novas operações usam `GET` e não ampliam autoridade
   para responder, atribuir ou modificar conversas.

## Ferramentas MCP

### `list_attention_queue`

Lista conversas que estão aguardando ação do atendimento, ordenadas por
`waiting_since` crescente — a espera mais antiga primeiro.

Entrada:

```json
{
  "status": "open",
  "inbox_id": 0,
  "team_id": 0,
  "assignee_id": 0,
  "limit": 20,
  "start_page": 1
}
```

- `status` é opcional, aceita `open` ou `pending` e usa `open` por padrão;
- IDs iguais a zero não filtram aquele campo;
- `limit` usa 20 por padrão e no máximo 50;
- `start_page` usa 1 por padrão e permite continuar uma varredura parcial;
- cada chamada lê no máximo 10 páginas da API, atualmente 250 conversas.

Saída:

```json
{
  "observed_at": "2026-10-08T15:00:00Z",
  "conversations": [
    {
      "conversation_id": 123,
      "contact_id": 45,
      "inbox_id": 2,
      "channel_type": "Channel::Whatsapp",
      "status": "open",
      "priority": "high",
      "assignee_id": 8,
      "team_id": 3,
      "waiting_since": "2026-10-08T13:30:00Z",
      "waiting_seconds": 5400,
      "last_activity_at": "2026-10-08T13:30:00Z",
      "unread_count": 2
    }
  ],
  "scanned_pages": 3,
  "scanned_conversations": 61,
  "complete": true,
  "next_page": 0,
  "truncated": false
}
```

Somente itens com `waiting_since` positivo entram na fila. O tempo de espera é
calculado em relação a um único `observed_at`, capturado no início da chamada.
Empates são ordenados por `conversation_id`, tornando o resultado
determinístico. `truncated` indica que existem mais itens elegíveis dentro da
varredura; `complete=false` indica que a varredura não alcançou a última página
da conta. Quando incompleto, `next_page` permite continuar, mas cada lote deve
ser entendido como parcial — ele não promete conter as conversas mais antigas
de páginas que ainda não foram lidas.

O resultado não inclui conteúdo de mensagem. Campos textuais originados da
conversa continuam sendo dados não confiáveis e sujeitos aos limites do MCP.

### `get_analytics_summary`

Devolve os principais indicadores para um intervalo e escopo.

Entrada:

```json
{
  "since": "2026-10-01T00:00:00-03:00",
  "until": "2026-10-08T00:00:00-03:00",
  "scope": "account",
  "scope_id": 0
}
```

- `since` e `until` são RFC 3339 com fuso explícito;
- `since` deve ser anterior a `until`;
- o intervalo máximo é 183 dias;
- `scope` aceita `account`, `agent`, `inbox`, `team` ou `label`;
- `scope_id` deve ser positivo para qualquer escopo diferente de `account` e
  deve ser zero para `account`.

Saída:

```json
{
  "since": "2026-10-01T03:00:00Z",
  "until": "2026-10-08T03:00:00Z",
  "scope": "account",
  "scope_id": 0,
  "current": {
    "conversations_count": 120,
    "incoming_messages_count": 480,
    "outgoing_messages_count": 510,
    "resolutions_count": 105,
    "avg_first_response_seconds": 92.4,
    "avg_resolution_seconds": 3800.2
  },
  "previous": {
    "conversations_count": 110,
    "incoming_messages_count": 450,
    "outgoing_messages_count": 470,
    "resolutions_count": 98,
    "avg_first_response_seconds": 101.7,
    "avg_resolution_seconds": 4020.1
  }
}
```

O módulo converte strings numéricas documentadas pelo Chatwoot em números. Um
campo ausente ou nulo permanece ausente; zero continua sendo um valor real. O
MCP não fabrica denominadores nem percentuais nesse resumo.

### `get_conversation_metrics`

Devolve os eventos de relatório oficiais de uma conversa e um resumo derivado
desses eventos.

Entrada:

```json
{"conversation_id": 123}
```

Saída:

```json
{
  "conversation_id": 123,
  "summary": {
    "first_response_seconds": 80,
    "first_response_business_seconds": 55,
    "resolution_seconds": 3600,
    "resolution_business_seconds": 2400,
    "reply_events": 4,
    "avg_reply_seconds": 140
  },
  "events": [
    {
      "id": 10,
      "name": "first_response",
      "value_seconds": 80,
      "business_value_seconds": 55,
      "event_start_time": "2026-10-08T12:00:00Z",
      "event_end_time": "2026-10-08T12:01:20Z",
      "inbox_id": 2,
      "user_id": 8
    }
  ],
  "total_events": 6,
  "returned_events": 6,
  "truncated": false
}
```

O serviço preserva a ordem cronológica retornada pela API. O resumo usa o
primeiro evento `first_response`, o último evento `resolution` e a média dos
eventos `reply_time`. Campos permanecem ausentes quando o evento correspondente
não existe. No máximo 50 eventos são expostos; se houver mais, são mantidos os
50 mais recentes e `truncated=true`, mas o resumo é calculado sobre todos os
eventos recebidos.

### `compare_performance`

Compara entidades em um intervalo com o período imediatamente anterior de
igual duração.

Entrada:

```json
{
  "since": "2026-10-01T00:00:00-03:00",
  "until": "2026-10-08T00:00:00-03:00",
  "group_by": "agent",
  "limit": 20
}
```

- as regras de data são iguais às do resumo;
- `group_by` aceita `agent`, `team`, `inbox` ou `channel`;
- `limit` usa 20 por padrão e no máximo 50.

O período anterior termina em `since` e tem a mesma duração do período atual.
A saída ecoa os dois intervalos e contém linhas ordenadas por identificador
estável. Cada linha traz nome/ID quando a rota os fornecer, métricas atuais,
métricas anteriores, delta absoluto e delta percentual. Delta percentual fica
ausente quando o valor anterior for zero ou ausente. Métricas não disponíveis
para um agrupamento ficam ausentes, não iguais a zero. Por exemplo, a rota por
canal oferece contagens por estado, mas não promete tempos médios.

Resultados acima de `limit` são cortados com `truncated=true` e
`total_rows`/`returned_rows`. A ferramenta falha por inteiro se uma das duas
consultas falhar; ela nunca combina um período real com outro vazio.

## Arquitetura e seams

### Tipos de domínio

Um novo arquivo `internal/core/reporting.go` define:

- intervalos e escopos de relatório;
- valores opcionais para distinguir ausência de zero;
- eventos de conversa;
- resumos e linhas agrupadas;
- a interface pequena `ReportsAPI`, contendo somente leituras nativas de
  relatórios do Chatwoot.

`core.API` continua sendo a interface das operações de atendimento e não
recebe os métodos históricos. O tipo `core.Conversation` ganha apenas os
metadados necessários à fila: `WaitingSince`, `LastActivityAt` e
`UnreadCount`. A listagem existente continua compatível porque os campos são
opcionais no JSON.

### Adapter Chatwoot

`internal/chatwoot/reports.go` implementa `core.ReportsAPI` e concentra:

- caminhos `/api/v2/.../reports`, `/summary` e `/summary_reports/...`;
- `/api/v1/.../conversations/{id}/reporting_events`;
- parâmetros Unix derivados dos intervalos RFC 3339;
- conversão tolerante de números JSON ou strings numéricas;
- decodificação de campos nulos;
- limite de corpo, classificação de erro e proteção de redirecionamento já
  existentes no cliente.

As rotas de relatórios enviam o token existente tanto em `api_access_token`
quanto em `Authorization: Bearer`, pois a documentação oficial mistura o
formato legado com o formato disponível a partir do Chatwoot 4.19. Os dois
cabeçalhos carregam a mesma credencial, são enviados somente ao host base já
validado e nunca aparecem em resultados ou logs. Não existe retry de
autenticação nem fallback para outro host.

A listagem de conversas continua no adapter atual e passa a decodificar os
campos da fila. Filtros suportados nativamente são enviados à API; filtros que
a rota não suporta são aplicados pelo módulo de analytics e refletidos na
completude da varredura.

### Módulo de analytics

Um novo pacote `internal/analytics` oferece uma interface `Service` com os
quatro métodos correspondentes às ferramentas. Ele depende de duas interfaces
estreitas:

- uma leitura de páginas de conversa para a fila;
- `core.ReportsAPI` para métricas históricas.

O módulo valida entradas, captura o relógio injetado, controla paginação,
ordena, limita, agrega eventos e calcula deltas. Testes usam fakes nessas duas
interfaces e um relógio fixo. Nenhum cálculo ou regra de paginação fica no
adaptador MCP.

### Adapter MCP e composição

`internal/mcpserver/analytics_tools.go` registra as quatro ferramentas e
projeta os resultados do módulo para envelopes MCP limitados. A construção do
servidor recebe o serviço operacional existente e o novo serviço analítico.
O ponto de composição em `cmd`/`internal/app` reutiliza a mesma instância do
cliente Chatwoot como adapter de operações e de relatórios.

O painel local apenas acrescenta as quatro descrições ao catálogo. Ele não
executa consultas analíticas nem exibe resultados.

## Fontes de dados

| Capacidade | Rota Chatwoot |
|---|---|
| Fila de atenção | `GET /api/v1/accounts/{account_id}/conversations` |
| Resumo | `GET /api/v2/accounts/{account_id}/reports/summary` |
| Eventos de conversa | `GET /api/v1/accounts/{account_id}/conversations/{conversation_id}/reporting_events` |
| Comparação | `GET /api/v2/accounts/{account_id}/summary_reports/{agent|team|inbox|channel}` |

As rotas agrupadas por canal são documentadas para Chatwoot 4.10 ou posterior.
Outras rotas de relatórios variam entre versões e permissões. `404` em uma rota
de relatório sem identificador de recurso é classificado como
`unsupported_feature`; `404` na conversa continua `not_found`. A resposta de
recurso não suportado orienta a verificar a versão do Chatwoot sem expor corpo
de resposta.

## Erros e segurança

- datas inválidas, intervalo invertido, intervalo acima de 183 dias, enum
  desconhecido e combinação inválida de escopo/ID retornam `invalid_input`
  antes de qualquer chamada externa;
- `401`, `403`, `429`, timeout, `5xx` e JSON inválido seguem as categorias já
  existentes;
- rotas analíticas ausentes retornam `unsupported_feature`;
- respostas não ecoam corpo upstream, cabeçalhos ou token;
- todas as listas e textos continuam sujeitos aos limites do MCP;
- nenhum conteúdo de mensagem é necessário para os relatórios;
- nomes de contato, agente, equipe, inbox, canal ou etiqueta são tratados como
  dados não confiáveis e nunca como instruções.

## Estratégia de testes

### Adapter Chatwoot

- caminho, query e os dois cabeçalhos de autenticação por rota;
- resposta normal, vazia, nula, número e string numérica;
- decodificação dos campos de fila;
- eventos de conversa em ordem;
- variantes de agrupamento;
- `401`, `403`, `404`, `429`, `5xx`, timeout, redirecionamento, corpo grande e
  JSON malformado;
- nenhuma mensagem de erro contém token ou corpo upstream.

### Módulo de analytics

- validação ocorre antes de acessar dependências;
- relógio fixo produz `waiting_seconds` determinístico;
- filtros, ordenação, empates, limites e continuação de página;
- distinção entre `truncated` e `complete`;
- ausência, zero e média de eventos;
- períodos anterior e atual possuem duração idêntica;
- deltas com baseline zero não geram infinito ou percentual enganoso;
- falha em qualquer período aborta a comparação inteira.

### Adapter MCP e composição

- registro exato das quatro ferramentas e campos obrigatórios;
- chamadas por sessão MCP com resultados estruturados;
- limites de lista e texto, truncamento e completude;
- códigos estáveis para validação, permissão e versão não suportada;
- catálogo do painel e construção da aplicação incluem o módulo;
- suíte completa `go test ./...`, `go vet ./...` e `gofmt -l .` limpa.

## Compatibilidade e validação em instalação real

A implementação é testada com respostas gravadas e servidor HTTP local, mas
deve ser validada em pelo menos uma instalação real antes de publicação. A
validação confirma:

- versão do Chatwoot;
- aceitação dos cabeçalhos nas quatro famílias de rota;
- unidade e nulabilidade de `waiting_since`;
- tamanho e metadados reais da paginação de conversas;
- disponibilidade dos relatórios para administrador e agente comum;
- semântica de horário comercial;
- formatos reais dos agrupamentos por agente, equipe, inbox e canal.

Uma incompatibilidade real deve produzir erro explícito e teste de regressão;
não deve ser escondida por dados estimados.

## Critérios de conclusão

1. Um agente identifica as conversas aguardando há mais tempo e sabe se a
   varredura cobriu toda a conta.
2. Um agente obtém resumo de período e período anterior para conta ou escopo.
3. Um agente inspeciona primeira resposta, respostas e resolução de uma
   conversa usando eventos oficiais.
4. Um agente compara agentes, equipes, caixas ou canais entre períodos sem
   confundir ausência com zero.
5. Falta de permissão, versão incompatível, limite e truncamento são explícitos.
6. Nenhuma consulta modifica o Chatwoot ou persiste conteúdo localmente.
7. A suíte automatizada e a validação manual contra uma instalação real passam.
