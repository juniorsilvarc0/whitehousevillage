package vitrine

// POST /public/holds — a pré-reserva feita pelo próprio cliente no site (passo
// B1 de docs/unificacao-site-crm.md).
//
// A regra do plano vale aqui mais do que em qualquer outra rota pública: o
// site pergunta, não calcula — e, quando grava, grava pelo MESMO caminho do
// painel. Não existe "reserva do site": existe `reservas.Servico.Criar`, com a
// mesma idempotência, o mesmo orçamento recalculado no servidor, o mesmo
// snapshot por noite e a mesma constraint `EXCLUDE` como única defesa contra
// overbooking (o `23P01` vira 409 DATE_CONFLICT lá dentro, não aqui).
//
// Quem assina a escrita é a CONTA DE SERVIÇO do site (seed: perfil `vitrine`,
// só `contacts:create` e `reservations:create`, sem senha utilizável). É ela
// que o painel mostra como dona da venda, e é a matriz de RBAC — dado, não
// código — que limita o que uma requisição anônima consegue fazer.
//
// O que a rota acrescenta é só o que uma porta anônima precisa:
//   - aceite explícito do uso dos dados para a reserva, anotado com a data
//     na ficha do contato que nasce aqui;
//   - teto de pré-reservas abertas por telefone (negação de inventário: quem
//     não paga nada não segura a casa inteira) — o limite por IP fica no
//     roteador;
//   - resposta recortada: código, datas e valores. Nada de id interno, dono,
//     corretor, tabela, versão de política ou nome interno do produto.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/contatos"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/idempotencia"
)

const (
	// EmailDaContaDoSite é a conta de serviço que assina as escritas do site
	// (cmd/seed). Constante aqui, e não import do seed: a API não depende do
	// binário de carga.
	EmailDaContaDoSite = "vitrine@site.whitehouse.invalid"

	// OrigemSite é o `reservations.source` da venda que nasce no site — é por
	// ela que o painel separa o que veio do balcão do que veio da internet.
	OrigemSite = "site"

	// maxPreReservasAbertasPorTelefone é o teto de pré-reservas do site ainda
	// em `hold` para o mesmo telefone. Duas cobrem quem está em dúvida entre
	// dois fins de semana; a terceira já é segurar a casa sem pagar.
	maxPreReservasAbertasPorTelefone = 2

	// BaseLegalDoSite é o `lgpd_basis` do contato que nasce aqui: `contrato`
	// (LGPD art. 7º, V — procedimentos preliminares a um contrato a pedido do
	// titular), o mesmo de quem já se hospedou (docs/db.md §16). NÃO é
	// `consentimento` de marketing: a caixa marcada autoriza usar os dados
	// para ESTA reserva, não mandar oferta — `marketing_opt_in` fica falso e
	// `consent_at` (que no cadastro é o aceite de marketing) fica vazio.
	BaseLegalDoSite = "contrato"
)

// contaDoSite carrega (e guarda) o id da conta de serviço. A identidade
// completa — perfil e matriz — é relida a cada pedido por CarregarSessao: se
// alguém desativar a conta ou mexer no perfil no painel, vale na hora.
type contaDoSite struct {
	pool *pgxpool.Pool
	auth *auth.Repository

	mu sync.Mutex
	id uuid.UUID
}

func (c *contaDoSite) identidade(ctx context.Context, casa uuid.UUID) (*auth.Usuario, error) {
	c.mu.Lock()
	id := c.id
	c.mu.Unlock()
	if id == uuid.Nil {
		err := c.pool.QueryRow(ctx,
			`SELECT id FROM users WHERE email = $1 AND property_id = $2 AND deleted_at IS NULL`,
			EmailDaContaDoSite, casa).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.Internal.WithCause(errors.New("vitrine: conta de serviço do site ausente (rode o seed)"))
		}
		if err != nil {
			return nil, db.MapError(err)
		}
		c.mu.Lock()
		c.id = id
		c.mu.Unlock()
	}
	u, err := c.auth.CarregarSessao(ctx, id)
	if err != nil {
		// Conta desativada no painel: a pré-reserva pelo site está fechada.
		// Para o visitante isso é indisponibilidade, não "não autenticado".
		return nil, apperr.Internal.WithCause(errors.New("vitrine: conta de serviço do site inativa"))
	}
	return u, nil
}

// PedidoDePreReserva é o que o visitante envia. O decoder recusa campo a
// mais: desconto, corretor, origem, tabela — nada disso é dele.
type PedidoDePreReserva struct {
	UnitTypeID uuid.UUID `json:"unit_type_id" validate:"required"`
	CheckIn    string    `json:"check_in" validate:"required"`
	CheckOut   string    `json:"check_out" validate:"required"`
	Hospedes   int       `json:"guests_count" validate:"required,min=1"`

	Nome     string  `json:"name" validate:"required,min=2,max=120"`
	Telefone string  `json:"phone" validate:"required"`
	Email    *string `json:"email"`
	// Consentimento precisa vir `true`: sem ele não há base legal para
	// guardar nome e telefone de quem nunca assinou nada.
	Consentimento bool    `json:"consent"`
	Observacoes   *string `json:"notes"`
}

