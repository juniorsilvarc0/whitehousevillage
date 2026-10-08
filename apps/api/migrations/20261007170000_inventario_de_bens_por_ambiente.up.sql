-- Fase 5 — inventário de bens físicos por ambiente (docs/db.md §11).
--
-- §11 projetava duas tabelas desde 20/08/2026 e nenhuma foi criada:
--
--     inventory_items(id, property_id, name, category, unit_measure, min_stock, cost_cents)
--     unit_inventory(unit_id, item_id, standard_qty)
--
-- O que faltava ali, e é o que o dono pediu agora, são duas dimensões: o
-- AMBIENTE (o enxoval não está "no AP-01", está na cozinha do AP-01 — quem
-- confere caminha cômodo a cômodo, não unidade a unidade) e a FOTO (quem
-- recebe a lista precisa reconhecer a taça, não ler "taça de vinho tinto nº 2").
-- Por isso `unit_inventory` vira `room_inventory`: a colocação passa a pendurar
-- no cômodo, e o cômodo é `unit_rooms` — conceito que não existia em lugar
-- nenhum deste banco.
--
-- `min_stock`/`cost_cents` de §11 não entram como estavam: estoque mínimo é
-- reposição (`stock_movements`, que continua projeto) e `cost_cents` sem sufixo
-- de propósito virou `replacement_cost_cents`, porque o número que a operação
-- usa é o de REPOR o prato quebrado, não o de comprá-lo em 2019.
--
-- ───────────────────────────────────────────────────────────────────────────
-- POR QUE O ITEM É CATÁLOGO DA PROPRIEDADE, E NÃO UMA LINHA POR CÔMODO
-- ───────────────────────────────────────────────────────────────────────────
--
-- A alternativa óbvia — uma linha por (cômodo, bem), com nome, foto e custo na
-- própria linha — foi medida contra a casa real e custa isto:
--
--   * 12 unidades. "Prato raso branco" está em 6 cozinhas de duplex. Nessa
--     forma são 6 linhas independentes do MESMO objeto: corrigir o nome ou o
--     custo de reposição é 1 UPDATE × 6, e a primeira esquecida transforma o
--     prato em dois bens diferentes que nenhum relatório soma. "Quantos pratos
--     a casa tem" deixa de ter resposta conferível.
--   * A foto pertence ao OBJETO, não à prateleira. Linha por cômodo pede o
--     mesmo arquivo enviado 6 vezes (ou uma tabela de mídia chaveada por
--     cômodo, que é a mesma duplicação com outro nome).
--   * A avaria ("3 quebrados") precisa apontar para o objeto para ser cobrável
--     do hóspede. Apontando para a linha-do-cômodo, o prejuízo da casa é uma
--     soma sobre linhas que ninguém audita.
--
-- Então: `inventory_items` é o catálogo (um "Prato raso branco" na propriedade,
-- esteja ele em dois ambientes ou em seis apartamentos) e `room_inventory` é a
-- colocação — só o par (cômodo, item) e a quantidade esperada. É a mesma
-- separação de `unit_types` × `unit_type_members` (§3): o cadastro num lugar, a
-- composição noutro.
--
-- ───────────────────────────────────────────────────────────────────────────
-- MÍDIA PRÓPRIA, E NÃO `site_media`
-- ───────────────────────────────────────────────────────────────────────────
--
-- `site_media` (20261003120000, §13b) parece servir e não serve, por dois
-- motivos de schema, não de gosto: ela NÃO tem `property_id` (é configuração da
-- vitrine, exceção consciente à regra do `property_id`) e o `down` da migration
-- do CMS a DERRUBA — pendurar a foto do inventário lá deixaria `20261003120000
-- down` quebrado ou, pior, apagando bens em cascata. `inventory_media` é tabela
-- de negócio: tem `property_id`, e cai só com esta migration.
--
-- O vínculo item × foto é MUITOS-PARA-MUITOS (`inventory_item_media`), e não uma
-- coluna `media_id` no item: a foto que o gestor tira da bancada mostra a
-- travessa, a leiteira e o açucareiro de uma vez, e o mesmo item quer a foto de
-- catálogo e a foto do defeito. Coluna escalar obrigaria a escolher uma das duas.
--
-- ───────────────────────────────────────────────────────────────────────────
-- O QUE ESTAS FKs NÃO FECHAM, DITO PARA NINGUÉM ACHAR QUE FECHARAM
-- ───────────────────────────────────────────────────────────────────────────
--
-- `room_inventory` e `inventory_count_lines` não têm `property_id` (penduram em
-- `unit_rooms`/`inventory_items`/`inventory_counts`, como `rates` pendura em
-- `rate_tables` — §4). A consequência é que, numa segunda propriedade, nada no
-- banco impede colocar um item da propriedade A num cômodo da propriedade B: a
-- conferência é da API, que filtra tudo por `property_id`. Fechar isso exigiria
-- `UNIQUE (property_id, id)` nos pais e FK composta (o padrão de
-- `users.broker_id`, 20261002180000) — lá havia exploit medido, aqui não há, e
-- não se compra constraint no escuro. Fica escrito em vez de virar código.

