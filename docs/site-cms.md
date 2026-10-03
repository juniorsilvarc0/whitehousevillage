# Área administrativa do site (menu "Site" no painel)

Pedido do dono em 03/10/2026: *"preciso em gestor o menu site, onde é a área
administrativa do site, onde o gestor possa editar tudo no site — textos,
imagens, vídeos; tudo que tem no site deve ser possível alterar através da área
administrativa"*.

Este documento é o contrato da fatia. Banco, API, painel, site e infra
implementam o que está aqui; divergência se resolve mudando este documento
primeiro.

## 1. Decisões

| Tema | Decisão | Por quê |
| --- | --- | --- |
| Onde mora o conteúdo | Banco (`site_content`), uma linha por campo editado. Campo sem linha = texto original. | O site continua funcionando com o HTML de hoje se a API cair; "restaurar o original" é apagar a linha. |
| Quais campos existem | Um **catálogo de campos** em código na API (`internal/modules/site/catalogo.go`): chave, seção, rótulo, tipo, ajuda e valor original. | Uma fonte só: o painel desenha o formulário a partir dele; o site sabe onde pôr cada chave; o teste confere que toda chave do HTML existe no catálogo e vice-versa. |
| Publicação | **Salvar = publicar** (sem rascunho). Cada campo tem "Restaurar original". Toda gravação vai para `audit_log`. | O gestor é leigo: um botão, efeito imediato. Rascunho/versões fica para depois, se pedirem. |
| Destaque em títulos | Texto entre asteriscos vira itálico dourado (`*exclusividade*` → `<em>exclusividade</em>`); quebra de linha vira `<br>`. Nada de HTML digitado. | Os títulos de hoje usam `<em>`. O site **escapa** tudo e só então aplica essas duas regras — conteúdo do banco nunca vira HTML arbitrário. |
| Fotos e vídeos | Arquivo enviado pelo painel, guardado em disco num volume próprio da API (`/data/midia`), registrado em `site_media`. O id do arquivo é imutável; trocar a foto = novo arquivo, o campo aponta para o novo id. | Simples, sem serviço externo. Id imutável permite cache longo no navegador. |
| Limites | Imagem: JPEG, PNG ou WebP até **15 MB**. Vídeo: MP4 ou WebM até **300 MB**. Tipo conferido pelos bytes do arquivo, não pela extensão. | Foto de celular cabe; vídeo de capa de alguns minutos cabe. |
| Como o site aplica | O HTML mantém o texto original. `scripts/conteudo.js` busca `GET /api/v1/public/site` e troca o que estiver marcado com `data-cms*`. | Sem build, sem servidor novo; se a API não responder, o visitante vê o texto original. |
| Quem pode | Recurso RBAC novo `site`, ações `ver` e `editar` (escopo `all`). Seed concede ao perfil de gestão e ao admin. | RBAC é dado (regra 8). |

## 2. Banco

```sql
site_content (
  key         text PRIMARY KEY,          -- chave do catálogo, ex. 'inicio.titulo'
  value       jsonb NOT NULL,            -- string, {media_id}, ou lista (ver tipos)
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES users(id)
)

site_media (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  kind          text NOT NULL CHECK (kind IN ('imagem','video')),
  mime          text NOT NULL,
  bytes         bigint NOT NULL CHECK (bytes > 0),
  original_name text NOT NULL,
  storage_key   text NOT NULL UNIQUE,    -- nome do arquivo no volume
  created_at    timestamptz NOT NULL DEFAULT now(),
  created_by    uuid REFERENCES users(id)
)
```

Não multi-propriedade (CLAUDE.md: não é multi-tenant). Recurso RBAC `site`
registrado no catálogo de recursos com ações `ver`, `editar`, sem suporte a
`own`.

## 3. Tipos de campo

| Tipo | Valor gravado | No painel | No site |
| --- | --- | --- | --- |
| `texto` | string (≤ 200) | input de uma linha | `textContent` |
| `texto_longo` | string (≤ 2000) | área de texto | parágrafos (linha em branco = novo `<p>`) |
| `titulo` | string (≤ 200), aceita `*destaque*` e quebra de linha | input com dica do asterisco | escapado + `<em>`/`<br>` |
| `imagem` | `{"media_id": "<uuid>", "alt": "..."}` | envio com prévia, texto alternativo | `src` de `<img>` ou `background-image` do bloco |
| `video` | `{"media_id": "<uuid>"}` | envio com barra de progresso | `src` do `<video>` |
| `lista` | array de objetos com subcampos do catálogo | linhas com adicionar/remover/reordenar | renderizada por um template do site |

## 4. Catálogo de campos (chaves estáveis)

O valor original de cada campo é o texto que está hoje no HTML/JS do site
(`apps/site/public`). Seções na ordem em que aparecem no painel:

