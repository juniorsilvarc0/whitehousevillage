package contatos

import (
	"context"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Motivos gravados em `pii_access_log.reason`. Vocabulário fechado: a coluna é
// texto livre, mas um valor por chamador tornaria a consulta "quantas vezes
// alguém abriu a ficha desta pessoa?" impossível de escrever.
const (
	MotivoDeLeituraFicha     = "detail"
	MotivoDeLeituraExportada = "export"
)

// registrarAcessoPII grava a LEITURA de dado pessoal.
//
// # A tabela existia e ninguém escrevia nela
//
// `pii_access_log` está no schema desde 20260820120000_core.up.sql:111. Medido
// nesta árvore antes desta entrega: `grep` por `pii_access_log` em todo o
// `apps/api` só encontrava as duas migrations, e `SELECT count(*)` no banco de
// desenvolvimento devolvia 0. A spec §16 cobra o registro de leitura de dado
// pessoal — que a auditoria comum, que só registra ESCRITA, não cobre por
// definição.
//
// # Falha fechada
//
// Erro ao gravar o acesso ABORTA a leitura. É deliberado, e é o ponto em que
// esta função difere de `audit.Registrar` (que grava um aviso e segue): a
// trilha de acesso é a razão de o endpoint ser diferente da lista. Servir a
// ficha sem conseguir registrar quem a leu é cumprir a parte cômoda da
// obrigação e descartar a outra — em silêncio, e justamente no caso em que
// alguém está varrendo a base.
//
// O custo assumido: `pii_access_log` indisponível derruba `GET /contacts/{id}`
// e `/export`. É uma tabela sem FK de saída, sem trigger e com um INSERT de
// quatro colunas; se ela não aceita escrita, o banco não está saudável de
// qualquer jeito.
//
// PARA O INTEGRADOR: o lugar desta função é `internal/platform/pii`, irmã de
// `internal/platform/audit` — a próxima tela que mostrar dado pessoal
// (financeiro, chat, hóspedes de uma reserva) precisa dela e não deve importar
// o módulo de contatos para consegui-la. Está aqui porque `internal/platform`
// não é pasta deste agente nesta rodada.
func registrarAcessoPII(ctx context.Context, exec db.DBTX, contatoID uuid.UUID, motivo string) error {
	const q = `INSERT INTO pii_access_log (actor_id, contact_id, reason) VALUES ($1, $2, $3)`

	var ator *uuid.UUID
	if u, ok := auth.UserFrom(ctx); ok {
		id := u.ID
		ator = &id
	}

	// `db.From` faz a linha entrar na transação de quem chamou, quando há uma.
	// Nas leituras não há — e é correto que não haja: o registro do acesso não
	// deve ser desfeito por um erro posterior de serialização da resposta. O
	// acesso ACONTECEU.
	if _, err := db.From(ctx, exec).Exec(ctx, q, ator, contatoID, motivo); err != nil {
		return db.MapError(err)
	}
	return nil
}