-- ═══════════════════════ 1. O ambiente (`unit_rooms`) ══════════════════════
--
-- A unidade física dividida em cômodos. Nove colunas.
--
-- `kind` é vocabulário fechado porque a tela agrupa e ordena por ele (a lista de
-- conferência sai na ordem em que se caminha pela casa), e texto livre viraria
-- "Quarto", "quarto " e "Dormitório" no mesmo relatório. Enum = `text` + `CHECK`
-- (Convenções de docs/db.md): acrescentar 'closet' depois é uma migration de
-- uma linha, não um `ALTER TYPE` que trava a tabela.
CREATE TABLE unit_rooms (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),

    -- RESTRICT: unidade não se apaga (tem histórico em `stay_blocks`), se
    -- desativa. CASCADE aqui apagaria o inventário inteiro de uma unidade
    -- porque alguém tentou um DELETE que o resto do schema já recusa.
    unit_id     uuid NOT NULL REFERENCES units(id) ON DELETE RESTRICT,

    name        text NOT NULL
        CONSTRAINT unit_rooms_name_nao_vazio CHECK (btrim(name) <> ''),
    kind        text NOT NULL
        CONSTRAINT unit_rooms_kind_valido CHECK (kind IN
            ('quarto','banheiro','cozinha','sala','area_externa','lavanderia','varanda','outro')),
    sort_order  int     NOT NULL DEFAULT 0,

    -- Soft delete desta tabela: cômodo com conferência ou avaria no histórico
    -- não pode sumir (as FKs abaixo são RESTRICT). Uma coluna `deleted_at` ao
    -- lado diria a mesma coisa de outro jeito — é a decisão de `brokers.active`.
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    -- Dois "Suíte 1" na mesma unidade é o defeito que faz quem confere contar a
    -- mesma cama duas vezes e nunca a outra.
    CONSTRAINT unit_rooms_nome_unico UNIQUE (unit_id, name)
);

-- Índices pelas consultas da tela, não por simetria.
--
-- A listagem de ambientes de uma unidade é
--   WHERE unit_id = $1 ORDER BY sort_order, name, id
-- e o `id` no fim não é zelo: sem desempate determinístico, dois cômodos com o
-- mesmo `sort_order` trocam de lugar entre duas aberturas da tela e a paginação
-- repete ou perde linha. O índice carrega as três colunas para a ordenação sair
-- do índice, sem Sort.
--
-- `unit_rooms_nome_unico` já cobre a FK `unit_id`; esta cobre o ORDER BY.
CREATE INDEX unit_rooms_listagem_idx ON unit_rooms(unit_id, sort_order, name, id);
CREATE INDEX unit_rooms_property_idx ON unit_rooms(property_id);

COMMENT ON TABLE unit_rooms IS
    'Ambiente (cômodo) de uma unidade física. É a dimensão que faltava em docs/db.md §11: o enxoval não está "no AP-01", está na cozinha do AP-01, e quem confere caminha cômodo a cômodo.';
