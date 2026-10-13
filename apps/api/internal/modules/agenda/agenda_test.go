package agenda

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	regra "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Testes sem banco: a forma dos corpos, os filtros da lista, o ramo derivado,
// a ordem e o que o PUT/PATCH do automático considera mudança. A regra em si é
// do domínio, testada lá; aqui fica o que este módulo decide sozinho.

var fortaleza = calendar.MustLoadZone("America/Fortaleza")

func decodificar[T any](t *testing.T, corpo string) (T, map[string]any) {
	t.Helper()
	r := httptest.NewRequest("POST", "/", strings.NewReader(corpo))
	v, err := httpx.Decode[T](r)
	if err == nil {
		return v, nil
	}
	return v, detalhesDoErro(t, err)
}

// detalhesDoErro exige VALIDATION_ERROR e devolve os `details` como mapa.
func detalhesDoErro(t *testing.T, err error) map[string]any {
	t.Helper()
	var ap *apperr.Error
	if !errors.As(err, &ap) || ap.Code != apperr.CodeValidationError {
		t.Fatalf("esperava VALIDATION_ERROR, veio %v", err)
	}
	bruto, err := json.Marshal(ap.Details)
	if err != nil {
		t.Fatal(err)
	}
	detalhes := map[string]any{}
	if err := json.Unmarshal(bruto, &detalhes); err != nil {
		t.Fatal(err)
	}
	return detalhes
}

func TestFormaDoEventoMarcadoAMao(t *testing.T) {
	casos := []struct {
		nome, corpo string
		campos      []string
	}{
		{"sem tipo, título e início", `{}`, []string{"type", "title", "starts_at"}},
		{"check-in à mão", `{"type":"checkin","title":"x","starts_at":"2026-11-20T10:00:00-03:00"}`, []string{"type"}},
		{"manutenção à mão", `{"type":"manutencao","title":"x","starts_at":"2026-11-20T10:00:00-03:00"}`, []string{"type"}},
		{"título só espaço", `{"type":"visita","title":"   ","starts_at":"2026-11-20T10:00:00-03:00"}`, []string{"title"}},
		{"título de 201", `{"type":"visita","title":"` + strings.Repeat("á", 201) + `","starts_at":"2026-11-20T10:00:00-03:00"}`, []string{"title"}},
		{"instante sem fuso", `{"type":"visita","title":"x","starts_at":"2026-11-20T10:00:00"}`, []string{"starts_at"}},
		{"ends_at nulo", `{"type":"visita","title":"x","starts_at":"2026-11-20T10:00:00-03:00","ends_at":null}`, []string{"ends_at"}},
		{"all_day nulo", `{"type":"visita","title":"x","starts_at":"2026-11-20T10:00:00-03:00","all_day":null}`, []string{"all_day"}},
		{"notas de 2001", `{"type":"visita","title":"x","starts_at":"2026-11-20T10:00:00-03:00","notes":"` + strings.Repeat("a", 2001) + `"}`, []string{"notes"}},
		{"campo desconhecido", `{"type":"visita","title":"x","starts_at":"2026-11-20T10:00:00-03:00","sala":1}`, []string{"sala"}},
		{"chave com outra caixa", `{"type":"visita","Title":"x","starts_at":"2026-11-20T10:00:00-03:00"}`, []string{"Title"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, detalhes := decodificar[EventoDaAgendaCriar](t, c.corpo)
			for _, campo := range c.campos {
				if detalhes[campo] == nil {
					t.Errorf("faltou %q em %v", campo, detalhes)
				}
			}
		})
	}
	ok, detalhes := decodificar[EventoDaAgendaCriar](t,
		`{"type":"limpeza","title":"  Faxina  ","starts_at":"2026-11-20T10:00:00Z","assignee_id":null,"unit_id":null}`)
	if detalhes != nil {
		t.Fatalf("corpo válido recusado: %v", detalhes)
	}
	if !ok.ResponsavelID.Set || ok.ResponsavelID.Valid || ok.UnidadeID != nil {
		t.Errorf("assignee_id null tem de chegar como presente-e-nulo; unit_id null como ausente: %+v", ok)
	}
}

