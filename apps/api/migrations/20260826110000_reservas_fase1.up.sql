-- O que a Fase 1 (spec §2 a §5) precisa e o schema ainda não tinha: dono
-- comercial da reserva, código legível sem corrida, limite de extensão de
-- pré-reserva e os índices das consultas quentes.

-- ═══════════════ 1. Dono comercial (escopo `own` do RBAC) ═══════════════
-- `role_permissions.scope = 'own'` vira `AND owner_id = $usuario` no SQL do
-- repositório (CLAUDE.md regra 8). Sem esta coluna o corretor com escopo `own`
-- não tem por onde ser filtrado, e o escopo silenciosamente vira `all`.
--
-- É `users(id)` e não `brokers(id)` de propósito: quem o RBAC filtra é o
-- USUÁRIO autenticado. O vínculo com a carteira do corretor já existe em
-- `users.broker_id`; duplicá-lo aqui criaria duas verdades sobre a mesma coisa.
--
-- Anulável porque nem toda reserva tem dono comercial: importação de OTA
-- (Fase 4) e bloqueio criado pela operação nascem sem corretor. Uma reserva sem
-- dono simplesmente não aparece para quem tem escopo `own` — que é o
-- comportamento correto, e o motivo de o índice ser parcial.
ALTER TABLE reservations ADD COLUMN owner_id uuid REFERENCES users(id);

-- Quem criou é o melhor palpite de dono para o que já existe. Vazio hoje, mas
-- uma `up` que ignora o passado é uma `up` que quebra no ambiente que tem dado.
UPDATE reservations SET owner_id = created_by WHERE owner_id IS NULL;

CREATE INDEX reservations_owner_idx ON reservations(owner_id) WHERE owner_id IS NOT NULL;

