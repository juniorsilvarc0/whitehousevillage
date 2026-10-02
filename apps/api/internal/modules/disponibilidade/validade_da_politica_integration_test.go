//go:build integration

package disponibilidade_test

import (
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
)

// F2-05: a validade do orçamento emitido sem `valid_until` é a
// `quote_validity_days` da política que o precificou, e fica congelada em
// `quotes.valid_until`. Até esta rodada a API somava uma constante de 7 dias e a
// coluna era lida por ninguém.
//
// 15 e 30 dias, longe do DEFAULT 7: com 7 o teste passaria contra a constante
// antiga.
func TestValidadeDoOrcamentoVemDaPoliticaECongelaNaEmissao(t *testing.T) {
	a := subir(t, auth.EscopoAll)
	produto := a.produto(t, "apto-2s")

	// A vigente do seed passa a dizer 15 dias. UPDATE (e não versão nova) só
	// porque é teste: publicar versão deixaria o seed com uma política a mais
	// para todos os pacotes seguintes. O Cleanup devolve o número original.
	var versao, original int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT version, quote_validity_days FROM commercial_policies
		 WHERE property_id = $1 AND valid_from <= current_date
		 ORDER BY version DESC LIMIT 1`, a.propriedade).Scan(&versao, &original); err != nil {
		t.Fatalf("lendo a política vigente: %v", err)
	}
	mudar := func(dias int) {
		t.Helper()
		if _, err := a.pool.Exec(a.ctx, `
			UPDATE commercial_policies SET quote_validity_days = $3
			 WHERE property_id = $1 AND version = $2`, a.propriedade, versao, dias); err != nil {
			t.Fatalf("ajustando a validade da política: %v", err)
		}
	}
	mudar(15)
	t.Cleanup(func() { mudar(original) })

	salvo := a.emitir(t, pedido(t, produto, a.dia(t, 320), a.dia(t, 323), 2,
		func(p *disponibilidade.Pedido) { p.Persistir = true }))
	if salvo.PolicyVersion != versao {
		t.Fatalf("o orçamento foi precificado pela versão %d, esperado %d", salvo.PolicyVersion, versao)
	}

	var dias float64
	if err := a.pool.QueryRow(a.ctx, `
		SELECT extract(epoch FROM valid_until - created_at) / 86400 FROM quotes WHERE id = $1`,
		salvo.ID).Scan(&dias); err != nil {
		t.Fatalf("lendo a validade gravada: %v", err)
	}
	if dias != 15 {
		t.Fatalf("valid_until - created_at = %v dias, esperado 15 (quote_validity_days da v%d)", dias, versao)
	}

	// Mudar a política DEPOIS da emissão não estica nem encurta o orçamento.
	mudar(30)
	relido, err := a.svc.BuscarOrcamento(a.ctx, salvo.ID)
	if err != nil {
		t.Fatalf("relendo o orçamento: %v", err)
	}
	if !relido.ValidoAte.Equal(salvo.ValidoAte) {
		t.Fatalf("a validade emitida mudou com a política: %s → %s", salvo.ValidoAte, relido.ValidoAte)
	}

	// E o próximo orçamento já sai com a validade nova.
	outro := a.emitir(t, pedido(t, produto, a.dia(t, 330), a.dia(t, 333), 2,
		func(p *disponibilidade.Pedido) { p.Persistir = true }))
	if err := a.pool.QueryRow(a.ctx, `
		SELECT extract(epoch FROM valid_until - created_at) / 86400 FROM quotes WHERE id = $1`,
		outro.ID).Scan(&dias); err != nil {
		t.Fatalf("lendo a validade do segundo orçamento: %v", err)
	}
	if dias != 30 {
		t.Fatalf("segundo orçamento com %v dias, esperado 30", dias)
	}
}
