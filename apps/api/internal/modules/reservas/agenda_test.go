package reservas

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/agenda"
)

// A agenda lê a reserva em SQL com o vocabulário DESTE módulo — os estados que
// aceitam evento, o tipo do evento de check-in na timeline e o recurso RBAC
// das reservas —, mas não pode
// importá-lo (a reserva importa a agenda). As duas cópias são conferidas aqui:
// renomear um estado de um lado só faria a agenda deixar de achar as reservas
// confirmadas, sem erro nenhum.
func TestAAgendaLeAReservaComOMesmoVocabulario(t *testing.T) {
	pares := map[string][2]string{
		"hold":               {agenda.ReservaEmHold, EstadoHold},
		"confirmed":          {agenda.ReservaConfirmada, EstadoConfirmada},
		"checked_in":         {agenda.ReservaHospedada, EstadoCheckIn},
		"evento de check-in": {agenda.EventoDeEntradaNaReserva, EventoCheckIn},
		// O recurso RBAC cujo escopo de `ver` decide a que reserva um evento
		// pode se ligar.
		"recurso": {agenda.RecursoDasReservas, Recurso},
	}
	for nome, par := range pares {
		if par[0] != par[1] {
			t.Errorf("%s: a agenda lê %q, a reserva grava %q", nome, par[0], par[1])
		}
	}
}

// O `/confirm` roda a agenda PRIMEIRO, e o que vier depois (o financeiro)
// entra atrás dela, sem tirá-la do lugar.
func TestOsEfeitosDaConfirmacaoComecamPelaAgenda(t *testing.T) {
	s := NovoServico(NewRepository(nil), nil, nil)
	if len(s.efeitosDaConfirmacao) != 1 {
		t.Fatalf("efeitos: %d, esperado só a agenda", len(s.efeitosDaConfirmacao))
	}
	if _, ok := s.efeitosDaConfirmacao[0].(*agenda.Ligacao); !ok {
		t.Fatalf("o primeiro efeito é %T, e não a agenda", s.efeitosDaConfirmacao[0])
	}
	if s.agenda == nil {
		t.Fatal("o service nasce sem a agenda ligada")
	}
	outro := efeitoDeTeste{}
	s.ComEfeitosDaConfirmacao(outro)
	if len(s.efeitosDaConfirmacao) != 2 || s.efeitosDaConfirmacao[1] != EfeitoDaConfirmacao(outro) {
		t.Fatalf("o efeito acrescentado entra depois da agenda: %v", s.efeitosDaConfirmacao)
	}
}

type efeitoDeTeste struct{}

func (efeitoDeTeste) AoConfirmar(_ context.Context, _ uuid.UUID) error { return nil }
