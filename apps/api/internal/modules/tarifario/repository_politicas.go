package tarifario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Chaves dos advisory locks que serializam a PUBLICAÇÃO de política.
//
// A numeração da versão é `max(version) + 1`, e sem trava duas publicações
// simultâneas leem o mesmo máximo: a segunda morre no `UNIQUE (property_id,
// version)` e a gestão vê um erro de constraint por ter clicado duas vezes.
// Com a trava, a segunda espera e publica a versão seguinte — que é o que
// qualquer pessoa esperaria de "publicar de novo".
//
// Publicar política é ato raro (dias, não segundos), então o custo da
// serialização é zero. O formato da chave segue o de
// `users.ChaveDaTravaDeAdministradores`: data da decisão + sequência.
const (
	ChaveDaTravaDePoliticaComercial      int64 = 2026_0826_0002
	ChaveDaTravaDePoliticaDeCancelamento int64 = 2026_0826_0003
)

// travarPublicacao pega o advisory lock de transação. Fora de transação o lock
// seria solto na hora e a trava não valeria nada — por isso falha alto.
func (r *Repository) travarPublicacao(ctx context.Context, chave int64) error {
	if !db.EmTransacao(ctx) {
		return apperr.Internal.WithCause(errors.New(
			"publicação de política fora de transação: o advisory lock não protegeria a numeração"))
	}
	if _, err := r.exec(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, chave); err != nil {
		return db.MapError(err)
	}
	return nil
}

// ═══════════════════════ Política comercial ═══════════════════════

// Os percentuais são `numeric(5,2)` no banco e `number` no contrato. O
// `::float8` é explícito para o driver nunca ter de adivinhar a conversão —
// duas casas decimais cabem em float sem perda, e a coluna continua numeric,
// que é o que impede o binário de ponto flutuante de entrar no banco.
const camposDaPoliticaComercial = `
	cp.id, cp.version, cp.deposit_pct::float8, cp.balance_due_days, cp.hold_hours,
	cp.discount_auto_pct::float8, cp.discount_approval_pct::float8, cp.event_deposit_cents,
	cp.hold_extension_hours, cp.hold_max_extensions, cp.quote_validity_days,
	cp.valid_from, cp.created_at`

// umaPoliticaComercial roda a consulta de UMA política e traduz `sem linha` no
// 404 do contrato.
func (r *Repository) umaPoliticaComercial(ctx context.Context, q string, args ...any) (PoliticaComercial, error) {
	var (
		pol      PoliticaComercial
		validoDe time.Time
	)
	err := r.exec(ctx).QueryRow(ctx, q, args...).Scan(
		&pol.ID, &pol.Versao, &pol.SinalPct, &pol.SaldoDiasAntes, &pol.HoldHoras,
		&pol.DescontoAutoPct, &pol.DescontoAprovacaoPct, &pol.CaucaoDeEventoCents,
		&pol.ExtensaoDeHoldHoras, &pol.ExtensoesDeHoldMax, &pol.ValidadeOrcamentoDia,
		&validoDe, &pol.CriadaEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return pol, apperr.NotFound("Política comercial")
	}
	if err != nil {
		return pol, db.MapError(err)
	}
	pol.ValidoDe = DataDe(validoDe)
	return pol, nil
}

// umaPoliticaDeCancelamento devolve o cabeçalho SEM as faixas: quem junta as
// duas coisas é o service, porque a leitura da versão e a das faixas são duas
// consultas e só o service sabe se elas precisam correr na mesma transação.
func (r *Repository) umaPoliticaDeCancelamento(ctx context.Context, q string, args ...any) (PoliticaDeCancelamento, error) {
	var (
		pol      PoliticaDeCancelamento
		validoDe time.Time
	)
	err := r.exec(ctx).QueryRow(ctx, q, args...).Scan(&pol.ID, &pol.Versao, &pol.Nome, &validoDe)
	if errors.Is(err, pgx.ErrNoRows) {
		return pol, apperr.NotFound("Política de cancelamento")
	}
	if err != nil {
		return pol, db.MapError(err)
	}
	pol.ValidoDe = DataDe(validoDe)
	pol.Faixas = []FaixaDeCancelamento{}
	return pol, nil
}

// BuscarPoliticaComercialVigente devolve a maior versão cujo `valid_from` já
// chegou no calendário da propriedade.
func (r *Repository) BuscarPoliticaComercialVigente(ctx context.Context, prop uuid.UUID) (PoliticaComercial, error) {
	const q = `
		SELECT ` + camposDaPoliticaComercial + `
		  FROM commercial_policies cp
		  JOIN properties p ON p.id = cp.property_id
		 WHERE cp.property_id = $1 AND cp.valid_from <= ` + hoje + `
		 ORDER BY cp.version DESC
		 LIMIT 1`

	return r.umaPoliticaComercial(ctx, q, prop)
}