COMMENT ON COLUMN unit_rooms.sort_order IS
    'Ordem de caminhada pela casa, usada na lista de conferência. Empate é desfeito por name e id — a tela nunca ordena só por esta coluna.';

-- ═════════════════ 2. O catálogo de bens (`inventory_items`) ═══════════════
--
-- Onze colunas. Um "Prato raso branco" por propriedade — ver o bloco do alto.
CREATE TABLE inventory_items (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),
    name        text NOT NULL
        CONSTRAINT inventory_items_name_nao_vazio CHECK (btrim(name) <> ''),

    -- O detalhe que distingue dois bens parecidos ("borda dourada", "60×80 cm").
    -- Anulável: a maior parte do enxoval é autoexplicativa com nome e foto.
    description text,

    category    text NOT NULL
        CONSTRAINT inventory_items_category_valida CHECK (category IN
            ('louca','talher','copo','cama','banho','mobilia','eletro','utensilio','decoracao','outro')),

    -- Unidade de contagem. Padrão 'un' porque quase tudo se conta em peças; o
    -- lençol vai em 'jogo' e a fronha em 'par', e contar jogo como peça é o
    -- erro que faz a conferência fechar com o dobro.
    unit_measure text NOT NULL DEFAULT 'un'
        CONSTRAINT inventory_items_unit_measure_valida
            CHECK (unit_measure IN ('un','par','jogo','kg','l','m')),

    -- Quanto custa REPOR, em centavos (regra 4 do CLAUDE.md). Não é o preço de
    -- compra histórico: o número que a operação usa é o de substituir a peça
    -- hoje, e é dele que sai o valor a cobrar de uma avaria. NULL = ainda não
    -- cotado, que é diferente de "custa zero" — por isso o CHECK é `> 0` e não
    -- `>= 0`: zero seria um segundo jeito, errado, de escrever "não sei".
    replacement_cost_cents bigint
        CONSTRAINT inventory_items_custo_positivo
            CHECK (replacement_cost_cents IS NULL OR replacement_cost_cents > 0),

    -- Soft delete: item colocado em algum ambiente não se apaga (as FKs são
    -- RESTRICT), sai de linha desativado.
    active      boolean NOT NULL DEFAULT true,

    -- Origem da importação, no formato `chatwoot:2184:367988`
    -- (fonte:conversa:mensagem). É o que torna a importação REPETÍVEL: a
    -- segunda passada reencontra a própria linha pelo índice único parcial
    -- abaixo em vez de criar um segundo "Prato raso branco".
    source_ref  text
        CONSTRAINT inventory_items_source_ref_nao_vazia
            CHECK (source_ref IS NULL OR btrim(source_ref) <> ''),

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Único QUANDO PREENCHIDO, e por propriedade. Parcial porque o item cadastrado
-- à mão nasce sem origem, e NULL não deve colidir com NULL nem contar no
-- índice. Escopado por `property_id` pelo mesmo motivo de `units(property_id,
-- code)` (§3): a chave global impediria uma segunda propriedade de importar a
-- mesma mensagem de origem. Quem importa usa
--   ON CONFLICT (property_id, source_ref) WHERE source_ref IS NOT NULL
CREATE UNIQUE INDEX inventory_items_source_ref_idx
    ON inventory_items(property_id, source_ref) WHERE source_ref IS NOT NULL;

-- A tela do catálogo é
--   WHERE property_id = $1 [AND category = $2] ORDER BY category, name, id
-- — um índice só serve os dois casos, e o `id` fecha a ordenação. Cobre também
-- a FK `property_id`.
CREATE INDEX inventory_items_catalogo_idx ON inventory_items(property_id, category, name, id);

-- Busca por nome NÃO ganha índice trigram agora, embora `pg_trgm` esteja
-- instalado (é o que `contacts.name` usa). O catálogo real tem algumas centenas
-- de linhas: o seq scan é mais rápido que o índice e o índice custaria escrita
-- em toda edição. Entra quando houver medição que o justifique — índice que
-- ninguém mediu é peso comprado no escuro (§14).

