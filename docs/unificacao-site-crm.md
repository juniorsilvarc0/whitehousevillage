# Unificação do site de vendas com o ERP/CRM

> **Decisão de 02/10/2026.** O site público do White House Village deixa de ser um
> MVP separado e passa a ser **o front de cliente deste monorepo**. O que era
> `/Users/junior/DEV/spincode/whitehouse` agora é [`apps/site`](../apps/site).
> Este documento é o plano: o que já existe, o que falta, em que ordem, com que
> dono e com que critério de pronto.

## 1. O que esta decisão reverte — e o que ela traz junto

O `docs/roadmap.md` listava em **Fora de escopo** duas linhas que esta decisão
derruba: *"motor de reserva público com pagamento online"* e *"substituir o site
de marketing"*. Não há problema em mudar de ideia; há problema em mudar de ideia
sem pagar o que a ideia nova custa. Abrir reserva ao público arrasta, de uma vez:

| O que entra | Por que é obrigatório, não desejável |
| --- | --- |
| Pagamento do sinal online | Sem ele, "reserva pelo site" é só um formulário que a gestão confirma no WhatsApp — o problema que o CRM já resolve |
| Limitador de taxa **distribuído** | O limitador de hoje é um mapa na memória do processo (dívida **D1** do roadmap). Na internet, com mais de uma réplica da API, o limite vale por processo, ou seja, não vale |
| Negação de inventário | Quem cria pré-reserva sem pagar nada pode bloquear a casa inteira num domingo. É abuso barato e eficaz |
| LGPD na porta de entrada | O formulário público captura nome, telefone e e-mail de quem nunca assinou nada. Precisa de consentimento registrado e base legal escrita |
| Suporte a quem não tem sessão | Toda rota pública é uma rota sem usuário: sem `owner_id`, sem matriz de RBAC, sem trilha de auditoria com autor |

O item que **não** entra agora, e precisa ficar dito: **o preço público não é
negociável pelo cliente**. A alçada de desconto é decisão de gestão (até 5%),
de proprietário (6 a 10%) ou proibida (acima de 10%). Hoje a página pública tem
um **controle deslizante de desconto de 0 a 15% na mão do visitante**
(`apps/site/public/scripts/disponibilidade.js`, perto da linha 261), que ainda
exibe para ele qual alçada aquele desconto exigiria. Isso é política interna
publicada, e sai no passo **D2**.

## 2. Onde cada coisa vive depois da unificação

| Pasta | O que é | Porta local |
| --- | --- | --- |
| `apps/site` | Front de cliente. HTML/CSS/JS servido por nginx, sem build | `SITE_PORT` (3200) |
| `apps/admin` | Painel de gestão, Next.js com BFF | 3100 |
| `apps/api` | **A regra de negócio, inteira.** Go, domínio puro em `internal/domain` | 8080 |
| Postgres | A invariante: `EXCLUDE USING gist` em `stay_blocks` | 5433 |

O caminho que importa, depois da unificação:

```
hóspede → apps/site → superfície pública da API → internal/domain → Postgres
gestão  → apps/admin → BFF → a MESMA API → o MESMO domínio → o MESMO Postgres
```

Uma regra sai disso: **os dois fronts são clientes do mesmo contrato**. O site
não ganha um cálculo paralelo, não ganha um banco próprio e não ganha um
endpoint que "quase" faz o que o painel já faz.

## 3. A regra única desta integração

> **O site pergunta. Ele não calcula.**

Hoje o site calcula tudo em JavaScript, no navegador, a partir de um arquivo de
dados fictícios (`apps/site/public/scripts/data.js`, 223 linhas). Ele decide
sozinho, e portanto decide errado:

| O que o `data.js` decide hoje | Que regra do `CLAUDE.md` isso viola |
| --- | --- |
| A tarifa de cada noite por tipo de data | 1 — tarifa vive em `internal/domain` |
| O mínimo de noites por período | 1 — política é domínio |
| Sinal, prazo de saldo e prazo de pré-reserva | 1 e 7 — a reserva congela a versão da política |
| Desconto e alçada | 1 — e expõe política interna |
| Preço em reais com ponto flutuante | 4 — dinheiro é `bigint` em centavos |
| Quais datas estão livres | 2 — disponibilidade é o banco que responde |

