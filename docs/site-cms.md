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
3. **Topo (capa)** — `inicio.local` (texto: "Praia do Coqueiro · Luís Correia · Piauí"), `inicio.titulo` (titulo), `inicio.texto` (texto_longo), `inicio.video` (video; hoje `/videos/hero.mp4`), `inicio.video-celular` (video, opcional, sem original — versão em pé 9:16 para tela em pé; vazio = celular usa `inicio.video`; pedido do dono em 03/10/2026), `inicio.numeros` (lista de {valor, rotulo}: 24 hóspedes…, 12 acomodações, 300m…).
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

## 4b. Campos acrescentados em 03/10/2026 (rodada 2: "tudo no site")

Pedido do dono: **tudo** o que aparece no site é editável — menu, botões, legendas, avisos do calendário, o orçamento e o formulário de pré-reserva. Esta lista é o contrato para o `catalogo.go`; o site já está marcado com estas chaves (`data-cms*` no HTML, `WH.t()`/`WH.cru()` no JavaScript). **118 chaves novas.**

Regras desta rodada:

- **Marcadores `{nome}`.** Alguns textos levam um número ou nome que vem do sistema (prazo da pré-reserva, sinal, nome da acomodação, data). O gestor escreve a frase e deixa o marcador onde o número deve entrar; o site troca o marcador pelo valor que a API devolveu. Marcador que o gestor apagar simplesmente não aparece — nada quebra. Marcador desconhecido fica como está. O painel avisa quando um marcador do texto original sumiu. Marcadores usados: `{horas}`, `{sinal}`, `{dias}` (política vigente), `{acomodacao}`, `{data}`, `{n}`, `{noites}`, `{mes}`, `{hospedes}`, `{prazo}`, `{codigo}`. A API valida como texto comum (não precisa conhecer os marcadores).
- **Mudança em chave existente:** `chamada.texto` passa a ter original `Consulte o calendário em tempo real, monte o orçamento da sua estadia e garanta a data com uma pré-reserva de {horas} horas.` (é a frase que o visitante vê hoje, com o número da política) e a ajuda explica o `{horas}`. O HTML continua com a frase sem número, usada só quando a política não responde.
- **Texto legal:** `pre-reserva.consentimento` é a autorização da LGPD que o visitante marca. Fica editável (o dono pediu tudo), com ajuda que diz isso; toda gravação já vai para `audit_log`.
- `marca.icone` é imagem aplicada ao `<link rel="icon">` (só o `href`; o alt é ignorado).
- `max` abaixo é o limite sugerido; onde está vazio vale o padrão do tipo.

Seções novas, na ordem do painel: **Menu e botões** (`menu`, logo depois de Marca); **Calendário de disponibilidade** (`calendario`), **Orçamento** (`orcamento`), **Formulário de pré-reserva** (`pre-reserva`) e **Mensagens prontas do WhatsApp** (`whatsapp`), nesta ordem, depois de "Página de disponibilidade" e antes de "Página não encontrada".

### Marca — `marca` (existente)

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `marca.nome` | Nome no topo (linha grande) | texto | 40 | WHITE HOUSE | O nome ao lado do logotipo, no topo de todas as páginas. |
| `marca.subtitulo` | Nome no topo (linha pequena) | texto | 40 | Village | A palavra menor embaixo do nome. |
| `marca.icone` | Ícone da aba do navegador | imagem |  | /assets/logo.png (alt "") | A figurinha que aparece na aba do navegador. Prefira imagem quadrada. |