func TestFormaDoPUTEDoPATCH(t *testing.T) {
	_, d := decodificar[EventoDaAgendaSubstituir](t, `{"type":null,"title":null,"all_day":null}`)
	for _, campo := range []string{"type", "title", "all_day", "starts_at", "ends_at"} {
		if d[campo] == nil {
			t.Errorf("PUT: faltou %q em %v", campo, d)
		}
	}
	_, d = decodificar[EventoDaAgendaAtualizar](t, `{"starts_at":null,"ends_at":"amanhã","type":"bloqueio"}`)
	for _, campo := range []string{"starts_at", "ends_at", "type"} {
		if d[campo] == nil {
			t.Errorf("PATCH: faltou %q em %v", campo, d)
		}
	}
	vazio, d := decodificar[EventoDaAgendaAtualizar](t, `{}`)
	if d != nil || !vazio.vazio() {
		t.Errorf("PATCH vazio: %v %+v", d, vazio)
	}
	nulo, d := decodificar[EventoDaAgendaAtualizar](t, `{"notes":null}`)
	if d != nil || nulo.vazio() {
		t.Errorf("`notes: null` é limpar, não corpo vazio: %v", d)
	}
}

func TestFormaDoBloqueioDeHorario(t *testing.T) {
	_, d := decodificar[BloqueioDeHorarioCriar](t, `{"reason":"  "}`)
	for _, campo := range []string{"starts_at", "ends_at", "reason"} {
		if d[campo] == nil {
			t.Errorf("POST: faltou %q em %v", campo, d)
		}
	}
	_, d = decodificar[BloqueioDeHorarioAtualizar](t, `{"reason":null,"all_day":null,"starts_at":null}`)
	for _, campo := range []string{"reason", "all_day", "starts_at"} {
		if d[campo] == nil {
			t.Errorf("PATCH: faltou %q em %v", campo, d)
		}
	}
}

func TestFormaDaConfiguracao(t *testing.T) {
	_, d := decodificar[ConfiguracaoDaAgendaEntrada](t,
		`{"checkin_time":"14h","checkout_time":"9:00","cleaning_start":"11:00","business_hours":[{"start":"08:00","end":"24:30"}]}`)
	for _, campo := range []string{"checkin_time", "checkout_time", "cleaning_end", "slot_minutes", "buffer_minutes",
		"business_hours[0].weekday", "business_hours[0].end"} {
		if d[campo] == nil {
			t.Errorf("faltou %q em %v", campo, d)
		}
	}
	_, d = decodificar[ConfiguracaoDaAgendaEntrada](t, `{"checkin_time":"14:00","timezone":"UTC"}`)
	if d["timezone"] == nil {
		t.Errorf("timezone é só leitura: %v", d)
	}

	intervalos := make([]string, maximoDeIntervalos+1)
	for i := range intervalos {
		intervalos[i] = `{"weekday":1,"start":"08:00","end":"09:00"}`
	}
	_, d = decodificar[ConfiguracaoDaAgendaEntrada](t, `{"checkin_time":"14:00","checkout_time":"11:00",
		"cleaning_start":"11:00","cleaning_end":"14:00","slot_minutes":30,"buffer_minutes":15,
		"business_hours":[`+strings.Join(intervalos, ",")+`]}`)
	if d["business_hours"] == nil {
		t.Errorf("mais de %d intervalos: %v", maximoDeIntervalos, d)
	}

	ok, d := decodificar[ConfiguracaoDaAgendaEntrada](t, `{"checkin_time":"15:00","checkout_time":"12:00",
		"cleaning_start":"12:00","cleaning_end":"15:00","slot_minutes":30,"buffer_minutes":0,
		"business_hours":[{"weekday":7,"start":"18:00","end":"24:00"},{"weekday":1,"start":"08:00","end":"20:00"}]}`)
	if d != nil {
		t.Fatalf("corpo válido recusado: %v", d)
	}
	s, err := ok.dominio()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("o domínio recusou o que a forma aceitou: %v", err)
	}
	if s.CheckIn != calendar.MustParseClock("15:00") || s.BufferMinutes != 0 || s.BusinessHours[0].End != calendar.EndOfDay {
		t.Errorf("conversão: %+v", s)
	}

	// O jsonb grava em ordem de dia e início, e volta igual.
	bruto, err := horarioParaOBanco(s.BusinessHours)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(bruto), `[{"weekday":1,"start":"08:00","end":"20:00"}`) {
		t.Errorf("ordem do jsonb: %s", bruto)
	}
	volta, err := horarioDoBanco(bruto)
	if err != nil || len(volta) != 2 || volta[1].End != calendar.EndOfDay {
		t.Errorf("volta do jsonb: %+v %v", volta, err)
	}
	if _, err := horarioDoBanco([]byte(`[{"weekday":1,"start":"8h","end":"20:00"}]`)); err == nil {
		t.Error("hora ilegível no banco tem de ser erro")
	}
}

