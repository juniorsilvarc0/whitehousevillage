//go:build integration

// Testes de integração do módulo de disponibilidade.
//
// Aqui não há duplo: o Postgres é real, o seed é o do repositório e a Tabela
// Comercial V1 sai do banco. É o que os testes unitários NÃO conseguem provar —
// eles garantem que a montagem está certa dado um estado; estes garantem que o
// estado que o banco carrega é o que a montagem espera.
package disponibilidade_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

type ambiente struct {
	pool        *pgxpool.Pool
	svc         *disponibilidade.Servico
	ctx         context.Context
	propriedade uuid.UUID
	usuario     uuid.UUID
}

func subir(t *testing.T, escopo string) *ambiente {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado")
	}

	pool, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	t.Cleanup(pool.Close)

	var propriedade, usuario uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM properties WHERE slug = 'white-house-village'`).Scan(&propriedade); err != nil {
		t.Fatalf("propriedade do seed não encontrada (rodou cmd/seed?): %v", err)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM users ORDER BY created_at LIMIT 1`).Scan(&usuario); err != nil {
		t.Fatalf("usuário do seed não encontrado: %v", err)
	}

	u := &auth.Usuario{
		ID:         usuario,
		PropertyID: propriedade,
		Permissoes: auth.NovoConjunto([]auth.Permissao{
			{Resource: "calendar", Action: auth.AcaoVer, Scope: escopo},
			{Resource: "quotes", Action: auth.AcaoCriar, Scope: escopo},
			// `ver` entrou com o orçamento persistido: `GET /quotes/{id}`
			// responde por ele, e o escopo dele é o que filtra por `owner_id`.
			{Resource: "quotes", Action: auth.AcaoVer, Scope: escopo},
		}),
	}

	return &ambiente{
		pool:        pool,
		svc:         disponibilidade.NovoServico(disponibilidade.NewRepository(pool), db.NewTxManager(pool)),
		ctx:         auth.WithUser(context.Background(), u),
		propriedade: propriedade,
		usuario:     usuario,
	}
}

func (a *ambiente) produto(t *testing.T, codigo string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM unit_types WHERE property_id = $1 AND code = $2`, a.propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("produto %q: %v", codigo, err)
	}
	return id
}

func (a *ambiente) unidade(t *testing.T, codigo string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM units WHERE property_id = $1 AND code = $2`, a.propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("unidade %q: %v", codigo, err)
	}
	return id
}

// ocupar cria uma reserva confirmada segurando UMA unidade no período, do jeito
// que o módulo de reservas fará: a linha de `stay_blocks` é a ocupação real, e é
// ela que a constraint `stay_no_overlap` protege.
func (a *ambiente) ocupar(t *testing.T, unidade uuid.UUID, de, ate string, dono uuid.UUID) (reserva uuid.UUID, codigo string) {
	t.Helper()
	return a.ocuparCom(t, unidade, de, ate, dono, "confirmed", "confirmed")
}