COMMENT ON TABLE inventory_items IS
    'Catálogo de bens da propriedade — UM item por objeto, não uma linha por cômodo. A quantidade por ambiente vive em room_inventory. Corrigir nome ou custo é um UPDATE, e "quantos pratos a casa tem" tem resposta conferível.';
COMMENT ON COLUMN inventory_items.replacement_cost_cents IS
    'Custo de REPOSIÇÃO em centavos (não o de compra histórico). Base do valor a cobrar numa avaria. NULL = não cotado; zero é recusado pelo CHECK para não virar sinônimo de NULL.';
COMMENT ON COLUMN inventory_items.source_ref IS
    'Origem da importação, formato fonte:conversa:mensagem (ex. chatwoot:2184:367988). Único por propriedade quando preenchido — é o que torna a importação idempotente.';

-- ═══════════════ 3. A colocação (`room_inventory`) ═════════════════════════
--
-- Quanto de cada item há em cada ambiente. Seis colunas, e a PK é a chave
-- natural: não há id próprio porque não há nada a referenciar aqui — a avaria e
-- a linha de conferência apontam para (ambiente, item) por conta própria.
CREATE TABLE room_inventory (
    -- CASCADE: a colocação PERTENCE ao ambiente. Apagar um cômodo (que só
    -- acontece antes de ele ter histórico) leva embora a lista dele.
    room_id      uuid NOT NULL REFERENCES unit_rooms(id) ON DELETE CASCADE,

    -- RESTRICT: item colocado em algum ambiente não se apaga, se desativa.
    -- CASCADE aqui esvaziaria silenciosamente a lista de seis cozinhas porque
    -- alguém apagou um item do catálogo.
    item_id      uuid NOT NULL REFERENCES inventory_items(id) ON DELETE RESTRICT,

    -- Quantidade ESPERADA (o padrão da casa), não a contada. Zero é permitido e
    -- significa "este ambiente não tem este item, e isso é intencional" — serve
    -- para o cômodo que perdeu a peça sem perder a linha de histórico.
    expected_qty int NOT NULL
        CONSTRAINT room_inventory_qty_nao_negativa CHECK (expected_qty >= 0),
    note         text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (room_id, item_id)
);

-- A PK cobre `room_id` e serve "itens de um ambiente" e, por nested loop a
-- partir de `unit_rooms_listagem_idx`, "itens de uma unidade". `item_id` precisa
-- do próprio índice: é por ele que se responde "em que ambientes este item
-- está" e é ele que o RESTRICT consulta ao tentar apagar um item.
CREATE INDEX room_inventory_item_idx ON room_inventory(item_id);

COMMENT ON TABLE room_inventory IS
    'Colocação: quanto de cada item do catálogo há em cada ambiente. É o unit_inventory projetado em docs/db.md §11, com o cômodo no lugar da unidade. Sem property_id de propósito: pendura nos dois pais, como rates pendura em rate_tables.';