1. **Google e redes** — `seo.inicio.titulo` (texto), `seo.inicio.descricao` (texto_longo), `seo.disponibilidade.titulo`, `seo.disponibilidade.descricao`.
2. **Marca** — `marca.logo` (imagem; hoje `/assets/logo.png`).
3. **Topo (capa)** — `inicio.local` (texto: "Praia do Coqueiro · Luís Correia · Piauí"), `inicio.titulo` (titulo), `inicio.texto` (texto_longo), `inicio.video` (video; hoje `/videos/hero.mp4`), `inicio.numeros` (lista de {valor, rotulo}: 24 hóspedes…, 12 acomodações, 300m…).
4. **Faixa de temas** — `faixa.itens` (lista de {texto}).
5. **A casa** — `casa.rotulo` ("A casa"), `casa.titulo` (titulo), `casa.texto` (texto_longo, dois parágrafos), `casa.foto` (imagem; hoje cena "Pôr do sol"), `casa.numeros` (lista {valor, rotulo}).
6. **Acomodações** — `acomodacoes.rotulo`, `acomodacoes.titulo`, `acomodacoes.texto`; e por categoria (`duplex`, `suites`, `grand-villa`, `classic-villa`, `completa`): `categoria.<c>.titulo`, `categoria.<c>.selo`, `categoria.<c>.descricao` (texto_longo), `categoria.<c>.itens` (lista {texto} — os "specs"), `categoria.<c>.foto` (imagem). Preço, lotação e disponibilidade **não** são campos: vêm do tarifário (o site pergunta, não calcula).
7. **Eventos** — `eventos.rotulo`, `eventos.titulo`, `eventos.texto`, `eventos.cards` (lista de {titulo, texto, itens (texto com um item por linha), foto (imagem)}).
8. **Estrutura** — `estrutura.rotulo`, `estrutura.titulo`, `estrutura.texto`, `estrutura.itens` (lista de {icone (texto, um emoji), titulo, texto}).
9. **Localização** — `local.rotulo`, `local.titulo`, `local.texto`, `local.itens` (lista {destaque, texto}), `local.foto` (imagem).
10. **Chamada final** — `chamada.titulo`, `chamada.texto`.
11. **Rodapé e contato** — `rodape.texto`, `rodape.email`, `rodape.horario`, `rodape.endereco` (texto_longo, uma linha por linha do endereço), `rodape.copyright`.
12. **Página de disponibilidade** — `disp.rotulo`, `disp.titulo`, `disp.texto`, `disp.tarifas.rotulo`, `disp.tarifas.titulo`, `disp.chamada.titulo`.
13. **Página não encontrada** — `erro.titulo`, `erro.texto`.

O número do WhatsApp **não** entra no catálogo nesta fatia: ele vive no
`nginx.conf` (SSI, os botões funcionam sem JavaScript) e trocá-lo é raro.

## 5. API

Rotas autenticadas (painel):

| Rota | Permissão | O que faz |
| --- | --- | --- |
| `GET /site/content` | `site:ver` | `{data: {sections: [{key, label, fields: [...]}]}}` — catálogo inteiro agrupado por seção, cada campo com `key, label, kind, help, max, value, default_value, is_default`; campo `lista` traz também `item_fields: [{key, label, kind}]` (subtipos: `texto`, `texto_longo`, `imagem`); mídia resolvida para `{media_id, url, alt}` (vídeo: `{media_id, url}`). |
| `PUT /site/content/{key}` | `site:editar` | Grava o valor (validado pelo tipo do catálogo; chave fora do catálogo = 404; mídia inexistente = 422). Devolve o campo. |
| `DELETE /site/content/{key}` | `site:editar` | Restaura o original (apaga a linha). 204. |
| `POST /site/media` | `site:editar` | `multipart/form-data`, campo `file`. Valida tipo pelos bytes e tamanho. Devolve `{id, kind, mime, bytes, url}`. 201. |

Rotas públicas (site), com `Motivo` e no limitador:

| Rota | O que faz |
| --- | --- |
| `GET /public/site` | `{"data": {"values": {key: valor}}}` só com o que **foi editado** (o site já tem o original no HTML). Mídia como `{url, alt}`. `Cache-Control: public, max-age=60`. |
| `GET /public/media/{id}` | O arquivo, com `http.ServeContent` (suporta Range, necessário para vídeo). `Cache-Control: public, max-age=31536000, immutable`. **Fora** do limitador de 120/min da vitrine (um vídeo faz dezenas de pedidos Range), com limitador próprio mais largo. |

Erros de upload: `422 VALIDATION_ERROR` com `details.file` em linguagem do
gestor ("A foto precisa ser JPG, PNG ou WebP, até 15 MB.").

## 6. Painel

Menu **Site** (ícone de globo) → `/app/site`. Uma página com as seções do
catálogo, cada uma recolhível. Cada campo mostra o valor atual, um aviso
"texto original" ou "editado", **Salvar** e **Restaurar original**. Foto:
prévia, **Trocar foto**, texto alternativo. Vídeo: prévia, barra de progresso no
envio. Listas: adicionar, remover, subir/descer. Botão **Ver no site** abre
`https://www.whitehousevillage.com.br` numa aba nova. Linguagem de gestor
(sem termos de TI).

Envio de arquivo: navegador → rota do BFF (`app/api/site/midia/route.ts`,
repassando o corpo em fluxo para a API) — Server Action tem limite de 1 MB.

## 7. Site

`scripts/conteudo.js`, carregado antes de `vitrine.js`/`main.js` nas três
páginas. Marcações:

- `data-cms="chave"` → texto; `data-cms-titulo="chave"` → título com destaque;
  `data-cms-paragrafos="chave"` → texto longo;
- `data-cms-img="chave"` → `<img>` (src/alt) ou bloco `.scene` (fundo com a foto,
  some o rótulo de cena);
- `data-cms-video="chave"` → `<video>`;
- `data-cms-lista="chave"` + `<template>` filho → lista.

As categorias de acomodação (hoje `CATEGORIAS` em `vitrine.js`) passam a ler os
campos `categoria.<c>.*` quando editados.

## 8. Infra

- Volume `midia` montado em `/data/midia` no contêiner da API (dev e prod),
  variável `MEDIA_DIR=/data/midia`.
- **Na VPS (passo manual do dono, uma vez):** o nginx do host que atende
  `gestor.whitehousevillage.com.br` precisa de `client_max_body_size 300m;`
  — o padrão (1 MB) recusa qualquer foto. O site público não precisa (só GET).
- Backup: o volume `midia` entra no backup junto com o banco.
