# Especificação funcional

> Regras, estados, validações e critérios de aceite por módulo. O modelo físico está em [`db.md`](db.md), o contrato em [`api.md`](api.md).

## Convenções

- **Noite** é a unidade de venda. Uma estadia `[check_in, check_out)` de 20 a 23 tem 3 noites: 20, 21 e 22. O dia do check-out fica livre para outro hóspede (back-to-back).
- **Dinheiro** em centavos inteiros. Arredondamento sempre no total, nunca por noite.
- **Fuso** `America/Fortaleza`. "Hoje" tem uma única definição no sistema.
- **Tudo que é regra comercial é dado versionado**, não constante em código.

---

## 1. Auth, usuários e RBAC

### Autenticação
- Login por e-mail + senha (argon2id). Access token JWT de 15 min; refresh rotativo de 30 dias em cookie `httpOnly`, `SameSite=Lax`, `Secure` em produção.
- Reuso de refresh token revogado invalida a família inteira de sessões (detecção de roubo).
- 5 tentativas erradas em 15 min bloqueiam o e-mail por 15 min; a resposta de "e-mail não existe" e "senha errada" é idêntica.
- Recuperação de senha por token de uso único válido por 1 h.

### Autorização
Quatro eixos, todos em tabela:

```
papel  ×  recurso  ×  ação (ver|criar|editar|excluir)  ×  escopo (all|own)
```

- `escopo = own` filtra por `owner_id = usuário` **no SQL**, não na aplicação.
- O front lê `/auth/me` para esconder o que o usuário não pode fazer; a barreira real é sempre o middleware.
- Perfis nascem como *seed* da matriz: `admin` (tudo, `all`), `usuario` (operação, `all`, sem configurações), `corretor` (CRM + reservas + comissões, `own`).

### Critérios de aceite
- Corretor autenticado recebe `403` em `/finance/*` e lista apenas as próprias oportunidades — provado por teste de integração.
- Alterar a matriz de um papel muda o comportamento sem deploy.
- Nenhum endpoint fica sem checagem de permissão (teste de contrato varre a tabela de rotas e falha se faltar).

---

## 2. Propriedades e inventário

### Estrutura
- **Propriedade** — a casa (o `property_id` acompanha todas as tabelas, para não impedir uma segunda propriedade no futuro).
- **Unidade física** (`units`) — o que é efetivamente ocupado e limpo: `AP-01..03`, `SP-01..04`, `COB-01`. Oito unidades.
- **Produto** (`unit_types`) — o que é vendido:

| Produto | Capacidade | Consome | Unidades |
|---|---|---|---|
| Apartamento 2 Suítes | 6 | uma unidade do grupo | AP-01, AP-02, AP-03 |
| Suítes da Piscina | 3 | uma unidade do grupo | SP-01 … SP-04 |
| White House Cobertura | 10 | uma unidade do grupo | COB-01 |
| **White House Completa** | 24 | **todas as unidades** | AP-01..03, SP-01..04, COB-01 |

- A capacidade da Completa é **declarada** (24), não somada (a soma daria 40): é limite operacional de evento, decidido pelos proprietários.
- A composição exata da Completa é configurável — é uma das decisões em aberto do diagnóstico.

### Alocação de unidade
Ao criar uma reserva de produto simples, o sistema **aloca automaticamente** a unidade que menos fragmenta o calendário (menor sobra entre reservas vizinhas), com desempate por código. A gestão pode trocar a unidade (`reassign-unit`) e travar a escolha (`unit_locked`) quando o hóspede pede um apartamento específico.

### Critérios de aceite
- Vender três Apartamentos 2 Suítes na mesma data é possível (há três unidades); o quarto pedido recebe `409`.
- Vender a Completa com qualquer unidade ocupada é impossível — recusado pelo banco.

---

## 3. Tarifário e política comercial

### Tipos de data e precedência
Cada noite recebe **um** tipo, resolvido por precedência numérica em tabela:

| Tipo | Precedência | Regra |
|---|---:|---|
| `reveillon` | 100 | período especial |
| `carnaval` | 100 | período especial |
| `feriado` | 80 | data em `holidays` |
| `alta` | 60 | período especial de alta temporada |
| `fds` | 40 | sexta ou sábado |
| `normal` | 0 | demais |

Períodos se sobrepõem de propósito (Réveillon dentro da alta temporada); vence o de maior precedência. Mudar a precedência é mudar uma linha, não o código.

### Tarifas
- Tabela de tarifas versionada por vigência (`valid_from`/`valid_to`): produto × tipo de data → valor.
- Valores iniciais (Tabela Comercial V1):

