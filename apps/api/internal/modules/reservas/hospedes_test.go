package reservas

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/guests"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// A rooming list (F2-22) do lado do módulo: a ponte com `internal/domain/guests`
// e a tradução das recusas. As regras em si são testadas no domínio; aqui se
// confere que cada recusa chega ao contrato com o code, o status e os details
// certos — e que o defeito nosso sai 500, nunca 422.

func reservaDeTeste(status string, lead uuid.UUID, vendidos, capacidade int, lista ...uuid.UUID) guests.Reservation[uuid.UUID] {
	membros := make([]MembroDaLista, len(lista))
	for i, id := range lista {
		membros[i] = MembroDaLista{ContactID: id}
	}
	return reservaDaLista(Estado{Status: status, ContactID: lead, Hospedes: vendidos, Capacidade: capacidade}, membros)
}

func exigirTraducao(t *testing.T, err error, code string, status int) *apperr.Error {
	t.Helper()
	e := apperr.From(traduzirRegraDaLista(err))
	if e.Code != code || e.Status() != status {
		t.Fatalf("tradução = %s/%d, esperado %s/%d (%v)", e.Code, e.Status(), code, status, err)
	}
	return e
}

func detalhesDe(t *testing.T, e *apperr.Error) map[string]any {
	t.Helper()
	d, ok := e.Details.(map[string]any)
	if !ok {
		t.Fatalf("details = %#v, esperado mapa", e.Details)
	}
	return d
}

// Cada recusa do domínio sai com o code e o status do CATÁLOGO, e com a frase e
// os details do domínio. Os casos são produzidos pelas funções do domínio, não
// montados à mão: é a ponte que está em teste.
func TestRecusasDaListaChegamAoContratoPeloCatalogo(t *testing.T) {
	titular, ana, bruno := uuid.New(), uuid.New(), uuid.New()
	viva := reservaDeTeste(EstadoConfirmada, titular, 2, 6, titular, ana)

	e := exigirTraducao(t, guests.CheckAdd(viva, guests.Contact[uuid.UUID]{ID: bruno, Found: true}),
		apperr.CodeGuestListFull, http.StatusUnprocessableEntity)
	d := detalhesDe(t, e)
	if d["guests_count"] != 2 || d["listed"] != 2 || d["capacity"] != 6 {
		t.Errorf("GUEST_LIST_FULL sem os três números da tela: %v", d)
	}
	if !strings.Contains(e.Message, "aumente") {
		t.Errorf("com folga na capacidade a frase manda aumentar o número: %q", e.Message)
	}

	e = exigirTraducao(t, guests.CheckAdd(viva, guests.Contact[uuid.UUID]{ID: bruno}),
		apperr.CodeValidationError, http.StatusUnprocessableEntity)
	if _, ok := detalhesDe(t, e)["contact_id"].(string); !ok {
		t.Errorf("ficha inexistente ou de outra casa é erro do campo contact_id: %v", e.Details)
	}

	e = exigirTraducao(t, guests.CheckAdd(viva, guests.Contact[uuid.UUID]{ID: bruno, Found: true, Anonymized: true}),
		apperr.CodeContactAnonymized, http.StatusConflict)
	if detalhesDe(t, e)["contact_id"] != bruno {
		t.Errorf("CONTACT_ANONYMIZED sem o contact_id: %v", e.Details)
	}

	e = exigirTraducao(t, guests.CheckAdd(viva, guests.Contact[uuid.UUID]{ID: ana, Found: true}),
		apperr.CodeCodeInUse, http.StatusConflict)
	if d := detalhesDe(t, e); d["contact_id"] != ana || d["is_lead_guest"] != false {
		t.Errorf("CODE_IN_USE da pessoa repetida: %v", d)
	}

	encerrada := reservaDeTeste(EstadoCheckOut, titular, 2, 6, titular)
	e = exigirTraducao(t, guests.CheckAdd(encerrada, guests.Contact[uuid.UUID]{ID: bruno, Found: true}),
		apperr.CodeInvalidStateTransition, http.StatusConflict)
	if d := detalhesDe(t, e); d["status"] != EstadoCheckOut || !reflect.DeepEqual(d["allowed"], []string{EstadoHold, EstadoConfirmada, EstadoCheckIn}) {
		t.Errorf("INVALID_STATE_TRANSITION sem status/allowed: %v", d)
	}

	e = exigirTraducao(t, guests.CheckRemove(viva, titular), apperr.CodeResourceInUse, http.StatusConflict)
	if detalhesDe(t, e)["is_lead_guest"] != true {
		t.Errorf("RESOURCE_IN_USE do titular sem is_lead_guest: %v", e.Details)
	}
	if e := exigirTraducao(t, guests.CheckRemove(viva, bruno), apperr.CodeNotFound, http.StatusNotFound); !strings.Contains(e.Message, "não está na lista") {
		t.Errorf("o 404 de quem não está na lista perdeu a frase do domínio: %q", e.Message)
	}

	_, err := guests.PlanEdit(reservaDeTeste(EstadoConfirmada, titular, 2, 6, titular, ana),
		guests.Edit[uuid.UUID]{GuestsCount: ptr(1)})
	e = exigirTraducao(t, err, apperr.CodeValidationError, http.StatusUnprocessableEntity)
	if _, ok := detalhesDe(t, e)["guests_count"]; !ok {
		t.Errorf("baixar o número abaixo da lista é erro do campo guests_count: %v", e.Details)
	}

	_, err = guests.Carry(titular, []guests.Contact[uuid.UUID]{{ID: titular}, {ID: ana}, {ID: bruno}}, 2)
	e = exigirTraducao(t, err, apperr.CodeValidationError, http.StatusUnprocessableEntity)
	if _, ok := detalhesDe(t, e)["guests_count"]; !ok {
		t.Errorf("a remarcação que não cabe é erro do campo guests_count: %v", e.Details)
	}
}

