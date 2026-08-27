package crm

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Testes de FORMA e de derivação — sem banco, sem HTTP.
//
// O que se protege aqui é o que um teste de integração não pega barato: a
// recusa por campo, o cálculo do prazo e a montagem da timeline. Regra de
// negócio que depende de estado é conferida na suíte de integração.

func TestStatusDaEtapaEspelhaOTipo(t *testing.T) {
	casos := map[string]string{
		EtapaAberta:  OportunidadeAberta,
		EtapaGanho:   OportunidadeGanha,
		EtapaPerdido: OportunidadePerdida,
	}
	for tipo, esperado := range casos {
		if got := StatusDaEtapa(tipo); got != esperado {
			t.Errorf("StatusDaEtapa(%q) = %q, esperado %q", tipo, got, esperado)
		}
	}
	if !EtapaTerminal(EtapaGanho) || !EtapaTerminal(EtapaPerdido) || EtapaTerminal(EtapaAberta) {
		t.Error("EtapaTerminal não separa terminal de aberta")
	}
}

// O banco escreve `aberto|ganho|perdido` (a mesma palavra do `type` da etapa) e
// o contrato escreve `aberta|ganha|perdida`. A tradução tem de fechar nos dois
// sentidos, senão um filtro de query vira `WHERE status = 'ganha'` e não casa
// com linha nenhuma — silenciosamente.
func TestTraducaoDeEstadoFechaNosDoisSentidos(t *testing.T) {
	for _, doBanco := range []string{OportunidadeAberta, OportunidadeGanha, OportunidadePerdida} {
		doContrato := EstadoParaContrato(doBanco)
		volta, ok := EstadoDoContrato(doContrato)
		if !ok || volta != doBanco {
			t.Errorf("%q → %q → %q (ok=%v): a tradução não fecha", doBanco, doContrato, volta, ok)
		}
	}
	if _, ok := EstadoDoContrato("inventado"); ok {
		t.Error("EstadoDoContrato aceitou um valor fora do vocabulário")
	}
}

// ═══ A tarefa automática é tudo-ou-nada ═══
//
// O CHECK `crm_stages_auto_task_completa` exige assunto, tipo e prazo os três ou
// nenhum. Se este preenchimento divergir dele, o operador recebe "valor fora do
// permitido pela regra do banco" em vez de uma mensagem que diz o que fazer.
func TestTarefaAutomaticaNasceCompletaOuNaoNasce(t *testing.T) {
	t.Run("assunto sozinho ganha tipo e prazo padrão", func(t *testing.T) {
		var e Etapa
		aplicarTarefaAutomatica(&e, httpx.De(3), httpx.De("Ligar"), httpx.Opt[string]{}, httpx.Opt[int]{})

		if e.TarefaAssunto == nil || e.TarefaTipo == nil || e.TarefaPrazoDias == nil {
			t.Fatalf("os três campos deveriam estar preenchidos: %+v", e)
		}
		if *e.TarefaTipo != AtividadeTarefa {
			t.Errorf("tipo padrão = %q, esperado %q", *e.TarefaTipo, AtividadeTarefa)
		}
		// O prazo da tarefa e o do SLA são a mesma promessa.
		if *e.TarefaPrazoDias != 3 {
			t.Errorf("prazo = %d, esperado herdar o sla_days (3)", *e.TarefaPrazoDias)
		}
	})

	t.Run("sem assunto os três caem juntos", func(t *testing.T) {
		e := Etapa{
			TarefaAssunto:   ptr("Ligar"),
			TarefaTipo:      ptr(AtividadeLigacao),
			TarefaPrazoDias: ptrInt(2),
		}
		aplicarTarefaAutomatica(&e, httpx.Opt[int]{}, httpx.Nulo[string](), httpx.Opt[string]{}, httpx.Opt[int]{})

		if e.TarefaAssunto != nil || e.TarefaTipo != nil || e.TarefaPrazoDias != nil {
			t.Fatalf("limpar o assunto deveria zerar os três: %+v", e)
		}
	})

	t.Run("sem sla e sem prazo a tarefa vence hoje", func(t *testing.T) {
		var e Etapa
		aplicarTarefaAutomatica(&e, httpx.Nulo[int](), httpx.De("Ligar"), httpx.Opt[string]{}, httpx.Opt[int]{})
		if e.TarefaPrazoDias == nil || *e.TarefaPrazoDias != 0 {
			t.Fatalf("prazo = %v, esperado 0 (hoje) — tarefa sem vencimento é tarefa que não existe", e.TarefaPrazoDias)
		}
	})
}