// BuscarPoliticaComercialPorVersao é como a tela de uma reserva antiga mostra a
// política que ela congelou em `reservations.policy_version`.
func (r *Repository) BuscarPoliticaComercialPorVersao(ctx context.Context, prop uuid.UUID, versao int) (PoliticaComercial, error) {
	const q = `
		SELECT ` + camposDaPoliticaComercial + `
		  FROM commercial_policies cp
		 WHERE cp.property_id = $1 AND cp.version = $2`

	return r.umaPoliticaComercial(ctx, q, prop, versao)
}

// UltimaPoliticaComercial é a de maior versão, tenha ela entrado em vigor ou
// não. É contra ELA que a antedatação é conferida: uma versão agendada para o
// mês que vem já ocupou o número, e publicar algo com vigência anterior faria a
// ordem das versões deixar de contar a história na sequência certa.
func (r *Repository) UltimaPoliticaComercial(ctx context.Context, prop uuid.UUID) (PoliticaComercial, bool, error) {
	const q = `
		SELECT ` + camposDaPoliticaComercial + `
		  FROM commercial_policies cp
		 WHERE cp.property_id = $1
		 ORDER BY cp.version DESC
		 LIMIT 1`

	p, err := r.umaPoliticaComercial(ctx, q, prop)
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Code == apperr.CodeNotFound {
		return PoliticaComercial{}, false, nil
	}
	if err != nil {
		return PoliticaComercial{}, false, err
	}
	return p, true, nil
}

// sobrescritaDaPolitica é o que o PUT muda na linha copiada da versão anterior,
// com os nomes das COLUNAS como chave JSON (é o formato que
// `jsonb_populate_record` lê). Campo opcional ausente fica fora do JSON — e
// coluna fora do JSON é coluna herdada.
type sobrescritaDaPolitica struct {
	SinalPct             float64 `json:"deposit_pct"`
	SaldoDiasAntes       int     `json:"balance_due_days"`
	HoldHoras            int     `json:"hold_hours"`
	DescontoAutoPct      float64 `json:"discount_auto_pct"`
	DescontoAprovacaoPct float64 `json:"discount_approval_pct"`
	ValidoDe             string  `json:"valid_from"`
	CaucaoDeEventoCents  *int64  `json:"event_deposit_cents,omitempty"`
	ExtensaoDeHoldHoras  *int    `json:"hold_extension_hours,omitempty"`
	ExtensoesDeHoldMax   *int    `json:"hold_max_extensions,omitempty"`
	ValidadeOrcamentoDia *int    `json:"quote_validity_days,omitempty"`
}

func novaSobrescrita(e PoliticaComercialEntrada) sobrescritaDaPolitica {
	return sobrescritaDaPolitica{
		SinalPct:             *e.SinalPct,
		SaldoDiasAntes:       *e.SaldoDiasAntes,
		HoldHoras:            *e.HoldHoras,
		DescontoAutoPct:      *e.DescontoAutoPct,
		DescontoAprovacaoPct: *e.DescontoAprovacaoPct,
		ValidoDe:             e.ValidoDe.String(),
		CaucaoDeEventoCents:  definido(e.CaucaoDeEventoCents),
		ExtensaoDeHoldHoras:  definido(e.ExtensaoDeHoldHoras),
		ExtensoesDeHoldMax:   definido(e.ExtensoesDeHoldMax),
		ValidadeOrcamentoDia: definido(e.ValidadeOrcamentoDia),
	}
}

// definido devolve o valor do Opt quando há valor, e nil para ausente ou `null`
// — as duas formas de "não mudei" numa coluna NOT NULL.
func definido[T any](o httpx.Opt[T]) *T {
	if v, ok := o.Definido(); ok {
		return &v
	}
	return nil
}

