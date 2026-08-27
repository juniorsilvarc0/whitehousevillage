-- O barramento de tempo real: `LISTEN`/`NOTIFY` para o mapa de ocupação e o
-- funil. O módulo `internal/modules/stream` escuta os canais criados aqui.
--
-- ═════════════════════ O QUE TRAFEGA, E O QUE NÃO ═══════════════════════
--
-- O payload é MAGRO por decisão de segurança, não por economia de bytes:
--
--     {"topic":"calendar","entity":"stay_block","id":"…","unit_id":"…",
--      "property_id":"…","v":123456}
--     {"topic":"crm","entity":"opportunity","id":"…","pipeline_id":"…",
--      "property_id":"…","v":123456}
--
-- Só identificadores. Nome de hóspede, valor da reserva, telefone e motivo de
-- cancelamento NÃO entram. A razão é que `NOTIFY` não tem RBAC: quem der
-- `LISTEN` numa conexão do banco recebe tudo, e o eixo `scope='own'` — que é o
-- que faz o corretor ver só a carteira dele — vive no `WHERE` do repositório,
-- não no barramento. Mandar a linha inteira pelo canal seria construir, ao lado
-- da API, um segundo caminho de leitura sem permissão nenhuma.
--
-- O contrato com o cliente é, portanto: o evento diz O QUE mudou, e o cliente
-- REFAZ O FETCH pela API, com o token dele e o RBAC aplicado. Se ele não pode
-- ver aquela reserva, o refetch devolve 404 e a tela não muda — que é
-- exatamente o comportamento correto.
--
-- Efeito colateral bem-vindo: o limite de 8 kB do `pg_notify` deixa de ser um
-- risco. O payload tem tamanho FIXO por construção (cinco UUIDs e um inteiro,
-- ~200 bytes), então não existe reserva grande o bastante, nem cancelamento com
-- justificativa longa o bastante, para estourá-lo. Um payload que carregasse a
-- linha estouraria o limite justamente no caso extremo — e `NOTIFY` que estoura
-- aborta a TRANSAÇÃO DE NEGÓCIO, ou seja, a venda falharia por causa do
-- tempo real. Aqui isso é impossível.
--
-- ─────────────────────────── Os canais ──────────────────────────────
--
--   `whv_calendar` — `stay_blocks` e `reservations` (o mapa de ocupação)
--   `whv_crm`      — `crm_opportunities` (o kanban)
--
-- Um canal por assunto, e não um canal só, porque `LISTEN` é por conexão: a
-- aba que só olha o kanban não precisa acordar a cada check-out. O campo
-- `topic` fica no payload MESMO ASSIM, para o envelope ser auto-descritivo — um
-- consumidor que amanhã multiplexe os dois canais numa conexão só não precisa
-- guardar de qual canal cada mensagem veio.
--
-- ─────────────────────────── O campo `v` ────────────────────────────
--
-- `v` é o id da transação que produziu a mudança (`pg_current_xact_id()`), e
-- não um contador de mensagens. A escolha resolve duas coisas que um contador
-- não resolveria:
--
--   1. TODOS os eventos de uma mesma mudança atômica compartilham `v`. Vender a
--      White House Completa emite 8 eventos de `stay_block` + 1 de
--      `reservation`; com o mesmo `v`, o cliente sabe que são UMA venda e faz
--      UM refetch, em vez de nove.
--   2. `v` MENOR que o último aplicado é resposta velha, e o cliente descarta
--      sem refetch — a defesa contra a entrega fora de ordem que acontece
--      quando o front reconecta.
--
-- O que `v` NÃO é: uma sequência densa. Ids de transação são consumidos por
-- transações que abortam (um `409 DATE_CONFLICT` queima um), então buraco em
-- `v` é normal e NÃO significa evento perdido. Cliente que tratar buraco como
-- perda vai refazer a tela inteira a cada conflito de data.
--
-- ─────────────────── Por que trigger, e não NOTIFY no Go ────────────
--
-- Porque `NOTIFY` é transacional: a mensagem só é entregue se a transação
-- COMITAR. Emitindo do Go depois do commit existiriam dois modos de falha reais
-- — o processo morre entre o commit e o notify (a venda aconteceu e o mapa não
-- soube), e o notify sai de uma transação que depois faz rollback (o mapa
-- mostra uma reserva que não existe). No trigger, os dois são impossíveis por
-- construção. E o `UPDATE` que o job de expiração faz em massa passa a
-- notificar também, sem que o job precise saber que o tempo real existe.

