package agenda

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	regra "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// Os bloqueios de horário da operação (`/agenda/blocks`): "equipe em
// treinamento das 14:00 às 16:00". Não ocupam unidade e não recusam nada — o
// evento marcado em cima recebe o aviso `bloqueio_de_horario` —, e não entram
// na lista de eventos (o painel os desenha como fundo da grade).
//
// Não têm responsável: lidos com `agenda:ver` em QUALQUER escopo, e escritos
// só em `all`. Em `own` a escrita é 403 `own_scope_not_applicable` — nunca
// "um bloqueio só dele", nunca degradar para `all`.

// exigirEscopoDaOperacao recusa a escrita em `own`, ANTES de procurar o
// bloqueio: ele não tem dono, então o 403 não vaza nada sobre a existência.
func exigirEscopoDaOperacao(ctx context.Context, acao string) error {
	e, err := escopo(ctx, acao)
	if err != nil {
		return err
	}
	if e == regra.ScopeOwn {
		return apperr.Forbidden.
			WithMessage("O bloqueio de horário é da operação inteira e não tem responsável: o seu escopo (só os seus) não se aplica a ele.").
			WithDetails(map[string]string{
				"resource": Recurso,
				"action":   acao,
				"scope":    string(regra.ScopeOwn),
				"reason":   string(regra.ReasonOwnScopeNotApplicable),
			})
	}
	return nil
}

// ListarBloqueios é o `GET /agenda/blocks`: os que cruzam `[de, ate)`, dias da
// casa convertidos em instantes por `calendar.Zone`.
func (s *Service) ListarBloqueios(ctx context.Context, de, ate calendar.Date, pagina, porPagina int) ([]BloqueioDeHorario, int64, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	rel, err := s.repo.Relogio(ctx, prop)
	if err != nil {
		return nil, 0, err
	}
	lista, total, err := s.repo.ListarBloqueios(ctx, prop, rel.zona.StartOfDay(de), rel.zona.StartOfDay(ate), pagina, porPagina)
	if err != nil {
		return nil, 0, err
	}
	out := make([]BloqueioDeHorario, len(lista))
	for i, b := range lista {
		out[i] = bloqueioDaResposta(b, rel.zona)
	}
	return out, total, nil
}

// BuscarBloqueio é o detalhe — em qualquer escopo.
func (s *Service) BuscarBloqueio(ctx context.Context, id uuid.UUID) (BloqueioDeHorario, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return BloqueioDeHorario{}, err
	}
	rel, err := s.repo.Relogio(ctx, prop)
	if err != nil {
		return BloqueioDeHorario{}, err
	}
	b, err := s.repo.BuscarBloqueio(ctx, prop, id, false)
	if err != nil {
		return BloqueioDeHorario{}, err
	}
	return bloqueioDaResposta(b, rel.zona), nil
}

// CriarBloqueio grava o bloqueio: janela half-open de até 366 dias
// (`regra.MaxOperationBlockDays`), no horizonte dos eventos; dia inteiro à
// meia-noite da casa.
func (s *Service) CriarBloqueio(ctx context.Context, c BloqueioDeHorarioCriar) (BloqueioDeHorario, error) {
	if err := exigirEscopoDaOperacao(ctx, auth.AcaoCriar); err != nil {
		return BloqueioDeHorario{}, err
	}
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return BloqueioDeHorario{}, err
	}
	alvo, err := bloqueioDoCorpo(c)
	if err != nil {
		return BloqueioDeHorario{}, err
	}
	alvo.CriadoPor = atorOuNulo(ctx)

	var out BloqueioDeHorario
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		rel, err := s.repo.Relogio(ctx, prop)
		if err != nil {
			return err
		}
		if err := s.conferirJanelaDoBloqueio(ctx, alvo, rel); err != nil {
			return err
		}
		novo, err := s.repo.InserirBloqueio(ctx, prop, alvo)
		if err != nil {
			return err
		}
		if err := audit.Criacao(ctx, s.repo.pool, entidadeBloqueio, audit.VerboCriado, novo.ID, novo.auditavel()); err != nil {
			return err
		}
		out = bloqueioDaResposta(novo, rel.zona)
		return nil
	})
	return out, err
}

// SubstituirBloqueio é o PUT: as regras da criação.
func (s *Service) SubstituirBloqueio(ctx context.Context, id uuid.UUID, c BloqueioDeHorarioCriar) (BloqueioDeHorario, error) {
	pedido, err := bloqueioDoCorpo(c)
	if err != nil {
		return BloqueioDeHorario{}, err
	}
	return s.editarBloqueio(ctx, id, func(b *bloqueioGravado) {
		b.Inicio, b.Fim, b.DiaInteiro, b.Motivo = pedido.Inicio, pedido.Fim, pedido.DiaInteiro, pedido.Motivo
	})
}