| Produto | Normal | FDS | Feriado | Alta | Réveillon | Carnaval |
|---|---:|---:|---:|---:|---:|---:|
| Apartamento 2 Suítes | 850 | 1.100 | 1.400 | 1.600 | 3.200 | 2.600 |
| Suítes da Piscina | 550 | 700 | 900 | 1.050 | 2.100 | 1.700 |
| Cobertura | 1.900 | 2.400 | 3.100 | 3.600 | 7.500 | 6.000 |
| Completa | 5.500 | 6.900 | 8.900 | 10.500 | 21.000 | 17.000 |

- Taxa de limpeza por produto (180 / 120 / 350 / 900), cobrada **uma vez por estadia**.
- Estadia mínima por tipo de data: normal 1, fds 2, feriado 3, alta 3, réveillon 4, carnaval 4. Vale o **maior** mínimo entre as noites da estadia.

### Política comercial (versionada)
- Sinal para confirmar: **50%**.
- Saldo: até **7 dias** antes do check-in.
- Pré-reserva segura a data por **48 h** sem pagamento.
- Alçada de desconto: **≤ 5%** a gestão fecha · **6–10%** exige aprovação do proprietário · **> 10%** não autorizado.
- Caução de evento: R$ 2.000, cobrada como recebível reembolsável.

### Política de cancelamento (versionada, em faixas)
| Antecedência | Regra |
|---|---|
| ≥ 30 dias | devolução integral do sinal |
| 7 a 29 dias | retenção de 50% do sinal |
| < 7 dias | retenção integral do sinal |
| no-show | retenção integral (regra a confirmar com os proprietários) |

A reserva **congela** a versão da política vigente na criação. Mudar a política amanhã não altera reservas já feitas.

### Critérios de aceite
- `GET /rates` reproduz a Tabela V1 exatamente.
- Alterar uma tarifa na tela muda o próximo orçamento e **não** muda nenhum orçamento ou reserva já emitidos.
- Réveillon atravessando o ano (28/12 a 02/01) classifica todas as noites como `reveillon`.

---

## 4. Disponibilidade e orçamento

### Motor (puro, sem I/O)
1. Para cada noite em `[check_in, check_out)`: resolve o tipo de data e busca a tarifa do produto na tabela vigente.
2. Agrupa por tipo (ex.: "Fim de semana 2× R$ 4.800").
3. `subtotal` = soma das noites.
4. `desconto` = `subtotal × pct` — **incide só sobre diárias**, nunca sobre a limpeza.
5. `total` = `subtotal − desconto + limpeza` (+ caução, se evento).
6. `sinal` = `total × 50%`; `saldo` = `total − sinal`, vencendo 7 dias antes do check-in.

### Validações que bloqueiam a emissão
| Situação | Resposta |
|---|---|
| Noites < mínimo do período | `422 MIN_STAY_NOT_MET`, com o mínimo exigido |
| Hóspedes > capacidade do produto | `422 CAPACITY_EXCEEDED` |
| Desconto > alçada do usuário | `422 DISCOUNT_ABOVE_LIMIT` (6–10% pede aprovação; > 10% recusado) |
| Alguma noite ocupada | `409 DATE_CONFLICT` com o intervalo em conflito |
| `check_out <= check_in` | `422 VALIDATION_ERROR` |

### Disponibilidade
- `GET /availability/units` devolve a matriz **unidade × dia** — a fonte do mapa de ocupação.
- `GET /availability` devolve, por produto, quantas unidades estão livres em cada dia; a Completa é livre apenas quando **todas** estão.
- Orçamento (`POST /quotes`) **não bloqueia data**. Só a pré-reserva bloqueia.

### Critérios de aceite
- 20 cenários tabelados (incluindo Réveillon, Carnaval e feriado em fim de semana) batem centavo a centavo com a planilha comercial.
- Teste de propriedade: para estadias aleatórias, `total` nunca é negativo, o desconto nunca toca a limpeza e o sinal é sempre metade do total arredondado.

---

## 5. Reservas

### Estados

```
        ┌──────────────── expired ◄──── (48h sem sinal)
quote → hold ──► confirmed ──► checked_in ──► checked_out ──► closed
          │          │                              ↘ no_show
          └──────────┴──► cancelled
```

| Estado | Significado | Bloqueia calendário |
|---|---|---|
| `quote` | orçamento emitido | não |
| `hold` | pré-reserva, expira em 48 h | **sim** |
| `confirmed` | sinal recebido | **sim** |
| `checked_in` / `checked_out` | estadia em curso / encerrada | sim / não |
| `cancelled` / `expired` / `no_show` | encerrada sem estadia | não |