Enquanto o passo **A2** não entrar, **nenhum número exibido em `apps/site` vale
como preço**. Está escrito no `apps/site/README.md`, e é o primeiro aviso que
alguém que abrir a pasta precisa ler.

## 4. O que já existe e não será reescrito

A API madura é a maior parte do trabalho, e ela já está pronta. O mapeamento
abaixo é literal: cada linha da esquerda morre e vira a chamada da direita.

| O site faz hoje (mock, no navegador) | Quem passa a responder |
| --- | --- |
| `units[]` com nome, fotos, capacidade e specs | `GET /unit-types`, `GET /units` |
| `units[].rates` por tipo de data | `GET /rate-tables`, `GET /rates` |
| `feriados` e `periodos` fixos no arquivo | `GET /holidays`, `GET /special-periods` |
| `WH.tarifa(dia)` e a soma das diárias | **`POST /quotes`** — roda o motor puro e devolve o cálculo aberto: noite a noite com o tipo, linhas agrupadas, subtotal, desconto, limpeza, total, sinal, saldo, mínimo de noites e a alçada |
| `politica` (sinal 50%, saldo 7 dias, pré-reserva 48h, alçadas) | `GET /policies/commercial`, e o próprio `POST /quotes` já devolve aplicado |
| `minNoites` por tipo de período | `GET /min-nights`, aplicado pelo `POST /quotes` |
| Calendário de livre/ocupado | `GET /availability`, `GET /availability/units` |
| "Completa trava as individuais" calculado em JS | Composição de tipos de unidade mais a constraint `EXCLUDE` — o banco recusa, não a tela |
| `reservas[]` fictícias | `reservations` e `stay_blocks`, ciclo `quote → hold → confirmed → checked_in → checked_out → closed` |
| `leads[]` fictícios | `POST /crm/leads`, `POST /crm/opportunities` |
| `taxaLimpeza` por produto | Já embutida no total que o `POST /quotes` devolve |

Vale repetir o que o contrato do `POST /quotes` garante, porque é exatamente o
que o JavaScript do site faz errado: o desconto incide **só** sobre as diárias,
o total é `subtotal − desconto + limpeza + caução`, e o arredondamento acontece
**no total**, nunca noite a noite.

## 5. O que falta construir: a superfície pública

Nenhuma rota citada acima é pública. Todas exigem token e célula da matriz de
RBAC — o que está certo, porque foram feitas para a gestão. Então a pergunta
real da unificação é **como o navegador de um desconhecido chega ao domínio**.

Duas saídas foram consideradas:

- **(A) Superfície pública mínima na API Go.** Um punhado de rotas novas,
  declaradas `AcessoPublico` na tabela de rotas, com justificativa escrita, cada
  uma com limite de taxa e resposta sem nenhum campo interno.
- **(B) Um intermediário com credencial de serviço** na frente do site, que
  chama as rotas autenticadas que já existem.

**Escolhemos (A).** O motivo é o mecanismo que o repositório já tem: a tabela de
rotas em `apps/api/internal/router/routes.go` classifica cada rota num eixo de
acesso, exige justificativa escrita quando a rota não consulta a matriz, e o
teste de contrato varre essa tabela e **reprova** rota sem classificação ou fora
da OpenAPI. Ou seja: o caminho (A) é auditável por teste. A opção (B) guarda uma
credencial de serviço capaz de tudo atrás de um proxy, e troca uma garantia
verificável por uma configuração que ninguém testa.

As rotas abaixo são **proposta**, não contrato: quem fecha nome, formato e
código de erro é o `tech-lead`, no `openapi.yaml`, antes de qualquer linha de Go.

