//go:build integration

// A pré-reserva do próprio cliente (POST /public/holds, passo B1 de
// docs/unificacao-site-crm.md), pela porta da frente e sem token.
//
// O critério de pronto do B1 é o da concorrência: dois navegadores pedindo a
// mesma data ao mesmo tempo, um recebe 201 e o outro 409 DATE_CONFLICT — quem
// decide é a constraint EXCLUDE, não um SELECT antes do INSERT. Os demais
// testes medem as travas de porta anônima: consentimento, idempotência, teto
// por telefone, limite por IP e resposta sem campo interno.
package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// preReservar faz o POST como um visitante: sem token, com o IP de origem no
// X-Forwarded-For (o servidor de teste é loopback, salto em que RealIP
// confia) e com a chave de idempotência que o site gera.
func (a *ambiente) preReservar(t *testing.T, ip, chave string, corpo map[string]any) resposta {
	t.Helper()
	bruto, err := json.Marshal(corpo)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodPost, a.servidor.URL+PrefixoDaAPI+"/public/holds", bytes.NewReader(bruto))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	if chave != "" {
		req.Header.Set("Idempotency-Key", chave)
	}
	resp, err := a.servidor.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	return resposta{Status: resp.StatusCode, Corpo: buf.Bytes()}
}

// telefoneDoSite é único por chamada: o teto de pré-reservas é por
// telefone, e o banco de integração sobrevive entre execuções locais.
func telefoneDoSite() string {
	return fmt.Sprintf("+55869%08d", rand.Intn(100000000))
}

func pedidoDePreReserva(produto uuid.UUID, entrada, saida, telefone string) map[string]any {
	return map[string]any{
		"unit_type_id": produto, "check_in": entrada, "check_out": saida, "guests_count": 2,
		"name": "Hóspede do Site", "phone": telefone, "email": "hospede@example.com", "consent": true,
	}
}

type preReservaQA struct {
	Codigo   string    `json:"code"`
	Status   string    `json:"status"`
	Produto  string    `json:"product_name"`
	Total    int64     `json:"total_cents"`
	Sinal    int64     `json:"deposit_cents"`
	Saldo    int64     `json:"balance_cents"`
	ExpiraEm time.Time `json:"hold_expires_at"`
}

func TestPreReservaPublicaSeguraADataPeloMesmoCaminhoDoPainel(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	produto := a.produtoPublico(t, "cobertura")
	entrada, saida := janelaAPartirDeHoje(211, noitesDaVitrine)
	telefone := telefoneDoSite()

	orcado := envelopeDe[orcamentoPublicoQA](t, a.chamar(t, http.MethodPost, "/public/quotes", "",
		map[string]any{"unit_type_id": produto.ID, "check_in": entrada, "check_out": saida, "guests_count": 2}),
		http.StatusOK, "orçamento antes da pré-reserva")

	chave := "site-" + uuid.NewString()
	r := a.preReservar(t, "203.0.113.10", chave, pedidoDePreReserva(produto.ID, entrada, saida, telefone))
	feita := envelopeDe[preReservaQA](t, r, http.StatusCreated, "POST /public/holds")

	if !strings.HasPrefix(feita.Codigo, "WH-") || feita.Status != "hold" {
		t.Fatalf("pré-reserva sem código ou fora de hold: %+v", feita)
	}
	if feita.Total != orcado.Total || feita.Sinal != orcado.Sinal || feita.Sinal+feita.Saldo != feita.Total {
		t.Fatalf("a pré-reserva cobrou diferente do orçamento público: orçado %+v, gravado %+v", orcado, feita)
	}
	if horas := time.Until(feita.ExpiraEm).Hours(); horas < 40 || horas > 49 {
		t.Fatalf("a pré-reserva deveria segurar ~48h; expira em %.1fh", horas)
	}

	// Recorte: nenhuma chave além das do contrato público.
	var bruto struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(r.Corpo, &bruto); err != nil {
		t.Fatal(err)
	}
	permitidas := map[string]bool{"code": true, "status": true, "product_name": true, "check_in": true, "check_out": true,
		"night_count": true, "guests_count": true, "total_cents": true, "deposit_cents": true, "balance_cents": true, "hold_expires_at": true}
	for k := range bruto.Data {
		if !permitidas[k] {
			t.Errorf("a resposta pública carrega %q: %s", k, r.Corpo)
		}
	}

	// No banco: origem site, dona a conta de serviço, contato com a base legal
	// da reserva e o aceite anotado — e SEM opt-in de marketing.
	var (
		origem, dono, base string
		aceiteAnotado      bool
	)
	if err := a.pool.QueryRow(a.ctx, `
		SELECT r.source, u.email, COALESCE(c.lgpd_basis, ''),
		       COALESCE(c.notes, '') LIKE '%aceite do uso dos dados%' AND NOT c.marketing_opt_in AND c.consent_at IS NULL
		  FROM reservations r
		  JOIN users u ON u.id = r.owner_id
		  JOIN contacts c ON c.id = r.contact_id
		 WHERE r.code = $1`, feita.Codigo).Scan(&origem, &dono, &base, &aceiteAnotado); err != nil {
		t.Fatalf("a reserva %s não está no banco: %v", feita.Codigo, err)
	}
	if origem != "site" || dono != "vitrine@site.whitehouse.invalid" || base != "contrato" || !aceiteAnotado {
		t.Fatalf("gravação errada: origem %q, dono %q, base %q, aceite anotado sem marketing %v", origem, dono, base, aceiteAnotado)
	}

	// A data saiu do calendário público.
	dias := envelopeDe[[]struct {
		Livre bool `json:"available"`
	}](t, a.chamar(t, http.MethodGet, "/public/availability?unit_type_id="+produto.ID.String()+"&from="+entrada+"&to="+saida, "", nil),
		http.StatusOK, "calendário depois da pré-reserva")
	for i, d := range dias {
		if d.Livre {
			t.Fatalf("a noite %d continua livre no site depois da pré-reserva", i)
		}
	}

	// Repetir a MESMA chave (rede caiu, clique duplo) devolve a mesma pré-reserva.
	denovo := envelopeDe[preReservaQA](t, a.preReservar(t, "203.0.113.10", chave, pedidoDePreReserva(produto.ID, entrada, saida, telefone)),
		http.StatusCreated, "repetição da chave")
	if denovo.Codigo != feita.Codigo {
		t.Fatalf("a repetição da chave criou outra pré-reserva: %s e %s", feita.Codigo, denovo.Codigo)
	}
}

