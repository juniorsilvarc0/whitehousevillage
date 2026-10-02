package reservas

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/idempotencia"
)

// As ações que movem a reserva pela máquina de estados.
//
// Todas seguem a mesma forma: abre transação, TRAVA a reserva (`FOR UPDATE`),
// confere a transição, move os blocos do calendário, muda o status e grava o
// evento. A trava é o que impede dois cliques simultâneos no mesmo botão de
// virarem dois pagamentos ou dois cancelamentos.

// ─────────────────────────── POST /confirm ──────────────────────────

// Confirmar registra o sinal e leva a reserva de `hold` a `confirmed`.
//
// O que muda no calendário: os blocos passam de `hold` para `confirmed` e
// perdem o prazo. A data continua bloqueada — o que ela deixa de ter é validade.
func (s *Servico) Confirmar(ctx context.Context, id uuid.UUID, chave string, corpo ConfirmacaoDeReserva) (Resultado, error) {
	u, err := ator(ctx)
	if err != nil {
		return Resultado{}, err
	}
	hash, err := idempotencia.Impressao(corpo)
	if err != nil {
		return Resultado{}, err
	}
	rota := "POST /reservations/" + id.String() + "/confirm"

	var saida Resultado
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		guardada, err := idempotencia.Reservar(ctx, s.repo.pool, chave, rota, hash, idempotencia.DonoDe(u))
		if err != nil {
			return err
		}
		if guardada != nil {
			saida = repetir(guardada)
			return nil
		}

		e, err := s.repo.TravarReserva(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		if e.Status != EstadoHold {
			return transicaoInvalida(e.Status, OrigensDe(EstadoConfirmada))
		}
		// Hold vencido não se confirma por cima: as unidades já podem ter sido
		// liberadas pelo job, e confirmar seria vender uma data que talvez já
		// tenha outro dono.
		if e.HoldVencido {
			return apperr.HoldExpired.WithDetails(map[string]any{"hold_expires_at": e.HoldExpiraEm})
		}

		// `deposit_paid_cents` ausente significa "recebeu o sinal cheio".
		// É este valor que a política de cancelamento usa depois como base — por
		// isso ele é gravado sempre, mesmo quando igual ao teórico.
		pago := e.Sinal
		if corpo.SinalPago != nil {
			pago = *corpo.SinalPago
		}
		// A FAIXA DO SINAL — o ALTO 2 da revisão.
		//
		// MEDIDO ANTES: reserva de total_cents=192000 aceitou
		// `{"deposit_paid_cents": 96000000}` com 200, e o /cancel seguinte
		// devolveu `refund_cents: 96000000` — R$ 960.000 de devolução numa venda
		// de R$ 1.920, gravados em `reservation_events`, com o razão
		// concordando. Um dígito a mais numa tela, ou um insider, e a saída de
		// caixa está autorizada por um documento que o próprio sistema emitiu.
		//
		// O teto é o TOTAL, e não o sinal: pagar a estadia inteira adiantada é
		// legítimo e acontece. O piso é 1 centavo: confirmar sem dinheiro nenhum
		// é o que o `hold` já faz, e com prazo.
		//
		// A validação vive AQUI, e não no DTO, porque o teto é `total_cents` —
		// número que só existe com a reserva travada em mãos. E vem ANTES de
		// qualquer escrita: um 422 não pode deixar a reserva meio confirmada.
		if pago < 1 || pago > e.Total {
			return apperr.Validation(map[string]string{
				"deposit_paid_cents": fmt.Sprintf(
					"deve estar entre 1 e %d (o total da reserva); veio %d.", e.Total, pago),
			})
		}

		promovidos, err := s.repo.MoverBlocosDaReserva(ctx, id, []string{BlocoHold}, BlocoConfirmado)
		if err != nil {
			return err
		}
		if promovidos == 0 {
			// A reserva diz `hold` mas não segura nada. Ou o job expirou os
			// blocos, ou alguém mexeu por fora — em qualquer dos dois, a data
			// não está garantida e confirmar seria mentir.
			return apperr.HoldExpired.WithDetails(map[string]any{
				"hint": "a pré-reserva não segura mais nenhuma unidade.",
			})
		}

		agora := time.Now()
		if err := s.repo.AtualizarStatus(ctx, id, EstadoConfirmada, &agora, nil, nil, true); err != nil {
			return err
		}

		payload := map[string]any{"deposit_paid_cents": pago, "blocks_promoted": promovidos}
		if corpo.Meio != "" {
			payload["method"] = corpo.Meio
		}
		if corpo.PagoEm != nil {
			payload["paid_at"] = *corpo.PagoEm
		}
		if corpo.ReferExt != nil {
			payload["external_ref"] = *corpo.ReferExt
		}
		if corpo.Observacoes != nil {
			payload["note"] = *corpo.Observacoes
		}
		if err := s.repo.InserirEvento(ctx, id, EventoConfirmada, payload, &u.ID); err != nil {
			return err
		}
		if err := s.auditarReserva(ctx, VerboConfirmada, id,
			instantaneo(e),
			instantaneo(e).comStatus(EstadoConfirmada).comSinalPago(pago)); err != nil {
			return err
		}

		reserva, err := s.repo.Buscar(ctx, u.PropertyID, id, false, u.ID)
		if err != nil {
			return err
		}
		env := envelope{Data: reserva}
		if err := idempotencia.Guardar(ctx, s.repo.pool, chave, rota, idempotencia.DonoDe(u), http.StatusOK, env); err != nil {
			return err
		}
		saida = Resultado{Status: http.StatusOK, Corpo: env}
		return nil
	})
	return saida, err
}

