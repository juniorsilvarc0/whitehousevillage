package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// O funil padrão do CRM — docs/spec.md §7.
//
// "Novo lead → Em atendimento → Disponibilidade consultada → Orçamento enviado
// → Negociação → Pré-reserva → Ganho / Perdido". A ordem é a da spec e não é
// decorativa: `position` é o que desenha as colunas do kanban da esquerda para
// a direita, e a `probability` de cada etapa é o que multiplica o valor da
// oportunidade na previsão de receita.
const funilPadraoNome = "Funil de Reservas"

// etapa é uma coluna do kanban.
//
// `slaDias` e a tarefa automática são o que este projeto herdou do sistema de
// referência por terem funcionado lá: ao entrar na etapa, uma tarefa nasce com
// prazo e responsável, e o alerta de SLA estourado é calculado no servidor a
// partir de `entered_stage_at`. Etapa terminal não tem nem um nem outro —
// não existe prazo para sair de "Ganho".
type etapaDoFunil struct {
	nome          string
	posicao       int32
	probabilidade float64
	cor           string
	tipo          string // aberto | ganho | perdido
	slaDias       *int32
	tarefa        string
	tarefaTipo    string
	tarefaPrazo   *int32
	notificar     bool
}

func dias(n int32) *int32 { return &n }

// As oito etapas da spec §7.
//
// As probabilidades sobem de 10 a 100 acompanhando o compromisso real do
// cliente: quem só pediu preço (10%) não é quem já recebeu o link do sinal
// (90%). Os SLAs são curtos de propósito no começo do funil — o lead de
// temporada compara três casas no mesmo dia, e responder em 24 h é o que
// decide a venda; no meio do funil o prazo afrouxa porque a bola está com o
// cliente.
//
// As cores vão do cinza (indefinido) ao verde (ganho), com o vermelho reservado
// à perda: é o mesmo semáforo que a operação já lê em qualquer quadro.
var etapasDoFunilPadrao = []etapaDoFunil{
	{
		nome: "Novo lead", posicao: 1, probabilidade: 10, cor: "#94A3B8", tipo: "aberto",
		slaDias: dias(1),
		tarefa:  "Fazer o primeiro contato", tarefaTipo: "ligacao", tarefaPrazo: dias(1),
		notificar: true,
	},
	{
		nome: "Em atendimento", posicao: 2, probabilidade: 25, cor: "#38BDF8", tipo: "aberto",
		slaDias: dias(2),
		tarefa:  "Levantar datas, número de hóspedes e ocasião", tarefaTipo: "tarefa", tarefaPrazo: dias(1),
	},
	{
		nome: "Disponibilidade consultada", posicao: 3, probabilidade: 40, cor: "#818CF8", tipo: "aberto",
		slaDias: dias(1),
		tarefa:  "Enviar as opções de datas e produtos disponíveis", tarefaTipo: "whatsapp", tarefaPrazo: dias(1),
	},
	{
		// A etapa do critério de aceite: "mover o card para 'Orçamento enviado'
		// cria sozinho a tarefa de follow-up com o prazo do SLA".
		nome: "Orçamento enviado", posicao: 4, probabilidade: 60, cor: "#FBBF24", tipo: "aberto",
		slaDias: dias(2),
		tarefa:  "Follow-up do orçamento enviado", tarefaTipo: "ligacao", tarefaPrazo: dias(2),
		notificar: true,
	},
	{
		nome: "Negociação", posicao: 5, probabilidade: 75, cor: "#FB923C", tipo: "aberto",
		slaDias: dias(3),
		tarefa:  "Retomar a negociação e tratar as objeções", tarefaTipo: "ligacao", tarefaPrazo: dias(2),
	},
	{
		// Pré-reserva é `hold`: a data está SEGURA e o relógio está correndo
		// (docs/spec.md §5 — 48 h de política). Por isso o SLA de 1 dia e a
		// notificação ligada: perder este prazo é perder a data, não só o lead.
		nome: "Pré-reserva", posicao: 6, probabilidade: 90, cor: "#34D399", tipo: "aberto",
		slaDias: dias(1),
		tarefa:  "Cobrar o sinal antes de a pré-reserva vencer", tarefaTipo: "whatsapp", tarefaPrazo: dias(1),
		notificar: true,
	},
	{
		// Terminal: sem SLA e sem tarefa automática. Ganhar cria a reserva a
		// partir do orçamento vigente (spec §7), e a partir daí quem cobra o
		// saldo é o financeiro, não o funil.
		nome: "Ganho", posicao: 7, probabilidade: 100, cor: "#16A34A", tipo: "ganho",
	},
	{
		// Terminal, e exige motivo — a constraint
		// `crm_opportunities_perda_motivada` recusa perda sem `lost_reason_id`.
		nome: "Perdido", posicao: 8, probabilidade: 0, cor: "#DC2626", tipo: "perdido",
	},
}

