# Integrações

## 1. WhatsApp — uazapi

Provedor principal de atendimento. A API usa header **`token`** (não `Bearer`).

### Fluxo

```
Hóspede ──► WhatsApp ──► uazapi ──► POST /api/v1/chat/webhook/uazapi
                                      ├─ upsert do contato (por telefone E.164)
                                      ├─ upsert da conversa (por chatid)
                                      ├─ grava a mensagem
                                      ├─ cria lead se for primeiro contato
                                      └─ se conversation.status = 'bot' → repassa envelope cru ao relay do agente

Gestão ──► POST /chat/conversations/{id}/send ──► uazapi /send/text|/send/media ──► WhatsApp
                                                    └─ grava a mensagem SÓ depois do 200
```

### Armadilhas que já custaram caro (documentadas, não deduzidas)

| Armadilha | Regra |
|---|---|
| Conversa chaveada pelo número do remetente | **Chavear por `chatid`.** Em mensagens próprias (`fromMe`) o remetente é o dono da instância — chavear por ele cria uma conversa do dono consigo mesmo |
| Mídia "vazia" na mensagem | O `content` inicial traz URL `.enc` inútil. O arquivo chega depois, em evento de download — re-hospedar em storage próprio, com guarda contra SSRF na URL |
| Tick regredindo | `pending < sent < delivered < read`. Webhook fora de ordem **nunca** rebaixa o status (guarda monótona no repositório) |
| Mensagem editada | Gera `messageid` novo — atualizar o `external_id`, senão os ticks congelam e o eco duplica |
| Gravar antes de enviar | Gravar só após o `200` da uazapi. Senão o painel afirma ter enviado o que não saiu |
| Webhook de saída duplicado | Registrar **um** webhook de instância. Dois causam envio dobrado |
| Grupos | `@g.us` é ignorado nesta fase |

Deduplicação por `UNIQUE(conversation_id, external_id)` — o eco da própria mensagem não vira linha nova.
Fila de saída com throttle por instância e backoff; reconexão re-registra o webhook mantendo o id de integração estável (reconectar não pode duplicar conversas).

### Configuração
`UAZAPI_BASE_URL`, `UAZAPI_TOKEN`, `UAZAPI_WEBHOOK_SECRET` — o segredo entra na URL do webhook (`?s=...`), porque a uazapi não envia header customizado.

---

## 2. Canais / OTA

### O estado real do acesso

| Canal | Programa | Situação |
|---|---|---|
| **Airbnb** | Software Partner API | **Fechado.** Exige NDA mútuo, aceite dos Termos da API, termos específicos do parceiro, aprovação em avaliação de segurança e implementação dos recursos obrigatórios em até 6 meses do lançamento. Proibido uso em produto concorrente |
| **Booking.com** | Connectivity / Supply | **Fechado.** Exige credenciamento e certificação no Connectivity Partner Programme. ⚠️ A *Demand API* (`developers.booking.com/demand`) é outra coisa: serve a quem **vende** inventário da Booking (agências e afiliados), não a quem publica as próprias unidades |

Conclusão: **nenhum dos dois é ligável hoje.** Por isso a integração começa por iCal, que ambos suportam sem parceria.

### Arquitetura — `ChannelProvider`

```go
type ChannelProvider interface {
    PullReservations(ctx context.Context, l Listing) ([]ExternalBooking, error)
    PushAvailability(ctx context.Context, l Listing, blocks []Block) error
    PushRates(ctx context.Context, l Listing, rates []Rate) error
}
```

Implementações: `ICalProvider` (ativa), `AirbnbProvider` e `BookingProvider` (stubs que retornam `ErrNotConfigured` e ligam por credencial). Uma **camada anticorrupção** converte o formato externo no domínio interno — nenhum tipo de OTA vaza para dentro do sistema.

### iCal — export

- Um feed por unidade: `GET /ical/{export_token}.ics`, token longo e revogável no path (feeds não enviam header).
- `UID` estável (`{reservation_id}@whitehousevillage`), `DTEND` exclusivo, `X-WR-CALNAME` com o nome da unidade.
- Reservas, bloqueios de manutenção e uso do proprietário entram; orçamentos não.

