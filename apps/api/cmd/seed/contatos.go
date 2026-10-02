package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Contatos de demonstração — as pessoas de mentira sem as quais nenhuma tela do
// CRM tem o que desenhar.
//
// Existem por duas razões concretas:
//
//   - `reservations.contact_id` e `crm_opportunities.contact_id` são NOT NULL:
//     sem contato no banco não há como abrir um orçamento, uma pré-reserva, uma
//     oportunidade — nem rodar o smoke da jornada (WhatsApp → lead → orçamento
//     → hold → sinal) num ambiente recém-semeado.
//   - Com UM contato só, a tela de contatos e o funil nascem praticamente
//     vazios: não dá para ver ordenação, busca por nome, recorte por base legal
//     nem a diferença entre quem aceitou receber oferta e quem não aceitou.
//     Tela vazia não prova que a tela funciona.
//
// É dado de APOIO ao desenvolvimento, não dado real da casa — daí a trava de
// produção lá embaixo e o `.invalid` em todo e-mail.
//
// AS QUATRO BASES LEGAIS, e por que variam: `lgpd_basis` é a base que sustenta
// GUARDAR a ficha (LGPD art. 7), e o seed é o único lugar onde alguém aprende o
// vocabulário por exemplo. Um seed em que todo mundo é `legitimo_interesse`
// ensina que o campo é decorativo. Aqui cada ficha nasce com a base que a
// história dela justifica, e `marketing_opt_in` só é `true` onde existe
// `consent_at` — porque são eixos separados: `contrato` legitima guardar o
// cadastro de quem se hospedou e NÃO legitima mandar oferta para ele.
type contatoDemo struct {
	nome     string
	email    string
	telefone string
	cidade   string
	uf       string
	base     string // consentimento | contrato | obrigacao_legal | legitimo_interesse
	optIn    bool
	consenti *time.Time // preenchido só quando optIn é true
	nota     string
}

// O instante do consentimento é FIXO, e não `now()`: seed idempotente não pode
// gravar um valor diferente a cada execução — na segunda rodada a linha
// apareceria como "atualizada" para sempre, e a prova de idempotência (criadas
// = atualizadas = 0) morreria junto.
//
// E é um instante COM FUSO, não uma data solta. `data("2026-08-01")` renderia
// meia-noite UTC, que em America/Fortaleza é 31/07 às 21h — a ficha diria que a
// pessoa consentiu no dia anterior ao que o seed escreveu. É a regra 5 do
// CLAUDE.md aplicada ao próprio dado de demonstração.
var consentimentoDemo = instante("2026-08-01T12:00:00-03:00")

// Os telefones ficam todos na faixa `9 0000 xxxx`, que NÃO é atribuível a
// celular no Brasil: nenhum número aqui colide com o de uma pessoa real, e o
// WhatsApp jamais casa um contato de verdade com estas linhas. O formato é
// E.164 completo (`+55` + DDD + 9 dígitos), que é como a coluna
// `contacts.phone_e164` é lida e como a chave natural do upsert funciona.
//
// `.invalid` é reservado pela RFC 2606: nunca resolve, então nenhum disparo de
// e-mail acidental sai daqui para uma caixa de verdade.
//
// Documento (`doc_type`/`doc_number`) fica NULO de propósito: CPF inventado ou
// passa na validação de dígito e vira dado plausível que alguém acredita, ou
// não passa e quebra a primeira tela que validar. Nenhum dos dois ajuda.
var contatosDemo = []contatoDemo{
	{
		// O primeiro é o contato histórico do seed (mesmo telefone, mesma
		// ficha): a chave natural é `phone_e164`, então mantê-lo idêntico é o
		// que impede esta mudança de reescrever o que já está nos bancos em
		// uso.
		nome:     "Hóspede de Demonstração",
		email:    "hospede.demo@whitehousevillage.invalid",
		telefone: "+5585900000000",
		cidade:   "Fortaleza",
		uf:       "CE",
		base:     "legitimo_interesse",
		optIn:    false,
		nota:     "Contato criado pelo seed para orçamentos e reservas de exemplo. Não é pessoa real.",
	},
	{
		// O lead que preencheu formulário no site: base `consentimento`, e é o
		// único que pode receber oferta — com a data do aceite gravada.
		nome:     "Mariana Alencar",
		email:    "mariana.alencar@whitehousevillage.invalid",
		telefone: "+5585900000001",
		cidade:   "Fortaleza",
		uf:       "CE",
		base:     "consentimento",
		optIn:    true,
		consenti: &consentimentoDemo,
		nota:     "Demonstração: lead do formulário do site, aceitou receber ofertas. Não é pessoa real.",
	},
	{
		// Quem já se hospedou: base `contrato`. Guardar a ficha é legítimo pela
		// estadia; mandar oferta, não — por isso `optIn` false.
		nome:     "Ricardo Belchior",
		email:    "ricardo.belchior@whitehousevillage.invalid",
		telefone: "+5511900000002",
		cidade:   "São Paulo",
		uf:       "SP",
		base:     "contrato",
		optIn:    false,
		nota:     "Demonstração: hóspede recorrente, ficha sustentada pela estadia. Não é pessoa real.",
	},
	{
		// Prospecção de evento registrada pelo corretor: `legitimo_interesse`,
		// que é a base da prospecção — e não autoriza marketing.
		nome:     "Tatiana Furtado",
		email:    "tatiana.furtado@whitehousevillage.invalid",
		telefone: "+5588900000003",
		cidade:   "Juazeiro do Norte",
		uf:       "CE",
		base:     "legitimo_interesse",
		optIn:    false,
		nota:     "Demonstração: prospecção de casamento na casa inteira, aberta pelo corretor. Não é pessoa real.",
	},
}

