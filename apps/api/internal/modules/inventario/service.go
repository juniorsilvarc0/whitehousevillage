package inventario

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Repositorio é o repositório visto pelo service. Interface, e não o tipo
// concreto, para as regras de composição e de exclusão bloqueada serem
// testáveis sem Postgres — que é onde a maior parte dos defeitos deste módulo
// cabe.
type Repositorio interface {
	ListarPropriedades(ctx context.Context, propriedadeID uuid.UUID, f Filtro) ([]Propriedade, int64, error)
	BuscarPropriedade(ctx context.Context, propriedadeID, id uuid.UUID) (Propriedade, error)
	AtualizarPropriedade(ctx context.Context, propriedadeID, id uuid.UUID, a PropriedadeAtualizar) error

	ListarProdutos(ctx context.Context, propriedadeID uuid.UUID, f Filtro) ([]Produto, int64, error)
	BuscarProduto(ctx context.Context, propriedadeID, id uuid.UUID) (Produto, error)
	CriarProduto(ctx context.Context, propriedadeID uuid.UUID, c ProdutoEntrada) (uuid.UUID, error)
	SubstituirProduto(ctx context.Context, propriedadeID, id uuid.UUID, c ProdutoEntrada) error
	AtualizarProduto(ctx context.Context, propriedadeID, id uuid.UUID, a ProdutoAtualizar) error
	DesativarProduto(ctx context.Context, propriedadeID, id uuid.UUID) error
	ContarReservasAtivasDoProduto(ctx context.Context, id uuid.UUID) (int, error)
	ReservasVivasDoProduto(ctx context.Context, propriedadeID, unitTypeID uuid.UUID) ([]ReservaViva, error)

	Composicao(ctx context.Context, unitTypeID uuid.UUID) ([]UnidadeDaComposicao, error)
	UnidadesDaPropriedade(ctx context.Context, propriedadeID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]UnidadeResumida, error)
	SubstituirComposicao(ctx context.Context, propriedadeID, unitTypeID uuid.UUID, ids []uuid.UUID) error

	ListarUnidades(ctx context.Context, propriedadeID uuid.UUID, f Filtro) ([]Unidade, int64, error)
	BuscarUnidade(ctx context.Context, propriedadeID, id uuid.UUID) (Unidade, error)
	CriarUnidade(ctx context.Context, propriedadeID uuid.UUID, c UnidadeEntrada) (uuid.UUID, error)
	SubstituirUnidade(ctx context.Context, propriedadeID, id uuid.UUID, c UnidadeEntrada) error
	AtualizarUnidade(ctx context.Context, propriedadeID, id uuid.UUID, a UnidadeAtualizar) error
	DesativarUnidade(ctx context.Context, propriedadeID, id uuid.UUID) error
	ContarBloqueiosFuturosDaUnidade(ctx context.Context, propriedadeID, id uuid.UUID) (int, error)
	ProdutosQueUsamAUnidade(ctx context.Context, id uuid.UUID) ([]VinculoDeComposicao, error)
	ReservasExclusivasSemAUnidade(ctx context.Context, propriedadeID, id uuid.UUID) ([]ReservaExclusiva, error)
}

type Service struct {
	repo   Repositorio
	tx     auth.Transacionador
	trilha Trilha
}

func NewService(repo Repositorio, tx auth.Transacionador, trilha Trilha) *Service {
	return &Service{repo: repo, tx: tx, trilha: trilha}
}

// propriedadeDoAtor resolve a casa em que a requisição opera.
//
// Todo o módulo é filtrado por ela — listagem, leitura, gravação e exclusão.
// O recurso `inventory` não oferece escopo `own` no catálogo (não há dono de
// apartamento), então o isolamento que resta é o da propriedade: `property_id`
// acompanha toda tabela de negócio justamente para a segunda casa não exigir
// reescrita, e um cadastro que ignorasse esse eixo já nasceria vazando.
func propriedadeDoAtor(ctx context.Context) (uuid.UUID, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return uuid.Nil, apperr.Unauthorized
	}
	if u.PropertyID == uuid.Nil {
		return uuid.Nil, apperr.Internal.WithCause(
			fmt.Errorf("usuário %s sem property_id: sessão incompleta", u.ID))
	}
	return u.PropertyID, nil
}

// ─────────────────────────── Propriedades ───────────────────────────

func (s *Service) ListarPropriedades(ctx context.Context, f Filtro) ([]Propriedade, int64, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarPropriedades(ctx, propriedade, f)
}

func (s *Service) BuscarPropriedade(ctx context.Context, id uuid.UUID) (Propriedade, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Propriedade{}, err
	}
	return s.repo.BuscarPropriedade(ctx, propriedade, id)
}

func (s *Service) AtualizarPropriedade(ctx context.Context, id uuid.UUID, a PropriedadeAtualizar) (Propriedade, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Propriedade{}, err
	}
	a.Normalizar()

	var atualizada Propriedade
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		// O estado ANTERIOR é lido dentro da transação, e não antes dela: entre
		// um SELECT solto e o UPDATE cabe outra escrita, e a trilha registraria
		// um "antes" que já não era o antes.
		antes, err := s.repo.BuscarPropriedade(ctx, propriedade, id)
		if err != nil {
			return err
		}
		if err := s.repo.AtualizarPropriedade(ctx, propriedade, id, a); err != nil {
			return err
		}
		atualizada, err = s.repo.BuscarPropriedade(ctx, propriedade, id)
		if err != nil {
			return err
		}
		return s.trilha.Alteracao(ctx, entidadePropriedade,
			verboDaTransicao(antes.Ativa, atualizada.Ativa), id, antes, atualizada)
	})
	if err != nil {
		return Propriedade{}, err
	}
	return atualizada, nil
}