// ─────────────────────────── Filtros da lista ───────────────────────

func filtro(t *testing.T, consulta string) (FiltroDeEventos, map[string]any) {
	t.Helper()
	f, err := filtroDeEventos(httptest.NewRequest("GET", "/agenda/events?"+consulta, nil))
	if err == nil {
		return f, nil
	}
	return f, detalhesDoErro(t, err)
}

func TestFiltroPadraoDaLista(t *testing.T) {
	f, d := filtro(t, "from=2026-11-01&to=2026-12-01")
	if d != nil {
		t.Fatal(d)
	}
	if f.De.String() != "2026-11-01" || f.Ate.String() != "2026-12-01" || f.Decrescente ||
		len(f.Status) != 2 || f.Status[0] != regra.Scheduled || f.Status[1] != regra.Done || f.PorPagina != 25 {
		t.Errorf("padrão: %+v", f)
	}
	f, d = filtro(t, "reservation_id="+uuid.NewString()+"&type=checkin,%20checkout&status=cancelado&sort=-starts_at&unassigned=true")
	if d != nil {
		t.Fatal(d)
	}
	if f.De != nil || len(f.Tipos) != 2 || f.Tipos[1] != regra.CheckOut || len(f.Status) != 1 || !f.Decrescente ||
		f.SemResponsavel == nil || !*f.SemResponsavel {
		t.Errorf("com reserva, sem janela: %+v", f)
	}
}

func TestFiltroInvalidoNuncaViraSemFiltro(t *testing.T) {
	janela := "from=2026-11-01&to=2026-11-08&"
	casos := map[string]string{
		"":                              "from",
		"from=2026-11-01":               "to",
		"from=2026-11-01&to=2026-11-01": "to",
		"from=2026-11-01&to=2027-02-02": "to",
		"from=01/11/2026&to=2026-11-08": "from",
		janela + "unassigned=sim":       "unassigned",
		janela + "unassigned=1":         "unassigned",
		janela + "unassigned=0":         "unassigned",
		janela + "unassigned=t":         "unassigned",
		janela + "unassigned=TRUE":      "unassigned",
		janela + "unassigned=False":     "unassigned",
		janela + "unassigned=true&assignee_id=" + uuid.NewString(): "unassigned",
		janela + "type=xpto":       "type",
		janela + "type=,":          "type",
		janela + "status=pendente": "status",
		janela + "sort=title":      "sort",
		janela + "unit_id=abc":     "unit_id",
		"reservation_id=abc":       "reservation_id",
	}
	for consulta, campo := range casos {
		if _, d := filtro(t, consulta); d[campo] == nil {
			t.Errorf("%q: faltou %q em %v", consulta, campo, d)
		}
	}
	if _, d := filtro(t, "from=2026-11-01&to=2027-02-01"); d != nil {
		t.Errorf("92 dias cabem: %v", d)
	}
	if f, d := filtro(t, janela+"unassigned=false"); d != nil || f.SemResponsavel == nil || *f.SemResponsavel {
		t.Errorf("unassigned=false: %+v %v", f.SemResponsavel, d)
	}
}

// ─────────────────────────── O ramo derivado ────────────────────────

func TestQuandoORamoDerivadoEntra(t *testing.T) {
	de, ate := calendar.MustParse("2026-11-01"), calendar.MustParse("2026-11-08")
	base := FiltroDeEventos{De: &de, Ate: &ate, Status: statusPadrao()}
	eu := uuid.New()
	falso := false
	casos := []struct {
		nome   string
		c      consultaDaLista
		fontes []string
	}{
		{"padrão", consultaDaLista{FiltroDeEventos: base}, []string{"maintenance", "owner_hold", "ota"}},
		{"só manutenção", consultaDaLista{FiltroDeEventos: comTipos(base, regra.Maintenance)}, []string{"maintenance"}},
		{"só bloqueio", consultaDaLista{FiltroDeEventos: comTipos(base, regra.Block, regra.Visit)}, []string{"owner_hold", "ota"}},
		{"só visita", consultaDaLista{FiltroDeEventos: comTipos(base, regra.Visit)}, nil},
		{"own", consultaDaLista{FiltroDeEventos: base, Responsavel: &eu}, nil},
		{"por responsável", consultaDaLista{FiltroDeEventos: comResponsavel(base, eu)}, nil},
		{"com responsável", consultaDaLista{FiltroDeEventos: comSemResponsavel(base, &falso)}, nil},
		{"só cancelados", consultaDaLista{FiltroDeEventos: comStatus(base, regra.Cancelled)}, nil},
		{"detalhe", consultaDaLista{FiltroDeEventos: base, SoOEvento: &eu}, nil},
		{"sem janela", consultaDaLista{}, nil},
	}
	for _, c := range casos {
		fontes, ok := c.c.fontesDerivadas()
		if ok != (c.fontes != nil) || strings.Join(fontes, ",") != strings.Join(c.fontes, ",") {
			t.Errorf("%s: %v %v, esperado %v", c.nome, fontes, ok, c.fontes)
		}
	}
}

