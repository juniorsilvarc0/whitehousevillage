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
de proprietário (6 a 10%) ou proibida (acima de 10%). Até 02/10/2026 a página
pública tinha um **controle deslizante de desconto de 0 a 15% na mão do
visitante** (`apps/site/public/scripts/disponibilidade.js`, perto da linha 261),
que ainda exibia para ele qual alçada aquele desconto exigiria. Era política
interna publicada, e **saiu no passo D2**, na Rodada 5.

Uma correção que a verificação de 02/10 obrigou a escrever: a alçada "6 a 10%
com aprovação do proprietário" **não existe no código**. O motor
(`internal/domain/booking`) classifica o desconto e devolve o rótulo em
`discount_authority`, mas só recusa acima de 10%; não há fluxo de aprovação, e
uma venda com 8% fecha sem ninguém aprovar. Ou ela vira fluxo, ou sai dos
documentos — é a decisão 13 da lista de pendências do dono no `roadmap.md`. Para
este plano, a consequência é direta: a resposta pública de orçamento **não**
pode reaproveitar o DTO do painel, que expõe `discount_authority`.

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
dados fictícios (`apps/site/public/scripts/data.js`). Eram 223 linhas; o passo
D3 parcial de 02/10 tirou dele tudo o que era dado de terceiro ou regra interna
(reservas com nome, valor e origem; leads com telefone; funil, etapas do CRM,
alçada, caução e inventário) e ele caiu para **177 linhas** — sem mudar um
preço: o `next-frontend` comparou o calendário e a tarifa antigos com os novos
dia a dia, 2072 produto-dias, zero diferenças. O que sobrou continua decidindo
sozinho, e portanto errado:

| O que o `data.js` decide hoje | Que regra do `CLAUDE.md` isso viola |
| --- | --- |
| A tarifa de cada noite por tipo de data | 1 — tarifa vive em `internal/domain` |
| O mínimo de noites por período | 1 — política é domínio |
| Sinal, prazo de saldo e prazo de pré-reserva | 1 e 7 — a reserva congela a versão da política |
| ~~Desconto e alçada~~ | Saiu em 02/10 (passo D2). O site não tem mais desconto nenhum, nem a palavra "alçada" |
| Preço em reais inteiros num `Number` do JavaScript, exibido sem centavos (`maximumFractionDigits: 0`), com o sinal arredondado por conta própria e sem a caução de evento no total — que a API inclui | 4 — dinheiro é `bigint` em centavos; e 1, porque o total do site já diverge do da API |
| Quais datas estão livres (calendário fictício, `ocupacoes` só com unidade e período) | 2 — disponibilidade é o banco que responde |

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
acesso, e o teste de contrato varre essa tabela e **reprova** rota sem
classificação ou fora da OpenAPI. Ou seja: o caminho (A) é auditável por teste.
A opção (B) guarda uma credencial de serviço capaz de tudo atrás de um proxy, e
troca uma garantia verificável por uma configuração que ninguém testa.

**Correção de 02/10/2026 — a "justificativa escrita" para rota pública não
existe; o A1 tem de criá-la.** A versão anterior deste parágrafo dizia que a
tabela "exige justificativa escrita quando a rota não consulta a matriz". Medido
no código: o campo `Motivo` só é obrigatório para `AcessoAutenticado`
(`routes.go:48-50` e `ValidarTabela`); `AcessoPublico` é conferido só por não
declarar recurso e ação. O controle que existe para rota pública é outro, e é
mais fraco do que o texto prometia: uma **lista fechada** de seis rotas em
`routes_test.go` (`/healthz`, `/readyz` e quatro de `/auth`), que reprova
qualquer sétima. Também **não existe limite de taxa por rota**: a struct `Rota`
não tem o campo; o roteador liga um limitador por caminho fixo, só em
`/auth/login` (`router.go`), e os demais (tentativas por e-mail, disparo de
recuperação de senha) vivem dentro do serviço de autenticação. Para a opção (A) valer o que este plano promete, o A1 entrega
junto com o contrato: `Motivo` obrigatório também para `AcessoPublico`, com
`ValidarTabela` e teste reprovando a rota pública sem ele; um campo de limite de
taxa na `Rota`, com teste reprovando rota pública sem limite; e as rotas novas
na lista fechada, uma a uma.