### Menu e botões — `menu` (nova, logo depois de "Marca")

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `menu.inicio` | Menu: Início | texto | 30 | Início | Item do menu no topo e no menu do celular. |
| `menu.acomodacoes` | Menu: Acomodações | texto | 30 | Acomodações | Item do menu no topo, no celular e no rodapé. |
| `menu.eventos` | Menu: Eventos | texto | 30 | Eventos | Item do menu no topo, no celular e no rodapé. |
| `menu.estrutura` | Menu: Estrutura | texto | 30 | Estrutura | Item do menu no topo, no celular e no rodapé. |
| `menu.disponibilidade` | Menu: Disponibilidade | texto | 30 | Disponibilidade | Item do menu no topo, no celular e no rodapé. |
| `menu.reservar` | Botão do topo (início) | texto | 30 | Reservar | O botão dourado no canto do topo, na página inicial e na página não encontrada. |
| `menu.falar-com-reservas` | Botão de WhatsApp (disponibilidade) | texto | 40 | Falar com reservas | O botão do topo e o da chamada final na página de disponibilidade. |
| `menu.whatsapp-flutuante` | Botão flutuante de WhatsApp | texto | 40 | 💬 Falar com reservas | O botão verde que fica no canto da tela na página inicial. |
| `menu.pular` | Atalho "pular para o conteúdo" | texto | 60 | Pular para o conteúdo | Só aparece para quem navega pelo teclado ou usa leitor de tela. |

### Topo (capa) — `inicio` (existente)

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `inicio.botao-disponibilidade` | Botão principal da capa | texto | 40 | Ver disponibilidade |  |
| `inicio.botao-acomodacoes` | Segundo botão da capa | texto | 40 | Acomodações |  |
| `inicio.rolar` | Convite para rolar a página | texto | 40 | Role para descobrir | A frase pequena no pé da capa. |

### A casa — `casa` (existente)

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `casa.foto-legenda` | Legenda do fundo desenhado | texto | 80 | Pôr do sol · Praia do Coqueiro | Só aparece enquanto não houver foto. |

### Acomodações — `acomodacoes` (existente)

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `acomodacoes.botao` | Botão ao lado do título | texto | 60 | Ver calendário e tarifas → |  |
| `acomodacoes.botao-card` | Botão de cada card | texto | 40 | Ver disponibilidade |  |
| `acomodacoes.preco-sufixo` | Texto ao lado do preço | texto | 60 | / diária · a partir de | O preço vem da tabela de tarifas. |
| `acomodacoes.sob-consulta` | Preço sob consulta | texto | 40 | Sob consulta | No lugar do preço, quando a acomodação não tem preço de tabela. |
| `acomodacoes.sob-consulta-nota` | Nota do preço sob consulta | texto | 60 | fale com a gente |  |
| `acomodacoes.opcoes` | Quantidade de opções | texto | 40 | {n} opções | {n} vira o número de unidades da categoria. |
| `acomodacoes.falha-titulo` | Aviso quando as acomodações não carregam: título | texto | 60 | Acomodações |  |
| `acomodacoes.falha-texto` | Aviso quando as acomodações não carregam: texto | texto_longo | 400 | Não conseguimos carregar as acomodações agora. Consulte datas e valores na central de reservas. |  |

### Localização — `local` (existente)

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `local.foto-legenda` | Legenda do fundo desenhado | texto | 80 | Área da piscina | Só aparece enquanto não houver foto. |

### Chamada final — `chamada` (existente)

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `chamada.botao-datas` | Botão principal | texto | 40 | Consultar datas |  |
| `chamada.botao-whatsapp` | Botão de WhatsApp | texto | 40 | WhatsApp |  |

### Rodapé e contato — `rodape` (existente)

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `rodape.titulo-navegue` | Título da coluna de links | texto | 40 | Navegue |  |
| `rodape.titulo-reservas` | Título da coluna de contato | texto | 40 | Reservas |  |
| `rodape.titulo-endereco` | Título da coluna de endereço | texto | 40 | Endereço |  |
| `rodape.whatsapp` | Texto do link de WhatsApp | texto | 60 | WhatsApp oficial | O número continua sendo configurado à parte. |

