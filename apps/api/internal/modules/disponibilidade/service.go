package disponibilidade

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Recurso e ação do RBAC deste módulo. Os códigos vêm do catálogo semeado em
// cmd/seed/acesso.go; o service os cita para consultar o ESCOPO concedido, que o
// middleware não repassa (ele só responde sim/não).
const (
	recursoCalendario = "calendar"
)

// repositorio é o repositório visto pelo service. Interface, e não o tipo
// concreto, para a montagem da resposta — inclusive a tarja do escopo `own` —
// ser testável sem Postgres.
type repositorio interface {
	Contexto(ctx context.Context, propriedade uuid.UUID, tabela *uuid.UUID, versao *int) (Contexto, error)
	Calendario(ctx context.Context, propriedade uuid.UUID, j Janela) (calendar.Commercial, error)
	Tarifas(ctx context.Context, tabela uuid.UUID, produto *uuid.UUID) (map[uuid.UUID]map[calendar.DateType]money.Cents, error)
	EstadiaMinima(ctx context.Context, tabela uuid.UUID) (map[calendar.DateType]int, error)
	Regras(ctx context.Context, tabela uuid.UUID, produto *uuid.UUID) (map[uuid.UUID]RegrasDoProduto, error)
	Produtos(ctx context.Context, propriedade uuid.UUID, produto *uuid.UUID) ([]Produto, error)
	Composicao(ctx context.Context, propriedade, produto uuid.UUID) (ComposicaoDoProduto, error)
	Ocupacao(ctx context.Context, propriedade uuid.UUID, j Janela, produto *uuid.UUID) (map[ChaveDia]Contagem, error)
	Mapa(ctx context.Context, propriedade uuid.UUID, j Janela, unidade *uuid.UUID) ([]CelulaBruta, error)
}

// Servico liga o banco ao motor comercial.
type Servico struct {
	repo repositorio

	// escrita é o MESMO repositório, no tipo concreto. Ela existe porque a
	// emissão do orçamento (`POST /quotes` com `persist: true`) grava, e escrita
	// não tem dublê: o que a interface `repositorio` protege é a MONTAGEM da
	// resposta de calendário, que é onde a lógica de tela mora e onde o teste
	// sem Postgres tem valor. Obrigar `repoFalso` a fingir dez métodos de
	// gravação para nunca serem chamados só encheria o dublê de ruído.
	//
	// Quem monta o Servico com um dublê simplesmente não tem as rotas de
	// orçamento persistido — e elas respondem erro nomeado, não nil pointer.
	escrita *Repository

	// tx é a transação do módulo. As rotas de calendário são de leitura e não
	// abrem nenhuma; a EMISSÃO abre, porque o cabeçalho e as noites do orçamento
	// só fazem sentido comitados juntos — e é no COMMIT que a constraint
	// `quote_nights_fecham_o_orcamento` confere que o snapshot fecha.
	tx *db.TxManager
}

func NovoServico(repo repositorio, tx *db.TxManager) *Servico {
	s := &Servico{repo: repo, tx: tx}
	if concreto, ok := repo.(*Repository); ok {
		s.escrita = concreto
	}
	return s
}

// propriedade devolve a casa do requisitante. Toda consulta deste módulo é
// escopada por ela — sem isso, uma segunda propriedade no futuro veria o
// calendário da primeira.
func propriedade(ctx context.Context) (uuid.UUID, error) {
	if u, ok := auth.UserFrom(ctx); ok {
		return u.PropertyID, nil
	}
	// Requisição da vitrine pública (módulo vitrine): sem usuário, a casa vem
	// do contexto, posta lá pelo handler público e por ninguém mais. Sem as
	// duas coisas o pedido não tem dono, e a resposta continua sendo 401 — a
	// ausência de sessão nunca vira "qualquer casa".
	if casa, ok := ctx.Value(chaveCasaPublica{}).(uuid.UUID); ok && casa != uuid.Nil {
		return casa, nil
	}
	return uuid.Nil, apperr.Unauthorized
}

// chaveCasaPublica é a chave de contexto da casa da vitrine. Tipo não
// exportado: só ComCasaPublica escreve nela.
type chaveCasaPublica struct{}