// ─────────────────────────── POST /cancel ───────────────────────────

// Cancelar aplica a faixa da política CONGELADA na reserva.
//
// `dryRun` calcula e devolve o mesmo corpo sem executar nada — é o que a tela
// mostra antes de a gestão confirmar, e é por isso que a simulação e a execução
// compartilham exatamente o mesmo cálculo: dois caminhos separados divergiriam
// no dia em que só um fosse ajustado.
func (s *Servico) Cancelar(ctx context.Context, id uuid.UUID, dryRun bool, corpo PedidoDeCancelamento) (ResultadoDeCancelamento, error) {
	u, err := ator(ctx)
	if err != nil {
		return ResultadoDeCancelamento{}, err
	}

	var out ResultadoDeCancelamento
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		e, err := s.repo.TravarReserva(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		if !Cancelavel(e.Status) {
			return NaoCancelavel.WithDetails(map[string]any{
				"status":  e.Status,
				"allowed": EstadosCancelaveis(),
			})
		}

		destino := EstadoCancelada
		if corpo.Motivo == MotivoNoShow {
			// O estado é separado porque o BI precisa distinguir quem avisou de
			// quem não apareceu — as duas linhas não são a mesma coisa.
			destino = EstadoNoShow
		}
		if !PodeTransitar(e.Status, destino) {
			return transicaoInvalida(e.Status, DestinosDe(e.Status))
		}

		resultado, err := s.calcularCancelamento(ctx, e, corpo.Motivo, dryRun, destino)
		if err != nil {
			return err
		}
		out = resultado
		if dryRun {
			return nil
		}

		// Libera a data e preserva o histórico: `cancelled` e `completed` estão
		// os dois fora do WHERE da constraint, então a unidade volta ao estoque
		// sem a linha sumir. QUAL dos dois depende de o hóspede ter dormido ou
		// não — é o BAIXO da segunda revisão.
		//
		// MEDIDO ANTES: SP-02 de 26 a 30/08, confirmada, CHECK-IN FEITO, e um
		// /cancel mandava os blocos para `cancelled`. O dinheiro saía certo
		// (retenção integral), mas a noite já dormida ficava byte a byte igual a
		// uma venda que nunca existiu: sumia do mapa (`hold|confirmed|completed`
		// não a alcançava mais), da ocupação, do ADR e do RevPAR.
		//
		// É a MESMA família do check-out, e a mesma resposta: `completed`. O
		// rótulo comercial da reserva continua `cancelled` — a venda de fato
		// terminou em cancelamento —, mas o CALENDÁRIO registra o fato físico,
		// que é outro: a unidade foi ocupada. As duas verdades convivem porque
		// moram em tabelas diferentes, e confundi-las foi o defeito.
		destinoDoBloco := BlocoCancelado
		if e.Status == EstadoCheckIn {
			destinoDoBloco = BlocoConcluido
		}
		liberados, err := s.repo.MoverBlocosDaReserva(ctx, id, statusQueBloqueiam, destinoDoBloco)
		if err != nil {
			return err
		}

		agora := time.Now()
		motivo := corpo.Motivo
		var motivoPtr *string
		if motivo != "" {
			motivoPtr = &motivo
		}
		if err := s.repo.AtualizarStatus(ctx, id, destino, nil, &agora, motivoPtr, true); err != nil {
			return err
		}

		tipo := EventoCancelada
		if destino == EstadoNoShow {
			tipo = EventoNoShow
		}
		// A trilha guarda o SINAL DEVOLVIDO no `depois`. É a pergunta que a
		// revisão fez e que ninguém conseguia responder: quem autorizou esta
		// devolução, de que estado a reserva saiu, e de onde veio a requisição.
		if err := s.auditarReserva(ctx, VerboCancelada, id,
			instantaneo(e),
			instantaneo(e).comStatus(destino).comSinalPago(resultado.Devolucao)); err != nil {
			return err
		}
		return s.repo.InserirEvento(ctx, id, tipo, map[string]any{
			"reason":             motivo,
			"refund_cents":       resultado.Devolucao,
			"retained_cents":     resultado.Retido,
			"deposit_paid_cents": resultado.SinalPago,
			"days_before":        resultado.Antecedencia,
			"policy_version":     resultado.PolicyVersion,
			"tier":               resultado.Rotulo,
			"released_blocks":    liberados,
			"block_status":       destinoDoBloco,
			// O crédito em aberto vai para a TIMELINE junto com a devolução:
			// sem ele, a linha do cancelamento afirma que a conta fechou.
			"credit_cents": resultado.Credito,
		}, &u.ID)
	})
	return out, err
}