func TestPrazoDaTarefaPreferOAutoTaskDueDaysAoSLA(t *testing.T) {
	e := Etapa{SLADias: ptrInt(5), TarefaAssunto: ptr("x"), TarefaPrazoDias: ptrInt(2)}
	if dias, ok := e.PrazoDaTarefa(); !ok || dias != 2 {
		t.Fatalf("PrazoDaTarefa = %d, %v; esperado 2", dias, ok)
	}

	semPrazo := Etapa{SLADias: ptrInt(5), TarefaAssunto: ptr("x")}
	if dias, ok := semPrazo.PrazoDaTarefa(); !ok || dias != 5 {
		t.Fatalf("sem auto_task_due_days deveria cair no sla_days: %d, %v", dias, ok)
	}

	terminal := Etapa{}
	if _, ok := terminal.PrazoDaTarefa(); ok {
		t.Error("etapa sem SLA e sem prazo não deveria produzir prazo")
	}
	if terminal.TemTarefaAutomatica() {
		t.Error("etapa sem assunto não tem tarefa automática")
	}
	if (Etapa{TarefaAssunto: ptr("   ")}).TemTarefaAutomatica() {
		t.Error("assunto em branco não liga o mecanismo")
	}
}

// ═══ SLA derivado ═══

func TestSLAEstouradoSoValeParaOportunidadeAberta(t *testing.T) {
	agora := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	vencido := agora.Add(-48 * time.Hour)
	futuro := agora.Add(48 * time.Hour)

	if !slaEstourado(&vencido, "aberta", agora) {
		t.Error("card aberto com prazo vencido deveria estar estourado")
	}
	// Card ganho em cima do prazo não é alerta — é história.
	if slaEstourado(&vencido, "ganha", agora) {
		t.Error("oportunidade ganha não pode acender alerta de SLA")
	}
	if slaEstourado(&futuro, "aberta", agora) {
		t.Error("prazo no futuro não está estourado")
	}
	if slaEstourado(nil, "aberta", agora) {
		t.Error("etapa sem SLA não estoura")
	}
}

// `days_left` NEGATIVO quando estourou, e nunca "0 dias" enquanto ainda há
// prazo: a diferença entre "vence hoje" e "venceu" é a diferença entre ligar
// agora e ter perdido o cliente.
func TestDiasRestantesDoSLASaoNegativosQuandoEstourou(t *testing.T) {
	daqui := time.Now().Add(30 * time.Hour)
	comPrazo := faixaDeSLA(Oportunidade{SLAVenceEm: &daqui, SLAEstourado: false}, Etapa{})
	if comPrazo.DiasRestantes == nil || *comPrazo.DiasRestantes < 1 {
		t.Fatalf("days_left = %v com 30 h de prazo, esperado ao menos 1", comPrazo.DiasRestantes)
	}

	atrasado := time.Now().Add(-30 * time.Hour)
	estourada := faixaDeSLA(Oportunidade{SLAVenceEm: &atrasado, SLAEstourado: true}, Etapa{})
	if estourada.DiasRestantes == nil || *estourada.DiasRestantes >= 0 {
		t.Fatalf("days_left = %v depois de estourar, esperado negativo", estourada.DiasRestantes)
	}

	semSLA := faixaDeSLA(Oportunidade{}, Etapa{})
	if semSLA.DiasRestantes != nil {
		t.Fatalf("etapa sem SLA deveria devolver days_left nulo, veio %v", *semSLA.DiasRestantes)
	}
}