As rotas abaixo são **proposta**, não contrato: quem fecha nome, formato e
código de erro é o `tech-lead`, no `openapi.yaml`, antes de qualquer linha de Go.

| Rota proposta | Para quê | Cuidado que não pode faltar |
| --- | --- | --- |
| `GET /public/products` | Catálogo das acomodações. Hoje ele **não** está escrito à mão no HTML, como este documento dizia: a home tem um `<div data-units>` vazio, e `main.js` monta os cartões — inclusive o "a partir de" — a partir de `WH.units`, do `data.js` | Só campos de vitrine. Nada de custo, dono ou nota interna |
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

E uma condição para a terceira valer, que a verificação de 02/10 achou e nenhum
passo cobria: **o limitador conta pelo IP do visitante, e o IP só chega à API se
quem está na frente o repassar.** Se o A2 puser um proxy `/api` no nginx de
`apps/site` — o caminho natural para o navegador em `:3200` falar com a API sem
abrir CORS —, esse `location` precisa de `proxy_set_header X-Forwarded-For
$proxy_add_x_forwarded_for` e `X-Real-IP $remote_addr`. Sem isso, todo visitante
chega à API com o IP do contêiner do site, e o limitador de cada rota pública
vira um contador único para a internet inteira: a primeira dúzia de curiosos
tranca o resto. É exatamente a falha medida no painel em 27/08 (dívida **D4**
do roadmap: 20 logins da instalação inteira por 15 minutos, porque o BFF não
repassa o cabeçalho). `httpx.RealIP` já consome o cabeçalho quando o peer é rede
interna. O critério de pronto do proxy é um teste com dois IPs de origem
diferentes caindo em janelas diferentes do limitador.

**Questão aberta para o A1 decidir, por escrito:** `docs/integracao.md` §6 prevê
ferramentas em `/api/v1/integracao/*`, autenticadas por token de API, que
"consultam disponibilidade, geram orçamento, **criam pré-reserva**, registram
lead". Isso é uma **segunda porta externa para a mesma operação** que
`POST /public/holds`, e nenhum dos dois documentos cita o outro. Duas portas
para criar pré-reserva são dois lugares para esquecer a idempotência, o limite
de taxa ou a defesa contra negação de inventário. O A1 decide se as duas passam
pelo mesmo serviço de domínio (e qual guarda é de qual porta), ou se uma delas
não existe.

## 6. Backlog ordenado

A ordem não é estética. Ela existe para que o site nunca fique num estado em que
**mostra um preço que o banco não confirma**.

Dois passos abaixo têm dois donos (A2 e B0), contra a regra de backlog do
`squad-lead` — um item, um dono. Ficam assim enquanto são **plano**; quando um
deles entrar numa rodada, vira item de `docs/backlog/` quebrado em dois com
handoff (no B0, por exemplo: `devops` sobe o Redis no compose; `backend-go` troca
o `Limitador` de `httpx/middleware.go` e entrega o teste das duas réplicas). A
ordem entre este plano e o Bloco 1 da Fase 2 é a decisão 1 da lista de
pendências do dono no `roadmap.md`.

### A — Fundação