// ─────────────────────────── Produtos ───────────────────────────────

func (s *Service) ListarProdutos(ctx context.Context, f Filtro) ([]Produto, int64, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarProdutos(ctx, propriedade, f)
}

func (s *Service) BuscarProduto(ctx context.Context, id uuid.UUID) (Produto, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Produto{}, err
	}
	return s.repo.BuscarProduto(ctx, propriedade, id)
}

// CriarProduto grava o cadastro. A composição NÃO vem aqui: ela tem endpoint
// próprio, e um produto que nascesse com composição implícita seria a forma
// mais silenciosa de vender unidade que ninguém escolheu.
func (s *Service) CriarProduto(ctx context.Context, c ProdutoEntrada) (Produto, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Produto{}, err
	}
	c.Normalizar()
	c = c.ComPadroes()

	var criado Produto
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.CriarProduto(ctx, propriedade, c)
		if err != nil {
			return err
		}
		criado, err = s.repo.BuscarProduto(ctx, propriedade, id)
		if err != nil {
			return err
		}
		return s.trilha.Criacao(ctx, entidadeProduto, audit.VerboCriado, id, criado)
	})
	if err != nil {
		return Produto{}, err
	}
	return criado, nil
}

// SubstituirProduto é o PUT — substituição integral do cadastro. A composição
// sai intacta: quem a troca é PUT /unit-types/{id}/members. Um PUT de cadastro
// que a zerasse por omissão tornaria o produto invendável sem ninguém ter
// pedido isso.
//
// `active` é campo do corpo como qualquer outro, e por isso o PUT passa pela
// MESMA guarda do DELETE: sem ela, quem não pode desativar por um verbo
// desativa pelo outro.
func (s *Service) SubstituirProduto(ctx context.Context, id uuid.UUID, c ProdutoEntrada) (Produto, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Produto{}, err
	}
	c.Normalizar()
	c = c.ComPadroes()

	var atualizado Produto
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarProduto(ctx, propriedade, id)
		if err != nil {
			return err
		}
		if err := s.guardarTransicaoDoProduto(ctx, antes, *c.Ativo); err != nil {
			return err
		}
		// `consumes` também entra pelo PUT, e pelo PUT ele entra SEMPRE (é
		// substituição integral). Sem esta linha, quem não consegue trocar o
		// consumo pelo PATCH troca reenviando o cadastro inteiro.
		if err := s.guardarConsumoDoProduto(ctx, propriedade, antes, c.Consome); err != nil {
			return err
		}
		if err := s.repo.SubstituirProduto(ctx, propriedade, id, c); err != nil {
			return err
		}
		atualizado, err = s.repo.BuscarProduto(ctx, propriedade, id)
		if err != nil {
			return err
		}
		return s.trilha.Alteracao(ctx, entidadeProduto,
			verboDaTransicao(antes.Ativo, atualizado.Ativo), id, antes, atualizado)
	})
	if err != nil {
		return Produto{}, err
	}
	return atualizado, nil
}

// AtualizarProduto é o PATCH.
//
// Mudar `cleaning_fee_cents` ou `capacity` NÃO reescreve o passado: orçamento e
// reserva já emitidos guardam o que usaram (reservations.cleaning_cents,
// reservation_nights.price_cents). A alteração vale do próximo cálculo em
// diante — e é por isso que este service não toca em nada fora de unit_types.
//
// A exceção é `active`: ele é a mesma decisão do DELETE, escrita com outro
// verbo, e carrega as mesmas guardas.
func (s *Service) AtualizarProduto(ctx context.Context, id uuid.UUID, a ProdutoAtualizar) (Produto, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Produto{}, err
	}
	a.Normalizar()

	var atualizado Produto
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarProduto(ctx, propriedade, id)
		if err != nil {
			return err
		}
		if err := s.guardarTransicaoDoProduto(ctx, antes, ativoPedido(antes.Ativo, a.Ativo)); err != nil {
			return err
		}
		if err := s.guardarConsumoDoProduto(ctx, propriedade, antes,
			textoPedido(antes.Consome, a.Consome)); err != nil {
			return err
		}
		if err := s.repo.AtualizarProduto(ctx, propriedade, id, a); err != nil {
			return err
		}
		atualizado, err = s.repo.BuscarProduto(ctx, propriedade, id)
		if err != nil {
			return err
		}
		return s.trilha.Alteracao(ctx, entidadeProduto,
			verboDaTransicao(antes.Ativo, atualizado.Ativo), id, antes, atualizado)
	})
	if err != nil {
		return Produto{}, err
	}
	return atualizado, nil
}