### Página de disponibilidade — `disp` (existente)

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `disp.campo-produto` | Nome do campo de acomodação | texto | 40 | Produto |  |
| `disp.campo-hospedes` | Nome do campo de hóspedes | texto | 40 | Hóspedes |  |
| `disp.legenda-livre` | Legenda: livre | texto | 40 | Livre |  |
| `disp.legenda-indisponivel` | Legenda: indisponível | texto | 40 | Indisponível |  |
| `disp.legenda-consulta` | Legenda: sob consulta | texto | 40 | Sob consulta |  |
| `disp.legenda-especial` | Legenda: data especial | texto | 40 | Data especial |  |
| `disp.tarifas.texto` | Texto da tabela de tarifas | texto | 200 | Valores por diária, por produto, da tabela vigente. |  |
| `disp.tarifas.minimos` | Aviso de estadia mínima | texto_longo | 400 | A estadia mínima varia por acomodação e tipo de data — passe o mouse sobre o dia no calendário para ver a de cada data. | Aparece depois do texto da tabela. |
| `disp.tabela-produto` | Tabela: coluna do nome | texto | 40 | Produto | As colunas dos tipos de data (Normal, Feriado…) vêm do sistema de tarifas. |
| `disp.tabela-capacidade` | Tabela: coluna da lotação | texto | 40 | Capacidade |  |
| `disp.tabela-consulta` | Tabela: preço sob consulta | texto | 40 | consulta | Aparece na célula sem preço de tabela. |
| `disp.tabela-pacotes` | Tabela: começo da linha de pacotes | texto | 40 | Pacotes: |  |
| `disp.chamada.texto` | Texto da chamada final | texto_longo | 600 | A pré-reserva bloqueia o calendário por {horas} horas. A confirmação acontece com o sinal de {sinal}%; o saldo vence {dias} dias antes do check-in. | {horas}, {sinal} e {dias} viram os números da política em vigor. Se o sistema não responder, o site mostra uma frase sem números. |
| `disp.chamada.botao-inicio` | Botão "voltar" da chamada final | texto | 40 | Voltar ao início |  |

### Calendário de disponibilidade — `calendario` (nova, depois de "Página de disponibilidade")

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `calendario.carregando` | Enquanto o calendário carrega | texto | 80 | consultando a central… |  |
| `calendario.noite-livre` | Resumo do mês (uma noite) | texto | 120 | {n} noite livre para {acomodacao} | {n} vira o número de noites; {acomodacao}, o nome escolhido. |
| `calendario.noites-livres` | Resumo do mês (várias noites) | texto | 120 | {n} noites livres para {acomodacao} | {n} vira o número de noites; {acomodacao}, o nome escolhido. |
| `calendario.falha` | Aviso quando o calendário não carrega | texto_longo | 400 | A central de reservas está indisponível agora. Tente em instantes ou fale com a gente pelo WhatsApp. |  |
| `calendario.falha-mes` | Aviso quando um mês não carrega | texto | 120 | Não foi possível carregar o calendário. |  |
| `calendario.botao-whatsapp` | Botão de WhatsApp nos avisos | texto | 40 | Falar no WhatsApp |  |
| `calendario.dia-minimo` | Dica do dia: estadia mínima | texto | 60 | mínimo {noites} noites | Aparece ao passar o mouse no dia. {noites} vira o mínimo da tabela. |
| `calendario.dia-consulta` | Dica do dia: sob consulta | texto | 40 | sob consulta |  |
| `calendario.dia-indisponivel` | Dica do dia: indisponível | texto | 40 | Indisponível |  |
| `calendario.aviso-passou` | Aviso: data que já passou | texto | 120 | Data já passou. |  |
| `calendario.aviso-consulta` | Aviso: data sob consulta | texto | 160 | {data} é sob consulta — fale com a gente pelo WhatsApp. | {data} vira a data clicada. |
| `calendario.aviso-indisponivel` | Aviso: data indisponível | texto | 120 | {data} indisponível. | {data} vira a data clicada. |
| `calendario.aviso-longa` | Aviso: estadia muito longa | texto | 160 | Para estadias acima de 90 noites, fale com a gente pelo WhatsApp. |  |
| `calendario.aviso-intervalo` | Aviso: datas ocupadas no meio | texto | 160 | Há datas ocupadas no intervalo. Escolha um novo check-in. |  |
| `calendario.kpi-livres` | Quadro: noites livres | texto | 60 | Noites livres em {mes} | {mes} vira o nome do mês. |
| `calendario.kpi-diaria` | Quadro: menor diária | texto | 60 | Diária a partir de |  |
| `calendario.kpi-diaria-nota` | Quadro: nota da menor diária | texto | 60 | no mês selecionado |  |
| `calendario.kpi-sinal` | Quadro: sinal | texto | 60 | Sinal para confirmar |  |
| `calendario.kpi-sinal-nota` | Quadro: nota do sinal | texto | 60 | saldo até {dias} dias antes | {dias} vira o prazo do saldo. |
| `calendario.kpi-pre-reserva` | Quadro: pré-reserva | texto | 60 | Pré-reserva sem pagamento |  |
| `calendario.kpi-pre-reserva-nota` | Quadro: nota da pré-reserva | texto | 60 | a data fica segura |  |