func comTipos(f FiltroDeEventos, ts ...regra.Type) FiltroDeEventos { f.Tipos = ts; return f }
func comStatus(f FiltroDeEventos, ss ...regra.Status) FiltroDeEventos {
	f.Status = ss
	return f
}
func comResponsavel(f FiltroDeEventos, id uuid.UUID) FiltroDeEventos { f.ResponsavelID = &id; return f }
func comSemResponsavel(f FiltroDeEventos, v *bool) FiltroDeEventos   { f.SemResponsavel = v; return f }

// O derivado é convertido pela zona da casa e entra na mesma ordem dos
// gravados — por início, empate por id, nos dois sentidos.
func TestOrdemDaListaMisturaGravadosEDerivados(t *testing.T) {
	d := calendar.MustParse("2026-11-20")
	idA, idB := uuid.MustParse("00000000-0000-0000-0000-00000000000a"), uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	idC := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	linhas := []linhaDaLista{
		{ID: idC, Inicio: fortaleza.At(d, calendar.MustParseClock("14:00")), Fim: fortaleza.At(d, calendar.MustParseClock("14:30"))},
		{ID: idB, Derivado: true, De: "2026-11-20", Ate: "2026-11-22"},
		{ID: idA, Inicio: fortaleza.StartOfDay(d), Fim: fortaleza.At(d, calendar.MustParseClock("01:00"))},
	}
	if err := instantesDosDerivados(linhas, fortaleza); err != nil {
		t.Fatal(err)
	}
	if !linhas[1].Inicio.Equal(fortaleza.StartOfDay(d)) || !linhas[1].Fim.Equal(fortaleza.StartOfDay(d.AddDays(2))) {
		t.Fatalf("derivado: %s–%s", linhas[1].Inicio, linhas[1].Fim)
	}
	if h := linhas[1].Inicio.UTC().Hour(); h != 3 {
		t.Errorf("a meia-noite da casa é 03:00 UTC; veio %d", h)
	}
	ordenar(linhas, false)
	if linhas[0].ID != idA || linhas[1].ID != idB || linhas[2].ID != idC {
		t.Errorf("crescente, empate às 00:00 desempatado por id: %v %v %v", linhas[0].ID, linhas[1].ID, linhas[2].ID)
	}
	ordenar(linhas, true)
	if linhas[0].ID != idC || linhas[1].ID != idB || linhas[2].ID != idA {
		t.Errorf("decrescente: %v %v %v", linhas[0].ID, linhas[1].ID, linhas[2].ID)
	}
}

func TestTitulos(t *testing.T) {
	codigo, titulo, nota := "WH-2026-0007", "Faxina", "Uso do dono"
	if got := tituloDoGravado(linhaDaLista{Tipo: "checkin", Origem: "reservation", ReservaCodigo: &codigo}); got != "Check-in · WH-2026-0007" {
		t.Errorf("automático: %q", got)
	}
	if got := tituloDoGravado(linhaDaLista{Tipo: "limpeza", Origem: "manual", Titulo: &titulo}); got != titulo {
		t.Errorf("manual: %q", got)
	}
	if got := tituloDoDerivado(linhaDaLista{Tipo: "bloqueio", Nota: &nota}); got != nota {
		t.Errorf("bloqueio com nota: %q", got)
	}
	if got := tituloDoDerivado(linhaDaLista{Tipo: "manutencao"}); got != "Manutenção" {
		t.Errorf("manutenção sem ordem nem nota: %q", got)
	}
}

// ─────────────────────────── O que é mudança ────────────────────────

