package bens

import (
	"context"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

func (s *Service) ListarAvarias(ctx context.Context, f FiltroDeAvarias) ([]Avaria, int64, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarAvarias(ctx, prop, f)
}

func (s *Service) BuscarAvaria(ctx context.Context, id uuid.UUID) (Avaria, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Avaria{}, err
	}
	return s.repo.BuscarAvaria(ctx, prop, id)
}

// CriarAvaria registra o bem quebrado, faltando ou avariado.
//
// Tudo que o corpo cita tem de ser DESTA casa — cômodo, bem, reserva e
// conferência —, e o bem tem de estar colocado no cômodo: a avaria aponta para
// o bem do catálogo NAQUELE ambiente, e é isso que torna o prejuízo somável. A
// conferência citada, se houver, tem de ser da mesma unidade do cômodo: uma
// contagem do AP-01 não acha prato quebrado no AP-02.
func (s *Service) CriarAvaria(ctx context.Context, c AvariaCriar) (Avaria, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Avaria{}, err
	}
	nova := avariaGravada{
		AmbienteID: c.AmbienteID, BemID: c.BemID, Tipo: c.Tipo, Qtd: *c.Qtd, Nota: textoOuNulo(c.Nota),
		ConferenciaID: c.ConferenciaID,
	}

	var criada Avaria
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		erros := map[string]string{}
		unidade, ambienteOK, err := s.repo.AmbienteDaPropriedade(ctx, prop, c.AmbienteID)
		if err != nil {
			return err
		}
		if !ambienteOK {
			erros["room_id"] = "ambiente não encontrado nesta propriedade."
		}
		bemOK, err := s.repo.BemDaPropriedade(ctx, prop, c.BemID)
		if err != nil {
			return err
		}
		if !bemOK {
			erros["item_id"] = "bem não encontrado nesta propriedade."
		}
		if ambienteOK && bemOK {
			colocado, err := s.repo.ColocacaoExiste(ctx, c.AmbienteID, c.BemID)
			if err != nil {
				return err
			}
			if !colocado {
				erros["item_id"] = "este bem não está colocado no ambiente informado."
			}
		}
		if nova.ReservaID, err = s.resolverReserva(ctx, prop, c.ReservaID, c.ReservaCodigo, erros); err != nil {
			return err
		}
		if c.ConferenciaID != nil {
			unidadeDaConf, ok, err := s.repo.UnidadeDaConferencia(ctx, prop, *c.ConferenciaID)
			if err != nil {
				return err
			}
			switch {
			case !ok:
				erros["count_id"] = "conferência não encontrada nesta propriedade."
			case ambienteOK && unidadeDaConf != unidade:
				erros["count_id"] = "esta conferência é de outra unidade."
			}
		}
		if len(erros) > 0 {
			return apperr.Validation(erros)
		}

		id, err := s.repo.CriarAvaria(ctx, prop, nova, atorOuNulo(ctx))
		if err != nil {
			return err
		}
		nova.ID = id
		if criada, err = s.repo.BuscarAvaria(ctx, prop, id); err != nil {
			return err
		}
		return audit.Criacao(ctx, s.repo.pool, entidadeAvaria, audit.VerboCriado, id, nova)
	})
	return criada, err
}

// resolverReserva transforma a referência do corpo — id OU código — no id
// que a avaria guarda, conferindo que a reserva é DESTA casa. Os dois nulos é
// "sem reserva". O erro de campo vai para `erros` com o nome da porta usada,
// e a resposta continua mostrando só o `code`: nada do hóspede.
func (s *Service) resolverReserva(ctx context.Context, prop uuid.UUID, id *uuid.UUID, codigo *string, erros map[string]string) (*uuid.UUID, error) {
	if codigo != nil {
		achada, ok, err := s.repo.ReservaPorCodigo(ctx, prop, NormalizarCodigoDaReserva(*codigo))
		if err != nil {
			return nil, err
		}
		if !ok {
			erros["reservation_code"] = "reserva não encontrada nesta propriedade."
			return nil, nil
		}
		return &achada, nil
	}
	if id == nil {
		return nil, nil
	}
	ok, err := s.repo.ReservaDaPropriedade(ctx, prop, *id)
	if err != nil {
		return nil, err
	}
	if !ok {
		erros["reservation_id"] = "reserva não encontrada nesta propriedade."
		return nil, nil
	}
	return id, nil
}

