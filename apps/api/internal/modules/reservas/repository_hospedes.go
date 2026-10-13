package reservas

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// A rooming list em `reservation_guests` (F2-22).
//
// Nenhuma função daqui trava a reserva: quem escreve na lista chega com a linha
// da reserva já travada em `FOR UPDATE` (TravarReserva), a MESMA trava da
// edição de `guests_count`. É ela que torna a contagem e a decisão do domínio
// imunes a uma escrita concorrente na mesma lista — dois acréscimos no último
// lugar fazem fila nela, e o segundo conta a lista com o primeiro dentro.

// MembroDaLista é uma linha da lista como as regras precisam dela: quem é e se
// a ficha foi anonimizada (a remarcação não carrega anonimizado). Quem é o
// titular o domínio sabe pela reserva — é `reservations.contact_id`.
type MembroDaLista struct {
	ContactID   uuid.UUID
	Anonimizado bool
}

// ListaDaReserva devolve quem está na lista, o titular incluso, em ordem de
// `contact_id` — a ordem em que a remarcação insere os carregados.
func (r *Repository) ListaDaReserva(ctx context.Context, reserva uuid.UUID) ([]MembroDaLista, error) {
	const q = `
		SELECT g.contact_id, c.anonymized_at IS NOT NULL
		  FROM reservation_guests g
		  JOIN contacts c ON c.id = g.contact_id
		 WHERE g.reservation_id = $1
		 ORDER BY g.contact_id`

	linhas, err := r.exec(ctx).Query(ctx, q, reserva)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []MembroDaLista{}
	for linhas.Next() {
		var m MembroDaLista
		if err := linhas.Scan(&m.ContactID, &m.Anonimizado); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, m)
	}
	return out, db.MapError(linhas.Err())
}

// colunasDoHospede é o recorte da ficha que a lista exibe — o mínimo que a
// recepção usa (spec §6). Nascimento, cidade e anotação não entram.
const colunasDoHospede = `c.id, c.name, c.doc_type, c.doc_number, c.phone_e164, c.email, g.is_lead_guest`

func lerHospede(linha pgx.Row) (HospedeDaReserva, error) {
	var h HospedeDaReserva
	err := linha.Scan(&h.ContactID, &h.Nome, &h.DocTipo, &h.Documento, &h.Telefone, &h.Email, &h.Titular)
	return h, err
}

// HospedesDe devolve a rooming list: o titular primeiro, os acompanhantes por
// nome e, no empate, por `contact_id` — homônimo não pode trocar de lugar entre
// duas leituras. Lista vazia é `[]`, nunca nulo: o contrato promete array.
func (r *Repository) HospedesDe(ctx context.Context, reserva uuid.UUID) ([]HospedeDaReserva, error) {
	q := `SELECT ` + colunasDoHospede + `
		  FROM reservation_guests g
		  JOIN contacts c ON c.id = g.contact_id
		 WHERE g.reservation_id = $1
		 ORDER BY g.is_lead_guest DESC, c.name, c.id`

	linhas, err := r.exec(ctx).Query(ctx, q, reserva)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []HospedeDaReserva{}
	for linhas.Next() {
		h, err := lerHospede(linhas)
		if err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, h)
	}
	return out, db.MapError(linhas.Err())
}

// HospedeDaLista relê UMA pessoa da lista, no formato da lista — é a resposta
// do POST, lida depois do INSERT para sair exatamente como a lista a mostra.
func (r *Repository) HospedeDaLista(ctx context.Context, reserva, contato uuid.UUID) (HospedeDaReserva, error) {
	q := `SELECT ` + colunasDoHospede + `
		  FROM reservation_guests g
		  JOIN contacts c ON c.id = g.contact_id
		 WHERE g.reservation_id = $1 AND g.contact_id = $2`

	h, err := lerHospede(r.exec(ctx).QueryRow(ctx, q, reserva, contato))
	if errors.Is(err, pgx.ErrNoRows) {
		return h, apperr.NotFound("Hóspede")
	}
	return h, db.MapError(err)
}

// AcrescentarAcompanhante põe a pessoa na lista como acompanhante. O titular
// nunca entra por aqui: a linha dele é a projeção de `reservations.contact_id`.
func (r *Repository) AcrescentarAcompanhante(ctx context.Context, reserva, contato uuid.UUID) error {
	const q = `
		INSERT INTO reservation_guests (reservation_id, contact_id, is_lead_guest)
		VALUES ($1, $2, false)`

	_, err := r.exec(ctx).Exec(ctx, q, reserva, contato)
	return traduzirEscritaDaLista(err, contato)
}

// AcrescentarAcompanhantes grava os acompanhantes que a remarcação carrega para
// a reserva nova, numa instrução só. A ordem por `contact_id` é a ordem em que
// as fichas são travadas pela FK — a mesma em toda remarcação.
func (r *Repository) AcrescentarAcompanhantes(ctx context.Context, reserva uuid.UUID, contatos []uuid.UUID) error {
	if len(contatos) == 0 {
		return nil
	}
	const q = `
		INSERT INTO reservation_guests (reservation_id, contact_id, is_lead_guest)
		SELECT $1, x.contato, false
		  FROM unnest($2::uuid[]) AS x(contato)
		 ORDER BY x.contato`

	_, err := r.exec(ctx).Exec(ctx, q, reserva, contatos)
	if db.IsUniqueViolation(err, restricaoDaPessoaNaLista) {
		// A reserva acabou de nascer, a lista dela só tem o titular e
		// `guests.Carry` não o devolve: pessoa repetida aqui não é pedido de
		// ninguém, é defeito nosso — 500, não CODE_IN_USE.
		return apperr.Internal.WithCause(err)
	}
	return traduzirEscritaDaLista(err, uuid.Nil)
}

// RetirarAcompanhante apaga a linha do acompanhante e diz se havia uma.
//
// `AND NOT is_lead_guest` no WHERE, e não só no domínio: o titular não sai por
// esta porta nem se o domínio errar — ele se troca editando a reserva.
func (r *Repository) RetirarAcompanhante(ctx context.Context, reserva, contato uuid.UUID) (bool, error) {
	const q = `
		DELETE FROM reservation_guests
		 WHERE reservation_id = $1 AND contact_id = $2 AND NOT is_lead_guest`

	tag, err := r.exec(ctx).Exec(ctx, q, reserva, contato)
	if err != nil {
		return false, db.MapError(err)
	}
	return tag.RowsAffected() > 0, nil
}

// RetirarTitularAnterior apaga a linha do titular que SAI numa troca de
// titular (`guests.Plan.Remove`). Ele não vira acompanhante: a troca é quase
// sempre correção, e rebaixá-lo deixaria um estranho associado à estadia.
func (r *Repository) RetirarTitularAnterior(ctx context.Context, reserva, contato uuid.UUID) error {
	const q = `DELETE FROM reservation_guests WHERE reservation_id = $1 AND contact_id = $2`

	_, err := r.exec(ctx).Exec(ctx, q, reserva, contato)
	return db.MapError(err)
}
