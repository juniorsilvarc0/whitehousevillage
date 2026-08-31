package contatos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// ColunasDeOrdenacao é a whitelist do `?sort=`. O valor do cliente é só a CHAVE
// do mapa — nada do que ele digitou entra concatenado no ORDER BY.
var ColunasDeOrdenacao = map[string]string{
	"name":       "c.name",
	"created_at": "c.created_at",
}

// OrdenacaoPadrao — a agenda abre em ordem alfabética, que é como o atendimento
// procura.
const OrdenacaoPadrao = "c.name ASC"

// desempate fecha toda ordenação pelo id: sem critério determinístico, dois
// homônimos trocam de posição entre uma página e outra e um deles some da
// listagem sem nunca ter sido mostrado.
const desempate = ", c.id ASC"

// classeDaTravaDeDocumento é a classe do advisory lock que serializa a
// deduplicação por documento. Ver TravarDocumento.
const classeDaTravaDeDocumento int32 = 2026_0827

const camposContato = `
	c.id, c.name, c.email, c.phone_e164, c.doc_type, c.doc_number,
	to_char(c.birth_date, 'YYYY-MM-DD'), c.city, c.state, c.notes,
	c.lgpd_basis, c.marketing_opt_in, c.consent_at, c.anonymized_at,
	c.created_at, c.updated_at`

type Repository struct {
	pool db.DBTX
}

func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// ErroDeDuplicidade é a violação de unicidade já identificada por CAMPO.
//
// Não vira `409` aqui: para o contrato o erro precisa carregar o `id` do
// contato que já existe, e essa consulta não pode acontecer dentro da transação
// que o `23505` acabou de abortar. O service completa o erro depois do
// rollback. Ver Servico.duplicidade.
type ErroDeDuplicidade struct{ Campo string }

func (e *ErroDeDuplicidade) Error() string { return "contato duplicado em " + e.Campo }

// ─────────────────────────── Leitura ────────────────────────────────

// Listar devolve a página e o total na MESMA consulta (count(*) OVER()): duas
// consultas separadas podem discordar sob concorrência e fazer o total mentir.
//
// `phone` e `doc_number` são IGUALDADE, nunca prefixo: são as consultas de
// deduplicação (o inbound de WhatsApp e o balcão), e devolvem zero ou um. `q` é
// a busca humana, com trigram — serve para a tela, não para deduplicar.
func (r *Repository) Listar(ctx context.Context, f Filtro) ([]Contato, int64, error) {
	ordem := f.OrderBy
	if ordem == "" {
		ordem = OrdenacaoPadrao
	}

	q := `
		SELECT ` + camposContato + `, count(*) OVER() AS total
		  FROM contacts c
		 WHERE ($1::text IS NULL OR c.name ILIKE '%' || $1 || '%' OR c.email ILIKE '%' || $1 || '%')
		   AND ($2::text IS NULL OR c.phone_e164 = $2)
		   AND ($3::text IS NULL OR c.doc_number = $3)
		   AND ($4::boolean IS NULL OR c.marketing_opt_in = $4)
		   AND ($5::boolean OR c.anonymized_at IS NULL)
		 ORDER BY ` + ordem + desempate + `
		 LIMIT $6 OFFSET $7`

	linhas, err := r.exec(ctx).Query(ctx, q,
		nuloSeVazio(f.Busca), nuloSeVazio(f.Telefone), nuloSeVazio(f.Documento),
		f.OptIn, f.IncluirAnonimizados, f.PorPagina, httpx.Offset(f.Pagina, f.PorPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []Contato
		total int64
	)
	for linhas.Next() {
		var c Contato
		if err := linhas.Scan(alvosDoScan(&c, &total)...); err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, c)
	}
	if err := linhas.Err(); err != nil {
		return nil, 0, db.MapError(err)
	}
	return out, total, nil
}