// referenciaDaReserva é o que o PUT/PATCH disse sobre o vínculo com a reserva.
// `mexer` falso é o PATCH que não citou nenhuma das duas portas.
type referenciaDaReserva struct {
	mexer  bool
	id     *uuid.UUID
	codigo *string
}

// SubstituirAvaria é o PUT. Substituição integral: `resolution` ausente é nulo,
// e nulo REABRE a pendência.
func (s *Service) SubstituirAvaria(ctx context.Context, id uuid.UUID, c AvariaSubstituir) (Avaria, error) {
	ref := referenciaDaReserva{mexer: true, id: c.ReservaID, codigo: c.ReservaCodigo}
	return s.gravarAvaria(ctx, id, ref, func(a *avariaGravada) {
		a.Tipo, a.Qtd, a.Nota = c.Tipo, *c.Qtd, textoOuNulo(c.Nota)
		a.Desfecho = c.Desfecho
	})
}

// AtualizarAvaria é o PATCH, e é por aqui que a avaria se resolve: preencher
// `resolution` grava `resolved_at`/`resolved_by` no servidor; `null` reabre.
func (s *Service) AtualizarAvaria(ctx context.Context, id uuid.UUID, p AvariaAtualizar) (Avaria, error) {
	ref := referenciaDaReserva{mexer: p.ReservaID.Set || p.ReservaCodigo.Set}
	if v, ok := p.ReservaID.Definido(); ok {
		ref.id = &v
	}
	if v, ok := p.ReservaCodigo.Definido(); ok {
		ref.codigo = &v
	}
	return s.gravarAvaria(ctx, id, ref, func(a *avariaGravada) {
		if v, ok := p.Tipo.Definido(); ok {
			a.Tipo = v
		}
		if v, ok := p.Qtd.Definido(); ok {
			a.Qtd = v
		}
		a.Nota = textoDoOpt(p.Nota, a.Nota)
		if p.Desfecho.Set {
			if v, ok := p.Desfecho.Definido(); ok {
				a.Desfecho = &v
			} else {
				a.Desfecho = nil
			}
		}
	})
}

func (s *Service) gravarAvaria(ctx context.Context, id uuid.UUID, ref referenciaDaReserva, mudar func(*avariaGravada)) (Avaria, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Avaria{}, err
	}
	var out Avaria
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarAvaria(ctx, prop, id)
		if err != nil {
			return err
		}
		depois := antes
		mudar(&depois)

		if ref.mexer {
			erros := map[string]string{}
			if depois.ReservaID, err = s.resolverReserva(ctx, prop, ref.id, ref.codigo, erros); err != nil {
				return err
			}
			if len(erros) > 0 {
				return apperr.Validation(erros)
			}
		}

		if err := s.repo.GravarAvaria(ctx, prop, depois, atorOuNulo(ctx)); err != nil {
			return err
		}
		if out, err = s.repo.BuscarAvaria(ctx, prop, id); err != nil {
			return err
		}
		depois.ResolvidaPor, depois.ResolvidaEm = out.ResolvidaPor, out.ResolvidaEm
		return audit.Alteracao(ctx, s.repo.pool, entidadeAvaria, verboDoDesfecho(antes.Desfecho, depois.Desfecho), id, antes, depois)
	})
	return out, err
}

// verboDoDesfecho: "quem resolveu esta avaria?" e "quem a reabriu?" são UMA
// consulta por `action` cada.
func verboDoDesfecho(antes, depois *string) string {
	switch {
	case antes == nil && depois != nil:
		return "resolvida"
	case antes != nil && depois == nil:
		return "reaberta"
	default:
		return audit.VerboAlterado
	}
}

// ApagarAvaria apaga o registro que nunca devia ter nascido. Resolver não é
// apagar — o rastro de que existiu fica em `audit_log`.
func (s *Service) ApagarAvaria(ctx context.Context, id uuid.UUID) error {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return err
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarAvaria(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.ApagarAvaria(ctx, prop, id); err != nil {
			return err
		}
		return audit.Exclusao(ctx, s.repo.pool, entidadeAvaria, audit.VerboExcluido, id, antes)
	})
}