// ocuparCom é a ocupar com o estado escolhido, para os cenários em que o estado
// É o assunto — a estadia CUMPRIDA (`checked_out` na reserva, `completed` no
// bloco) é o caso que separa o predicado de venda do predicado de exibição.
func (a *ambiente) ocuparCom(t *testing.T, unidade uuid.UUID, de, ate string, dono uuid.UUID,
	estadoDaReserva, estadoDoBloco string) (reserva uuid.UUID, codigo string) {
	t.Helper()

	var contato uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, email)
		VALUES ($1, 'Hóspede de Integração', 'integracao@exemplo.invalid')
		RETURNING id`, a.propriedade).Scan(&contato); err != nil {
		t.Fatalf("criando contato: %v", err)
	}

	// `code` é omitido de propósito: o DEFAULT da coluna chama
	// proximo_codigo_reserva(), e passar código pronto não faria o contador andar.
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO reservations (property_id, unit_type_id, contact_id, status,
		                          check_in, check_out, guests_count, owner_id)
		SELECT $1, ut.id, $2, $6, $3::date, $4::date, 2, $5
		  FROM unit_types ut
		 WHERE ut.property_id = $1 AND ut.code = 'apto-2s'
		RETURNING id, code`, a.propriedade, contato, de, ate, dono, estadoDaReserva).Scan(&reserva, &codigo); err != nil {
		t.Fatalf("criando reserva: %v", err)
	}

	var bloco uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO stay_blocks (property_id, unit_id, reservation_id, source, status, period)
		VALUES ($1, $2, $3, 'reservation', $6, daterange($4::date, $5::date, '[)'))
		RETURNING id`, a.propriedade, unidade, reserva, de, ate, estadoDoBloco).Scan(&bloco); err != nil {
		t.Fatalf("criando stay_block: %v", err)
	}

	t.Cleanup(func() {
		limpeza := context.Background()
		_, _ = a.pool.Exec(limpeza, `DELETE FROM stay_blocks WHERE id = $1`, bloco)
		_, _ = a.pool.Exec(limpeza, `DELETE FROM reservations WHERE id = $1`, reserva)
		_, _ = a.pool.Exec(limpeza, `DELETE FROM contacts WHERE id = $1`, contato)
	})
	return reserva, codigo
}

func janela(t *testing.T, de, ate string) disponibilidade.Janela {
	t.Helper()
	j, err := disponibilidade.NovaJanela(de, ate)
	if err != nil {
		t.Fatalf("NovaJanela(%s, %s): %v", de, ate, err)
	}
	return j
}

// ─────────────────────────── Tabela Comercial V1 ────────────────────────────

// O cenário demonstrado aos proprietários, agora contra o BANCO: a Cobertura de
// 20 a 23/11/2026 fecha em R$ 7.050 com sinal de R$ 3.525.
//
// Se este teste cair e o unitário equivalente passar, o defeito está no seed ou
// na leitura do tarifário — não no motor.
func TestCoberturaDeNovembroFechaEmSeteMilECinquentaNoBanco(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	o, err := a.svc.Orcar(a.ctx, disponibilidade.Entrada{
		UnitTypeID: a.produto(t, "cobertura"),
		CheckIn:    calendar.MustParse("2026-11-20"),
		CheckOut:   calendar.MustParse("2026-11-23"),
		Hospedes:   8,
	})
	if err != nil {
		t.Fatalf("Orcar: %v", err)
	}

	if o.Total != 705000 {
		t.Errorf("total = %s, quero R$ 7.050,00", money.Cents(o.Total))
	}
	if o.Sinal != 352500 {
		t.Errorf("sinal = %s, quero R$ 3.525,00", money.Cents(o.Sinal))
	}
	if o.Saldo != 352500 {
		t.Errorf("saldo = %s, quero R$ 3.525,00", money.Cents(o.Saldo))
	}
	if o.Subtotal != 670000 || o.Limpeza != 35000 {
		t.Errorf("subtotal = %s / limpeza = %s", money.Cents(o.Subtotal), money.Cents(o.Limpeza))
	}
	if o.MediaPorNoite != 235000 {
		t.Errorf("diária média = %s, quero R$ 2.350,00", money.Cents(o.MediaPorNoite))
	}
	if o.PolicyVersion != 1 || o.RateTableID == uuid.Nil {
		t.Errorf("o orçamento não congelou o que usou: versão %d, tabela %v", o.PolicyVersion, o.RateTableID)
	}

	// Sexta e sábado são fds; domingo é diária normal — o hóspede vai embora.
	quero := []calendar.DateType{calendar.Weekend, calendar.Weekend, calendar.Normal}
	for i, n := range o.Diarias {
		if n.Tipo != quero[i] {
			t.Errorf("noite %s = %q, quero %q", n.Data, n.Tipo, quero[i])
		}
	}
}

// Réveillon atravessando o ano (docs/spec.md §3): 28/12/2026 a 03/01/2027 sai
// com as seis noites em `reveillon`, inclusive 01/01, que é feriado nacional
// dentro da alta temporada. A precedência do banco resolve.
func TestReveillonAtravessaOAnoNoBanco(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	o, err := a.svc.Orcar(a.ctx, disponibilidade.Entrada{
		UnitTypeID: a.produto(t, "completa"),
		CheckIn:    calendar.MustParse("2026-12-28"),
		CheckOut:   calendar.MustParse("2027-01-03"),
		Hospedes:   20,
	})
	if err != nil {
		t.Fatalf("Orcar: %v", err)
	}
	if len(o.Diarias) != 6 {
		t.Fatalf("noites = %d, quero 6", len(o.Diarias))
	}
	for _, n := range o.Diarias {
		if n.Tipo != calendar.NewYear {
			t.Errorf("noite %s = %q, quero \"reveillon\"", n.Data, n.Tipo)
		}
		if n.Preco != 2100000 {
			t.Errorf("noite %s = %s, quero R$ 21.000,00", n.Data, money.Cents(n.Preco))
		}
	}
	if o.MinNoites != 4 {
		t.Errorf("min_nights = %d, quero 4", o.MinNoites)
	}
	if o.Total != 6*2100000+90000 {
		t.Errorf("total = %s", money.Cents(o.Total))
	}
}

// Estadia de 2 noites no réveillon é recusada com o mínimo do período.
func TestReveillonExigeQuatroNoites(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	_, err := a.svc.Orcar(a.ctx, disponibilidade.Entrada{
		UnitTypeID: a.produto(t, "completa"),
		CheckIn:    calendar.MustParse("2026-12-28"),
		CheckOut:   calendar.MustParse("2026-12-30"),
		Hospedes:   10,
	})
	e := apperr.From(err)
	if e.Code != "MIN_STAY_NOT_MET" {
		t.Fatalf("code = %q, quero MIN_STAY_NOT_MET (erro: %v)", e.Code, err)
	}
	if e.Status() != 422 {
		t.Errorf("status = %d, quero 422", e.Status())
	}
}

// ─────────────────────────── A Completa e as oito unidades ──────────────────

// UMA unidade ocupada fecha a White House Completa — e a regra sai da
// composição real do banco (`unit_type_members`), não de código.
func TestUmaUnidadeOcupadaFechaACompleta(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	const (
		de  = "2027-03-10"
		ate = "2027-03-13"
	)
	a.ocupar(t, a.unidade(t, "AP-01"), de, ate, a.usuario)

	linhas, err := a.svc.PorProduto(a.ctx, janela(t, "2027-03-09", "2027-03-14"), nil)
	if err != nil {
		t.Fatalf("PorProduto: %v", err)
	}

	porCodigo := map[string]disponibilidade.DisponibilidadeDoProduto{}
	for _, l := range linhas {
		porCodigo[l.UnitTypeCode] = l
	}
	if len(porCodigo) != 4 {
		t.Fatalf("produtos = %d, quero 4", len(porCodigo))
	}

	completa := porCodigo["completa"]
	if completa.TotalUnidades != 8 {
		t.Errorf("a Completa tem %d unidades, quero 8", completa.TotalUnidades)
	}
	esperado := map[string]int{
		"2027-03-09": 1, // véspera livre
		"2027-03-10": 0, // AP-01 ocupada
		"2027-03-11": 0,
		"2027-03-12": 0,
		"2027-03-13": 1, // o dia do check-out já volta a ser vendável (back-to-back)
	}
	for _, d := range completa.Dias {
		if quero, ok := esperado[d.Data]; ok && d.Disponivel != quero {
			t.Errorf("Completa em %s: available = %d, quero %d", d.Data, d.Disponivel, quero)
		}
		if d.Disponivel > 1 {
			t.Errorf("Completa em %s: available = %d — all_members é 0 ou 1", d.Data, d.Disponivel)
		}
	}

	// O apartamento, no mesmo dia, ainda tem duas das três unidades: vender um
	// não fecha o grupo, fecha só a Completa.
	apto := porCodigo["apto-2s"]
	if apto.TotalUnidades != 3 {
		t.Errorf("apto-2s tem %d unidades, quero 3", apto.TotalUnidades)
	}
	for _, d := range apto.Dias {
		quero := 3
		if d.Data >= de && d.Data < ate {
			quero = 2
		}
		if d.Disponivel != quero {
			t.Errorf("apto-2s em %s: available = %d, quero %d", d.Data, d.Disponivel, quero)
		}
	}

	// A Cobertura é uma unidade só e não foi tocada.
	for _, d := range porCodigo["cobertura"].Dias {
		if d.Disponivel != 1 {
			t.Errorf("cobertura em %s: available = %d, quero 1", d.Data, d.Disponivel)
		}
	}
}

// A célula do mapa traz preço nenhum, mas traz tudo que o mapa de ocupação
// precisa: bloco, reserva, código e hóspede.
func TestMapaDeOcupacaoMostraAReserva(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	const (
		de  = "2027-03-20"
		ate = "2027-03-22"
	)
	reserva, codigo := a.ocupar(t, a.unidade(t, "SP-02"), de, ate, a.usuario)

	linhas, err := a.svc.PorUnidade(a.ctx, janela(t, "2027-03-19", "2027-03-23"), nil)
	if err != nil {
		t.Fatalf("PorUnidade: %v", err)
	}
	if len(linhas) != 8 {
		t.Fatalf("linhas = %d, quero 8 unidades", len(linhas))
	}
	// Ordenado por units.code — a mesma ordem que a inserção de reserva usa.
	if linhas[0].UnitCode != "AP-01" || linhas[len(linhas)-1].UnitCode != "SP-04" {
		t.Errorf("ordem das unidades: %s .. %s", linhas[0].UnitCode, linhas[len(linhas)-1].UnitCode)
	}

	var alvo *disponibilidade.LinhaDoMapa
	for i := range linhas {
		if linhas[i].UnitCode == "SP-02" {
			alvo = &linhas[i]
		}
	}
	if alvo == nil {
		t.Fatal("SP-02 não apareceu no mapa")
	}
	if len(alvo.Dias) != 4 {
		t.Fatalf("dias = %d, quero 4", len(alvo.Dias))
	}

	for _, d := range alvo.Dias {
		ocupado := d.Data >= de && d.Data < ate
		if !ocupado {
			if d.Status != "livre" || d.StayBlockID != nil || d.Hospede != nil {
				t.Errorf("%s deveria estar livre: %+v", d.Data, d)
			}
			continue
		}
		if d.Status != "confirmed" {
			t.Errorf("%s: status = %q, quero \"confirmed\"", d.Data, d.Status)
		}
		if d.StayBlockID == nil || d.ReservaID == nil || *d.ReservaID != reserva {
			t.Errorf("%s: bloco/reserva ausentes: %+v", d.Data, d)
		}
		if d.ReservaCodigo == nil || *d.ReservaCodigo != codigo {
			t.Errorf("%s: código = %v, quero %q", d.Data, d.ReservaCodigo, codigo)
		}
		if d.Hospede == nil || *d.Hospede != "Hóspede de Integração" {
			t.Errorf("%s: hóspede = %v", d.Data, d.Hospede)
		}
	}

	// Outra unidade no mesmo dia continua livre.
	for _, l := range linhas {
		if l.UnitCode != "SP-03" {
			continue
		}
		for _, d := range l.Dias {
			if d.Status != "livre" {
				t.Errorf("SP-03 em %s deveria estar livre, veio %q", d.Data, d.Status)
			}
		}
	}
}

// Escopo `own`: a ocupação alheia continua visível (senão o mapa mentiria e o
// corretor prometeria data ocupada), mas o hóspede e o código somem.
func TestEscopoOwnNoBancoEscondeSoAIdentificacao(t *testing.T) {
	a := subir(t, auth.EscopoOwn)

	const (
		de  = "2027-04-05"
		ate = "2027-04-07"
	)
	// Dono é OUTRO usuário: uuid que não existe em users seria FK inválida, então
	// usamos um usuário real diferente do requisitante quando houver; senão, o
	// próprio requisitante deixa de servir e o teste cria a diferença por NULL.
	var outro *uuid.UUID
	var candidato uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM users WHERE id <> $1 ORDER BY created_at LIMIT 1`, a.usuario).Scan(&candidato); err == nil {
		outro = &candidato
	}
	var dono uuid.UUID
	if outro != nil {
		dono = *outro
	}

	a.ocupar(t, a.unidade(t, "AP-03"), de, ate, dono)

	linhas, err := a.svc.PorUnidade(a.ctx, janela(t, de, ate), nil)
	if err != nil {
		t.Fatalf("PorUnidade: %v", err)
	}

	for _, l := range linhas {
		if l.UnitCode != "AP-03" {
			continue
		}
		for _, d := range l.Dias {
			if d.Status != "confirmed" || d.StayBlockID == nil {
				t.Errorf("%s: a ocupação alheia sumiu do mapa (%+v) — calendário que mente causa overbooking", d.Data, d)
			}
			if d.Hospede != nil || d.ReservaCodigo != nil || d.ReservaID != nil {
				t.Errorf("%s: escopo own vazou identificação alheia: %+v", d.Data, d)
			}
		}
	}
}

