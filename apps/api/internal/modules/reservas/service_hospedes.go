package reservas

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/guests"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/pii"
)

// A rooming list (F2-22, spec §6 "A rooming list"): quem dorme na casa nesta
// reserva — o titular e os acompanhantes.
//
// As regras são de `internal/domain/guests` (Admit, CheckAdd, CheckRemove,
// PlanEdit, Carry, Editable) e este arquivo só as consome: lê o que o domínio
// precisa com a reserva travada, pede a decisão e grava o que ela manda. Três
// garantias são do banco e não daqui — um titular por reserva, a linha de
// titular ser a do `contact_id` da reserva, a mesma pessoa uma vez só — e a
// tradução das violações delas está em erros.go.
//
// O titular tem UMA fonte, `reservations.contact_id`; a linha dele na lista é
// consequência. A troca de titular é o PUT/PATCH da reserva (service.go,
// `atualizar`), a reserva nasce com a linha dele (`emitir`) e a remarcação leva
// a lista junto (`Remarcar`). Por aqui entram e saem só acompanhantes.

// entidadeDoContato é a entidade que `pii_access_log` registra: a pessoa
// exibida é sempre uma ficha de `contacts`.
const entidadeDoContato = "contacts"

// ─────────────────────────── GET /reservations/{id}/guests ──────────

// ListarHospedes devolve a lista da reserva e o `meta` que a tela usa para
// desenhar os botões.
//
// A leitura é a de UMA reserva, com o escopo de `ver` no SQL: fora do escopo é
// 404 — nunca 403, que confirmaria que a reserva existe — e nada é gravado.
//
// O `meta` (o número vendido, se a lista se edita) e a lista saem da MESMA
// fotografia do banco (`db.TxManager.Fotografia`), pelo mesmo motivo do `/full`:
// lidos em instantes diferentes, um PATCH de `guests_count` ou uma troca de
// titular comitada entre as duas leituras fariam a resposta dizer "cabem 1" ao
// lado de dois nomes. Sem trava: leitura não espera — nem atrasa — a escrita em
// curso na mesma reserva.
//
// Documento, telefone e e-mail saem cheios, então cada pessoa exibida grava uma
// linha em `pii_access_log`, numa instrução: depois da fotografia, fora dela (que
// não escreve) e com falha FECHADA — se o registro não entra, a lista não sai.
func (s *Servico) ListarHospedes(ctx context.Context, id uuid.UUID) (ListaDeHospedes, error) {
	u, err := ator(ctx)
	if err != nil {
		return ListaDeHospedes{}, err
	}

	var out ListaDeHospedes
	err = s.tx.Fotografia(ctx, func(ctx context.Context) error {
		e, err := s.repo.LerEstado(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoVer), u.ID)
		if err != nil {
			return err
		}
		hospedes, err := s.repo.HospedesDe(ctx, id)
		if err != nil {
			return err
		}
		out = ListaDeHospedes{
			Hospedes: hospedes,
			Meta: MetaDaListaDeHospedes{
				GuestsCount: e.Hospedes,
				Capacity:    e.Capacidade,
				Editable:    listaEditavel(e.Status),
			},
		}
		return nil
	})
	if err != nil {
		return ListaDeHospedes{}, err
	}

	if err := s.registrarHospedesExibidos(ctx, out.Hospedes); err != nil {
		return ListaDeHospedes{}, err
	}
	return out, nil
}

// registrarHospedesExibidos grava UMA linha de `pii_access_log` por pessoa
// exibida (`rooming_list`), numa instrução só. A pergunta da LGPD — "quem viu o
// meu documento?" — é por pessoa, não por tela: abrir a lista de um evento da
// Completa grava até 24 linhas, e é o custo certo. Lista vazia não exibiu
// ninguém e não grava.
func (s *Servico) registrarHospedesExibidos(ctx context.Context, hospedes []HospedeDaReserva) error {
	exibidos := make([]uuid.UUID, 0, len(hospedes))
	for _, h := range hospedes {
		exibidos = append(exibidos, h.ContactID)
	}
	return pii.RegistrarVarios(ctx, s.repo.pool, entidadeDoContato, exibidos, pii.MotivoListaDeHospedes)
}

// listaEditavel é `guests.Editable` sobre o estado gravado — o `meta.editable`
// da lista e o `guests_editable` do `/full`. A tela desenha os botões daqui,
// sem uma segunda cópia da regra.
func listaEditavel(status string) bool {
	return guests.Editable(guests.ReservationStatus(status))
}

// ─────────────────────────── POST /reservations/{id}/guests ─────────