// DesativarProduto é o DELETE do contrato — `active = false`, nunca remoção.
//
// Recusa enquanto houver reserva viva no produto. O banco sozinho não recusaria
// de forma legível: a FK de reservations→unit_types é RESTRICT, então o DELETE
// físico estouraria 23503 sem dizer quantas reservas nem quais; e a
// DESATIVAÇÃO, que é o que este endpoint faz de fato, nenhuma constraint
// impede. A checagem existe para o gestor ler "há 3 reservas ativas" em vez de
// descobrir na alta temporada que o produto sumiu da disponibilidade com gente
// hospedada dentro.
func (s *Service) DesativarProduto(ctx context.Context, id uuid.UUID) error {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return err
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		produto, err := s.repo.BuscarProduto(ctx, propriedade, id)
		if err != nil {
			return err
		}
		if !produto.Ativo {
			return nil // já desativado: repetir o DELETE é no-op, não erro
		}
		if err := s.guardarTransicaoDoProduto(ctx, produto, false); err != nil {
			return err
		}
		if err := s.repo.DesativarProduto(ctx, propriedade, id); err != nil {
			return err
		}
		depois, err := s.repo.BuscarProduto(ctx, propriedade, id)
		if err != nil {
			return err
		}
		// Alteracao, e não Exclusao: aqui não se exclui nada — `active = false`
		// deixa a linha inteira legível em `unit_types`, e um retrato completo
		// dela em `before` seria cópia do que ainda está lá. O que a trilha
		// precisa guardar é a MUDANÇA, e é ela que o Diff recorta.
		//
		// A escolha também mantém as três portas da desativação (DELETE, PATCH,
		// PUT) gravando linhas do MESMO formato e com o MESMO verbo: quem
		// consulta a auditoria pergunta pela decisão, não pelo verbo HTTP que a
		// carregou.
		return s.trilha.Alteracao(ctx, entidadeProduto, verboDesativado, id, produto, depois)
	})
}

// guardarTransicaoDoProduto recusa tirar do ar produto com reserva viva.
//
// A guarda mora AQUI, e não em cada endpoint, porque as três portas — DELETE,
// PATCH `{"active":false}` e PUT com `active:false` — são a mesma decisão de
// negócio. A revisão adversarial mediu a assimetria: o DELETE recusava com 409
// e o PATCH devolvia 200 desativando, ou seja, a guarda existia e bastava
// escolher outro verbo para contorná-la.
//
// Reativar não tem guarda: um produto que volta ao ar volta com a composição
// que tinha, e a composição já é protegida pelas guardas de unidade abaixo.
func (s *Service) guardarTransicaoDoProduto(ctx context.Context, atual Produto, querAtivo bool) error {
	if !atual.Ativo || querAtivo {
		return nil
	}

	reservas, err := s.repo.ContarReservasAtivasDoProduto(ctx, atual.ID)
	if err != nil {
		return err
	}
	if reservas > 0 {
		return apperr.ResourceInUse.
			WithMessage("Há reserva ativa neste produto. Encerre-a antes de desativá-lo.").
			WithDetails(map[string]any{"reservations_count": reservas})
	}
	return nil
}

// guardarConsumoDoProduto recusa trocar `consumes` com venda viva no produto.
//
// ─────────────── O QUE ESTA GUARDA MEDIU, AO VIVO ───────────────
//
// `consumes` é o que diz QUANTAS unidades da composição uma venda ocupa. Ele é
// lido no momento da venda e nunca mais: a reserva já emitida segura o número
// ANTIGO. Trocá-lo depois faz o produto declarar uma coisa e a venda viva
// segurar outra — e as duas direções foram reproduzidas contra Postgres real:
//
//   - `all_members` → `one_member`: a Completa vendida 2035-05-10→13 por
//     R$ 20.200,00 (`WH-2026-0005`) saiu com UM apartamento, AP-01. Os outros
//     sete ficaram livres, e `WH-2026-0006` foi vendida em AP-02 dentro das
//     mesmas datas — 201, e corretamente, porque não havia bloco com que
//     colidir. É o estranho dormindo dentro da casa alugada inteira, agora
//     alcançado sem tocar em `active` nem na composição;
//   - `one_member` → `all_members`: a venda de um apartamento (1 linha em
//     `reservation_units`) passa a pertencer a um produto que declara o
//     conjunto inteiro (4 na medição). A invariante da venda exclusiva —
//     `|reservation_units| == |composição|` — nasce quebrada, e as guardas que
//     dependem de `consumes = 'all_members'` (ReservasExclusivasSemAUnidade,
//     codigosDosVinculos) passam a apontar para uma venda que nunca foi
//     exclusiva.
//
// Recusar, e não propagar: propagar significaria inserir ou apagar blocos de
// uma estadia já vendida. Apagar bloco de gente com data marcada é a operação
// que a FK `ON DELETE RESTRICT` de stay_blocks existe para impedir, e inserir
// bloco novo muda o que o hóspede comprou sem que ninguém tenha decidido isso.
// A regra 7 do CLAUDE.md é explícita: toda entidade financeira congela o que
// usou, e mudar o cadastro nunca reescreve o passado. O consumo é parte do que
// a venda usou.
//
// O custo da recusa está escrito e é real: enquanto houver estadia futura de
// pé, o produto não muda de consumo. A saída do operador é a mesma de sempre —
// remarcar, cancelar, ou esperar o período passar — e ela aparece na mensagem
// com o código de cada reserva, porque "não pode" sem o nome da reserva é uma
// recusa que o gestor não consegue resolver.
func (s *Service) guardarConsumoDoProduto(ctx context.Context, propriedade uuid.UUID, atual Produto, querConsome string) error {
	if querConsome == "" || querConsome == atual.Consome {
		return nil
	}

	reservas, err := s.repo.ReservasVivasDoProduto(ctx, propriedade, atual.ID)
	if err != nil {
		return err
	}
	if len(reservas) == 0 {
		return nil
	}

	return apperr.ResourceInUse.
		WithMessage(fmt.Sprintf(
			"O consumo de %q não pode mudar enquanto %s de pé: %s. "+
				"O consumo define quantas unidades a venda ocupa, e %s já ocupa pelo número antigo — "+
				"trocá-lo agora faria o produto declarar um conjunto e a estadia segurar outro. "+
				"Remarque ou cancele %s, ou espere o período terminar.",
			atual.Codigo,
			plural(len(reservas), "houver estadia futura", "houver estadias futuras"),
			codigosDasReservas(reservas),
			plural(len(reservas), "ela", "elas"),
			plural(len(reservas), "essa reserva", "essas reservas"))).
		WithDetails(map[string]any{
			"live_reservations":  reservas,
			"reservations_count": len(reservas),
			"current_consumes":   atual.Consome,
			"requested_consumes": querConsome,
		})
}

