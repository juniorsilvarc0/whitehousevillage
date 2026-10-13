package main

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/agenda"
)

// sqlConfiguracaoDaAgenda garante a linha de `agenda_settings` da propriedade
// (docs/db.md §9, 20261013120000) e NUNCA a reescreve.
//
// Só `property_id`: os horários são os DEFAULTs da migration — check-in 14:00,
// check-out 11:00, limpeza das 11:00 às 14:00, funcionamento das 08:00 às 20:00
// nos sete dias, passo de 30 e folga de 15 —, PROVISÓRIOS e a confirmar pelo
// dono (decisão 12 do roadmap), que os grava em Configurações → Agenda. Com
// `updated_by` nulo, a API os devolve como `provisional: true`.
//
// `DO NOTHING`, e não o `DO UPDATE ... WHERE IS DISTINCT FROM` das outras
// etapas: aqui não existe divergência que o seed tenha o direito de corrigir.
// Todo valor diferente do DEFAULT é decisão do dono, e o seed roda a cada
// deploy — um `DO UPDATE` devolveria o check-in às 14:00 na manhã seguinte à
// confirmação, em silêncio, e o `PUT /agenda/settings` que reprogramou a
// agenda inteira ficaria desmentido pela configuração. É a regra 3 do seed
// (docs/db.md §16) no limite: corrigir divergência, nunca apagar decisão.
//
// A migration já cria a linha das propriedades que existiam quando ela rodou;
// esta etapa cobre o banco novo, em que a propriedade só nasce DEPOIS dela, na
// primeira etapa do seed (`propriedade`). O módulo da agenda também a cria na
// primeira leitura, se ainda faltar, com o mesmo `ON CONFLICT DO NOTHING`.
const sqlConfiguracaoDaAgenda = `
	INSERT INTO agenda_settings (property_id)
	VALUES ($1)
	ON CONFLICT (property_id) DO NOTHING
	RETURNING (xmax = 0)`

func configuracaoDaAgenda(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	// Com DO NOTHING, a linha que já existia não volta no RETURNING: a segunda
	// execução conta zero criadas e zero atualizadas, e a linha cai em
	// `inalteradas` — que é exatamente o que ela é para o seed.
	c, err := upsert(ctx, tx, sqlConfiguracaoDaAgenda, st.propriedadeID)
	c.Previstas = 1
	return c, err
}

// agendaDasReservasConfirmadas leva para a agenda as reservas que foram
// confirmadas ANTES de ela existir (docs/db.md §9 e §16): as `confirmed` e
// `checked_in` da propriedade com saída de hoje em diante, no dia da casa,
// ganham o check-in, o check-out e a limpeza automáticos.
//
// A regra não mora aqui. É `agenda.PreencherReservasConfirmadas`, o mesmo
// `regra.Plan` do `/confirm`: uma terceira cópia de "o check-in é às 14:00 do
// dia da entrada" divergiria das outras duas na primeira mudança de horário.
// A etapa só a chama na transação do seed, e por isso vem DEPOIS de
// `agenda_settings`: os horários dos eventos saem daquela linha, lida FOR
// SHARE. O seed não cria reserva nenhuma, então não há etapa de reserva a
// esperar.
//
// Idempotente pelo índice `agenda_events_da_reserva_idx`: a segunda execução
// cria zero. E o seed roda em TODO deploy (`infra/deploy.sh`), então a etapa
// também refaz, no deploy seguinte, o automático de qualquer reserva
// confirmada que tenha ficado sem ele — sem nunca tocar no que já existe.
//
// A contagem é só do que nasceu: `Previstas` = `Criadas`. Saber quantos
// eventos a etapa "quer ver" exigiria repetir aqui quais reservas entram e o
// "hoje" no fuso da casa — a segunda cópia que a chamada acima existe para
// evitar. Na segunda execução a linha do log sai toda em zero, e é essa a
// prova de idempotência desta etapa.
//
// Não grava `audit_log`: o seed não tem ator, e a confirmação de cada reserva
// já está na trilha dela. Uma falha aqui derruba o seed inteiro (transação
// única), e com ele o deploy — o mesmo que aconteceria com o `/confirm`
// diante de horários incoerentes.
func agendaDasReservasConfirmadas(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	n, err := agenda.PreencherReservasConfirmadas(ctx, tx, uuid.UUID(st.propriedadeID.Bytes))
	if err != nil {
		return contagem{}, err
	}
	return contagem{Previstas: n, Criadas: n}, nil
}
