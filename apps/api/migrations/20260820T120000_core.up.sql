-- Núcleo: extensões, propriedade, identidade, RBAC por dados, auditoria e configurações.

CREATE EXTENSION IF NOT EXISTS btree_gist;   -- constraint de sobreposição de datas
CREATE EXTENSION IF NOT EXISTS pg_trgm;      -- busca por nome
CREATE EXTENSION IF NOT EXISTS pgcrypto;     -- gen_random_uuid()

-- ─────────────────────────── Propriedade ───────────────────────────
CREATE TABLE properties (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    slug        text NOT NULL UNIQUE,
    timezone    text NOT NULL DEFAULT 'America/Fortaleza',
    address     text,
    city        text,
    state       text,
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- ─────────────────────────── RBAC por dados ────────────────────────
-- Papel × recurso × ação × ESCOPO. O eixo de escopo é o que resolve
-- "corretor vê só o dele" sem espalhar `if role == ...` pelo código.
CREATE TABLE roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code        text NOT NULL UNIQUE,          -- admin | usuario | corretor
    name        text NOT NULL,
    is_system   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE resources (
    code        text PRIMARY KEY,              -- reservations, crm.opportunities, finance...
    label       text NOT NULL,
    group_label text NOT NULL
);

CREATE TABLE role_permissions (
    role_id       uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    resource_code text NOT NULL REFERENCES resources(code) ON DELETE CASCADE,
    action        text NOT NULL CHECK (action IN ('ver','criar','editar','excluir')),
    scope         text NOT NULL DEFAULT 'all' CHECK (scope IN ('all','own')),
    PRIMARY KEY (role_id, resource_code, action)
);

-- ─────────────────────────── Usuários ──────────────────────────────
CREATE TABLE users (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id    uuid NOT NULL REFERENCES properties(id),
    role_id        uuid NOT NULL REFERENCES roles(id),
    name           text NOT NULL,
    email          text NOT NULL UNIQUE,
    password_hash  text NOT NULL,
    phone          text,
    active         boolean NOT NULL DEFAULT true,
    last_login_at  timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    deleted_at     timestamptz
);
CREATE INDEX users_property_idx ON users(property_id);
CREATE INDEX users_role_idx     ON users(role_id);
CREATE UNIQUE INDEX users_email_active_idx ON users(lower(email)) WHERE deleted_at IS NULL;

-- Refresh rotativo: reusar um token revogado invalida a família inteira,
-- que é como se detecta roubo de sessão.
CREATE TABLE refresh_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id    uuid NOT NULL,
    token_hash   text NOT NULL UNIQUE,
    user_agent   text,
    ip           inet,
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    replaced_by  uuid REFERENCES refresh_tokens(id),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX refresh_tokens_user_idx   ON refresh_tokens(user_id);
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens(family_id);

CREATE TABLE password_resets (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  text NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- ─────────────────────────── Auditoria ─────────────────────────────
CREATE TABLE audit_log (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id uuid REFERENCES properties(id),
    actor_id    uuid REFERENCES users(id),
    action      text NOT NULL,
    entity      text NOT NULL,
    entity_id   uuid,
    before      jsonb,
    after       jsonb,
    ip          inet,
    user_agent  text,
    request_id  text,
    at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_entity_idx ON audit_log(entity, entity_id, at DESC);
CREATE INDEX audit_log_actor_idx  ON audit_log(actor_id, at DESC);

-- A LGPD cobra o registro de LEITURA de dado pessoal, que a auditoria comum
-- (só de escrita) não cobre.
CREATE TABLE pii_access_log (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id   uuid REFERENCES users(id),
    contact_id uuid,
    reason     text,
    at         timestamptz NOT NULL DEFAULT now()
);

-- ─────────────────────────── Configurações e idempotência ──────────
CREATE TABLE app_settings (
    namespace   text NOT NULL,
    key         text NOT NULL,
    value       jsonb NOT NULL,
    is_secret   boolean NOT NULL DEFAULT false,
    updated_by  uuid REFERENCES users(id),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (namespace, key)
);

CREATE TABLE idempotency_keys (
    key           text NOT NULL,
    endpoint      text NOT NULL,
    request_hash  text NOT NULL,
    status        int,
    response_body jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (key, endpoint)
);