// ComCasaPublica marca o contexto de uma requisição SEM sessão com a casa que
// ela consulta. Existe para a vitrine pública (site de vendas) reusar o MESMO
// motor e as MESMAS consultas do painel — o site pergunta, não calcula
// (docs/unificacao-site-crm.md §3). Um segundo cálculo, só para o público, é
// exatamente o defeito que a unificação existe para acabar.
func ComCasaPublica(ctx context.Context, casa uuid.UUID) context.Context {
	return context.WithValue(ctx, chaveCasaPublica{}, casa)
}

// ehPublico diz se a requisição veio da vitrine (sem sessão).
func ehPublico(ctx context.Context) bool {
	if _, ok := auth.UserFrom(ctx); ok {
		return false
	}
	casa, ok := ctx.Value(chaveCasaPublica{}).(uuid.UUID)
	return ok && casa != uuid.Nil
}

// ─────────────────────────── Catálogo ───────────────────────────────────────

// ProdutoDoCatalogo é um produto vendável com o tarifário vigente: a tarifa e
// o mínimo de noites por tipo de data. É o que a tabela de tarifas e os cards
// do site mostram — lido do banco, nunca escrito à mão no HTML.
type ProdutoDoCatalogo struct {
	Produto
	Tarifas   map[calendar.DateType]money.Cents
	MinNoites map[calendar.DateType]int // já com a regra própria do produto aplicada
	Pacotes   []booking.Package
}

// minimosDoProduto sobrepõe a regra própria do produto à geral, tipo a tipo.
// É a mesma precedência que o motor aplica (booking.Product.MinNights).
func minimosDoProduto(geral, proprio map[calendar.DateType]int) map[calendar.DateType]int {
	out := make(map[calendar.DateType]int, len(geral)+len(proprio))
	for t, n := range geral {
		out[t] = n
	}
	for t, n := range proprio {
		out[t] = n
	}
	return out
}

// Catalogo devolve os produtos ativos da casa com a tabela e a política
// VIGENTES (as mesmas que POST /quotes usaria agora).
func (s *Servico) Catalogo(ctx context.Context) ([]ProdutoDoCatalogo, booking.Policy, error) {
	casa, err := propriedade(ctx)
	if err != nil {
		return nil, booking.Policy{}, err
	}
	produtos, err := s.repo.Produtos(ctx, casa, nil)
	if err != nil {
		return nil, booking.Policy{}, err
	}
	comercial, err := s.repo.Contexto(ctx, casa, nil, nil)
	if err != nil {
		return nil, booking.Policy{}, err
	}
	tarifas, err := s.repo.Tarifas(ctx, comercial.RateTableID, nil)
	if err != nil {
		return nil, booking.Policy{}, err
	}
	minimos, err := s.repo.EstadiaMinima(ctx, comercial.RateTableID)
	if err != nil {
		return nil, booking.Policy{}, err
	}
	regras, err := s.repo.Regras(ctx, comercial.RateTableID, nil)
	if err != nil {
		return nil, booking.Policy{}, err
	}
	out := make([]ProdutoDoCatalogo, 0, len(produtos))
	for _, p := range produtos {
		out = append(out, ProdutoDoCatalogo{
			Produto: p, Tarifas: tarifas[p.ID],
			MinNoites: minimosDoProduto(minimos, regras[p.ID].MinNoites),
			Pacotes:   regras[p.ID].Pacotes,
		})
	}
	politica := comercial.Politica
	politica.MinNights = minimos
	return out, politica, nil
}

// ─────────────────────────── GET /availability ──────────────────────────────

