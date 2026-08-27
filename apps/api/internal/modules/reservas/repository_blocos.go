package reservas

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// stay_blocks — TODA ocupação do calendário passa por aqui, e a exclusividade é
// garantida pela constraint `stay_no_overlap`, não por código.
//
// A regra que este arquivo existe para respeitar: NUNCA `SELECT` de
// disponibilidade seguido de `INSERT` contando que a data continue livre. Insere
// e deixa o banco decidir. O `23P01` que volta é a resposta, não um imprevisto.

// colunasDoBloco é a projeção do schema `Bloqueio` do contrato.
const colunasDoBloco = `
	sb.id, sb.unit_id, u.code, sb.source, sb.status,
	lower(sb.period)::text, upper(sb.period)::text,
	sb.reservation_id, sb.expires_at, sb.note, sb.created_at`

func lerBloco(linha pgx.Row) (Bloqueio, error) {
	var b Bloqueio
	err := linha.Scan(&b.ID, &b.UnitID, &b.UnitCode, &b.Origem, &b.Status,
		&b.De, &b.Ate, &b.ReservaID, &b.ExpiraEm, &b.Observacao, &b.CriadoEm)
	return b, err
}

// BlocoDeReserva descreve o que a reserva quer segurar no calendário.
type BlocoDeReserva struct {
	PropertyID uuid.UUID
	ReservaID  uuid.UUID
	Status     string
	CheckIn    string
	CheckOut   string
	ExpiraEm   *time.Time
	// CriadoPor é AUDITORIA: quem digitou. Imutável.
	CriadoPor *uuid.UUID
	// DonoID é COMÉRCIO: de quem é a linha, e é o que o escopo `own` filtra.
	// São dois campos porque são duas perguntas — a revisão mediu a diferença:
	// o corretor abre a venda, o gestor aperta "confirmar", e um calendário
	// filtrado por `created_by` esconde do corretor a própria venda dele.
	DonoID *uuid.UUID
}

// AlocarUmaUnidade tenta segurar a data na primeira unidade da ordem de
// preferência que o banco aceitar, e devolve a que venceu.
//
// COMO O TOCTOU É EVITADO: a ordem que chega aqui é preferência (menor
// fragmentação), não uma promessa de disponibilidade. Cada tentativa é um
// INSERT de verdade dentro de um SAVEPOINT; a constraint é que decide. Quando
// ela recusa com 23P01, o savepoint desfaz APENAS aquela tentativa — sem ele o
// primeiro conflito abortaria a transação inteira e a segunda unidade livre
// nunca seria tentada, transformando "o AP-01 está ocupado" em "não há
// apartamento".
//
// Esgotadas as candidatas, o erro é 409 DATE_CONFLICT com os códigos das
// unidades ocupadas — o que a tela precisa dizer ao hóspede.
func (r *Repository) AlocarUmaUnidade(ctx context.Context, ordem []Candidata, b BlocoDeReserva, codigoDoProduto string, declaradas int) (uuid.UUID, uuid.UUID, error) {
	if !db.EmTransacao(ctx) {
		// Sem transação não há savepoint, e sem savepoint a primeira recusa
		// levaria a conexão inteira junto. Falhar alto é melhor que degradar.
		return uuid.Nil, uuid.Nil, apperr.Internal.WithCause(errors.New("AlocarUmaUnidade exige transação"))
	}
	if len(ordem) == 0 {
		// Produto sem NENHUMA unidade ativa é o mesmo defeito do CRÍTICO 1 na
		// sua forma extrema, e merece o mesmo código: não é erro de campo, é
		// inventário mal configurado, e nenhuma outra data resolve.
		return uuid.Nil, uuid.Nil, composicaoIncompleta(codigoDoProduto, declaradas, 0, nil)
	}

	exec := r.exec(ctx)
	var ultimo error
	for _, c := range ordem {
		if _, err := exec.Exec(ctx, `SAVEPOINT alocacao`); err != nil {
			return uuid.Nil, uuid.Nil, db.MapError(err)
		}

		bloco, err := r.inserirBloco(ctx, c.ID, b)
		if err == nil {
			if _, err := exec.Exec(ctx, `RELEASE SAVEPOINT alocacao`); err != nil {
				return uuid.Nil, uuid.Nil, db.MapError(err)
			}
			return c.ID, bloco, nil
		}
		if !ehSobreposicao(err) {
			return uuid.Nil, uuid.Nil, db.MapError(err)
		}

		ultimo = err
		// Obrigatório: depois de um erro, nenhuma outra instrução roda na
		// transação até voltar ao savepoint.
		if _, err := exec.Exec(ctx, `ROLLBACK TO SAVEPOINT alocacao`); err != nil {
			return uuid.Nil, uuid.Nil, db.MapError(err)
		}
	}

	return uuid.Nil, uuid.Nil, conflitoDeData(ultimo,
		r.codigosOcupados(ctx, IDsEmOrdemDeCodigo(ordem), b.CheckIn, b.CheckOut),
		periodo(b.CheckIn, b.CheckOut))
}

