# `apps/site` — front de cliente

Site público do White House Village: é por aqui que o hóspede conhece a casa,
consulta datas e — quando a unificação terminar — **faz a reserva**. O painel
administrativo é o `apps/admin`; a regra de negócio é a `apps/api`.

> **Desde 03/10/2026 o site pergunta, não calcula** (passo A2 de
> [`docs/unificacao-site-crm.md`](../../docs/unificacao-site-crm.md)). Catálogo,
> calendário, orçamento, estadias mínimas, sinal e prazos vêm de
> `/api/v1/public/*` — a MESMA API e o MESMO motor do painel. Trocar uma tarifa
> ou a taxa de limpeza no painel muda o que o visitante vê, sem editar arquivo
> nenhum (medido no navegador: R$ 9.150 → R$ 9.273,45 depois de um PATCH). O
> `data.js` morreu. Se a API não responde, a página diz isso e oferece o
> WhatsApp — ela nunca inventa preço.

## Subir

O site entra junto com o resto do stack:

```bash
make up          # site em http://localhost:${SITE_PORT:-3200}
```

`public/` é montado como volume no desenvolvimento: editar um arquivo e dar F5
já reflete, sem rebuild. O `nginx.conf` **não** é montado — ele vai para dentro
da imagem, então mudar o nginx pede `make up` de novo (o alvo já faz `--build`).

## Páginas

| Rota | O que é |
| --- | --- |
| `/` | Home: hero em vídeo, as acomodações por categoria (Duplex, Pool Suítes, Grand Villa, Classic Villa, Completa), eventos, estrutura, localização |
| `/disponibilidade.html` | Consulta de datas, orçamento do hóspede e tabela de tarifas |
| qualquer outro caminho | **404 de verdade**, com `public/404.html` (via `error_page`) |

Deep link que a página de disponibilidade entende:

```
/disponibilidade.html?produto=grand-villa&checkin=2026-11-20&checkout=2026-11-23
```

**`/admin` não existe mais.** O back-office mocado (`public/admin/`,
`scripts/admin.js`, `styles/admin.css` — 1143 linhas servidas sem autenticação,
com receita, conversão e funil fictícios) foi apagado no passo D1. O
`nginx.conf` ainda tem uma trava `location ^~ /admin { return 404; }`, para que
uma pasta `admin/` que reapareça (merge antigo, cópia esquecida no volume de dev)
não vá ao ar. O painel de gestão é o `apps/admin`.

O fallback antigo (`try_files ... /index.html`) devolvia a home com 200 para
qualquer caminho; saiu, junto com o `$uri.html`. Consequência: `/disponibilidade`
sem `.html` agora é 404 — os links do site sempre usaram `.html`.

## Estrutura

```
public/
  index.html              home
  disponibilidade.html    consulta de datas e orçamento
  404.html                página de erro (servida pelo error_page, nunca direto)
  styles/
    fonts.css             @font-face das fontes locais
    tokens.css, base.css, components.css, home.css, disponibilidade.css, erro.css
  scripts/
    vitrine.js            cliente da API pública + texto editorial das acomodações
    main.js               header, menu, reveals, cards da home (catálogo da API)
    disponibilidade.js    calendário, orçamento, indicadores (tudo da API)
  fonts/                  Cormorant Garamond e Jost (woff2) + licenças OFL
  assets/                 logo
  videos/hero.mp4         vídeo da home (12 MB — destino em aberto, passo D5)
e2e/fumaca-site.mjs       fumaça HTTP do site (Node 22, sem dependência)
nginx.conf                404 real, trava em /admin, SSI do WhatsApp, proxy SÓ de /api/v1/public/
Dockerfile                nginx:1.27-alpine, sem build
```

## O que a página pública não mostra

A resposta pública **nunca** carrega dado de terceiro nem regra comercial interna
(invariante 1 do plano). Na prática, hoje:

- **Sem negociação de preço.** Não há controle de desconto, nem aviso de quem
  aprovaria um (passo D2). O preço é o da tabela.
