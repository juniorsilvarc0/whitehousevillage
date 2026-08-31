-- Duas dívidas apontadas pela revisão desta rodada, medidas antes de escrever.
--
-- ══════════════ 1. O documento: promessa da aplicação sem lastro ═══════════
--
-- `POST /contacts` promete `409 CONTACT_DUPLICATE` quando o documento repete.
-- Medido no banco no ar, antes desta migration:
--
--     INSERT ... doc_number '11144477735'  → INSERT 0 1
--     INSERT ... doc_number '11144477735'  → INSERT 0 1
--     SELECT count(*) → 2
--
-- O banco aceitava dois. O que segurava era `contacts_phone_idx` (único, mas do
-- TELEFONE) e, no eixo do documento, `Repository.TravarDocumento` — um
-- `pg_advisory_xact_lock` por (propriedade, tipo, número). Essa trava FUNCIONA:
-- seis criações simultâneas com o mesmo CPF deram 1× 201 e 5× 409, medido pela
-- API. Ela não é o problema.
--
-- O problema é o alcance dela, e o próprio módulo escreve isso
-- (`repository.go`, `TravarDocumento`): "o que ela NÃO faz: proteger contra
-- escrita que não passe por esta API (`psql`, importação, outro serviço)". Um
-- seed, um script de migração de base ou o segundo serviço que um dia leia esta
-- tabela criam a duplicata sem encostar na trava — e duas fichas com o mesmo
-- CPF é o hóspede com dois históricos, a reserva colada na ficha errada e a
-- exportação LGPD devolvendo metade da vida da pessoa.
--
-- O nome do índice não é escolha nova: `repository.go:571` já traduz
-- `contacts_doc_unico_idx` para `ErroDeDuplicidade{Campo: "doc_number"}`,
-- escrito à espera desta migration. Nenhuma linha de Go muda — a tradução
-- começa a disparar sozinha, e a trava vira a redundância barata que o
-- comentário de lá prevê.
--
-- `coalesce(doc_type, '')` e não `doc_type` puro: a API já recusa número sem
-- tipo (`dto.go:210`), mas um `INSERT` direto pode gravar tipo nulo, e em índice
-- único NULO é distinto de NULO — dois CPFs iguais sem tipo passariam pelo
-- índice feito para impedi-los. Justamente a escrita fora da API que motiva
-- este índice.
CREATE UNIQUE INDEX contacts_doc_unico_idx
    ON contacts (property_id, coalesce(doc_type, ''), doc_number)
 WHERE doc_number IS NOT NULL AND doc_number <> '';

COMMENT ON INDEX contacts_doc_unico_idx IS
    'Documento único por propriedade. Parcial porque anonimizar zera doc_number (LGPD) e fichas anonimizadas não colidem entre si.';

-- ═══════════ 2. A coluna morta do funil, apontando para a tabela errada ════
--
-- `crm_opportunities.quote_id` nasceu em 20260827110000 como
-- `REFERENCES reservations(id)`, quando orçamento era uma reserva no estado
-- `quote`. A migration 20260827130000 criou a tabela `quotes` e não repontou a
-- FK — de propósito, para não quebrar o CRM no meio da rodada.
--
-- O que sobrou é pior que uma FK errada: uma coluna que NINGUÉM escreve e
-- NINGUÉM lê. Medido: a oportunidade ganha pelo `/win` ficou com
-- `quote_id = NULL`, e não há um `SET quote_id` em todo o `apps/api`. Quem
-- responde "qual é o orçamento vigente" é a subconsulta de
-- `colunasDaOportunidade` — `WHERE q.opportunity_id = o.id ORDER BY
-- q.created_at DESC LIMIT 1` —, que é o fato certo: emitir outro substitui o
-- anterior, e `quotes_oportunidade_idx` a cobre.
--
-- Repontar a FK manteria uma coluna morta com o alvo certo: a próxima pessoa
-- leria `quote_id` e suporia que ela significa alguma coisa. Gravar um id de
-- `quotes` nela hoje é `23503` na cara. Some.
--
-- Se um dia o produto quiser "o orçamento ESCOLHIDO" — diferente do último
-- emitido —, a coluna volta apontando para `quotes(id)`, e aí com quem a
-- escreva.
ALTER TABLE crm_opportunities DROP COLUMN quote_id;
