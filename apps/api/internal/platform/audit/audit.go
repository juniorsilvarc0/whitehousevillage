// Package audit grava a trilha de `audit_log` — quem escreveu o quê, quando, de
// onde, e como o dado estava antes e depois.
//
// # A regra que este pacote existe para sustentar
//
// `docs/spec.md` §16: "toda escrita grava ator, entidade, antes e depois, IP e
// request_id". Na Fase 1 a promessa não foi cumprida por nenhum módulo — medido
// nesta árvore, uma execução da suíte de integração de inventário, reservas e
// tarifário produziu 1626 tuplas escritas nas tabelas de negócio e ZERO linhas
// em `audit_log`. Sem elas não há resposta para "quem desativou AP-03?" nem
// para "quem triplicou a tarifa?".
//
// # As duas decisões que definem o pacote
//
//  1. A trilha entra na TRANSAÇÃO DE QUEM CHAMA. Registrar pega o executor por
//     [db.From], que devolve a transação do contexto quando ela existe. Auditoria
//     que comita em transação própria MENTE quando a operação de negócio depois
//     falha: fica no banco o registro de um cancelamento que nunca aconteceu.
//     O caminho inverso — negócio comitado sem trilha — é o defeito acima.
//
//  2. Segredo nunca é gravado. A redação roda DENTRO de Registrar, no último
//     passo antes de serializar, e não tem como ser desligada por quem chama.
//     Ver redigir.go.
//
// # Uso
//
//	antes := unidadeAtual              // struct do repositório, antes do UPDATE
//	depois := unidadeAtualizada
//	err := audit.Alteracao(ctx, r.pool, "units", audit.VerboAlterado, u.ID, antes, depois)
//
// Ator, IP, user-agent e request_id saem do contexto: quem chama não os digita
// e, portanto, não os esquece.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/netip"
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Campos é o documento que vai para `before`/`after` (colunas `jsonb`).
//
// Mapa, e não coluna por campo, porque a trilha registra entidades de formatos
// diferentes: uma coluna nova a cada campo auditado tornaria a tabela refém de
// migration a cada módulo novo.
type Campos map[string]any

// Verbos canônicos do `action`. O vocabulário é aberto de propósito — reserva
// tem "confirmada", "cancelada", "remarcada", "check_in" —, mas as três escritas
// genéricas têm nome único para a tela de auditoria poder agrupar.
const (
	VerboCriado   = "criado"
	VerboAlterado = "alterado"
	VerboExcluido = "excluido"
)

// Acao monta o `action` no formato `<entidade>.<verbo>`.
//
// A gramática NÃO é nova: é a que `internal/modules/users` já usa
// (`users.email_alterado`, montado como recurso + "." + verbo). Padronizar por
// cima dela mantém as linhas que já existem legíveis pelo mesmo filtro da tela
// de auditoria, sem reescrever o módulo que já auditava.
func Acao(entidade, verbo string) string { return entidade + "." + verbo }

// Evento é uma linha de `audit_log`.
//
// Os campos de origem (ator, IP, user-agent, request_id) podem ficar vazios:
// Registrar os preenche a partir do contexto da requisição. Preenchê-los à mão
// só é necessário fora de requisição HTTP — worker, seed, script de manutenção.
type Evento struct {
	// PropriedadeID pode ser uuid.Nil: a coluna é anulável justamente para o
	// rastro de um script sem propriedade resolvida não ser descartado.
	PropriedadeID uuid.UUID
	// AtorID nulo é ator anônimo (seed, job). A coluna é anulável pela mesma
	// razão: sem usuário para culpar o rastro continua valendo.
	AtorID     *uuid.UUID
	Acao       string
	Entidade   string
	EntidadeID uuid.UUID
	Antes      Campos
	Depois     Campos
	IP         string
	UserAgent  string
	RequestID  string
}

// tamanhoMaximoUserAgent espelha o truncamento de `internal/auth` (255): o
// header é texto de fora e um valor gigante encheria a tabela.
const tamanhoMaximoUserAgent = 255