// simular é o cálculo do cancelamento sem qualquer efeito — alimenta o
// `cancellation_preview` do /full.
func (s *Servico) simular(ctx context.Context, id uuid.UUID, motivo string) (ResultadoDeCancelamento, error) {
	u, err := ator(ctx)
	if err != nil {
		return ResultadoDeCancelamento{}, err
	}
	// Leitura sem trava: a prévia é informativa e não pode segurar a linha.
	e, err := s.repo.LerEstado(ctx, u.PropertyID, id, false, u.ID)
	if err != nil {
		return ResultadoDeCancelamento{}, err
	}
	return s.calcularCancelamento(ctx, e, motivo, true, EstadoCancelada)
}

// calcularCancelamento roda o motor sobre a política congelada.
func (s *Servico) calcularCancelamento(ctx context.Context, e Estado, motivo string, simulado bool, destino string) (ResultadoDeCancelamento, error) {
	if e.CancelPolicyID == nil {
		// Sem política congelada não há como decidir a devolução, e escolher a
		// vigente de hoje seria aplicar à venda de ontem uma regra que ela
		// nunca aceitou.
		return ResultadoDeCancelamento{}, apperr.NotFound("Política de cancelamento congelada na reserva")
	}
	politica, err := s.repo.PoliticaDeCancelamento(ctx, *e.CancelPolicyID)
	if err != nil {
		return ResultadoDeCancelamento{}, err
	}

	// Base da devolução: o sinal EFETIVAMENTE recebido, que vive em
	// reservation_events. Reserva nunca confirmada não pagou nada — devolver
	// `deposit_cents` ali seria devolver dinheiro que nunca entrou.
	base := int64(0)
	pago, err := s.repo.SinalPago(ctx, e.ID)
	if err != nil {
		return ResultadoDeCancelamento{}, err
	}
	switch {
	case pago != nil:
		base = *pago
	case e.ConfirmadaEm != nil:
		base = e.Sinal
	}
	// TETO NA BASE, mesmo com o /confirm já validando a faixa. As duas guardas
	// não são redundância inútil: o `Confirmar` protege as confirmações NOVAS, e
	// esta protege o que já está gravado — as linhas de `reservation_events`
	// escritas antes desta rodada seguem no banco, com valores que passaram sem
	// teto nenhum. Uma delas viraria ordem de devolução no primeiro /cancel.
	// Devolução nunca pode passar do total da venda.
	//
	// O QUE MUDOU: o truncamento não pode mais ser SILENCIOSO. Era aqui que o
	// excedente da remarcação evaporava — a revisão mediu R$ 8.300,00 pagos,
	// R$ 2.250,00 devolvidos e `retained_cents: 0`, com o sistema declarando a
	// conta encerrada. O que sobra do teto vira crédito EM ABERTO, informado no
	// resultado e gravado no evento de cancelamento.
	sobra := int64(0)
	if base > e.Total {
		sobra = base - e.Total
		base = e.Total
	}
	if base < 0 {
		base = 0
	}

	// Crédito em aberto = o que já foi registrado como crédito nesta reserva
	// (o excedente que a remarcação converteu) + o que esta base acabou de
	// truncar (linha antiga, gravada antes do teto do /confirm existir).
	credito, err := s.repo.CreditoRegistrado(ctx, e.ID)
	if err != nil {
		return ResultadoDeCancelamento{}, err
	}
	credito += sobra

	// Antecedência para a FAIXA: no `no_show` é sempre a menor (retenção
	// integral), porque quem não apareceu não avisou com antecedência nenhuma —
	// mesmo que a operação registre o no-show antes da data.
	diasDaFaixa := e.Antecedencia
	if destino == EstadoNoShow && diasDaFaixa > 0 {
		diasDaFaixa = 0
	}

	resultado := politica.Simulate(money.Cents(base), diasDaFaixa)
	versao := politica.Version

	return ResultadoDeCancelamento{
		Rotulo:    resultado.Label,
		Devolucao: centavos(resultado.Refund),
		Retido:    centavos(resultado.Retained),
		SinalPago: base,
		// A antecedência devolvida é a REAL, não a usada na faixa: a tela mostra
		// o fato, e o `label` já explica a regra aplicada.
		Antecedencia:  e.Antecedencia,
		PolicyVersion: versao,
		Simulado:      simulado,
		Status:        destino,
		Credito:       credito,
	}, nil
}

// ─────────────────────────── POST /reschedule ───────────────────────