// PublicarPoliticaComercial grava a VERSÃO NOVA. Nunca um UPDATE: as reservas já
// emitidas apontam para o número da versão, e reescrever a linha mudaria a regra
// sob os pés delas (CLAUDE.md regra 7).
//
// A versão nova é a ANTERIOR COPIADA PELO BANCO, com o pedido por cima (F2-05).
// A versão antiga deste INSERT listava as colunas nome a nome, e
// `quote_validity_days` ficou de fora dela: toda publicação devolvia a validade
// ao DEFAULT 7, em silêncio (risco R6). Copiando a linha inteira
// (`jsonb_populate_record(anterior, pedido)`), coluna que nasce amanhã é herdada
// sem ninguém lembrar de listá-la.
//
// O que NÃO se herda é a identidade da linha: `id`, `version` e `created_at`.
// Coluna nova que também seja identidade ou autoria da linha (um `created_by`,
// por exemplo) tem de entrar nessa lista — senão a versão nova sairia assinada
// pelo autor da anterior. O teste PoliticaNaoPerdeColuna confere o catálogo e
// reprova coluna que ele não conhece, justamente para forçar essa decisão.
func (r *Repository) PublicarPoliticaComercial(ctx context.Context, prop uuid.UUID, e PoliticaComercialEntrada) (int, error) {
	pedido, err := json.Marshal(novaSobrescrita(e))
	if err != nil {
		return 0, apperr.Internal.WithCause(err)
	}

	// O LATERAL avalia a função uma vez por linha; `(f(x)).*` no SELECT a
	// avaliaria uma vez por COLUNA, e cada avaliação sortearia outro uuid.
	const qCopia = `
		WITH anterior AS (
		    SELECT cp
		      FROM commercial_policies cp
		     WHERE cp.property_id = $1
		     ORDER BY cp.version DESC
		     LIMIT 1
		)
		INSERT INTO commercial_policies
		SELECT nova.*
		  FROM anterior a,
		       LATERAL jsonb_populate_record(a.cp, $2::jsonb || jsonb_build_object(
		           'id',         gen_random_uuid(),
		           'version',    (a.cp).version + 1,
		           'created_at', now())) AS nova
		RETURNING version`

	var versao int
	err = r.exec(ctx).QueryRow(ctx, qCopia, prop, pedido).Scan(&versao)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.publicarPrimeiraPoliticaComercial(ctx, prop, e)
	}
	if err != nil {
		return 0, erroDePublicacao(err)
	}
	return versao, nil
}

// publicarPrimeiraPoliticaComercial é a primeira versão da casa: não há linha
// para copiar, e o campo opcional ausente fica FORA da lista de colunas — quem
// o preenche é o DEFAULT do banco. Repetir o DEFAULT aqui seria o mesmo número
// em dois lugares, e o de cá envelheceria no primeiro ALTER.
func (r *Repository) publicarPrimeiraPoliticaComercial(ctx context.Context, prop uuid.UUID, e PoliticaComercialEntrada) (int, error) {
	colunas := []string{
		"property_id", "version", "deposit_pct", "balance_due_days", "hold_hours",
		"discount_auto_pct", "discount_approval_pct", "valid_from",
	}
	valores := []string{"$1", "1", "$2::float8::numeric", "$3", "$4",
		"$5::float8::numeric", "$6::float8::numeric", "$7::date"}
	args := []any{prop, *e.SinalPct, *e.SaldoDiasAntes, *e.HoldHoras,
		*e.DescontoAutoPct, *e.DescontoAprovacaoPct, e.ValidoDe.String()}

	opcional := func(coluna string, v any, ok bool) {
		if !ok {
			return
		}
		args = append(args, v)
		colunas = append(colunas, coluna)
		valores = append(valores, fmt.Sprintf("$%d", len(args)))
	}
	caucao, ok := e.CaucaoDeEventoCents.Definido()
	opcional("event_deposit_cents", caucao, ok)
	extensao, ok := e.ExtensaoDeHoldHoras.Definido()
	opcional("hold_extension_hours", extensao, ok)
	extensoes, ok := e.ExtensoesDeHoldMax.Definido()
	opcional("hold_max_extensions", extensoes, ok)
	validade, ok := e.ValidadeOrcamentoDia.Definido()
	opcional("quote_validity_days", validade, ok)

	// Os nomes de coluna são literais deste arquivo, nunca entrada do cliente.
	q := `INSERT INTO commercial_policies (` + strings.Join(colunas, ", ") + `)
	      VALUES (` + strings.Join(valores, ", ") + `)
	      RETURNING version`

	var versao int
	if err := r.exec(ctx).QueryRow(ctx, q, args...).Scan(&versao); err != nil {
		return 0, erroDePublicacao(err)
	}
	return versao, nil
}

func erroDePublicacao(err error) error {
	if db.IsUniqueViolation(err) {
		return apperr.PolicyImmutable.
			WithMessage("Outra publicação levou este número de versão; tente novamente.").
			WithCause(err)
	}
	return db.MapError(err)
}

// ═══════════════════════ Política de cancelamento ═══════════════════════

func (r *Repository) BuscarPoliticaDeCancelamentoVigente(ctx context.Context, prop uuid.UUID) (PoliticaDeCancelamento, error) {
	const q = `
		SELECT cp.id, cp.version, cp.name, cp.valid_from
		  FROM cancellation_policies cp
		  JOIN properties p ON p.id = cp.property_id
		 WHERE cp.property_id = $1 AND cp.valid_from <= ` + hoje + `
		 ORDER BY cp.version DESC
		 LIMIT 1`

	return r.umaPoliticaDeCancelamento(ctx, q, prop)
}