// O critério de pronto do B1: dois visitantes, a mesma data, ao mesmo tempo.
// Dez rodadas, cada uma numa janela própria da cobertura (unidade única).
func TestPreReservaPublicaConcorrenteUmLevaOOutroRecebe409(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	produto := a.produtoPublico(t, "cobertura")

	for rodada := 0; rodada < 10; rodada++ {
		entrada, saida := janelaAPartirDeHoje(230+rodada*8, noitesDaVitrine)
		var (
			wg      sync.WaitGroup
			largada = make(chan struct{})
			status  [2]int
			codigos [2]string
		)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-largada
				r := a.preReservar(t, fmt.Sprintf("198.51.100.%d", rodada*2+i+1), "site-"+uuid.NewString(),
					pedidoDePreReserva(produto.ID, entrada, saida, telefoneDoSite()))
				status[i] = r.Status
				if r.Status != http.StatusCreated {
					codigos[i] = r.codigoDeErro(t)
					// O 409 do painel aponta a unidade física e a constraint;
					// o do público não diz nada além de "tomada".
					if strings.Contains(string(r.Corpo), "COB-01") || strings.Contains(string(r.Corpo), "stay_no_overlap") {
						t.Errorf("o 409 público vaza vocabulário interno: %s", r.Corpo)
					}
				}
			}(i)
		}
		close(largada)
		wg.Wait()

		criados, conflitos := 0, 0
		for i := range status {
			switch {
			case status[i] == http.StatusCreated:
				criados++
			case status[i] == http.StatusConflict && codigos[i] == "DATE_CONFLICT":
				conflitos++
			}
		}
		if criados != 1 || conflitos != 1 {
			t.Fatalf("rodada %d (%s→%s): esperado um 201 e um 409 DATE_CONFLICT, veio %v %v", rodada, entrada, saida, status, codigos)
		}
	}
}

