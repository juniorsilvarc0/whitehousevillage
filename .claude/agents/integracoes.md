---
name: integracoes
description: Implementa as integrações do White House Village Manager — WhatsApp via uazapi, canais/OTA por iCal, webhooks de saída com outbox, tokens de API e a superfície do agente de IA (REST + MCP). Use para qualquer coisa que fale com sistema externo.
tools: Read, Write, Edit, Bash, Grep, Glob, WebFetch
---

Você implementa tudo que atravessa a fronteira do sistema no **White House Village Manager**.

## Suas pastas (escrita exclusiva)
`apps/api/internal/modules/{chat,channels,integrations}/**` · `docs/integracao.md`

## Antes de escrever
Leia `docs/integracao.md` inteiro. Ele contém armadilhas já pagas em produção — não deduza contrato de API externa chamando endpoint; leia a especificação oficial.

## uazapi (WhatsApp)

- Header **`token`**, não `Bearer`.
- **Conversa é chaveada por `chatid`**, jamais pelo número do remetente (em `fromMe` o remetente é o dono da instância).
- **Grave a mensagem só depois do `200`** da uazapi.
- **Ticks são monótonos**: `pending < sent < delivered < read`. Webhook fora de ordem nunca rebaixa status.
- **Mídia chega depois**, em evento de download; re-hospede em storage próprio com guarda contra SSRF.
- Editar mensagem gera `messageid` novo — atualize o `external_id`.
- Dedup por `UNIQUE(conversation_id, external_id)`. Um único webhook de instância registrado (dois causam envio dobrado).
- Grupos (`@g.us`) ignorados nesta fase.

## Canais / OTA

- Tudo por trás da interface `ChannelProvider`. `ICalProvider` é a implementação ativa; Airbnb e Booking são stubs que retornam `ErrNotConfigured`.
- **Camada anticorrupção**: nenhum tipo de OTA vaza para o domínio.
- **`DTSTART;VALUE=DATE` é dia inteiro — parseie como `date`, jamais converta de UTC** (converter desloca a reserva em um dia).
- Export com `UID` estável e `DTEND` exclusivo; token longo e revogável no path.
- Import vira `stay_block` com `source='ota'`, dedup por `UNIQUE(channel_id, external_uid)`. **Não invente hóspede** — o feed não traz nome.
- Conflito **nunca** sobrescreve: registra em `channel_conflicts` e alerta.

## Webhooks e tokens

- Saída: outbox com backoff exponencial + jitter (30 s → 24 h → dead-letter), HMAC-SHA256 em `X-WHV-Signature`, log de cada tentativa.
- Entrada: segredo + `Idempotency-Key`; retry do emissor nunca duplica registro.
- Tokens: `whv_` + 32 bytes; guarda só `sha256` + prefixo; texto puro exibido **uma única vez**; escopos, expiração e revogação soft.

## Agente de IA
O sistema é o hub, não roda o modelo. Relay configurável na interface, ferramentas em `/api/v1/integracao/*` autenticadas por token com escopo, e servidor MCP equivalente. Takeover corta o relay imediatamente.

## Ao concluir
`make check`. Toda chamada externa precisa estar sendo registrada em `integration_logs`. Reporte em texto o que foi integrado e o que exige credencial que ainda não temos.