func (r *Repository) BuscarPoliticaDeCancelamentoPorVersao(ctx context.Context, prop uuid.UUID, versao int) (PoliticaDeCancelamento, error) {
	const q = `
		SELECT cp.id, cp.version, cp.name, cp.valid_from
		  FROM cancellation_policies cp
		 WHERE cp.property_id = $1 AND cp.version = $2`

	return r.umaPoliticaDeCancelamento(ctx, q, prop, versao)
}

func (r *Repository) UltimaPoliticaDeCancelamento(ctx context.Context, prop uuid.UUID) (PoliticaDeCancelamento, bool, error) {
	const q = `
		SELECT cp.id, cp.version, cp.name, cp.valid_from
		  FROM cancellation_policies cp
		 WHERE cp.property_id = $1
		 ORDER BY cp.version DESC
		 LIMIT 1`

	p, err := r.umaPoliticaDeCancelamento(ctx, q, prop)
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Code == apperr.CodeNotFound {
		return PoliticaDeCancelamento{}, false, nil
	}
	if err != nil {
		return PoliticaDeCancelamento{}, false, err
	}
	return p, true, nil
}

// Faixas devolve as faixas ordenadas por `sort_order` — da mais generosa à mais
// restritiva, que é a ordem em que `booking.CancellationPolicy.Simulate`
// procura a primeira aplicável.
func (r *Repository) Faixas(ctx context.Context, politica uuid.UUID) ([]FaixaDeCancelamento, error) {
	const q = `
		SELECT days_before_min, days_before_max, refund_pct::float8, label, sort_order
		  FROM cancellation_tiers
		 WHERE policy_id = $1
		 ORDER BY sort_order`

	linhas, err := r.exec(ctx).Query(ctx, q, politica)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []FaixaDeCancelamento{}
	for linhas.Next() {
		var f FaixaDeCancelamento
		if err := linhas.Scan(&f.DiasMin, &f.DiasMax, &f.DevolucaoPct, &f.Rotulo, &f.Ordem); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, f)
	}
	return out, db.MapError(linhas.Err())
}

// PublicarPoliticaDeCancelamento grava a versão nova COM as faixas, na mesma
// transação. Política sem faixa não é política: se o INSERT das faixas falhasse
// depois do da política, o motor cairia no "sem faixa aplicável" e passaria a
// reter o sinal inteiro de todo mundo, calado.
func (r *Repository) PublicarPoliticaDeCancelamento(ctx context.Context, prop uuid.UUID, e PoliticaDeCancelamentoEntrada) (uuid.UUID, int, error) {
	const qPolitica = `
		INSERT INTO cancellation_policies (property_id, version, name, valid_from)
		SELECT $1, COALESCE(max(cp.version), 0) + 1, $2, $3
		  FROM cancellation_policies cp
		 WHERE cp.property_id = $1
		RETURNING id, version`

	var (
		id     uuid.UUID
		versao int
	)
	if err := r.exec(ctx).QueryRow(ctx, qPolitica, prop, e.Nome, e.ValidoDe.Tempo()).Scan(&id, &versao); err != nil {
		if db.IsUniqueViolation(err) {
			return uuid.Nil, 0, apperr.PolicyImmutable.
				WithMessage("Outra publicação levou este número de versão; tente novamente.").
				WithCause(err)
		}
		return uuid.Nil, 0, db.MapError(err)
	}

	valores := make([]string, 0, len(e.Faixas))
	args := make([]any, 0, 1+len(e.Faixas)*5)
	args = append(args, id)
	for _, f := range e.Faixas {
		base := len(args)
		valores = append(valores, fmt.Sprintf("($1, $%d, $%d, $%d::float8::numeric, $%d, $%d)",
			base+1, base+2, base+3, base+4, base+5))
		args = append(args, f.DiasMin, f.DiasMax, *f.DevolucaoPct, f.Rotulo, *f.Ordem)
	}

	q := `INSERT INTO cancellation_tiers
			(policy_id, days_before_min, days_before_max, refund_pct, label, sort_order)
		  VALUES ` + strings.Join(valores, ", ")

	if _, err := r.exec(ctx).Exec(ctx, q, args...); err != nil {
		if db.IsUniqueViolation(err) {
			return uuid.Nil, 0, apperr.Validation(map[string]string{
				"tiers": "sort_order repetido — a chave natural é (policy_id, sort_order).",
			}).WithCause(err)
		}
		return uuid.Nil, 0, db.MapError(err)
	}
	return id, versao, nil
}