// Remarcar preserva o histórico: a original vai para `cancelled` com motivo
// `remarcacao` e nasce uma reserva NOVA apontando para ela por
// `rebooked_from_id`.
//
// As duas coisas acontecem na MESMA transação, e é isso que garante o contrato:
// o DATE_CONFLICT da data nova não deixa a reserva antiga cancelada. Ou as duas
// mudanças valem, ou nenhuma vale.
//
// A ordem também importa: os blocos antigos são soltos ANTES de os novos serem
// inseridos. Sem isso, remarcar para um período que se sobrepõe ao atual faria a
// reserva colidir consigo mesma.
func (s *Servico) Remarcar(ctx context.Context, id uuid.UUID, chave string, corpo PedidoDeRemarcacao) (Resultado, error) {
	u, err := ator(ctx)
	if err != nil {
		return Resultado{}, err
	}
	hash, err := idempotencia.Impressao(corpo)
	if err != nil {
		return Resultado{}, err
	}
	rota := "POST /reservations/" + id.String() + "/reschedule"

	var saida Resultado
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		guardada, err := idempotencia.Reservar(ctx, s.repo.pool, chave, rota, hash, idempotencia.DonoDe(u))
		if err != nil {
			return err
		}
		if guardada != nil {
			saida = repetir(guardada)
			return nil
		}

		e, err := s.repo.TravarReserva(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		if e.Status != EstadoHold && e.Status != EstadoConfirmada {
			return transicaoInvalida(e.Status, []string{EstadoHold, EstadoConfirmada})
		}

		// O dinheiro do hóspede acompanha a reserva nova quando ela nasce
		// confirmada — senão o hóspede que pagou 50% viraria um hóspede que
		// nunca pagou nada.
		//
		// O QUE ACOMPANHA É O SALDO DELE COM A CASA, e não só o sinal aplicado:
		// sinal aplicado + crédito em aberto. Sem somar o crédito, a segunda
		// remarcação da cadeia perdia de novo o que a primeira tinha salvo —
		// A (paga 358.000) → B (103.000 aplicados + 255.000 de crédito) → C
		// herdaria 103.000 e os 255.000 ficariam órfãos numa reserva cancelada.
		// Somando, C consome o crédito quando a estadia nova é mais cara, e o
		// que sobrar vira crédito de novo.
		var sinalHerdado *int64
		if e.Status == EstadoConfirmada {
			pago, err := s.repo.SinalPago(ctx, id)
			if err != nil {
				return err
			}
			valor := e.Sinal
			if pago != nil {
				valor = *pago
			}
			credito, err := s.repo.CreditoRegistrado(ctx, id)
			if err != nil {
				return err
			}
			valor += credito
			sinalHerdado = &valor
		}

		// 1. Solta o calendário da original.
		if _, err := s.repo.MoverBlocosDaReserva(ctx, id, statusQueBloqueiam, BlocoCancelado); err != nil {
			return err
		}
		agora := time.Now()
		motivo := MotivoRemarcacao
		if err := s.repo.AtualizarStatus(ctx, id, EstadoCancelada, nil, &agora, &motivo, true); err != nil {
			return err
		}

		// 2. Emite a nova, recalculada do zero com a tabela e a política
		//    VIGENTES HOJE — remarcar é uma venda nova, não uma edição de preço.
		pedido := ReservaCriar{
			UnitTypeID:  e.UnitTypeID,
			CheckIn:     corpo.CheckIn,
			CheckOut:    corpo.CheckOut,
			Hospedes:    e.Hospedes,
			DescontoPct: e.DescontoPct,
			IsEvento:    e.IsEvento,
			TipoEvento:  e.TipoEvento,
			ContactID:   e.ContactID,
			BrokerID:    e.BrokerID,
			Origem:      e.Origem,
			Observacoes: e.Observacoes,
		}
		if corpo.UnitTypeID != nil {
			pedido.UnitTypeID = *corpo.UnitTypeID
		}
		if corpo.Hospedes != nil {
			pedido.Hospedes = *corpo.Hospedes
		}
		if corpo.DescontoPct != nil {
			pedido.DescontoPct = *corpo.DescontoPct
		}

		nova, credito, err := s.emitir(ctx, u, pedido, e.Status, &e.ID, sinalHerdado)
		if err != nil {
			return err
		}

		if err := s.repo.InserirEvento(ctx, id, EventoRemarcada, map[string]any{
			"to_reservation_id": nova.ID.String(),
			"to_code":           nova.Codigo,
			"reason":            textoOu(corpo.Motivo, MotivoRemarcacao),
			// O crédito aparece nas DUAS timelines: quem abre a reserva antiga
			// para entender por que o hóspede reclama do valor precisa ver aqui
			// que sobrou dinheiro dele na remarcação.
			"credit_cents": credito,
		}, &u.ID); err != nil {
			return err
		}
		if err := s.repo.InserirEvento(ctx, nova.ID, EventoRemarcada, map[string]any{
			"from_reservation_id": e.ID.String(),
			"from_code":           e.Codigo,
			"difference_cents":    nova.Total - e.Total,
			"credit_cents":        credito,
		}, &u.ID); err != nil {
			return err
		}
		// A reserva NOVA já foi auditada como criação dentro de `emitir`; o que
		// falta é a trilha da ORIGINAL, que saiu de cena — sem ela, `audit_log`
		// mostraria uma reserva nascendo e outra sumindo sem ligação nenhuma.
		if err := s.auditarReserva(ctx, VerboRemarcada, e.ID,
			instantaneo(e),
			reservaAuditada{
				Codigo: e.Codigo, Status: EstadoCancelada,
				CheckIn: pedido.CheckIn, CheckOut: pedido.CheckOut,
				Hospedes: pedido.Hospedes, Total: nova.Total,
			}); err != nil {
			return err
		}

		env := envelope{Data: nova, Meta: MetaDaRemarcacao{
			ReservaAnteriorID: e.ID,
			CodigoAnterior:    e.Codigo,
			TotalAnterior:     e.Total,
			Diferenca:         nova.Total - e.Total,
			Credito:           credito,
		}}
		if err := idempotencia.Guardar(ctx, s.repo.pool, chave, rota, idempotencia.DonoDe(u), http.StatusCreated, env); err != nil {
			return err
		}
		saida = Resultado{Status: http.StatusCreated, Corpo: env, Local: localDaReserva(nova.ID)}
		return nil
	})
	return saida, err
}

