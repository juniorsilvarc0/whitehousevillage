//go:build integration

package main

import (
	"context"
	"testing"
)

// As duas etapas da agenda contra o banco (20261013120000).
//
// `agenda_settings`, nas duas metades: cria a linha que falta com os horários
// PROVISÓRIOS dos DEFAULTs, e não toca na linha que o dono já gravou.
//
// `agenda_das_reservas_confirmadas`: leva para a agenda as reservas confirmadas
// antes de ela existir, e a segunda execução cria zero.
//
// Tudo numa transação com ROLLBACK, como em politica_comercial_integration_test.go:
// o banco de integração é compartilhado pelos outros pacotes.

// sqlLinhaDaAgenda lê a linha inteira, em texto, para comparar antes e depois
// sem depender de como o pgx decodifica `time` e `jsonb`.
const sqlLinhaDaAgenda = `
	SELECT checkin_time::text, checkout_time::text, cleaning_start::text, cleaning_end::text,
	       business_hours::text, slot_minutes, buffer_minutes,
	       coalesce(updated_by::text, ''), updated_at::text
	  FROM agenda_settings
	 WHERE property_id = $1`

type linhaDaAgenda struct {
	checkin, checkout, limpezaInicio, limpezaFim, funcionamento string
	passo, folga                                                int
	editor, editadaEm                                           string
}