// Reenviar o automático como foi lido não é mudança: o título composto, o
// titular e a unidade da reserva passam; diferentes viram identidade (que o
// domínio recusa) e NUNCA chegam à linha gravada.
func TestPropostaDoAutomaticoNaoGravaIdentidade(t *testing.T) {
	reserva, contato, unidade := uuid.New(), uuid.New(), uuid.New()
	antes := eventoGravado{ID: uuid.New(), Tipo: "checkin", Origem: "reservation", ReservaID: &reserva, Status: "agendado"}
	ex := exibido{titulo: "Check-in · WH-1", contato: &contato, unidades: map[uuid.UUID]bool{unidade: true}}

	p := proposta{alvo: antes}
	p.titulo("  Check-in · WH-1 ", ex)
	p.contato(&contato, ex)
	p.unidade(&unidade, ex)
	p.reserva(&reserva)
	p.diaInteiro(false)
	p.tipo("checkin")
	if len(p.identidade) != 0 {
		t.Fatalf("o eco do automático virou mudança: %v", p.identidade)
	}

	// `null` é DIFERENTE do titular e das unidades que o evento mostra — ainda
	// que a coluna gravada seja nula.
	nulos := proposta{alvo: antes}
	nulos.contato(nil, ex)
	nulos.unidade(nil, ex)
	if strings.Join(nulos.identidade, ",") != "contact_id,unit_id" || nulos.alvo.ContatoID != nil || nulos.alvo.UnidadeID != nil {
		t.Fatalf("null no automático: %v %+v", nulos.identidade, nulos.alvo)
	}
	semUnidades := proposta{alvo: antes}
	semUnidades.unidade(nil, exibido{unidades: map[uuid.UUID]bool{}})
	if len(semUnidades.identidade) != 0 {
		t.Fatalf("sem unidade exibida, null é repetição: %v", semUnidades.identidade)
	}

	outro := uuid.New()
	p.titulo("Outro", ex)
	p.contato(&outro, ex)
	p.unidade(&outro, ex)
	p.reserva(nil)
	p.diaInteiro(true)
	p.tipo("limpeza")
	if strings.Join(p.identidade, ",") != "title,contact_id,unit_id,reservation_id,all_day,type" {
		t.Fatalf("identidade: %v", p.identidade)
	}
	if p.alvo.Titulo != nil || p.alvo.ContatoID != nil || p.alvo.UnidadeID != nil || p.alvo.ReservaID != &reserva ||
		p.alvo.DiaInteiro || p.alvo.Tipo != "checkin" {
		t.Fatalf("a identidade do automático não pode chegar à linha: %+v", p.alvo)
	}
}

func TestPropostaDoManualAplicaOQueMuda(t *testing.T) {
	titulo, unidade := "Visita", uuid.New()
	antes := eventoGravado{ID: uuid.New(), Tipo: "visita", Origem: "manual", Titulo: &titulo, UnidadeID: &unidade, Status: "agendado"}
	p := proposta{alvo: antes}
	p.titulo(" Visita ", exibido{})
	p.unidade(&unidade, exibido{})
	if len(p.identidade) != 0 {
		t.Fatalf("repetir não é mudança: %v", p.identidade)
	}
	p.unidade(nil, exibido{})
	p.tipo("evento")
	if strings.Join(p.identidade, ",") != "unit_id,type" || p.alvo.UnidadeID != nil || p.alvo.Tipo != "evento" {
		t.Fatalf("manual: %v %+v", p.identidade, p.alvo)
	}
}

func TestCampoDoResponsavel(t *testing.T) {
	id := uuid.New()
	if f := campo(httpx.Opt[uuid.UUID]{}); f.Present || f.Value != nil {
		t.Errorf("ausente: %+v", f)
	}
	if f := campo(httpx.Nulo[uuid.UUID]()); !f.Present || f.Value != nil {
		t.Errorf("null: %+v", f)
	}
	if f := campo(httpx.De(id)); !f.Present || f.Value == nil || *f.Value != id {
		t.Errorf("valor: %+v", f)
	}
}

func TestAuditavelNormalizaOFuso(t *testing.T) {
	em := time.Date(2026, 11, 20, 14, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	e := eventoGravado{Inicio: em, Fim: em.Add(time.Hour), ConcluidoEm: &em}.auditavel()
	if e.Inicio.Location() != time.UTC || e.ConcluidoEm.Location() != time.UTC || !e.Inicio.Equal(em) {
		t.Errorf("auditável: %+v", e)
	}
}