// ─────────────────────────── Check-in / check-out ───────────────────

// RegistrarCheckIn leva `confirmed → checked_in`.
//
// Os blocos CONTINUAM `confirmed`: o hóspede está dentro, a data segue
// bloqueada. O que muda é o estado comercial.
func (s *Servico) RegistrarCheckIn(ctx context.Context, id uuid.UUID, corpo RegistroDeEstadia) (Reserva, error) {
	return s.registrarEstadia(ctx, id, corpo, EstadoConfirmada, EstadoCheckIn, EventoCheckIn, VerboCheckIn, "")
}

// RegistrarCheckOut leva `checked_in → checked_out` e devolve a data ao estoque
// SEM apagar a estadia.
//
// Como o intervalo é half-open, a noite do check-out nunca foi vendida a este
// hóspede — a data já estava livre para um back-to-back. O que o fechamento do
// bloco resolve é a saída ANTECIPADA: as noites restantes voltam ao estoque.
//
// O bloco vai para `completed`, e não para `cancelled`. Os dois liberam a data
// (nenhum está no `WHERE` da constraint), mas só um diz a verdade: com
// `cancelled`, a revisão mediu o mapa devolvendo os dias da estadia CUMPRIDA
// como `livre`, `reservation_code: null` — e a linha ficava byte a byte igual à
// de uma venda que o hóspede cancelou sem nunca chegar. Ocupação, ADR e RevPAR
// saem daí.
func (s *Servico) RegistrarCheckOut(ctx context.Context, id uuid.UUID, corpo RegistroDeEstadia) (Reserva, error) {
	return s.registrarEstadia(ctx, id, corpo, EstadoCheckIn, EstadoCheckOut, EventoCheckOut, VerboCheckOut, BlocoConcluido)
}

// registrarEstadia é o miolo das duas rotas. `destinoDoBloco` vazio significa
// "não mexe no calendário" (é o check-in).
func (s *Servico) registrarEstadia(ctx context.Context, id uuid.UUID, corpo RegistroDeEstadia, origem, destino, evento, verbo, destinoDoBloco string) (Reserva, error) {
	u, err := ator(ctx)
	if err != nil {
		return Reserva{}, err
	}

	var out Reserva
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		e, err := s.repo.TravarReserva(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		if e.Status != origem {
			return transicaoInvalida(e.Status, []string{origem})
		}

		instante, err := instanteDoRegistro(corpo)
		if err != nil {
			return err
		}
		if err := s.conferirInstanteDaEstadia(ctx, u.PropertyID, e, instante, destino); err != nil {
			return err
		}

		payload := map[string]any{}
		if destinoDoBloco != "" {
			movidos, err := s.repo.MoverBlocosDaReserva(ctx, id, statusQueBloqueiam, destinoDoBloco)
			if err != nil {
				return err
			}
			payload["released_blocks"] = movidos
			payload["block_status"] = destinoDoBloco
		}
		if err := s.repo.AtualizarStatus(ctx, id, destino, nil, nil, nil, false); err != nil {
			return err
		}

		// O instante vai para a TIMELINE, não para uma coluna de `reservations`:
		// a tabela já nasceu acima do teto de colunas do db.md, e um evento
		// append-only registra melhor o "quando" do que uma coluna que alguém
		// pode sobrescrever.
		if corpo.Instante != nil {
			payload["at"] = *corpo.Instante
		}
		if corpo.Observacao != nil {
			payload["note"] = *corpo.Observacao
		}
		if err := s.repo.InserirEvento(ctx, id, evento, payload, &u.ID); err != nil {
			return err
		}
		if err := s.auditarReserva(ctx, verbo, id, instantaneo(e), instantaneo(e).comStatus(destino)); err != nil {
			return err
		}

		out, err = s.repo.Buscar(ctx, u.PropertyID, id, false, u.ID)
		return err
	})
	return out, err
}