func TestSeedDaAgendaCriaALinhaProvisoria(t *testing.T) {
	pool := abrirPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("abrindo transação: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st := &estado{}
	if _, err := propriedade(ctx, tx, st); err != nil {
		t.Fatalf("resolvendo a propriedade do seed: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM agenda_settings WHERE property_id = $1`, st.propriedadeID); err != nil {
		t.Fatalf("tirando a linha para o seed recriar: %v", err)
	}

	c, err := configuracaoDaAgenda(ctx, tx, st)
	if err != nil {
		t.Fatalf("etapa agenda_settings: %v", err)
	}
	if c.Criadas != 1 || c.Atualizadas != 0 {
		t.Fatalf("esperava 1 criada e 0 atualizadas, veio %+v", c)
	}

	var l linhaDaAgenda
	var dias int
	if err := tx.QueryRow(ctx, sqlLinhaDaAgenda, st.propriedadeID).Scan(
		&l.checkin, &l.checkout, &l.limpezaInicio, &l.limpezaFim, &l.funcionamento,
		&l.passo, &l.folga, &l.editor, &l.editadaEm); err != nil {
		t.Fatalf("relendo a linha: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT jsonb_array_length(business_hours) FROM agenda_settings WHERE property_id = $1`,
		st.propriedadeID).Scan(&dias); err != nil {
		t.Fatalf("contando os dias de funcionamento: %v", err)
	}

	// Os provisórios da decisão 12, vindos dos DEFAULTs — o seed não escreve
	// nenhum deles.
	if l.checkin != "14:00:00" || l.checkout != "11:00:00" || l.limpezaInicio != "11:00:00" || l.limpezaFim != "14:00:00" {
		t.Errorf("horários %s/%s/%s–%s; os provisórios são 14:00/11:00/11:00–14:00",
			l.checkin, l.checkout, l.limpezaInicio, l.limpezaFim)
	}
	if l.passo != 30 || l.folga != 15 || dias != 7 {
		t.Errorf("passo %d, folga %d, %d dias de funcionamento; os provisórios são 30, 15 e os sete dias", l.passo, l.folga, dias)
	}
	if l.editor != "" {
		t.Errorf("updated_by = %s numa linha que ninguém gravou — a API diria provisional: false", l.editor)
	}
}

func TestSeedDaAgendaNaoReescreveOQueODonoGravou(t *testing.T) {
	pool := abrirPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("abrindo transação: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st := &estado{}
	if _, err := propriedade(ctx, tx, st); err != nil {
		t.Fatalf("resolvendo a propriedade do seed: %v", err)
	}
	if _, err := configuracaoDaAgenda(ctx, tx, st); err != nil {
		t.Fatalf("garantindo a linha: %v", err)
	}

	// O dono confirma horários diferentes dos provisórios. `updated_by` sai de
	// qualquer usuário que exista (nulo num banco sem usuários, e o teste vale
	// do mesmo jeito: o que se confere é que NADA muda).
	etiqueta, err := tx.Exec(ctx, `
		UPDATE agenda_settings
		   SET checkin_time = '15:00', checkout_time = '12:00',
		       cleaning_start = '12:00', cleaning_end = '15:00',
		       business_hours = '[{"weekday":6,"start":"09:00","end":"13:00"}]',
		       slot_minutes = 15, buffer_minutes = 10,
		       updated_by = (SELECT id FROM users ORDER BY created_at, id LIMIT 1),
		       updated_at = now() - interval '1 day'
		 WHERE property_id = $1`, st.propriedadeID)
	if err != nil {
		t.Fatalf("gravando como o dono: %v", err)
	}
	if etiqueta.RowsAffected() != 1 {
		t.Fatalf("linha de agenda_settings não encontrada (%d linhas)", etiqueta.RowsAffected())
	}

	var antes linhaDaAgenda
	if err := tx.QueryRow(ctx, sqlLinhaDaAgenda, st.propriedadeID).Scan(
		&antes.checkin, &antes.checkout, &antes.limpezaInicio, &antes.limpezaFim, &antes.funcionamento,
		&antes.passo, &antes.folga, &antes.editor, &antes.editadaEm); err != nil {
		t.Fatalf("lendo a linha do dono: %v", err)
	}

	c, err := configuracaoDaAgenda(ctx, tx, st)
	if err != nil {
		t.Fatalf("reexecutando a etapa: %v", err)
	}
	if c.Criadas != 0 || c.Atualizadas != 0 || c.inalteradas() != 1 {
		t.Fatalf("a etapa contou %+v sobre a linha do dono; esperado 1 inalterada", c)
	}

	var depois linhaDaAgenda
	if err := tx.QueryRow(ctx, sqlLinhaDaAgenda, st.propriedadeID).Scan(
		&depois.checkin, &depois.checkout, &depois.limpezaInicio, &depois.limpezaFim, &depois.funcionamento,
		&depois.passo, &depois.folga, &depois.editor, &depois.editadaEm); err != nil {
		t.Fatalf("relendo a linha: %v", err)
	}
	if depois != antes {
		t.Fatalf("o seed reescreveu a configuração do dono:\nantes  %+v\ndepois %+v", antes, depois)
	}
}

// Cinco reservas sem agenda, como as que foram confirmadas antes de ela
// existir. Só duas entram: a confirmada com saída à frente e a que está com o
// hóspede dentro (cujo check-in nasce concluído, no instante registrado na
// reserva). A de saída passada, a pré-reserva e a cancelada ficam de fora.
func TestSeedPreencheAAgendaDasReservasConfirmadas(t *testing.T) {
	pool := abrirPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("abrindo transação: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st := &estado{}
	if _, err := propriedade(ctx, tx, st); err != nil {
		t.Fatalf("resolvendo a propriedade do seed: %v", err)
	}
	if _, err := configuracaoDaAgenda(ctx, tx, st); err != nil {
		t.Fatalf("garantindo os horários: %v", err)
	}

	// O banco de integração é compartilhado, e outra suíte pode ter deixado
	// reserva confirmada sem agenda (criada por SQL). Uma primeira passada
	// absorve o que houver, para as contagens abaixo serem só deste teste.
	if _, err := agendaDasReservasConfirmadas(ctx, tx, st); err != nil {
		t.Fatalf("primeira passada: %v", err)
	}

	// Produto e contato próprios, e as datas contadas do "hoje" da casa — o
	// mesmo que a etapa usa. Tudo volta com o ROLLBACK.
	linhas, err := tx.Query(ctx, `
		WITH hoje AS (
		  SELECT (now() AT TIME ZONE p.timezone)::date AS d FROM properties p WHERE p.id = $1
		), produto AS (
		  INSERT INTO unit_types (property_id, code, name, capacity, consumes)
		  VALUES ($1, 'teste-preenchimento-da-agenda', 'Teste do preenchimento da agenda', 4, 'one_member')
		  RETURNING id
		), contato AS (
		  INSERT INTO contacts (property_id, name) VALUES ($1, 'Hóspede do preenchimento') RETURNING id
		)
		INSERT INTO reservations (property_id, unit_type_id, contact_id, status, check_in, check_out, guests_count, notes)
		SELECT $1, produto.id, contato.id, r.status, hoje.d + r.de, hoje.d + r.ate, 2, r.rotulo
		  FROM produto, contato, hoje,
		       (VALUES ('futura',    'confirmed',   30,  33),
		               ('hospedada', 'checked_in',  -1,   2),
		               ('passada',   'confirmed',  -10,  -1),
		               ('em_hold',   'hold',        40,  42),
		               ('cancelada', 'cancelled',   50,  52)) AS r(rotulo, status, de, ate)
		RETURNING notes, id::text`, st.propriedadeID)
	if err != nil {
		t.Fatalf("criando as reservas sem agenda: %v", err)
	}
	reservas := map[string]string{}
	for linhas.Next() {
		var rotulo, id string
		if err := linhas.Scan(&rotulo, &id); err != nil {
			t.Fatalf("lendo as reservas criadas: %v", err)
		}
		reservas[rotulo] = id
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("criando as reservas sem agenda: %v", err)
	}
	if len(reservas) != 5 {
		t.Fatalf("esperava 5 reservas de teste, vieram %d", len(reservas))
	}

	// A entrada da hospedada foi registrada ontem às 15:20 da casa: é esse o
	// instante em que o check-in automático tem de nascer concluído.
	var entrada string
	if err := tx.QueryRow(ctx, `
		INSERT INTO reservation_events (reservation_id, type, at)
		SELECT $1::uuid, 'checked_in', ((now() AT TIME ZONE p.timezone)::date - 1 + time '15:20') AT TIME ZONE p.timezone
		  FROM properties p WHERE p.id = $2
		RETURNING at::text`, reservas["hospedada"], st.propriedadeID).Scan(&entrada); err != nil {
		t.Fatalf("registrando a entrada da hospedada: %v", err)
	}

	c, err := agendaDasReservasConfirmadas(ctx, tx, st)
	if err != nil {
		t.Fatalf("etapa agenda_das_reservas_confirmadas: %v", err)
	}
	if c.Criadas != 6 || c.Previstas != 6 || c.Atualizadas != 0 {
		t.Fatalf("a etapa contou %+v; esperado 6 criadas (três da futura, três da hospedada)", c)
	}

	c, err = agendaDasReservasConfirmadas(ctx, tx, st)
	if err != nil {
		t.Fatalf("segunda execução: %v", err)
	}
	if c.Criadas != 0 || c.Previstas != 0 || c.mudou() {
		t.Fatalf("a segunda execução contou %+v; esperado zero — o preenchimento não é idempotente", c)
	}

	// O que nasceu, conferido contra a reserva e contra os horários da casa
	// lidos na mesma transação (o teste não supõe os provisórios: outra suíte
	// pode ter gravado outros).
	type evento struct {
		tipo, status        string
		diaCerto, horaCerta bool
		soAReserva          bool
		concluidoNaEntrada  bool
	}
	porReserva := map[string][]evento{}
	linhas, err = tx.Query(ctx, `
		SELECT r.notes, e.type, e.status,
		       (e.starts_at AT TIME ZONE p.timezone)::date
		         = CASE e.type WHEN 'checkin' THEN r.check_in ELSE r.check_out END,
		       CASE e.type
		         WHEN 'checkin'  THEN (e.starts_at AT TIME ZONE p.timezone)::time = s.checkin_time
		         WHEN 'checkout' THEN (e.ends_at   AT TIME ZONE p.timezone)::time = s.checkout_time
		         ELSE (e.starts_at AT TIME ZONE p.timezone)::time = s.cleaning_start
		          AND (e.ends_at   AT TIME ZONE p.timezone)::time = s.cleaning_end
		       END,
		       e.origin = 'reservation' AND e.unit_id IS NULL AND e.contact_id IS NULL
		         AND e.title IS NULL AND e.created_by IS NULL,
		       e.done_at IS NOT DISTINCT FROM $2::timestamptz
		  FROM agenda_events e
		  JOIN reservations r    ON r.id = e.reservation_id
		  JOIN properties p      ON p.id = r.property_id
		  JOIN agenda_settings s ON s.property_id = r.property_id
		 WHERE r.id = ANY($1::uuid[])
		 ORDER BY r.notes, e.type`,
		[]string{reservas["futura"], reservas["hospedada"], reservas["passada"], reservas["em_hold"], reservas["cancelada"]},
		entrada)
	if err != nil {
		t.Fatalf("lendo os eventos: %v", err)
	}
	for linhas.Next() {
		var rotulo string
		var e evento
		if err := linhas.Scan(&rotulo, &e.tipo, &e.status, &e.diaCerto, &e.horaCerta, &e.soAReserva, &e.concluidoNaEntrada); err != nil {
			t.Fatalf("lendo um evento: %v", err)
		}
		porReserva[rotulo] = append(porReserva[rotulo], e)
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("lendo os eventos: %v", err)
	}

	for _, fora := range []string{"passada", "em_hold", "cancelada"} {
		if n := len(porReserva[fora]); n != 0 {
			t.Errorf("a reserva %s ganhou %d eventos; fica fora do preenchimento", fora, n)
		}
	}
	for _, dentro := range []string{"futura", "hospedada"} {
		evs := porReserva[dentro]
		if len(evs) != 3 {
			t.Errorf("a reserva %s tem %d eventos automáticos; esperado 3", dentro, len(evs))
			continue
		}
		// ORDER BY e.type: checkin, checkout, limpeza.
		for i, tipo := range []string{"checkin", "checkout", "limpeza"} {
			e := evs[i]
			if e.tipo != tipo {
				t.Errorf("%s: evento %d é %s, esperado %s", dentro, i, e.tipo, tipo)
				continue
			}
			if !e.diaCerto || !e.horaCerta {
				t.Errorf("%s: %s fora do dia (%v) ou da hora da casa (%v)", dentro, tipo, e.diaCerto, e.horaCerta)
			}
			if !e.soAReserva {
				t.Errorf("%s: %s guarda mais que a reserva (unidade, titular, título ou autor)", dentro, tipo)
			}
			hospedeDentro := dentro == "hospedada" && tipo == "checkin"
			switch {
			case hospedeDentro && (e.status != "concluido" || !e.concluidoNaEntrada):
				t.Errorf("hospedada: o check-in tinha de nascer concluído no instante da entrada (%s); veio %s", entrada, e.status)
			case !hospedeDentro && e.status != "agendado":
				t.Errorf("%s: %s nasceu %s, esperado agendado", dentro, tipo, e.status)
			}
		}
	}
}
