-- Fecha as lacunas que apareceram ao escrever o seed e o módulo de perfis:
-- o vínculo do usuário com o corretor, o catálogo de recursos como DADO
-- completo, e as chaves naturais que faltavam para o seed ser idempotente.

-- ─────────────────── Usuário × corretor ───────────────────
-- Previsto em docs/db.md (`users(..., broker_id?, ...)`) e no contrato, mas
-- ausente na 20260820120000_core: sem ele o campo `broker_id` de /users sai
-- sempre null. Fica SEM foreign key por enquanto porque `brokers` só nasce na
-- migration do financeiro; a FK entra lá, junto com a tabela que ela referencia.
ALTER TABLE users ADD COLUMN broker_id uuid;
CREATE INDEX users_broker_idx ON users(broker_id) WHERE broker_id IS NOT NULL;

-- ─────────────────── Catálogo de recursos ───────────────────
-- `GET /roles/resources` é a fonte única da grade de permissões: precisa dizer
-- quais AÇÕES o recurso oferece e se o escopo `own` faz sentido nele. Isso
-- estava numa lista Go (internal/modules/roles/catalogo.go), o que contradiz a
-- regra 8: RBAC é dado. Não é decisão de autorização — quem autoriza continua
-- sendo role_permissions — é metadado de TELA: onde não há dono, a grade
-- desabilita "só os meus" em vez de oferecer um escopo que o SQL não sabe
-- aplicar; onde não há "excluir" (o razão financeiro é append-only, mensagem
-- enviada não se apaga), a coluna nem aparece.
ALTER TABLE resources
    ADD COLUMN supports_own boolean NOT NULL DEFAULT false,
    ADD COLUMN actions      text[]  NOT NULL DEFAULT ARRAY['ver','criar','editar','excluir'],
    ADD COLUMN sort_order   int     NOT NULL DEFAULT 0;

-- O mesmo vocabulário do CHECK de role_permissions.action, agora também no
-- catálogo: sem isto uma ação inventada entraria como texto livre e a grade
-- mostraria uma coluna que nenhum perfil consegue conceder.
ALTER TABLE resources ADD CONSTRAINT resources_actions_validas CHECK (
    cardinality(actions) > 0 AND actions <@ ARRAY['ver','criar','editar','excluir']
);

-- ─────────────────── Chaves naturais do seed ───────────────────
-- Estas três tabelas nasceram só com id gerado. Um seed idempotente precisa de
-- uma chave que ele saiba reproduzir na segunda execução: com id novo a cada
-- rodada, `ON CONFLICT` nunca dispara e a segunda execução duplica tudo.
--
-- Nomes de período carregam o ano de propósito ("Réveillon 2026/2027"): o
-- Réveillon do ano seguinte é OUTRA linha, não uma edição desta.
CREATE UNIQUE INDEX special_periods_nome_idx ON special_periods(property_id, name);
CREATE UNIQUE INDEX rate_tables_nome_idx     ON rate_tables(property_id, name);

-- A faixa de cancelamento é identificada pela posição dentro da política, não
-- pelo rótulo: reescrever "Retenção de 50%" para "Retenção parcial" é edição da
-- mesma faixa, não uma faixa nova.
CREATE UNIQUE INDEX cancellation_tiers_ordem_idx ON cancellation_tiers(policy_id, sort_order);
