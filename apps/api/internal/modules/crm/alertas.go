package crm

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Derivados do CRM: SLA, alertas e linha do tempo.
//
// ═══ Por que nada disto é coluna gravada ═══
//
// "SLA estourado", "tarefa vencida" e "cliente parado há N dias" mudam com a
// PASSAGEM DO TEMPO, não com uma escrita. Uma coluna `sla_breached` precisaria
// de um job para nascer e de outro para morrer; no dia em que o job atrasasse,
// a tela mostraria "no prazo" para um card estourado — e ninguém descobriria,
// porque a tela é a única testemunha. Derivar na leitura custa uma comparação
// por linha e não tem estado para envelhecer errado.
//
// O "agora" vem SEMPRE do banco (`Repository.Agora`), lido uma vez por
// requisição: assim todas as linhas da mesma resposta são julgadas pelo mesmo
// instante, e o fuso é o da casa (`properties.timezone`, America/Fortaleza), não
// o do navegador de quem abriu a tela.

// slaEstourado é a regra única: passou do vencimento E a oportunidade continua
// ABERTA. Card ganho em cima do prazo não é alerta — é história.
func slaEstourado(vence *time.Time, status string, agora time.Time) bool {
	if vence == nil {
		return false
	}
	if status != EstadoParaContrato(OportunidadeAberta) && status != OportunidadeAberta {
		return false
	}
	return vence.Before(agora)
}

// faixaDeSLA monta a faixa da tela da oportunidade.
//
// `days_left` é NEGATIVO quando estourou — e é arredondado para CIMA em módulo
// (`Ceil` no que falta, `Floor` no que passou) para que "faltam 0 dias" nunca
// apareça enquanto ainda há prazo: a diferença entre "vence hoje" e "venceu" é a
// diferença entre ligar agora e ter perdido o cliente.
func faixaDeSLA(o Oportunidade, etapa Etapa) FaixaDeSLA {
	f := FaixaDeSLA{
		EtapaID:   o.EtapaID,
		EtapaNome: o.EtapaNome,
		SLADias:   o.SLADias,
		EntrouEm:  o.EntrouNaEtapaEm,
		VenceEm:   o.SLAVenceEm,
		Estourado: o.SLAEstourado,
	}
	if f.EtapaNome == "" {
		f.EtapaNome = etapa.Nome
	}
	if o.SLAVenceEm == nil {
		return f
	}

	// A base é o próprio vencimento comparado com o "agora" que já decidiu
	// `SLAEstourado`: reconstruir o instante aqui abriria a chance de os dois
	// números discordarem na mesma resposta.
	restante := time.Until(*o.SLAVenceEm).Hours() / 24
	dias := int(math.Ceil(restante))
	if o.SLAEstourado {
		dias = int(math.Floor(restante))
	}
	f.DiasRestantes = &dias
	return f
}

