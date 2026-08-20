-- Inventário, tarifário e o coração do sistema: stay_blocks.

-- ─────────────────────────── Inventário ────────────────────────────
-- Produto (o que se vende) é separado de unidade física (o que é ocupado e
-- limpo). Sem unidade nominal não existe constraint capaz de impedir
-- overbooking — e a operação não sabe qual apartamento preparar.
CREATE TABLE unit_types (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id        uuid NOT NULL REFERENCES properties(id),
    code               text NOT NULL,
    name               text NOT NULL,
    capacity           int  NOT NULL CHECK (capacity > 0),
    consumes           text NOT NULL CHECK (consumes IN ('one_member','all_members')),
    cleaning_fee_cents bigint NOT NULL DEFAULT 0 CHECK (cleaning_fee_cents >= 0),
    description        text,
    sort_order         int NOT NULL DEFAULT 0,
    active             boolean NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (property_id, code)
);

CREATE TABLE units (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id  uuid NOT NULL REFERENCES properties(id),
    code         text NOT NULL,
    name         text NOT NULL,
    floor        text,
    notes        text,
    active       boolean NOT NULL DEFAULT true,
    sort_order   int NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (property_id, code)
);

-- A White House Completa é 'all_members' e aponta para as 8 unidades: é daqui
-- que a exclusividade bidirecional nasce.
CREATE TABLE unit_type_members (
    unit_type_id uuid NOT NULL REFERENCES unit_types(id) ON DELETE CASCADE,
    unit_id      uuid NOT NULL REFERENCES units(id) ON DELETE CASCADE,
    PRIMARY KEY (unit_type_id, unit_id)
);
CREATE INDEX unit_type_members_unit_idx ON unit_type_members(unit_id);

-- ─────────────────────────── Calendário comercial ──────────────────
CREATE TABLE holidays (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),
    date        date NOT NULL,
    name        text NOT NULL,
    active      boolean NOT NULL DEFAULT true,
    UNIQUE (property_id, date)
);

-- Períodos PODEM se sobrepor de propósito (réveillon dentro da alta temporada);
-- a precedência resolve. Por isso não há constraint de exclusão aqui.
CREATE TABLE special_periods (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),
    name        text NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('reveillon','carnaval','alta','evento')),
    starts_on   date NOT NULL,
    ends_on     date NOT NULL,
    active      boolean NOT NULL DEFAULT true,
    CHECK (ends_on >= starts_on)
);
CREATE INDEX special_periods_range_idx ON special_periods
    USING gist (daterange(starts_on, ends_on, '[]'));

CREATE TABLE date_type_rules (
    kind        text PRIMARY KEY CHECK (kind IN ('normal','fds','feriado','alta','reveillon','carnaval')),
    precedence  int  NOT NULL,
    weekday_mask int NOT NULL DEFAULT 0   -- bitmask dom..sáb; usado só por 'fds'
);

-- ─────────────────────────── Tarifário e política ──────────────────
CREATE TABLE rate_tables (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),
    name        text NOT NULL,
    valid_from  date NOT NULL,
    valid_to    date,
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE rates (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rate_table_id uuid NOT NULL REFERENCES rate_tables(id) ON DELETE CASCADE,
    unit_type_id  uuid NOT NULL REFERENCES unit_types(id) ON DELETE CASCADE,
    date_type     text NOT NULL REFERENCES date_type_rules(kind),
    amount_cents  bigint NOT NULL CHECK (amount_cents > 0),
    UNIQUE (rate_table_id, unit_type_id, date_type)
);

CREATE TABLE min_nights_rules (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rate_table_id uuid NOT NULL REFERENCES rate_tables(id) ON DELETE CASCADE,
    date_type     text NOT NULL REFERENCES date_type_rules(kind),
    nights        int NOT NULL CHECK (nights > 0),
    UNIQUE (rate_table_id, date_type)
);