// ─────────────────────────── Calendário vindo do banco ──────────────────────

// A classificação e o mínimo de noites de cada dia saem do banco: feriado
// semeado, fim de semana pela máscara de `date_type_rules`, período especial.
func TestClassificacaoDoDiaVemDoBanco(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	linhas, err := a.svc.PorProduto(a.ctx, janela(t, "2026-11-13", "2026-11-17"), nil)
	if err != nil {
		t.Fatalf("PorProduto: %v", err)
	}

	var cobertura disponibilidade.DisponibilidadeDoProduto
	for _, l := range linhas {
		if l.UnitTypeCode == "cobertura" {
			cobertura = l
		}
	}

	quero := map[string]struct {
		tipo  calendar.DateType
		preco int64
		min   int
	}{
		"2026-11-13": {calendar.Weekend, 240000, 2}, // sexta
		"2026-11-14": {calendar.Weekend, 240000, 2}, // sábado
		"2026-11-15": {calendar.Holiday, 310000, 3}, // domingo, mas feriado: precedência 80 > 40
		"2026-11-16": {calendar.Normal, 190000, 1},  // segunda
	}
	for _, d := range cobertura.Dias {
		esperado, ok := quero[d.Data]
		if !ok {
			continue
		}
		if d.TipoDeData != esperado.tipo {
			t.Errorf("%s: tipo = %q, quero %q", d.Data, d.TipoDeData, esperado.tipo)
		}
		if d.Preco == nil || *d.Preco != esperado.preco {
			t.Errorf("%s: preço = %v, quero %d", d.Data, d.Preco, esperado.preco)
		}
		if d.MinNoites != esperado.min {
			t.Errorf("%s: min_nights = %d, quero %d", d.Data, d.MinNoites, esperado.min)
		}
	}
}