// Entrada que nenhum chamador correto produz é defeito NOSSO: 500, com a causa
// no log — nunca um 422 que mandaria o operador procurar erro no formulário.
func TestEntradaInvalidaParaODominioEhErroInterno(t *testing.T) {
	titular := uuid.New()
	err := guests.CheckAdd(reservaDeTeste(EstadoConfirmada, titular, 0, 6, titular),
		guests.Contact[uuid.UUID]{ID: uuid.New(), Found: true})
	if !errors.Is(err, guests.ErrInvalidInput) {
		t.Fatalf("o domínio deveria recusar zero hóspedes como entrada inválida: %v", err)
	}
	e := exigirTraducao(t, err, apperr.CodeInternal, http.StatusInternalServerError)
	if e.Message != apperr.Internal.Message {
		t.Errorf("a frase do defeito vazou na resposta: %q", e.Message)
	}
	if traduzirRegraDaLista(nil) != nil {
		t.Error("sem erro, a tradução não pode inventar um")
	}
}

// Os nove estados do módulo são, palavra por palavra, os do domínio da lista —
// senão `Editable` responderia `false` para um estado que só mudou de grafia, e
// o `ErrInvalidInput` viraria 500 em produção.
func TestEstadosDaReservaSaoOsDaLista(t *testing.T) {
	editaveis := map[string]bool{EstadoHold: true, EstadoConfirmada: true, EstadoCheckIn: true}
	for _, s := range []string{EstadoQuote, EstadoHold, EstadoConfirmada, EstadoCheckIn, EstadoCheckOut,
		EstadoFechada, EstadoCancelada, EstadoExpirada, EstadoNoShow} {
		if !EstadoConhecido(s) || !guests.ReservationStatus(s).Valid() {
			t.Errorf("%s não é estado dos dois lados", s)
		}
		if listaEditavel(s) != editaveis[s] {
			t.Errorf("listaEditavel(%s) = %v", s, listaEditavel(s))
		}
	}
}

func pgErroDaLista(code, constraint string) error {
	return fmt.Errorf("gravando: %w", &pgconn.PgError{
		Code: code, ConstraintName: constraint, TableName: "reservation_guests", Message: "erro do postgres",
	})
}

