package maintenance

import (
	"fmt"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// O bloqueio de calendário da ordem.
//
// É uma linha de `stay_blocks` com `source = 'maintenance'` e status
// `confirmed`, na unidade da ordem — a MESMA tabela das reservas, para que a
// manutenção impeça uma venda pela mesma constraint que impede duas vendas.
// Liberar é `status = 'cancelled'`, como em `DELETE /blocks/{id}`: a data volta
// a ser vendável e o registro fica.
//
// # Uma regra atravessa tudo daqui: noite que já passou não muda
//
// "Hoje" é o dia D no fuso da propriedade. As noites anteriores a D já
// aconteceram — estiveram bloqueadas ou não, e isso é história do calendário,
// que o mapa de ocupação e o BI leem. A noite de D ainda não aconteceu: ela
// ainda pode ser vendida ou bloqueada. Formalmente, para todo período P,
//
//	passado(P) = P ∩ (−∞, D)
//
// e nenhuma operação deste arquivo muda `passado(P)` de um bloqueio vivo. É
// dela que saem as três regras que, escritas soltas, pareceriam arbitrárias:
//
//   - liberar ao encerrar APAGA o bloqueio que não começou, CORTA o fim do que
//     está em curso para D e DEIXA o que já terminou;
//   - estender ou encurtar um bloqueio em curso não mexe no início, e o fim não
//     volta para antes de D;
//   - bloqueio novo começa em D ou depois.

// Period é o intervalo half-open [From, To) — as noites From … To−1, como toda
// estadia (regra 5 do CLAUDE.md). De 10 a 15 são 5 noites, e o dia 15 fica
// livre para check-in.
type Period struct {
	From calendar.Date
	To   calendar.Date
}

// Nights conta as noites do período.
func (p Period) Nights() int { return p.From.Nights(p.To) }

// Block é o bloqueio da ordem como ele está no banco.
type Block struct {
	Period
	// Active: a linha ainda ocupa o calendário (`stay_blocks.status =
	// 'confirmed'`). Bloqueio liberado (`cancelled`) é false.
	Active bool
}

// ─────────────────────────── Fase ───────────────────────────────────

// Phase é como o bloqueio está HOJE — o `block.phase` da resposta, que a tela
// mostra ("bloqueado até sexta"). Derivada do período e de D, nunca gravada:
// gravada, ficaria errada à meia-noite.
type Phase string

const (
	// Scheduled — ainda não começou: From > D.
	Scheduled Phase = "agendado"
	// Running — cobre a noite de hoje: From ≤ D < To.
	Running Phase = "em_curso"
	// Ended — terminou: To ≤ D. As noites ficam no mapa como história.
	Ended Phase = "encerrado"
	// Released — liberado (`cancelled`): não ocupa nem aparece no mapa.
	Released Phase = "liberado"
)

// PhaseOf classifica o bloqueio no dia `today`.
func PhaseOf(b Block, today calendar.Date) Phase {
	switch {
	case !b.Active:
		return Released
	case !b.To.After(today):
		return Ended
	case b.From.After(today):
		return Scheduled
	default:
		return Running
	}
}

// ─────────────────────────── Liberação ──────────────────────────────

// ReleaseKind é o que fazer com a linha de `stay_blocks`.
type ReleaseKind string

const (
	// Keep — nada: o bloqueio já terminou (To ≤ D) ou já foi liberado.
	Keep ReleaseKind = "manter"
	// Truncate — `period = [From, D)`: está em curso, as noites passadas
	// ficam e a de hoje em diante volta à venda.
	Truncate ReleaseKind = "encurtar"
	// Drop — `status = 'cancelled'`: ainda não começou (From ≥ D), nenhuma
	// noite dele aconteceu.
	Drop ReleaseKind = "liberar"
)

// Release é a decisão sobre a linha. `Period` é o que fica no calendário:
// o mesmo em Keep, `[From, D)` em Truncate e zero em Drop.
type Release struct {
	Kind   ReleaseKind
	Period Period
}

// ReleaseOn decide o que sobra do bloqueio quando a ordem encerra — concluída
// ou cancelada — no dia `today`, ou quando a gestão solta o bloqueio de uma
// ordem ainda aberta (`DELETE /maintenance-orders/{id}/block`).
//
// "Apagar" o bloqueio que não começou é `cancelled`, e não `DELETE`: é o que
// `DELETE /blocks/{id}` já faz, a ordem continua apontando para a linha, e a
// trilha de "esta unidade esteve bloqueada de tal a tal, e foi solta" não some.
//
// O bloqueio que começa HOJE (From = D) é apagado inteiro, e não cortado para
// `[D, D)`: nenhuma noite dele passou, e o período vazio violaria
// `stay_period_valid` (`lower < upper`).
func ReleaseOn(b Block, today calendar.Date) Release {
	switch {
	case !b.Active, !b.To.After(today):
		return Release{Kind: Keep, Period: b.Period}
	case !b.From.Before(today):
		return Release{Kind: Drop}
	default:
		return Release{Kind: Truncate, Period: Period{From: b.From, To: today}}
	}
}

// ─────────────────────────── Remarcação ─────────────────────────────

// ReplanKind é o que fazer com o pedido de período novo.
type ReplanKind string

const (
	// Create — inserir uma linha nova em `stay_blocks` e apontar a ordem para
	// ela. Acontece quando não há bloqueio VIVO: a ordem nunca teve um, ele foi
	// liberado, ou ele já terminou (To ≤ D). A linha antiga, se houver, fica
	// como está — é história do calendário.
	Create ReplanKind = "criar"
	// Change — `UPDATE stay_blocks SET period` na linha atual. A constraint
	// `EXCLUDE` confere a sobreposição no próprio UPDATE (`23P01` →
	// `409 DATE_CONFLICT`); estender sobre uma reserva falha ali, e não num
	// SELECT antes.
	Change ReplanKind = "alterar"
	// NoChange — o período pedido é o atual. Repetir o pedido (o segundo toque
	// no celular) não é erro.
	NoChange ReplanKind = "nada"
)

// Replan decide o pedido de período `next` para o bloqueio da ordem.
// `current` nil quer dizer que a ordem nunca teve bloqueio.
//
// Recusa com VALIDATION_ERROR, campo `from` ou `to` (o que o operador
// corrige):
//
//   - `to` não posterior a `from`, ou mais de `calendar.MaxBlockNights` noites;
//   - `from` além de hoje + `calendar.BlockHorizonDays`;
//   - bloqueio novo, ou vivo que ainda não começou, com `from` antes de hoje:
//     noite que já passou não se vende — nem se bloqueia;
//   - bloqueio em curso com `from` diferente do atual, ou `to` antes de hoje:
//     as noites que já passaram bloqueadas não se desfazem.
//
// O teto é o MESMO de `POST /blocks`: dois tetos para a mesma ocupação fariam
// da ordem a porta dos fundos do `/blocks`.
func Replan(current *Block, next Period, today calendar.Date) (ReplanKind, error) {
	if err := checkWindow(next, today); err != nil {
		return "", err
	}

	live := current != nil && current.Active && current.To.After(today)
	if !live {
		if next.From.Before(today) {
			return "", periodError("from", fmt.Sprintf(
				"o bloqueio começa hoje (%s) ou depois: noite que já passou não se vende, nem se bloqueia.", today))
		}
		return Create, nil
	}

	if current.From.Before(today) {
		// Em curso: o passado do bloqueio é [current.From, today), e ele não
		// muda — nem o início, nem um fim que volte para antes de hoje.
		if next.From != current.From {
			return "", periodError("from", fmt.Sprintf(
				"o bloqueio está em curso desde %s: o início não muda. Ajuste só o fim.", current.From))
		}
		if next.To.Before(today) {
			return "", periodError("to", fmt.Sprintf(
				"o fim não pode ficar antes de hoje (%s): as noites que já passaram bloqueadas não se desfazem.", today))
		}
	} else if next.From.Before(today) {
		return "", periodError("from", fmt.Sprintf(
			"o bloqueio começa hoje (%s) ou depois: noite que já passou não se vende, nem se bloqueia.", today))
	}

	if next == current.Period {
		return NoChange, nil
	}
	return Change, nil
}

// checkWindow aplica os dois tetos de `calendar` ao período pedido.
func checkWindow(p Period, today calendar.Date) error {
	switch n := p.Nights(); {
	case n <= 0:
		return periodError("to", "deve ser posterior a `from` (o intervalo é half-open: `to` não entra).")
	case n > calendar.MaxBlockNights:
		return periodError("to", fmt.Sprintf(
			"o bloqueio cobre %d noites; o máximo é %d (um ano). Parta em bloqueios menores, "+
				"ou desative a unidade no inventário se ela sai do catálogo.", n, calendar.MaxBlockNights))
	}
	if limit := today.AddDays(calendar.BlockHorizonDays); p.From.After(limit) {
		return periodError("from", fmt.Sprintf(
			"o bloqueio começa em %s, além do horizonte de %s (%d dias).", p.From, limit, calendar.BlockHorizonDays))
	}
	return nil
}

func periodError(field, msg string) *RuleError {
	return &RuleError{
		Code:    codeValidation,
		Message: "Período de bloqueio inválido.",
		Details: map[string]any{field: msg},
	}
}