### iCal — import

- Pull a cada 15 min por listing (`ICAL_PULL_INTERVAL`).
- Evento externo vira `stay_block` com `source='ota'`, deduplicado por `UNIQUE(channel_id, external_uid)`.
- **`DTSTART;VALUE=DATE:20260821` é dia inteiro, sem fuso — parsear como `date`, jamais converter de UTC.** Converter desloca a reserva em um dia; é o bug clássico de canal.
- O Airbnb não envia nome do hóspede no feed (`SUMMARY` = "Reserved"). **Não inventar hóspede**: cria-se bloqueio, não reserva.

### Janela de risco e conflitos

- Airbnb busca o feed a cada ~1–3 h; Booking, ~15–60 min. **iCal nunca é seguro para as próximas 48 h.**
- `channel_listings.risk_window_days` (padrão 2): venda direta dentro da janela exige confirmação manual, e o mapa marca o período com selo de risco.
- Conflito detectado **não é sobrescrito**: vira registro em `channel_conflicts` + alerta no painel, com fluxo de resolução — realocar a unidade (possível porque as unidades são nominais) ou cancelar com compensação.

---

## 3. Webhooks de saída

- `webhooks(url, events[], secret, active)` + outbox `webhook_deliveries`.
- Assinatura `X-WHV-Signature: sha256=<hmac>` sobre o corpo cru; `X-WHV-Event` e `X-WHV-Delivery` no cabeçalho.
- Backoff exponencial com jitter: 30 s, 2 min, 10 min, 30 min, 2 h, 6 h, 12 h, 24 h → dead-letter, com reenvio manual pela interface.
- Eventos: `reservation.created|confirmed|cancelled|checked_in|checked_out`, `hold.expired`, `opportunity.stage_changed|won|lost`, `payment.received`, `channel.conflict_detected`.
- Toda tentativa é registrada com status, corpo e erro.

## 4. Webhooks de entrada

Segredo (header ou query) + `Idempotency-Key`; o retry do emissor nunca duplica registro, porque a chave é única por endpoint e a resposta gravada é devolvida no replay.

## 5. Tokens de API

```go
const prefix = "whv_"
raw   := base64url(random(32))
token := prefix + raw                 // exibido UMA única vez
hash  := sha256(token)                // é isso que vai para o banco
```

- `api_tokens(name, prefix, token_hash, scopes[], expires_at, last_used_at, revoked_at)`.
- **Escopos** (`reservations:read`, `crm:write`, …) e expiração — evolução sobre o all-or-nothing do SanareVita.
- Revogação é soft (`revoked_at`), preservando histórico. `last_used_at` atualizado fire-and-forget.
- Índice parcial `WHERE revoked_at IS NULL` para o lookup ser barato.

## 6. Agente de IA

O sistema é o **hub**, não roda o modelo:

```
WhatsApp → uazapi → webhook do sistema → (conversa em 'bot') → relay do agente (n8n / Python / LangChain)
                                                                    ↓ usa as ferramentas
                                                        /api/v1/integracao/*  ou  MCP
                                                                    ↓
                                                        uazapi /send/text → hóspede
```

- **Relay configurável na interface** (`app_settings`), com precedência UI → env → desligado, e indicador de estado na tela.
- **Ferramentas** expostas em `/api/v1/integracao/*`, autenticadas por token de API com escopo: consultar disponibilidade, gerar orçamento, criar pré-reserva, agendar visita, registrar lead, consultar reserva.
- **Servidor MCP** equivalente para agentes que falam MCP.
- **Takeover**: assumir a conversa muda o status para `human` e corta o relay imediatamente; o agente é avisado.
- Assinatura do bot configurável (o sistema é a fonte da verdade; o agente lê e aplica o prefixo).

## 7. Registro de tudo

`integration_logs(provider, direction, action, status, payload, error)` grava toda chamada externa com corpo truncado — é o primeiro lugar a olhar quando "a mensagem não chegou" ou "o Airbnb não atualizou".
