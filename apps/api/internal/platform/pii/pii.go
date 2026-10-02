// Package pii cuida das duas obrigações que acompanham dado pessoal em toda
// tela que o exibe: REGISTRAR quem leu e IMPEDIR que o valor lido acabe copiado
// para uma trilha que outra gente consegue ler.
//
// # Por que é plataforma, e não um arquivo dentro do módulo que serve a ficha
//
// Até esta entrega as duas funções moravam em `internal/modules/contatos`
// (`registrarAcessoPII` e `semPII`), porque `internal/platform` não era pasta
// do agente que as escreveu — a dívida D6 do roadmap. Contatos era a única tela
// com dado pessoal e a obrigação estava cumprida ali.
//
// A Fase 2 traz três telas novas com PII (recebível com nome do hóspede,
// comissão com nome do corretor, rooming list com CPF e telefone). Com a função
// privada do módulo, a tela nova ou importa o módulo de contatos para conseguir
// registrar, ou — o que é mais provável e muito pior — não registra nada, e a
// obrigação da LGPD passa a valer só onde ela já valia.
//
// # Falha fechada
//
// Não conseguir gravar o acesso ABORTA a leitura. É o ponto em que este pacote
// difere de `audit.Registrar`, que grava um aviso e segue: a trilha de acesso é
// a razão de o endpoint da ficha ser diferente do da lista. Servir a ficha sem
// conseguir registrar quem a leu é cumprir a parte cômoda da obrigação e
// descartar a outra — em silêncio, e justamente no caso em que alguém está
// varrendo a base.
//
// O custo assumido: `pii_access_log` indisponível derruba `GET /contacts/{id}`
// e `/export`. É uma tabela sem FK de saída, sem trigger e com um INSERT de
// quatro colunas; se ela não aceita escrita, o banco não está saudável de
// qualquer jeito.
package pii

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Motivos gravados em `pii_access_log.reason`.
//
// Vocabulário fechado: a coluna é texto livre, mas um valor por chamador
// tornaria "quantas vezes abriram a ficha desta pessoa, e para quê?" impossível
// de escrever. Medido no banco de desenvolvimento em 31/08/2026:
// `select reason, count(*) from pii_access_log group by reason` devolve uma
// linha só — `detail | 44`. O vocabulário está fechado hoje e é barato mantê-lo
// assim: quem precisa de um motivo novo acrescenta a constante aqui, e é isso
// que faz o motivo novo aparecer para quem consulta.
const (
	MotivoFicha      = "detail"
	MotivoExportacao = "export"
)

var motivosConhecidos = map[string]bool{
	MotivoFicha:      true,
	MotivoExportacao: true,
}

// inserePorEntidade amarra a entidade lida à coluna que a registra.
//
// `pii_access_log` tem UMA coluna de alvo — `contact_id` —, então uma entidade
// fora deste mapa não tem onde pousar. O mapa é a razão de `entidade` ser
// parâmetro e não constante: sem ele, a tela de comissão de amanhã gravaria o
// id do corretor dentro de `contact_id` e a consulta de acesso passaria a
// misturar duas populações de gente diferente — que é pior do que não ter a
// linha, porque parece certo.
//
// PARA O INTEGRADOR (`db-migrations`): quando a primeira entidade não-contato
// precisar de trilha de leitura, a tabela ganha `entity text` + `entity_id
// uuid` e este mapa ganha a linha correspondente. Até lá, entidade desconhecida
// falha fechada — ver Registrar.
var inserePorEntidade = map[string]string{
	// O nome é o da TABELA, o mesmo que vai em `audit_log.entity`, para a
	// consulta de LGPD conseguir cruzar leitura e escrita sem tradução no meio.
	"contacts": `INSERT INTO pii_access_log (actor_id, contact_id, reason) VALUES ($1, $2, $3)`,
}

var (
	errEntidadeSemColuna  = errors.New("pii: entidade sem coluna em pii_access_log")
	errMotivoDesconhecido = errors.New("pii: motivo fora do vocabulário de pii_access_log")
)

// Registrar grava a LEITURA de dado pessoal e devolve erro se não conseguir.
//
// Quem chama ABORTA a leitura com esse erro — é o contrato do pacote, e o
// motivo de a função devolver `error` em vez de registrar e seguir.
//
// `exec` é o pool de quem chama; [db.From] o troca pela transação do contexto
// quando existe uma. Nas leituras não existe, e é correto que não exista: o
// registro do acesso não deve ser desfeito por um erro posterior de
// serialização da resposta. O acesso ACONTECEU.
func Registrar(ctx context.Context, exec db.DBTX, entidade string, id uuid.UUID, motivo string) error {
	inserir, conhecida := inserePorEntidade[entidade]
	if !conhecida {
		return apperr.Internal.WithCause(fmt.Errorf("%w: %q", errEntidadeSemColuna, entidade))
	}
	if !motivosConhecidos[motivo] {
		return apperr.Internal.WithCause(fmt.Errorf("%w: %q", errMotivoDesconhecido, motivo))
	}

	var ator *uuid.UUID
	if u, autenticado := auth.UserFrom(ctx); autenticado {
		atorID := u.ID
		ator = &atorID
	}

	if _, err := db.From(ctx, exec).Exec(ctx, inserir, ator, id, motivo); err != nil {
		return db.MapError(err)
	}
	return nil
}
