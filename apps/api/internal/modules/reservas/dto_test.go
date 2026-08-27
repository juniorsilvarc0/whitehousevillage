package reservas

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

func optAusente() httpx.Opt[string]       { return httpx.Opt[string]{} }
func optNulo() httpx.Opt[string]          { return httpx.Nulo[string]() }
func optValor(v string) httpx.Opt[string] { return httpx.De(v) }

func TestReservaCriarValidaSoOFormato(t *testing.T) {
	corpo := ReservaCriar{
		UnitTypeID: uuid.New(), ContactID: uuid.New(),
		CheckIn: "20/11/2026", CheckOut: "2026-11-23", Hospedes: 2,
	}
	falhas := corpo.Validar()
	if falhas["check_in"] == "" {
		t.Fatalf("data em formato brasileiro deveria falhar: %v", falhas)
	}
	if falhas["check_out"] != "" {
		t.Fatalf("data ISO válida não deveria falhar: %v", falhas)
	}

	// check_out ANTES de check_in passa aqui de propósito: quem recusa é
	// booking.Build, e duplicar a regra criaria dois lugares para o mesmo "não".
	invertido := ReservaCriar{
		UnitTypeID: uuid.New(), ContactID: uuid.New(),
		CheckIn: "2026-11-23", CheckOut: "2026-11-20", Hospedes: 2,
	}
	if len(invertido.Validar()) != 0 {
		t.Fatalf("a ordem das datas é regra do motor, não do DTO: %v", invertido.Validar())
	}
}

func TestOrigemPadraoEhDireto(t *testing.T) {
	corpo := ReservaCriar{Origem: "   "}
	corpo.Normalizar()
	if corpo.Origem != "direto" {
		t.Fatalf("source = %q, esperado direto", corpo.Origem)
	}

	vazio := "   "
	comEspaco := ReservaCriar{Origem: "ota", TipoEvento: &vazio}
	comEspaco.Normalizar()
	if comEspaco.Origem != "ota" {
		t.Fatalf("source informado foi sobrescrito: %q", comEspaco.Origem)
	}
	if comEspaco.TipoEvento != nil {
		t.Fatalf("texto em branco deveria virar nulo, veio %q", *comEspaco.TipoEvento)
	}
}

func TestBloqueioExigeJanelaHalfOpenEOrigemOperacional(t *testing.T) {
	base := BloqueioCriar{Unidades: []uuid.UUID{uuid.New()}, De: "2026-05-10", Ate: "2026-05-12", Origem: OrigemManutencao}
	if falhas := base.Validar(); len(falhas) != 0 {
		t.Fatalf("bloqueio válido recusado: %v", falhas)
	}

	mesmoDia := base
	mesmoDia.Ate = mesmoDia.De
	if mesmoDia.Validar()["to"] == "" {
		t.Fatal("`to` igual a `from` não bloqueia noite nenhuma e tem de ser recusado")
	}

	// `ota` é do importador de canais e `reservation` nasce com a reserva:
	// nenhum dos dois se cria por esta rota.
	for _, origem := range []string{"ota", OrigemReserva, "", "MAINTENANCE"} {
		invalida := base
		invalida.Origem = origem
		if invalida.Validar()["source"] == "" {
			t.Errorf("source %q deveria ser recusado", origem)
		}
	}

	repetida := base
	u := uuid.New()
	repetida.Unidades = []uuid.UUID{u, u}
	if repetida.Validar()["unit_ids"] == "" {
		t.Fatal("unidade repetida colidiria consigo mesma na constraint")
	}
}

// A DURAÇÃO tem teto — o MÉDIO da segunda revisão. `2040-01-01 → 2050-01-01`
// nas oito unidades passou com 201 e tirou 29.224 noites-unidade do mercado.
// O horizonte (`from` longe demais) é a outra metade e vive no service, porque
// depende de HOJE no fuso da casa.
func TestBloqueioTemDuracaoMaxima(t *testing.T) {
	base := BloqueioCriar{Unidades: []uuid.UUID{uuid.New()}, Origem: OrigemProprietario}

	decada := base
	decada.De, decada.Ate = "2040-01-01", "2050-01-01"
	if decada.Validar()["to"] == "" {
		t.Fatal("uma década numa tecla continua passando")
	}

	// O limite é inclusivo dos dois lados: um ano cheio passa, um dia a mais não.
	noLimite := base
	noLimite.De = "2026-05-10"
	noLimite.Ate = calendar.MustParse(noLimite.De).AddDays(tetoDeNoitesPorBloqueio).String()
	if falhas := noLimite.Validar(); len(falhas) != 0 {
		t.Fatalf("%d noites deveriam passar: %v", tetoDeNoitesPorBloqueio, falhas)
	}

	umDiaAMais := noLimite
	umDiaAMais.Ate = calendar.MustParse(umDiaAMais.De).AddDays(tetoDeNoitesPorBloqueio + 1).String()
	if umDiaAMais.Validar()["to"] == "" {
		t.Fatalf("%d noites deveriam ser recusadas", tetoDeNoitesPorBloqueio+1)
	}
}

