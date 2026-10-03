-- Conteúdo editável do site de vendas (menu "Site" do painel) — contrato em
-- docs/site-cms.md §2.
--
--   site_content — uma linha por campo do catálogo que a gestão EDITOU. Campo
--                  sem linha = texto original do HTML; "restaurar o original" é
--                  apagar a linha. A chave é a do catálogo em código
--                  (internal/modules/site/catalogo.go); o banco só confere a forma.
--   site_media   — arquivo enviado pelo painel (foto ou vídeo), guardado no
--                  volume da API (/data/midia). Imutável: trocar a foto é
--                  enviar arquivo novo e apontar o campo para o novo id.
--
-- Sem property_id de propósito: o site é um só (CLAUDE.md — não é multi-tenant)
-- e as duas tabelas são configuração da vitrine, não registro de negócio.
-- Sem soft delete: site_content não tem histórico (a trilha fica em
-- audit_log) e site_media é imutável.

CREATE TABLE site_content (
    key        text PRIMARY KEY
        CONSTRAINT site_content_key_formato
            CHECK (key ~ '^[a-z0-9]+([.-][a-z0-9]+)*$'),
    -- string, {media_id[, alt]} ou lista — a forma por tipo é validada pela API
    -- contra o catálogo; o banco só garante que há valor.
    value      jsonb NOT NULL
        CONSTRAINT site_content_value_nao_nulo CHECK (jsonb_typeof(value) <> 'null'),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid REFERENCES users(id)
);
CREATE INDEX site_content_updated_by_idx ON site_content(updated_by);

COMMENT ON TABLE site_content IS
    'Campos do site editados pela gestão. Sem linha = texto original do HTML. Salvar = publicar; restaurar = DELETE. Toda gravação vai para audit_log.';
COMMENT ON COLUMN site_content.key IS
    'Chave estável do catálogo de campos (ex. inicio.titulo, categoria.grand-villa.foto). Chave fora do catálogo é recusada pela API (404).';
COMMENT ON COLUMN site_content.value IS
    'Valor conforme o tipo do campo: string (texto/texto_longo/titulo), {"media_id","alt"} (imagem), {"media_id"} (video) ou array de objetos (lista). JSON null é recusado — restaurar o original é apagar a linha.';

CREATE TABLE site_media (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind          text NOT NULL
        CONSTRAINT site_media_kind_valido CHECK (kind IN ('imagem','video')),
    mime          text NOT NULL
        CONSTRAINT site_media_mime_nao_vazio CHECK (btrim(mime) <> ''),
    bytes         bigint NOT NULL
        CONSTRAINT site_media_bytes_positivo CHECK (bytes > 0),
    original_name text NOT NULL,
    storage_key   text NOT NULL
        CONSTRAINT site_media_storage_key_nao_vazia CHECK (btrim(storage_key) <> ''),
    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    uuid REFERENCES users(id),
    CONSTRAINT site_media_storage_key_unica UNIQUE (storage_key)
);
CREATE INDEX site_media_created_by_idx ON site_media(created_by);

COMMENT ON TABLE site_media IS
    'Fotos e vídeos enviados pelo painel para o site. Linha imutável: o id entra na URL pública com cache de um ano (immutable).';
COMMENT ON COLUMN site_media.storage_key IS
    'Nome do arquivo no volume de mídia da API (MEDIA_DIR). Nunca vem do nome enviado pelo usuário.';
COMMENT ON COLUMN site_media.mime IS
    'Tipo detectado pelos bytes do arquivo, não pela extensão (JPEG/PNG/WebP até 15 MB; MP4/WebM até 300 MB — limite aplicado pela API).';