// AtualizarBloqueio é o PATCH: ausente não muda; nenhum campo aceita `null`.
func (s *Service) AtualizarBloqueio(ctx context.Context, id uuid.UUID, c BloqueioDeHorarioAtualizar) (BloqueioDeHorario, error) {
	if c.vazio() {
		if err := exigirEscopoDaOperacao(ctx, auth.AcaoEditar); err != nil {
			return BloqueioDeHorario{}, err
		}
		return s.BuscarBloqueio(ctx, id)
	}
	var (
		inicio, fim = c.Inicio, c.Fim
		falhas      = map[string]string{}
	)
	pedido := bloqueioGravado{}
	if v, ok := inicio.Definido(); ok {
		t, err := instante(v)
		if err != nil {
			falhas["starts_at"] = "instante inválido."
		}
		pedido.Inicio = t
	}
	if v, ok := fim.Definido(); ok {
		t, err := instante(v)
		if err != nil {
			falhas["ends_at"] = "instante inválido."
		}
		pedido.Fim = t
	}
	if len(falhas) > 0 {
		return BloqueioDeHorario{}, apperr.Validation(falhas)
	}
	return s.editarBloqueio(ctx, id, func(b *bloqueioGravado) {
		if inicio.Set {
			b.Inicio = pedido.Inicio
		}
		if fim.Set {
			b.Fim = pedido.Fim
		}
		if v, ok := c.DiaInteiro.Definido(); ok {
			b.DiaInteiro = v
		}
		if v, ok := c.Motivo.Definido(); ok {
			b.Motivo = strings.TrimSpace(v)
		}
	})
}

func (s *Service) editarBloqueio(ctx context.Context, id uuid.UUID, aplicar func(*bloqueioGravado)) (BloqueioDeHorario, error) {
	if err := exigirEscopoDaOperacao(ctx, auth.AcaoEditar); err != nil {
		return BloqueioDeHorario{}, err
	}
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return BloqueioDeHorario{}, err
	}
	var out BloqueioDeHorario
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarBloqueio(ctx, prop, id, true)
		if err != nil {
			return err
		}
		rel, err := s.repo.Relogio(ctx, prop)
		if err != nil {
			return err
		}
		depois := antes
		aplicar(&depois)
		// A janela só é conferida quando muda: corrigir o motivo de um
		// bloqueio antigo não cai na guarda de digitação da janela.
		if !depois.Inicio.Equal(antes.Inicio) || !depois.Fim.Equal(antes.Fim) || depois.DiaInteiro != antes.DiaInteiro {
			if err := s.conferirJanelaDoBloqueio(ctx, depois, rel); err != nil {
				return err
			}
		}
		gravado, err := s.repo.GravarBloqueio(ctx, depois)
		if err != nil {
			return err
		}
		if err := audit.Alteracao(ctx, s.repo.pool, entidadeBloqueio, audit.VerboAlterado, id,
			antes.auditavel(), gravado.auditavel()); err != nil {
			return err
		}
		out = bloqueioDaResposta(gravado, rel.zona)
		return nil
	})
	return out, err
}

// RemoverBloqueio apaga de verdade — o antes fica em `audit_log`.
func (s *Service) RemoverBloqueio(ctx context.Context, id uuid.UUID) error {
	if err := exigirEscopoDaOperacao(ctx, auth.AcaoExcluir); err != nil {
		return err
	}
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return err
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarBloqueio(ctx, prop, id, true)
		if err != nil {
			return err
		}
		if err := s.repo.ApagarBloqueio(ctx, id); err != nil {
			return err
		}
		return audit.Exclusao(ctx, s.repo.pool, entidadeBloqueio, audit.VerboExcluido, id, antes.auditavel())
	})
}

// conferirJanelaDoBloqueio é `regra.CheckWindow` com o teto do bloqueio de
// horário (366 dias), e "hoje" da casa.
func (s *Service) conferirJanelaDoBloqueio(ctx context.Context, b bloqueioGravado, rel relogio) error {
	w := regra.Window{Start: b.Inicio, End: b.Fim}
	if err := regra.CheckWindow(w, b.DiaInteiro, rel.zona, rel.hoje(), regra.MaxOperationBlockDays); err != nil {
		return s.traduzirRegra(ctx, err)
	}
	return nil
}

// bloqueioDoCorpo converte o corpo do POST/PUT (já validado).
func bloqueioDoCorpo(c BloqueioDeHorarioCriar) (bloqueioGravado, error) {
	inicio, errInicio := instante(c.Inicio)
	fim, errFim := instante(c.Fim)
	if errInicio != nil || errFim != nil {
		return bloqueioGravado{}, apperr.Validation(map[string]string{"starts_at": "instante inválido."})
	}
	return bloqueioGravado{Inicio: inicio, Fim: fim, DiaInteiro: c.DiaInteiro.Ou(false), Motivo: strings.TrimSpace(c.Motivo)}, nil
}

func bloqueioDaResposta(b bloqueioGravado, z calendar.Zone) BloqueioDeHorario {
	return BloqueioDeHorario{
		ID: b.ID, Inicio: z.In(b.Inicio), Fim: z.In(b.Fim), DiaInteiro: b.DiaInteiro, Motivo: b.Motivo,
		CriadoPor: b.CriadoPor, CriadoEm: z.In(b.CriadoEm), AtualizadoEm: z.In(b.AtualizadoEm),
	}
}