-- ═══════════════════ 4. A foto (`inventory_media`) ═════════════════════════
--
-- Onze colunas. Tabela própria, não `site_media` — ver o bloco do alto.
--
-- Imutável, como `site_media`: trocar a foto é enviar arquivo novo e apontar o
-- item para o novo id. É o que permite `Cache-Control: immutable` na URL.
CREATE TABLE inventory_media (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id   uuid NOT NULL REFERENCES properties(id),

    -- Tipo detectado pelos BYTES, não pela extensão. Sem `CHECK` de lista e sem
    -- teto de tamanho no schema, pela mesma razão de `site_media` (§13b):
    -- trocar o limite ou aceitar um formato novo não deve exigir migration.
    mime          text NOT NULL
        CONSTRAINT inventory_media_mime_nao_vazio CHECK (btrim(mime) <> ''),
    bytes         bigint NOT NULL
        CONSTRAINT inventory_media_bytes_positivo CHECK (bytes > 0),

    -- Dimensões em pixels. Anuláveis: o upload antigo pode não tê-las medido, e
    -- a tela só usa para não pedir ao navegador que adivinhe o espaço da imagem.
    width         int
        CONSTRAINT inventory_media_width_positiva  CHECK (width  IS NULL OR width  > 0),
    height        int
        CONSTRAINT inventory_media_height_positiva CHECK (height IS NULL OR height > 0),

    original_name text NOT NULL,

    -- Nome do arquivo no volume de mídia da API. NUNCA derivado do nome enviado
    -- pelo usuário.
    storage_key   text NOT NULL
        CONSTRAINT inventory_media_storage_key_nao_vazia CHECK (btrim(storage_key) <> ''),

    -- Miniatura, gerada pelo upload. Anulável porque a geração pode falhar sem
    -- invalidar o original (a tela cai no original); único quando presente pelo
    -- índice abaixo, porque duas linhas com a mesma chave sobrescreveriam o
    -- mesmo arquivo no volume.
    thumb_key     text
        CONSTRAINT inventory_media_thumb_key_nao_vazia
            CHECK (thumb_key IS NULL OR btrim(thumb_key) <> ''),

    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    uuid REFERENCES users(id),

    CONSTRAINT inventory_media_storage_key_unica UNIQUE (storage_key),

    -- A miniatura mora no mesmo namespace do original: se as duas chaves
    -- coincidirem, a thumb sobrescreve a foto.
    CONSTRAINT inventory_media_thumb_difere
        CHECK (thumb_key IS NULL OR thumb_key <> storage_key)
);

CREATE UNIQUE INDEX inventory_media_thumb_key_idx
    ON inventory_media(thumb_key) WHERE thumb_key IS NOT NULL;
CREATE INDEX inventory_media_property_idx ON inventory_media(property_id);
-- Parcial, como nas tabelas do CRM: upload por script ou seed nasce sem autor.
CREATE INDEX inventory_media_criador_idx ON inventory_media(created_by) WHERE created_by IS NOT NULL;

COMMENT ON TABLE inventory_media IS
    'Foto de bem do inventário, no volume de mídia da API. Linha imutável. NÃO é site_media: aquela não tem property_id e o down de 20261003120000 a derruba — pendurar o inventário lá quebraria o down do CMS.';

-- ═════════ 5. Item × foto, muitos-para-muitos (`inventory_item_media`) ═════
--
-- Uma foto de cena mostra vários itens; um item tem foto de catálogo e foto de
-- defeito. Coluna `media_id` escalar no item obrigaria a escolher uma das duas.
CREATE TABLE inventory_item_media (
    -- As duas pontas em CASCADE: a LIGAÇÃO não tem vida própria. Apagar a foto
    -- tira a foto do item (o item fica), apagar o item tira o item da foto (a
    -- foto fica, e pode estar ligada a outros itens da mesma cena).
    item_id    uuid NOT NULL REFERENCES inventory_items(id)  ON DELETE CASCADE,
    media_id   uuid NOT NULL REFERENCES inventory_media(id)  ON DELETE CASCADE,

    -- Ordem de exibição. A CAPA do item é o menor `sort_order`, desempatado por
    -- `media_id` — sem o desempate, item com duas fotos em `sort_order = 0`
    -- troca de capa a cada abertura da tela.
    sort_order int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (item_id, media_id)
);

-- A capa e a galeria do item saem deste índice sem Sort.
CREATE INDEX inventory_item_media_ordem_idx ON inventory_item_media(item_id, sort_order, media_id);
-- "Quais itens esta foto mostra" e a checagem do CASCADE ao apagar a foto.
CREATE INDEX inventory_item_media_media_idx ON inventory_item_media(media_id);

COMMENT ON TABLE inventory_item_media IS
    'Liga bem a foto, muitos-para-muitos. A capa do item é o menor sort_order, desempatado por media_id.';