### Orçamento — `orcamento` (nova, depois de "Calendário de disponibilidade")

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `orcamento.titulo` | Título do quadro | texto | 60 | Seu orçamento |  |
| `orcamento.selo` | Etiqueta do quadro | texto | 40 | Tabela vigente |  |
| `orcamento.detalhes` | Linha de lotação e horários | texto | 160 | até {hospedes} hóspedes · check-in 14h · check-out 11h | {hospedes} vira a lotação da acomodação. Os horários de entrada e saída estão escritos aqui. |
| `orcamento.check-in` | Rótulo: entrada | texto | 30 | Check-in | Também usado na mensagem pronta do WhatsApp. |
| `orcamento.check-out` | Rótulo: saída | texto | 30 | Check-out | Também usado na mensagem pronta do WhatsApp. |
| `orcamento.escolha-entrada` | Instrução: escolher a entrada | texto | 160 | Selecione a data de check-in no calendário para ver o valor da estadia. |  |
| `orcamento.escolha-saida` | Instrução: escolher a saída | texto | 160 | Agora selecione a data de check-out no calendário. |  |
| `orcamento.calculando` | Enquanto calcula | texto | 80 | Calculando com a tabela vigente… |  |
| `orcamento.sinal` | Resumo: sinal | texto | 60 | Sinal para confirmar |  |
| `orcamento.saldo` | Resumo: saldo | texto | 40 | Saldo |  |
| `orcamento.saldo-prazo` | Resumo: prazo do saldo | texto | 60 | até {dias} dias antes | {dias} vira o prazo do saldo. |
| `orcamento.pre-reserva` | Resumo: pré-reserva | texto | 40 | Pré-reserva |  |
| `orcamento.pre-reserva-prazo` | Resumo: prazo da pré-reserva | texto | 40 | segura {horas}h | {horas} vira o prazo da pré-reserva. |
| `orcamento.consulta-produto` | Acomodação sob consulta | texto_longo | 300 | {acomodacao} tem valores sob consulta: cada pedido é montado com a nossa equipe. | {acomodacao} vira o nome escolhido. |
| `orcamento.consulta-data` | Data sob consulta | texto_longo | 300 | {data} tem valores sob consulta para {acomodacao}. | {data} vira a data; {acomodacao}, o nome. |
| `orcamento.consulta-datas` | Período sob consulta | texto_longo | 300 | Essas datas têm valores sob consulta. Fale com a gente e montamos o seu pedido. |  |
| `orcamento.botao-consultar` | Botão: consultar no WhatsApp | texto | 40 | Consultar no WhatsApp |  |
| `orcamento.limpeza` | Linha da taxa de limpeza | texto | 60 | Taxa de limpeza | O valor vem da tabela. |
| `orcamento.total` | Rótulo do total | texto | 30 | Total |  |
| `orcamento.sinal-valor` | Linha do sinal | texto | 60 | Sinal ({sinal}%) para confirmar | {sinal} vira a porcentagem do sinal. |
| `orcamento.saldo-valor` | Linha do saldo | texto | 60 | Saldo até {dias} dias antes | {dias} vira o prazo do saldo. |
| `orcamento.diaria-media` | Linha da diária média | texto | 40 | Diária média |  |
| `orcamento.botao-whatsapp` | Botão: preferir o WhatsApp | texto | 40 | Prefiro falar no WhatsApp |  |
| `orcamento.nota` | Nota do orçamento | texto_longo | 400 | Valores da tabela vigente. A pré-reserva segura a data por {horas}h; sem o sinal, a data é liberada automaticamente. | {horas} vira o prazo da pré-reserva. |