// Validar confere a forma. Regra de negócio (mínimo de noites, lotação,
// tarifa) é do motor, e chega pelo orçamento.
func (p PedidoDePreReserva) Validar() map[string]string {
	falhas := map[string]string{}
	if !p.Consentimento {
		falhas["consent"] = "é preciso aceitar o uso dos dados para a reserva."
	}
	if _, err := contatos.NormalizarTelefone(p.Telefone); err != nil || strings.TrimSpace(p.Telefone) == "" {
		falhas["phone"] = "informe um telefone com DDD."
	}
	if p.Email != nil && strings.TrimSpace(*p.Email) != "" && !httpx.ValidarValor(strings.TrimSpace(*p.Email), "email") {
		falhas["email"] = "e-mail inválido."
	}
	if p.Observacoes != nil && len(*p.Observacoes) > 1000 {
		falhas["notes"] = "no máximo 1000 caracteres."
	}
	return falhas
}

// PreReservaPublica é a resposta: o que o hóspede precisa para pagar o sinal
// e falar com a casa. O código (WH-AAAA-NNNN) é o que ele cita no WhatsApp.
type PreReservaPublica struct {
	Codigo   string    `json:"code"`
	Status   string    `json:"status"`
	Produto  string    `json:"product_name"`
	CheckIn  string    `json:"check_in"`
	CheckOut string    `json:"check_out"`
	Noites   int       `json:"night_count"`
	Hospedes int       `json:"guests_count"`
	Total    int64     `json:"total_cents"`
	Sinal    int64     `json:"deposit_cents"`
	Saldo    int64     `json:"balance_cents"`
	ExpiraEm time.Time `json:"hold_expires_at"`
}

// reservaGravada é o recorte do envelope que reservas.Criar devolve (ou
// repete, byte a byte, na segunda chamada com a mesma chave).
type reservaGravada struct {
	Data struct {
		Codigo   string     `json:"code"`
		Status   string     `json:"status"`
		CheckIn  string     `json:"check_in"`
		CheckOut string     `json:"check_out"`
		Noites   int        `json:"night_count"`
		Hospedes int        `json:"guests_count"`
		Total    int64      `json:"total_cents"`
		Sinal    int64      `json:"deposit_cents"`
		Saldo    int64      `json:"balance_cents"`
		ExpiraEm *time.Time `json:"hold_expires_at"`
	} `json:"data"`
}