// instanteDoRegistro traduz o `at` do corpo. Nulo é "agora" — e "agora" também
// é conferido: foi por aí que o defeito passou.
func instanteDoRegistro(corpo RegistroDeEstadia) (*time.Time, error) {
	if corpo.Instante == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *corpo.Instante)
	if err != nil {
		// O DTO já validou o formato; chegar aqui com erro é defesa em
		// profundidade, não caminho esperado.
		return nil, apperr.Validation(map[string]string{"at": "instante inválido: use RFC 3339."})
	}
	return &t, nil
}

// conferirInstanteDaEstadia recusa entrada e saída fora do período vendido — o
// MÉDIO 8 da revisão.
//
// MEDIDO ANTES: reserva de 2031, `POST /check-in` HOJE (2026) → 200, e o
// `/check-out` logo em seguida → 200. Dois cliques devolveram ao estoque uma
// venda confirmada e futura sem passar por /cancel: sem política de
// cancelamento, sem retenção, sem registro de cancelamento no razão. Uma
// estadia registrou `checked_in` com `at: 2019-01-01`, sete anos antes da venda.
//
// OS DOIS TETOS SÃO DIFERENTES, e a diferença não é descuido:
//
//   - check-in vale em `[check_in, check_out)` — teto ESTRITO. A diária do dia
//     do check-out não foi vendida a este hóspede; entrar nela seria ocupar
//     noite de outra pessoa.
//   - check-out vale em `[check_in, check_out]` — teto INCLUSIVO. Half-open
//     conta NOITES VENDIDAS, não pessoas na porta: o hóspede sai na manhã do
//     primeiro dia que não é dele, e essa manhã é `check_out`.
//
// A comparação é por DIA no fuso da PROPRIEDADE (`DataLocal`), não por instante
// em UTC: às 22h de Fortaleza o dia UTC já virou, e um check-out legítimo às
// 23h do último dia seria recusado por um fuso que não é o da casa.
func (s *Servico) conferirInstanteDaEstadia(ctx context.Context, propriedade uuid.UUID, e Estado, instante *time.Time, destino string) error {
	dia, err := s.repo.DataLocal(ctx, propriedade, instante)
	if err != nil {
		return err
	}
	entrada, err := calendar.Parse(e.CheckIn)
	if err != nil {
		return apperr.Internal.WithCause(err)
	}
	saida, err := calendar.Parse(e.CheckOut)
	if err != nil {
		return apperr.Internal.WithCause(err)
	}

	if destino == EstadoCheckOut {
		if dia.Before(entrada) || saida.Before(dia) {
			return foraDoPeriodo("check-out", dia, e, "]")
		}
		// Saída antes da entrada é impossível, e o período sozinho não pega:
		// entrada no dia 3 e saída no dia 1 estão as duas dentro da estadia.
		anterior, err := s.repo.InstanteDoEvento(ctx, e.ID, EventoCheckIn)
		if err != nil {
			return err
		}
		efetivo := time.Now()
		if instante != nil {
			efetivo = *instante
		}
		if anterior != nil && efetivo.Before(*anterior) {
			return apperr.Validation(map[string]string{
				"at": "o check-out não pode ser anterior ao check-in registrado (" +
					anterior.Format(time.RFC3339) + ").",
			})
		}
		return nil
	}

	if dia.Before(entrada) || !dia.Before(saida) {
		return foraDoPeriodo("check-in", dia, e, ")")
	}
	return nil
}

// foraDoPeriodo monta o 422. É VALIDATION_ERROR e não um código novo porque,
// para o painel, isto é erro de CAMPO: a tela marca o `at` e o operador
// corrige. Código próprio só se justifica quando a ação do usuário é outra.
func foraDoPeriodo(acao string, dia calendar.Date, e Estado, fecha string) error {
	return apperr.Validation(map[string]string{
		"at": fmt.Sprintf("%s em %s está fora do período da reserva [%s, %s%s.",
			acao, dia, e.CheckIn, e.CheckOut, fecha),
	})
}

// ─────────────────────────── POST /reassign-unit ────────────────────