| # | O que | Dono | Pronto quando |
| --- | --- | --- | --- |
| **A0** | Trazer o site para `apps/site`, servir pelo Compose, documentar e dar dono de pasta | — | **Feito em 02/10/2026** (`957e6e3`). O dono de pasta ficou só em `docs/agents.md` até o passo D4 fechar a definição do agente, na Rodada 5 |
| **A1** | Contrato da superfície pública no `openapi.yaml` e as linhas na tabela de rotas — **criando** o mecanismo de justificativa e de limite de taxa para `AcessoPublico`, que não existe (§5) | `tech-lead` | O teste de contrato passa com as rotas novas; rota pública sem `Motivo` ou sem limite de taxa reprova em `ValidarTabela` e no teste, com controle negativo; a resposta pública de orçamento não carrega `discount_authority`, `policy_version` nem `rate_table_id`; e a questão das duas portas de pré-reserva (§5) está decidida por escrito <br>**Feito em 03/10/2026, na parte de leitura.** Quatro rotas (tag Vitrine): `GET /public/products`, `/public/policy`, `/public/availability` e `POST /public/quotes`, no módulo `internal/modules/vitrine`, que reusa o motor do painel. `ValidarTabela` passou a exigir `Motivo` em toda `AcessoPublico` (as seis antigas ganharam o seu), e `TestTodaRotaPublicaDeNegocioFicaAtrasDoLimitador` reprova rota pública fora de `/public/` que não seja sonda nem `/auth`. Limite: 120/min por IP (em memória — D1). O orçamento público recusa `discount_pct`, `persist`, tabela, versão e contato com 422, e não devolve alçada. **Fica para o B1**: `/public/holds` e a questão das duas portas de pré-reserva, que dependem da decisão 8.3 |
| **A2** | O site para de calcular: catálogo, calendário e orçamento passam a vir da API. `data.js` encolhe até morrer | `next-frontend` com `backend-go` | Trocar uma tarifa no painel muda o preço que o site mostra, sem editar arquivo nenhum <br>**Feito em 03/10/2026.** O nginx do site encaminha só `/api/v1/public/*`; `data.js` foi apagado e virou `vitrine.js` (cliente da API + texto editorial). Medido no navegador contra a API servida: orçamento da tela = `POST /public/quotes` centavo a centavo, e um `PATCH /unit-types/{id}` no painel levou o total de R$ 9.150 para R$ 9.273,45 sem tocar em arquivo. O botão falso "Gerar pré-reserva" (que só empurrava a data num array do navegador) saiu: a pré-reserva é pedida pelo WhatsApp até o B1 |

### B — Vender de verdade

| # | O que | Dono | Pronto quando |
| --- | --- | --- | --- |
| **B0** | Limitador de taxa distribuído (dívida **D1** do roadmap): sai o mapa de processo, entra Redis. **Pré-requisito de qualquer rota `/public/*` em produção** — o roadmap e o backlog da Fase 2 o empurravam para a Fase 6, e foram corrigidos em 02/10 | `devops` com `backend-go` | Com duas réplicas da API no ar, a sexta tentativa de login é recusada — hoje ela passa se cair na outra réplica |
| **B1** | Pré-reserva pública: `POST /public/holds` com idempotência, `409` em conflito e expiração automática | `backend-go` | Dois navegadores pedindo a mesma data ao mesmo tempo: um recebe `201`, o outro `409 DATE_CONFLICT`. Teste de concorrência com `-count=10`. A expiração automática depende do worker, que até 02/10 ficava no profile `full` e **não subia** no `make up` nem na fumaça do CI; desde a Rodada 5 ele sobe no stack padrão, e o `make smoke-stack` confere que está de pé (`make worker-vivo`) |
| **B2** | Pagamento do sinal e confirmação automática | `integracoes` | Sinal pago no provedor vira reserva `confirmed` sem ninguém clicar, e o reenvio do webhook não confirma duas vezes |
| **B3** | O lead do site entra no funil com origem `Site` e aparece no kanban em tempo real | `backend-go` | Pré-reserva feita no site aparece no painel sem recarregar a página |

### C — Experiência

| # | O que | Dono | Pronto quando |
| --- | --- | --- | --- |
| **C1** | Área do hóspede (ver reserva, enviar documento, pagar saldo) | a decidir | **Depende da decisão 8.2** — pode não existir |
| **C2** | Unificar a linguagem visual: os tokens do site e o design system do painel | `next-frontend` | Uma cor de marca muda em um lugar e vale nos dois fronts |
| **C3** | Decidir se `apps/site` continua estático ou vira Next.js | `tech-lead` | Decisão escrita com motivo. Hoje estático é suficiente: 3250 linhas que funcionam, sem build, e indexável |

### D — Limpeza (dívida que nasce com a importação)

> Os IDs desta seção são **passos do plano** e colidem por acaso com as dívidas
> D1..D11 do `roadmap.md` (lá, D1 é o limitador de login; aqui, apagar o
> `/admin/`). Quando este documento fala de dívida, escreve "dívida D1 do
> roadmap"; quando fala de passo, escreve "passo D1".