// funilPadrao cria (ou reconcilia) o funil e guarda o id para as etapas.
//
// Chave natural `(property_id, name)`: renomear o funil na tela cria um segundo
// na próxima execução do seed, e é por isso que o nome é constante do código, e
// não texto editável em duas fontes.
func funilPadrao(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	const q = `
		INSERT INTO crm_pipelines (property_id, name, is_default, active)
		VALUES ($1, $2, true, true)
		ON CONFLICT (property_id, name) DO UPDATE
		   SET is_default = EXCLUDED.is_default,
		       active     = EXCLUDED.active,
		       updated_at = now()
		 WHERE (crm_pipelines.is_default, crm_pipelines.active)
		       IS DISTINCT FROM (EXCLUDED.is_default, EXCLUDED.active)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID, funilPadraoNome)
	if err != nil {
		return c, err
	}
	c.Previstas = 1

	// Segunda consulta pelo mesmo motivo de `propriedade`: com a guarda
	// `IS DISTINCT FROM`, a linha inalterada não volta no RETURNING — e esse é
	// o caso comum da segunda execução em diante.
	if err := tx.QueryRow(ctx,
		`SELECT id FROM crm_pipelines WHERE property_id = $1 AND name = $2`,
		st.propriedadeID, funilPadraoNome,
	).Scan(&st.funilID); err != nil {
		return c, fmt.Errorf("lendo o id do funil padrão: %w", err)
	}
	return c, nil
}

// etapasDoFunil grava as oito colunas do kanban.
//
// A unicidade de `(pipeline_id, position)` é ADIÁVEL na migration
// 20260827110000, e é ela que permite este INSERT único reordenar tudo de uma
// vez: durante a instrução duas etapas podem ocupar a mesma posição, e o banco
// só confere no commit. Com a unicidade imediata, ajustar a ordem no seed
// exigiria uma passada para posições negativas antes de renumerar.
func etapasDoFunil(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	const q = `
		INSERT INTO crm_stages (pipeline_id, name, position, probability, color, type,
		                        sla_days, auto_task_subject, auto_task_type,
		                        auto_task_due_days, auto_notify)
		SELECT $1, e.nome, e.posicao, e.probabilidade, e.cor, e.tipo,
		       e.sla, nullif(e.tarefa, ''), nullif(e.tarefa_tipo, ''), e.tarefa_prazo, e.notificar
		  FROM unnest($2::text[], $3::int[], $4::numeric[], $5::text[], $6::text[],
		              $7::int[], $8::text[], $9::text[], $10::int[], $11::bool[])
		       AS e(nome, posicao, probabilidade, cor, tipo, sla, tarefa, tarefa_tipo, tarefa_prazo, notificar)
		ON CONFLICT (pipeline_id, name) DO UPDATE
		   SET position           = EXCLUDED.position,
		       probability        = EXCLUDED.probability,
		       color              = EXCLUDED.color,
		       type               = EXCLUDED.type,
		       sla_days           = EXCLUDED.sla_days,
		       auto_task_subject  = EXCLUDED.auto_task_subject,
		       auto_task_type     = EXCLUDED.auto_task_type,
		       auto_task_due_days = EXCLUDED.auto_task_due_days,
		       auto_notify        = EXCLUDED.auto_notify,
		       updated_at         = now()
		 WHERE (crm_stages.position, crm_stages.probability, crm_stages.color, crm_stages.type,
		        crm_stages.sla_days, crm_stages.auto_task_subject, crm_stages.auto_task_type,
		        crm_stages.auto_task_due_days, crm_stages.auto_notify)
		       IS DISTINCT FROM
		       (EXCLUDED.position, EXCLUDED.probability, EXCLUDED.color, EXCLUDED.type,
		        EXCLUDED.sla_days, EXCLUDED.auto_task_subject, EXCLUDED.auto_task_type,
		        EXCLUDED.auto_task_due_days, EXCLUDED.auto_notify)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.funilID,
		coluna(etapasDoFunilPadrao, func(e etapaDoFunil) string { return e.nome }),
		coluna(etapasDoFunilPadrao, func(e etapaDoFunil) int32 { return e.posicao }),
		coluna(etapasDoFunilPadrao, func(e etapaDoFunil) float64 { return e.probabilidade }),
		coluna(etapasDoFunilPadrao, func(e etapaDoFunil) string { return e.cor }),
		coluna(etapasDoFunilPadrao, func(e etapaDoFunil) string { return e.tipo }),
		coluna(etapasDoFunilPadrao, func(e etapaDoFunil) *int32 { return e.slaDias }),
		coluna(etapasDoFunilPadrao, func(e etapaDoFunil) string { return e.tarefa }),
		coluna(etapasDoFunilPadrao, func(e etapaDoFunil) string { return e.tarefaTipo }),
		coluna(etapasDoFunilPadrao, func(e etapaDoFunil) *int32 { return e.tarefaPrazo }),
		coluna(etapasDoFunilPadrao, func(e etapaDoFunil) bool { return e.notificar }),
	)
	c.Previstas = len(etapasDoFunilPadrao)
	return c, err
}