// Realocar troca a unidade física sem mexer em datas nem em preço — é o que
// salva um conflito de canal sem cancelar ninguém.
func (s *Servico) Realocar(ctx context.Context, id uuid.UUID, corpo PedidoDeRealocacao) (Reserva, error) {
	u, err := ator(ctx)
	if err != nil {
		return Reserva{}, err
	}

	var out Reserva
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		e, err := s.repo.TravarReserva(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		// Reserva que não bloqueia calendário não tem unidade para trocar.
		if !BloqueiaCalendario(e.Status) {
			return transicaoInvalida(e.Status, []string{EstadoHold, EstadoConfirmada, EstadoCheckIn})
		}
		// Hold vencido não realoca — o BAIXO 9 da revisão. `Confirmar` e
		// `EstenderHold` já recusavam com HOLD_EXPIRED; esta não, e a
		// consequência medida foi um hold vencido migrando de AP-01 para AP-02 e
		// voltando a bloquear o calendário: uma pré-reserva morta ressuscitada
		// por uma ação que nem devia enxergá-la. Ou as três ações concordam
		// sobre o que é um hold vivo, ou "vencido" não quer dizer nada.
		if e.HoldVencido {
			return apperr.HoldExpired.WithDetails(map[string]any{"hold_expires_at": e.HoldExpiraEm})
		}
		// A Completa já ocupa todas as unidades: não há para onde mover.
		if e.Consome == ConsomeTodosMembros {
			return EstadoInvalido.
				WithMessage("Produto que consome todas as unidades não realoca: ele já ocupa a casa inteira.").
				WithDetails(map[string]any{"status": e.Status, "consumes": e.Consome})
		}

		codigoDestino, err := s.repo.ConferirUnidadeDoProduto(ctx, e.UnitTypeID, corpo.UnidadeDeDestino)
		if err != nil {
			return err
		}

		atuais, err := s.repo.UnidadesAtuais(ctx, id)
		if err != nil {
			return err
		}
		origem, err := unidadeDeOrigem(atuais, corpo.UnidadeDeOrigem)
		if err != nil {
			return err
		}

		travar := corpo.Travar.Ou(true) // o default do contrato é `true`
		if err := s.repo.Realocar(ctx, e, origem.ID, corpo.UnidadeDeDestino, codigoDestino, travar, &u.ID); err != nil {
			return err
		}
		if err := s.repo.InserirEvento(ctx, id, EventoUnidadeTrocada, map[string]any{
			"from_unit_id":   origem.ID.String(),
			"from_unit_code": origem.Codigo,
			"to_unit_id":     corpo.UnidadeDeDestino.String(),
			"to_unit_code":   codigoDestino,
			"locked":         travar,
			"reason":         textoOu(corpo.Motivo, ""),
		}, &u.ID); err != nil {
			return err
		}
		if err := s.auditarReserva(ctx, VerboRealocada, id,
			instantaneo(e).comUnidades(origem.Codigo),
			instantaneo(e).comUnidades(codigoDestino)); err != nil {
			return err
		}

		out, err = s.repo.Buscar(ctx, u.PropertyID, id, false, u.ID)
		return err
	})
	return out, err
}

// unidadeDeOrigem resolve qual unidade sai. Omitir `from_unit_id` só é legítimo
// quando a reserva ocupa uma só unidade — com duas, adivinhar seria escolher
// pelo hóspede.
func unidadeDeOrigem(atuais []unidadeDaComposicao, pedida *uuid.UUID) (unidadeDaComposicao, error) {
	if pedida != nil {
		for _, u := range atuais {
			if u.ID == *pedida {
				return u, nil
			}
		}
		return unidadeDaComposicao{}, apperr.Validation(map[string]string{
			"from_unit_id": "esta unidade não está alocada nesta reserva.",
		})
	}
	if len(atuais) == 1 {
		return atuais[0], nil
	}
	if len(atuais) == 0 {
		return unidadeDaComposicao{}, apperr.Validation(map[string]string{
			"from_unit_id": "a reserva não tem unidade alocada.",
		})
	}
	return unidadeDaComposicao{}, apperr.Validation(map[string]string{
		"from_unit_id": "a reserva ocupa mais de uma unidade; informe qual sai.",
	})
}

// ─────────────────────────── POST /extend-hold ──────────────────────

// EstenderHold empurra o prazo da pré-reserva. Ação EXPLÍCITA e AUDITADA
// (spec §5), nunca automática: pré-reserva que se renova sozinha é data morta no
// calendário.
func (s *Servico) EstenderHold(ctx context.Context, id uuid.UUID, corpo PedidoDeExtensaoDeHold) (ResultadoDeExtensaoDeHold, error) {
	u, err := ator(ctx)
	if err != nil {
		return ResultadoDeExtensaoDeHold{}, err
	}

	var out ResultadoDeExtensaoDeHold
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		e, err := s.repo.TravarReserva(ctx, u.PropertyID, id, somenteMinhas(ctx, auth.AcaoEditar), u.ID)
		if err != nil {
			return err
		}
		if e.Status != EstadoHold {
			return transicaoInvalida(e.Status, []string{EstadoHold})
		}
		if e.HoldVencido {
			// Prazo vencido não se conserta para trás: as unidades já foram
			// liberadas e a data pode ter dono novo.
			return apperr.HoldExpired.WithDetails(map[string]any{"hold_expires_at": e.HoldExpiraEm})
		}

		// A política é a CONGELADA na reserva, não a vigente hoje: mudar o
		// limite amanhã não reabre a pré-reserva que já esgotou o dela.
		comercial, err := s.repo.ContextoComercial(ctx, u.PropertyID, e.PolicyVersion)
		if err != nil {
			return err
		}

		// Quantas extensões já houve sai da CONTAGEM dos eventos, não de uma
		// coluna: o log append-only já é a verdade sobre o fato.
		feitas, err := s.repo.ContarEventos(ctx, id, EventoHoldEstendido)
		if err != nil {
			return err
		}
		if feitas >= comercial.HoldMaxExtensoes {
			return LimiteDeHold.WithDetails(map[string]any{
				"max_extensions":   comercial.HoldMaxExtensoes,
				"extensions_count": feitas,
			})
		}

		horas := comercial.HoldExtensaoHoras
		if corpo.Horas != nil {
			horas = *corpo.Horas
		}
		novo, err := s.repo.EstenderHold(ctx, id, horas)
		if err != nil {
			return err
		}
		if err := s.repo.InserirEvento(ctx, id, EventoHoldEstendido, map[string]any{
			"hours":           horas,
			"hold_expires_at": novo,
			"reason":          textoOu(corpo.Motivo, ""),
		}, &u.ID); err != nil {
			return err
		}
		depois := instantaneo(e)
		depois.HoldExpiraEm = &novo
		if err := s.auditarReserva(ctx, VerboHoldEstendido, id, instantaneo(e), depois); err != nil {
			return err
		}

		out = ResultadoDeExtensaoDeHold{
			ID:           id,
			HoldExpiraEm: novo,
			Extensoes:    feitas + 1,
			MaxExtensoes: comercial.HoldMaxExtensoes,
		}
		return nil
	})
	return out, err
}