// ═══ Notas são atividades de tipo `nota` ═══

func TestNotasSaoSeparadasDasAtividades(t *testing.T) {
	dono := uuid.New()
	todas := []Atividade{
		{ID: uuid.New(), Tipo: AtividadeLigacao, Assunto: "Ligar", DonoID: dono},
		{ID: uuid.New(), Tipo: AtividadeNota, Assunto: "resumo", Descricao: ptr("o cliente pediu desconto"), DonoID: dono},
		{ID: uuid.New(), Tipo: AtividadeNota, Assunto: "só o assunto", DonoID: dono},
	}
	atividades, notas := separarNotas(todas)

	if len(atividades) != 1 || atividades[0].Tipo != AtividadeLigacao {
		t.Fatalf("activities = %+v; nota não pode aparecer na lista de tarefas", atividades)
	}
	if len(notas) != 2 {
		t.Fatalf("notes = %d, esperado 2", len(notas))
	}
	// O corpo da nota é a `description`; sem ela, o `subject` serve — nota sem
	// corpo nenhum na tela seria pior que nota curta.
	if notas[0].Corpo != "o cliente pediu desconto" {
		t.Errorf("corpo da nota = %q", notas[0].Corpo)
	}
	if notas[1].Corpo != "só o assunto" {
		t.Errorf("nota sem description deveria cair no subject, veio %q", notas[1].Corpo)
	}
}

// ═══ Timeline ═══

func TestTimelineOrdenaDoMaisRecenteParaOMaisAntigo(t *testing.T) {
	base := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	dono := uuid.New()
	concluida := base.Add(72 * time.Hour)

	c := OportunidadeCompleta{
		Oportunidade: Oportunidade{
			ID: uuid.New(), ContatoNome: "Fernanda", CriadoEm: base, Status: "aberta",
			EtapaID: uuid.New(),
		},
		Historico: []EventoDeEtapa{
			{ID: uuid.New(), ParaEtapaNome: "Negociação", DeEtapaNome: ptr("Novo lead"), Instante: base.Add(48 * time.Hour)},
			{ID: uuid.New(), ParaEtapaNome: "Novo lead", Instante: base},
		},
		Atividades: []Atividade{
			{ID: uuid.New(), Tipo: AtividadeLigacao, Assunto: "Primeiro contato",
				CriadoEm: base.Add(time.Hour), ConcluidaEm: &concluida, DonoID: dono},
		},
		Notas: []NotaDaOportunidade{
			{ID: uuid.New(), Corpo: "pediu 10% de desconto", CriadaEm: base.Add(24 * time.Hour)},
		},
	}

	linhas := montarTimeline(c)
	if len(linhas) != 6 {
		t.Fatalf("timeline com %d linhas, esperado 6 (criação + 2 etapas + criação e conclusão da atividade + nota)", len(linhas))
	}
	for i := 1; i < len(linhas); i++ {
		if linhas[i].Instante.After(linhas[i-1].Instante) {
			t.Fatalf("timeline fora de ordem em %d: %s vem depois de %s",
				i, linhas[i].Instante, linhas[i-1].Instante)
		}
	}
	// A conclusão é uma SEGUNDA linha, e não a substituição da primeira: o
	// intervalo entre as duas é o tempo de resposta que o §15 mede.
	if linhas[0].Tipo != LinhaAtividadeConcluida {
		t.Fatalf("a linha mais recente deveria ser a conclusão, veio %q", linhas[0].Tipo)
	}
	if linhas[len(linhas)-1].Tipo != LinhaCriada && linhas[len(linhas)-2].Tipo != LinhaCriada {
		t.Fatalf("a criação deveria estar no fim: %+v", linhas[len(linhas)-1])
	}
}

