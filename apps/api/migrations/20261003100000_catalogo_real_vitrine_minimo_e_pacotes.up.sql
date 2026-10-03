-- Catálogo real da casa: três peças de schema que o tarifário de demonstração
-- não precisava e o catálogo definido pelo dono precisa.
--
--   (a) unit_types.public_name — nome de vitrine (site), separado do nome
--       interno que a operação usa ("AP 01 — Duplex Aurora" no painel,
--       "Duplex Aurora" no site).
--   (b) unit_type_min_nights — estadia mínima POR PRODUTO, que sobrepõe a regra
--       geral de min_nights_rules para o mesmo tipo de data.
--   (c) rate_packages — preço por duração: N noites consecutivas pelo total do
--       pacote, e não pela soma das diárias.
--
-- As três penduram em rate_tables/unit_types como `rates` e `min_nights_rules`
-- já penduram: o property_id chega pelo tarifário, e repeti-lo aqui seria uma
-- segunda fonte da mesma verdade (§4 de docs/db.md).

-- ─────────────────────────── (a) nome de vitrine ───────────────────────────
ALTER TABLE unit_types
    ADD COLUMN public_name text NULL
        CONSTRAINT unit_types_public_name_nao_vazio CHECK (btrim(public_name) <> '');

COMMENT ON COLUMN unit_types.public_name IS
    'Nome de vitrine, o que o site público mostra. NULL = usa name. Nunca vazio: string em branco esconderia o produto na vitrine sem cair no fallback.';

-- ─────────────────── (b) estadia mínima por produto ────────────────────────
CREATE TABLE unit_type_min_nights (
    rate_table_id uuid NOT NULL REFERENCES rate_tables(id) ON DELETE CASCADE,
    unit_type_id  uuid NOT NULL REFERENCES unit_types(id)  ON DELETE CASCADE,
    date_type     text NOT NULL REFERENCES date_type_rules(kind),
    nights        int  NOT NULL CHECK (nights > 0),
    PRIMARY KEY (rate_table_id, unit_type_id, date_type)
);
-- A PK cobre rate_table_id. unit_type_id precisa do próprio índice: apagar um
-- produto faz o CASCADE procurar por ele aqui.
CREATE INDEX unit_type_min_nights_unit_type_idx ON unit_type_min_nights(unit_type_id);

COMMENT ON TABLE unit_type_min_nights IS
    'Estadia mínima por produto e tipo de data, dentro de um tarifário. Quando existe linha para (tarifário, produto, tipo), ela SOBREPÕE min_nights_rules do mesmo tipo; sem linha, vale a regra geral. Quem aplica é o domínio (booking).';

-- ─────────────────────── (c) pacotes por duração ───────────────────────────
CREATE TABLE rate_packages (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rate_table_id uuid NOT NULL REFERENCES rate_tables(id) ON DELETE CASCADE,
    unit_type_id  uuid NOT NULL REFERENCES unit_types(id)  ON DELETE CASCADE,
    nights        int  NOT NULL CHECK (nights >= 2),
    -- Não há FK de array no Postgres; o vocabulário é o mesmo CHECK de
    -- date_type_rules.kind. `<@` com elemento NULL é falso, então NULL dentro
    -- do array também é recusado.
    date_types    text[] NOT NULL
        CONSTRAINT rate_packages_date_types_validos CHECK (
            cardinality(date_types) > 0
            AND date_types <@ ARRAY['normal','fds','feriado','alta','reveillon','carnaval']::text[]
        ),
    total_cents   bigint NOT NULL CHECK (total_cents > 0),
    CONSTRAINT rate_packages_unicos UNIQUE (rate_table_id, unit_type_id, nights, date_types)
);
-- A UNIQUE cobre rate_table_id; unit_type_id ganha índice pelo CASCADE.
CREATE INDEX rate_packages_unit_type_idx ON rate_packages(unit_type_id);

COMMENT ON TABLE rate_packages IS
    'Preço por duração (pacote). N noites CONSECUTIVAS cujos tipos de data estão todos em date_types podem ser cobradas por total_cents em vez da soma das diárias. É só o dado: quem decide se e onde o pacote se aplica é o domínio (booking).';
COMMENT ON COLUMN rate_packages.date_types IS
    'Tipos de data que o pacote cobre; cada noite do bloco precisa ter o tipo dentro desta lista. A UNIQUE compara o array como está escrito — {normal,fds} e {fds,normal} são linhas diferentes; o seed grava em ordem fixa.';