CREATE TABLE commercial_policies (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id            uuid NOT NULL REFERENCES properties(id),
    version                int NOT NULL,
    deposit_pct            numeric(5,2) NOT NULL CHECK (deposit_pct BETWEEN 0 AND 100),
    balance_due_days       int NOT NULL CHECK (balance_due_days >= 0),
    hold_hours             int NOT NULL CHECK (hold_hours > 0),
    discount_auto_pct      numeric(5,2) NOT NULL,
    discount_approval_pct  numeric(5,2) NOT NULL,
    event_deposit_cents    bigint NOT NULL DEFAULT 0,
    valid_from             date NOT NULL,
    created_at             timestamptz NOT NULL DEFAULT now(),
    UNIQUE (property_id, version),
    CHECK (discount_approval_pct >= discount_auto_pct)
);

CREATE TABLE cancellation_policies (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid NOT NULL REFERENCES properties(id),
    version     int NOT NULL,
    name        text NOT NULL,
    valid_from  date NOT NULL,
    UNIQUE (property_id, version)
);

CREATE TABLE cancellation_tiers (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id         uuid NOT NULL REFERENCES cancellation_policies(id) ON DELETE CASCADE,
    days_before_min   int,          -- NULL = sem piso
    days_before_max   int,          -- NULL = sem teto
    refund_pct        numeric(5,2) NOT NULL CHECK (refund_pct BETWEEN 0 AND 100),
    label             text NOT NULL,
    sort_order        int NOT NULL DEFAULT 0
);

-- ─────────────────────────── Contatos ──────────────────────────────
CREATE TABLE contacts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id     uuid NOT NULL REFERENCES properties(id),
    name            text NOT NULL,
    email           text,
    phone_e164      text,
    doc_type        text,
    doc_number      text,
    birth_date      date,
    city            text,
    state           text,
    notes           text,
    lgpd_basis      text,
    marketing_opt_in boolean NOT NULL DEFAULT false,
    consent_at      timestamptz,
    anonymized_at   timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX contacts_phone_idx ON contacts(phone_e164) WHERE phone_e164 IS NOT NULL;
CREATE INDEX contacts_name_trgm_idx ON contacts USING gin (name gin_trgm_ops);
CREATE INDEX contacts_doc_idx ON contacts(doc_number) WHERE doc_number IS NOT NULL;

-- ─────────────────────────── Reservas ──────────────────────────────
CREATE TABLE reservations (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id            uuid NOT NULL REFERENCES properties(id),
    code                   text NOT NULL UNIQUE,           -- WH-2026-0001
    unit_type_id           uuid NOT NULL REFERENCES unit_types(id),
    contact_id             uuid NOT NULL REFERENCES contacts(id),
    broker_id              uuid,
    source                 text NOT NULL DEFAULT 'direto',
    status                 text NOT NULL CHECK (status IN
                             ('quote','hold','confirmed','checked_in','checked_out','closed','cancelled','expired','no_show')),
    check_in               date NOT NULL,
    check_out              date NOT NULL,
    guests_count           int  NOT NULL CHECK (guests_count > 0),
    is_event               boolean NOT NULL DEFAULT false,
    event_type             text,
    subtotal_cents         bigint NOT NULL DEFAULT 0,
    discount_pct           numeric(5,2) NOT NULL DEFAULT 0,
    discount_cents         bigint NOT NULL DEFAULT 0,
    cleaning_cents         bigint NOT NULL DEFAULT 0,
    event_deposit_cents    bigint NOT NULL DEFAULT 0,
    total_cents            bigint NOT NULL DEFAULT 0,
    deposit_cents          bigint NOT NULL DEFAULT 0,
    rate_table_id          uuid REFERENCES rate_tables(id),
    policy_version         int,
    cancellation_policy_id uuid REFERENCES cancellation_policies(id),
    hold_expires_at        timestamptz,
    confirmed_at           timestamptz,
    cancelled_at           timestamptz,
    cancel_reason          text,
    rebooked_from_id       uuid REFERENCES reservations(id),
    notes                  text,
    created_by             uuid REFERENCES users(id),
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CHECK (check_out > check_in),
    CHECK (discount_pct BETWEEN 0 AND 100)
);
CREATE INDEX reservations_dates_idx   ON reservations(check_in, check_out);
CREATE INDEX reservations_status_idx  ON reservations(status);
CREATE INDEX reservations_contact_idx ON reservations(contact_id);
CREATE INDEX reservations_hold_idx    ON reservations(hold_expires_at) WHERE status = 'hold';