-- ═══════════════ 6. A conferência (`inventory_counts` + linhas) ════════════
--
-- Dez colunas no cabeçalho. `opened_at` É o `created_at` desta tabela: duas
-- colunas para o mesmo instante seriam duas fontes da mesma verdade, e a que a
-- operação lê é "quando a conferência abriu".
CREATE TABLE inventory_counts (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),
    unit_id     uuid NOT NULL REFERENCES units(id) ON DELETE RESTRICT,

    status      text NOT NULL DEFAULT 'aberta'
        CONSTRAINT inventory_counts_status_valido CHECK (status IN ('aberta','fechada','cancelada')),
    note        text,

    opened_by   uuid REFERENCES users(id),
    opened_at   timestamptz NOT NULL DEFAULT now(),
    closed_by   uuid REFERENCES users(id),
    closed_at   timestamptz,
    updated_at  timestamptz NOT NULL DEFAULT now(),

    -- Mesma forma de `crm_opportunities` (`(status = 'aberto') = (closed_at IS
    -- NULL)`): conferência aberta com data de fechamento, ou fechada sem ela,
    -- são os dois estados impossíveis que fazem a tela mostrar duas verdades.
    -- `cancelada` também tem `closed_at` — é quando se desistiu dela.
    -- `closed_by` continua anulável: fechamento por rotina não tem autor.
    CONSTRAINT inventory_counts_fechamento CHECK ((status = 'aberta') = (closed_at IS NULL))
);

-- UMA conferência aberta por unidade, e essa é a constraint que sustenta o
-- módulo. Sem ela, dois funcionários abrem a contagem do AP-01 no mesmo
-- plantão, cada um conta metade, e o fechamento de um sobrescreve o do outro —
-- conferir com SELECT antes de INSERT é TOCTOU e não vale sob concorrência. É
-- índice único PARCIAL porque a unidade acumula conferências FECHADAS para
-- sempre, e é exatamente por isso que uma UNIQUE comum não serviria.
CREATE UNIQUE INDEX inventory_counts_aberta_idx ON inventory_counts(unit_id) WHERE status = 'aberta';

-- Histórico de conferências de uma unidade, da mais recente para a mais antiga,
-- com desempate. Cobre também a FK `unit_id` fora do predicado parcial.
CREATE INDEX inventory_counts_unidade_idx  ON inventory_counts(unit_id, opened_at DESC, id);
CREATE INDEX inventory_counts_property_idx ON inventory_counts(property_id, opened_at DESC, id);
CREATE INDEX inventory_counts_autor_idx    ON inventory_counts(opened_by) WHERE opened_by IS NOT NULL;
CREATE INDEX inventory_counts_fechador_idx ON inventory_counts(closed_by) WHERE closed_by IS NOT NULL;

COMMENT ON TABLE inventory_counts IS
    'Conferência de inventário de uma unidade. Índice único parcial inventory_counts_aberta_idx garante no máximo UMA aberta por unidade. opened_at é o created_at desta tabela.';

-- A linha da conferência. Onze colunas.
CREATE TABLE inventory_count_lines (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    -- CASCADE: a linha PERTENCE à conferência.
    count_id     uuid NOT NULL REFERENCES inventory_counts(id) ON DELETE CASCADE,

    -- RESTRICT nos dois: cômodo e item com conferência no histórico não se
    -- apagam. É isto que faz a contagem do mês passado continuar legível.
    room_id      uuid NOT NULL REFERENCES unit_rooms(id)       ON DELETE RESTRICT,
    item_id      uuid NOT NULL REFERENCES inventory_items(id)  ON DELETE RESTRICT,

    -- CONGELADA na abertura, copiada de `room_inventory.expected_qty` (regra 7
    -- do CLAUDE.md: toda entidade financeira/operacional guarda o que usou). É o
    -- mesmo motivo de `reservation_nights` guardar a tarifa aplicada: se a
    -- conferência lesse a quantidade esperada por JOIN, mudar o padrão da casa
    -- hoje reescreveria o que a contagem de março esperava, e a divergência que
    -- foi apurada e cobrada deixaria de existir no relatório.
    expected_qty int NOT NULL
        CONSTRAINT inventory_count_lines_esperada_nao_negativa CHECK (expected_qty >= 0),

    -- NULL enquanto não contado — e é por isso que o pendente é NULL e não zero:
    -- zero é "contei, não achei nenhum", que é uma informação, não a ausência
    -- dela.
    counted_qty  int
        CONSTRAINT inventory_count_lines_contada_nao_negativa
            CHECK (counted_qty IS NULL OR counted_qty >= 0),
    note         text,

    counted_by   uuid REFERENCES users(id),
    counted_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    -- Linha contada registra QUANDO. `counted_by` fica anulável (contagem
    -- lançada por importação), o instante não: sem ele, "quando foi conferido"
    -- só existe no `updated_at`, que qualquer edição posterior apaga.
    CONSTRAINT inventory_count_lines_contagem
        CHECK ((counted_qty IS NULL) = (counted_at IS NULL)),

    -- O mesmo item, no mesmo cômodo, duas vezes na mesma conferência é a linha
    -- duplicada que faz a divergência aparecer em dobro.
    CONSTRAINT inventory_count_lines_unica UNIQUE (count_id, room_id, item_id)
);

