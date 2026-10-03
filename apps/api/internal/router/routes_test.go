package router

import (
	"net/http"
	"strings"
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
				// Vitrine do site de vendas (rotas_vitrine.go).
				"GET /public/products":     true,
				"GET /public/policy":       true,
				"GET /public/availability": true,
				"POST /public/quotes":      true,
				// Pré-reserva do próprio cliente (B1): a única pública que grava.
				"POST /public/holds": true,
				// Conteúdo e mídia do site editados pela gestão (rotas_site.go).
				"GET /public/site":       true,
				"GET /public/media/{id}": true,
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
		"pública sem motivo": {
			{Metodo: http.MethodGet, Path: "/public/finance", Acesso: AcessoPublico},
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

// Toda rota da vitrine passa pelo limitador por IP — é a terceira invariante
// do §5 de docs/unificacao-site-crm.md. O teste olha a TABELA real: uma rota
// pública nova sob /public/ entra no freio sem ninguém lembrar, e uma rota
// pública FORA de /public/ que não seja sonda nem /auth acende aqui.
//
// A única exceção ao limitador da vitrine é a entrega de mídia, que passa por
// um limitador PRÓPRIO (EhMidiaPublica) — e a exceção é uma rota só, por path
// exato: se mais alguma /public/ escapar do freio da vitrine, acende aqui.
func TestTodaRotaPublicaDeNegocioFicaAtrasDoLimitador(t *testing.T) {
	vitrine, midia := 0, 0
	for _, r := range tabela(t) {
		if r.Acesso != AcessoPublico {
			continue
		}
		if strings.HasPrefix(r.Path, "/public/") {
			vitrine++
			daVitrine, daMidia := EhDaVitrine(r), EhMidiaPublica(r)
			if daMidia {
				midia++
			}
			if daVitrine == daMidia {
				t.Errorf("%s %s precisa passar por exatamente um limitador (vitrine=%v, mídia=%v)",
					r.Metodo, r.Path, daVitrine, daMidia)
			}
			continue
		}
		if !r.NaRaiz && !strings.HasPrefix(r.Path, "/auth/") {
			t.Errorf("%s %s é pública fora de /public/: sem limitador e sem dono", r.Metodo, r.Path)
		}
	}
	if vitrine == 0 {
		t.Fatal("nenhuma rota /public/ na tabela: a vitrine sumiu do router")
	}
	if midia != 1 {
		t.Fatalf("esperada exatamente uma rota de mídia pública (%s), achei %d", RotaDaMidiaPublica, midia)
	}
}

// As rotas de arquivo do site ficam fora do teto de 50 s; as de JSON, dentro.
func TestRotasDeArquivoDoSiteFicamForaDoTeto(t *testing.T) {
	for _, r := range tabela(t) {
		switch r.Path {
		case RotaDeEnvioDeMidia, RotaDaMidiaPublica:
			if !EhDeLongaDuracao(r) {
				t.Errorf("%s %s está sob o teto de 50 s: upload/entrega de vídeo seria cortado", r.Metodo, r.Path)
			}
		case "/site/content", "/site/content/{key}", "/public/site":
			if EhDeLongaDuracao(r) {
				t.Errorf("%s %s escapou do teto por requisição", r.Metodo, r.Path)
			}
		}
	}
}