// montarAlertas devolve os alertas DERIVADOS da tela da oportunidade.
//
// Não há tabela de alertas: cada um destes é uma comparação sobre dado que a
// própria chamada do `/full` já carregou — nenhuma consulta a mais, nenhum
// estado para envelhecer errado.
func montarAlertas(c OportunidadeCompleta, agora time.Time) []AlertaDaOportunidade {
	alertas := []AlertaDaOportunidade{}

	// Oportunidade fechada não gera alerta. Cobrar follow-up de negócio
	// encerrado é o ruído que ensina a operação a ignorar a coluna inteira.
	if c.Oportunidade.Status != "aberta" {
		return alertas
	}

	if c.Oportunidade.SLAEstourado && c.Oportunidade.SLAVenceEm != nil {
		dias := int(agora.Sub(*c.Oportunidade.SLAVenceEm).Hours() / 24)
		alertas = append(alertas, AlertaDaOportunidade{
			Codigo:     AlertaSLAEstourado,
			Severidade: SeveridadeCritico,
			Mensagem: fmt.Sprintf("SLA da etapa %s estourou há %s.",
				c.Oportunidade.EtapaNome, emDias(dias)),
			VenceEm:    c.Oportunidade.SLAVenceEm,
			EntidadeID: &c.Oportunidade.EtapaID,
		})
	}

	for _, a := range c.Atividades {
		if a.Status != AtividadePendente || a.VenceEm == nil || !a.VenceEm.Before(agora) {
			continue
		}
		dias := int(agora.Sub(*a.VenceEm).Hours() / 24)
		id := a.ID
		alertas = append(alertas, AlertaDaOportunidade{
			Codigo:     AlertaTarefaVencida,
			Severidade: SeveridadeAtencao,
			Mensagem:   fmt.Sprintf("A tarefa %q venceu há %s.", a.Assunto, emDias(dias)),
			VenceEm:    a.VenceEm,
			EntidadeID: &id,
		})
	}

	// "Parado há N dias": nada aconteceu desde o último sinal de vida — nem
	// movimento de etapa, nem atividade concluída. A base é o MAIS RECENTE dos
	// dois, e não só a entrada na etapa: o card que ficou em "Negociação"
	// enquanto o corretor ligou três vezes está vivo, e acusá-lo de parado seria
	// punir quem está trabalhando.
	if ultimo := ultimoSinalDeVida(c); !ultimo.IsZero() {
		parado := int(agora.Sub(ultimo).Hours() / 24)
		if parado >= diasParadoParaAlerta {
			alertas = append(alertas, AlertaDaOportunidade{
				Codigo:     AlertaParado,
				Severidade: SeveridadeAtencao,
				Mensagem:   fmt.Sprintf("Sem contato com o cliente há %s.", emDias(parado)),
			})
		}
	}

	// A pré-reserva que vai expirar é alerta do CRM porque quem perde a data é o
	// negócio, não o calendário: o corretor precisa ver isso no card, e não
	// descobrir pelo mapa de ocupação depois de o bloco cair.
	if c.Reserva != nil && c.Reserva.HoldExpiraEm != nil && c.Reserva.Status == "hold" {
		if c.Reserva.HoldExpiraEm.After(agora) {
			id := c.Reserva.ID
			alertas = append(alertas, AlertaDaOportunidade{
				Codigo:     AlertaHoldExpirando,
				Severidade: SeveridadeCritico,
				Mensagem: fmt.Sprintf("A pré-reserva %s expira em %s.",
					c.Reserva.Codigo, emHoras(c.Reserva.HoldExpiraEm.Sub(agora))),
				VenceEm:    c.Reserva.HoldExpiraEm,
				EntidadeID: &id,
			})
		}
	}

	return alertas
}

// ultimoSinalDeVida devolve o instante do último movimento de etapa ou da última
// atividade concluída — o que for mais recente.
func ultimoSinalDeVida(c OportunidadeCompleta) time.Time {
	ultimo := c.Oportunidade.EntrouNaEtapaEm
	for _, h := range c.Historico {
		if h.Instante.After(ultimo) {
			ultimo = h.Instante
		}
	}
	for _, a := range c.Atividades {
		if a.ConcluidaEm != nil && a.ConcluidaEm.After(ultimo) {
			ultimo = *a.ConcluidaEm
		}
	}
	for _, n := range c.Notas {
		if n.CriadaEm.After(ultimo) {
			ultimo = n.CriadaEm
		}
	}
	return ultimo
}

func emDias(n int) string {
	if n <= 1 {
		return "1 dia"
	}
	return fmt.Sprintf("%d dias", n)
}

func emHoras(d time.Duration) string {
	horas := int(d.Hours())
	if horas < 1 {
		return "menos de 1 hora"
	}
	if horas == 1 {
		return "1 hora"
	}
	return fmt.Sprintf("%d horas", horas)
}

// ═══════════════════════════ Linha do tempo ═════════════════════════