// AlocarComposicaoCompleta é a White House Completa: ou a casa inteira entra, ou
// a venda não sai.
//
// ─────────────── O CRÍTICO QUE ESTA FUNÇÃO EXISTE PARA FECHAR ───────────────
//
// A exclusividade da Completa NÃO era invariante de nada: ela era consequência
// de uma consulta devolver oito linhas. A versão anterior recebia as candidatas
// de `Candidatas`, que filtra `u.active` — e `PATCH /units/{AP-03}
// {"active": false}`, que é manutenção rotineira, fazia a lista voltar com
// SETE. Medido ao vivo nesta árvore, contra Postgres real:
//
//	POST /reservations da Completa → 201, `units` com 7 elementos,
//	total_cents = 2 020 000 — o mesmo preço das oito.
//
// A constraint `stay_no_overlap` não pega e não tinha como: a oitava linha
// NUNCA FOI INSERIDA, e o que não existe não colide. Com AP-03 de volta ao ar,
// um Apartamento 2 Suítes era vendido nas mesmas datas e o banco aceitava,
// corretamente — aquela unidade estava livre. O desfecho não é "entregamos 7 de
// 8": é estranho dormindo dentro da casa que alguém alugou inteira.
//
// ─────────────── POR QUE UMA INSTRUÇÃO SÓ, E NÃO UM `if` ANTES ───────────────
//
// Conferir a composição em Go e inserir depois seria o mesmo TOCTOU que a regra
// 2 do CLAUDE.md proíbe, um nível acima: entre o SELECT que conta as unidades
// ativas e o INSERT que as segura, o `READ COMMITTED` deixa passar um
// `UPDATE units SET active = false` de outra transação — e a venda incompleta
// nasce de novo, agora numa janela mais estreita e mais difícil de reproduzir.
//
// Aqui a conferência e a inserção são LITERALMENTE a mesma instrução. As CTEs
// de um comando compartilham um snapshot: `membros` é a composição DECLARADA
// (`unit_type_members`, sem filtro de `active`), e o INSERT só produz linha
// quando nenhum membro está inativo. Se algum estiver, zero linhas são
// inseridas e o SELECT final devolve QUAIS — que é o que o operador precisa
// saber para reativar. Não existe instante entre uma coisa e outra.
//
// Não há savepoint por unidade, também de propósito: bloqueio parcial não
// existe. Uma unidade ocupada fecha o produto inteiro, que é exatamente a
// exclusividade que a Completa vende.
func (r *Repository) AlocarComposicaoCompleta(ctx context.Context, produto Produto, b BlocoDeReserva) ([]uuid.UUID, error) {
	if !db.EmTransacao(ctx) {
		return nil, apperr.Internal.WithCause(errors.New("AlocarComposicaoCompleta exige transação"))
	}

	exec := r.exec(ctx)
	if _, err := exec.Exec(ctx, `SAVEPOINT composicao`); err != nil {
		return nil, db.MapError(err)
	}

	// `ORDER BY u.code` na fonte do INSERT não é estilo: é a ordem de aquisição
	// dos locks. Duas transações inserindo subconjuntos das mesmas unidades em
	// ordens opostas se travam mutuamente, e o Postgres só desfaz o nó depois de
	// `deadlock_timeout` (docs/db.md §14).
	const q = `
		WITH membros AS (
		    SELECT u.id, u.code, u.active
		      FROM unit_type_members m
		      JOIN units u ON u.id = m.unit_id AND u.property_id = $1
		     WHERE m.unit_type_id = $2
		),
		inseridos AS (
		    INSERT INTO stay_blocks (property_id, unit_id, reservation_id, source, status,
		                             period, expires_at, created_by, owner_id)
		    SELECT $1, mb.id, $3, 'reservation', $4,
		           daterange($5::date, $6::date, '[)'), $7, $8, $9
		      FROM membros mb
		     WHERE NOT EXISTS (SELECT 1 FROM membros WHERE NOT active)
		       AND EXISTS     (SELECT 1 FROM membros)
		     ORDER BY mb.code
		    RETURNING id
		)
		SELECT (SELECT count(*) FROM membros),
		       (SELECT count(*) FROM membros WHERE active),
		       (SELECT array_agg(code ORDER BY code) FROM membros WHERE NOT active),
		       (SELECT array_agg(id) FROM inseridos)`

	var (
		declaradas, ativas int
		inativas           []string
		blocos             []uuid.UUID
	)
	err := exec.QueryRow(ctx, q, b.PropertyID, produto.ID, b.ReservaID, b.Status,
		b.CheckIn, b.CheckOut, b.ExpiraEm, b.CriadoPor, b.DonoID).
		Scan(&declaradas, &ativas, &inativas, &blocos)

	if err == nil {
		if len(blocos) == declaradas && declaradas > 0 {
			if _, err := exec.Exec(ctx, `RELEASE SAVEPOINT composicao`); err != nil {
				return nil, db.MapError(err)
			}
			return blocos, nil
		}
		// Zero linhas inseridas: a guarda do WHERE recusou. A transação NÃO está
		// condenada (nenhum erro do banco aconteceu), então nem precisa de
		// rollback — mas o savepoint é liberado para não ficar pendurado.
		if _, err := exec.Exec(ctx, `RELEASE SAVEPOINT composicao`); err != nil {
			return nil, db.MapError(err)
		}
		return nil, composicaoIncompleta(produto.Codigo, declaradas, ativas, inativas)
	}

	if !ehSobreposicao(err) {
		return nil, db.MapError(err)
	}
	// Volta ao savepoint só para poder CONTAR ao cliente qual unidade derrubou a
	// venda. A transação de fora continua condenada — quem chama devolve erro.
	if _, errRB := exec.Exec(ctx, `ROLLBACK TO SAVEPOINT composicao`); errRB != nil {
		return nil, db.MapError(err)
	}
	return nil, conflitoDeData(err,
		r.codigosOcupadosDoProduto(ctx, produto.ID, b.CheckIn, b.CheckOut),
		periodo(b.CheckIn, b.CheckOut))
}