// Filtro por produto devolve só ele; filtro por produto inexistente é 404.
func TestFiltrosDaDisponibilidade(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	completa := a.produto(t, "completa")
	linhas, err := a.svc.PorProduto(a.ctx, janela(t, "2027-05-10", "2027-05-13"), &completa)
	if err != nil {
		t.Fatalf("PorProduto: %v", err)
	}
	if len(linhas) != 1 || linhas[0].UnitTypeCode != "completa" {
		t.Fatalf("filtro por produto devolveu %+v", linhas)
	}

	fantasma := uuid.New()
	if _, err := a.svc.PorProduto(a.ctx, janela(t, "2027-05-10", "2027-05-13"), &fantasma); apperr.From(err).Status() != 404 {
		t.Errorf("produto inexistente = %v, quero 404", err)
	}

	sp := a.unidade(t, "SP-01")
	mapa, err := a.svc.PorUnidade(a.ctx, janela(t, "2027-05-10", "2027-05-13"), &sp)
	if err != nil {
		t.Fatalf("PorUnidade: %v", err)
	}
	if len(mapa) != 1 || mapa[0].UnitCode != "SP-01" {
		t.Fatalf("filtro por unidade devolveu %+v", mapa)
	}
}

// Reproduzir um orçamento antigo: forçar a tabela e a versão devolve o mesmo
// número que o caminho vigente, porque hoje só existe a V1.
func TestOrcamentoReproduzivelPelaTabelaEVersao(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	var tabela uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM rate_tables WHERE property_id = $1 AND name = 'Tabela Comercial V1'`,
		a.propriedade).Scan(&tabela); err != nil {
		t.Fatalf("tabela V1: %v", err)
	}
	versao := 1

	base := disponibilidade.Entrada{
		UnitTypeID: a.produto(t, "cobertura"),
		CheckIn:    calendar.MustParse("2026-11-20"),
		CheckOut:   calendar.MustParse("2026-11-23"),
		Hospedes:   8,
	}
	forcado := base
	forcado.RateTableID = &tabela
	forcado.PolicyVersion = &versao

	vigente, err := a.svc.Orcar(a.ctx, base)
	if err != nil {
		t.Fatalf("Orcar vigente: %v", err)
	}
	reproduzido, err := a.svc.Orcar(a.ctx, forcado)
	if err != nil {
		t.Fatalf("Orcar reproduzido: %v", err)
	}
	if vigente.Total != reproduzido.Total || vigente.RateTableID != reproduzido.RateTableID {
		t.Errorf("reprodução divergiu: %s vs %s", money.Cents(vigente.Total), money.Cents(reproduzido.Total))
	}

	inexistente := uuid.New()
	forcado.RateTableID = &inexistente
	if _, err := a.svc.Orcar(a.ctx, forcado); apperr.From(err).Status() != 404 {
		t.Errorf("tabela inexistente = %v, quero 404", err)
	}
}

// ─────────────────────────── Desempenho ─────────────────────────────────────

// O mapa de 90 dias × 8 unidades tem de responder em menos de 300 ms.
//
// A medição é o MELHOR de cinco: o primeiro tiro paga plano de consulta e
// conexão fria, e o que interessa aqui é o custo em regime, que é o que o
// painel sente ao arrastar o calendário. O teto do teste é o do requisito; o
// número medido vai no relatório.
func TestMapaDeNoventaDiasRespondeEmMenosDeTrezentosMilissegundos(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	j := janela(t, "2027-05-01", "2027-07-30")
	if len(j.Noites()) != 90 {
		t.Fatalf("janela de %d dias, quero 90", len(j.Noites()))
	}

	// Ocupa algumas unidades para a consulta não medir o caso trivial vazio.
	a.ocupar(t, a.unidade(t, "AP-02"), "2027-05-10", "2027-05-20", a.usuario)
	a.ocupar(t, a.unidade(t, "COB-01"), "2027-06-01", "2027-06-15", a.usuario)

	melhor := time.Hour
	for i := 0; i < 5; i++ {
		inicio := time.Now()
		linhas, err := a.svc.PorUnidade(a.ctx, j, nil)
		decorrido := time.Since(inicio)
		if err != nil {
			t.Fatalf("PorUnidade: %v", err)
		}
		if len(linhas) != 8 || len(linhas[0].Dias) != 90 {
			t.Fatalf("mapa = %d linhas × %d dias, quero 8 × 90", len(linhas), len(linhas[0].Dias))
		}
		if decorrido < melhor {
			melhor = decorrido
		}
	}
	t.Logf("mapa 90 dias × 8 unidades (720 células): %s", melhor.Round(time.Microsecond))
	if melhor > 300*time.Millisecond {
		t.Errorf("mapa levou %s, teto é 300ms", melhor)
	}

	// A visão comercial da mesma janela também é medida: são 4 produtos × 90
	// dias, e é ela que o painel chama primeiro.
	melhor = time.Hour
	for i := 0; i < 5; i++ {
		inicio := time.Now()
		if _, err := a.svc.PorProduto(a.ctx, j, nil); err != nil {
			t.Fatalf("PorProduto: %v", err)
		}
		if d := time.Since(inicio); d < melhor {
			melhor = d
		}
	}
	t.Logf("disponibilidade 90 dias × 4 produtos: %s", melhor.Round(time.Microsecond))
	if melhor > 300*time.Millisecond {
		t.Errorf("disponibilidade levou %s, teto é 300ms", melhor)
	}
}

// ═════════════ REGRESSÃO — `available` conta o que a VENDA aceita ═══════════
//
// As três funções abaixo cobrem os três defeitos medidos em 26/08/2026. Cada
// uma percorre as DUAS pontas do mesmo cenário — o que a consulta responde e o
// que a venda faria — porque o defeito nunca esteve numa ponta só: estava na
// divergência entre elas.

// ACHADO (a): dia sem tarifa era oferecido e o orçamento recusava.
//
// EM LINGUAGEM DE NEGÓCIO: alguém esqueceu de cadastrar a tarifa de fim de
// semana do apartamento. O calendário do painel mostrava as três unidades
// livres, com o preço em branco, e o corretor prometia a sexta ao hóspede. Ao
// clicar em calcular, `RATE_NOT_FOUND`. A data nunca foi vendável — a tela é
// que não sabia.
//
// Falta de tarifa é a MESMA condição que gera RATE_NOT_FOUND: se a consulta e o
// orçamento discordam sobre ela, um dos dois está mentindo.
func TestDisponibilidadeNaoOfereceDiaSemTarifaQueOOrcamentoRecusa(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	// 14 e 15/05/2027 são sexta e sábado (fds pela máscara de
	// `date_type_rules`); 16 é domingo, que na White House é diária normal.
	const (
		sexta   = "2027-05-14"
		sabado  = "2027-05-15"
		domingo = "2027-05-16"
		fim     = "2027-05-17"
	)
	apto := a.produto(t, "apto-2s")

	// Apaga a tarifa de fds SÓ do apartamento — o cenário que a revisão mediu.
	// A restauração devolve o valor lido, não um literal: um número escrito à
	// mão aqui viraria uma alteração silenciosa do tarifário se o seed mudar.
	var valor int64
	var tabela uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`DELETE FROM rates WHERE unit_type_id = $1 AND date_type = 'fds'
		 RETURNING rate_table_id, amount_cents`, apto).Scan(&tabela, &valor); err != nil {
		t.Fatalf("apagando a tarifa de fds do apartamento: %v", err)
	}
	t.Cleanup(func() {
		if _, err := a.pool.Exec(context.Background(),
			`INSERT INTO rates (rate_table_id, unit_type_id, date_type, amount_cents)
			 VALUES ($1, $2, 'fds', $3)`, tabela, apto, valor); err != nil {
			t.Fatalf("restaurando a tarifa de fds: %v", err)
		}
	})

	linhas, err := a.svc.PorProduto(a.ctx, janela(t, sexta, fim), &apto)
	if err != nil {
		t.Fatalf("PorProduto: %v", err)
	}
	if len(linhas) != 1 {
		t.Fatalf("produtos = %d, quero 1", len(linhas))
	}

	porDia := map[string]disponibilidade.DiaDoProduto{}
	for _, d := range linhas[0].Dias {
		porDia[d.Data] = d
	}

	for _, dia := range []string{sexta, sabado} {
		d := porDia[dia]
		if d.Preco != nil {
			t.Errorf("%s: price_cents = %d com a tarifa apagada", dia, *d.Preco)
		}
		if d.Disponivel != 0 {
			t.Errorf("%s: available = %d com price_cents null — a tela oferece o que POST /quotes "+
				"recusa com RATE_NOT_FOUND, e quem descobre é o hóspede ao telefone", dia, d.Disponivel)
		}
		if d.Motivo == nil || *d.Motivo != disponibilidade.MotivoSemTarifa {
			t.Errorf("%s: unavailable_reason = %v, quero %q — sem ele a célula cinza de "+
				"\"falta cadastrar tarifa\" fica igual à de \"já vendido\", e a gestão não sabe que tem "+
				"configuração pela metade", dia, textoDe(d.Motivo), disponibilidade.MotivoSemTarifa)
		}
	}

	// O CONTROLE: o domingo tem tarifa `normal` e continua vendável. Sem ele,
	// um sistema que zerasse tudo passaria neste teste.
	if d := porDia[domingo]; d.Disponivel != 3 || d.Preco == nil {
		t.Errorf("%s (diária normal, tarifa intacta): available = %d, price_cents = %v — "+
			"quero 3 unidades vendáveis", domingo, d.Disponivel, d.Preco)
	}

	// A outra ponta: o orçamento das mesmas noites recusa, e é por isso que a
	// consulta não podia ter oferecido.
	_, err = a.svc.Orcar(a.ctx, disponibilidade.Entrada{
		UnitTypeID: apto,
		CheckIn:    calendar.MustParse(sexta),
		CheckOut:   calendar.MustParse(fim),
		Hospedes:   2,
	})
	if e := apperr.From(err); e.Code != "RATE_NOT_FOUND" {
		t.Fatalf("POST /quotes nas mesmas datas devolveu %q, quero RATE_NOT_FOUND (erro: %v)", e.Code, err)
	}
}

// ACHADO (b): composição incompleta era oferecida E cotada pelo preço da casa
// inteira, e só POST /reservations recusava.
//
// EM LINGUAGEM DE NEGÓCIO: AP-03 está em manutenção. O calendário mostrava a
// White House Completa disponível, o orçamento fechava em cima das oito
// unidades, o operador prometia a data ao hóspede — e a venda morria na frente
// dele com 422. A casa não está inteira: 7 de 8 não é a casa.
func TestCompletaComUnidadeInativaNemAparecerNemSerCotada(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	completa := a.produto(t, "completa")
	alvo := a.unidade(t, "AP-03")

	// Chega-se ao estado pelo BANCO, e não pela API, de propósito: o inventário
	// já recusa desativar uma unidade que compõe a Completa, mas dado legado
	// (unidade inativa antes daquela guarda, importação de OTA) continua
	// existindo — e é dele que esta rota tem de se defender.
	if _, err := a.pool.Exec(a.ctx, `UPDATE units SET active = false WHERE id = $1`, alvo); err != nil {
		t.Fatalf("desativando AP-03: %v", err)
	}
	t.Cleanup(func() {
		if _, err := a.pool.Exec(context.Background(),
			`UPDATE units SET active = true WHERE id = $1`, alvo); err != nil {
			t.Fatalf("reativando AP-03: %v", err)
		}
	})

	linhas, err := a.svc.PorProduto(a.ctx, janela(t, "2027-06-10", "2027-06-14"), &completa)
	if err != nil {
		t.Fatalf("PorProduto: %v", err)
	}
	if len(linhas) != 1 {
		t.Fatalf("produtos = %d, quero 1", len(linhas))
	}
	linha := linhas[0]

	// `total_units` é o tamanho DECLARADO e não encolhe: encolher esconderia
	// justamente o defeito. Quem denuncia é `active_units`.
	if linha.TotalUnidades != 8 {
		t.Errorf("total_units = %d, quero 8 (a composição declarada não muda porque alguém "+
			"desativou uma unidade)", linha.TotalUnidades)
	}
	if linha.UnidadesAtivas != 7 {
		t.Errorf("active_units = %d, quero 7 — é este número que diz à gestão que o produto "+
			"não pode ser entregue", linha.UnidadesAtivas)
	}

	// TODOS os dias, inclusive os que nenhum hóspede tocou: não é a data que
	// está indisponível, é o produto que não pode ser entregue.
	for _, d := range linha.Dias {
		if d.Disponivel != 0 {
			t.Errorf("%s: available = %d com AP-03 fora do ar — a tela promete a casa inteira "+
				"e a venda recusa com 422 COMPOSITION_INCOMPLETE", d.Data, d.Disponivel)
		}
		if d.Motivo == nil || *d.Motivo != disponibilidade.MotivoComposicaoIncompleta {
			t.Errorf("%s: unavailable_reason = %v, quero %q", d.Data, textoDe(d.Motivo),
				disponibilidade.MotivoComposicaoIncompleta)
		}
	}

	// A outra ponta: o orçamento recusa com o MESMO código da venda, em vez de
	// devolver 200 com o preço das oito unidades.
	_, err = a.svc.Orcar(a.ctx, disponibilidade.Entrada{
		UnitTypeID: completa,
		CheckIn:    calendar.MustParse("2027-06-10"),
		CheckOut:   calendar.MustParse("2027-06-13"),
		Hospedes:   10,
	})
	e := apperr.From(err)
	if e.Code != "COMPOSITION_INCOMPLETE" {
		t.Fatalf("POST /quotes da Completa com AP-03 inativa devolveu %q — orçar o que não se pode "+
			"vender é prometer (erro: %v)", e.Code, err)
	}
	if e.Status() != 422 {
		t.Errorf("status = %d, quero 422: a data não está em disputa e NENHUMA data resolve "+
			"composição quebrada", e.Status())
	}
	// A recusa tem de dizer QUAL unidade falta, senão 422 vira "tente outra
	// data" para sempre — que é o motivo de o código não ser 409.
	detalhes, ok := e.Details.(map[string]any)
	if !ok {
		t.Fatalf("details = %T, quero o mesmo formato que POST /reservations emite", e.Details)
	}
	faltando, _ := detalhes["missing_unit_codes"].([]string)
	if len(faltando) != 1 || faltando[0] != "AP-03" {
		t.Errorf("missing_unit_codes = %v, quero [AP-03] — sem o código o operador fica sem ação", faltando)
	}
	if detalhes["expected_units"] != 8 || detalhes["active_units"] != 7 {
		t.Errorf("details = %v, quero expected_units 8 e active_units 7", detalhes)
	}

	// O CONTROLE: o apartamento é `one_member` e continua vendável com as duas
	// unidades que sobraram. Uma guarda que zerasse o inventário inteiro
	// satisfaria as asserções acima sem proteger ninguém.
	apto := a.produto(t, "apto-2s")
	outras, err := a.svc.PorProduto(a.ctx, janela(t, "2027-06-10", "2027-06-14"), &apto)
	if err != nil {
		t.Fatalf("PorProduto (apto): %v", err)
	}
	for _, d := range outras[0].Dias {
		if d.Disponivel != 2 {
			t.Errorf("apto-2s em %s: available = %d, quero 2 — uma unidade fora do ar não fecha "+
				"um produto one_member, fecha só a Completa", d.Data, d.Disponivel)
		}
	}
	if _, err := a.svc.Orcar(a.ctx, disponibilidade.Entrada{
		UnitTypeID: apto,
		CheckIn:    calendar.MustParse("2027-06-10"),
		CheckOut:   calendar.MustParse("2027-06-13"),
		Hospedes:   2,
	}); err != nil {
		t.Errorf("orçar o apartamento com AP-03 fora do ar falhou (%v) — ele ainda tem duas unidades", err)
	}
}

// ACHADO (c): o mapa era cego ao estado `completed`.
//
// EM LINGUAGEM DE NEGÓCIO: o hóspede dormiu três noites, pagou e foi embora. No
// dia seguinte a gestão abre o mapa e as três noites aparecem como se ninguém
// nunca tivesse estado lá — ocupação, ADR e RevPAR contam zero noite numa
// estadia consumada e faturada.
//
// A janela é a mesma coisa vista de dois ângulos, e os dois têm de valer AO
// MESMO TEMPO. Confundir os predicados é o defeito nos dois sentidos: com
// `completed` dentro do predicado de venda, o check-out trava a noite seguinte
// e o estoque some; com `completed` fora do predicado do mapa, a história some.
func TestOsPredicadosDeVendaEDeExibicaoNaoSaoOMesmo(t *testing.T) {
	a := subir(t, auth.EscopoAll)

	const (
		de  = "2027-08-10"
		ate = "2027-08-13"
	)
	unidade := a.unidade(t, "SP-01")
	_, codigo := a.ocuparCom(t, unidade, de, ate, a.usuario, "checked_out", "completed")

	// ── Ângulo 1, o mapa: a noite TEVE hóspede ─────────────────────────────
	mapa, err := a.svc.PorUnidade(a.ctx, janela(t, de, ate), &unidade)
	if err != nil {
		t.Fatalf("PorUnidade: %v", err)
	}
	if len(mapa) != 1 || len(mapa[0].Dias) != 3 {
		t.Fatalf("mapa = %d linhas × %d dias, quero 1 × 3", len(mapa), len(mapa[0].Dias))
	}
	for _, d := range mapa[0].Dias {
		if d.Status != disponibilidade.StatusConcluido {
			t.Errorf("%s: status = %q, quero %q — a estadia CUMPRIDA evaporou do mapa, que é a "+
				"fonte de ocupação, ADR e RevPAR", d.Data, d.Status, disponibilidade.StatusConcluido)
		}
		if d.ReservaCodigo == nil || *d.ReservaCodigo != codigo {
			t.Errorf("%s: reservation_code = %v, quero %q — sem o código a estadia consumada fica "+
				"indistinguível da venda que o hóspede cancelou sem nunca chegar",
				d.Data, textoDe(d.ReservaCodigo), codigo)
		}
		if d.StayBlockID == nil || d.Hospede == nil {
			t.Errorf("%s: célula sem bloco ou sem hóspede: %+v", d.Data, d)
		}
	}

	// ── Ângulo 2, o inventário: a noite está LIVRE de novo ─────────────────
	//
	// Sem este controle, "o mapa mostra o hóspede" seria satisfeito por um
	// sistema que simplesmente não libera a data — o defeito oposto, e mais
	// caro, porque some com o estoque.
	suite := a.produto(t, "suite-piscina")
	linhas, err := a.svc.PorProduto(a.ctx, janela(t, de, ate), &suite)
	if err != nil {
		t.Fatalf("PorProduto: %v", err)
	}
	for _, d := range linhas[0].Dias {
		if d.Disponivel != 4 {
			t.Errorf("suite-piscina em %s: available = %d, quero 4 — a estadia encerrada continua "+
				"segurando o estoque", d.Data, d.Disponivel)
		}
	}

	// ── E a Completa, que consome as oito, também volta a ser vendável ─────
	completa := a.produto(t, "completa")
	linhas, err = a.svc.PorProduto(a.ctx, janela(t, de, ate), &completa)
	if err != nil {
		t.Fatalf("PorProduto (completa): %v", err)
	}
	for _, d := range linhas[0].Dias {
		if d.Disponivel != 1 {
			t.Errorf("completa em %s: available = %d, quero 1 (motivo: %v)", d.Data, d.Disponivel, textoDe(d.Motivo))
		}
	}

	// ── E o ativo VENCE o concluído na mesma célula ────────────────────────
	//
	// `completed` está fora da constraint `stay_no_overlap`, então o banco
	// PERMITE uma estadia nova sobre a noite de uma já cumprida — é o
	// back-to-back e a realocação. Duas linhas na mesma coordenada não podem
	// virar duas células: a grade da tela desalinharia inteira, e quem a gestão
	// precisa ver é quem está na casa, não quem já saiu.
	_, novo := a.ocuparCom(t, unidade, de, ate, a.usuario, "confirmed", "confirmed")

	mapa, err = a.svc.PorUnidade(a.ctx, janela(t, de, ate), &unidade)
	if err != nil {
		t.Fatalf("PorUnidade (sobreposto): %v", err)
	}
	if len(mapa) != 1 || len(mapa[0].Dias) != 3 {
		t.Fatalf("com bloco ativo e concluído no mesmo dia o mapa devolveu %d linhas × %d dias, "+
			"quero 1 × 3 — célula duplicada desalinha a grade da tela", len(mapa), len(mapa[0].Dias))
	}
	for _, d := range mapa[0].Dias {
		if d.Status != disponibilidade.StatusConfirmado {
			t.Errorf("%s: status = %q, quero %q — vence o ativo", d.Data, d.Status, disponibilidade.StatusConfirmado)
		}
		if d.ReservaCodigo == nil || *d.ReservaCodigo != novo {
			t.Errorf("%s: reservation_code = %v, quero %q (a reserva em curso, não a que já saiu)",
				d.Data, textoDe(d.ReservaCodigo), novo)
		}
	}
}

// textoDe imprime ponteiro de string sem o endereço, que não ajuda ninguém a
// entender por que o teste caiu.
func textoDe(s *string) string {
	if s == nil {
		return "null"
	}
	return *s
}