-- ─────────────── O envelope, e por que ele não é uma função ─────────────
--
-- Todo evento sai com a MESMA forma, montada com `json_build_object` (e não com
-- concatenação de texto: escapar JSON à mão é como um `"` num campo futuro vira
-- payload inválido no cliente):
--
--     topic        assunto do canal          'calendar' | 'crm'
--     entity       o que mudou               'stay_block' | 'reservation' | 'opportunity'
--     id           a linha que mudou
--     property_id  a casa                    (roteamento; hoje há uma só)
--     v            a transação de origem     (veja acima)
--     + UMA chave de escopo com o nome que a entidade já usa na API:
--       `unit_id` no bloco, `pipeline_id` na oportunidade, nenhuma na reserva.
--
-- Isso ficava numa função `notificar_mudanca(...)` compartilhada — a forma
-- óbvia de não repetir o envelope em três lugares. Ela foi REMOVIDA depois da
-- medição, e o número é a razão: uma venda da White House Completa emite 9
-- notificações, e cada chamada aninhada de PL/pgSQL custava ~220 µs, somando
-- **+1,98 ms por venda** (medido com `pgbench -c 1 -t 800` no caminho de
-- inserção puro: 6,90 ms sem notificação → 8,88 ms com). Montando o mesmo JSON
-- DENTRO da função de gatilho, sem a chamada intermediária, o custo cai para
-- **+0,3 ms por venda** — seis vezes menor, mesmo payload, byte a byte.
--
-- O preço da decisão é honesto: a forma do envelope aparece em três funções em
-- vez de uma, e mudar um campo exige mexer nas três. Está escrito aqui em cima
-- para que a próxima pessoa não descubra a divergência por um cliente quebrado.

-- ─────────────────────────── O mapa de ocupação ─────────────────────
--
-- A chave de escopo é `unit_id`: é a coluna pela qual o mapa é desenhado, e é o que
-- permite ao cliente refazer o fetch de UMA coluna do calendário em vez do mês
-- inteiro.
CREATE FUNCTION trg_notificar_stay_block() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    l record := CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
BEGIN
    PERFORM pg_notify('whv_calendar', json_build_object(
        'topic',       'calendar',
        'entity',      'stay_block',
        'id',          l.id,
        'unit_id',     l.unit_id,
        'property_id', l.property_id,
        'v',           pg_current_xact_id()::text::bigint)::text);

    -- Realocação move o bloco de unidade: a coluna ANTIGA do mapa também tem de
    -- se redesenhar, senão a reserva aparece nas duas até alguém dar F5.
    IF TG_OP = 'UPDATE' AND OLD.unit_id IS DISTINCT FROM NEW.unit_id THEN
        PERFORM pg_notify('whv_calendar', json_build_object(
            'topic',       'calendar',
            'entity',      'stay_block',
            'id',          OLD.id,
            'unit_id',     OLD.unit_id,
            'property_id', OLD.property_id,
            'v',           pg_current_xact_id()::text::bigint)::text);
    END IF;
    RETURN NULL;
END;
$$;

-- O `WHEN` é o que separa "mudou o que o mapa desenha" de "mudou qualquer
-- coisa". Sem ele, um `UPDATE` que só acerta a `note` acordaria todas as abas
-- abertas da gestão. As quatro colunas do predicado são exatamente as que o
-- mapa lê: status (a cor), período (a largura), unidade (a coluna) e dono (o
-- filtro `scope='own'`).
CREATE TRIGGER stay_blocks_notificar
    AFTER INSERT OR DELETE ON stay_blocks
    FOR EACH ROW EXECUTE FUNCTION trg_notificar_stay_block();

CREATE TRIGGER stay_blocks_notificar_mudanca
    AFTER UPDATE ON stay_blocks
    FOR EACH ROW WHEN (
        OLD.status   IS DISTINCT FROM NEW.status   OR
        OLD.period   IS DISTINCT FROM NEW.period   OR
        OLD.unit_id  IS DISTINCT FROM NEW.unit_id  OR
        OLD.owner_id IS DISTINCT FROM NEW.owner_id)
    EXECUTE FUNCTION trg_notificar_stay_block();