func TestPreReservaPublicaExigeConsentimentoEChave(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	produto := a.produtoPublico(t, "apto-2s")
	entrada, saida := janelaAPartirDeHoje(320, noitesDaVitrine)

	sem := pedidoDePreReserva(produto.ID, entrada, saida, telefoneDoSite())
	sem["consent"] = false
	if r := a.preReservar(t, "203.0.113.20", "site-"+uuid.NewString(), sem); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(string(r.Corpo), `"consent"`) {
		t.Fatalf("sem consentimento deveria ser 422 apontando consent: %d %s", r.Status, r.Corpo)
	}
	if r := a.preReservar(t, "203.0.113.20", "", pedidoDePreReserva(produto.ID, entrada, saida, telefoneDoSite())); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(string(r.Corpo), "Idempotency-Key") {
		t.Fatalf("sem chave de idempotência deveria ser 422: %d %s", r.Status, r.Corpo)
	}
	// O público não negocia nem escolhe dono: campo a mais é recusado.
	com := pedidoDePreReserva(produto.ID, entrada, saida, telefoneDoSite())
	com["discount_pct"] = 10
	if r := a.preReservar(t, "203.0.113.20", "site-"+uuid.NewString(), com); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("discount_pct na pré-reserva pública deveria ser 422: %d %s", r.Status, r.Corpo)
	}
	// Nada disso gravou.
	var n int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM reservations WHERE unit_type_id = $1 AND check_in = $2::date AND source = 'site'`,
		produto.ID, entrada).Scan(&n); err != nil || n != 0 {
		t.Fatalf("pedido recusado gravou reserva (%d, %v)", n, err)
	}
}

// Negação de inventário: quem não paga não segura a casa inteira. Duas
// pré-reservas abertas por telefone; a terceira é recusada.
func TestPreReservaPublicaTetoPorTelefone(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	produto := a.produtoPublico(t, "apto-2s")
	telefone := telefoneDoSite()

	for i := 0; i < 3; i++ {
		entrada, saida := janelaAPartirDeHoje(340+i*6, noitesDaVitrine)
		r := a.preReservar(t, fmt.Sprintf("192.0.2.%d", 40+i), "site-"+uuid.NewString(),
			pedidoDePreReserva(produto.ID, entrada, saida, telefone))
		if i < 2 && r.Status != http.StatusCreated {
			t.Fatalf("a pré-reserva %d deveria passar: %d %s", i+1, r.Status, r.Corpo)
		}
		if i == 2 && (r.Status != http.StatusConflict || r.codigoDeErro(t) != "HOLD_LIMIT_REACHED") {
			t.Fatalf("a terceira pré-reserva aberta do mesmo telefone deveria ser 409 HOLD_LIMIT_REACHED: %d %s", r.Status, r.Corpo)
		}
	}
}

// Telefone já cadastrado é a mesma pessoa: a ficha é reaproveitada e NÃO é
// reescrita por um formulário anônimo.
func TestPreReservaPublicaNaoSobrescreveContatoExistente(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	produto := a.produtoPublico(t, "suite-piscina")
	telefone := telefoneDoSite()

	var existente uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, phone_e164)
		SELECT id, 'Cliente Antigo', $1 FROM properties WHERE active LIMIT 1
		RETURNING id`, telefone).Scan(&existente); err != nil {
		t.Fatalf("criando o contato antigo: %v", err)
	}

	entrada, saida := janelaAPartirDeHoje(360, noitesDaVitrine)
	pedido := pedidoDePreReserva(produto.ID, entrada, saida, telefone)
	pedido["name"] = "Outro Nome Qualquer"
	feita := envelopeDe[preReservaQA](t, a.preReservar(t, "203.0.113.30", "site-"+uuid.NewString(), pedido),
		http.StatusCreated, "pré-reserva com telefone já cadastrado")

	var (
		contato uuid.UUID
		nome    string
	)
	if err := a.pool.QueryRow(a.ctx, `
		SELECT c.id, c.name FROM reservations r JOIN contacts c ON c.id = r.contact_id WHERE r.code = $1`,
		feita.Codigo).Scan(&contato, &nome); err != nil {
		t.Fatal(err)
	}
	if contato != existente || nome != "Cliente Antigo" {
		t.Fatalf("esperava a ficha existente intacta; veio contato %s com nome %q", contato, nome)
	}
}

// Limite próprio da pré-reserva por IP, além do da vitrine: a sétima na mesma
// hora, do mesmo IP, é recusada mesmo com telefones diferentes.
func TestPreReservaPublicaLimitaPorIP(t *testing.T) {
	a := subirAPI(t)
	exigirSeed(t, a)
	produto := a.produtoPublico(t, "apto-2s")
	entrada, saida := janelaAPartirDeHoje(380, noitesDaVitrine)

	// Pedidos sem consentimento: o limitador conta na porta, antes do
	// handler, então nada precisa ser gravado para medir o limite.
	pedido := pedidoDePreReserva(produto.ID, entrada, saida, telefoneDoSite())
	pedido["consent"] = false
	for i := 1; i <= 6; i++ {
		if r := a.preReservar(t, "203.0.113.99", "site-"+uuid.NewString(), pedido); r.Status == http.StatusTooManyRequests {
			t.Fatalf("o pedido %d de 6 foi barrado pelo limite", i)
		}
	}
	if r := a.preReservar(t, "203.0.113.99", "site-"+uuid.NewString(), pedido); r.Status != http.StatusTooManyRequests {
		t.Fatalf("o 7º pedido do mesmo IP na hora deveria ser 429: %d %s", r.Status, r.Corpo)
	}
	// Outro IP continua passando pela porta.
	if r := a.preReservar(t, "203.0.113.100", "site-"+uuid.NewString(), pedido); r.Status == http.StatusTooManyRequests {
		t.Fatal("o limite de um IP vazou para outro")
	}
}
