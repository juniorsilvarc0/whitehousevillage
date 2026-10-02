package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
)

// O cadastro comercial do corretor de desenvolvimento (F2-09).
//
// Sem ele, a conta `corretor@wh.local` entra no painel com `broker_id` nulo, e a
// partir do F2-13 isso tem efeito: em escopo `own` o corretor só grava venda no
// PRÓPRIO `users.broker_id`, e nulo quer dizer que ele não consegue atribuir
// venda nenhuma a si — a jornada do corretor (PRD 5.3) para no primeiro passo
// num banco recém-semeado. E a API não tem como criar o vínculo: o CRUD de
// `/brokers` é do F2-17/Fase 3, e até lá o contrato diz que o vínculo "só nasce
// fora da API (seed)".
//
// São três escritas por corretor, nesta ordem — e a ordem é a do banco, não
// preferência: o cadastro exige o contato (FK `brokers.contact_id`), e a conta
// só aponta para um cadastro que já aponta de volta para ela (FK composta
// `users_broker_id_fkey`, migration 20261002180000).
//
//  1. a pessoa, em `contacts` (chave natural: telefone);
//  2. o cadastro, em `brokers` (chave natural: a conta, `UNIQUE (user_id)`);
//  3. o vínculo, em `users.broker_id`.
type corretorSeed struct {
	email   string // a conta, que tem de estar em usuariosSeed com perfil corretor
	contato contatoDemo
}

// Mesmas regras de dado fictício dos contatos de demonstração (contatos.go):
// telefone na faixa `9 0000 xxxx`, que não é atribuível a celular no Brasil, e
// e-mail em `.invalid` (RFC 2606). O telefone NÃO pode repetir o de um contato
// de demonstração: as duas etapas gravam a mesma ficha por chave natural, e
// cada uma devolveria a ficha ao próprio estado — a linha voltaria como
// "atualizada" em toda execução, para sempre. O teste de unidade confere.
//
// Base legal `contrato`: a ficha do corretor existe pela parceria comercial
// (LGPD art. 7, V), que legitima guardar o cadastro e pagar a comissão — e não
// legitima mandar oferta, por isso `optIn` false.
var corretoresSeed = []corretorSeed{
	{
		email: "corretor@wh.local",
		contato: contatoDemo{
			nome:     "Corretor Demo",
			email:    "corretor.demo@whitehousevillage.invalid",
			telefone: "+5585900000010",
			cidade:   "Fortaleza",
			uf:       "CE",
			base:     "contrato",
			optIn:    false,
			nota:     "Demonstração: cadastro comercial da conta de desenvolvimento de perfil corretor. Não é pessoa real.",
		},
	},
}

// escritasPorCorretor é o que a etapa quer ver no banco para cada corretor: a
// ficha, o cadastro e o vínculo da conta.
const escritasPorCorretor = 3