// inserirBloco grava UMA linha de stay_blocks e devolve o erro CRU — quem chama
// precisa distinguir 23P01 de falha real antes de traduzir.
func (r *Repository) inserirBloco(ctx context.Context, unidade uuid.UUID, b BlocoDeReserva) (uuid.UUID, error) {
	const q = `
		INSERT INTO stay_blocks (property_id, unit_id, reservation_id, source, status,
		                         period, expires_at, created_by, owner_id)
		VALUES ($1, $2, $3, 'reservation', $4, daterange($5::date, $6::date, '[)'), $7, $8, $9)
		RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, b.PropertyID, unidade, b.ReservaID, b.Status,
		b.CheckIn, b.CheckOut, b.ExpiraEm, b.CriadoPor, b.DonoID).Scan(&id)
	return id, err
}

// VincularUnidades liga a reserva às unidades pelos blocos recém-criados.
func (r *Repository) VincularUnidades(ctx context.Context, reserva uuid.UUID, blocos []uuid.UUID, travada bool) error {
	if len(blocos) == 0 {
		return nil
	}

	const q = `
		INSERT INTO reservation_units (reservation_id, unit_id, stay_block_id, locked)
		SELECT $1, sb.unit_id, sb.id, $2
		  FROM stay_blocks sb
		 WHERE sb.id = ANY($3::uuid[])
		 ORDER BY sb.unit_id
		ON CONFLICT (reservation_id, unit_id)
		DO UPDATE SET stay_block_id = EXCLUDED.stay_block_id, locked = EXCLUDED.locked`

	_, err := r.exec(ctx).Exec(ctx, q, reserva, travada, blocos)
	return db.MapError(err)
}

// codigosOcupados devolve, em texto, quais unidades da lista estão ocupadas no
// período — só para compor `details.unit_code` do 409. Best-effort: se a
// consulta falhar, o erro do conflito continua valendo sem o detalhe.
func (r *Repository) codigosOcupados(ctx context.Context, ids []uuid.UUID, checkIn, checkOut string) string {
	const q = `
		SELECT string_agg(DISTINCT u.code, ', ' ORDER BY u.code)
		  FROM stay_blocks sb
		  JOIN units u ON u.id = sb.unit_id
		 WHERE sb.unit_id = ANY($1::uuid[])
		   AND sb.status = ANY($4::text[])
		   AND sb.period && daterange($2::date, $3::date, '[)')`

	var codigos *string
	if err := r.exec(ctx).QueryRow(ctx, q, ids, checkIn, checkOut, statusQueBloqueiam).Scan(&codigos); err != nil || codigos == nil {
		return ""
	}
	return *codigos
}

// codigosOcupadosDoProduto é a mesma explicação do 409, mas partindo do PRODUTO
// e não de uma lista de ids — a composição completa não monta lista em Go.
func (r *Repository) codigosOcupadosDoProduto(ctx context.Context, produto uuid.UUID, checkIn, checkOut string) string {
	const q = `
		SELECT string_agg(DISTINCT u.code, ', ' ORDER BY u.code)
		  FROM unit_type_members m
		  JOIN units u        ON u.id = m.unit_id
		  JOIN stay_blocks sb ON sb.unit_id = u.id
		 WHERE m.unit_type_id = $1
		   AND sb.status = ANY($4::text[])
		   AND sb.period && daterange($2::date, $3::date, '[)')`

	var codigos *string
	if err := r.exec(ctx).QueryRow(ctx, q, produto, checkIn, checkOut, StatusQueBloqueiam).Scan(&codigos); err != nil || codigos == nil {
		return ""
	}
	return *codigos
}

func periodo(de, ate string) string { return fmt.Sprintf("[%s, %s)", de, ate) }

// ─────────────────────────── Transições dos blocos ──────────────────

// MoverBlocosDaReserva muda o status das linhas que a reserva segura.
//
// É esta chamada que LIBERA a data: `cancelled`, `expired` e `completed` estão
// os três fora do `WHERE` da constraint, então a unidade volta ao estoque
// vendável sem que a linha (e a prova de quem segurou a data) desapareça. QUAL
// dos três é decisão de quem chama, e não é intercambiável: `completed` diz que
// a estadia aconteceu e continua no mapa e na ocupação; `cancelled` diz que a
// venda não aconteceu e some dos dois.
//
// `expires_at` é zerado ao sair de `hold` porque a constraint
// `stay_hold_expires` só exige prazo enquanto o status for `hold` — e um prazo
// pendurado num bloco confirmado confundiria o job de expiração.
func (r *Repository) MoverBlocosDaReserva(ctx context.Context, reserva uuid.UUID, de []string, para string) (int64, error) {
	const q = `
		UPDATE stay_blocks
		   SET status = $3,
		       expires_at = CASE WHEN $3 = 'hold' THEN expires_at ELSE NULL END
		 WHERE reservation_id = $1 AND status = ANY($2::text[])`

	tag, err := r.exec(ctx).Exec(ctx, q, reserva, de, para)
	if err != nil {
		return 0, db.MapError(err)
	}
	return tag.RowsAffected(), nil
}

// ─────────────────────────── Realocação de unidade ──────────────────

// Realocar troca a unidade física de uma reserva sem tocar em datas nem preço.
//
// Numa transação: fecha o bloco da unidade antiga e insere o da nova no MESMO
// período. Se a nova estiver ocupada, a constraint recusa e o savepoint desfaz
// tudo — a reserva continua exatamente onde estava. Esse caso é
// UNIT_NOT_AVAILABLE, e não DATE_CONFLICT: a estadia não está em disputa, só a
// unidade.
func (r *Repository) Realocar(ctx context.Context, e Estado, de, para uuid.UUID, codigoDestino string, travar bool, autor *uuid.UUID) error {
	if !db.EmTransacao(ctx) {
		return apperr.Internal.WithCause(errors.New("Realocar exige transação"))
	}
	exec := r.exec(ctx)

	if _, err := exec.Exec(ctx, `SAVEPOINT realocacao`); err != nil {
		return db.MapError(err)
	}

	err := r.trocarUnidade(ctx, e, de, para, travar, autor)
	if err == nil {
		if _, err := exec.Exec(ctx, `RELEASE SAVEPOINT realocacao`); err != nil {
			return db.MapError(err)
		}
		return nil
	}
	if !ehSobreposicao(err) {
		return db.MapError(err)
	}
	// A troca não vingou, mas a RESERVA continua válida: o savepoint devolve a
	// transação ao estado anterior, e ela segue na unidade antiga.
	if _, errRB := exec.Exec(ctx, `ROLLBACK TO SAVEPOINT realocacao`); errRB != nil {
		return db.MapError(err)
	}
	return UnidadeIndisponivel.WithDetails(map[string]any{
		"unit_code": codigoDestino,
		"period":    periodo(e.CheckIn, e.CheckOut),
	}).WithCause(err)
}

func (r *Repository) trocarUnidade(ctx context.Context, e Estado, de, para uuid.UUID, travar bool, autor *uuid.UUID) error {
	exec := r.exec(ctx)

	// A ordem importa: primeiro solta a unidade antiga. Trocar AP-02 por AP-01
	// numa reserva que já ocupa AP-02 é caso real; inserindo antes de soltar, a
	// própria reserva colidiria consigo mesma quando a unidade fosse a mesma.
	const qSoltar = `
		UPDATE stay_blocks SET status = 'cancelled', expires_at = NULL
		 WHERE reservation_id = $1 AND unit_id = $2 AND status = ANY($3::text[])`
	if _, err := exec.Exec(ctx, qSoltar, e.ID, de, statusQueBloqueiam); err != nil {
		return err
	}

	const qDesvincular = `DELETE FROM reservation_units WHERE reservation_id = $1 AND unit_id = $2`
	if _, err := exec.Exec(ctx, qDesvincular, e.ID, de); err != nil {
		return err
	}

	bloco, err := r.inserirBloco(ctx, para, BlocoDeReserva{
		PropertyID: e.PropertyID,
		ReservaID:  e.ID,
		Status:     StatusDoBlocoPara(e.Status),
		CheckIn:    e.CheckIn,
		CheckOut:   e.CheckOut,
		ExpiraEm:   e.HoldExpiraEm,
		CriadoPor:  autor,
		// O bloco novo herda o dono DA VENDA, não de quem apertou o botão de
		// realocar — trocar de apartamento não transfere a carteira.
		DonoID: e.OwnerID,
	})
	if err != nil {
		return err
	}

	const qVincular = `
		INSERT INTO reservation_units (reservation_id, unit_id, stay_block_id, locked)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (reservation_id, unit_id)
		DO UPDATE SET stay_block_id = EXCLUDED.stay_block_id, locked = EXCLUDED.locked`
	if _, err := exec.Exec(ctx, qVincular, e.ID, para, bloco, travar); err != nil {
		return err
	}
	return nil
}

// unidadeDaComposicao é o par (id, código) de uma unidade física — o mínimo
// para resolver de onde a reserva sai e para onde vai.
type unidadeDaComposicao struct {
	ID     uuid.UUID
	Codigo string
}

// ConferirUnidadeDoProduto recusa destino que não compõe o produto da reserva.
// É 422, e não 404: a unidade existe, ela é que não pertence a este produto.
func (r *Repository) ConferirUnidadeDoProduto(ctx context.Context, produto, unidade uuid.UUID) (string, error) {
	const q = `
		SELECT u.code
		  FROM unit_type_members m
		  JOIN units u ON u.id = m.unit_id AND u.active
		 WHERE m.unit_type_id = $1 AND m.unit_id = $2`

	var codigo string
	err := r.exec(ctx).QueryRow(ctx, q, produto, unidade).Scan(&codigo)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperr.Validation(map[string]string{
			"to_unit_id": "esta unidade não compõe o produto da reserva.",
		})
	}
	return codigo, db.MapError(err)
}

// UnidadesAtuais devolve as unidades que a reserva ocupa hoje.
func (r *Repository) UnidadesAtuais(ctx context.Context, reserva uuid.UUID) ([]unidadeDaComposicao, error) {
	const q = `
		SELECT u.id, u.code
		  FROM reservation_units ru JOIN units u ON u.id = ru.unit_id
		 WHERE ru.reservation_id = $1 ORDER BY u.code`

	linhas, err := r.exec(ctx).Query(ctx, q, reserva)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	var out []unidadeDaComposicao
	for linhas.Next() {
		var u unidadeDaComposicao
		if err := linhas.Scan(&u.ID, &u.Codigo); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, u)
	}
	return out, db.MapError(linhas.Err())
}

// ─────────────────────────── Bloqueio operacional ───────────────────

// CriarBloqueioOperacional insere uma linha por unidade, todas na mesma
// transação e em ordem de `units.code`.
//
// Tudo-ou-nada: qualquer unidade ocupada derruba a transação inteira. Bloqueio
// parcial não existe — a equipe de manutenção não pode achar que reservou a casa
// e receber metade dela.
//
// O status é `confirmed`, e não `hold`: bloqueio operacional não tem prazo de
// validade nem sinal a receber, e um `hold` sem `expires_at` violaria a
// constraint `stay_hold_expires`.
func (r *Repository) CriarBloqueioOperacional(ctx context.Context, propriedade uuid.UUID, b BloqueioCriar, autor *uuid.UUID) ([]Bloqueio, error) {
	// `owner_id` = quem criou. Bloqueio operacional não tem carteira nem
	// corretor: quem pediu a manutenção é o dono da linha. É o que faz o escopo
	// `own` de `calendar` ter um eixo para filtrar — sem coluna de dono, `own`
	// degrada silenciosamente para `all`.
	q := `
		WITH inseridos AS (
		    INSERT INTO stay_blocks (property_id, unit_id, source, status, period, note, created_by, owner_id)
		    SELECT $1, u.id, $2, 'confirmed', daterange($3::date, $4::date, '[)'), $5, $6, $6
		      FROM units u
		     WHERE u.id = ANY($7::uuid[]) AND u.active AND u.property_id = $1
		     ORDER BY u.code
		    RETURNING id, unit_id, source, status, period, reservation_id, expires_at, note, created_at
		)
		SELECT ` + colunasDoBloco + `
		  FROM inseridos sb JOIN units u ON u.id = sb.unit_id
		 ORDER BY u.code`

	linhas, err := r.exec(ctx).Query(ctx, q, propriedade, b.Origem, b.De, b.Ate, b.Observacao, autor, b.Unidades)
	if err != nil {
		if ehSobreposicao(err) {
			return nil, conflitoDeData(err, r.codigosOcupados(ctx, b.Unidades, b.De, b.Ate), periodo(b.De, b.Ate))
		}
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	var out []Bloqueio
	for linhas.Next() {
		bloco, err := lerBloco(linhas)
		if err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, bloco)
	}
	if err := linhas.Err(); err != nil {
		if ehSobreposicao(err) {
			return nil, conflitoDeData(err, "", periodo(b.De, b.Ate))
		}
		return nil, db.MapError(err)
	}

	// Unidade inexistente, inativa ou de outra propriedade some no `WHERE` do
	// SELECT — e sumir em silêncio faria a operação achar que bloqueou oito
	// unidades tendo bloqueado seis.
	if len(out) != len(b.Unidades) {
		return nil, apperr.Validation(map[string]string{
			"unit_ids": "há unidade inexistente, inativa ou de outra propriedade na lista.",
		})
	}
	return out, nil
}

// LiberarBloqueio marca o bloqueio operacional como `cancelled`: a data volta a
// ser vendável e o registro fica. Devolve o bloqueio COMO ELE ESTAVA, que é o
// `before` da trilha de auditoria — depois do UPDATE essa cópia não existe mais.
//
// Bloqueio de RESERVA não se libera por aqui. Apagar o bloco por fora deixaria
// uma reserva `confirmed` sem calendário, e ninguém veria até a data ser vendida
// duas vezes.
//
// `somenteMinhas` é o escopo `own` do RBAC aplicado NO SQL, e não é enfeite: a
// matriz passou a conceder ao corretor `calendar` nas quatro ações (a revisão
// mediu a assimetria — ele criava e não conseguia excluir, cadeado sem chave).
// Sem o `AND owner_id = $usuario` aqui, `own` degradaria para `all` e ele
// passaria a apagar bloqueio alheio, que é pior do que a assimetria original.
//
// Dono errado devolve 404, e não 403, pela mesma razão que a reserva alheia
// devolve 404: 403 confirmaria que o bloqueio existe.
func (r *Repository) LiberarBloqueio(ctx context.Context, propriedade, id uuid.UUID, somenteMeus bool, usuario uuid.UUID) (Bloqueio, error) {
	qLer := `
		SELECT ` + colunasDoBloco + `
		  FROM stay_blocks sb JOIN units u ON u.id = sb.unit_id
		 WHERE sb.id = $1 AND sb.property_id = $2
		   AND ($3::boolean = false OR sb.owner_id = $4::uuid)
		   FOR UPDATE OF sb`

	antes, err := lerBloco(r.exec(ctx).QueryRow(ctx, qLer, id, propriedade, somenteMeus, usuario))
	if errors.Is(err, pgx.ErrNoRows) {
		return Bloqueio{}, apperr.NotFound("Bloqueio")
	}
	if err != nil {
		return Bloqueio{}, db.MapError(err)
	}
	if antes.ReservaID != nil {
		return antes, EstadoInvalido.WithDetails(map[string]any{
			"reservation_id": antes.ReservaID.String(),
			"hint":           "bloqueio de reserva se solta por /cancel, /check-out ou pelo job de expiração.",
		})
	}
	// Bloco terminal não se libera duas vezes. `completed` em especial: liberá-lo
	// seria dizer que a estadia não aconteceu.
	if BlocoTerminal(antes.Status) {
		return antes, EstadoInvalido.
			WithMessage("Este bloqueio já está encerrado.").
			WithDetails(map[string]any{
				"status":  antes.Status,
				"allowed": []string{BlocoHold, BlocoConfirmado},
			})
	}

	const qLiberar = `UPDATE stay_blocks SET status = 'cancelled', expires_at = NULL WHERE id = $1`
	if _, err := r.exec(ctx).Exec(ctx, qLiberar, id); err != nil {
		return antes, db.MapError(err)
	}
	return antes, nil
}

// ─────────────────────────── Expiração ──────────────────────────────

// Expirada é o que o job precisa saber sobre cada reserva que venceu.
type Expirada struct {
	ID      uuid.UUID
	Codigo  string
	Blocos  int64
	Unidade string
}

// ExpirarLote leva ao estado `expired` até `limite` pré-reservas vencidas e
// libera TODAS as unidades de cada uma na MESMA transação.
//
// `FOR UPDATE SKIP LOCKED` é o que permite duas réplicas do worker rodarem sem
// disputar as mesmas linhas — e o que impede o job de ficar preso atrás de uma
// confirmação em curso: a reserva que está sendo confirmada agora simplesmente
// não entra neste lote, e no minuto seguinte já não estará vencida.
//
// A Completa expira as oito linhas juntas porque o UPDATE dos blocos e o da
// reserva estão na mesma transação. Meio-livre não existe.
func (r *Repository) ExpirarLote(ctx context.Context, limite int) ([]Expirada, error) {
	const qReservas = `
		WITH vencidas AS (
		    SELECT id FROM reservations
		     WHERE status = 'hold' AND hold_expires_at IS NOT NULL AND hold_expires_at < now()
		     ORDER BY hold_expires_at
		     FOR UPDATE SKIP LOCKED
		     LIMIT $1
		)
		UPDATE reservations r
		   SET status = 'expired', hold_expires_at = NULL, updated_at = now()
		  FROM vencidas v
		 WHERE r.id = v.id
		RETURNING r.id, r.code`

	linhas, err := r.exec(ctx).Query(ctx, qReservas, limite)
	if err != nil {
		return nil, db.MapError(err)
	}

	var out []Expirada
	for linhas.Next() {
		var e Expirada
		if err := linhas.Scan(&e.ID, &e.Codigo); err != nil {
			linhas.Close()
			return nil, db.MapError(err)
		}
		out = append(out, e)
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		return nil, db.MapError(err)
	}
	if len(out) == 0 {
		return nil, nil
	}

	for i := range out {
		blocos, err := r.MoverBlocosDaReserva(ctx, out[i].ID, []string{BlocoHold}, BlocoExpirado)
		if err != nil {
			return nil, err
		}
		out[i].Blocos = blocos

		// Autor nulo: quem expirou foi o relógio, não uma pessoa. Gravar o
		// usuário de serviço aqui faria a auditoria mentir sobre quem decidiu.
		if err := r.InserirEvento(ctx, out[i].ID, EventoExpirada, map[string]any{
			"released_blocks": blocos,
			"by":              "job",
		}, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}