// ─────────────────────────── Composição ─────────────────────────────

func (s *Service) Composicao(ctx context.Context, id uuid.UUID) ([]UnidadeDaComposicao, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, err
	}
	// Confere o produto antes: sem isso, um id inexistente devolveria lista
	// vazia com 200, e a tela mostraria "produto sem composição" para um
	// produto que não existe.
	if _, err := s.repo.BuscarProduto(ctx, propriedade, id); err != nil {
		return nil, err
	}
	return s.repo.Composicao(ctx, id)
}

// SubstituirComposicao troca o conjunto inteiro de unidades do produto.
//
// É o coração da exclusividade. `consumes` diz QUANTAS unidades a venda ocupa;
// a composição diz QUAIS. Sem ela correta, a garantia contra overbooking não
// existe: a White House Completa vendida com composição parcial travaria menos
// de oito unidades, e o banco aceitaria uma segunda venda por cima — sem erro
// nenhum, porque não há sobreposição de unidade a detectar.
func (s *Service) SubstituirComposicao(ctx context.Context, id uuid.UUID, c ComposicaoEntrada) ([]UnidadeDaComposicao, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, err
	}

	produto, err := s.repo.BuscarProduto(ctx, propriedade, id)
	if err != nil {
		return nil, err
	}

	// Unidade de OUTRA propriedade é recusada por índice, e não pela FK: a FK
	// aceitaria (units é uma tabela só), e o produto passaria a consumir um
	// apartamento de outra casa.
	achadas, err := s.repo.UnidadesDaPropriedade(ctx, propriedade, c.UnidadeIDs)
	if err != nil {
		return nil, err
	}
	erros := map[string]string{}
	for i, unidade := range c.UnidadeIDs {
		resumo, existe := achadas[unidade]
		if !existe {
			erros[fmt.Sprintf("unit_ids[%d]", i)] = "unidade inexistente nesta propriedade."
			continue
		}
		// Unidade INATIVA na composição é a mesma falha da desativação, entrando
		// pela porta oposta: o produto passa a declarar N unidades e a venda
		// aloca N-1, porque a consulta de candidatas filtra `u.active`. No
		// `all_members` isso é a Completa vendida pelo preço de oito travando
		// sete — a oitava fica livre para um estranho dormir dentro da casa
		// alugada inteira. Ative a unidade primeiro, ou deixe-a fora do produto.
		if !resumo.Ativa {
			erros[fmt.Sprintf("unit_ids[%d]", i)] = fmt.Sprintf(
				"a unidade %s está inativa e não pode compor um produto vendável.", resumo.Codigo)
		}
	}
	if len(erros) > 0 {
		return nil, apperr.Validation(erros)
	}

	var gravada []UnidadeDaComposicao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.Composicao(ctx, id)
		if err != nil {
			return err
		}
		// A guarda roda DENTRO da transação, entre a leitura do conjunto atual e
		// a troca: fora dela, entre o SELECT e o DELETE+INSERT caberia uma venda
		// concorrente, e a alteração recusada em teoria passaria na prática.
		// (Isso fecha a corrida contra OUTRA alteração de composição, não a
		// corrida contra uma VENDA — ver o comentário de guardarComposicao.)
		if err := s.guardarComposicao(ctx, propriedade, produto, antes, c.UnidadeIDs, achadas); err != nil {
			return err
		}
		if err := s.repo.SubstituirComposicao(ctx, propriedade, id, c.UnidadeIDs); err != nil {
			return err
		}
		gravada, err = s.repo.Composicao(ctx, id)
		if err != nil {
			return err
		}
		// Cinto e suspensório: se por qualquer motivo a gravação tivesse
		// resultado em conjunto vazio, o produto sairia da transação vendável e
		// sem consumir nada. Desfaz em vez de publicar esse estado.
		if len(gravada) == 0 {
			return apperr.Validation(map[string]string{
				"unit_ids": fmt.Sprintf(
					"a composição do produto %q ficaria vazia; produto sem composição não pode ser vendido.",
					produto.Codigo),
			})
		}
		// Entidade `unit_type_members` com o id do PRODUTO: a tabela não tem
		// chave própria, e a pergunta que se faz depois é "o que mudou na
		// composição da Completa?".
		return s.trilha.Alteracao(ctx, entidadeComposicao, verboSubstituida, id,
			retratoDaComposicao(antes), retratoDaComposicao(gravada))
	})
	if err != nil {
		return nil, err
	}
	return gravada, nil
}

