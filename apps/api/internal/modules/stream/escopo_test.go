package stream

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/realtime"
)

// Os três desfechos que o filtro resolve SEM ir ao banco. O pool é nulo de
// propósito: se algum deles consultasse, o teste estouraria em vez de passar
// silenciosamente — é a forma de provar que o caminho barato é mesmo barato.
func TestFiltroDecideSemBancoQuandoDa(t *testing.T) {
	f := NovoFiltro(nil)
	ctx := context.Background()

	gestor := usuarioCom(map[string]string{recursoCalendario: auth.EscopoAll})

	t.Run("escopo all entrega sem consultar", func(t *testing.T) {
		pode, err := f.PodeEntregar(ctx, gestor, realtime.Evento{
			Entidade: "stay_block", ID: uuid.New(), PropertyID: gestor.PropertyID,
		})
		if err != nil || !pode {
			t.Fatalf("pode = %v, err = %v", pode, err)
		}
	})

	// Hoje há uma casa só. A barreira existe para o dia da segunda: sem ela, a
	// migração de multi-imóvel passaria com o barramento vazando movimento de um
	// imóvel para a gestão do outro, e ninguém veria.
	t.Run("evento de outra propriedade não sai", func(t *testing.T) {
		pode, err := f.PodeEntregar(ctx, gestor, realtime.Evento{
			Entidade: "stay_block", ID: uuid.New(), PropertyID: uuid.New(),
		})
		if err != nil || pode {
			t.Fatalf("pode = %v, err = %v", pode, err)
		}
	})

	t.Run("sem a célula na matriz não sai", func(t *testing.T) {
		semNada := usuarioCom(map[string]string{"reservations": auth.EscopoAll})
		pode, err := f.PodeEntregar(ctx, semNada, realtime.Evento{
			Entidade: "stay_block", ID: uuid.New(), PropertyID: semNada.PropertyID,
		})
		if err != nil || pode {
			t.Fatalf("pode = %v, err = %v", pode, err)
		}
	})

	t.Run("entidade desconhecida não sai", func(t *testing.T) {
		pode, err := f.PodeEntregar(ctx, gestor, realtime.Evento{
			Entidade: "marciano", ID: uuid.New(), PropertyID: gestor.PropertyID,
		})
		if err != nil || pode {
			t.Fatalf("pode = %v, err = %v", pode, err)
		}
	})
}