### Regras
- **Código legível** `WH-2026-0001`, sequencial por ano, imutável.
- Criar reserva grava o **snapshot das noites** (`reservation_nights`) — tarifa por noite, tipo de data e produto.
- `hold` grava `expires_at = now() + política`. Um job expira automaticamente a cada minuto, libera as unidades e registra uma atividade no CRM.
- Estender a pré-reserva é ação explícita e auditada (`extend-hold`), com limite configurável.
- Confirmar exige registrar o sinal; gera os recebíveis (sinal quitado + saldo agendado) e a comissão do corretor, se houver.
- Cancelar aplica a faixa da política **congelada na reserva**, com `?dry_run=1` para mostrar o valor antes de executar.
- Remarcar preserva o histórico: a reserva original vai para `cancelled` com motivo `remarcacao` e a nova referencia a anterior; a diferença tarifária é calculada e cobrada.
- Bloqueio operacional (manutenção, uso do proprietário) é `stay_block` sem reserva, criado direto no mapa.

### Critérios de aceite
- 50 goroutines tentando reservar a mesma data na Completa e na Cobertura: **exatamente uma** vence, as demais recebem `409 DATE_CONFLICT`, nenhuma `500`.
- Criar `hold`, esperar a expiração: a data libera sozinha e o mapa atualiza sem refresh.
- Cancelar com 10 dias de antecedência retém 50% do sinal e gera o reembolso a pagar.

---

## 6. Contatos e hóspedes

- **Uma pessoa, um registro.** Lead, hóspede, corretor e proprietário apontam para `contacts`.
- Deduplicação por telefone E.164 (índice único parcial) e por documento; o WhatsApp identifica o contato pelo número antes de criar outro.
- Rooming list por reserva: hóspede titular + acompanhantes, com o mínimo de dado necessário.
- LGPD: base legal por finalidade, `marketing_opt_in` separado, consentimento datado, exportação em JSON e **anonimização** que preserva os registros financeiros.

---

## 7. CRM

### Funis e etapas
- Funis configuráveis; etapas com `posição`, `probabilidade`, `cor`, `tipo` (`aberto|ganho|perdido`), **`sla_dias`** e **tarefa automática** (assunto, tipo, prazo).
- Funil inicial: Novo lead → Em atendimento → Disponibilidade consultada → Orçamento enviado → Negociação → Pré-reserva → Ganho / Perdido.

### Oportunidade
- Vinculada a **contato** (obrigatório), lead (origem), produto pretendido, datas pretendidas, orçamento vigente e — quando ganha — a reserva.
- Entrar numa etapa cria **uma** tarefa automática (idempotente: não duplica se já existe pendente da mesma etapa), com prazo do SLA e responsável = dono da oportunidade.
- Mudar de etapa grava `stage_history` (insert-only) e dispara evento para webhooks.
- Etapa do tipo `ganho` **cria a reserva** a partir do orçamento vigente; `perdido` exige motivo.

### Atividades e alertas
- Tipos: tarefa, ligação, reunião, e-mail, WhatsApp, nota. Vínculo com lead, oportunidade ou contato.
- Alertas derivados, calculados no servidor: SLA estourado, tarefa vencida, cliente parado há N dias, pré-reserva expirando, saldo a vencer.
- A página de oportunidade carrega tudo numa chamada (`/full`): dados, etapas, SLA, atividades, notas, documentos, histórico e timeline unificada (inclui mensagens do chat).

### Critérios de aceite
- Mover o card para "Orçamento enviado" cria sozinho a tarefa de follow-up com o prazo do SLA.
- Ganhar a oportunidade cria a reserva com o orçamento vigente, sem redigitar nada.
- Corretor vê no kanban apenas os próprios cards.

---

## 8. Chat / WhatsApp (uazapi)

- Conexão por QR code, com estado da instância visível e reconexão automática.
- Conversa chaveada por **`chatid`** (nunca pelo número do remetente — em mensagens próprias isso criaria uma conversa do dono consigo mesmo).
- Estados: `bot` (agente de IA responde), `human` (assumida pela gestão), `resolved`.
- Inbound cria/atualiza contato e lead; se a conversa está em `bot`, o envelope é repassado ao relay do agente.
- Ticks (`pending → sent → delivered → read`) **monotônicos**: webhook fora de ordem nunca regride o status.
- Mídia chega depois, no evento de download; o arquivo é re-hospedado em storage próprio, com guarda contra SSRF.
- Mensagem só é gravada **depois** do `200` da uazapi — o sistema nunca afirma ter enviado o que não saiu.
- Deduplicação por `(conversation_id, external_id)`; editar mensagem gera novo id externo e atualiza o vínculo.
- Respostas rápidas, assinatura do operador, etiquetas e busca dentro da conversa.

### Critérios de aceite
- Mensagem enviada de um celular real aparece na tela em < 3 s e cria o lead.
- Áudio gravado no painel chega no WhatsApp do hóspede.
- Assumir a conversa corta o agente de IA imediatamente.