func TestStatusDaQueryRecusaEstadoInventado(t *testing.T) {
	got, err := StatusDaQuery(" hold , confirmed ")
	if err != nil {
		t.Fatalf("filtro válido recusado: %v", err)
	}
	if len(got) != 2 || got[0] != EstadoHold || got[1] != EstadoConfirmada {
		t.Fatalf("status = %v", got)
	}

	if _, err := StatusDaQuery("hold,pendente"); err == nil {
		t.Fatal("estado inventado deveria ser 422, nunca ignorado em silêncio")
	} else if apperr.From(err).Code != "VALIDATION_ERROR" {
		t.Fatalf("code = %s", apperr.From(err).Code)
	}

	vazio, err := StatusDaQuery("")
	if err != nil || vazio != nil {
		t.Fatalf("filtro ausente = (%v, %v), esperado (nil, nil)", vazio, err)
	}
}

func TestChaveDeIdempotenciaEhObrigatoriaEDelimitada(t *testing.T) {
	if _, err := ChaveDeIdempotencia("   "); err == nil {
		t.Fatal("chave ausente tem de ser recusada: criar reserva é irreversível")
	}
	if _, err := ChaveDeIdempotencia("curta"); err == nil {
		t.Fatal("chave abaixo de 8 caracteres deveria ser recusada (minLength do contrato)")
	}
	chave, err := ChaveDeIdempotencia("  chave-de-teste-0001  ")
	if err != nil || chave != "chave-de-teste-0001" {
		t.Fatalf("chave = %q, err = %v", chave, err)
	}
}

// A impressão do corpo é o que decide entre "repetição" e IDEMPOTENCY_MISMATCH.
// Ela tem de ignorar formatação e reagir a CONTEÚDO.
func TestImpressaoIgnoraFormatacaoEReageAConteudo(t *testing.T) {
	produto, contato := uuid.New(), uuid.New()
	a := ReservaCriar{UnitTypeID: produto, ContactID: contato, CheckIn: "2026-11-20", CheckOut: "2026-11-23", Hospedes: 2}
	b := a

	ha, err := impressao(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, _ := impressao(b)
	if ha != hb {
		t.Fatal("o mesmo pedido produziu impressões diferentes")
	}

	c := a
	c.Hospedes = 3
	hc, _ := impressao(c)
	if ha == hc {
		t.Fatal("mudar o número de hóspedes tem de mudar a impressão")
	}

	// Mesmos campos, ordem diferente no JSON de origem: o hash é do DTO
	// decodificado, então isso é o MESMO pedido.
	var d ReservaCriar
	bruto := `{"guests_count":2,"check_out":"2026-11-23","check_in":"2026-11-20","contact_id":"` +
		contato.String() + `","unit_type_id":"` + produto.String() + `"}`
	if err := json.Unmarshal([]byte(bruto), &d); err != nil {
		t.Fatal(err)
	}
	hd, _ := impressao(d)
	if ha != hd {
		t.Fatal("a ordem dos campos no JSON não pode mudar a impressão")
	}
}

// As linhas do /full agrupam o SNAPSHOT, sem recalcular nada.
func TestAgruparEmLinhasPreservaOSnapshot(t *testing.T) {
	noites := []NoiteDaReserva{
		{Noite: "2026-11-20", Tipo: calendar.Weekend, Preco: 235000},
		{Noite: "2026-11-21", Tipo: calendar.Weekend, Preco: 235000},
		{Noite: "2026-11-22", Tipo: calendar.Normal, Preco: 235000},
	}
	linhas := AgruparEmLinhas(noites)
	if len(linhas) != 2 {
		t.Fatalf("linhas = %d, esperado 2", len(linhas))
	}
	// A ordem é a de APARIÇÃO, para o orçamento sair na ordem em que o hóspede
	// vive a estadia.
	if linhas[0].Tipo != calendar.Weekend || linhas[0].Noites != 2 || linhas[0].Subtotal != 470000 {
		t.Fatalf("linha de fds errada: %+v", linhas[0])
	}
	if linhas[1].Tipo != calendar.Normal || linhas[1].Noites != 1 || linhas[1].Subtotal != 235000 {
		t.Fatalf("linha normal errada: %+v", linhas[1])
	}
	if linhas[0].Rotulo != "Fim de semana" {
		t.Fatalf("rótulo = %q", linhas[0].Rotulo)
	}
}

func TestTextoOpcionalDistingueAusenteDeNulo(t *testing.T) {
	atual := "casamento"
	if got := textoOpcional(&atual, optAusente()); got == nil || *got != atual {
		t.Fatal("campo ausente tem de manter o que está gravado")
	}
	if got := textoOpcional(&atual, optNulo()); got != nil {
		t.Fatalf("null tem de limpar, veio %q", *got)
	}
	if got := textoOpcional(&atual, optValor("aniversario")); got == nil || *got != "aniversario" {
		t.Fatal("valor tem de trocar")
	}
}