// contatosDeDemonstracao insere (ou reconcilia) as fichas de apoio.
//
// Mesma trava dos usuários de desenvolvimento: dado fictício não entra em
// produção sem alguém pedir. A diferença é que aqui o risco não é senha
// conhecida, é o CRM da casa nascer com leads que não existem — e a base de
// contatos é justamente onde a operação confia no que vê.
func contatosDeDemonstracao(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	var c contagem
	c.Previstas = len(contatosDemo)

	if st.producao && os.Getenv("SEED_DEMO_DATA") != "true" {
		slog.Warn("contatos de demonstração ignorados em produção",
			"motivo", "contato fictício polui a base real de leads",
			"quantos", len(contatosDemo),
			"como_forcar", "SEED_DEMO_DATA=true")
		c.Previstas = 0
		return c, nil
	}

	cu, err := gravarContatos(ctx, tx, st.propriedadeID, contatosDemo)
	c.Criadas = cu.Criadas
	c.Atualizadas = cu.Atualizadas
	return c, err
}

// gravarContatos é o upsert de ficha por telefone, compartilhado pelas etapas
// que semeiam pessoas: os contatos de demonstração e o contato do corretor de
// desenvolvimento (corretores.go). Uma instrução só para as duas, e não uma
// cópia em cada arquivo: a guarda `IS DISTINCT FROM` precisa listar exatamente
// as colunas que o `SET` escreve, e duas cópias divergem na primeira coluna
// nova — a linha passa a voltar como "atualizada" em toda execução.
func gravarContatos(ctx context.Context, tx pgx.Tx, propriedade pgtype.UUID, contatos []contatoDemo) (contagem, error) {
	// O `ON CONFLICT` precisa repetir o predicado do índice parcial: sem o
	// `WHERE phone_e164 IS NOT NULL` o Postgres não consegue inferir qual
	// índice arbitra o conflito e recusa a instrução.
	//
	// A guarda `IS DISTINCT FROM` cobre TODAS as colunas que o seed escreve —
	// inclusive `consent_at`. Coluna escrita e ausente da comparação é linha
	// que volta como "atualizada" em toda execução, e é assim que uma
	// idempotência se perde sem ninguém notar.
	const q = `
		INSERT INTO contacts (property_id, name, email, phone_e164, city, state,
		                      lgpd_basis, marketing_opt_in, consent_at, notes)
		SELECT $1, c.nome, c.email, c.telefone, c.cidade, c.uf,
		       c.base, c.opt_in, c.consent_at, c.nota
		  FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::text[],
		              $7::text[], $8::boolean[], $9::timestamptz[], $10::text[])
		       AS c(nome, email, telefone, cidade, uf, base, opt_in, consent_at, nota)
		ON CONFLICT (phone_e164) WHERE phone_e164 IS NOT NULL DO UPDATE
		   SET name             = EXCLUDED.name,
		       email            = EXCLUDED.email,
		       city             = EXCLUDED.city,
		       state            = EXCLUDED.state,
		       lgpd_basis       = EXCLUDED.lgpd_basis,
		       marketing_opt_in = EXCLUDED.marketing_opt_in,
		       consent_at       = EXCLUDED.consent_at,
		       notes            = EXCLUDED.notes,
		       updated_at       = now()
		 WHERE (contacts.name, contacts.email, contacts.city, contacts.state,
		        contacts.lgpd_basis, contacts.marketing_opt_in, contacts.consent_at,
		        contacts.notes)
		       IS DISTINCT FROM
		       (EXCLUDED.name, EXCLUDED.email, EXCLUDED.city, EXCLUDED.state,
		        EXCLUDED.lgpd_basis, EXCLUDED.marketing_opt_in, EXCLUDED.consent_at,
		        EXCLUDED.notes)
		RETURNING (xmax = 0)`

	return upsert(ctx, tx, q, propriedade,
		coluna(contatos, func(c contatoDemo) string { return c.nome }),
		coluna(contatos, func(c contatoDemo) string { return c.email }),
		coluna(contatos, func(c contatoDemo) string { return c.telefone }),
		coluna(contatos, func(c contatoDemo) string { return c.cidade }),
		coluna(contatos, func(c contatoDemo) string { return c.uf }),
		coluna(contatos, func(c contatoDemo) string { return c.base }),
		coluna(contatos, func(c contatoDemo) bool { return c.optIn }),
		coluna(contatos, func(c contatoDemo) *time.Time { return c.consenti }),
		coluna(contatos, func(c contatoDemo) string { return c.nota }),
	)
}
