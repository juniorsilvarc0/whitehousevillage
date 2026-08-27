package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5"
)

// Contato de demonstração — o destinatário das reservas de exemplo da Fase 1.
//
// Existe porque `reservations.contact_id` é NOT NULL: sem um contato no banco
// não há como abrir um orçamento, uma pré-reserva ou um smoke test da jornada
// (WhatsApp → lead → orçamento → hold → sinal) num ambiente recém-semeado. É
// dado de apoio ao desenvolvimento, não dado real da casa.
const (
	// `.invalid` é reservado pela RFC 2606: nunca resolve, então nenhum
	// disparo de e-mail acidental sai daqui para uma caixa de verdade.
	contatoDemoEmail = "hospede.demo@whitehousevillage.invalid"

	// A chave natural do contato (docs/db.md §6: `UNIQUE(phone_e164) WHERE
	// phone_e164 IS NOT NULL`) — é ela que torna esta etapa idempotente. O
	// prefixo `9 0000` não é faixa atribuível a celular no Brasil, então o
	// número não colide com o de nenhuma pessoa e o WhatsApp jamais casa um
	// contato real com esta linha.
	contatoDemoTelefone = "+5585900000000"

	contatoDemoNome = "Hóspede de Demonstração"
)

// contatoDeDemonstracao insere (ou reconcilia) o contato de apoio.
//
// Mesma trava dos usuários de desenvolvimento: dado fictício não entra em
// produção sem alguém pedir. A diferença é que aqui o risco não é senha
// conhecida, é o CRM da casa nascer com um lead que não existe — e a base de
// contatos é justamente onde a operação confia no que vê.
func contatoDeDemonstracao(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	var c contagem
	c.Previstas = 1

	if st.producao && os.Getenv("SEED_DEMO_DATA") != "true" {
		slog.Warn("contato de demonstração ignorado em produção",
			"motivo", "contato fictício polui a base real de leads",
			"como_forcar", "SEED_DEMO_DATA=true")
		c.Previstas = 0
		return c, nil
	}

	// O `ON CONFLICT` precisa repetir o predicado do índice parcial: sem o
	// `WHERE phone_e164 IS NOT NULL` o Postgres não consegue inferir qual
	// índice arbitra o conflito e recusa a instrução.
	//
	// `lgpd_basis = 'legitimo_interesse'` e `marketing_opt_in = false` porque é
	// assim que um contato nasce sem consentimento datado (spec §6): o registro
	// de demonstração não pode ensinar o padrão errado a quem ler o seed.
	const q = `
		INSERT INTO contacts (property_id, name, email, phone_e164, city, state,
		                      lgpd_basis, marketing_opt_in, notes)
		VALUES ($1, $2, $3, $4, 'Fortaleza', 'CE', 'legitimo_interesse', false, $5)
		ON CONFLICT (phone_e164) WHERE phone_e164 IS NOT NULL DO UPDATE
		   SET name             = EXCLUDED.name,
		       email            = EXCLUDED.email,
		       city             = EXCLUDED.city,
		       state            = EXCLUDED.state,
		       lgpd_basis       = EXCLUDED.lgpd_basis,
		       marketing_opt_in = EXCLUDED.marketing_opt_in,
		       notes            = EXCLUDED.notes,
		       updated_at       = now()
		 WHERE (contacts.name, contacts.email, contacts.city, contacts.state,
		        contacts.lgpd_basis, contacts.marketing_opt_in, contacts.notes)
		       IS DISTINCT FROM
		       (EXCLUDED.name, EXCLUDED.email, EXCLUDED.city, EXCLUDED.state,
		        EXCLUDED.lgpd_basis, EXCLUDED.marketing_opt_in, EXCLUDED.notes)
		RETURNING (xmax = 0)`

	const nota = "Contato criado pelo seed para orçamentos e reservas de exemplo. Não é pessoa real."

	cu, err := upsert(ctx, tx, q, st.propriedadeID,
		contatoDemoNome, contatoDemoEmail, contatoDemoTelefone, nota)
	c.Criadas = cu.Criadas
	c.Atualizadas = cu.Atualizadas
	return c, err
}