// PorProduto responde "dá para vender?": por produto, quantas unidades estão
// livres em cada dia da janela, com a tarifa e o mínimo de noites daquela noite.
func (s *Servico) PorProduto(ctx context.Context, j Janela, produto *uuid.UUID) ([]DisponibilidadeDoProduto, error) {
	casa, err := propriedade(ctx)
	if err != nil {
		return nil, err
	}

	produtos, err := s.repo.Produtos(ctx, casa, produto)
	if err != nil {
		return nil, err
	}
	// Filtro que não casa com nada é 404, não lista vazia: a tela pediu UM
	// produto e precisa saber que ele não existe (ou foi desativado).
	if produto != nil && len(produtos) == 0 {
		return nil, apperr.NotFound("Produto")
	}

	comercial, err := s.repo.Contexto(ctx, casa, nil, nil)
	if err != nil {
		return nil, err
	}
	tarifas, err := s.repo.Tarifas(ctx, comercial.RateTableID, produto)
	if err != nil {
		return nil, err
	}
	minimos, err := s.repo.EstadiaMinima(ctx, comercial.RateTableID)
	if err != nil {
		return nil, err
	}
	regras, err := s.repo.Regras(ctx, comercial.RateTableID, produto)
	if err != nil {
		return nil, err
	}
	cal, err := s.repo.Calendario(ctx, casa, j)
	if err != nil {
		return nil, err
	}
	ocupacao, err := s.repo.Ocupacao(ctx, casa, j, produto)
	if err != nil {
		return nil, err
	}

	// Classifica cada dia UMA vez, e não uma vez por produto: com 4 produtos e
	// 366 dias, a diferença é 1.464 classificações contra 366.
	noites := j.Noites()
	tipos := make([]calendar.Classification, len(noites))
	for i, d := range noites {
		tipos[i] = cal.Classify(d)
	}

	out := make([]DisponibilidadeDoProduto, 0, len(produtos))
	for _, p := range produtos {
		linha := DisponibilidadeDoProduto{
			UnitTypeID:   p.ID,
			UnitTypeCode: p.Codigo,
			Nome:         p.Nome,
			Consome:      p.Consome,
			Dias:         make([]DiaDoProduto, 0, len(noites)),
		}
		for i, d := range noites {
			iso := d.String()
			c := ocupacao[ChaveDia{Produto: p.ID, Dia: iso}]
			// total_units e active_units são os mesmos em todos os dias da
			// janela (a composição não muda dia a dia); a primeira célula já
			// os define.
			if c.Declaradas > linha.TotalUnidades {
				linha.TotalUnidades = c.Declaradas
			}
			if c.Ativas > linha.UnidadesAtivas {
				linha.UnidadesAtivas = c.Ativas
			}

			dia := DiaDoProduto{
				Data:       iso,
				TipoDeData: tipos[i].Type,
				MinNoites:  minimosDoProduto(minimos, regras[p.ID].MinNoites)[tipos[i].Type],
			}
			// A tarifa é lida ANTES de decidir a disponibilidade porque ela
			// faz parte da decisão: dia sem tarifa é dia que o orçamento
			// recusa com RATE_NOT_FOUND, e prometer o que a venda recusa é o
			// defeito que esta rota tinha.
			valor, temTarifa := tarifas[p.ID][tipos[i].Type]
			if temTarifa {
				centavos := int64(valor)
				dia.Preco = &centavos
			}
			dia.Disponivel, dia.Motivo = vendaveis(p.Consome, c, temTarifa)
			linha.Dias = append(linha.Dias, dia)
		}
		out = append(out, linha)
	}
	return out, nil
}

// vendaveis traduz o estado da composição num dia em "quantas dá para VENDER" —
// e, quando nenhuma, em por quê.
//
// VENDÁVEL, não livre. A rota antiga contava unidade sem bloqueio e parava aí,
// e por isso prometia data que a venda recusava. Foi medido dos dois jeitos:
// apto-2s com a tarifa de fds apagada devolvia `available: 3` com
// `price_cents: null` enquanto POST /quotes na mesma data respondia 422
// RATE_NOT_FOUND; e a Completa com AP-03 inativa devolvia `available: 1`
// enquanto POST /reservations respondia 422 COMPOSITION_INCOMPLETE. Um número
// que a venda não honra não é disponibilidade, é uma promessa que morre na
// frente do operador.
//
// Para `all_members` — a White House Completa — o resultado é 0 ou 1, nunca um
// número intermediário: ela consome as oito unidades, então UMA unidade ocupada
// fecha o produto inteiro. A regra sai da COMPOSIÇÃO (`consumes` +
// `unit_type_members`), nunca do nome nem do código do produto: acrescentar uma
// nona unidade à casa não pode exigir edição de código.
//
// A ordem dos testes é a precedência do contrato, e ela ordena por QUEM PRECISA
// AGIR: composição quebrada e tarifa faltando são configuração pela metade (a
// gestão age hoje); ocupado é o negócio funcionando (não há o que consertar).
func vendaveis(consome string, c Contagem, temTarifa bool) (int, *string) {
	switch {
	case consome == ConsomeTodas && c.Ativas < c.Declaradas:
		// 7 de 8 não é a casa inteira. Zera TODOS os dias, inclusive os que
		// nenhum hóspede tocou — não é a data que está indisponível, é o
		// produto que não pode ser entregue.
		return 0, motivo(MotivoComposicaoIncompleta)
	case !temTarifa:
		return 0, motivo(MotivoSemTarifa)
	case c.Ativas == 0:
		// Composição sem nenhuma unidade de pé (ou sem membro nenhum): não há
		// o que entregar, e a venda recusa pelo mesmo motivo.
		return 0, motivo(MotivoUnidadeInativa)
	}

	livres := c.Ativas - c.Ocupadas
	if consome == ConsomeTodas {
		livres = 0
		if c.Ocupadas == 0 {
			livres = 1
		}
	}
	if livres <= 0 {
		return 0, motivo(MotivoOcupado)
	}
	return livres, nil
}