| # | O que | Dono | Pronto quando | Estado |
| --- | --- | --- | --- | --- |
| **D1** | **Apagar `apps/site/public/admin/`** — back-office mocado, 1143 linhas, servido pelo mesmo nginx **sem autenticação nenhuma** e com link "Acesso da gestão" no rodapé das duas páginas públicas (a versão anterior deste item dizia "a quem souber o caminho"; estava no rodapé). Mostrava receita, ticket médio, conversão e funil fictícios. Enquanto o dado é falso é vergonha; no dia em que for real é vazamento | `next-frontend` | O caminho `/admin/` no site responde 404, e o que ele desenhava existe no `apps/admin` | **Feito em 02/10/2026** (Rodada 5). O critério escrito era impossível só apagando a pasta: o `nginx.conf` tinha `try_files $uri $uri/ $uri.html /index.html`, que devolvia a home com **200** para qualquer caminho — medido antes da mudança: `/admin/` 200, `/nao-existe` 200 com a home. Agora `try_files $uri $uri/ =404` com `error_page 404 /404.html`, e uma trava `location ^~ /admin { return 404; }` para o caso de a pasta voltar. Medido depois pelo `squad-lead`: `/admin`, `/admin/`, `/scripts/admin.js` e `/nao-existe` = **404**. A segunda metade do critério (receita, ticket médio e conversão no painel) é o BI da Fase 3 e não existe — os números do mock eram fictícios, então nada real se perdeu. Efeito colateral aceito: `/disponibilidade` sem `.html` passou a dar 404 (nenhum link do site usa essa forma) |
| **D2** | Tirar o controle de desconto da mão do visitante | `next-frontend` | A página pública não tem como pedir desconto, e não informa alçada nenhuma | **Feito em 02/10/2026**. Saíram o controle de 0–15%, a aplicação no total, a mensagem de alçada e a condição que desabilitava a pré-reserva; os comentários do JavaScript, que são servidos ao visitante e descreviam a alçada, foram reescritos. Foi além do item, pela invariante 1 do §5: o `title` e o toast do calendário mostravam **o nome de quem ocupa a data** ("Pré-reserva — Ensaio Vogue PI", "indisponível — Família Andrade") e distinguiam pré-reserva, reservado e bloqueado; agora o público vê só livre ou indisponível. `grep -rniE "al[cç]ada\|desconto" apps/site/public` = zero |
| **D3** | Matar `data.js` por completo | `next-frontend` | O arquivo não existe, e nenhuma tela quebra | **Feito em 03/10/2026, com o A2:** o arquivo não existe (`/scripts/data.js` responde 404) e as duas páginas rodam sem erro de JavaScript no navegador. Histórico: **parcial em 02/10/2026.** O arquivo caiu de 223 para **177 linhas** e não tem mais dado de terceiro nem regra interna (reservas com nome, valor e origem; leads com telefone; funil; etapas do CRM; alçada; caução; inventário). Ficam produtos, tarifa, mínimo de noites, calendário fictício e a política exibida — e eles só morrem com o **A2**, porque é o A2 que põe a API no lugar deles. Prova de que nada mudou para o visitante: calendário e preço antigos contra novos, dia a dia, 2072 produto-dias, zero diferenças |
| **D4** | Registrar `apps/site/**` nas definições dos agentes em `.claude/agents/` | `squad-lead` | O agente de front sabe que a pasta é dele sem alguém lembrar | **Feito em 02/10/2026**. `.claude/agents/next-frontend.md` dá `apps/site/**` ao agente, com as duas regras do site: ele pergunta e não calcula, e nunca expõe dado de terceiro |
| **D5** | Decidir o destino do vídeo de 12 MB em `public/videos/hero.mp4` | `devops` | Ou está em Git LFS, ou está num CDN, ou a decisão de deixá-lo no repositório está escrita | Aberto. São 13.002.875 bytes já no histórico (`957e6e3`), sem `.gitattributes`: mover para LFS ou CDN agora não encolhe o repositório sem reescrever o histórico |
| **D6** | Trocar o número de WhatsApp fictício `5586999999999`. A versão anterior deste item dizia "**cinco** botões em `index.html` e `disponibilidade.html`"; medido, eram **8 ocorrências**: 6 links nos dois HTML, 1 no link de orçamento gerado por `disponibilidade.js` e 1 no `admin.js` | `next-frontend` | Todo botão de contato abre a conversa certa. Melhor ainda se o número vier de configuração, não do HTML | **Parcial em 02/10/2026.** O número vive numa linha só — `set $whv_whatsapp` em `apps/site/nginx.conf:17` — e chega ao HTML por SSI, então os links funcionam sem JavaScript; o botão de orçamento lê um `<meta name="whv:whatsapp">` e some se a página for servida sem SSI, em vez de virar link quebrado. **Falta o número real**, que é decisão do dono (§8, item 7). A fumaça do site avisa que o atual parece fictício, mas não reprova |
| **D7** | Servir as fontes do próprio site. Cormorant Garamond e Jost vinham de `fonts.googleapis.com`/`fonts.gstatic.com`, o que **entrega o IP de cada visitante ao Google** — uma transferência de dado pessoal a terceiro que nenhum aviso de LGPD do site menciona | `next-frontend` | Nenhuma requisição a host externo além de `wa.me` | **Feito em 02/10/2026.** Três `woff2` variáveis, subconjunto latin, 103 KB, com a licença OFL das duas famílias e `font-display: swap`. `grep -rnE "googleapis\|gstatic" apps/site` = zero; a fumaça do site reprova qualquer host externo fora da lista |
| **D8** | O botão "Gerar pré-reserva" responde "Pré-reserva registrada… **A data ficou bloqueada por 48h**" e grava só na memória do navegador | `next-frontend` | O site não promete bloqueio que o banco não fez: ou o botão chama `POST /public/holds` e só diz "bloqueada" com `201`, ou vira um link de WhatsApp com o orçamento até lá | Aberto. Enquanto o site não estiver publicado, é inofensivo; no dia em que estiver, é uma promessa falsa a um cliente. **Depende da decisão 8.3** para saber qual das duas saídas |

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
7. **O número oficial do WhatsApp de reservas.** Hoje o site usa o fictício
   `5586999999999`, numa linha só (`apps/site/nginx.conf:17`). Fecha o passo
   **D6**. Trocar é editar essa linha e subir o site de novo.