- **Dia indisponível é só "indisponível".** A API pública manda `available:
  false` e mais nada: nem quem ocupa, nem por quê, nem quantas unidades sobram
  (a contagem somada no ano seria a taxa de ocupação da casa).
- **O orçamento público não negocia.** `POST /api/v1/public/quotes` não tem
  campo de desconto — mandar `discount_pct` é 422 — e a resposta não traz
  alçada, versão de política nem tabela.

## WhatsApp: um número, uma linha

O número de reservas existe **uma vez** no site inteiro: a variável
`$whv_whatsapp` no `nginx.conf`. As páginas recebem o valor por SSI
(`<!--# echo var='whv_whatsapp' -->`), então todo botão de contato funciona sem
JavaScript; o link "Enviar orçamento no WhatsApp", montado em JS, lê o mesmo
valor do `<meta name="whv:whatsapp">`.

> **PENDENTE:** o número atual é o **fictício** herdado do MVP. O real depende do
> dono do negócio. Trocar é editar aquela linha e rodar `make up`.

Consequência do SSI: as páginas só ficam certas servidas por **este** nginx.
Abertas por `file://` ou por outro servidor estático, os links do WhatsApp saem
com o comentário SSI cru (e o botão do orçamento some de propósito). A fumaça
reprova se isso acontecer no servidor de verdade.

## Fontes locais

Cormorant Garamond e Jost são servidas de `public/fonts/`, e não mais pelos
servidores do Google Fonts: carregar de lá entregava o IP de cada visitante a um
terceiro antes de qualquer consentimento (LGPD).

São três arquivos woff2 **variáveis**, só do subconjunto `latin` (nenhum
caractere do site cai em outro), com os intervalos de peso realmente usados —
medidos no navegador em todas as telas: Cormorant normal 300–600, Cormorant
itálico 400, Jost 300–500. A origem, a versão e o motivo de cada escolha estão no
cabeçalho de `public/styles/fonts.css`. Licença: SIL Open Font License 1.1, em
`public/fonts/OFL-cormorant-garamond.txt` e `public/fonts/OFL-jost.txt`.

## Fumaça

```bash
node apps/site/e2e/fumaca-site.mjs                        # padrão: http://localhost:3200
node apps/site/e2e/fumaca-site.mjs http://localhost:3299  # ou SITE_URL=...
```

Roda contra o que **está no ar** e sai com 1 se qualquer critério reprovar.
Confere: as duas páginas com 200; `/admin`, `/admin/`, `/scripts/admin.js` e
`/styles/admin.css` com 404; caminho inventado com 404 e a página de erro do
site; todo recurso interno (inclusive as fontes, achadas dentro dos CSS) com 200
e Content-Type certo; nenhum host externo além de `wa.me`/`api.whatsapp.com`; e
o WhatsApp — sem SSI cru, o mesmo número em todos os links e **uma** ocorrência
dele no código-fonte de `apps/site`. O porquê de cada critério está no topo do
script.

## A API pelo site

O nginx encaminha **só** `/api/v1/public/*` para `api:8080`, pela mesma origem
(sem CORS e sem a API ganhar nome no DNS). Qualquer outro `/api/...` pelo site é
404 — a fumaça confere `/api/v1/auth/me`, `/reservations` e `/users`. O nome
`api` é resolvido por pedido (`resolver` do Docker), para o site subir mesmo com
a API fora do ar. O IP do visitante segue em `X-Forwarded-For`: é por ele que a
API limita a vitrine (120 pedidos por minuto por IP).

As quatro rotas: `GET /public/products`, `GET /public/policy`,
`GET /public/availability` (janela de até 93 dias, entre 31 dias atrás e 548 à
frente) e `POST /public/quotes`. Contrato em `apps/api/openapi/openapi.yaml`, tag
Vitrine.

## Regra que não se negocia aqui

Esta pasta **não decide preço, não decide disponibilidade e não decide política**.
Ela pergunta. O único texto daqui é editorial (selo, diferenciais e descrição de
cada acomodação, em `vitrine.js`); número de preço, prazo ou regra escrito nesta
pasta é uma segunda fonte da verdade competindo com o banco — e a que o cliente vê.
