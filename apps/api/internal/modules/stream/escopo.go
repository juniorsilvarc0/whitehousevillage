package stream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/realtime"
)

// tempoDaConferenciaDeEscopo é o teto de UMA conferência de dono. Curto de
// propósito: o laço da conexão SSE não pode ficar preso no banco — é ele que
// escreve o batimento que mantém a conexão viva.
const tempoDaConferenciaDeEscopo = 2 * time.Second

// tabelaDaEntidade mapeia a entidade do envelope para a tabela onde mora o dono
// comercial. Mapa FECHADO: o nome da tabela entra numa string de SQL, e a única
// forma segura de fazer isso é ele nunca vir de fora.
var tabelaDaEntidade = map[string]string{
	"stay_block":  "stay_blocks",
	"reservation": "reservations",
	"opportunity": "crm_opportunities",
	"lead":        "crm_leads",
	"activity":    "crm_activities",
}

// recursoDaEntidade diz qual célula da matriz governa cada entidade — é ela que
// decide se o escopo é `all` ou `own`.
var recursoDaEntidade = map[string]string{
	"stay_block":  recursoCalendario,
	"reservation": recursoCalendario,
	"opportunity": recursoCRM,
	"lead":        recursoCRM,
	"activity":    recursoCRM,
}

// Filtro decide, evento a evento, se aquela conexão pode saber que aquilo
// mudou.
//
// Por que isto existe, já que o evento é magro: o envelope não carrega dado
// sensível, mas carrega o `id` — e o `id` de uma oportunidade alheia já é
// informação. O contrato é explícito: "o corretor não recebe aviso de
// oportunidade alheia — nem o id dela". Sem este filtro, um corretor com escopo
// `own` conseguiria contar quantos negócios a casa fecha por dia, e quando.
type Filtro struct {
	pool *pgxpool.Pool
}

func NovoFiltro(pool *pgxpool.Pool) *Filtro { return &Filtro{pool: pool} }

// PodeEntregar responde se o evento deve ir para aquela conexão.
//
// Três barreiras, da mais barata para a mais cara:
//
//  1. propriedade — comparação em memória, custo zero. Hoje há uma casa só, mas
//     a coluna já existe em toda tabela e é ela que impede um segundo imóvel
//     futuro de vazar movimento pelo barramento no dia da migração, e não no dia
//     em que alguém lembrar;
//  2. escopo `all` — a gestão vê tudo, e ninguém consulta o banco;
//  3. escopo `own` — UMA leitura indexada por `id`, e só para quem tem escopo
//     restrito. É o único caminho que custa round-trip, e é o caminho de quem
//     recebe poucos eventos por definição.
//
// Linha que não existe mais (DELETE) é tratada como NÃO entregável para escopo
// `own`: sem a linha não há como saber de quem ela era, e entregar por via das
// dúvidas transformaria a exclusão num oráculo — "sumiu algo que eu não podia
// ver" é exatamente o que a regra proíbe. O custo é o corretor não ser avisado
// da remoção de um bloco que era dele; o refetch periódico da tela cobre.
func (f *Filtro) PodeEntregar(ctx context.Context, u *auth.Usuario, ev realtime.Evento) (bool, error) {
	if ev.PropertyID != uuid.Nil && u.PropertyID != uuid.Nil && ev.PropertyID != u.PropertyID {
		return false, nil
	}

	recurso, ok := recursoDaEntidade[ev.Entidade]
	if !ok {
		return false, nil
	}
	escopo, ok := u.Permissoes.Escopo(recurso, auth.AcaoVer)
	if !ok {
		return false, nil
	}
	if escopo == auth.EscopoAll {
		return true, nil
	}

	tabela, ok := tabelaDaEntidade[ev.Entidade]
	if !ok {
		return false, nil
	}

	ctx, cancelar := context.WithTimeout(ctx, tempoDaConferenciaDeEscopo)
	defer cancelar()

	var dono *uuid.UUID
	// #nosec — `tabela` vem do mapa fechado acima; o id é parâmetro.
	q := fmt.Sprintf(`SELECT owner_id FROM %s WHERE id = $1`, tabela)
	if err := f.pool.QueryRow(ctx, q, ev.ID).Scan(&dono); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return dono != nil && *dono == u.ID, nil
}