// motivo de perda — a lista fechada que a constraint
// `crm_opportunities_perda_motivada` obriga a preencher.
type motivoDePerda struct {
	rotulo string
	ordem  int32
}

// Os motivos existem para que o relatório de perdas responda uma pergunta de
// gestão: o que estamos perdendo por PREÇO (mexe no tarifário), o que estamos
// perdendo por DATA (mexe no inventário) e o que estamos perdendo por
// ATENDIMENTO (mexe no SLA). Uma lista com "outros" no topo devolve "outros"
// como resposta, e ninguém age em cima disso — por isso "Outro motivo" é o
// último da ordem, e não o primeiro.
var motivosDePerdaSeed = []motivoDePerda{
	{"Preço acima do orçamento do cliente", 1},
	{"Datas indisponíveis na casa", 2},
	{"Fechou com outro imóvel", 3},
	{"Desistiu da viagem", 4},
	{"Sem retorno do cliente", 5},
	{"Grupo maior que a capacidade da casa", 6},
	{"Contato duplicado", 7},
	{"Outro motivo", 8},
}

func motivosDePerda(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	const q = `
		INSERT INTO crm_lost_reasons (property_id, label, sort_order, active)
		SELECT $1, m.rotulo, m.ordem, true
		  FROM unnest($2::text[], $3::int[]) AS m(rotulo, ordem)
		ON CONFLICT (property_id, label) DO UPDATE
		   SET sort_order = EXCLUDED.sort_order,
		       active     = EXCLUDED.active,
		       updated_at = now()
		 WHERE (crm_lost_reasons.sort_order, crm_lost_reasons.active)
		       IS DISTINCT FROM (EXCLUDED.sort_order, EXCLUDED.active)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID,
		coluna(motivosDePerdaSeed, func(m motivoDePerda) string { return m.rotulo }),
		coluna(motivosDePerdaSeed, func(m motivoDePerda) int32 { return m.ordem }),
	)
	c.Previstas = len(motivosDePerdaSeed)
	return c, err
}
