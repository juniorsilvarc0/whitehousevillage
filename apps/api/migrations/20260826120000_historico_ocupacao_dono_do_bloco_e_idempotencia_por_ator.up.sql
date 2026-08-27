-- Três achados da revisão adversarial que só o schema resolve:
--   1. o check-out apagava a estadia do mapa (não havia estado terminal que
--      preservasse a história);
--   2. a chave de idempotência não tinha ator, e o replay vazava a resposta de
--      um usuário para outro;
--   3. `stay_blocks` não tinha dono, então `scope='own'` em `calendar` não
--      tinha por onde virar SQL.

-- ═════════ 1. `completed`: o estado terminal que preserva a história ═════════
--
-- MEDIDO ANTES: reserva de cobertura 10–13/09/2030, confirmada → check-in →
-- check-out. `service_acoes.go` move os blocos para `cancelled` (é o único
-- terminal que existia), e o mapa passa a devolver os três dias como `livre`,
-- `reservation_code: null`. A estadia aconteceu e sumiu:
--
--     >>> ANTES  2030-09-10 | WH-2026-0001    >>> DEPOIS  2030-09-10 | (livre)
--                2030-09-11 | WH-2026-0001                2030-09-11 | (livre)
--                2030-09-12 | WH-2026-0001                2030-09-12 | (livre)
--     noites ocupadas em setembro/2030, contadas sobre stay_blocks: 0
--
-- Pior que o mapa vazio é a AMBIGUIDADE: depois do check-out, a linha da estadia
-- concluída fica byte a byte igual à de uma reserva que o hóspede cancelou sem
-- nunca ter chegado. Nenhuma consulta consegue separar receita realizada de
-- venda perdida, e toda taxa de ocupação calculada sobre `stay_blocks`
-- subestima — para menos, sempre, e sem deixar rastro do erro.
ALTER TABLE stay_blocks DROP CONSTRAINT stay_blocks_status_check;
ALTER TABLE stay_blocks ADD CONSTRAINT stay_blocks_status_check
    CHECK (status IN ('hold','confirmed','completed','cancelled','expired'));

COMMENT ON COLUMN stay_blocks.status IS
    'hold/confirmed ocupam o inventário (predicado de stay_no_overlap). completed é a estadia consumada: sai do inventário vendável e PERMANECE no histórico. cancelled/expired são a venda que não aconteceu.';

-- ─── A decisão que importa: `completed` NÃO entra no WHERE da EXCLUDE ───
--
-- A constraint continua valendo só para ('hold','confirmed'). Quatro razões,
-- em ordem de peso:
--
-- (a) A constraint protege INVENTÁRIO VENDÁVEL, não o arquivo. Um bloco
--     `completed` descreve consumo que já ocorreu — é lançamento de razão, não
--     promessa de data. Bloquear venda futura com base nele é confundir o livro
--     com a prateleira.
--
-- (b) CHECK-OUT ANTECIPADO quebraria na hora. O bloco carrega o período do
--     CONTRATO — `[check_in, check_out)` —, não o que o hóspede de fato ficou.
--     Hóspede de 10–13 que devolve a chave no dia 11 deixa um bloco 10–13 para
--     trás; com `completed` dentro da EXCLUDE, as noites 11 e 12 ficariam
--     travadas por uma estadia que já terminou, e a recepção não conseguiria
--     nem revender nem registrar um walk-in para hoje à noite. Justamente o
--     overbooking ao contrário: recusar hóspede com a casa vazia.
--
-- (c) CORREÇÃO DE HISTÓRICO ficaria refém. Importar uma estadia passada de OTA
--     (Fase 4), corrigir a unidade errada num registro antigo ou reconstruir
--     ocupação de antes do sistema passariam a estourar 23P01 em cima de dado
--     que não tem NENHUM efeito comercial — e o `23P01` do projeto significa,
--     por contrato, "data ocupada, 409 DATE_CONFLICT". Traduzir "seu histórico
--     conflita" como "a data está vendida" mentiria para o operador.
--
-- (d) O índice da EXCLUDE fica LIMITADO AO FUTURO. Com `completed` dentro, ele
--     cresceria monotonicamente — toda estadia da história, para sempre, dentro
--     do índice que é consultado a cada inserção de bloco. Fora, ele carrega só
--     o inventário vivo e volta a encolher a cada check-out. O índice quente do
--     caminho mais crítico do sistema não pode ser o que mais cresce.
--
-- O QUE ISTO CUSTA, dito por inteiro: com `completed` fora do predicado, o banco
-- deixa de impedir duas estadias concluídas sobrepostas na MESMA unidade. Na
-- prática a transição é sempre `confirmed → completed` sobre uma linha que
-- esteve protegida enquanto era `confirmed`, então a sobreposição só nasceria de
-- um INSERT retroativo direto — importação de histórico, o mesmo caso (c) que
-- queremos deixar passar. É uma troca consciente: o histórico aceita conserto,
-- o inventário não aceita venda dupla.
--
-- CONSEQUÊNCIA PARA QUEM LÊ: "ocupa o inventário" e "aparece no mapa" deixam de
-- ser o mesmo predicado. Bloqueio de venda continua sendo ('hold','confirmed');
-- desenho do mapa e ocupação passam a ser ('hold','confirmed','completed').