// guardarComposicao é o CRÍTICO desta rodada: mexer na composição de um produto
// que já foi vendido.
//
// ─────────────── O QUE FOI MEDIDO, EM POSTGRES REAL ───────────────
//
//	Completa vendida 2033-05-10→13 (WH-2026-0001, 8 blocos)
//	POST /units cria AP-99
//	PUT /unit-types/{completa}/members com os 8 atuais + AP-99  → 200
//	banco: composicao = 9, reservation_units = 8, stay_blocks = 8
//	AP-99 somado ao apto-2s, POST /reservations 2033-05-11→12   → 201
//	⇒ WH-2026-0002 dorme dentro da casa que WH-2026-0001 alugou inteira.
//
// A `EXCLUDE` não detecta e não tinha como: a linha que faltaria em stay_blocks
// NUNCA É INSERIDA, e o buraco é a AUSÊNCIA de uma linha. Constraint nenhuma vê
// ausência — foi por isso que quatro portas fechadas na rodada anterior (as duas
// de `active` na unidade, a do produto, a da unidade inativa entrando na
// composição) deixaram esta quinta aberta: todas elas guardam uma COLUNA, e o
// que muda aqui é um CONJUNTO.
//
// A REMOÇÃO é a mesma falha pela porta oposta, e também foi medida:
//
//	AP-03 tirado da composição da Completa (200, com a casa vendida)
//	POST /reservations da Completa em 2034-05-10→13 → 201 com 7 unidades,
//	  pelo preço de oito; AP-03 livre
//	POST /reservations do apto-2s 2034-05-11→12    → 201 em AP-03
//	⇒ o crítico original da Fase 1, reproduzido sem tocar em `active`.
//
// ─────────────── RECUSAR, E NÃO PROPAGAR ───────────────
//
// As duas saídas foram consideradas:
//
//   - PROPAGAR (inserir nas reservas vivas os blocos que faltam, e recusar a
//     alteração inteira se algum conflitar) é mais gentil com a operação e
//     resolve o caso da ADIÇÃO. Não resolve o da REMOÇÃO — ali propagar
//     significa APAGAR bloco de estadia vendida, que é exatamente o que o
//     `ON DELETE RESTRICT` de stay_blocks existe para impedir: some a prova de
//     quem ia dormir onde, e o hóspede que já recebeu o número do apartamento
//     deixa de ter apartamento. Custa ainda reimplementar aqui a alocação que
//     mora em `reservas` (AlocarComposicaoCompleta + VincularUnidades): duas
//     cópias da mesma regra em módulos diferentes é a forma mais confiável de
//     elas divergirem na próxima fase. E, no fundo, propagar reescreve o
//     passado — a venda entregaria um conjunto que ninguém vendeu —, contra a
//     regra 7 do CLAUDE.md.
//
//   - RECUSAR custa ao operador: com estadia futura de pé, a composição não
//     muda. Mas composição é cadastro raro e venda viva é o normal, então o
//     custo cai sobre o evento raro; a recusa nomeia as reservas, de modo que a
//     saída (remarcar, cancelar, esperar) é acionável; e a decisão fica na
//     trilha, porque o operador precisa tomá-la explicitamente.
//
// Escolhida a recusa.
//
// ─────────────── POR QUE `one_member` NÃO É TRATADO IGUAL ───────────────
//
// No `one_member` a venda escolheu UMA unidade no ato e a segurou; crescer o
// conjunto depois não muda o que ela segura, e recusar toda adição travaria a
// operação normal (o apto-2s quase sempre tem venda viva — ninguém conseguiria
// jamais acrescentar um apartamento ao pool). O que é recusado ali é só a
// remoção da unidade que uma venda viva OCUPA: sem isso a estadia continuaria
// hospedando numa unidade que já não pertence ao produto vendido, e toda
// derivação produto → composição → unidades (limpeza, check-in, mapa) deixaria
// essa estadia de fora.
//
// ─────────────── O QUE ESTA GUARDA NÃO FECHA ───────────────
//
// Ela roda dentro da transação, o que a serializa contra outra alteração de
// composição. NÃO a serializa contra uma VENDA concorrente: em READ COMMITTED,
// entre este SELECT e o commit cabe um `POST /reservations` de outra transação,
// e a composição alterada seria publicada com a venda nova já emitida sobre o
// conjunto antigo. Fechar isso é trabalho do banco — a invariante pedida no
// relatório (constraint trigger DEFERRABLE sobre unit_type_members,
// reservation_units e unit_types.consumes) é o que a torna impossível de
// atravessar, porque ela é conferida no COMMIT, não antes dele.
func (s *Service) guardarComposicao(
	ctx context.Context,
	propriedade uuid.UUID,
	produto Produto,
	antes []UnidadeDaComposicao,
	pedidos []uuid.UUID,
	achadas map[uuid.UUID]UnidadeResumida,
) error {
	atual := map[uuid.UUID]string{}
	for _, m := range antes {
		atual[m.UnidadeID] = m.Codigo
	}
	pedido := map[uuid.UUID]bool{}
	for _, id := range pedidos {
		pedido[id] = true
	}

	entram, saem := []string{}, []string{}
	saemIDs := []uuid.UUID{}
	for id := range pedido {
		if _, ja := atual[id]; !ja {
			entram = append(entram, achadas[id].Codigo)
		}
	}
	for id, codigo := range atual {
		if !pedido[id] {
			saem = append(saem, codigo)
			saemIDs = append(saemIDs, id)
		}
	}
	// Conjunto idêntico é no-op: a tela salva o formulário sem ninguém ter
	// tocado na grade, e recusar isso ensinaria o operador a temer o botão.
	if len(entram) == 0 && len(saem) == 0 {
		return nil
	}
	sort.Strings(entram)
	sort.Strings(saem)

	reservas, err := s.repo.ReservasVivasDoProduto(ctx, propriedade, produto.ID)
	if err != nil {
		return err
	}
	if len(reservas) == 0 {
		return nil
	}

	detalhes := map[string]any{
		"live_reservations":   reservas,
		"reservations_count":  len(reservas),
		"adding_unit_codes":   entram,
		"removing_unit_codes": saem,
	}

	if produto.Consome == ConsomeTodosMembros {
		return apperr.ResourceInUse.
			WithMessage(fmt.Sprintf(
				"A composição de %q não pode mudar enquanto %s de pé: %s. "+
					"O produto vende a casa por inteiro, e %s congelou o conjunto que entregaria — "+
					"mexer nele agora deixaria a estadia ocupando um conjunto diferente do que o produto declara, "+
					"que é como um estranho passa a dormir dentro da casa alugada inteira. "+
					"Remarque ou cancele %s, ou espere o período terminar.",
				produto.Codigo,
				plural(len(reservas), "houver estadia exclusiva", "houver estadias exclusivas"),
				codigosDasReservas(reservas),
				plural(len(reservas), "ela", "elas"),
				plural(len(reservas), "essa reserva", "essas reservas"))).
			WithDetails(detalhes)
	}

	// `one_member`: só a saída de unidade OCUPADA é recusada.
	ocupadas := []string{}
	for i, id := range saemIDs {
		for _, r := range reservas {
			if r.Ocupa(id) {
				ocupadas = append(ocupadas, fmt.Sprintf("%s (%s)", saem[i], r.Codigo))
				break
			}
		}
	}
	if len(ocupadas) == 0 {
		return nil
	}
	sort.Strings(ocupadas)
	detalhes["occupied_unit_codes"] = ocupadas

	return apperr.ResourceInUse.
		WithMessage(fmt.Sprintf(
			"%s de %q %s ocupada por estadia futura já vendida: %s. "+
				"Tirá-la da composição deixaria a venda hospedando fora do próprio produto. "+
				"Remarque a estadia antes, ou espere o período terminar.",
			plural(len(ocupadas), "A unidade", "As unidades"),
			produto.Codigo,
			plural(len(ocupadas), "está", "estão"),
			strings.Join(ocupadas, ", "))).
		WithDetails(detalhes)
}