// Registrar grava a linha usando o executor da transação em curso.
//
// `exec` é o pool do repositório que chama; [db.From] o troca pela transação do
// contexto quando existe uma. Assim a MESMA chamada serve dentro e fora de
// transação, e dentro dela o rollback do negócio desfaz a trilha junto — que é
// o comportamento correto: não existe cancelamento auditado que não aconteceu.
//
// Fora de transação a linha é gravada assim mesmo (não descartar rastro é mais
// importante do que ser purista), mas sai um aviso no log: escrita de negócio
// sem transação é quase sempre esquecimento de quem chamou, e o aviso é o que
// faz esse esquecimento aparecer antes da auditoria precisar da linha.
func Registrar(ctx context.Context, exec db.DBTX, ev Evento) error {
	if strings.TrimSpace(ev.Acao) == "" || strings.TrimSpace(ev.Entidade) == "" {
		// Erro de programação, não do usuário: uma linha sem ação nem entidade
		// é ruído que ninguém consegue consultar depois.
		return apperr.Internal.WithCause(errCampoObrigatorio)
	}

	ev = comOrigemDoContexto(ctx, ev)

	if !db.EmTransacao(ctx) {
		slog.WarnContext(ctx, "trilha de auditoria gravada FORA de transação",
			"action", ev.Acao, "entity", ev.Entidade, "entity_id", ev.EntidadeID,
			"request_id", ev.RequestID)
	}

	// A redação é o último passo antes de serializar e não é opcional: é o
	// único ponto por onde os dois documentos passam a caminho do banco.
	antes, err := paraJSONB(Redigir(ev.Antes))
	if err != nil {
		return err
	}
	depois, err := paraJSONB(Redigir(ev.Depois))
	if err != nil {
		return err
	}

	const q = `
		INSERT INTO audit_log
		       (property_id, actor_id, action, entity, entity_id, before, after, ip, user_agent, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	if _, err := db.From(ctx, exec).Exec(ctx, q,
		nuloSeUUIDVazio(ev.PropriedadeID),
		ev.AtorID,
		ev.Acao,
		ev.Entidade,
		nuloSeUUIDVazio(ev.EntidadeID),
		antes,
		depois,
		enderecoIP(ev.IP),
		nuloSeVazio(truncar(ev.UserAgent, tamanhoMaximoUserAgent)),
		nuloSeVazio(ev.RequestID),
	); err != nil {
		return db.MapError(err)
	}
	return nil
}

// Criacao registra o nascimento de uma entidade: `before` fica nulo porque não
// havia nada antes — gravar `{}` faria a tela de auditoria mostrar "de vazio
// para X" como se fosse uma edição.
func Criacao(ctx context.Context, exec db.DBTX, entidade, verbo string, id uuid.UUID, depois any) error {
	return Registrar(ctx, exec, Evento{
		PropriedadeID: propriedadeDoContexto(ctx),
		Acao:          Acao(entidade, verbo),
		Entidade:      entidade,
		EntidadeID:    id,
		Depois:        Snapshot(depois),
	})
}

// Alteracao registra uma edição guardando SÓ o que mudou.
//
// O recorte é o que responde "quem triplicou a tarifa?" numa linha: sem ele,
// `before`/`after` trazem a entidade inteira duas vezes e o campo alterado fica
// escondido no meio de vinte iguais. A linha é gravada mesmo quando nada mudou
// (com os dois documentos nulos) — pular o INSERT nesse caso transformaria um
// snapshot mal tirado pelo chamador em rastro silenciosamente perdido.
func Alteracao(ctx context.Context, exec db.DBTX, entidade, verbo string, id uuid.UUID, antes, depois any) error {
	a, d := Diff(Snapshot(antes), Snapshot(depois))
	return Registrar(ctx, exec, Evento{
		PropriedadeID: propriedadeDoContexto(ctx),
		Acao:          Acao(entidade, verbo),
		Entidade:      entidade,
		EntidadeID:    id,
		Antes:         a,
		Depois:        d,
	})
}

// Exclusao registra a remoção (ou desativação) guardando o estado que deixou de
// existir: depois do DELETE/UPDATE a linha já não está mais na tabela, e o
// `before` é a única cópia que sobra.
func Exclusao(ctx context.Context, exec db.DBTX, entidade, verbo string, id uuid.UUID, antes any) error {
	return Registrar(ctx, exec, Evento{
		PropriedadeID: propriedadeDoContexto(ctx),
		Acao:          Acao(entidade, verbo),
		Entidade:      entidade,
		EntidadeID:    id,
		Antes:         Snapshot(antes),
	})
}

// ─────────────────────────── Contexto ───────────────────────────────────────

// comOrigemDoContexto completa o que o chamador não preencheu. O que ele
// preencheu explicitamente MANDA — é assim que um worker informa o ator real de
// uma ação disparada por alguém em outra requisição.
func comOrigemDoContexto(ctx context.Context, ev Evento) Evento {
	if ev.AtorID == nil {
		if u, ok := auth.UserFrom(ctx); ok {
			id := u.ID
			ev.AtorID = &id
		}
	}
	if ev.PropriedadeID == uuid.Nil {
		ev.PropriedadeID = propriedadeDoContexto(ctx)
	}
	if ev.RequestID == "" {
		ev.RequestID = httpx.RequestIDDoContexto(ctx)
	}
	o := OrigemDoContexto(ctx)
	if ev.IP == "" {
		ev.IP = o.IP
	}
	if ev.UserAgent == "" {
		ev.UserAgent = o.UserAgent
	}
	return ev
}

// propriedadeDoContexto usa a propriedade do ator. A instalação é de uma
// propriedade só; quando houver a segunda, o módulo que auditar entidade de
// OUTRA propriedade preenche Evento.PropriedadeID à mão.
func propriedadeDoContexto(ctx context.Context) uuid.UUID {
	if u, ok := auth.UserFrom(ctx); ok {
		return u.PropertyID
	}
	return uuid.Nil
}

// ─────────────────────────── Auxiliares ─────────────────────────────────────

var errCampoObrigatorio = errors.New("auditoria sem action ou entity")

// paraJSONB serializa o documento. Mapa vazio vira NULL de SQL, e não o `null`
// de jsonb: `before IS NULL` é o teste que a consulta da tela de auditoria faz
// para separar criação de edição, e `'null'::jsonb` não satisfaz esse teste.
func paraJSONB(c Campos) ([]byte, error) {
	if len(c) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, apperr.Internal.WithCause(err)
	}
	return b, nil
}

// enderecoIP valida antes de mandar para a coluna `inet`.
//
// String inválida ali não é dado ruim gravado: é erro 22P02 que ABORTA a
// transação do negócio. Auditoria derrubando a operação que ela deveria apenas
// testemunhar seria o pior desfecho possível, então o que não é endereço vira
// NULL. `netip.Addr` é o tipo que o pgx v5 encoda nativamente para `inet`.
func enderecoIP(s string) any {
	addr, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return addr
}

func nuloSeUUIDVazio(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func nuloSeVazio(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// truncar corta por RUNA, não por byte: cortar no meio de um caractere
// multibyte produziria texto inválido numa coluna que alguém vai ler depois.
func truncar(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