func corretoresDeDesenvolvimento(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	var c contagem
	c.Previstas = escritasPorCorretor * len(corretoresSeed)

	// A mesma trava das contas, e não a dos contatos de demonstração: o
	// cadastro é da CONTA de desenvolvimento. Sem a conta não há o que ligar, e
	// com ela forçada em produção (`SEED_DEV_USERS=true`) o corretor dela
	// precisa do cadastro para vender — senão a trava liberaria uma conta que
	// não consegue fazer a única coisa para a qual existe.
	if !contasDeDesenvolvimentoLiberadas(st) {
		slog.Warn("cadastro do corretor de desenvolvimento ignorado em produção",
			"motivo", "acompanha as contas de desenvolvimento, que estão travadas",
			"como_forcar", "SEED_DEV_USERS=true")
		c.Previstas = 0
		return c, nil
	}

	emails := coluna(corretoresSeed, func(s corretorSeed) string { return strings.ToLower(s.email) })
	telefones := coluna(corretoresSeed, func(s corretorSeed) string { return s.contato.telefone })

	// 1. A pessoa.
	cc, err := gravarContatos(ctx, tx, st.propriedadeID,
		coluna(corretoresSeed, func(s corretorSeed) contatoDemo { return s.contato }))
	if err != nil {
		return c, fmt.Errorf("gravando o contato do corretor: %w", err)
	}
	c.somar(contagem{Criadas: cc.Criadas, Atualizadas: cc.Atualizadas})

	// 2. O cadastro. A chave natural é a conta (`ON CONFLICT (user_id)`): esta
	// etapa existe para que CADA conta de corretor tenha um cadastro, e é por
	// ela que a segunda execução reencontra a linha.
	//
	// O `DO UPDATE` corrige só o que é do seed — a ficha a que o cadastro
	// aponta. `goal_cents` e `active` ficam fora de propósito: meta é decisão
	// comercial que a spec não numera (o DEFAULT 0 é "sem meta"), e reescrevê-la
	// a cada `make seed` apagaria a meta que a gestão definiu; desativar o
	// corretor é decisão da gestão pelo mesmo motivo. É a regra das contas
	// (quem já existe não tem a senha reescrita) aplicada ao cadastro.
	const qCadastro = `
		INSERT INTO brokers (property_id, contact_id, user_id)
		SELECT $1, ct.id, u.id
		  FROM unnest($2::text[], $3::text[]) AS s(email, telefone)
		  JOIN users    u  ON lower(u.email) = s.email
		  JOIN contacts ct ON ct.phone_e164  = s.telefone
		ON CONFLICT (user_id) DO UPDATE
		   SET contact_id = EXCLUDED.contact_id,
		       updated_at = now()
		 WHERE brokers.contact_id IS DISTINCT FROM EXCLUDED.contact_id
		RETURNING (xmax = 0)`

	cb, err := upsert(ctx, tx, qCadastro, st.propriedadeID, emails, telefones)
	if err != nil {
		return c, fmt.Errorf("gravando o cadastro do corretor: %w", err)
	}
	c.somar(cb)

	// 3. O vínculo. É UPDATE e não upsert — a conta já existe (etapa anterior)
	// —, então toda linha devolvida é uma atualização, e o RETURNING diz isso
	// literalmente: `(xmax = 0)` seria verdadeiro também na versão nova de uma
	// linha atualizada, e o log contaria o vínculo como linha criada.
	const qVinculo = `
		UPDATE users u
		   SET broker_id  = b.id,
		       updated_at = now()
		  FROM brokers b
		 WHERE b.user_id = u.id
		   AND lower(u.email) = ANY($1::text[])
		   AND u.broker_id IS DISTINCT FROM b.id
		RETURNING false`

	cv, err := upsert(ctx, tx, qVinculo, emails)
	if err != nil {
		return c, fmt.Errorf("ligando a conta ao cadastro do corretor: %w", err)
	}
	c.somar(cv)

	// Pós-condição, conferida no banco. Os JOINs acima descartam EM SILÊNCIO
	// a conta que não existe (perfil `corretor` ausente, e-mail digitado
	// errado em corretoresSeed): a etapa contaria três "inalteradas" para um
	// corretor que nunca foi ligado, e a prova de idempotência mentiria. Aqui
	// a ausência vira erro e o seed inteiro volta atrás.
	var soltas []string
	linhas, err := tx.Query(ctx, `
		SELECT s.email
		  FROM unnest($1::text[], $2::text[]) AS s(email, telefone)
		 WHERE NOT EXISTS (
		         SELECT 1
		           FROM users u
		           JOIN brokers  b  ON b.id = u.broker_id AND b.user_id = u.id
		           JOIN contacts ct ON ct.id = b.contact_id
		          WHERE lower(u.email) = s.email
		            AND ct.phone_e164  = s.telefone)
		 ORDER BY 1`, emails, telefones)
	if err != nil {
		return c, fmt.Errorf("conferindo o vínculo dos corretores: %w", err)
	}
	for linhas.Next() {
		var e string
		if err := linhas.Scan(&e); err != nil {
			linhas.Close()
			return c, err
		}
		soltas = append(soltas, e)
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		return c, err
	}
	if len(soltas) > 0 {
		return c, fmt.Errorf("conta(s) de corretor sem cadastro comercial depois do seed: %s — "+
			"a conta existe em usuariosSeed com perfil corretor?", strings.Join(soltas, ", "))
	}
	return c, nil
}
