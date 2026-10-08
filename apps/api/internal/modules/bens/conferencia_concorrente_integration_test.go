//go:build integration

// Disputas da conferência. Todo teste deste arquivo tem no nome uma das
// palavras de TESTES_CONCORRENCIA do Makefile — é assim que a etapa
// `it-concorrencia` o repete (guarda_de_concorrencia_test.go confere).
package bens_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Dois (aqui, oito) funcionários abrindo a contagem do AP-01 no mesmo plantão:
// UMA abertura vence e as outras recebem 409 COUNT_ALREADY_OPEN apontando para
// ela. Quem decide é o índice único parcial, não um SELECT antes do INSERT.
func TestDisputaDeAberturaDaConferencia(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	cozinha := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	prato := a.bem(t, g, "Prato", nil)
	a.colocar(t, g, cozinha.ID, prato.ID, 12)

	const n = 8
	respostas := make([]resposta, n)
	var (
		largada sync.WaitGroup
		fim     sync.WaitGroup
	)
	largada.Add(1)
	for i := range n {
		fim.Add(1)
		go func() {
			defer fim.Done()
			largada.Wait()
			respostas[i] = a.chamar(t, http.MethodPost, "/inventory/counts", g, map[string]any{"unit_id": unidade})
		}()
	}
	largada.Done()
	fim.Wait()

	var vencedora uuid.UUID
	criadas, conflitos := 0, 0
	for _, r := range respostas {
		switch {
		case r.Status == http.StatusCreated:
			criadas++
			vencedora = dado[conferenciaResp](t, r).ID
		case r.Status == http.StatusConflict && r.codigo() == "COUNT_ALREADY_OPEN":
			conflitos++
		default:
			t.Errorf("resposta fora do esperado: %d %s", r.Status, r.Corpo)
		}
	}
	if criadas != 1 || conflitos != n-1 {
		t.Fatalf("esperado 1 × 201 e %d × 409; veio %d × 201 e %d × 409", n-1, criadas, conflitos)
	}
	for _, r := range respostas {
		if r.Status != http.StatusConflict {
			continue
		}
		d := r.detalhes(t)
		if d["count_id"] != vencedora.String() || d["opened_at"] == nil {
			t.Fatalf("o 409 deveria levar à conferência que venceu (%s): %v", vencedora, d)
		}
	}

	var abertas, linhas int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*), (SELECT count(*) FROM inventory_count_lines l JOIN inventory_counts c ON c.id = l.count_id
		                   WHERE c.unit_id = $1)
		  FROM inventory_counts WHERE unit_id = $1`, unidade).Scan(&abertas, &linhas); err != nil {
		t.Fatal(err)
	}
	if abertas != 1 || linhas != 1 {
		t.Fatalf("no banco: %d conferências e %d linhas; esperado 1 e 1", abertas, linhas)
	}
}

// Duas abas fechando a mesma conferência ao mesmo tempo: uma apura, a outra
// recebe COUNT_CLOSED — e as avarias de falta nascem UMA vez, não em dobro.
func TestDisputaDeFechamentoDuplo(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	cozinha := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	prato := a.bem(t, g, "Prato", nil)
	a.colocar(t, g, cozinha.ID, prato.ID, 12)
	conf := a.abrir(t, g, unidade)
	exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, prato.ID).ID, 10), http.StatusOK, "contando")

	const n = 4
	respostas := make([]resposta, n)
	var largada, fim sync.WaitGroup
	largada.Add(1)
	for i := range n {
		fim.Add(1)
		go func() {
			defer fim.Done()
			largada.Wait()
			respostas[i] = a.chamar(t, http.MethodPost, fmt.Sprintf("/inventory/counts/%s/close", conf.ID), g, nil)
		}()
	}
	largada.Done()
	fim.Wait()

	ok, fechada := 0, 0
	for _, r := range respostas {
		switch {
		case r.Status == http.StatusOK:
			ok++
		case r.Status == http.StatusConflict && r.codigo() == "COUNT_CLOSED":
			fechada++
		default:
			t.Errorf("resposta fora do esperado: %d %s", r.Status, r.Corpo)
		}
	}
	if ok != 1 || fechada != n-1 {
		t.Fatalf("esperado 1 × 200 e %d × COUNT_CLOSED; veio %d e %d", n-1, ok, fechada)
	}
	var avarias int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM inventory_issues WHERE count_id = $1`, conf.ID).Scan(&avarias); err != nil {
		t.Fatal(err)
	}
	if avarias != 1 {
		t.Fatalf("a falta deveria abrir UMA avaria; abriu %d", avarias)
	}
}