| Rota proposta | Para quê | Cuidado que não pode faltar |
| --- | --- | --- |
| `GET /public/products` | Catálogo das acomodações (hoje escrito à mão no HTML) | Só campos de vitrine. Nada de custo, dono ou nota interna |
| `GET /public/availability` | Dias livres por produto | Janela limitada (12 meses). Devolve **livre ou ocupado**, nunca o motivo, nunca o nome de quem ocupa |
| `POST /public/quotes` | Orçamento da data escolhida | `persist` sempre falso e **desconto sempre zero**. O público não negocia |
| `POST /public/holds` | Pré-reserva: cria contato, oportunidade e reserva em `hold` | `Idempotency-Key` obrigatório. Erro `23P01` do banco vira **`409 DATE_CONFLICT`**, nunca 500, nunca repetição automática |
| `POST /public/leads` | "Quero saber mais", sem data escolhida | Consentimento LGPD registrado junto |
| `POST /public/payments/webhook` | Sinal pago vira `confirm` | Assinatura do provedor verificada. Idempotente por natureza: provedor reenvia |

Três invariantes atravessam todas elas, e nenhuma é negociável:

1. **Resposta pública nunca carrega dado de terceiro.** Nem nome, nem telefone,
   nem valor de outra reserva, nem motivo de bloqueio.
2. **A defesa contra overbooking continua sendo o banco.** A rota pública não
   ganha um `SELECT` antes do `INSERT` para "ser gentil com o cliente".
3. **Toda rota pública é limitada por taxa**, e o limitador precisa ser o
   distribuído do item **B0**, não o mapa de processo de hoje.

## 6. Backlog ordenado

A ordem não é estética. Ela existe para que o site nunca fique num estado em que
**mostra um preço que o banco não confirma**.

### A — Fundação

| # | O que | Dono | Pronto quando |
| --- | --- | --- | --- |
| **A0** | Trazer o site para `apps/site`, servir pelo Compose, documentar e dar dono de pasta | — | **Feito em 02/10/2026** (esta rodada) |
| **A1** | Contrato da superfície pública no `openapi.yaml` e as linhas na tabela de rotas, com justificativa de `AcessoPublico` | `tech-lead` | O teste de contrato passa com as rotas novas, e o que elas devolvem está escrito antes de existir implementação |
| **A2** | O site para de calcular: catálogo, calendário e orçamento passam a vir da API. `data.js` encolhe até morrer | `next-frontend` com `backend-go` | Trocar uma tarifa no painel muda o preço que o site mostra, sem editar arquivo nenhum |

### B — Vender de verdade

| # | O que | Dono | Pronto quando |
| --- | --- | --- | --- |
| **B0** | Limitador de taxa distribuído (dívida **D1**): sai o mapa de processo, entra Redis | `devops` com `backend-go` | Com duas réplicas da API no ar, a sexta tentativa de login é recusada — hoje ela passa se cair na outra réplica |
| **B1** | Pré-reserva pública: `POST /public/holds` com idempotência, `409` em conflito e expiração automática | `backend-go` | Dois navegadores pedindo a mesma data ao mesmo tempo: um recebe `201`, o outro `409 DATE_CONFLICT`. Teste de concorrência com `-count=10` |
| **B2** | Pagamento do sinal e confirmação automática | `integracoes` | Sinal pago no provedor vira reserva `confirmed` sem ninguém clicar, e o reenvio do webhook não confirma duas vezes |
| **B3** | O lead do site entra no funil com origem `Site` e aparece no kanban em tempo real | `backend-go` | Pré-reserva feita no site aparece no painel sem recarregar a página |

### C — Experiência

| # | O que | Dono | Pronto quando |
| --- | --- | --- | --- |
| **C1** | Área do hóspede (ver reserva, enviar documento, pagar saldo) | a decidir | **Depende da decisão 8.2** — pode não existir |
| **C2** | Unificar a linguagem visual: os tokens do site e o design system do painel | `next-frontend` | Uma cor de marca muda em um lugar e vale nos dois fronts |
| **C3** | Decidir se `apps/site` continua estático ou vira Next.js | `tech-lead` | Decisão escrita com motivo. Hoje estático é suficiente: 3250 linhas que funcionam, sem build, e indexável |

### D — Limpeza (dívida que nasce com a importação)