// ─────────────────────────── Unidades ───────────────────────────────

func (s *Service) ListarUnidades(ctx context.Context, f Filtro) ([]Unidade, int64, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarUnidades(ctx, propriedade, f)
}

func (s *Service) BuscarUnidade(ctx context.Context, id uuid.UUID) (Unidade, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Unidade{}, err
	}
	return s.repo.BuscarUnidade(ctx, propriedade, id)
}

func (s *Service) CriarUnidade(ctx context.Context, c UnidadeEntrada) (Unidade, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Unidade{}, err
	}
	c.Normalizar()
	c = c.ComPadroes()

	var criada Unidade
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.CriarUnidade(ctx, propriedade, c)
		if err != nil {
			return err
		}
		criada, err = s.repo.BuscarUnidade(ctx, propriedade, id)
		if err != nil {
			return err
		}
		return s.trilha.Criacao(ctx, entidadeUnidade, audit.VerboCriado, id, criada)
	})
	if err != nil {
		return Unidade{}, err
	}
	return criada, nil
}

// SubstituirUnidade é o PUT. `active` vem no corpo como qualquer outro campo, e
// por isso passa pelas mesmas guardas do DELETE — ver guardarTransicaoDaUnidade.
func (s *Service) SubstituirUnidade(ctx context.Context, id uuid.UUID, c UnidadeEntrada) (Unidade, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Unidade{}, err
	}
	c.Normalizar()
	c = c.ComPadroes()

	var atualizada Unidade
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarUnidade(ctx, propriedade, id)
		if err != nil {
			return err
		}
		if err := s.guardarTransicaoDaUnidade(ctx, propriedade, antes, *c.Ativa); err != nil {
			return err
		}
		if err := s.repo.SubstituirUnidade(ctx, propriedade, id, c); err != nil {
			return err
		}
		atualizada, err = s.repo.BuscarUnidade(ctx, propriedade, id)
		if err != nil {
			return err
		}
		return s.trilha.Alteracao(ctx, entidadeUnidade,
			verboDaTransicao(antes.Ativa, atualizada.Ativa), id, antes, atualizada)
	})
	if err != nil {
		return Unidade{}, err
	}
	return atualizada, nil
}

