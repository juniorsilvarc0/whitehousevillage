# `apps/site` — front de cliente

Site público do White House Village: é por aqui que o hóspede conhece a casa,
consulta datas e — quando a unificação terminar — **faz a reserva**. O painel
administrativo é o `apps/admin`; a regra de negócio é a `apps/api`.

> **Estado atual: dados mocados.** Veio do MVP de apresentação
> (`/Users/junior/DEV/spincode/whitehouse`, importado em 02/10/2026) e **ainda
> não fala com a API**. Tarifa, disponibilidade, estadia mínima e sinal são
> calculados em JavaScript, no navegador, a partir de `public/scripts/data.js`.
> Isso contraria as regras 1, 4 e 7 do `CLAUDE.md` e é exatamente o que a
> unificação desfaz — o plano, a ordem e o critério de pronto de cada passo estão
> em [`docs/unificacao-site-crm.md`](../../docs/unificacao-site-crm.md).
>
> Enquanto o passo A2 não entrar, **nenhum número desta pasta vale como preço**.

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
| `/` | Home: hero em vídeo, as quatro acomodações, eventos, estrutura, localização |
| `/disponibilidade.html` | Consulta de datas, orçamento do hóspede e tabela de tarifas |
| qualquer outro caminho | **404 de verdade**, com `public/404.html` (via `error_page`) |

Deep link que a página de disponibilidade entende:

```
/disponibilidade.html?produto=cobertura&checkin=2026-11-20&checkout=2026-11-23
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
    data.js               MOCK: produtos, tarifas, calendário, política exibida
    main.js               header, menu, reveals, cards da home
    disponibilidade.js    calendário, orçamento, indicadores
  fonts/                  Cormorant Garamond e Jost (woff2) + licenças OFL
  assets/                 logo
  videos/hero.mp4         vídeo da home (12 MB — destino em aberto, passo D5)
e2e/fumaca-site.mjs       fumaça HTTP do site (Node 22, sem dependência)
nginx.conf                404 real, trava em /admin, SSI do WhatsApp, cache e gzip
Dockerfile                nginx:1.27-alpine, sem build
```

## O que a página pública não mostra

A resposta pública **nunca** carrega dado de terceiro nem regra comercial interna
(invariante 1 do plano). Na prática, hoje:

- **Sem negociação de preço.** Não há controle de desconto, nem aviso de quem
  aprovaria um (passo D2). O preço é o da tabela.
- **Dia indisponível é só "indisponível".** Nem o nome de quem ocupa (o
  calendário mostrava, no `title` e no toast), nem se é reserva, pré-reserva ou
  bloqueio. O mock `data.js` guarda só produto e período.
- **`data.js` é público** — qualquer um baixa `/scripts/data.js`. Por isso ele
  não tem mais reservas com nome e valor, leads com telefone, funil nem regra
  interna de negociação: saíram com o back-office (passo D3, parcial).

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

## Regra que não se negocia aqui

Esta pasta **não decide preço, não decide disponibilidade e não decide política**.
Ela pergunta. Enquanto `data.js` existir, cada número que ele devolve é uma
segunda fonte da verdade competindo com o banco — e a que o cliente vê.