// Buscar devolve a ficha. `NotFound` nomeia "Contato" para a mensagem não sair
// como "Registro não encontrado", que não diz a ninguém o que faltou.
func (r *Repository) Buscar(ctx context.Context, id uuid.UUID) (Contato, error) {
	var c Contato
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT `+camposContato+` FROM contacts c WHERE c.id = $1`, id).
		Scan(alvosDoScan(&c, nil)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, apperr.NotFound("Contato")
	}
	if err != nil {
		return c, db.MapError(err)
	}
	return c, nil
}

// BuscarIDPorTelefone devolve quem já tem este telefone. É chamado DEPOIS de um
// `23505`, para o `409` conseguir apontar o registro existente.
func (r *Repository) BuscarIDPorTelefone(ctx context.Context, e164 string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT id FROM contacts WHERE phone_e164 = $1`, e164).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, db.MapError(err)
	}
	return id, true, nil
}

// BuscarIDPorDocumento devolve quem já tem este documento na propriedade,
// ignorando o próprio contato (necessário no PUT/PATCH, que reenvia o documento
// que já é dele).
func (r *Repository) BuscarIDPorDocumento(ctx context.Context, propriedade uuid.UUID, tipo, numero string, exceto uuid.UUID) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT id FROM contacts
		 WHERE property_id = $1 AND doc_type = $2 AND doc_number = $3 AND id <> $4
		 LIMIT 1`, propriedade, tipo, numero, exceto).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, db.MapError(err)
	}
	return id, true, nil
}

// PropriedadeDoContato devolve a propriedade da ficha — necessária para a
// deduplicação por documento, que é por propriedade.
func (r *Repository) PropriedadeDoContato(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	var p uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `SELECT property_id FROM contacts WHERE id = $1`, id).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.NotFound("Contato")
	}
	if err != nil {
		return uuid.Nil, db.MapError(err)
	}
	return p, nil
}

// PropriedadePadrao é a propriedade em que o contato nasce quando não há
// usuário no contexto (seed, script). A instalação é de uma propriedade só.
func (r *Repository) PropriedadePadrao(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT id FROM properties WHERE active ORDER BY created_at LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.Validation(map[string]string{
			"property_id": "nenhuma propriedade cadastrada; rode o seed antes de criar contatos.",
		})
	}
	if err != nil {
		return uuid.Nil, db.MapError(err)
	}
	return id, nil
}

// Vinculos conta, por tipo, quem aponta para este contato.
//
// Uma consulta com subselects escalares, e não sete idas ao banco: as contagens
// precisam ser do MESMO instante para a decisão do `DELETE` fazer sentido, e
// sete consultas seriam sete instantes diferentes.
func (r *Repository) Vinculos(ctx context.Context, id uuid.UUID) (Vinculos, error) {
	const q = `
		SELECT (SELECT count(*) FROM reservations       WHERE contact_id = $1),
		       (SELECT count(*) FROM crm_leads          WHERE contact_id = $1),
		       (SELECT count(*) FROM crm_opportunities  WHERE contact_id = $1),
		       (SELECT count(*) FROM crm_activities     WHERE contact_id = $1),
		       (SELECT count(*) FROM crm_notes          WHERE contact_id = $1),
		       (SELECT count(*) FROM reservation_guests WHERE contact_id = $1),
		       (SELECT count(*) FROM quotes             WHERE contact_id = $1)`

	var v Vinculos
	if err := r.exec(ctx).QueryRow(ctx, q, id).Scan(
		&v.Reservas, &v.Leads, &v.Oportunidades, &v.Atividades,
		&v.Notas, &v.Hospedes, &v.Orcamentos); err != nil {
		return v, db.MapError(err)
	}
	return v, nil
}

// ReservasVivas devolve os códigos das reservas que impedem a anonimização.
//
// Inclui a estadia em que o contato é ACOMPANHANTE (`reservation_guests`), e
// não só aquela em que ele é o titular: quem está hospedado hoje está hospedado
// hoje, e a recepção precisa do nome para entregar a chave dos dois jeitos.
func (r *Repository) ReservasVivas(ctx context.Context, id uuid.UUID) ([]string, error) {
	const q = `
		SELECT DISTINCT res.code
		  FROM reservations res
		 WHERE res.status = ANY($2)
		   AND (res.contact_id = $1
		        OR EXISTS (SELECT 1 FROM reservation_guests g
		                    WHERE g.reservation_id = res.id AND g.contact_id = $1))
		 ORDER BY res.code`

	linhas, err := r.exec(ctx).Query(ctx, q, id, statusDeReservaViva)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	codigos := []string{}
	for linhas.Next() {
		var c string
		if err := linhas.Scan(&c); err != nil {
			return nil, db.MapError(err)
		}
		codigos = append(codigos, c)
	}
	return codigos, db.MapError(linhas.Err())
}

// ─────────────────────────── Travas ─────────────────────────────────

// TravarContato pega a linha em FOR UPDATE.
//
// Existe para fechar o TOCTOU de `DELETE` e de `/anonymize`: entre "contei zero
// vínculo" e "apaguei" cabe uma reserva sendo criada em outra transação. A
// inserção da reserva trava o contato em `FOR KEY SHARE` (é o que a FK faz), e
// `FOR UPDATE` é o modo que CONFLITA com `FOR KEY SHARE` — `FOR NO KEY UPDATE`
// não conflita, e por isso não serviria aqui. É a mesma trava, pelo mesmo
// motivo, que a migration 20260827140000 usa antes de contar reservas vivas.
func (r *Repository) TravarContato(ctx context.Context, id uuid.UUID) error {
	if !db.EmTransacao(ctx) {
		return apperr.Internal.WithCause(errors.New(
			"TravarContato chamado fora de transação: a trava seria solta no fim da consulta e não protegeria nada"))
	}
	var existe uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `SELECT id FROM contacts WHERE id = $1 FOR UPDATE`, id).Scan(&existe)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("Contato")
	}
	return db.MapError(err)
}

// TravarDocumento serializa a deduplicação por documento.
//
// # Por que existe uma trava aqui e não no telefone
//
// O telefone tem garantia de BANCO: `contacts_phone_idx` é
// `UNIQUE (phone_e164) WHERE phone_e164 IS NOT NULL`, e a regra 2 do CLAUDE.md
// se aplica — insere e deixa o `23505` decidir, sem `SELECT` antes.
//
// O documento NÃO tem: `contacts_doc_idx` é índice COMUM
// (20260820130000_inventario_reservas.up.sql:162). Sem índice único, a única
// forma de detectar a colisão é consultar antes de gravar — e isso é
// exatamente o TOCTOU que a regra 2 proíbe: dois pedidos simultâneos com o
// mesmo CPF consultam, os dois não acham nada, e os dois gravam.
//
// O advisory lock fecha a janela sem migration: dois pedidos com a MESMA chave
// (propriedade + tipo + número) serializam, então o segundo só consulta depois
// que o primeiro comitou e passa a enxergar a linha dele. A trava é de
// TRANSAÇÃO (`_xact_`): solta sozinha no commit e no rollback, sem risco de
// vazar para a próxima requisição que pegar a mesma conexão do pool.
//
// O que ela NÃO faz: proteger contra escrita que não passe por esta API
// (`psql`, importação, outro serviço). Só o índice único faz isso — ver "PARA O
// INTEGRADOR" no relatório.
func (r *Repository) TravarDocumento(ctx context.Context, propriedade uuid.UUID, tipo, numero string) error {
	if !db.EmTransacao(ctx) {
		return apperr.Internal.WithCause(errors.New(
			"TravarDocumento chamado fora de transação: a trava não valeria nada"))
	}
	chave := fmt.Sprintf("contatos:doc:%s:%s:%s", propriedade, tipo, numero)
	if _, err := r.exec(ctx).Exec(ctx,
		`SELECT pg_advisory_xact_lock($1, hashtext($2))`, classeDaTravaDeDocumento, chave); err != nil {
		return db.MapError(err)
	}
	return nil
}

// ─────────────────────────── Escrita ────────────────────────────────

// Criar insere e devolve a ficha completa.
//
// Sem `SELECT` antes para conferir o telefone: a unicidade é do índice, e a
// violação vira ErroDeDuplicidade aqui mesmo.
func (r *Repository) Criar(ctx context.Context, propriedade uuid.UUID, c Criar) (Contato, error) {
	const q = `
		INSERT INTO contacts (property_id, name, email, phone_e164, doc_type, doc_number,
		                      birth_date, city, state, notes, lgpd_basis,
		                      marketing_opt_in, consent_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8, $9, $10, $11, $12, $13)
		RETURNING ` + camposDoRetorno

	var out Contato
	err := r.exec(ctx).QueryRow(ctx, q,
		propriedade, c.Nome, c.Email, c.Telefone, c.TipoDeDocumento, c.Documento,
		c.Nascimento, c.Cidade, c.Estado, c.Notas, c.BaseLegal,
		c.OptInOuPadrao(), c.ConsentimentoEm).
		Scan(alvosDoScan(&out, nil)...)
	if err != nil {
		return Contato{}, r.traduzir(err)
	}
	return out, nil
}

// Substituir é o PUT: o que não veio no corpo volta a NULL. Substituição
// integral com campo ausente virando "não mexe" seria um PATCH com outro nome.
func (r *Repository) Substituir(ctx context.Context, id uuid.UUID, c Criar) (Contato, error) {
	const q = `
		UPDATE contacts SET
		       name = $2, email = $3, phone_e164 = $4, doc_type = $5, doc_number = $6,
		       birth_date = $7::date, city = $8, state = $9, notes = $10, lgpd_basis = $11,
		       marketing_opt_in = $12, consent_at = $13, updated_at = now()
		 WHERE id = $1
		RETURNING ` + camposDoRetorno

	var out Contato
	err := r.exec(ctx).QueryRow(ctx, q, id,
		c.Nome, c.Email, c.Telefone, c.TipoDeDocumento, c.Documento,
		c.Nascimento, c.Cidade, c.Estado, c.Notas, c.BaseLegal,
		c.OptInOuPadrao(), c.ConsentimentoEm).
		Scan(alvosDoScan(&out, nil)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Contato{}, apperr.NotFound("Contato")
	}
	if err != nil {
		return Contato{}, r.traduzir(err)
	}
	return out, nil
}

// Atualizar monta o SET só com o que veio. Os nomes de coluna são literais
// deste arquivo; do cliente vem apenas o VALOR, sempre por placeholder.
func (r *Repository) Atualizar(ctx context.Context, id uuid.UUID, a Atualizar) (Contato, error) {
	sets := []string{"updated_at = now()"}
	args := []any{id}

	adicionar := func(coluna, cast string, valor any) {
		args = append(args, valor)
		sets = append(sets, fmt.Sprintf("%s = $%d%s", coluna, len(args), cast))
	}
	texto := func(coluna, cast string, o httpx.Opt[string]) {
		switch {
		case o.DeveLimpar():
			adicionar(coluna, cast, nil)
		default:
			if v, ok := o.Definido(); ok {
				adicionar(coluna, cast, v)
			}
		}
	}

	texto("name", "", a.Nome)
	texto("email", "", a.Email)
	texto("phone_e164", "", a.Telefone)
	texto("doc_type", "", a.TipoDeDocumento)
	texto("doc_number", "", a.Documento)
	texto("birth_date", "::date", a.Nascimento)
	texto("city", "", a.Cidade)
	texto("state", "", a.Estado)
	texto("notes", "", a.Notas)
	texto("lgpd_basis", "", a.BaseLegal)

	if v, ok := a.OptInMarketing.Definido(); ok {
		adicionar("marketing_opt_in", "", v)
	}
	switch {
	case a.ConsentimentoEm.DeveLimpar():
		adicionar("consent_at", "", nil)
	default:
		if v, ok := a.ConsentimentoEm.Definido(); ok {
			adicionar("consent_at", "", v)
		}
	}

	q := `UPDATE contacts SET ` + strings.Join(sets, ", ") + ` WHERE id = $1 RETURNING ` + camposDoRetorno

	var out Contato
	err := r.exec(ctx).QueryRow(ctx, q, args...).Scan(alvosDoScan(&out, nil)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Contato{}, apperr.NotFound("Contato")
	}
	if err != nil {
		return Contato{}, r.traduzir(err)
	}
	return out, nil
}

// Excluir apaga de verdade. Só é chamado depois de a contagem de vínculos dar
// zero COM a linha travada — ver TravarContato.
func (r *Repository) Excluir(ctx context.Context, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx, `DELETE FROM contacts WHERE id = $1`, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Contato")
	}
	return nil
}

// Anonimizar esvazia a PII e carimba `anonymized_at`.
//
// `phone_e164` e `doc_number` viram NULL, e não hash, por dois motivos somados:
// hash de CPF é dado pseudonimizado e não anônimo (a base de CPFs é pequena o
// bastante para ser varrida inteira), e NULL é o que libera o índice único
// parcial para a pessoa se recadastrar amanhã — que é direito dela.
//
// O `WHERE anonymized_at IS NULL` é o que torna a operação idempotente DE FATO:
// a segunda chamada não encontra linha, devolve zero afetadas, e o service
// responde a ficha como está. Reescrever `anonymized_at` faria a segunda
// chamada mentir sobre quando a eliminação aconteceu.
func (r *Repository) Anonimizar(ctx context.Context, id uuid.UUID) (Contato, bool, error) {
	const q = `
		UPDATE contacts SET
		       name             = 'Contato anonimizado #' || left(replace(id::text, '-', ''), 8),
		       email            = NULL,
		       phone_e164       = NULL,
		       doc_type         = NULL,
		       doc_number       = NULL,
		       birth_date       = NULL,
		       city             = NULL,
		       state            = NULL,
		       notes            = NULL,
		       marketing_opt_in = false,
		       consent_at       = NULL,
		       anonymized_at    = now(),
		       updated_at       = now()
		 WHERE id = $1 AND anonymized_at IS NULL
		RETURNING ` + camposDoRetorno

	var out Contato
	err := r.exec(ctx).QueryRow(ctx, q, id).Scan(alvosDoScan(&out, nil)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Contato{}, false, nil // já estava anonimizado (ou não existe — quem chama já travou a linha)
	}
	if err != nil {
		return Contato{}, false, db.MapError(err)
	}
	return out, true, nil
}

// ─────────────────────────── Portabilidade ──────────────────────────

// Exportar devolve, em JSON já montado pelo Postgres, tudo que está vinculado
// ao contato.
//
// O pacote é do TITULAR: não há filtro por dono nem por status. Devolver ao
// titular só o que o corretor logado enxerga seria devolver menos do que existe
// sobre ele, que é o oposto da portabilidade.
//
// `crm_notes` fica de FORA: nota é anotação interna da operação sobre a
// negociação ("cliente reclamou do preço"), não dado fornecido pelo titular, e
// o schema `ExportacaoDeContato` do contrato não a lista. Continua contada em
// `references`, então ninguém apaga um contato achando que não há nota.
func (r *Repository) Exportar(ctx context.Context, id uuid.UUID) (reservas, leads, oportunidades, atividades json.RawMessage, err error) {
	const q = `
		SELECT
		  (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.check_in), '[]'::jsonb) FROM (
		      SELECT res.id, res.code, res.status, res.source,
		             res.check_in::text AS check_in, res.check_out::text AS check_out,
		             res.guests_count, res.is_event, res.event_type, res.created_at,
		             (SELECT to_jsonb(p) - 'reservation_id'
		                FROM reservation_pricing p WHERE p.reservation_id = res.id) AS pricing,
		             (SELECT coalesce(jsonb_agg(jsonb_build_object(
		                        'night', n.night::text, 'date_type', n.date_type,
		                        'price_cents', n.price_cents) ORDER BY n.night), '[]'::jsonb)
		                FROM reservation_nights n WHERE n.reservation_id = res.id) AS nights
		        FROM reservations res
		       WHERE res.contact_id = $1
		          OR EXISTS (SELECT 1 FROM reservation_guests g
		                      WHERE g.reservation_id = res.id AND g.contact_id = $1)
		  ) x),

		  (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.created_at), '[]'::jsonb) FROM (
		      SELECT l.id, l.source, l.status, l.score,
		             l.desired_check_in::text AS desired_check_in,
		             l.desired_check_out::text AS desired_check_out,
		             l.guests_count, l.notes, l.converted_at, l.created_at
		        FROM crm_leads l WHERE l.contact_id = $1
		  ) x),

		  (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.created_at), '[]'::jsonb) FROM (
		      SELECT o.id, o.title, o.status, o.amount_cents, o.probability,
		             o.check_in::text AS check_in, o.check_out::text AS check_out,
		             o.guests_count, o.expected_close::text AS expected_close,
		             o.closed_at, o.created_at
		        FROM crm_opportunities o WHERE o.contact_id = $1
		  ) x),

		  (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.created_at), '[]'::jsonb) FROM (
		      SELECT a.id, a.type, a.subject, a.description, a.due_at, a.done_at,
		             a.status, a.priority, a.created_at
		        FROM crm_activities a
		       WHERE a.contact_id = $1
		          OR a.lead_id        IN (SELECT id FROM crm_leads         WHERE contact_id = $1)
		          OR a.opportunity_id IN (SELECT id FROM crm_opportunities WHERE contact_id = $1)
		  ) x)`

	if err = r.exec(ctx).QueryRow(ctx, q, id).
		Scan(&reservas, &leads, &oportunidades, &atividades); err != nil {
		return nil, nil, nil, nil, db.MapError(err)
	}
	return reservas, leads, oportunidades, atividades, nil
}

// ─────────────────────────── Auxiliares ─────────────────────────────

// camposDoRetorno é a mesma lista de camposContato sem o alias `c.`, para as
// cláusulas RETURNING (que não enxergam alias de FROM).
const camposDoRetorno = `
	id, name, email, phone_e164, doc_type, doc_number,
	to_char(birth_date, 'YYYY-MM-DD'), city, state, notes,
	lgpd_basis, marketing_opt_in, consent_at, anonymized_at,
	created_at, updated_at`

// alvosDoScan mantém a ORDEM do Scan colada à ordem das colunas num lugar só.
// Duas listas de dezesseis campos em arquivos diferentes é como um campo entra
// numa e não na outra e a ficha passa a mostrar a cidade no lugar do estado.
func alvosDoScan(c *Contato, total *int64) []any {
	alvos := []any{
		&c.ID, &c.Nome, &c.Email, &c.Telefone, &c.TipoDeDocumento, &c.Documento,
		&c.Nascimento, &c.Cidade, &c.Estado, &c.Notas,
		&c.BaseLegal, &c.OptInMarketing, &c.ConsentimentoEm, &c.AnonimizadoEm,
		&c.CriadoEm, &c.AtualizadoEm,
	}
	if total != nil {
		alvos = append(alvos, total)
	}
	return alvos
}

// traduzir transforma a violação do índice de telefone no erro tipado que o
// service completa com o id do contato existente.
func (r *Repository) traduzir(err error) error {
	if db.IsUniqueViolation(err, "contacts_phone_idx") {
		return &ErroDeDuplicidade{Campo: "phone_e164"}
	}
	// Índice de documento AINDA NÃO É ÚNICO. No dia em que a migration o tornar
	// `UNIQUE (property_id, doc_type, doc_number) WHERE doc_number IS NOT NULL`,
	// esta linha começa a disparar sozinha e a trava de TravarDocumento vira
	// redundância barata — nesta ordem, sem janela sem proteção.
	if db.IsUniqueViolation(err, "contacts_doc_idx", "contacts_doc_unico_idx") {
		return &ErroDeDuplicidade{Campo: "doc_number"}
	}
	return db.MapError(err)
}