func TestAlertasNaoAcendemEmOportunidadeFechada(t *testing.T) {
	agora := time.Now()
	vencido := agora.Add(-72 * time.Hour)

	fechada := OportunidadeCompleta{
		Oportunidade: Oportunidade{Status: "ganha", SLAEstourado: true, SLAVenceEm: &vencido},
		Atividades: []Atividade{
			{Status: AtividadePendente, VenceEm: &vencido, Assunto: "Cobrar"},
		},
	}
	if alertas := montarAlertas(fechada, agora); len(alertas) != 0 {
		t.Fatalf("oportunidade ganha gerou %d alerta(s): %+v", len(alertas), alertas)
	}

	aberta := fechada
	aberta.Oportunidade.Status = "aberta"
	aberta.Oportunidade.EtapaNome = "Orçamento enviado"
	alertas := montarAlertas(aberta, agora)

	codigos := map[string]bool{}
	for _, a := range alertas {
		codigos[a.Codigo] = true
	}
	for _, esperado := range []string{AlertaSLAEstourado, AlertaTarefaVencida} {
		if !codigos[esperado] {
			t.Fatalf("faltou o alerta %q; vieram %v", esperado, codigos)
		}
	}
}

// ═══ Título derivado ═══
//
// Não existe campo `title` no contrato: o card se identifica por contato +
// produto + datas. A coluna do schema é NOT NULL, então o servidor compõe.
func TestTituloDoCardComponeContatoProdutoEDatas(t *testing.T) {
	casos := []struct {
		nome     string
		contato  string
		produto  *string
		entrada  *string
		saida    *string
		esperado string
	}{
		{"tudo", "Fernanda", ptr("White House Completa"), ptr("2026-12-28"), ptr("2027-01-02"),
			"Fernanda · White House Completa · 2026-12-28 a 2027-01-02"},
		{"só contato", "Fernanda", nil, nil, nil, "Fernanda"},
		{"sem saída", "Fernanda", nil, ptr("2026-12-28"), nil, "Fernanda · 2026-12-28"},
		{"contato vazio", "", nil, nil, nil, "Oportunidade"},
	}
	for _, caso := range casos {
		if got := tituloDoCard(caso.contato, caso.produto, caso.entrada, caso.saida); got != caso.esperado {
			t.Errorf("%s: título = %q, esperado %q", caso.nome, got, caso.esperado)
		}
	}
}

// ═══ Validação de entrada ═══

// `status: convertido` pelo PATCH deixaria o lead dizendo que virou negócio sem
// negócio nenhum do outro lado.
func TestLeadNaoSeDeclaraConvertidoPeloCorpo(t *testing.T) {
	falhas := LeadAtualizar{Status: httpx.De(LeadConvertido)}.Validar()
	if falhas["status"] == "" {
		t.Fatal("status: convertido foi aceito no corpo")
	}
	if (LeadAtualizar{Status: httpx.De(LeadQualificado)}).Validar()["status"] != "" {
		t.Fatal("um estado legítimo foi recusado")
	}
}

// `status: concluida` pelo PATCH entregaria o `done_at` ao relógio do
// navegador — e com ele o tempo médio de resposta do §15.
func TestAtividadeNaoSeDeclaraConcluidaPeloCorpo(t *testing.T) {
	if (AtividadeAtualizar{Status: httpx.De(AtividadeConcluida)}).Validar()["status"] == "" {
		t.Fatal("status: concluida foi aceito no PATCH")
	}
	if (AtividadeAtualizar{Status: httpx.De(AtividadeCancelada)}).Validar()["status"] != "" {
		t.Fatal("cancelar deveria ser aceito no PATCH")
	}
}

// Atividade solta não aparece em tela nenhuma e vira dado órfão.
func TestAtividadeExigeAoMenosUmVinculo(t *testing.T) {
	sem := AtividadeCriar{Tipo: AtividadeTarefa, Assunto: "x"}
	if sem.Validar()["opportunity_id"] == "" {
		t.Fatal("atividade sem vínculo nenhum foi aceita")
	}

	com := AtividadeCriar{Tipo: AtividadeTarefa, Assunto: "x", ContactID: httpx.De(uuid.New())}
	if len(com.Validar()) != 0 {
		t.Fatalf("atividade com contato foi recusada: %v", com.Validar())
	}
}