-- `inventory_count_lines_unica` é também o índice da tela de contagem
-- (`WHERE count_id = $1 ORDER BY room_id, item_id`) e cobre a FK `count_id`.
-- O que falta a ela é o filtro do que ainda não foi contado:
CREATE INDEX inventory_count_lines_pendentes_idx
    ON inventory_count_lines(count_id, room_id, item_id) WHERE counted_qty IS NULL;
CREATE INDEX inventory_count_lines_room_idx    ON inventory_count_lines(room_id);
CREATE INDEX inventory_count_lines_item_idx    ON inventory_count_lines(item_id);
CREATE INDEX inventory_count_lines_contador_idx
    ON inventory_count_lines(counted_by) WHERE counted_by IS NOT NULL;

COMMENT ON COLUMN inventory_count_lines.expected_qty IS
    'Quantidade esperada CONGELADA na abertura da conferência (regra 7 do CLAUDE.md). Mudar room_inventory depois não reescreve o que esta contagem esperava.';
COMMENT ON COLUMN inventory_count_lines.counted_qty IS
    'NULL = ainda não contado. Zero = contado e não encontrado. São coisas diferentes, e a diferença é o que a tela de pendências pergunta.';

-- ═════════════════ 7. Quebrado, faltando, avariado (`inventory_issues`) ════
--
-- Quinze colunas. `reported_at` é o `created_at` desta tabela, pelo mesmo
-- motivo de `opened_at` em `inventory_counts`.
CREATE TABLE inventory_issues (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id    uuid NOT NULL REFERENCES properties(id),
    room_id        uuid NOT NULL REFERENCES unit_rooms(id)      ON DELETE RESTRICT,
    item_id        uuid NOT NULL REFERENCES inventory_items(id) ON DELETE RESTRICT,

    kind           text NOT NULL
        CONSTRAINT inventory_issues_kind_valido
            CHECK (kind IN ('quebrado','faltando','avariado','outro')),

    -- Quantidade afetada. `> 0`: avaria de zero peças não é avaria, é linha que
    -- aparece na lista de pendências sem nada a resolver.
    qty            int NOT NULL
        CONSTRAINT inventory_issues_qty_positiva CHECK (qty > 0),
    note           text,

    -- A reserva durante a qual a avaria aconteceu. Anulável (desgaste sem
    -- hóspede, achado em conferência de rotina) e RESTRICT: é este vínculo que
    -- permite cobrar o hóspede, e perdê-lo em silêncio por um DELETE em cascata
    -- apagaria o lastro da cobrança. Reserva não se apaga (§5), se cancela.
    reservation_id uuid REFERENCES reservations(id)      ON DELETE RESTRICT,

    -- A conferência que achou. Anulável: a avaria também é reportada fora de
    -- conferência, pela governanta.
    count_id       uuid REFERENCES inventory_counts(id)  ON DELETE RESTRICT,

    -- O DESFECHO, e é ele que diz se a pendência está aberta: NULL = aberta.
    -- Não há coluna `status` ao lado de propósito — ela seria uma segunda fonte
    -- da mesma verdade, e o par "status='aberta' com resolution preenchida" é um
    -- estado impossível que alguém acabaria gravando.
    resolution     text
        CONSTRAINT inventory_issues_resolution_valida
            CHECK (resolution IS NULL OR resolution IN
                ('reposto','consertado','cobrado','perda_aceita','descartado')),

    reported_by    uuid REFERENCES users(id),
    reported_at    timestamptz NOT NULL DEFAULT now(),
    resolved_by    uuid REFERENCES users(id),
    resolved_at    timestamptz,
    updated_at     timestamptz NOT NULL DEFAULT now(),

    -- Desfecho e instante andam juntos. `resolved_by` fica anulável (resolução
    -- por rotina), o instante não.
    CONSTRAINT inventory_issues_desfecho
        CHECK ((resolution IS NULL) = (resolved_at IS NULL))
);

