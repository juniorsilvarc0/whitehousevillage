package main

import (
	"slices"
	"testing"
)

// Os testes de integração (~33 arquivos) dependem do catálogo de teste. Estas
// contagens são o que o seed semeava até 03/10/2026; se mudarem, a suíte muda
// junto — e isso tem de ser uma decisão, não um efeito colateral.
func TestCatalogoDeTesteCongelado(t *testing.T) {
	c := catalogoDeTeste
	conferir := func(oque string, got, want int) {
		t.Helper()
		if got != want {
			t.Errorf("catálogo de teste: %d %s, esperado %d", got, oque, want)
		}
	}
	conferir("unidades", len(c.unidades), 8)
	conferir("produtos", len(c.produtos), 4)
	conferir("membros", len(c.composicao), 16)
	conferir("linhas de tarifa", len(c.tarifas), 4)
	conferir("mínimos gerais", len(c.minimoGeral), 6)
	conferir("mínimos por produto", len(c.minimoPorProduto), 0)
	conferir("pacotes", len(c.pacotes), 0)
	conferir("feriados", len(c.feriados), 7)
	conferir("períodos", len(c.periodos), 4)
	for _, p := range c.produtos {
		if p.publico != nil {
			t.Errorf("catálogo de teste: %s com public_name %q — era NULL", p.codigo, *p.publico)
		}
	}
	for _, tar := range c.tarifas {
		for i, v := range tar.valores {
			if v == sobConsulta {
				t.Errorf("catálogo de teste: %s/%s sob consulta — eram 24 tarifas", tar.produto, ordemDosTipos[i])
			}
		}
	}
}

func TestCatalogoRealComoODonoDefiniu(t *testing.T) {
	c := catalogoDaCasa
	if len(c.unidades) != 12 || len(c.produtos) != 14 {
		t.Fatalf("catálogo real: %d unidades e %d produtos, esperado 12 e 14", len(c.unidades), len(c.produtos))
	}
	tarifadas := 0
	for _, tar := range c.tarifas {
		if tar.produto == "completa" {
			t.Error("a Completa real é sob consulta e não pode ter linha de tarifa")
		}
		for _, v := range tar.valores {
			if v != sobConsulta {
				tarifadas++
			}
		}
	}
	// 6 duplex × 6 + 4 suítes × 6 + pool 6 + grand-villa 3 + classic 6.
	if tarifadas != 75 {
		t.Errorf("catálogo real: %d tarifas, esperado 75", tarifadas)
	}
	if len(c.minimoPorProduto) != 5*6+3+6 {
		t.Errorf("catálogo real: %d mínimos por produto, esperado 39", len(c.minimoPorProduto))
	}
	if len(c.pacotes) != 4 || len(c.feriados) != 10 || len(c.periodos) != 7 {
		t.Errorf("catálogo real: %d pacotes, %d feriados, %d períodos — esperado 4, 10, 7",
			len(c.pacotes), len(c.feriados), len(c.periodos))
	}
}

// Coerência interna dos dois catálogos: tudo que é referenciado por código
// existe, a Completa consome todas as unidades, e os tipos de data são do
// vocabulário de date_type_rules. Um JOIN do seed descartaria em silêncio um
// código digitado errado e a contagem diria "inalterada".
func TestCatalogosCoerentes(t *testing.T) {
	for _, cat := range []*catalogo{&catalogoDeTeste, &catalogoDaCasa} {
		produtos := cat.codigosDeProduto()
		unidades := cat.codigosDeUnidade()
		tem := func(lista []string, v string) bool { return slices.Contains(lista, v) }

		vistos := map[composicao]bool{}
		membros := map[string]int{}
		for _, m := range cat.composicao {
			if !tem(produtos, m.produto) || !tem(unidades, m.unidade) {
				t.Errorf("%s: composição %s→%s fora do catálogo", cat.nome, m.produto, m.unidade)
			}
			if vistos[m] {
				t.Errorf("%s: membro %s→%s repetido", cat.nome, m.produto, m.unidade)
			}
			vistos[m] = true
			membros[m.produto]++
		}
		for _, p := range cat.produtos {
			if membros[p.codigo] == 0 {
				t.Errorf("%s: produto %s sem unidade — não teria como ser vendido", cat.nome, p.codigo)
			}
			if p.codigo == "completa" && membros[p.codigo] != len(cat.unidades) {
				t.Errorf("%s: a Completa consome %d de %d unidades", cat.nome, membros[p.codigo], len(cat.unidades))
			}
			if p.publico != nil && *p.publico == "" {
				t.Errorf("%s: %s com public_name vazio — o CHECK do banco recusa", cat.nome, p.codigo)
			}
		}
		for _, tar := range cat.tarifas {
			if !tem(produtos, tar.produto) {
				t.Errorf("%s: tarifa de produto inexistente %s", cat.nome, tar.produto)
			}
		}
		if len(cat.minimoGeral) != len(ordemDosTipos) {
			t.Errorf("%s: mínimo geral cobre %d de %d tipos", cat.nome, len(cat.minimoGeral), len(ordemDosTipos))
		}
		for _, m := range cat.minimoPorProduto {
			if !tem(produtos, m.produto) || !tem(ordemDosTipos, m.tipo) || m.noites <= 0 {
				t.Errorf("%s: mínimo por produto inválido %+v", cat.nome, m)
			}
		}
		chaves := map[string]bool{}
		for _, p := range cat.pacotes {
			if !tem(produtos, p.produto) || p.noites < 2 || p.total <= 0 || len(p.tipos) == 0 {
				t.Errorf("%s: pacote inválido %+v", cat.nome, p)
			}
			for _, tipo := range p.tipos {
				if !tem(ordemDosTipos, tipo) {
					t.Errorf("%s: pacote %s com tipo %q fora do vocabulário", cat.nome, p.chave(), tipo)
				}
			}
			if chaves[p.chave()] {
				t.Errorf("%s: pacote %s repetido", cat.nome, p.chave())
			}
			chaves[p.chave()] = true
		}
		for _, p := range cat.periodos {
			if p.fim.Before(p.inicio) {
				t.Errorf("%s: período %s termina antes de começar", cat.nome, p.nome)
			}
		}
	}
}

func TestEscolhaDoCatalogo(t *testing.T) {
	casos := []struct {
		valor    string
		esperado string
		erro     bool
	}{
		{"", catalogoReal, false},
		{"real", catalogoReal, false},
		{"teste", catalogoTeste, false},
		{"demo", "", true},
	}
	for _, c := range casos {
		t.Setenv("SEED_CATALOGO", c.valor)
		cat, outro, err := escolherCatalogo()
		if c.erro {
			if err == nil {
				t.Errorf("SEED_CATALOGO=%q aceito — deveria ser recusado", c.valor)
			}
			continue
		}
		if err != nil {
			t.Fatalf("SEED_CATALOGO=%q: %v", c.valor, err)
		}
		if cat.nome != c.esperado || outro.nome == cat.nome {
			t.Errorf("SEED_CATALOGO=%q escolheu %s (outro %s)", c.valor, cat.nome, outro.nome)
		}
	}
}

func TestForaDoCatalogo(t *testing.T) {
	got := foraDoCatalogo([]string{"AP-01", "COB-01", "SP-01"}, []string{"AP-01", "SP-01", "GV-01"})
	if !slices.Equal(got, []string{"COB-01"}) {
		t.Fatalf("foraDoCatalogo = %v, esperado [COB-01]", got)
	}
}