// motivo devolve o ponteiro que o contrato pede — `unavailable_reason` é
// `string | null`, e `null` é o caso em que há o que vender.
func motivo(m string) *string { return &m }

// ─────────────────────────── GET /availability/units ────────────────────────

// PorUnidade responde "quem está onde?": a matriz unidade × dia.
func (s *Servico) PorUnidade(ctx context.Context, j Janela, unidade *uuid.UUID) ([]LinhaDoMapa, error) {
	casa, err := propriedade(ctx)
	if err != nil {
		return nil, err
	}

	cal, err := s.repo.Calendario(ctx, casa, j)
	if err != nil {
		return nil, err
	}
	celulas, err := s.repo.Mapa(ctx, casa, j, unidade)
	if err != nil {
		return nil, err
	}
	if unidade != nil && len(celulas) == 0 {
		return nil, apperr.NotFound("Unidade")
	}

	tipos := map[string]calendar.DateType{}
	for _, d := range j.Noites() {
		tipos[d.String()] = cal.Classify(d).Type
	}

	// Escopo `own` NÃO filtra linhas aqui, e isso é decisão consciente.
	//
	// `AND owner_id = $usuario` num mapa de OCUPAÇÃO faria as datas dos outros
	// aparecerem como livres — o corretor prometeria a casa a um hóspede e a
	// gravação estouraria 409 depois. Um calendário que mente é pior que um
	// calendário sem nomes. Então a ocupação continua visível inteira e o que
	// some é a IDENTIFICAÇÃO da reserva alheia: id, código e hóspede.
	somenteProprias := auth.SomenteProprios(ctx, recursoCalendario, auth.AcaoVer)
	eu, _ := auth.UsuarioID(ctx)

	// O agrupamento anda por ÍNDICE, não por ponteiro para o elemento: `append`
	// realoca o slice, e um ponteiro guardado antes disso passaria a escrever no
	// array antigo — as diárias das primeiras unidades sumiriam da resposta.
	porNoite := len(j.Noites())
	out := make([]LinhaDoMapa, 0, 8)
	atual := -1
	for _, c := range celulas {
		if atual < 0 || out[atual].UnitID != c.UnitID {
			out = append(out, LinhaDoMapa{
				UnitID:   c.UnitID,
				UnitCode: c.UnitCode,
				UnitName: c.UnitName,
				Dias:     make([]DiaDaUnidade, 0, porNoite),
			})
			atual = len(out) - 1
		}

		dia := DiaDaUnidade{
			Data:       c.Dia,
			Status:     statusDaCelula(c),
			TipoDeData: tipos[c.Dia],
		}
		if c.StayBlockID != nil {
			dia.StayBlockID = c.StayBlockID
			if !somenteProprias || (c.DonoID != nil && *c.DonoID == eu) {
				dia.ReservaID = c.ReservaID
				dia.ReservaCodigo = c.ReservaCodigo
				dia.Hospede = c.Hospede
			}
		}
		out[atual].Dias = append(out[atual].Dias, dia)
	}
	return out, nil
}

// statusDaCelula deriva o status que a tela pinta.
//
// Bloco de reserva mostra o STATUS (`hold`/`confirmed`), porque a diferença
// entre "segurado" e "pago" é o que a gestão precisa ver para cobrar o sinal.
// Bloco operacional mostra a ORIGEM (`maintenance`/`owner_hold`/`ota`), porque
// ali não há sinal a cobrar — há um motivo de a unidade estar fora de venda.
func statusDaCelula(c CelulaBruta) string {
	if c.StayBlockID == nil || c.BlocoSource == nil {
		return StatusLivre
	}
	switch *c.BlocoSource {
	case "maintenance":
		return StatusManutencao
	case "owner_hold":
		return StatusProprietario
	case "ota":
		return StatusOTA
	}
	if c.BlocoStatus == nil {
		return StatusConfirmado
	}
	switch *c.BlocoStatus {
	case "hold":
		return StatusHold
	case blocoConcluido:
		// A estadia foi cumprida. A célula continua nomeando reserva e
		// hóspede: sem ela a ocupação realizada evapora do mapa e o ADR passa
		// a dividir receita por noites que o relatório diz que ninguém dormiu.
		return StatusConcluido
	}
	return StatusConfirmado
}

