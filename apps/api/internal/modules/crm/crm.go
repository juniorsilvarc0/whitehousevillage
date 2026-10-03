// Package crm implementa o funil comercial: funis, etapas, leads,
// oportunidades, atividades, motivos de perda e histórico de etapa (spec §7).
//
// # O que mora aqui e o que NÃO mora
//
// Mora aqui a máquina do funil: entrar numa etapa, gravar o histórico, criar a
// tarefa automática do SLA, fechar por ganho ou por perda. Não mora aqui NADA
// de preço nem de calendário: `/win` cria a reserva chamando o módulo
// `reservas`, que por sua vez chama o motor em `internal/domain/booking`.
// Recalcular um centavo aqui criaria uma terceira verdade sobre o mesmo
// orçamento (regra 1 do CLAUDE.md).
//
// # Os três derivados que este pacote se recusa a gravar
//
// SLA estourado, tarefa vencida e "parado há N dias" são calculados na LEITURA,
// com o relógio da casa (`properties.timezone`). Coluna gravada para qualquer um
// dos três precisaria de um job para nascer e de outro para morrer, e erraria em
// silêncio nos dois — o alerta que envelhece sozinho é o único que não mente.
//
// # Escopo `own`
//
// O corretor enxerga só a própria carteira. O filtro é `AND owner_id = $usuario`
// NO SQL, inclusive no kanban e no `/full` — peneira em memória faria o `total`
// da paginação e o total da coluna do quadro mentirem, que é justamente o número
// que a gestão lê.
package crm

// Recursos do catálogo de RBAC (semeados por cmd/seed/acesso.go).
//
// `crm.pipelines` cobre funis, ETAPAS e MOTIVOS DE PERDA: os três são
// configuração do funil, e é isso que faz o corretor — que tem `crm.pipelines`
// em somente-leitura no seed — conseguir ler o catálogo de motivos que o próprio
// `/lose` dele exige. Recurso separado para `crm.stages` obrigaria uma linha
// nova na matriz para um dado que ninguém edita sem editar o funil junto.
const (
	RecursoFunis         = "crm.pipelines"
	RecursoLeads         = "crm.leads"
	RecursoOportunidades = "crm.opportunities"
	RecursoAtividades    = "crm.activities"
)

// Tipos de etapa. `ganho` e `perdido` são TERMINAIS: só `/win` e `/lose` chegam
// neles, nunca `/stage`.
const (
	EtapaAberta  = "aberto"
	EtapaGanho   = "ganho"
	EtapaPerdido = "perdido"
)

// Estados da oportunidade. O texto do BANCO é `aberto|ganho|perdido`; o do
// CONTRATO é `aberta|ganha|perdida` (concordância com "oportunidade"). A
// tradução vive em EstadoParaContrato/EstadoDoContrato e não vaza para o SQL.
const (
	OportunidadeAberta  = "aberto"
	OportunidadeGanha   = "ganho"
	OportunidadePerdida = "perdido"
)

// Estados do lead.
const (
	LeadNovo          = "novo"
	LeadEmAtendimento = "em_atendimento"
	LeadQualificado   = "qualificado"
	LeadConvertido    = "convertido"
	LeadDescartado    = "descartado"
)

// Estados da atividade.
const (
	AtividadePendente  = "pendente"
	AtividadeConcluida = "concluida"
	AtividadeCancelada = "cancelada"
)

// Tipos de atividade. `nota` é o registro sem prazo — a nota da tela é uma
// atividade deste tipo, e NÃO uma tabela própria: a linha do tempo unificada é
// uma leitura só, e não a costura de seis tabelas (ver `crm_notes` no relatório).
const (
	AtividadeTarefa   = "tarefa"
	AtividadeLigacao  = "ligacao"
	AtividadeReuniao  = "reuniao"
	AtividadeEmail    = "email"
	AtividadeWhatsApp = "whatsapp"
	AtividadeNota     = "nota"
)

// Prioridades.
const (
	PrioridadeBaixa  = "baixa"
	PrioridadeNormal = "normal"
	PrioridadeAlta   = "alta"
)

// diasParadoParaAlerta é o N do alerta "cliente parado há N dias": nenhuma
// atividade concluída nem mudança de etapa desde então.
//
// Quinze dias, e não sete: no aluguel de temporada o cliente que pediu preço em
// agosto para o Réveillon some por semanas por conta própria, e um alerta que
// acende toda semana em toda oportunidade é um alerta que a operação aprende a
// ignorar — e aí ele não acende para nenhuma.
const diasParadoParaAlerta = 15

// ─────────────────────────── Erros do módulo ────────────────────────
//
// Os códigos do funil (STAGE_NOT_IN_PIPELINE, STAGE_ORDER_INCOMPLETE,
// DEFAULT_PIPELINE_REQUIRED, OPPORTUNITY_ALREADY_CLOSED, LOSS_REASON_REQUIRED,
// QUOTE_REQUIRED_TO_WIN, LEAD_ALREADY_CONVERTED) e os que o CRM reusa
// (RESOURCE_IN_USE, INVALID_STATE_TRANSITION, CODE_IN_USE) moram em
// `internal/platform/apperr`, com status e frase padrão. Os pontos de uso
// trocam a frase com WithMessage quando o contexto pede.

// EstadoParaContrato traduz o status do BANCO (`aberto|ganho|perdido`) para o do
// CONTRATO (`aberta|ganha|perdida`).
//
// Por que existe: o schema escreve o status da oportunidade com a mesma palavra
// do `type` da etapa, e o contrato escreve com a concordância de
// "oportunidade". Traduzir num par de funções — e não espalhar o `CASE` pelo SQL
// — mantém uma única definição de cada lado.
func EstadoParaContrato(status string) string {
	switch status {
	case OportunidadeGanha:
		return "ganha"
	case OportunidadePerdida:
		return "perdida"
	default:
		return "aberta"
	}
}

// EstadoDoContrato é o caminho inverso, para os filtros de query.
func EstadoDoContrato(valor string) (string, bool) {
	switch valor {
	case "aberta", "aberto":
		return OportunidadeAberta, true
	case "ganha", "ganho":
		return OportunidadeGanha, true
	case "perdida", "perdido":
		return OportunidadePerdida, true
	default:
		return "", false
	}
}

// StatusDaEtapa traduz o tipo da etapa no status da oportunidade que entra
// nela. É a ligação que faz `EstadoDaOportunidade` ser "espelho do `type` da
// etapa" sem que uma segunda regra decida o mesmo.
func StatusDaEtapa(tipo string) string {
	switch tipo {
	case EtapaGanho:
		return OportunidadeGanha
	case EtapaPerdido:
		return OportunidadePerdida
	default:
		return OportunidadeAberta
	}
}

// EtapaTerminal diz se a etapa fecha a oportunidade.
func EtapaTerminal(tipo string) bool { return tipo == EtapaGanho || tipo == EtapaPerdido }