-- O mapa e o BI varrem por período e status sem filtrar unidade; o gist de
-- `period` sozinho não distingue estadia consumada de venda cancelada, e a
-- partir daqui a maioria das linhas da tabela será `completed` ou `cancelled`.
CREATE INDEX stay_blocks_ocupacao_idx ON stay_blocks USING gist (period)
    WHERE status IN ('hold','confirmed','completed');

-- ═════════ 2. Idempotência com ator: fim do vazamento entre usuários ═════════
--
-- MEDIDO ANTES: PK `(key, endpoint)`. O admin cria a reserva com
-- `Idempotency-Key: K`; o corretor manda a MESMA chave com o MESMO corpo, o
-- `ON CONFLICT (key, endpoint) DO NOTHING` de `reservarChave` não afeta linha
-- nenhuma, e o ramo "a chave já concluiu" devolve ao corretor **201 com a
-- reserva inteira do admin** — código, nome do hóspede, valor. `GET` daquela
-- mesma reserva com o token dele dá 404. O replay fura o escopo `own` do RBAC
-- e transforma a idempotência num canal de leitura lateral.
--
-- A chave nunca foi global: ela sempre significou "esta requisição, deste
-- cliente". O schema é que só guardava metade disso.

-- POR QUE ESVAZIAR: `idempotency_keys` é CACHE DE REPLAY de vida curta, não
-- registro de negócio — nada referencia esta tabela, e a varredura de expiração
-- já apaga linha antiga por conta própria. As linhas existentes foram gravadas
-- sob o escopo furado: não há como descobrir retroativamente de quem era cada
-- resposta, e mantê-las com um ator adivinhado perpetuaria exatamente o
-- vazamento que esta migration fecha. Apagar é o único conserto honesto — e é o
-- que torna os dois `NOT NULL` abaixo possíveis sem backfill inventado.
-- O efeito visível é que uma requisição em voo no momento do deploy pode ser
-- executada de novo se o cliente reenviar a mesma chave. É o comportamento
-- normal de retry, não perda de dado.
DELETE FROM idempotency_keys;

-- ON DELETE CASCADE porque a linha é cache: apagar o usuário não pode ser
-- barrado por resposta guardada, e resposta órfã não serve a ninguém. (Usuário
-- normalmente sai por `deleted_at`; isto cobre o expurgo de verdade da LGPD.)
ALTER TABLE idempotency_keys
    ADD COLUMN actor_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ADD COLUMN property_id uuid NOT NULL REFERENCES properties(id);

