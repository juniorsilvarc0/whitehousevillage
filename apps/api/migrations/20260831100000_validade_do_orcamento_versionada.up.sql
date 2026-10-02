-- A validade do orçamento sai do binário e vira dado versionado — metade do
-- schema da dívida D7 (a outra metade é a cópia coluna a coluna do
-- `PublicarPoliticaComercial`, item F2-05).
--
-- Medido no banco no ar antes desta migration:
--
--     SELECT count(*) FROM information_schema.columns
--      WHERE table_name = 'commercial_policies';                    → 13
--     SELECT count(*) FROM information_schema.columns
--      WHERE table_name = 'commercial_policies'
--        AND column_name = 'quote_validity_days';                   → 0
--
-- Os outros três prazos da política comercial já são coluna aqui —
-- `balance_due_days`, `hold_hours` e `hold_extension_hours`. A validade do
-- orçamento era o único que morava em Go (`validadePadraoEmDias = 7`, em
-- `disponibilidade/dto_orcamento_salvo.go:22`), o que na prática significa que
-- mudar de 7 para 15 dias exigia recompilar e reimplantar a API. A gestão
-- publica versão de política pela tela; este número passa a ir junto.
--
-- Por que o passado não se move: `quotes.valid_until` é NOT NULL e gravada na
-- emissão. Esta coluna é só o PADRÃO de quem emite sem dizer até quando —
-- mudá-la nunca reescreve orçamento já emitido (CLAUDE.md regra 7).
--
-- Por que `DEFAULT 7` e não NOT NULL sem default: a v1 do seed já está
-- publicada em toda base existente e precisa de um valor. 7 é exatamente o
-- número que a aplicação vinha usando, então esta migration não muda
-- comportamento nenhum — só muda de onde o número vem.
--
-- Por que `> 0` e não `>= 0`: validade zero é um orçamento que nasce vencido —
-- `valid_until` sairia igual ao instante da emissão e a tela recusaria o
-- documento que ela mesma acabou de emitir. `hold_hours` carrega a mesma guarda
-- pelo mesmo motivo. Sem o CHECK, `quote_validity_days = 0` entra sem reclamar.
ALTER TABLE commercial_policies
    ADD COLUMN quote_validity_days int NOT NULL DEFAULT 7
        CHECK (quote_validity_days > 0);

COMMENT ON COLUMN commercial_policies.quote_validity_days IS
    'Dias de validade do orçamento emitido sem valid_until explícito. Congelado em quotes.valid_until na emissão: alterar aqui não reescreve orçamento já emitido.';

-- ATENÇÃO a quem publica versão de política (F2-05, `backend-go`):
-- `Repository.PublicarPoliticaComercial` (`tarifario/repository_politicas.go`)
-- monta o INSERT listando as colunas nome a nome. Enquanto
-- `quote_validity_days` não entrar nessa lista, toda publicação de versão nova
-- devolve este campo ao DEFAULT 7 em silêncio: a gestão mudaria a validade para
-- 15 dias, publicaria qualquer outra alteração depois, e voltaria para 7 sem
-- erro e sem linha de auditoria. O seed já foi corrigido nesta mesma entrega
-- (`cmd/seed/tarifario.go`), com teste que fica vermelho se a coluna sair da
-- lista.