// PreReservar — POST /public/holds
func (h *Handler) PreReservar(w http.ResponseWriter, r *http.Request) {
	chave, err := idempotencia.Chave(r.Header.Get(idempotencia.NomeDoHeader))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	ctx, err := h.contexto(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[PedidoDePreReserva](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	telefone, _ := contatos.NormalizarTelefone(corpo.Telefone)

	// 1. O orçamento PÚBLICO primeiro, no contexto sem sessão: as recusas do
	//    motor (mínimo de noites, lotação, data sob consulta) saem com o nome
	//    de vitrine, e nada é gravado para um pedido que o motor recusa.
	entrada, err := disponibilidade.Pedido{
		UnitTypeID: corpo.UnitTypeID,
		CheckIn:    corpo.CheckIn,
		CheckOut:   corpo.CheckOut,
		Hospedes:   corpo.Hospedes,
	}.Normalizar()
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	hoje, err := h.disp.Hoje(ctx)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if entrada.CheckIn.Before(hoje) {
		httpx.Error(w, r, apperr.Validation(map[string]string{"check_in": "a data de entrada já passou."}))
		return
	}
	if _, err := h.disp.Orcar(ctx, entrada); err != nil {
		httpx.Error(w, r, err)
		return
	}
	produto, err := h.nomeDeVitrine(ctx, corpo.UnitTypeID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	// 2. Daqui em diante quem escreve é a conta de serviço do site.
	casa, _ := h.casas.resolver(ctx)
	conta, err := h.conta.identidade(ctx, casa)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	ctxConta := auth.WithUser(ctx, conta)

	// 3. Teto de pré-reservas abertas por telefone. Uma repetição da MESMA
	//    chave não conta como nova: ela devolve a resposta guardada.
	abertas, err := h.preReservasAbertas(ctx, telefone)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if abertas >= maxPreReservasAbertasPorTelefone && !h.chaveJaUsada(ctx, chave, conta.ID) {
		httpx.Error(w, r, apperr.HoldLimitReached.WithMessage(
			"Você já tem pré-reservas em aberto. Conclua uma delas ou fale com a gente pelo WhatsApp."))
		return
	}

	// 4. O contato: quem já tem este telefone é a mesma pessoa — a ficha
	//    existente não é sobrescrita por um formulário anônimo.
	contato, err := h.contatoDoSite(ctxConta, corpo, telefone)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	// 5. A reserva, pelo serviço do painel.
	pedido := reservas.ReservaCriar{
		UnitTypeID:  corpo.UnitTypeID,
		CheckIn:     corpo.CheckIn,
		CheckOut:    corpo.CheckOut,
		Hospedes:    corpo.Hospedes,
		ContactID:   contato,
		Origem:      OrigemSite,
		Observacoes: corpo.Observacoes,
	}
	pedido.Normalizar()
	res, err := h.reservas.Criar(ctxConta, chave, pedido)
	if err != nil {
		httpx.Error(w, r, recortarErro(err))
		return
	}

	bruto := res.Bruto
	if bruto == nil {
		if bruto, err = json.Marshal(res.Corpo); err != nil {
			httpx.Error(w, r, apperr.Internal.WithCause(err))
			return
		}
	}
	var gravada reservaGravada
	if err := json.Unmarshal(bruto, &gravada); err != nil {
		httpx.Error(w, r, apperr.Internal.WithCause(err))
		return
	}
	d := gravada.Data
	saida := PreReservaPublica{
		Codigo: d.Codigo, Status: d.Status, Produto: produto,
		CheckIn: d.CheckIn, CheckOut: d.CheckOut, Noites: d.Noites, Hospedes: d.Hospedes,
		Total: d.Total, Sinal: d.Sinal, Saldo: d.Saldo,
	}
	if d.ExpiraEm != nil {
		saida.ExpiraEm = *d.ExpiraEm
	}
	httpx.JSON(w, res.Status, saida)
}

// nomeDeVitrine é o nome que o visitante viu no catálogo.
func (h *Handler) nomeDeVitrine(ctx context.Context, id uuid.UUID) (string, error) {
	catalogo, _, err := h.disp.Catalogo(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range catalogo {
		if p.ID == id {
			return p.NomePublico, nil
		}
	}
	return "", apperr.NotFound("Produto")
}

// preReservasAbertas conta as pré-reservas do site ainda em `hold`, e ainda
// no prazo, para este telefone.
func (h *Handler) preReservasAbertas(ctx context.Context, telefone string) (int, error) {
	var n int
	err := h.pool.QueryRow(ctx, `
		SELECT count(*)
		  FROM reservations r
		  JOIN contacts c ON c.id = r.contact_id
		 WHERE c.phone_e164 = $1
		   AND r.source = $2
		   AND r.status = 'hold'
		   AND (r.hold_expires_at IS NULL OR r.hold_expires_at > now())`,
		telefone, OrigemSite).Scan(&n)
	if err != nil {
		return 0, db.MapError(err)
	}
	return n, nil
}

// chaveJaUsada diz se esta chave de idempotência já produziu uma resposta: a
// repetição de um pedido que deu certo não pode ser barrada pelo teto que o
// próprio pedido ajudou a encher.
func (h *Handler) chaveJaUsada(ctx context.Context, chave string, conta uuid.UUID) bool {
	var existe bool
	err := h.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM idempotency_keys
		                WHERE key = $1 AND endpoint = $2 AND actor_id = $3 AND status IS NOT NULL)`,
		chave, reservas.RotaDeCriacao, conta).Scan(&existe)
	return err == nil && existe
}

// contatoDoSite devolve o contato deste telefone, criando-o com o
// consentimento registrado quando ainda não existe.
func (h *Handler) contatoDoSite(ctx context.Context, p PedidoDePreReserva, telefone string) (uuid.UUID, error) {
	if id, achou, err := h.contatosRepo.BuscarIDPorTelefone(ctx, telefone); err != nil {
		return uuid.Nil, err
	} else if achou {
		return id, nil
	}
	base := BaseLegalDoSite
	nota := "Cadastro feito pelo próprio cliente na pré-reserva do site; aceite do uso dos dados para a reserva em " +
		time.Now().In(fusoDaCasa).Format("02/01/2006 15:04") + "."
	tel := telefone
	novo, err := h.contatos.Criar(ctx, contatos.Criar{
		Nome:      p.Nome,
		Email:     p.Email,
		Telefone:  &tel,
		Notas:     &nota,
		BaseLegal: &base,
	})
	if err == nil {
		return novo.ID, nil
	}
	// Corrida: outro pedido com o mesmo telefone criou o contato entre a
	// busca e a inserção. A constraint decidiu; usamos quem ganhou.
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Code == apperr.CodeContactDuplicate {
		if id, achou, errBusca := h.contatosRepo.BuscarIDPorTelefone(ctx, telefone); errBusca == nil && achou {
			return id, nil
		}
	}
	return uuid.Nil, err
}

// recortarErro tira do erro o que é vocabulário interno. O 409 da constraint
// carrega a unidade física ("COB-01"), o nome da constraint e o período em
// disputa — úteis no painel, que escolhe outra unidade; para o visitante, a
// resposta é só "essas datas foram tomadas". Os demais erros do serviço de
// reservas passam como estão: as recusas de regra já saíram, com o nome de
// vitrine, no orçamento público feito antes.
func recortarErro(err error) error {
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Code == apperr.CodeDateConflict {
		return apperr.DateConflict
	}
	return err
}

// fusoDaCasa só formata a data do aceite na nota da ficha.
var fusoDaCasa = func() *time.Location {
	if l, err := time.LoadLocation("America/Fortaleza"); err == nil {
		return l
	}
	return time.FixedZone("-03", -3*3600)
}()