| # | O que | Dono | Pronto quando |
| --- | --- | --- | --- |
| **D1** | **Apagar `apps/site/public/admin/`** — back-office mocado, 1143 linhas, servido pelo mesmo nginx **sem autenticação nenhuma**. Hoje mostra receita, ticket médio, conversão e funil fictícios a quem souber o caminho. Enquanto o dado é falso é vergonha; no dia em que for real é vazamento | `next-frontend` | O caminho `/admin/` no site responde 404, e o que ele desenhava existe no `apps/admin` |
| **D2** | Tirar o controle de desconto da mão do visitante | `next-frontend` | A página pública não tem como pedir desconto, e não informa alçada nenhuma |
| **D3** | Matar `data.js` por completo | `next-frontend` | O arquivo não existe, e nenhuma tela quebra |
| **D4** | Registrar `apps/site/**` nas definições dos agentes em `.claude/agents/` | `squad-lead` | O agente de front sabe que a pasta é dele sem alguém lembrar |
| **D5** | Decidir o destino do vídeo de 12 MB em `public/videos/hero.mp4` | `devops` | Ou está em Git LFS, ou está num CDN, ou a decisão de deixá-lo no repositório está escrita |
| **D6** | Trocar o número de WhatsApp fictício `5586999999999`, que está em **cinco** botões de contato (`index.html` e `disponibilidade.html`) | `next-frontend` | Todo botão de contato abre a conversa certa. Melhor ainda se o número vier de configuração, não do HTML |

## 7. O que não muda

As dez regras inegociáveis do `CLAUDE.md` valem igual. O site é **mais um
consumidor do mesmo contrato**, não uma exceção a ele. Em particular:

- Regra 1: o site não ganha regra de negócio. Nem "só essa validação".
- Regra 2: overbooking continua impedido pelo banco.
- Regra 4: dinheiro em centavos, `bigint`. O site formata, não arredonda.
- Regra 7: a reserva criada pelo site congela a tarifa da noite e a versão da
  política, como qualquer outra.
- Regra 10: `openapi.yaml`, `routes.go` e `navigation.ts` continuam do `tech-lead`.

## 8. Decisões que precisam do dono do negócio

Nenhuma delas é técnica, e nenhuma pode ser adivinhada por quem implementa.

1. **Provedor de pagamento.** Pix, cartão, ou os dois? Quem concilia o
   recebimento? Isso define o passo **B2** inteiro.
2. **O hóspede faz login?** "Onde os clientes vão entrar e fazer reservas" pode
   significar *entrar no site* ou *entrar numa conta*. Se existe conta de
   hóspede, nasce o passo **C1**, com recuperação de senha, sessão e LGPD.
3. **Reserva pública nasce `hold` ou nasce pedido?** Opção 1: o site cria a
   pré-reserva e segura a data por 48h. Opção 2: o site registra a intenção e a
   gestão confirma. A opção 1 vende sozinha e abre a porta da negação de
   inventário; a opção 2 é segura e mantém o WhatsApp no caminho.
4. **O público vê a tabela de tarifas inteira?** Hoje vê. Mostrar o ano todo
   entrega o tarifário ao concorrente.
5. **A disponibilidade pública revela a ocupação da casa.** Quem olha o
   calendário todo dia sabe quanto a casa vende. Tudo bem?
6. **Domínios.** Site e painel no mesmo domínio ou em domínios separados? A
   resposta muda a configuração de CORS, de cookie e do Traefik.

## 9. Como rodar os dois juntos hoje

```bash
make up        # site, painel, API e Postgres
make migrate   # schema, num passo separado — nunca no boot
make seed      # produtos, tarifas, política e os usuários de desenvolvimento
make smoke     # sobe o navegador, faz login no painel e percorre as telas
```

O site **não** está na fumaça ainda, porque ele não fala com a API: não há o que
verificar além de o nginx responder. Ele entra na fumaça no passo **A2**, e o
critério é o mesmo que o painel já cumpre — nenhum link interno com 404 ou 5xx.

---

Mudou a decisão? Edite este arquivo primeiro, e só depois o código.