### Formulário de pré-reserva — `pre-reserva` (nova, depois de "Orçamento")

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `pre-reserva.titulo` | Título do formulário | texto | 60 | Garanta a data agora |  |
| `pre-reserva.nome` | Campo: nome | texto | 40 | Nome completo |  |
| `pre-reserva.whatsapp` | Campo: WhatsApp | texto | 40 | WhatsApp com DDD |  |
| `pre-reserva.whatsapp-exemplo` | Exemplo dentro do campo de WhatsApp | texto | 30 | (86) 99999-9999 | O texto cinza que some quando a pessoa começa a digitar. |
| `pre-reserva.email` | Campo: e-mail | texto | 40 | E-mail |  |
| `pre-reserva.opcional` | Aviso de campo opcional | texto | 30 | (opcional) |  |
| `pre-reserva.consentimento` | Autorização de uso dos dados | texto_longo | 500 | Autorizo a White House a usar meus dados para esta reserva e para falar comigo sobre ela. | Texto com valor legal (Lei Geral de Proteção de Dados): a pessoa marca esta caixa para autorizar. Mude só com orientação jurídica; a alteração fica registrada no histórico. |
| `pre-reserva.consentimento-falta` | Aviso: autorização não marcada | texto | 120 | é preciso aceitar para reservar. |  |
| `pre-reserva.botao` | Botão de enviar | texto | 40 | Fazer pré-reserva |  |
| `pre-reserva.enviando` | Botão enquanto envia | texto | 40 | Reservando… |  |
| `pre-reserva.nota` | Nota embaixo do botão | texto_longo | 300 | Sem pagamento agora. A data fica segura por {horas}h; para confirmar, paga-se o sinal de {sinal}%. | {horas} e {sinal} viram os números da política em vigor. |
| `pre-reserva.erro-conflito` | Aviso: data tomada por outra pessoa | texto_longo | 300 | Essas datas acabaram de ser reservadas por outra pessoa. Escolha outras no calendário. |  |
| `pre-reserva.erro-tentativas` | Aviso: tentativas demais | texto_longo | 300 | Muitas tentativas seguidas. Tente de novo mais tarde ou fale com a gente pelo WhatsApp. |  |
| `pre-reserva.erro-conexao` | Aviso: sem conexão | texto_longo | 300 | Sem conexão. Tente de novo — se a primeira tentativa chegou, a mesma pré-reserva é devolvida, sem duplicar. |  |
| `pre-reserva.erro-geral` | Aviso: outro problema | texto | 160 | Não foi possível concluir agora. |  |
| `pre-reserva.ok-rotulo` | Confirmação: rótulo | texto | 40 | Pré-reserva feita | Só aparece depois que a data foi de fato segurada. |
| `pre-reserva.ok-texto` | Confirmação: frase | texto | 200 | {acomodacao} está segura para você até {prazo}. | {acomodacao} vira o nome; {prazo}, a data e hora limite (em negrito). |
| `pre-reserva.ok-proximo` | Confirmação: próximo passo | texto_longo | 500 | Próximo passo: pagar o sinal até o prazo. Envie o código pelo WhatsApp e a nossa equipe passa os dados do pagamento. Sem o sinal, a data é liberada automaticamente. |  |
| `pre-reserva.ok-botao-whatsapp` | Confirmação: botão de WhatsApp | texto | 60 | Enviar o código no WhatsApp |  |
| `pre-reserva.ok-botao-nova` | Confirmação: botão de nova consulta | texto | 40 | Fazer outra consulta |  |