-- ═══════════════ 2. Código legível sem corrida ═══════════════
-- `WH-2026-0001`, sequencial por ano e imutável (spec §5).
--
-- POR QUE NÃO NA APLICAÇÃO: `SELECT max(code) + 1` seguido de `INSERT` é a
-- corrida clássica — duas reservas simultâneas leem o mesmo máximo e a segunda
-- só descobre o choque no `UNIQUE(code)`, virando 500 ou um retry em laço.
--
-- POR QUE NÃO UMA SEQUENCE: sequence não é transacional. Todo `ROLLBACK` (e a
-- Fase 1 tem um caminho de rollback MUITO frequente: o 23P01 de data ocupada)
-- queimaria um número. O hóspede receberia WH-2026-0004 sem que 0003 exista, e
-- a numeração de contrato passaria a ter buracos que ninguém sabe explicar.
--
-- COMO FUNCIONA: um contador por ano numa tabela. O `INSERT ... ON CONFLICT DO
-- UPDATE` é ATÔMICO: a segunda transação bloqueia na linha do ano até a
-- primeira terminar, e então lê o valor já incrementado. Não há janela entre
-- ler e gravar porque é a mesma instrução. Rollback devolve o número, então a
-- numeração é densa.
--
-- CUSTO ACEITO: o lock da linha do ano segura outras criações de reserva até o
-- commit. Para uma casa de 8 unidades isso é irrelevante, e é o preço de uma
-- numeração sem buracos.
CREATE TABLE reservation_code_counters (
    year        int PRIMARY KEY CHECK (year BETWEEN 2000 AND 2999),
    last_number int NOT NULL DEFAULT 0 CHECK (last_number >= 0),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE reservation_code_counters IS
    'Contador do código legível por ano. Serializa a numeração de reservas; ninguém escreve aqui a não ser proximo_codigo_reserva().';

-- Reconstrói o contador a partir dos códigos que já existem. Sem isto a tabela
-- nasce vazia e a numeração recomeça do 1 — o que quebra qualquer banco que já
-- tenha reservas, inclusive o que passou por `down` e voltou: a próxima reserva
-- pediria WH-2026-0001, que já está gravado, e morreria no `UNIQUE(code)`.
-- O regex ignora código fora do padrão (histórico importado com outra
-- numeração), que não deve influenciar o contador.
INSERT INTO reservation_code_counters (year, last_number)
SELECT split_part(code, '-', 2)::int AS ano,
       max(split_part(code, '-', 3)::int)
  FROM reservations
 WHERE code ~ '^WH-[0-9]{4}-[0-9]+$'
 GROUP BY 1
ON CONFLICT (year) DO NOTHING;

-- O ano é o de CRIAÇÃO em America/Fortaleza (CLAUDE.md regra 5), não o do
-- check-in: uma reserva fechada em dezembro de 2026 para janeiro de 2027 é
-- WH-2026-…, porque o código identifica o contrato, não a estadia.
CREATE FUNCTION proximo_codigo_reserva() RETURNS text
LANGUAGE sql VOLATILE AS $$
    INSERT INTO reservation_code_counters AS c (year, last_number)
    VALUES (EXTRACT(year FROM (now() AT TIME ZONE 'America/Fortaleza'))::int, 1)
    ON CONFLICT (year) DO UPDATE
       SET last_number = c.last_number + 1,
           updated_at  = now()
    RETURNING 'WH-' || c.year::text || '-' || lpad(c.last_number::text, 4, '0');
$$;

-- Como DEFAULT da coluna, e não como chamada explícita no repositório: assim
-- não existe caminho de criação de reserva que esqueça de gerar o código, e a
-- ORDEM de travamento fica garantida pelo próprio banco — a linha de
-- `reservations` nasce antes de qualquer `stay_blocks` (a FK exige), então o
-- lock do contador é sempre tomado ANTES dos locks das unidades. Ordem única de
-- aquisição é o que impede o deadlock entre as duas travas.
--
-- O repositório deve OMITIR `code` no INSERT e lê-lo no RETURNING. Passar um
-- código pronto continua sendo possível (importação, migração de histórico),
-- mas aí o contador não avança e a unicidade fica por conta do `UNIQUE(code)`.
ALTER TABLE reservations ALTER COLUMN code SET DEFAULT proximo_codigo_reserva();

-- ═══════════════ 3. Limite de extensão da pré-reserva ═══════════════
-- spec §5: "Estender a pré-reserva é ação explícita e auditada (extend-hold),
-- com limite configurável". O limite é POLÍTICA, então mora na política
-- versionada e é congelado na reserva junto com `policy_version` — mudar o
-- limite amanhã não reabre a pré-reserva que já esgotou o dela.
--
-- Quantas extensões já houve não vira coluna em `reservations`: sai de
-- `reservation_events` (`type = 'hold_extended'`), que já é o log append-only
-- da reserva. Contador denormalizado seria uma segunda verdade sobre o mesmo
-- fato, e a auditoria pediria os eventos de qualquer jeito.
--
-- Os valores padrão (uma extensão de 24 h) são ponto de partida, não regra
-- descoberta na spec: a gestão ajusta pela tela de política.
ALTER TABLE commercial_policies
    ADD COLUMN hold_extension_hours int NOT NULL DEFAULT 24 CHECK (hold_extension_hours > 0),
    ADD COLUMN hold_max_extensions  int NOT NULL DEFAULT 1  CHECK (hold_max_extensions >= 0);

-- ═══════════════ 4. Índices das consultas quentes ═══════════════
-- Só o que ainda NÃO existe. Já estavam no schema e não são duplicados aqui:
--   reservations(check_in, check_out) · reservations(status) ·
--   reservations(contact_id) · reservations(hold_expires_at) WHERE status='hold'
--   stay_blocks gist(period) · stay_blocks(reservation_id) ·
--   stay_blocks(expires_at) WHERE status='hold' ·
--   reservation_events(reservation_id, at DESC)
--
-- E, principalmente: a consulta de DISPONIBILIDADE POR UNIDADE E PERÍODO já é
-- servida pelo índice GiST que a constraint `stay_no_overlap` cria — ela é
-- `EXCLUDE USING gist (unit_id WITH =, period WITH &&) WHERE status IN
-- ('hold','confirmed')`, exatamente o predicado do mapa de ocupação. Criar um
-- índice igual ao lado dobraria o custo de escrita sem ganhar leitura nenhuma.

-- A listagem de reservas (`GET /reservations`) sempre filtra a propriedade e
-- quase sempre status + janela de datas. `reservations(status)` sozinho tem
-- cardinalidade baixa demais para resolver isso.
CREATE INDEX reservations_prop_status_check_in_idx
    ON reservations(property_id, status, check_in);

-- Ocupação e receita por PRODUTO no período — a leitura do painel e do BI.
CREATE INDEX reservations_unit_type_check_in_idx
    ON reservations(unit_type_id, check_in);

-- FKs de `reservations` que estavam sem índice (docs/db.md §14: "índice em toda
-- FK"). Parciais porque as três colunas são majoritariamente nulas — reserva
-- direta não tem corretor e quase nenhuma reserva é remarcação.
CREATE INDEX reservations_broker_idx ON reservations(broker_id) WHERE broker_id IS NOT NULL;
CREATE INDEX reservations_rebooked_from_idx ON reservations(rebooked_from_id) WHERE rebooked_from_id IS NOT NULL;
CREATE INDEX reservations_created_by_idx ON reservations(created_by) WHERE created_by IS NOT NULL;

-- "Que reserva ocupa esta unidade?" — a PK é (reservation_id, unit_id), então a
-- busca pela unidade não tinha índice. É a leitura do mapa e da governança.
CREATE INDEX reservation_units_unit_idx ON reservation_units(unit_id);
CREATE INDEX reservation_units_stay_block_idx ON reservation_units(stay_block_id) WHERE stay_block_id IS NOT NULL;

-- "Em que reservas esta pessoa foi hóspede?" — mesma situação: contact_id é a
-- segunda coluna da PK e sozinho não é indexado.
CREATE INDEX reservation_guests_contact_idx ON reservation_guests(contact_id);

-- ADR/RevPAR por produto e por noite saem de um GROUP BY sobre este snapshot
-- (docs/db.md §5). Sem índice, todo indicador é sequential scan.
CREATE INDEX reservation_nights_unit_type_night_idx ON reservation_nights(unit_type_id, night);

-- `stay_blocks.property_id` e `created_by` também eram FK sem índice; o mapa
-- filtra por propriedade antes de qualquer coisa.
CREATE INDEX stay_blocks_property_idx ON stay_blocks(property_id);
CREATE INDEX stay_blocks_created_by_idx ON stay_blocks(created_by) WHERE created_by IS NOT NULL;

-- `Idempotency-Key` é obrigatório em POST de reserva (CLAUDE.md, convenções de
-- API), então esta tabela passa a crescer de verdade na Fase 1. A varredura que
-- expira chaves antigas precisa deste índice para não varrer a tabela inteira.
CREATE INDEX idempotency_keys_created_idx ON idempotency_keys(created_at);