-- A NOVA CHAVE: (key, endpoint, actor_id, property_id).
--
-- `actor_id` é o que fecha o vazamento — é o usuário autenticado, o mesmo eixo
-- que o RBAC filtra com `scope='own'`.
--
-- `property_id` entra na chave mesmo sendo hoje FUNÇÃO do ator (`users.property_id`
-- é NOT NULL, um usuário pertence a uma propriedade). É seguro de propósito: a
-- resposta guardada é um retrato dos dados de UMA propriedade, e o dia em que
-- usuário passar a atender duas casas — o modelo inteiro carrega `property_id`
-- justamente para que a segunda propriedade custe pouco — o modo de falha seria
-- de novo devolver a resposta da casa errada. Uma coluna a mais na PK é barata;
-- descobrir o vazamento uma segunda vez, não.
ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey;
ALTER TABLE idempotency_keys ADD CONSTRAINT idempotency_keys_pkey
    PRIMARY KEY (key, endpoint, actor_id, property_id);

COMMENT ON TABLE idempotency_keys IS
    'Cache de replay por ATOR. A chave é (key, endpoint, actor_id, property_id): a mesma Idempotency-Key de dois usuários são duas requisições distintas, e cada um só relê a própria resposta.';

-- ═════════ 3. Dono do bloco: o escopo `own` de `calendar` vira SQL ═════════
--
-- MEDIDO ANTES: token de corretor bloqueou as 8 unidades por 364 dias (201) —
-- a casa inteira invendável por um ano — e não conseguiu desfazer (DELETE →
-- 403). O limite de tamanho é regra comercial e fica no domínio; o que falta
-- AQUI é o eixo do filtro.
--
-- `created_by` JÁ EXISTE em `stay_blocks` (desde 20260820130000), com índice
-- parcial desde 20260826110000, e `CriarBloqueioOperacional` o preenche. Ele
-- não resolve, e o teste mostra por quê:
--
--     corretor abre a reserva (reservations.owner_id = corretor)
--     admin confirma           (stay_blocks.created_by = admin)
--     calendário do corretor com `AND created_by = $corretor` → 0 blocos
--
-- A própria venda do corretor some do calendário dele porque quem apertou
-- "confirmar" foi outra pessoa. `created_by` responde "quem digitou" — fato de
-- auditoria, imutável. Dono é outra pergunta: "de quem é esta linha" — resposta
-- comercial, que se transfere quando a carteira muda de mãos. São dois campos
-- porque são duas perguntas; `reservations` já separou os dois pelo mesmo
-- motivo, e usar o mesmo NOME nas duas tabelas é o que permite ao repositório
-- traduzir `scope='own'` com uma regra só, sem `if recurso == 'calendar'`.
ALTER TABLE stay_blocks ADD COLUMN owner_id uuid REFERENCES users(id);

COMMENT ON COLUMN stay_blocks.owner_id IS
    'Dono comercial da linha, para scope=own. Bloco de reserva espelha reservations.owner_id; bloqueio operacional herda quem criou. Anulável: importação de OTA e bloqueio da casa nascem sem dono e não aparecem para quem tem escopo own.';

-- Backfill na ordem certa: bloco de reserva pega o dono DA RESERVA (e cai em
-- `created_by` quando a reserva também não tem dono); bloqueio operacional
-- herda quem o criou, que ali é a mesma pessoa.
UPDATE stay_blocks b
   SET owner_id = COALESCE(r.owner_id, b.created_by)
  FROM reservations r
 WHERE r.id = b.reservation_id AND b.owner_id IS NULL;

UPDATE stay_blocks
   SET owner_id = created_by
 WHERE reservation_id IS NULL AND owner_id IS NULL;

-- Parcial pela mesma razão de `reservations_owner_idx`: bloco de OTA e bloqueio
-- da operação nascem sem dono, e o índice não precisa carregá-los.
CREATE INDEX stay_blocks_owner_idx ON stay_blocks(owner_id) WHERE owner_id IS NOT NULL;