-- ─────────────────────────── A reserva ──────────────────────────────
--
-- O mapa mostra o CÓDIGO da reserva em cima do bloco, e a lista de reservas é
-- outra tela que precisa se mexer sozinha. `stay_blocks` sozinho não bastaria:
-- confirmar uma pré-reserva muda `reservations.status` e os blocos juntos, mas
-- registrar o check-in ou trocar o dono comercial mexe só na reserva.
--
-- Não há chave de escopo: a reserva da Completa cobre oito unidades, e escolher uma
-- delas seria arbitrário. O cliente resolve as unidades no refetch.
CREATE FUNCTION trg_notificar_reserva() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    l record := CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
BEGIN
    PERFORM pg_notify('whv_calendar', json_build_object(
        'topic',       'calendar',
        'entity',      'reservation',
        'id',          l.id,
        'property_id', l.property_id,
        'v',           pg_current_xact_id()::text::bigint)::text);
    RETURN NULL;
END;
$$;

CREATE TRIGGER reservations_notificar
    AFTER INSERT OR DELETE ON reservations
    FOR EACH ROW EXECUTE FUNCTION trg_notificar_reserva();

-- `hold_expires_at` entra no predicado porque `extend-hold` não muda mais nada
-- — e é justamente o relógio que a tela mostra correndo.
CREATE TRIGGER reservations_notificar_mudanca
    AFTER UPDATE ON reservations
    FOR EACH ROW WHEN (
        OLD.status          IS DISTINCT FROM NEW.status          OR
        OLD.check_in        IS DISTINCT FROM NEW.check_in        OR
        OLD.check_out       IS DISTINCT FROM NEW.check_out       OR
        OLD.unit_type_id    IS DISTINCT FROM NEW.unit_type_id    OR
        OLD.owner_id        IS DISTINCT FROM NEW.owner_id        OR
        OLD.hold_expires_at IS DISTINCT FROM NEW.hold_expires_at)
    EXECUTE FUNCTION trg_notificar_reserva();

-- ─────────────────────────── O funil ────────────────────────────────
--
-- A chave de escopo é `pipeline_id`: é o quadro inteiro que se redesenha, e é por ele
-- que o cliente decide se o evento lhe interessa (a aba aberta no funil de
-- eventos ignora o movimento do funil de hospedagem).
CREATE FUNCTION trg_notificar_oportunidade() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    l record := CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
BEGIN
    PERFORM pg_notify('whv_crm', json_build_object(
        'topic',       'crm',
        'entity',      'opportunity',
        'id',          l.id,
        'pipeline_id', l.pipeline_id,
        'property_id', l.property_id,
        'v',           pg_current_xact_id()::text::bigint)::text);

    -- Mover o card entre funis é raro, mas quando acontece o quadro de ORIGEM
    -- também precisa perder o cartão.
    IF TG_OP = 'UPDATE' AND OLD.pipeline_id IS DISTINCT FROM NEW.pipeline_id THEN
        PERFORM pg_notify('whv_crm', json_build_object(
            'topic',       'crm',
            'entity',      'opportunity',
            'id',          OLD.id,
            'pipeline_id', OLD.pipeline_id,
            'property_id', OLD.property_id,
            'v',           pg_current_xact_id()::text::bigint)::text);
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER crm_opportunities_notificar
    AFTER INSERT OR DELETE ON crm_opportunities
    FOR EACH ROW EXECUTE FUNCTION trg_notificar_oportunidade();

-- O kanban desenha coluna (`stage_id`), cartão (`title`, `amount_cents`),
-- responsável (`owner_id`) e o relógio do SLA (`entered_stage_at`). `updated_at`
-- de propósito fora: ele muda em toda escrita, e incluí-lo tornaria o `WHEN`
-- sempre verdadeiro — ou seja, nenhum filtro.
CREATE TRIGGER crm_opportunities_notificar_mudanca
    AFTER UPDATE ON crm_opportunities
    FOR EACH ROW WHEN (
        OLD.stage_id         IS DISTINCT FROM NEW.stage_id         OR
        OLD.pipeline_id      IS DISTINCT FROM NEW.pipeline_id      OR
        OLD.status           IS DISTINCT FROM NEW.status           OR
        OLD.owner_id         IS DISTINCT FROM NEW.owner_id         OR
        OLD.amount_cents     IS DISTINCT FROM NEW.amount_cents     OR
        OLD.title            IS DISTINCT FROM NEW.title            OR
        OLD.entered_stage_at IS DISTINCT FROM NEW.entered_stage_at)
    EXECUTE FUNCTION trg_notificar_oportunidade();