// Contagem chegando no meio do fechamento: ou entra antes (e o fechamento a
// enxerga), ou recebe COUNT_CLOSED. Nunca uma linha alterada DEPOIS de a
// conferência estar fechada.
func TestCorridaEntreContagemEFechamento(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	quarto := a.comodo(t, g, unidade, "Quarto", "quarto", 1)
	itens := make([]uuid.UUID, 6)
	for i := range itens {
		itens[i] = a.bem(t, g, fmt.Sprintf("Item %d", i), nil).ID
		a.colocar(t, g, quarto.ID, itens[i], 5)
	}
	conf := a.abrir(t, g, unidade)
	for _, it := range itens {
		exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, it).ID, 5), http.StatusOK, "contando")
	}

	var largada, fim sync.WaitGroup
	largada.Add(1)
	recontagens := make([]resposta, len(itens))
	var fechamento resposta
	fim.Add(1)
	go func() {
		defer fim.Done()
		largada.Wait()
		fechamento = a.chamar(t, http.MethodPost, fmt.Sprintf("/inventory/counts/%s/close", conf.ID), g,
			map[string]any{"raise_issues": false})
	}()
	for i, it := range itens {
		fim.Add(1)
		go func() {
			defer fim.Done()
			largada.Wait()
			recontagens[i] = a.contar(t, g, conf.ID, conf.linhaDo(t, it).ID, 4)
		}()
	}
	largada.Done()
	fim.Wait()

	exigir(t, fechamento, http.StatusOK, "fechando")
	apuradas := map[uuid.UUID]bool{}
	for _, d := range dado[struct {
		Divergencias []struct {
			BemID uuid.UUID `json:"item_id"`
		} `json:"divergences"`
	}](t, fechamento).Divergencias {
		apuradas[d.BemID] = true
	}

	// Cada recontagem ou entrou ANTES do fechamento (e ele a apurou), ou
	// recebeu COUNT_CLOSED (e a linha ficou como estava). O banco tem de dizer
	// exatamente o que o fechamento disse.
	for i, r := range recontagens {
		switch {
		case r.Status == http.StatusOK:
			if !apuradas[itens[i]] {
				t.Errorf("item %d recontado com 200 mas fora da apuração do fechamento", i)
			}
		case r.Status == http.StatusConflict && r.codigo() == "COUNT_CLOSED":
			if apuradas[itens[i]] {
				t.Errorf("item %d recusado com COUNT_CLOSED mas apurado como divergente", i)
			}
		default:
			t.Errorf("recontagem fora do esperado: %d %s", r.Status, r.Corpo)
		}
	}
	linhas, err := a.pool.Query(a.ctx, `
		SELECT item_id FROM inventory_count_lines WHERE count_id = $1 AND counted_qty <> expected_qty`, conf.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer linhas.Close()
	noBanco := 0
	for linhas.Next() {
		var item uuid.UUID
		if err := linhas.Scan(&item); err != nil {
			t.Fatal(err)
		}
		noBanco++
		if !apuradas[item] {
			t.Errorf("linha do item %s divergente no banco e fora da apuração: contagem entrou depois do fechamento", item)
		}
	}
	if noBanco != len(apuradas) {
		t.Fatalf("o banco tem %d linhas divergentes e o fechamento apurou %d", noBanco, len(apuradas))
	}
}

// Cópia para a unidade e abertura da conferência dela ao mesmo tempo. A trava
// por unidade faz uma esperar a outra; o desfecho aceitável é um destes dois:
//   - a cópia entra antes: a conferência congela TAMBÉM o que foi copiado;
//   - a abertura entra antes: a cópia recebe 409 COUNT_ALREADY_OPEN.
//
// O inaceitável — e o que o teste procura — é a conferência aberta sem linha
// para uma colocação que a cópia criou.
func TestCorridaEntreCopiaEAberturaDaConferencia(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	origem, _ := a.unidade(t)
	cozinhaOrigem := a.comodo(t, g, origem, "Cozinha", "cozinha", 1)
	prato := a.bem(t, g, "Prato", nil)
	taca := a.bem(t, g, "Taça", nil)
	a.colocar(t, g, cozinhaOrigem.ID, prato.ID, 12)
	a.colocar(t, g, cozinhaOrigem.ID, taca.ID, 6)

	for rodada := range 5 {
		destino, _ := a.unidade(t)
		sala := a.comodo(t, g, destino, "Sala", "sala", 1)
		a.colocar(t, g, sala.ID, prato.ID, 1) // o destino já tem o que contar

		var largada, fim sync.WaitGroup
		var copia, abertura resposta
		largada.Add(1)
		fim.Add(2)
		go func() {
			defer fim.Done()
			largada.Wait()
			copia = a.chamar(t, http.MethodPost, fmt.Sprintf("/units/%s/inventory/copy", destino), g,
				map[string]any{"source_unit_id": origem})
		}()
		go func() {
			defer fim.Done()
			largada.Wait()
			abertura = a.chamar(t, http.MethodPost, "/inventory/counts", g, map[string]any{"unit_id": destino})
		}()
		largada.Done()
		fim.Wait()

		exigir(t, abertura, http.StatusCreated, fmt.Sprintf("rodada %d: abrindo", rodada))
		switch {
		case copia.Status == http.StatusOK:
			var semLinha int
			if err := a.pool.QueryRow(a.ctx, `
				SELECT count(*) FROM room_inventory ri JOIN unit_rooms r ON r.id = ri.room_id
				 WHERE r.unit_id = $1 AND NOT EXISTS (
				       SELECT 1 FROM inventory_count_lines l JOIN inventory_counts c ON c.id = l.count_id
				        WHERE c.unit_id = $1 AND c.status = 'aberta'
				          AND l.room_id = ri.room_id AND l.item_id = ri.item_id)`, destino).Scan(&semLinha); err != nil {
				t.Fatal(err)
			}
			if semLinha != 0 {
				t.Fatalf("rodada %d: a cópia criou %d colocação(ões) fora da conferência aberta", rodada, semLinha)
			}
		case copia.Status == http.StatusConflict && copia.codigo() == "COUNT_ALREADY_OPEN":
			// a abertura venceu, e a cópia foi recusada inteira
		default:
			t.Fatalf("rodada %d: cópia fora do esperado: %d %s", rodada, copia.Status, copia.Corpo)
		}
	}
}