// ─────────────────────────── POST /quotes ───────────────────────────────────

// Orcar calcula o orçamento. NÃO grava e NÃO segura a data: só a pré-reserva
// (POST /reservations) insere em stay_blocks. Por isso um orçamento pode virar
// 409 DATE_CONFLICT na hora de virar reserva, e isso é correto.
func (s *Servico) Orcar(ctx context.Context, e Entrada) (Orcamento, error) {
	// GANHAR TRANSCREVE, NÃO REFAZ. Quando há um orçamento FIXADO no contexto,
	// esta chamada devolve o snapshot emitido e o motor não roda — é assim que
	// `/win` copia os valores congelados para a reserva em vez de reprecificar
	// com a tabela de hoje. Ver snapshot.go, que explica o mecanismo inteiro.
	if fixo, ok := orcamentoFixado(ctx); ok {
		if err := conferirOFixado(fixo, e); err != nil {
			return Orcamento{}, err
		}
		return fixo.Orcamento, nil
	}

	casa, err := propriedade(ctx)
	if err != nil {
		return Orcamento{}, err
	}

	produtos, err := s.repo.Produtos(ctx, casa, &e.UnitTypeID)
	if err != nil {
		return Orcamento{}, err
	}
	if len(produtos) == 0 {
		return Orcamento{}, apperr.NotFound("Produto")
	}
	p := produtos[0]

	// A composição é conferida ANTES de qualquer conta. Sem isso o orçamento
	// da Completa com AP-03 inativa saía 200, com o preço das oito unidades,
	// e só POST /reservations recusava — o operador cotava, prometia a data ao
	// hóspede e a venda morria na frente dele. Orçar o que não se pode vender é
	// prometer, e o contrato declara COMPOSITION_INCOMPLETE nesta rota
	// exatamente por isso.
	composicao, err := s.repo.Composicao(ctx, casa, p.ID)
	if err != nil {
		return Orcamento{}, err
	}
	if err := conferirComposicao(p, composicao); err != nil {
		return Orcamento{}, err
	}

	comercial, err := s.repo.Contexto(ctx, casa, e.RateTableID, e.PolicyVersion)
	if err != nil {
		return Orcamento{}, err
	}
	tarifas, err := s.repo.Tarifas(ctx, comercial.RateTableID, &e.UnitTypeID)
	if err != nil {
		return Orcamento{}, err
	}
	minimos, err := s.repo.EstadiaMinima(ctx, comercial.RateTableID)
	if err != nil {
		return Orcamento{}, err
	}
	regras, err := s.repo.Regras(ctx, comercial.RateTableID, &e.UnitTypeID)
	if err != nil {
		return Orcamento{}, err
	}

	// A janela do calendário é a própria estadia. Quando check_out não é
	// posterior a check_in, quem recusa é o motor (VALIDATION_ERROR); montar a
	// janela aqui devolveria o mesmo código por outro caminho, e dois lugares
	// dizendo o mesmo "não" divergem no dia em que só um for editado.
	cal := calendar.Commercial{}
	if e.CheckIn.Before(e.CheckOut) {
		if cal, err = s.repo.Calendario(ctx, casa, Janela{De: e.CheckIn, Ate: e.CheckOut}); err != nil {
			return Orcamento{}, err
		}
	}

	politica := comercial.Politica
	politica.MinNights = minimos

	// O nome entra nas mensagens do motor ("Sem tarifa para réveillon em …").
	// Na vitrine vai o nome de vitrine: o nome interno ("(casa principal)") é
	// vocabulário da equipe e não sai em resposta pública.
	nome := p.Nome
	if ehPublico(ctx) {
		nome = p.NomePublico
	}

	quote, err := booking.Build(booking.Request{
		Product: booking.Product{
			ID:          p.ID.String(),
			Name:        nome,
			Capacity:    p.Capacidade,
			Rates:       tarifas[p.ID],
			CleaningFee: p.LimpezaCent,
			MinNights:   regras[p.ID].MinNoites,
			Packages:    regras[p.ID].Pacotes,
		},
		CheckIn:     e.CheckIn,
		CheckOut:    e.CheckOut,
		Guests:      e.Hospedes,
		DiscountPct: e.DescontoPct,
		IsEvent:     e.IsEvento,
	}, cal, politica)
	if err != nil {
		return Orcamento{}, traduzirRegra(err)
	}

	return novoOrcamento(quote, comercial.RateTableID), nil
}