func TestDatasDaOportunidadeSaoMeiaAberta(t *testing.T) {
	iguais := OportunidadeCriar{
		CheckIn: httpx.De("2026-12-28"), CheckOut: httpx.De("2026-12-28"),
	}
	if iguais.Validar()["check_out"] == "" {
		t.Fatal("check_out igual a check_in foi aceito; a estadia é [in, out)")
	}
	invertidas := OportunidadeCriar{
		CheckIn: httpx.De("2026-12-30"), CheckOut: httpx.De("2026-12-28"),
	}
	if invertidas.Validar()["check_out"] == "" {
		t.Fatal("check_out antes de check_in foi aceito")
	}
	validas := OportunidadeCriar{
		CheckIn: httpx.De("2026-12-28"), CheckOut: httpx.De("2027-01-02"),
	}
	if len(validas.Validar()) != 0 {
		t.Fatalf("datas válidas recusadas: %v", validas.Validar())
	}
}

// Id repetido na lista de reordenação bate a contagem com o total do funil
// escondendo uma etapa ausente — o mesmo defeito que STAGE_ORDER_INCOMPLETE
// evita, entrando por outra porta.
func TestReordenacaoRecusaIDRepetido(t *testing.T) {
	id := uuid.New()
	pedido := PedidoDeReordenacao{FunilID: uuid.New(), EtapaIDs: []uuid.UUID{id, uuid.New(), id}}
	if pedido.Validar()["stage_ids"] == "" {
		t.Fatal("lista com id repetido foi aceita")
	}
}

func TestCorDaEtapaExigeFormatoHexadecimal(t *testing.T) {
	for _, cor := range []string{"verde", "#GG0000", "#fff", "8FA36B"} {
		if (EtapaEntrada{Cor: httpx.De(cor)}).Validar()["color"] == "" {
			t.Errorf("cor %q foi aceita", cor)
		}
	}
	for _, cor := range []string{"#8FA36B", "#000000", "#ffffff"} {
		if (EtapaEntrada{Cor: httpx.De(cor)}).Validar()["color"] != "" {
			t.Errorf("cor %q foi recusada", cor)
		}
	}
}

// `sla_days = 0` não é "SLA de hoje": é etapa cujo prazo vence no instante da
// entrada, e todo card nela nasceria estourado. Desligar o SLA é `null`.
func TestSLADiasZeroEhRecusadoENullDesliga(t *testing.T) {
	if (EtapaEntrada{SLADias: httpx.De(0)}).Validar()["sla_days"] == "" {
		t.Fatal("sla_days = 0 foi aceito")
	}
	if (EtapaEntrada{SLADias: httpx.Nulo[int]()}).Validar()["sla_days"] != "" {
		t.Fatal("sla_days = null deveria desligar o SLA, não ser erro")
	}
}

func TestListaDaQueryRecusaValorDesconhecido(t *testing.T) {
	valores, err := ListaDaQuery("status", "pendente,concluida", estadoDeAtividadeValidoParaTeste)
	if err != nil || len(valores) != 2 {
		t.Fatalf("lista válida = %v, %v", valores, err)
	}
	if _, err := ListaDaQuery("status", "pendente,inventado", estadoDeAtividadeValidoParaTeste); err == nil {
		t.Fatal("valor desconhecido foi aceito: filtro ignorado é a listagem mentindo sobre o recorte")
	}
	vazio, err := ListaDaQuery("status", "", estadoDeAtividadeValidoParaTeste)
	if err != nil || vazio != nil {
		t.Fatalf("query ausente deveria virar `sem filtro`: %v, %v", vazio, err)
	}
}

func estadoDeAtividadeValidoParaTeste(v string) bool {
	return v == AtividadePendente || v == AtividadeConcluida || v == AtividadeCancelada
}

func ptr(s string) *string { return &s }
func ptrInt(n int) *int    { return &n }