// AcrescentarHospede põe na lista, como acompanhante, uma pessoa que já tem
// ficha. Numa transação, nesta ordem:
//
//  1. trava a reserva (escopo de `editar`) — a mesma trava de toda escrita na
//     lista e da edição de `guests_count`;
//  2. lê a lista e a ficha da pessoa, esta em `FOR KEY SHARE`;
//  3. `guests.CheckAdd` decide: estado, ficha (422 se inexistente ou de outra
//     casa, 409 se anonimizada), repetida, cheia;
//  4. grava, audita por `contact_id` e põe `guest_added` na linha do tempo;
//  5. relê a pessoa no formato da lista e registra a leitura — a resposta sai
//     cheia, então grava UMA linha em `pii_access_log`, dentro da transação: se
//     o registro não entra, nem o acréscimo nem a resposta saem.
func (s *Servico) AcrescentarHospede(ctx context.Context, id uuid.UUID, corpo HospedeAcrescentar) (HospedeDaReserva, error) {
	u, err := ator(ctx)
	if err != nil {
		return HospedeDaReserva{}, err
	}

	var out HospedeDaReserva
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		e, err := s.repo.TravarReserva(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		lista, err := s.repo.ListaDaReserva(ctx, id)
		if err != nil {
			return err
		}
		ficha, err := s.repo.FichaDoContato(ctx, u.PropertyID, corpo.ContactID)
		if err != nil {
			return err
		}
		if err := guests.CheckAdd(reservaDaLista(e, lista), ficha); err != nil {
			return traduzirRegraDaLista(err)
		}

		if err := s.repo.AcrescentarAcompanhante(ctx, id, corpo.ContactID); err != nil {
			return err
		}
		if err := s.auditarAcompanhante(ctx, id, corpo.ContactID, false); err != nil {
			return err
		}
		if err := s.repo.InserirEvento(ctx, id, EventoHospedeAcrescentado, map[string]any{
			"contact_id": corpo.ContactID.String(),
		}, &u.ID); err != nil {
			return err
		}

		if out, err = s.repo.HospedeDaLista(ctx, id, corpo.ContactID); err != nil {
			return err
		}
		return pii.Registrar(ctx, s.repo.pool, entidadeDoContato, corpo.ContactID, pii.MotivoListaDeHospedes)
	})
	if err != nil {
		return HospedeDaReserva{}, defeitoDaLista(err)
	}
	return out, nil
}

// ─────────────────────────── DELETE …/guests/{contactId} ────────────

// RetirarHospede tira um acompanhante da lista; a ficha dele continua em
// `contacts`. `guests.CheckRemove` decide — estado, titular (409: troca-se pela
// reserva), quem não está na lista (404). Sem corpo e sem dado pessoal na
// resposta, então não grava `pii_access_log`.
func (s *Servico) RetirarHospede(ctx context.Context, id, contato uuid.UUID) error {
	u, err := ator(ctx)
	if err != nil {
		return err
	}

	err = s.tx.Do(ctx, func(ctx context.Context) error {
		e, err := s.repo.TravarReserva(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		lista, err := s.repo.ListaDaReserva(ctx, id)
		if err != nil {
			return err
		}
		if err := guests.CheckRemove(reservaDaLista(e, lista), contato); err != nil {
			return traduzirRegraDaLista(err)
		}

		saiu, err := s.repo.RetirarAcompanhante(ctx, id, contato)
		if err != nil {
			return err
		}
		if !saiu {
			// Com a reserva travada a lista não muda entre a leitura e o
			// DELETE, e CheckRemove já recusou o titular. Chegar aqui é uma
			// linha marcada como titular sem ser o `contact_id` da reserva — o
			// que a FK adiada do F2-22 impede —, e mesmo assim ela não sai por
			// esta porta: é defeito, 500, com a causa no log.
			return apperr.Internal.WithCause(fmt.Errorf(
				"reservas: a linha de %s na lista da reserva %s não saiu pelo DELETE de acompanhante", contato, id))
		}
		if err := s.auditarAcompanhante(ctx, id, contato, true); err != nil {
			return err
		}
		return s.repo.InserirEvento(ctx, id, EventoHospedeRetirado, map[string]any{
			"contact_id": contato.String(),
		}, &u.ID)
	})
	return defeitoDaLista(err)
}

// ─────────────────────────── Remarcação ─────────────────────────────

// carregarAcompanhantes grava, na reserva nova de uma remarcação, os
// acompanhantes que `guests.Carry` decidiu levar. Cada um entra no `audit_log`
// como qualquer acréscimo, por `contact_id`; a linha do tempo da nova não
// repete um `guest_added` por pessoa — a remarcação já está nela.
func (s *Servico) carregarAcompanhantes(ctx context.Context, nova uuid.UUID, carregados []uuid.UUID) error {
	if err := s.repo.AcrescentarAcompanhantes(ctx, nova, carregados); err != nil {
		return err
	}
	for _, contato := range carregados {
		if err := s.auditarAcompanhante(ctx, nova, contato, false); err != nil {
			return err
		}
	}
	return nil
}

// ─────────────────────────── Ponte com o domínio ────────────────────

// reservaDaLista monta a reserva como `internal/domain/guests` a enxerga. `e`
// vem de TravarReserva — a linha travada é o que torna a decisão imune a uma
// escrita concorrente na mesma lista.
func reservaDaLista(e Estado, lista []MembroDaLista) guests.Reservation[uuid.UUID] {
	nomes := make([]uuid.UUID, len(lista))
	for i, m := range lista {
		nomes[i] = m.ContactID
	}
	return guests.Reservation[uuid.UUID]{
		Status:      guests.ReservationStatus(e.Status),
		Lead:        e.ContactID,
		GuestsCount: e.Hospedes,
		Capacity:    e.Capacidade,
		Listed:      nomes,
	}
}

// fichasDaLista converte a lista no que `guests.Carry` lê: quem está nela
// existe (a FK garante), e o que importa é se foi anonimizado.
func fichasDaLista(lista []MembroDaLista) []guests.Contact[uuid.UUID] {
	out := make([]guests.Contact[uuid.UUID], len(lista))
	for i, m := range lista {
		out[i] = guests.Contact[uuid.UUID]{ID: m.ContactID, Found: true, Anonymized: m.Anonimizado}
	}
	return out
}