-- Snapshot da tarifa aplicada em cada noite. É o que faz o passado não mudar
-- quando o tarifário muda — e o que permite ADR/RevPAR sairem de um GROUP BY.
CREATE TABLE reservation_nights (
    reservation_id uuid NOT NULL REFERENCES reservations(id) ON DELETE CASCADE,
    night          date NOT NULL,
    date_type      text NOT NULL REFERENCES date_type_rules(kind),
    unit_type_id   uuid NOT NULL REFERENCES unit_types(id),
    price_cents    bigint NOT NULL CHECK (price_cents >= 0),
    PRIMARY KEY (reservation_id, night)
);

-- ═══════════════════════════════════════════════════════════════════
-- stay_blocks — TODA ocupação do calendário passa por aqui.
-- Não existe calendário paralelo: reserva, manutenção, uso do proprietário e
-- importação de OTA são todos linhas desta tabela.
-- ═══════════════════════════════════════════════════════════════════
CREATE TABLE stay_blocks (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id    uuid NOT NULL REFERENCES properties(id),
    unit_id        uuid NOT NULL REFERENCES units(id) ON DELETE RESTRICT,
    reservation_id uuid REFERENCES reservations(id) ON DELETE CASCADE,
    source         text NOT NULL CHECK (source IN ('reservation','maintenance','owner_hold','ota')),
    status         text NOT NULL CHECK (status IN ('hold','confirmed','cancelled','expired')),
    period         daterange NOT NULL,        -- SEMPRE '[check_in, check_out)'
    expires_at     timestamptz,
    external_ref   text,
    note           text,
    created_by     uuid REFERENCES users(id),
    created_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT stay_period_valid CHECK (lower(period) < upper(period)),
    CONSTRAINT stay_hold_expires CHECK (status <> 'hold' OR expires_at IS NOT NULL),

    -- A garantia contra overbooking. Não é lógica de aplicação: é o banco que
    -- se recusa a aceitar duas ocupações da mesma unidade no mesmo intervalo,
    -- mesmo sob concorrência. Vender a Completa insere 8 linhas; qualquer
    -- unidade ocupada faz a inserção estourar 23P01 → 409 DATE_CONFLICT.
    CONSTRAINT stay_no_overlap EXCLUDE USING gist (
        unit_id WITH =,
        period  WITH &&
    ) WHERE (status IN ('hold','confirmed'))
);
CREATE INDEX stay_blocks_period_idx      ON stay_blocks USING gist (period);
CREATE INDEX stay_blocks_reservation_idx ON stay_blocks(reservation_id);
CREATE INDEX stay_blocks_hold_idx        ON stay_blocks(expires_at) WHERE status = 'hold';
CREATE UNIQUE INDEX stay_blocks_ota_uid  ON stay_blocks(unit_id, external_ref)
    WHERE source = 'ota' AND external_ref IS NOT NULL;

CREATE TABLE reservation_units (
    reservation_id uuid NOT NULL REFERENCES reservations(id) ON DELETE CASCADE,
    unit_id        uuid NOT NULL REFERENCES units(id),
    stay_block_id  uuid REFERENCES stay_blocks(id) ON DELETE SET NULL,
    locked         boolean NOT NULL DEFAULT false,
    PRIMARY KEY (reservation_id, unit_id)
);

CREATE TABLE reservation_guests (
    reservation_id uuid NOT NULL REFERENCES reservations(id) ON DELETE CASCADE,
    contact_id     uuid NOT NULL REFERENCES contacts(id),
    is_lead_guest  boolean NOT NULL DEFAULT false,
    PRIMARY KEY (reservation_id, contact_id)
);

CREATE TABLE reservation_events (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reservation_id uuid NOT NULL REFERENCES reservations(id) ON DELETE CASCADE,
    type           text NOT NULL,
    payload        jsonb,
    actor_id       uuid REFERENCES users(id),
    at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX reservation_events_idx ON reservation_events(reservation_id, at DESC);