// AtualizarUnidade é o PATCH — e era o buraco do CRÍTICO 1: `{"active": false}`
// devolvia 200 e tirava do ar a unidade que o DELETE recusava tirar.
func (s *Service) AtualizarUnidade(ctx context.Context, id uuid.UUID, a UnidadeAtualizar) (Unidade, error) {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Unidade{}, err
	}
	a.Normalizar()

	var atualizada Unidade
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarUnidade(ctx, propriedade, id)
		if err != nil {
			return err
		}
		if err := s.guardarTransicaoDaUnidade(ctx, propriedade, antes, ativoPedido(antes.Ativa, a.Ativa)); err != nil {
			return err
		}
		if err := s.repo.AtualizarUnidade(ctx, propriedade, id, a); err != nil {
			return err
		}
		atualizada, err = s.repo.BuscarUnidade(ctx, propriedade, id)
		if err != nil {
			return err
		}
		return s.trilha.Alteracao(ctx, entidadeUnidade,
			verboDaTransicao(antes.Ativa, atualizada.Ativa), id, antes, atualizada)
	})
	if err != nil {
		return Unidade{}, err
	}
	return atualizada, nil
}

// DesativarUnidade é o DELETE do contrato: `active = false`, nunca remoção.
func (s *Service) DesativarUnidade(ctx context.Context, id uuid.UUID) error {
	propriedade, err := propriedadeDoAtor(ctx)
	if err != nil {
		return err
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		unidade, err := s.repo.BuscarUnidade(ctx, propriedade, id)
		if err != nil {
			return err
		}
		if !unidade.Ativa {
			return nil
		}
		if err := s.guardarTransicaoDaUnidade(ctx, propriedade, unidade, false); err != nil {
			return err
		}
		if err := s.repo.DesativarUnidade(ctx, propriedade, id); err != nil {
			return err
		}
		depois, err := s.repo.BuscarUnidade(ctx, propriedade, id)
		if err != nil {
			return err
		}
		// Alteracao pelo mesmo motivo do produto: a unidade continua na tabela,
		// e as três portas da desativação gravam a mesma linha.
		return s.trilha.Alteracao(ctx, entidadeUnidade, verboDesativado, id, unidade, depois)
	})
}

// guardarTransicaoDaUnidade é a guarda ÚNICA da coluna `active` de uma unidade.
//
// Existe porque desativar unidade é a mesma decisão que "excluí-la", e a Fase 1
// só protegia o DELETE: a revisão adversarial desativou AP-03 por
// `PATCH /units/{id} {"active":false}` (200, com token do perfil `usuario`) e
// em seguida vendeu a White House Completa — `all_members`, capacidade 24,
// R$ 5.500/noite — entregando SETE unidades, porque a consulta de candidatas
// filtra `u.active`. A `EXCLUDE` não pega: a oitava linha nunca é inserida,
// então não há sobreposição a detectar. Medido nesta árvore: candidatas ativas
// da Completa caem de 8 para 7 com a composição intacta em 8.
//
// As duas direções têm guardas diferentes porque os danos são diferentes:
//
//   - DESATIVAR abre o buraco (a venda seguinte trava menos do que promete);
//   - REATIVAR entrega o buraco já aberto (a unidade que ficou de fora de uma
//     venda exclusiva volta ao inventário DENTRO das datas dela).
func (s *Service) guardarTransicaoDaUnidade(ctx context.Context, propriedade uuid.UUID, atual Unidade, querAtiva bool) error {
	switch {
	case atual.Ativa && !querAtiva:
		return s.guardarDesativacaoDaUnidade(ctx, propriedade, atual)
	case !atual.Ativa && querAtiva:
		return s.guardarReativacaoDaUnidade(ctx, propriedade, atual)
	default:
		return nil
	}
}

// guardarDesativacaoDaUnidade recusa em duas situações, e cada uma protege uma
// coisa diferente:
//
//   - ocupação futura em stay_blocks (`hold`/`confirmed`): há gente com estadia
//     marcada naquele apartamento. Desativar o tiraria da alocação e do mapa de
//     ocupação com a reserva de pé — a operação deixaria de saber qual quarto
//     preparar. A FK de stay_blocks é ON DELETE RESTRICT justamente porque
//     apagar unidade apagaria a prova de quem dormiu onde; a desativação
//     precisa da mesma recusa, e nenhuma constraint a faz;
//   - ainda compõe produto: a próxima venda daquele produto travaria menos
//     unidades do que promete. Tire a unidade da composição primeiro
//     (PUT /unit-types/{id}/members) e desative depois — nessa ordem a decisão
//     é explícita, fica na trilha, e o preço do produto pode ser revisto junto.
//
// Vale para `one_member` também, e não só para a Completa: no `one_member` o
// dano é menor (o inventário encolhe, ninguém dorme onde não devia), mas a
// ordem certa é a mesma, e a diferença aparece na MENSAGEM, não na decisão —
// uma guarda que só vale para metade dos produtos é uma guarda que alguém
// contorna trocando o `consumes`.
func (s *Service) guardarDesativacaoDaUnidade(ctx context.Context, propriedade uuid.UUID, atual Unidade) error {
	bloqueios, err := s.repo.ContarBloqueiosFuturosDaUnidade(ctx, propriedade, atual.ID)
	if err != nil {
		return err
	}
	vinculos, err := s.repo.ProdutosQueUsamAUnidade(ctx, atual.ID)
	if err != nil {
		return err
	}
	if bloqueios == 0 && len(vinculos) == 0 {
		return nil
	}

	produtos, exclusivos := codigosDosVinculos(vinculos)
	// Os motivos vão JUNTOS no mesmo details: quem desativa precisa ver tudo o
	// que falta resolver, não descobrir um impedimento por tentativa.
	return apperr.ResourceInUse.
		WithMessage(mensagemDaUnidadeEmUso(bloqueios, produtos, exclusivos)).
		WithDetails(map[string]any{
			"blocks_count": bloqueios,
			"unit_types":   produtos,
			// Os `all_members` saem destacados porque a ação do operador é
			// outra: aqui não basta "esperar a estadia passar", é preciso
			// redefinir o produto e revisar o preço dele.
			"exclusive_unit_types": exclusivos,
		})
}