---

## 9. Agenda operacional

- Eventos: check-in, check-out, limpeza, manutenção, visita, evento, bloqueio.
- **Alimentada automaticamente** pelas reservas — confirmar reserva cria check-in, check-out e limpeza.
- Quatro visões: dia, semana, mês e lista. Horário de funcionamento e bloqueios configuráveis.
- Conflito de horário é **aviso**, não bloqueio (a operação sabe o que está fazendo); conflito de unidade é bloqueio.

---

## 10. Financeiro

- **Recebíveis** por reserva: sinal, saldo, caução (reembolsável) e extras. Status: aberto, pago, parcial, vencido, cancelado.
- **Pagáveis**: comissão de corretor, repasse ao proprietário, fornecedores, despesas, devolução de caução.
- **Pagamentos** com forma (PIX, cartão, transferência, dinheiro), data, referência externa e conciliação.
- **Comissões**: regra por corretor e produto (percentual sobre a diária, sem incidir sobre limpeza/caução); geradas na confirmação e liberadas conforme política.
- **Repasse ao proprietário**: apuração por competência, com receita, taxas e líquido; snapshot da regra usada.
- **Razão append-only**: nada é editado, tudo é estornado com contrapartida.
- Caução devolvida no check-out (integral ou parcial, com laudo de dano anexado).

### Critérios de aceite
- Confirmar reserva gera automaticamente 2 recebíveis (+ caução em evento) e a comissão.
- O fechamento do mês bate com o razão: diferença zero.

---

## 11. Corretores e parceiros

- Cadastro vinculado a um usuário de perfil `corretor`; regra de comissão por produto e faixa; meta mensal.
- Painel próprio: leads, oportunidades, pré-reservas, comissões previstas e pagas, desempenho.
- Corretor **nunca** vê financeiro global, dados de outros corretores ou configurações.

---

## 12. Inventário operacional

- Itens de enxoval e consumíveis por unidade, com quantidade padrão e estoque.
- Checklist de limpeza e vistoria por estadia, com registro de avaria (que alimenta a retenção de caução).
- Ordens de manutenção que **geram bloqueio no calendário** e liberam ao concluir.
- Custo por estadia (enxoval + consumo + limpeza) alimenta a margem no BI.

---

## 13. Canais / OTA

- **Export**: um feed iCal por unidade, com token longo e revogável no path, `UID` estável e `DTEND` exclusivo.
- **Import**: pull a cada 15 min por listing; evento externo vira `stay_block` de origem `ota`, deduplicado por `(canal, uid)`. Nunca inventa hóspede — o Airbnb não envia nome no feed.
- **Janela de risco** configurável (padrão 2 dias): venda direta dentro dela exige confirmação manual, porque o feed externo pode estar desatualizado.
- **Conflitos** detectados viram registro + alerta no painel, com fluxo de resolução (realocar unidade — possível porque as unidades são nominais — ou cancelar com compensação). O sistema nunca sobrescreve silenciosamente.
- Datas de evento all-day do iCal são lidas como **`date`**, jamais convertidas de UTC (converter desloca a reserva em um dia).

---

## 14. Integrações

- **Tokens de API**: `whv_` + 32 bytes; guarda apenas `sha256` + prefixo; texto puro exibido uma única vez; escopos e expiração; revogação soft; `last_used_at`.
- **Webhooks de saída**: assinatura HMAC-SHA256, outbox com backoff exponencial e jitter, log de cada tentativa, dead-letter e reenvio manual.
- **Webhooks de entrada**: segredo + `Idempotency-Key`; retry do emissor nunca duplica registro.
- **Agente de IA**: relay configurável na interface; superfície `/api/v1/integracao/*` como ferramentas (consultar disponibilidade, gerar orçamento, criar pré-reserva, agendar visita) e servidor MCP equivalente.

---

## 15. BI e indicadores

Ocupação (por unidade, produto e período), ADR, RevPAR, ticket médio, lead time de reserva, conversão por etapa e por origem, receita prevista × realizada, motivos de perda, tempo médio de resposta no chat, desempenho por corretor, sazonalidade e comparação ano a ano.

Views materializadas com refresh horário; exportação em XLSX e CSV.

---

## 16. Configurações e auditoria

- Parâmetros do sistema, tarifário, políticas, funis, usuários e perfis, canais, integrações e agente de IA.
- Segredos em cofre cifrado, editáveis sem redeploy.
- **Auditoria**: toda escrita grava ator, entidade, antes e depois, IP e `request_id`. Leitura de dado pessoal grava em log separado (exigência de LGPD).
- Tela de **decisões em aberto** — as regras que os proprietários ainda não fecharam ficam visíveis como pendência, com responsável e data.