8. **O texto e a base legal do consentimento LGPD** do formulário público: o
   que o visitante lê antes de deixar nome, telefone e e-mail, e com que
   finalidade a casa guarda isso. Bloqueia o contrato de `POST /public/leads` e
   da pré-reserva pública (**A1**) e o consentimento do **B3**.

A lista completa de decisões pendentes — estas oito e as do financeiro (caução
dentro ou fora do total, regra e liberação da comissão, retenção da caução,
horários da operação, a alçada de 6 a 10% que não existe no código) — está em
`roadmap.md`, "Decisões pendentes do dono do negócio", com o que cada uma
bloqueia.

## 9. Como rodar os dois juntos hoje

```bash
make up        # site, painel, API, worker e Postgres
make migrate   # schema, num passo separado — nunca no boot
make seed      # produtos, tarifas, política e os usuários de desenvolvimento
make smoke     # fumaça do painel (navegador, login, telas) E do site público
```

Desde a Rodada 5 (02/10/2026), duas coisas mudaram aqui:

- **O worker sobe no `make up`.** Até então ele ficava no profile `full`, que
  nem `make up` nem a fumaça do CI ligavam: a expiração automática que o **B1**
  promete não acontecia em ambiente nenhum que o time roda. O `make smoke-stack`
  confere que ele está de pé e sem reinício (`make worker-vivo`).
- **O site já está na fumaça**, antes do A2. A versão anterior desta seção dizia
  que não havia o que verificar além de o nginx responder — e havia: o fallback
  do nginx devolvia a home com 200 para qualquer caminho, o `/admin/` falso
  respondia, e as fontes vinham do Google. `apps/site/e2e/fumaca-site.mjs`
  (Node puro, sem dependência) reprova em página com status errado, caminho
  inventado sem 404 real, `/admin` respondendo, recurso interno com 404 ou tipo
  errado, host externo fora de `wa.me`, diretiva SSI crua no HTML e número de
  WhatsApp em mais de um lugar. Roda em `make smoke` (junto da do painel; uma
  não esconde a outra), em `make smoke-site`, e no job `site` do CI contra a
  imagem crua, sem o volume de desenvolvimento (`make smoke-site-imagem`). No
  A2, o critério cresce para o que o painel já cumpre: nenhum link interno com
  404 ou 5xx **e** o preço exibido igual ao do `POST /public/quotes`.

O SSI e o `error_page` do `nginx.conf` foram provados no nginx 1.24 local; a
prova na imagem `nginx:1.27-alpine` vem no primeiro CI depois do commit da
Rodada 5.

---

Mudou a decisão? Edite este arquivo primeiro, e só depois o código.
