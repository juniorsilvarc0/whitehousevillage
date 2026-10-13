package agenda

import (
	"context"
	"sort"

	regra "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// Os horários da operação são DADO (`agenda_settings`, uma linha por
// propriedade). Os que nascem no banco são provisórios, a confirmar pelo dono
// (decisão 12): `provisional: true` enquanto ninguém gravou.
//
// Gravar pede `settings:editar` — a rota diz, e não este código —, e
// REPROGRAMA na mesma transação o que a agenda já tinha enchido com o horário
// antigo (`regra.Retime`): o automático, agendado, de hoje em diante, que
// ainda está EXATAMENTE no horário que a configuração anterior lhe dava. O
// ajustado à mão, o passado e o concluído ficam.

// configuracaoAuditada é o documento da trilha de `agenda_settings`.
type configuracaoAuditada struct {
	Entrada         string             `json:"checkin_time"`
	Saida           string             `json:"checkout_time"`
	InicioDaLimpeza string             `json:"cleaning_start"`
	FimDaLimpeza    string             `json:"cleaning_end"`
	Funcionamento   []intervaloGravado `json:"business_hours"`
	PassoEmMinutos  int                `json:"slot_minutes"`
	FolgaEmMinutos  int                `json:"buffer_minutes"`
}

func paraATrilha(s regra.Settings) configuracaoAuditada {
	out := configuracaoAuditada{
		Entrada: s.CheckIn.String(), Saida: s.CheckOut.String(),
		InicioDaLimpeza: s.CleaningStart.String(), FimDaLimpeza: s.CleaningEnd.String(),
		Funcionamento:  []intervaloGravado{},
		PassoEmMinutos: s.SlotMinutes, FolgaEmMinutos: s.BufferMinutes,
	}
	for _, h := range ordenarHorario(s.BusinessHours) {
		out.Funcionamento = append(out.Funcionamento, intervaloGravado{DiaDaSemana: h.Weekday, Inicio: h.Start.String(), Fim: h.End.String()})
	}
	return out
}

// Configuracao é o `GET /agenda/settings`, com o fuso e o "hoje" da casa — o
// painel desenha "hoje" com o dia da casa, e não com o do navegador.
func (s *Service) Configuracao(ctx context.Context) (ConfiguracaoDaAgenda, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return ConfiguracaoDaAgenda{}, err
	}
	rel, err := s.repo.Relogio(ctx, prop)
	if err != nil {
		return ConfiguracaoDaAgenda{}, err
	}
	cfg, err := s.repo.Configuracao(ctx, prop, semTrava)
	if err != nil {
		return ConfiguracaoDaAgenda{}, err
	}
	return configuracaoDaResposta(rel, cfg), nil
}

// GravarConfiguracao é o `PUT /agenda/settings`: valida a coerência inteira
// (`regra.Settings.Validate`, todas as violações de uma vez), grava, e
// reprograma o que estava no horário antigo — tudo numa transação. Devolve
// quantos eventos foram reprogramados (`meta.retimed_events`).
func (s *Service) GravarConfiguracao(ctx context.Context, c ConfiguracaoDaAgendaEntrada) (ConfiguracaoDaAgenda, MetaDaConfiguracao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return ConfiguracaoDaAgenda{}, MetaDaConfiguracao{}, err
	}
	nova, err := c.dominio()
	if err != nil {
		return ConfiguracaoDaAgenda{}, MetaDaConfiguracao{}, apperr.Validation(map[string]string{"body": "hora ilegível."}).WithCause(err)
	}
	if err := nova.Validate(); err != nil {
		return ConfiguracaoDaAgenda{}, MetaDaConfiguracao{}, s.traduzirRegra(ctx, err)
	}
	autor := atorOuNulo(ctx)

	var (
		out  ConfiguracaoDaAgenda
		meta MetaDaConfiguracao
	)
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		meta = MetaDaConfiguracao{}
		rel, err := s.repo.Relogio(ctx, prop)
		if err != nil {
			return err
		}
		// O horário ANTIGO, travado: a reserva em voo (FOR SHARE) termina antes,
		// e a que começar depois espera esta transação e lê o horário novo.
		antiga, err := s.repo.Configuracao(ctx, prop, travaExclusiva)
		if err != nil {
			return err
		}
		if err := s.repo.GravarConfiguracao(ctx, prop, nova, autor); err != nil {
			return err
		}

		z, hoje := rel.zona, rel.hoje()
		eventos, err := s.repo.AutomaticosAgendadosDesde(ctx, prop, z.StartOfDay(hoje))
		if err != nil {
			return err
		}
		for _, e := range eventos {
			janela, mudou := regra.Retime(e.ligado(), antiga.Regras, nova, z, hoje)
			if !mudou {
				continue
			}
			depois, err := s.repo.MoverJanela(ctx, e.ID, janela)
			if err != nil {
				return err
			}
			if err := audit.Alteracao(ctx, s.repo.pool, entidadeEvento, verboReprogramado, e.ID,
				e.auditavel(), depois.auditavel()); err != nil {
				return err
			}
			meta.Reprogramados++
		}

		if err := audit.Alteracao(ctx, s.repo.pool, entidadeConfiguracao, audit.VerboAlterado, prop,
			paraATrilha(antiga.Regras), paraATrilha(nova)); err != nil {
			return err
		}
		gravada, err := s.repo.Configuracao(ctx, prop, semTrava)
		if err != nil {
			return err
		}
		out = configuracaoDaResposta(rel, gravada)
		return nil
	})
	return out, meta, err
}

func configuracaoDaResposta(rel relogio, cfg configuracaoGravada) ConfiguracaoDaAgenda {
	s := cfg.Regras
	out := ConfiguracaoDaAgenda{
		Fuso: rel.zona.Name(), Hoje: rel.hoje().String(),
		Entrada: s.CheckIn.String(), Saida: s.CheckOut.String(),
		InicioDaLimpeza: s.CleaningStart.String(), FimDaLimpeza: s.CleaningEnd.String(),
		Funcionamento:  []IntervaloDeFuncionamento{},
		PassoEmMinutos: s.SlotMinutes, FolgaEmMinutos: s.BufferMinutes,
		Provisoria:    cfg.AtualizadaPor == nil,
		AtualizadaEm:  rel.zona.In(cfg.AtualizadaEm),
		AtualizadaPor: cfg.AtualizadaPor, AtualizadaPorNome: cfg.AtualizadaPorNome,
	}
	for _, h := range ordenarHorario(s.BusinessHours) {
		dia := h.Weekday
		out.Funcionamento = append(out.Funcionamento, IntervaloDeFuncionamento{DiaDaSemana: &dia, Inicio: h.Start.String(), Fim: h.End.String()})
	}
	return out
}

// ordenarHorario põe o funcionamento por dia e início — a ordem do contrato.
func ordenarHorario(hs []regra.Hours) []regra.Hours {
	out := append([]regra.Hours(nil), hs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Weekday != out[j].Weekday {
			return out[i].Weekday < out[j].Weekday
		}
		return out[i].Start < out[j].Start
	})
	return out
}