// As três garantias do banco, pelo nome da restrição: a pessoa repetida é o
// CODE_IN_USE do contrato; as duas de titular só o nosso código viola, e saem
// 500 — inclusive a FK adiada, que chega do COMMIT já embrulhada pelo
// TxManager como 422.
func TestGarantiasDoBancoSobreALista(t *testing.T) {
	contato := uuid.New()

	e := apperr.From(traduzirEscritaDaLista(pgErroDaLista("23505", restricaoDaPessoaNaLista), contato))
	if e.Code != apperr.CodeCodeInUse || e.Status() != http.StatusConflict {
		t.Fatalf("pessoa repetida pela chave primária = %s/%d", e.Code, e.Status())
	}
	if detalhesDe(t, e)["contact_id"] != contato || e.Message != mensagemPessoaNaLista {
		t.Errorf("CODE_IN_USE da corrida diz outra coisa que o do domínio: %q %v", e.Message, e.Details)
	}

	for _, c := range []struct{ code, restricao string }{
		{"23505", restricaoDeUmTitular},
		{"23503", restricaoDoTitularDaReserva},
	} {
		if e := apperr.From(traduzirEscritaDaLista(pgErroDaLista(c.code, c.restricao), contato)); e.Code != apperr.CodeInternal {
			t.Errorf("%s em %s na instrução = %s, esperado INTERNAL", c.code, c.restricao, e.Code)
		}
		// O COMMIT: o TxManager passa o erro por db.MapError, que daria 422.
		noCommit := db.MapError(pgErroDaLista(c.code, c.restricao))
		if apperr.From(noCommit).Status() != http.StatusUnprocessableEntity {
			t.Fatalf("premissa do teste: MapError deveria dar 422 para %s", c.restricao)
		}
		if e := apperr.From(defeitoDaLista(noCommit)); e.Code != apperr.CodeInternal || e.Status() != http.StatusInternalServerError {
			t.Errorf("%s em %s no COMMIT = %s/%d, esperado INTERNAL/500", c.code, c.restricao, e.Code, e.Status())
		}
	}

	// O que não é da lista segue o caminho comum, intocado.
	alheio := db.MapError(pgErroDaLista("23503", "reservations_broker_id_fkey"))
	if !errors.Is(defeitoDaLista(alheio), alheio) || apperr.From(defeitoDaLista(alheio)).Status() != http.StatusUnprocessableEntity {
		t.Error("defeitoDaLista não pode reescrever erro de outra restrição")
	}
	if defeitoDaLista(nil) != nil || traduzirEscritaDaLista(nil, contato) != nil {
		t.Error("sem erro, a tradução não pode inventar um")
	}
}

// O corpo do POST é só `contact_id`. `is_lead_guest` é recusado — o titular se
// troca pela reserva —, e o resto também: campo desconhecido no corpo é 422.
func TestCorpoDoAcrescimoSoTemContactID(t *testing.T) {
	decodificar := func(corpo string) error {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(corpo))
		_, err := httpx.Decode[HospedeAcrescentar](r)
		return err
	}
	if err := decodificar(`{"contact_id":"` + uuid.NewString() + `"}`); err != nil {
		t.Fatalf("corpo válido recusado: %v", err)
	}
	for corpo, campo := range map[string]string{
		`{}`: "contact_id",
		`{"contact_id":"` + uuid.NewString() + `","is_lead_guest":true}`:   "is_lead_guest",
		`{"contact_id":"` + uuid.NewString() + `","name":"Fulano de Tal"}`: "name",
		`{"contact_id":"` + uuid.NewString() + `","doc_number":"123"}`:     "doc_number",
		`{"contact_id":"` + uuid.Nil.String() + `"}`:                       "contact_id",
	} {
		err := decodificar(corpo)
		e := apperr.From(err)
		if err == nil || e.Code != apperr.CodeValidationError {
			t.Errorf("%s: esperado 422 VALIDATION_ERROR, veio %v", corpo, err)
			continue
		}
		if d, _ := e.Details.(map[string]string); d[campo] == "" {
			t.Errorf("%s: a recusa não nomeia %q: %#v", corpo, campo, e.Details)
		}
	}
}

func chavesDoJSON(t *testing.T, v any) []string {
	t.Helper()
	bruto, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("serializando: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(bruto, &m); err != nil {
		t.Fatalf("lendo: %v", err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// HospedeDaReserva é o schema do contrato, campo a campo — os sete são
// `required`, e o documento sai mesmo nulo. MetaDaListaDeHospedes idem.
func TestFormatoDaListaEhODoContrato(t *testing.T) {
	if got, quer := chavesDoJSON(t, HospedeDaReserva{}),
		[]string{"contact_id", "doc_number", "doc_type", "email", "is_lead_guest", "name", "phone_e164"}; !reflect.DeepEqual(got, quer) {
		t.Errorf("HospedeDaReserva = %v, esperado %v", got, quer)
	}
	if got, quer := chavesDoJSON(t, MetaDaListaDeHospedes{}), []string{"capacity", "editable", "guests_count"}; !reflect.DeepEqual(got, quer) {
		t.Errorf("MetaDaListaDeHospedes = %v, esperado %v", got, quer)
	}
	completa := chavesDoJSON(t, ReservaCompleta{})
	if i := sort.SearchStrings(completa, "guests_editable"); i == len(completa) || completa[i] != "guests_editable" {
		t.Errorf("o /full não traz guests_editable: %v", completa)
	}
}

// A trilha da lista guarda o vínculo por id e nada da pessoa.
func TestTrilhaDaListaNaoCarregaAPessoa(t *testing.T) {
	campos := audit.Snapshot(vinculoAuditado{ContactID: uuid.New()})
	chaves := make([]string, 0, len(campos))
	for k := range campos {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	if !reflect.DeepEqual(chaves, []string{"contact_id", "is_lead_guest"}) {
		t.Fatalf("a trilha de reservation_guests carrega %v", chaves)
	}
}

func ptr[T any](v T) *T { return &v }