// guardarReativacaoDaUnidade fecha o caminho de VOLTA.
//
// Cenário medido pela revisão: AP-03 fora do ar, a Completa vendida nesse
// intervalo com sete blocos, AP-03 de volta ao ar. A partir daí a unidade está
// livre e vendável DENTRO das datas de uma estadia exclusiva já fechada — e a
// venda seguinte de "Apartamento 2 Suítes" nas mesmas datas é aceita, porque não
// existe bloco em AP-03 com que colidir. Não é "entregar 7 de 8": é um estranho
// dormindo dentro da casa alugada inteira.
//
// Com as guardas de desativação e de composição no lugar, esse estado não nasce
// mais por aqui; ele já EXISTE nos bancos que rodaram a Fase 1, e é por isso que
// a recusa fica: a reativação é o momento em que o dano se realizaria.
//
// A recusa nomeia as reservas porque a saída não está neste módulo — quem
// resolve é reservas (acrescentar o bloco que falta, remarcar ou cancelar) ou o
// próprio calendário, quando o período passar.
func (s *Service) guardarReativacaoDaUnidade(ctx context.Context, propriedade uuid.UUID, atual Unidade) error {
	reservas, err := s.repo.ReservasExclusivasSemAUnidade(ctx, propriedade, atual.ID)
	if err != nil {
		return err
	}
	if len(reservas) == 0 {
		return nil
	}

	codigos := make([]string, 0, len(reservas))
	for _, r := range reservas {
		codigos = append(codigos, r.Codigo)
	}

	return apperr.ResourceInUse.
		WithMessage(fmt.Sprintf(
			"Reativar %s a devolveria ao inventário dentro de %s: %s. "+
				"Resolva %s antes de reativar, ou aguarde o período terminar.",
			atual.Codigo,
			plural(len(reservas),
				"uma estadia exclusiva já vendida sem ela",
				"estadias exclusivas já vendidas sem ela"),
			strings.Join(codigos, ", "),
			plural(len(reservas), "essa reserva", "essas reservas"))).
		WithDetails(map[string]any{
			"conflicting_reservations": reservas,
			"reservations_count":       len(reservas),
		})
}

// ─────────────────────────── Auxiliares ─────────────────────────────

// ativoPedido resolve o `active` que o PATCH está pedindo. Ausente não mexe —
// é o terceiro estado do Opt, e confundi-lo com `false` faria todo PATCH de
// `sort_order` tentar desativar o registro.
//
// `null` explícito não chega aqui: o Validar do DTO já o recusa com 422 ("não
// pode ser nulo"), porque a coluna é NOT NULL.
func ativoPedido(atual bool, pedido httpx.Opt[bool]) bool {
	if v, ok := pedido.Definido(); ok {
		return v
	}
	return atual
}

// textoPedido é o `ativoPedido` das colunas de texto NOT NULL: ausente mantém o
// que está gravado. `null` explícito não chega aqui — o Validar do DTO já o
// recusa com 422, porque a coluna é NOT NULL.
func textoPedido(atual string, pedido httpx.Opt[string]) string {
	if v, ok := pedido.Definido(); ok {
		return v
	}
	return atual
}

// codigosDasReservas monta a lista legível que vai na recusa. É o que torna o
// 409 acionável: sem os códigos, o gestor lê "não pode" e não sabe o que
// resolver.
func codigosDasReservas(reservas []ReservaViva) string {
	out := make([]string, 0, len(reservas))
	for _, r := range reservas {
		out = append(out, fmt.Sprintf("%s (%s → %s)", r.Codigo, r.CheckIn, r.CheckOut))
	}
	return strings.Join(out, ", ")
}

// codigosDosVinculos separa os códigos por gravidade: todos, e os que quebram
// exclusividade.
func codigosDosVinculos(vinculos []VinculoDeComposicao) (todos, exclusivos []string) {
	todos, exclusivos = []string{}, []string{}
	for _, v := range vinculos {
		todos = append(todos, v.Codigo)
		if v.Consome == ConsomeTodosMembros {
			exclusivos = append(exclusivos, v.Codigo)
		}
	}
	return todos, exclusivos
}

func mensagemDaUnidadeEmUso(bloqueios int, produtos, exclusivos []string) string {
	partes := []string{}
	if bloqueios > 0 {
		partes = append(partes, "tem ocupação futura no calendário")
	}
	switch {
	case len(exclusivos) > 0:
		partes = append(partes, fmt.Sprintf(
			"compõe %s, que %s a casa por inteiro — desativá-la venderia o produto entregando uma unidade a menos",
			strings.Join(exclusivos, ", "),
			plural(len(exclusivos), "vende", "vendem")))
	case len(produtos) > 0:
		partes = append(partes, fmt.Sprintf(
			"ainda compõe %s; tire-a da composição antes de desativá-la",
			strings.Join(produtos, ", ")))
	}
	return "A unidade " + strings.Join(partes, " e ") + "."
}

// plural evita a frase robótica com "(s)" no meio de uma mensagem que o
// operador lê na tela.
func plural(n int, um, muitos string) string {
	if n == 1 {
		return um
	}
	return muitos
}
