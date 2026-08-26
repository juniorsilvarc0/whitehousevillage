package router

import (
	"net/http"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// Montar a tabela com Deps zerado tem de funcionar: method value sobre ponteiro
// nulo é legal em Go enquanto não for chamado, e é isso que permite ao teste de
// contrato inspecionar as rotas sem subir banco.
func tabela(t *testing.T) []Rota {
	t.Helper()
	rotas := Rotas(Deps{})
	if len(rotas) == 0 {
		t.Fatal("tabela de rotas vazia")
	}
	return rotas
}

func TestTabelaDeRotasEhValida(t *testing.T) {
	if err := ValidarTabela(tabela(t)); err != nil {
		t.Fatalf("ValidarTabela: %v", err)
	}
}

// A promessa do docs/spec.md §1: nenhum endpoint fica sem checagem de permissão.
// Quem abre exceção escreve o motivo, e o motivo aparece na revisão.
func TestNenhumaRotaFicaSemClassificacao(t *testing.T) {
	for _, r := range tabela(t) {
		chave := r.Metodo + " " + r.Path

		switch r.Acesso {
		case AcessoPublico:
			// Lista fechada: crescer aqui é decisão consciente, não descuido.
			publicasPermitidas := map[string]bool{
				"GET /healthz":               true,
				"GET /readyz":                true,
				"POST /auth/login":           true,
				"POST /auth/refresh":         true,
				"POST /auth/password/forgot": true,
				"POST /auth/password/reset":  true,
			}
			if !publicasPermitidas[chave] {
				t.Errorf("%s ficou pública sem estar na lista fechada", chave)
			}
		case AcessoAutenticado:
			if r.Motivo == "" {
				t.Errorf("%s não checa permissão e não explica por quê", chave)
			}
		case AcessoPermissao:
			if r.Recurso == "" || !auth.AcaoValida(r.Acao) {
				t.Errorf("%s tem recurso/ação inválidos (%q, %q)", chave, r.Recurso, r.Acao)
			}
		default:
			t.Errorf("%s sem classificação de acesso", chave)
		}

		if r.Handler == nil {
			t.Errorf("%s sem handler", chave)
		}
	}
}

// docs/api.md §2: todo recurso expõe os seis verbos.
func TestRecursosExpoemOsSeisVerbos(t *testing.T) {
	esperado := map[string][]string{
		"/users": {http.MethodGet, http.MethodPost},
		"/users/{id}": {
			http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete,
		},
	}

	presentes := map[string]map[string]bool{}
	for _, r := range tabela(t) {
		if presentes[r.Path] == nil {
			presentes[r.Path] = map[string]bool{}
		}
		presentes[r.Path][r.Metodo] = true
	}

	for path, verbos := range esperado {
		for _, verbo := range verbos {
			if !presentes[path][verbo] {
				t.Errorf("%s %s não está registrado", verbo, path)
			}
		}
	}
}

func TestValidarTabelaPegaRotaSemPermissao(t *testing.T) {
	casos := map[string][]Rota{
		"protegida sem recurso": {
			{Metodo: http.MethodGet, Path: "/finance", Acesso: AcessoPermissao},
		},
		"autenticada sem motivo": {
			{Metodo: http.MethodGet, Path: "/finance", Acesso: AcessoAutenticado},
		},
		"ação inexistente": {
			{Metodo: http.MethodGet, Path: "/finance", Acesso: AcessoPermissao, Recurso: "finance", Acao: "aprovar"},
		},
		"pública com recurso": {
			{Metodo: http.MethodGet, Path: "/finance", Acesso: AcessoPublico, Recurso: "finance", Acao: auth.AcaoVer},
		},
		"sem classificação": {
			{Metodo: http.MethodGet, Path: "/finance"},
		},
		"duplicada": {
			{Metodo: http.MethodGet, Path: "/finance", Acesso: AcessoPermissao, Recurso: "finance", Acao: auth.AcaoVer},
			{Metodo: http.MethodGet, Path: "/finance", Acesso: AcessoPermissao, Recurso: "finance", Acao: auth.AcaoVer},
		},
	}

	for nome, rotas := range casos {
		t.Run(nome, func(t *testing.T) {
			if err := ValidarTabela(rotas); err == nil {
				t.Fatal("ValidarTabela deveria ter recusado a tabela")
			}
		})
	}
}