### Mensagens prontas do WhatsApp — `whatsapp` (nova, depois de "Formulário de pré-reserva")

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `whatsapp.mensagem-consulta` | Primeira linha: pedido de valores | texto | 200 | Olá! Quero consultar valores na White House: | A mensagem que já vem escrita quando a pessoa toca em "Consultar no WhatsApp". As datas e a acomodação entram embaixo. |
| `whatsapp.mensagem-orcamento` | Primeira linha: orçamento pronto | texto | 200 | Olá! Quero pré-reservar na White House: | Em "Prefiro falar no WhatsApp". O orçamento entra embaixo. |
| `whatsapp.mensagem-pre-reserva` | Primeira linha: depois da pré-reserva | texto | 200 | Olá! Fiz a pré-reserva *{codigo}* no site da White House. | {codigo} vira o código da pré-reserva. No WhatsApp, *texto* fica em negrito. |
| `whatsapp.pergunta-sinal` | Última linha: pergunta do sinal | texto | 200 | Como faço o pagamento do sinal? |  |

### Google e redes — `seo` (existente)

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `seo.erro.titulo` | Título da página não encontrada | texto | 200 | Página não encontrada \| White House · Praia do Coqueiro | Aparece na aba do navegador. |

### Página não encontrada — `erro` (existente)

| Chave | Rótulo no painel | Tipo | max | Original | Ajuda |
| --- | --- | --- | --- | --- | --- |
| `erro.rotulo` | Rótulo acima do título | texto | 40 | Erro 404 |  |
| `erro.botao-disponibilidade` | Botão principal | texto | 40 | Ver disponibilidade |  |
| `erro.botao-inicio` | Botão "voltar" | texto | 40 | Voltar ao início |  |

**Ficaram de fora, de propósito:** os nomes dos tipos de data (Normal, Fim de semana, Feriado… — vêm do tarifário), nomes de meses e dias da semana do calendário, as setas `←`/`→`, e fragmentos gramaticais colados a números do sistema (`hóspede`/`hóspedes`, `noite`/`noites`, `N hóspedes` na tabela, `N diárias` dos pacotes, `até N hóspedes`/`sob consulta` dentro da lista de acomodações, `cons.` na célula do dia, "Carregando…" na dica do dia, as linhas "Hóspedes:/Total:/Sinal:" das mensagens do WhatsApp). Editar um pedaço de palavra sem a frase em volta mais confunde do que ajuda. Também ficam de fora os textos só para leitor de tela que não aparecem (`aria-label` do menu e das setas) e a frase usada quando o número do WhatsApp não está configurado.

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
  A imagem da API (distroless, `nonroot` uid 65532) já traz `/data/midia` com
  esse dono; o volume novo herda na primeira montagem. Só a API monta (o worker
  não). Nome no host: `whv-gestao_midia` na VPS, `whitehousevillage_midia` no dev.
  Sobrevive a todo redeploy; o `deploy.sh` confere o mount ao fim.
- **Na VPS (passo manual do dono, uma vez):** em `/etc/nginx/sites-available/whv-gestao`,
  no(s) bloco(s) `server` de `gestor`/`corretor` (o que tem
  `proxy_pass http://127.0.0.1:3110`), uma linha `client_max_body_size 300m;`
  abaixo do `server_name`; depois `sudo nginx -t && sudo systemctl reload nginx`.
  O padrão (1 MB) recusa qualquer foto com `413`. O bloco do `www` não muda (só
  GET), nem nada fora desse arquivo. Passo a passo e conferência:
  `docs/infra.md` §0, "Nginx do host já configurado?".
- Site (nginx de `apps/site`): `/api/v1/public/media/` tem location própria —
  sem buffer (vídeo passa em fluxo), `Range`/`If-Range` repassados, 206 e
  `Content-Range` da API intactos, e **sem** o `Cache-Control: no-store` da
  vitrine (que somado ao `immutable` da API anularia o cache).
- Backup: `make backup` grava o dump e o tar do volume com o mesmo carimbo; na
  VPS, comandos em `docs/infra.md` §5.1.
- Prazos que dependem da API (código da aplicação, não da infra): o
  `http.Server` de `cmd/api` tem `ReadTimeout` 30 s e `WriteTimeout` 60 s. O
  upload de 300 MB e o envio de um vídeo a cliente lento precisam estender o
  prazo **na própria rota** (`http.ResponseController.SetReadDeadline` /
  `SetWriteDeadline`). Na VPS o buffer do nginx do host esconde isso; no dev,
  sem nginx do host na frente do painel, não esconde.