-- A tela que importa é "pendências abertas", e ela é PARCIAL: avaria resolvida
-- acumula para sempre e nunca aparece nessa lista — é o mesmo argumento do
-- índice do kanban (§7). Da mais recente para a mais antiga, com desempate.
CREATE INDEX inventory_issues_abertas_idx
    ON inventory_issues(property_id, reported_at DESC, id) WHERE resolution IS NULL;

-- "O que quebrou nesta estadia", que é a pergunta do check-out e a base da
-- cobrança. Parcial porque a maioria das avarias não tem reserva.
CREATE INDEX inventory_issues_reserva_idx
    ON inventory_issues(reservation_id) WHERE reservation_id IS NOT NULL;
CREATE INDEX inventory_issues_conferencia_idx
    ON inventory_issues(count_id) WHERE count_id IS NOT NULL;

-- Histórico por ambiente e por item (o prato que quebra toda semana).
CREATE INDEX inventory_issues_room_idx ON inventory_issues(room_id, reported_at DESC, id);
CREATE INDEX inventory_issues_item_idx ON inventory_issues(item_id, reported_at DESC, id);
CREATE INDEX inventory_issues_relator_idx
    ON inventory_issues(reported_by) WHERE reported_by IS NOT NULL;
CREATE INDEX inventory_issues_resolvedor_idx
    ON inventory_issues(resolved_by) WHERE resolved_by IS NOT NULL;

COMMENT ON TABLE inventory_issues IS
    'Bem quebrado, faltando ou avariado. Pendência aberta é resolution IS NULL — não há coluna status ao lado, que seria segunda fonte da mesma verdade. reservation_id é o vínculo que permite cobrar o hóspede.';

-- ═══════════ A régua de ~25 colunas, conferida e não só prometida ══════════
--
-- Mesmo bloco de 20260827110000, agora sobre as sete tabelas deste módulo. A
-- regra 9 do CLAUDE.md já foi violada uma vez nesta árvore (`reservation_pricing`
-- nasceu com 32 colunas e custou a migration 20260826100000 para ser desmontada),
-- e regra que só existe em texto é regra que volta. A maior aqui tem 15.
DO $$
DECLARE
    v_excesso text;
BEGIN
    SELECT string_agg(format('%s (%s colunas)', tabela, colunas), ', ' ORDER BY tabela)
      INTO v_excesso
      FROM (
        SELECT c.relname AS tabela, count(*) AS colunas
          FROM pg_attribute a
          JOIN pg_class c     ON c.oid = a.attrelid
          JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE n.nspname = current_schema()
           AND c.relkind = 'r'
           AND c.relname IN ('unit_rooms','inventory_items','room_inventory',
                             'inventory_media','inventory_item_media',
                             'inventory_counts','inventory_count_lines','inventory_issues')
           AND a.attnum > 0
           AND NOT a.attisdropped
         GROUP BY c.relname
        HAVING count(*) > 25
      ) t;

    IF v_excesso IS NOT NULL THEN
        RAISE EXCEPTION 'tabela do inventário acima do teto de 25 colunas: %', v_excesso
            USING HINT = 'extraia uma tabela satélite 1:1 — a crm_opportunities de 118 colunas do portal_amimoveis é o antiexemplo do projeto (CLAUDE.md, regra 9)';
    END IF;
END;
$$;
