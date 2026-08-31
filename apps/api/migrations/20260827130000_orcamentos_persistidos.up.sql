-- O orçamento passa a EXISTIR no banco.
--
-- ═══════════════════════ O QUE ESTA MIGRATION DESTRAVA ══════════════════════
--
-- Hoje `POST /quotes` roda o motor `internal/domain/booking`, devolve o cálculo
-- inteiro aberto e JOGA FORA. Três consequências, todas medidas nesta árvore:
--
--   · `/crm/opportunities/{id}/win` pede um "orçamento vigente" e não existe
--     caminho de aplicação que crie um: a suíte do CRM fabrica a linha por SQL
--     direto (`ambiente_integration_test.go`, `orcamentoDireto` — o comentário
--     dela diz, com todas as letras, "`POST /quotes` é cálculo puro e não
--     persiste"). Um endpoint que só o teste consegue exercitar não está no ar.
--   · Reabrir o orçamento de ontem RECALCULA. Se o tarifário mudou entre
--     ontem e hoje — e mudar tarifário é operação de rotina —, o número que a
--     gestão falou ao telefone não é mais o número que a tela mostra.
--   · Não há como responder "quanto orçamos e quanto virou venda", que é a
--     única medida de conversão que o funil da spec §7 promete.
--
-- ═══════════════════ POR QUE TABELA PRÓPRIA, E NÃO `reservations` ═══════════
--
-- `reservations` já tem o estado `quote`, e a tentação é usá-lo. Foi recusado:
--
--   · Toda reserva ocupa uma unidade física — é o que `reservation_units` e
--     `stay_blocks` significam. Orçamento NÃO bloqueia data (spec §4), então
--     seria uma reserva permanentemente sem unidade: a exceção que enfraquece
--     a leitura de todo o resto do módulo.
--   · O funil emite VÁRIOS orçamentos para a mesma negociação (proposta,
--     contraproposta, desconto revisto). Como reserva, cada tentativa vira uma
--     linha em `reservations` que nunca foi venda, poluindo contagem, código
--     sequencial (`WH-2026-…` gastaria número em proposta recusada) e todo
--     relatório que parte de "quantas reservas".
--   · `reservations` está com 23 colunas e o teto da regra 9 é ~25. Aceitar
--     `valid_until` e `opportunity_id` lá dentro gastaria as duas últimas
--     vagas com um ciclo de vida que não é o da reserva.
--
-- E, principalmente: os dois objetos têm DONOS diferentes no tempo. A reserva
-- muda de estado a vida inteira; o orçamento é um instantâneo que, uma vez
-- emitido, não muda mais. Misturá-los é o mesmo erro que a extração de
-- `reservation_pricing` (dívida D2) desfez em 20260826100000.
--
-- ═══════════════════════ O QUE **NÃO** ESTÁ AQUI ════════════════════════════
--
-- NENHUMA referência a `stay_blocks`, e isso é a regra de negócio, não
-- esquecimento: "Orçamento (`POST /quotes`) não bloqueia data. Só a pré-reserva
-- bloqueia" (spec §4). Um orçamento que segurasse data faria o funil inteiro
-- travar o calendário com proposta que ninguém aceitou — e a Completa, que
-- consome as oito unidades, travaria a casa a cada simulação de preço.
-- Consequência aceita e correta: um orçamento pode virar `409 DATE_CONFLICT` na
-- hora de virar reserva.

CREATE TABLE quotes (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id            uuid NOT NULL REFERENCES properties(id),

    -- Anulável porque a simulação de preço vem antes da pessoa: o corretor
    -- abre o QuoteBuilder para responder "quanto fica o Réveillon?" e só
    -- depois cadastra quem perguntou. Orçamento sem contato é rascunho de
    -- preço; com contato, é proposta que se manda.
    contact_id             uuid REFERENCES contacts(id),

    -- O vínculo com o funil. `ON DELETE SET NULL` e não CASCADE: o expurgo de
    -- uma negociação não pode apagar o histórico de preço que a casa praticou,
    -- que é dado comercial agregado e não PII.
    --
    -- É N:1 de propósito — uma negociação emite vários orçamentos ao longo da
    -- conversa, e todos ficam. Qual deles está DE PÉ é a pergunta do CRM, não
    -- desta tabela (veja a nota do integrador no fim do arquivo).
    opportunity_id         uuid REFERENCES crm_opportunities(id) ON DELETE SET NULL,

    -- ── O que foi orçado ──────────────────────────────────────────────
    unit_type_id           uuid NOT NULL REFERENCES unit_types(id),
    check_in               date NOT NULL,
    check_out              date NOT NULL,          -- exclusivo: '[in, out)'
    guests_count           int  NOT NULL CHECK (guests_count > 0),
    is_event               boolean NOT NULL DEFAULT false,

    -- ── Os valores CONGELADOS (CLAUDE.md regra 7) ─────────────────────
    -- Não são cache do que o motor calcularia hoje: são o que o motor calculou
    -- NAQUELE dia, com aquela tabela e aquela política. Reler um orçamento de
    -- ontem tem de devolver o preço de ontem, mesmo que a tarifa tenha mudado
    -- às 6h da manhã.
    subtotal_cents         bigint NOT NULL CHECK (subtotal_cents >= 0),
    discount_pct           numeric(5,2) NOT NULL DEFAULT 0 CHECK (discount_pct BETWEEN 0 AND 100),
    discount_cents         bigint NOT NULL DEFAULT 0 CHECK (discount_cents >= 0),
    cleaning_cents         bigint NOT NULL DEFAULT 0 CHECK (cleaning_cents >= 0),
    event_deposit_cents    bigint NOT NULL DEFAULT 0 CHECK (event_deposit_cents >= 0),
    total_cents            bigint NOT NULL CHECK (total_cents >= 0),
    deposit_cents          bigint NOT NULL DEFAULT 0 CHECK (deposit_cents >= 0),

    -- `balance_cents` (saldo) e `avg_nightly_cents` (diária média) NÃO são
    -- colunas: são `total − sinal` e `total / noites`, exatos em inteiro, e
    -- guardar o que se deriva é criar a segunda verdade que um dia diverge.

    -- ── As referências que o snapshot precisa para se explicar ────────
    -- NOT NULL, ao contrário de `reservation_pricing`: lá a linha pode nascer
    -- com uma reserva em `quote` que ainda não escolheu tarifário; aqui a linha
    -- SÓ existe porque o motor rodou, e o motor não roda sem tabela e sem
    -- política. Um orçamento que não sabe dizer com que preço foi feito não é
    -- um orçamento, é um número solto.
    rate_table_id          uuid NOT NULL REFERENCES rate_tables(id),
    policy_version         int  NOT NULL CHECK (policy_version > 0),
    -- Anulável: a política de cancelamento não entra na conta do total, e uma
    -- propriedade recém-configurada pode ainda não ter publicado nenhuma.
    cancellation_policy_id uuid REFERENCES cancellation_policies(id),

    -- Dono comercial — `scope = 'own'` vira `AND owner_id = $usuario` no SQL
    -- (CLAUDE.md regra 8). Mesma escolha de `reservations.owner_id`: é
    -- `users(id)`, porque quem o RBAC filtra é o usuário autenticado, e o
    -- vínculo com a carteira já vive em `users.broker_id`. Anulável porque
    -- orçamento gerado pelo agente de IA ou por importação nasce sem dono, e
    -- nesse caso simplesmente não aparece para quem tem escopo `own`.
    owner_id               uuid REFERENCES users(id),

    -- Validade. É o que separa "proposta em pé" de "preço que já venceu", e é
    -- por isso que um orçamento não precisa ser apagado nem reescrito: ele
    -- expira. `timestamptz` e não `date` porque o vencimento é um instante — a
    -- tela e o job precisam saber se venceu AGORA, e "agora" tem fuso
    -- (America/Fortaleza, CLAUDE.md regra 5).
    valid_until            timestamptz NOT NULL,

    -- A reserva em que este orçamento virou venda. É esta coluna que `/win`
    -- preenche, e é ela que responde a conversão do funil sem recalcular nada.
    reservation_id         uuid REFERENCES reservations(id) ON DELETE SET NULL,

    created_by             uuid REFERENCES users(id),
    created_at             timestamptz NOT NULL DEFAULT now(),
    -- Existe apesar de o snapshot ser imutável: `opportunity_id` e
    -- `reservation_id` são preenchidos DEPOIS da emissão, e é bom saber quando.
    -- Os valores, esses, não voltam a mudar — reprecificar é emitir outro.
    updated_at             timestamptz NOT NULL DEFAULT now(),

    -- 25 colunas, no teto da regra 9. As candidatas cortadas estão ditas acima
    -- (saldo, diária média) e na nota do integrador (código legível).

    CONSTRAINT quotes_estadia CHECK (check_out > check_in),

    -- Sanidade aritmética, não alçada comercial. A alçada (≤5% a gestão fecha,
    -- 6–10% pede o proprietário, >10% não sai — spec §3) é política versionada
    -- avaliada no domínio: como CHECK fixo viraria número de schema, e todo
    -- orçamento antigo passaria a violar a regra nova no dia em que a gestão
    -- mudasse o limite.
    CONSTRAINT quotes_desconto_cabe CHECK (discount_cents <= subtotal_cents),

    -- A identidade do motor, escrita no banco: `booking.Build` faz
    -- `Total = Subtotal − Desconto + Limpeza + Caução`, e o arredondamento
    -- acontece no total, nunca noite a noite — então a igualdade é EXATA em
    -- centavos. Isto é o que impede um snapshot de nascer contando uma história
    -- que não fecha; sem ela, "o desconto incide só sobre as diárias" seria
    -- promessa de documentação e não fato do dado.
    CONSTRAINT quotes_total_fecha CHECK (
        total_cents = subtotal_cents - discount_cents + cleaning_cents + event_deposit_cents),

    CONSTRAINT quotes_sinal_cabe CHECK (deposit_cents <= total_cents),

    -- Caução é de evento. Fora dele, é cobrança sem fato gerador.
    CONSTRAINT quotes_caucao_so_em_evento CHECK (is_event OR event_deposit_cents = 0),

    -- Proposta que nasce vencida não é proposta.
    CONSTRAINT quotes_validade CHECK (valid_until > created_at)
);

COMMENT ON TABLE quotes IS
    'Orçamento emitido e CONGELADO: produto, datas, hóspedes e os valores que o motor calculou naquele dia, com a tabela de tarifas e a versão de política que valiam. Não bloqueia data (spec §4) — nenhuma linha aqui toca stay_blocks. Reprecificar é emitir outro, nunca UPDATE.';
COMMENT ON COLUMN quotes.valid_until IS
    'Até quando este preço está de pé. É o que faz o orçamento vencer sozinho em vez de precisar ser apagado.';
COMMENT ON COLUMN quotes.reservation_id IS
    'A reserva que este orçamento virou. Preenchida por /win e pela criação de pré-reserva a partir do orçamento; é a medida de conversão do funil.';
COMMENT ON COLUMN quotes.rate_table_id IS
    'A tabela de tarifas que precificou. Congelada (regra 7): mudar o tarifário amanhã não reescreve este número.';

-- ─────────────────────────── Índices ────────────────────────────────
-- Toda FK indexada (docs/db.md, Convenções), parcial nas majoritariamente
-- nulas — o mesmo tratamento que `reservations` recebeu.

-- A listagem: "meus orçamentos, mais recentes primeiro", sempre dentro da
-- propriedade. Cobre também a FK `property_id`.
CREATE INDEX quotes_prop_criacao_idx ON quotes(property_id, created_at DESC);

-- "O que está em pé para fechar" e a varredura do que venceu.
CREATE INDEX quotes_validade_idx ON quotes(property_id, valid_until);

-- Conversão e histórico de preço por produto — o GROUP BY do funil.
CREATE INDEX quotes_produto_idx ON quotes(unit_type_id, check_in);

CREATE INDEX quotes_oportunidade_idx  ON quotes(opportunity_id)         WHERE opportunity_id IS NOT NULL;
CREATE INDEX quotes_contato_idx       ON quotes(contact_id)             WHERE contact_id IS NOT NULL;
CREATE INDEX quotes_owner_idx         ON quotes(owner_id)               WHERE owner_id IS NOT NULL;
CREATE INDEX quotes_tarifario_idx     ON quotes(rate_table_id);
CREATE INDEX quotes_cancelamento_idx  ON quotes(cancellation_policy_id) WHERE cancellation_policy_id IS NOT NULL;
CREATE INDEX quotes_criador_idx       ON quotes(created_by)             WHERE created_by IS NOT NULL;

-- Uma reserva nasce de UM orçamento. Dois orçamentos reivindicando a mesma
-- venda fariam a conversão contar duas vezes e a auditoria não saber qual
-- preço foi o combinado.
CREATE UNIQUE INDEX quotes_reserva_uniq ON quotes(reservation_id) WHERE reservation_id IS NOT NULL;

-- ═════════════════════ As noites do orçamento ═══════════════════════
--
-- Mesma ideia — e o mesmo formato — de `reservation_nights`: a tarifa APLICADA
-- em cada noite, com o tipo de data que a classificou. É o que faz o orçamento
-- se explicar linha a linha ("Fim de semana 2× R$ 4.800") sem consultar o
-- tarifário de novo, e o que faz a leitura de amanhã devolver o preço de hoje.
--
-- Sem ela, `subtotal_cents` seria um número sem prova: ninguém saberia dizer
-- QUAIS noites o compuseram, e o QuoteBuilder teria de recalcular para desenhar
-- o detalhe — que é exatamente o recálculo que esta migration existe para
-- eliminar.
CREATE TABLE quote_nights (
    quote_id     uuid NOT NULL REFERENCES quotes(id) ON DELETE CASCADE,
    night        date NOT NULL,
    date_type    text NOT NULL REFERENCES date_type_rules(kind),
    unit_type_id uuid NOT NULL REFERENCES unit_types(id),
    price_cents  bigint NOT NULL CHECK (price_cents >= 0),
    PRIMARY KEY (quote_id, night)
);

COMMENT ON TABLE quote_nights IS
    'Tarifa congelada noite a noite do orçamento, espelhando reservation_nights. A soma FECHA com quotes.subtotal_cents — garantido pelo constraint trigger adiado quote_nights_fecham_o_orcamento.';

-- Preço orçado × preço vendido por produto e por noite: é assim que se descobre
-- se a casa está perdendo venda por preço ou por disponibilidade.
CREATE INDEX quote_nights_produto_noite_idx ON quote_nights(unit_type_id, night);

-- ═══════════ O snapshot tem de FECHAR — e isso é do banco ═══════════
--
-- O buraco é o mesmo da invariante da casa inteira (20260827100000), um andar
-- acima: constraint nenhuma vê a AUSÊNCIA de linha. Um orçamento gravado com
-- `subtotal_cents = 1.440.000` e ZERO noites passa em todos os CHECKs de
-- coluna — e é indistinguível, na leitura, de um orçamento correto, até alguém
-- abrir o detalhe e ver a tabela vazia. Pior: o `total` continua fechando com
-- o subtotal, então nem a aritmética denuncia.
--
-- O que se confere, numa instrução só:
--   · há exatamente uma noite para cada dia de `[check_in, check_out)`;
--   · a soma das noites é o `subtotal_cents` gravado;
--   · toda noite é do MESMO produto do cabeçalho.
--
-- POR QUE `CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED`, e não CHECK:
-- pela mesma razão escrita por extenso em 20260827100000 — `CHECK` não é
-- adiável e não sabe contar linhas de outra tabela, e a ordem de escrita
-- LEGÍTIMA da aplicação passa por um estado intermediário inconsistente: o
-- cabeçalho nasce antes das noites (a FK exige), portanto nasce com zero
-- noites e subtotal cheio. Estado intermediário inconsistente é normal; o que
-- não pode existir é estado inconsistente COMITADO.
CREATE FUNCTION conferir_noites_do_orcamento(p_orcamento uuid) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
    q          record;
    v_noites   int;
    v_soma     bigint;
    v_primeira date;
    v_ultima   date;
    v_produto  boolean;
BEGIN
    SELECT id, unit_type_id, check_in, check_out, subtotal_cents
      INTO q
      FROM quotes
     WHERE id = p_orcamento;

    -- Sem linha: o orçamento foi apagado nesta mesma transação e o
    -- `ON DELETE CASCADE` das noites chega aqui com o cabeçalho já ausente.
    -- Não há snapshot a defender.
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT count(*), coalesce(sum(n.price_cents), 0), min(n.night), max(n.night),
           coalesce(bool_and(n.unit_type_id = q.unit_type_id), true)
      INTO v_noites, v_soma, v_primeira, v_ultima, v_produto
      FROM quote_nights n
     WHERE n.quote_id = q.id;

    IF v_noites <> (q.check_out - q.check_in)
       OR v_primeira IS DISTINCT FROM q.check_in
       OR v_ultima   IS DISTINCT FROM (q.check_out - 1) THEN
        RAISE EXCEPTION
            'orçamento de % a % precisa de % noite(s) detalhada(s) e tem %: o preço não se explica',
            q.check_in, q.check_out, (q.check_out - q.check_in), v_noites
            USING ERRCODE    = '23514',
                  CONSTRAINT = 'quote_nights_fecham_o_orcamento',
                  TABLE      = 'quote_nights',
                  HINT       = 'grave uma linha em quote_nights para cada noite de [check_in, check_out), na mesma transação do cabeçalho';
    END IF;

    IF NOT v_produto THEN
        RAISE EXCEPTION
            'o orçamento tem noite de outro produto: a tarifa detalhada não é a do que está sendo vendido'
            USING ERRCODE    = '23514',
                  CONSTRAINT = 'quote_nights_fecham_o_orcamento',
                  TABLE      = 'quote_nights',
                  HINT       = 'quote_nights.unit_type_id tem de ser o mesmo unit_type_id do orçamento';
    END IF;

    IF v_soma <> q.subtotal_cents THEN
        RAISE EXCEPTION
            'as noites do orçamento somam % centavos e o subtotal gravado é %: o total não bate com o detalhe que o hóspede recebeu',
            v_soma, q.subtotal_cents
            USING ERRCODE    = '23514',
                  CONSTRAINT = 'quote_nights_fecham_o_orcamento',
                  TABLE      = 'quote_nights',
                  HINT       = 'subtotal_cents é a soma de quote_nights.price_cents — grave o que o motor calculou, não um número recomposto';
    END IF;
END;
$$;

COMMENT ON FUNCTION conferir_noites_do_orcamento(uuid) IS
    'Coerência do snapshot do orçamento: uma noite por dia da estadia, todas do produto orçado, somando exatamente o subtotal. Chamada por constraint triggers adiados — avalia no COMMIT, que é quando o conjunto está fechado.';

CREATE FUNCTION trg_noites_do_orcamento() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- Os dois lados de um UPDATE: mover uma noite de um orçamento para outro
    -- estraga os dois, e conferir só o destino deixaria a origem quebrada.
    IF TG_OP <> 'INSERT' THEN
        PERFORM conferir_noites_do_orcamento(OLD.quote_id);
    END IF;
    IF TG_OP <> 'DELETE' THEN
        PERFORM conferir_noites_do_orcamento(NEW.quote_id);
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION trg_noites_do_proprio_orcamento() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM conferir_noites_do_orcamento(NEW.id);
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER quote_nights_fecham_o_orcamento
    AFTER INSERT OR UPDATE OR DELETE ON quote_nights
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION trg_noites_do_orcamento();

-- O lado do cabeçalho: o orçamento que nasce SEM nenhuma noite não gera evento
-- nenhum no gatilho de cima (zero linhas, zero eventos), e é justamente o caso
-- que a leitura não distingue de um orçamento correto. `UPDATE` das colunas
-- que a soma toca entra pelo mesmo motivo: mexer no subtotal ou nas datas sem
-- mexer nas noites desfaz a coerência sem escrever uma linha em `quote_nights`.
CREATE CONSTRAINT TRIGGER quotes_fecham_com_as_noites
    AFTER INSERT OR UPDATE OF check_in, check_out, subtotal_cents, unit_type_id ON quotes
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION trg_noites_do_proprio_orcamento();

-- ═══════════════════════════ RBAC ═══════════════════════════════════
-- O recurso `quotes` JÁ existe no catálogo (semeado por `cmd/seed`, com as
-- ações e a matriz dos três perfis) — `POST /quotes` responde por ele hoje.
-- Nada a acrescentar aqui: catálogo de RBAC é dado do seed, não de migration.