// montarTimeline costura movimentos de etapa, atividades, notas e reserva numa
// ordem cronológica única — a resposta a "o que já foi falado com essa pessoa"
// sem trocar de tela.
//
// É montada EM MEMÓRIA, a partir do que o `/full` já leu: uma tabela de eventos
// unificada seria um terceiro registro do mesmo fato (depois de
// `crm_stage_history` e `crm_activities`), e três registros do mesmo fato
// divergem no dia em que só dois forem escritos.
func montarTimeline(c OportunidadeCompleta) []EventoDaOportunidade {
	linhas := []EventoDaOportunidade{}

	linhas = append(linhas, EventoDaOportunidade{
		ID:       c.Oportunidade.ID,
		Tipo:     LinhaCriada,
		Instante: c.Oportunidade.CriadoEm,
		AtorID:   c.Oportunidade.CriadoPor,
		Titulo:   "Oportunidade criada para " + c.Oportunidade.ContatoNome,
	})

	for _, h := range c.Historico {
		titulo := "Entrou em " + h.ParaEtapaNome
		if h.DeEtapaNome != nil {
			titulo = fmt.Sprintf("Movida de %s para %s", *h.DeEtapaNome, h.ParaEtapaNome)
		}
		tipo := LinhaEtapaMudou
		switch {
		case c.Oportunidade.Status == "ganha" && h.ParaEtapaID == c.Oportunidade.EtapaID:
			tipo = LinhaGanha
		case c.Oportunidade.Status == "perdida" && h.ParaEtapaID == c.Oportunidade.EtapaID:
			tipo = LinhaPerdida
		}
		payload := map[string]any{"to_stage_id": h.ParaEtapaID}
		if h.Motivo != nil {
			payload["reason"] = *h.Motivo
		}
		if h.DiasNaEtapa != nil {
			payload["days_in_stage"] = *h.DiasNaEtapa
		}
		linhas = append(linhas, EventoDaOportunidade{
			ID: h.ID, Tipo: tipo, Instante: h.Instante,
			AtorID: h.UsuarioID, AtorNome: h.UsuarioNome,
			Titulo: titulo, Payload: payload,
		})
	}

	for _, a := range c.Atividades {
		dono := a.DonoID
		linhas = append(linhas, EventoDaOportunidade{
			ID: a.ID, Tipo: LinhaAtividadeCriada, Instante: a.CriadoEm,
			AtorID: &dono, AtorNome: a.DonoNome,
			Titulo:  rotuloDaAtividade(a) + ": " + a.Assunto,
			Payload: map[string]any{"activity_type": a.Tipo, "auto": a.Auto},
		})
		// A conclusão é uma SEGUNDA linha, e não a substituição da primeira:
		// "criou a tarefa" e "fez a tarefa" são dois fatos, e o intervalo entre
		// eles é o tempo de resposta que o §15 mede.
		if a.ConcluidaEm != nil {
			linhas = append(linhas, EventoDaOportunidade{
				ID: a.ID, Tipo: LinhaAtividadeConcluida, Instante: *a.ConcluidaEm,
				AtorID: &dono, AtorNome: a.DonoNome,
				Titulo:  "Concluída: " + a.Assunto,
				Payload: map[string]any{"activity_type": a.Tipo},
			})
		}
	}

	for _, n := range c.Notas {
		linhas = append(linhas, EventoDaOportunidade{
			ID: n.ID, Tipo: LinhaNota, Instante: n.CriadaEm,
			AtorID: n.AutorID, AtorNome: n.AutorNome,
			Titulo: resumir(n.Corpo, 120),
		})
	}

	if c.Reserva != nil {
		linhas = append(linhas, EventoDaOportunidade{
			ID: c.Reserva.ID, Tipo: LinhaReservaCriada, Instante: c.Reserva.CriadaEm,
			Titulo:  "Reserva " + c.Reserva.Codigo + " criada",
			Payload: map[string]any{"status": c.Reserva.Status, "total_cents": c.Reserva.Total},
		})
	}

	// Do mais recente para o mais antigo. O desempate por tipo mantém estável a
	// ordem de duas linhas do mesmo instante — criação da oportunidade e
	// primeira entrada de etapa nascem no mesmo `now()` da transação.
	sort.SliceStable(linhas, func(i, j int) bool {
		if linhas[i].Instante.Equal(linhas[j].Instante) {
			return linhas[i].Tipo > linhas[j].Tipo
		}
		return linhas[i].Instante.After(linhas[j].Instante)
	})
	return linhas
}

func rotuloDaAtividade(a Atividade) string {
	switch a.Tipo {
	case AtividadeLigacao:
		return "Ligação"
	case AtividadeReuniao:
		return "Reunião"
	case AtividadeEmail:
		return "E-mail"
	case AtividadeWhatsApp:
		return "WhatsApp"
	case AtividadeNota:
		return "Nota"
	default:
		if a.Auto {
			return "Tarefa automática"
		}
		return "Tarefa"
	}
}

func resumir(texto string, teto int) string {
	texto = strings.TrimSpace(strings.ReplaceAll(texto, "\n", " "))
	if len([]rune(texto)) <= teto {
		return texto
	}
	return string([]rune(texto)[:teto]) + "…"
}