// ─────────────────────────── Bloqueio operacional ───────────────────

// CriarBloqueio grava a ocupação sem reserva — manutenção ou uso do
// proprietário. Vive na MESMA tabela `stay_blocks`: é o que garante que uma
// manutenção impeça uma venda pela mesma constraint que impede duas vendas.
//
// A linha nasce COM DONO (`owner_id = quem criou`). Sem essa coluna preenchida,
// o escopo `own` de `calendar` não tem eixo por onde filtrar e degrada
// silenciosamente para `all` — que foi o achado MÉDIO 5.
func (s *Servico) CriarBloqueio(ctx context.Context, corpo BloqueioCriar) ([]Bloqueio, error) {
	u, err := ator(ctx)
	if err != nil {
		return nil, err
	}

	var out []Bloqueio
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.conferirHorizonteDoBloqueio(ctx, u.PropertyID, corpo); err != nil {
			return err
		}
		out, err = s.repo.CriarBloqueioOperacional(ctx, u.PropertyID, corpo, &u.ID)
		if err != nil {
			return err
		}
		// Uma linha de trilha por unidade bloqueada, e não uma pelo lote: a
		// pergunta que a auditoria responde é "quem tirou ESTA unidade do ar
		// nestas datas", e `entity_id` é o que permite achá-la.
		for _, b := range out {
			if err := s.auditarBloco(ctx, VerboBloqueada, b, false); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// conferirHorizonteDoBloqueio é a metade do teto que o DTO não consegue aplicar:
// ela depende de HOJE, e "hoje" no sistema é o dia no fuso da PROPRIEDADE, que
// só o banco sabe (ver Repository.DataLocal). O DTO continua puro e cuida da
// DURAÇÃO; o horizonte fica aqui.
//
// Os dois juntos são o que transforma "a década numa tecla" em, no pior caso,
// um ano de uma casa de oito unidades dentro do horizonte de venda.
func (s *Servico) conferirHorizonteDoBloqueio(ctx context.Context, propriedade uuid.UUID, corpo BloqueioCriar) error {
	de, err := calendar.Parse(corpo.De)
	if err != nil {
		// O DTO já validou o formato; chegar aqui é defesa em profundidade.
		return apperr.Validation(map[string]string{"from": "data inválida: use AAAA-MM-DD."})
	}
	hoje, err := s.repo.DataLocal(ctx, propriedade, nil)
	if err != nil {
		return err
	}
	limite := hoje.AddDays(horizonteMaximoDoBloqueio)
	if de.After(limite) {
		return apperr.Validation(map[string]string{
			"from": fmt.Sprintf(
				"o bloqueio começa em %s, além do horizonte de %s (%d dias). "+
					"Calendário se bloqueia dentro do horizonte de venda; retirada permanente é `active = false` no inventário.",
				de, limite, horizonteMaximoDoBloqueio),
		})
	}
	return nil
}

// LiberarBloqueio devolve a data ao estoque vendável, preservando o registro.
//
// O escopo `own` é aplicado NO SQL (ver Repository.LiberarBloqueio): quem só
// mexe no que é seu recebe 404 no bloqueio alheio. A permissão de `excluir` só é
// segura com esse filtro — a matriz concede ao corretor o par `calendar:criar` +
// `calendar:excluir`, e sem o dono no WHERE ele apagaria o bloqueio de qualquer
// pessoa.
func (s *Servico) LiberarBloqueio(ctx context.Context, id uuid.UUID) error {
	u, err := ator(ctx)
	if err != nil {
		return err
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.LiberarBloqueio(ctx, u.PropertyID, id,
			auth.SomenteProprios(ctx, RecursoCalendario, auth.AcaoExcluir), u.ID)
		if err != nil {
			return err
		}
		// `before` guarda o bloqueio como ele estava: depois do UPDATE o período
		// e o status originais não estão mais em lugar nenhum.
		return s.auditarBloco(ctx, VerboDesbloqueada, antes, true)
	})
}

// ─────────────────────────── Auxiliar ───────────────────────────────

func textoOu(v *string, padrao string) string {
	if v == nil || *v == "" {
		return padrao
	}
	return *v
}