// conferirComposicao recusa o orçamento do produto que não pode ser entregue.
//
// O critério é o MESMO que POST /reservations aplica, e tem de continuar sendo:
// duas respostas diferentes para a mesma pergunta é o defeito de origem — a
// consulta dizia sim e a venda dizia não.
//
//   - `all_members`: só é entregável com a composição INTEIRA de pé. A Completa
//     vende a casa; sete oitavos pelo preço da casa é o hóspede encontrando um
//     estranho num dos quartos.
//   - `one_member`: basta UMA unidade ativa. Duas de três continua vendável —
//     o que a venda recusa é a composição sem nenhuma.
func conferirComposicao(p Produto, c ComposicaoDoProduto) error {
	entregavel := c.Ativas > 0
	if p.Consome == ConsomeTodas {
		entregavel = c.Declaradas > 0 && c.Ativas == c.Declaradas
	}
	if entregavel {
		return nil
	}
	return composicaoIncompleta(p.Codigo, c)
}

// composicaoIncompleta monta o 422 com o mesmo Code e o mesmo `details` que a
// venda emite — a tela que já sabe listar as unidades que faltam não precisa
// aprender um segundo formato.
//
// É 422 e não 409 porque a data não está em disputa: NENHUMA data resolve
// composição quebrada, e mandar o operador tentar outra semana seria mandá-lo
// tentar para sempre. Quem age é a gestão do inventário.
//
// Code, status e frase vêm do apperr — o mesmo erro base que `reservas` usa,
// sem importar aquele pacote (ele já importa este, e o ciclo seria imediato).
func composicaoIncompleta(codigo string, c ComposicaoDoProduto) error {
	faltando := c.Faltando
	if faltando == nil {
		faltando = []string{}
	}
	return apperr.CompositionIncomplete.WithDetails(map[string]any{
		"unit_type_code":     codigo,
		"expected_units":     c.Declaradas,
		"active_units":       c.Ativas,
		"missing_unit_codes": faltando,
	})
}

// traduzirRegra converte a violação do motor no erro da API PRESERVANDO o Code,
// a frase e os details.
//
// O front reage ao Code, nunca ao texto — então o código que sai daqui tem de
// ser exatamente o que booking.RuleError carimbou, com o status que o catálogo
// do apperr dá a ele (todos os do motor são 422). Inventar VALIDATION_ERROR no
// lugar seria mentir sobre a causa e deixar a tela sem como dizer "falta
// cadastrar a tarifa deste período".
//
// Código que o catálogo não conhece é defeito nosso — o domínio carimbou algo
// fora do contrato — e sai 500 com a causa no log, em vez de um code que o
// painel não sabe ler. O teste de catálogo do apperr confere que todo code
// carimbado em internal/domain existe lá, então isso não acontece hoje.
func traduzirRegra(err error) error {
	var regra *booking.RuleError
	if !errors.As(err, &regra) {
		return err
	}
	base, conhecido := apperr.PorCodigo(regra.Code)
	if !conhecido {
		return apperr.Internal.WithCause(err)
	}
	return base.WithMessage(regra.Message).WithDetails(regra.Details).WithCause(err)
}

// Hoje é a data comercial de hoje, no fuso da casa, segundo o banco — a mesma
// que o orçamento usa. A vitrine a usa para recortar a janela pública, e o
// site para saber o que já passou sem confiar no relógio do visitante.
func (s *Servico) Hoje(ctx context.Context) (calendar.Date, error) {
	casa, err := propriedade(ctx)
	if err != nil {
		return calendar.Date{}, err
	}
	comercial, err := s.repo.Contexto(ctx, casa, nil, nil)
	if err != nil {
		return calendar.Date{}, err
	}
	return comercial.Hoje, nil
}
