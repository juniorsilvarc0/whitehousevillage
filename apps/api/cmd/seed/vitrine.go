package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// A conta de serviço da vitrine — a identidade das escritas que nascem no site
// de vendas (`POST /public/holds`, B1 de docs/unificacao-site-crm.md).
//
// Por que existe: a rota pública roda o MESMO serviço de reserva e de contato
// do painel (`reservas.Servico.Criar`, `contatos.Servico.Criar`), e esses
// serviços exigem um `auth.Usuario` no contexto — é dele que saem o escopo da
// matriz, o `owner_id`/`created_by` da reserva e a trilha de auditoria. Em vez
// de abrir um atalho "sem usuário" no domínio, o handler público carrega esta
// conta com `auth.Repository.CarregarSessao` e chama o serviço como qualquer
// outro ator. Tudo o que o site gravar fica com o nome "Site (vitrine)" na
// trilha, separável do que a equipe gravou.
//
// Por que o perfil tem só duas células: quem dispara a escrita é um navegador
// desconhecido, sem login. Cada permissão a mais neste perfil é uma permissão
// concedida à internet inteira. Por isso — e só aqui — o seed é dono do perfil
// depois que ele nasce: permissão a mais encontrada numa reexecução é
// REMOVIDA, ao contrário da matriz dos perfis de gente (acesso.go), onde o
// acréscimo da gestão sobrevive.
const (
	perfilVitrine      = "vitrine"
	nomePerfilVitrine  = "Site — vitrine pública"
	emailContaVitrine  = "vitrine@site.whitehouse.invalid"
	nomeContaVitrine   = "Site (vitrine)"
	bytesSenhaDescarte = 32
)

// concessoesDaVitrine — exatamente estas, nada mais. `all` porque nenhum dos
// dois recursos é escopo do site: `contacts` não tem dono, e a reserva criada
// pelo site é da casa, não de um corretor.
var concessoesDaVitrine = []concessao{
	{"contacts", []string{auth.AcaoCriar}, escopoAll},
	{"reservations", []string{auth.AcaoCriar}, escopoAll},
}

// matrizDaVitrine valida as concessões contra o catálogo com a mesma régua da
// matriz dos outros perfis. Pura, para o teste conferir sem banco.
func matrizDaVitrine() ([]linhaDaMatriz, error) {
	return expandirConcessoes(perfilVitrine, concessoesDaVitrine)
}

// perfilDaVitrine cria o perfil e deixa a matriz dele IGUAL à declarada:
// corrige escopo divergente e apaga qualquer célula a mais.
//
// `is_system = false` não é detalhe: `RoleIsSystem` é o sinal de perfil raiz
// (users/autoridade.go, roles/autoridade.go) — um perfil de sistema passaria
// por cima do teto de privilégio. A conta mais exposta do sistema não pode ser
// a que tem esse passe.
func perfilDaVitrine(ctx context.Context, tx pgx.Tx, _ *estado) (contagem, error) {
	linhas, err := matrizDaVitrine()
	if err != nil {
		return contagem{}, err
	}

	const qPerfil = `
		INSERT INTO roles (code, name, is_system)
		VALUES ($1, $2, false)
		ON CONFLICT (code) DO UPDATE
		   SET name = EXCLUDED.name, is_system = EXCLUDED.is_system
		 WHERE (roles.name, roles.is_system)
		       IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.is_system)
		RETURNING (xmax = 0)`
	c, err := upsert(ctx, tx, qPerfil, perfilVitrine, nomePerfilVitrine)
	if err != nil {
		return c, fmt.Errorf("perfil %s: %w", perfilVitrine, err)
	}

	recursos := coluna(linhas, func(l linhaDaMatriz) string { return l.recurso })
	acoes := coluna(linhas, func(l linhaDaMatriz) string { return l.acao })
	escopos := coluna(linhas, func(l linhaDaMatriz) string { return l.escopo })

	// O DELETE vem antes do INSERT só por clareza do log; na mesma transação a
	// ordem não muda o resultado.
	removidas, err := afetadas(ctx, tx, `
		DELETE FROM role_permissions rp
		 USING roles ro
		 WHERE ro.id = rp.role_id
		   AND ro.code = $1
		   AND (rp.resource_code, rp.action) NOT IN (
		         SELECT p.recurso, p.acao
		           FROM unnest($2::text[], $3::text[]) AS p(recurso, acao))`,
		perfilVitrine, recursos, acoes)
	if err != nil {
		return c, fmt.Errorf("limpando a matriz do perfil %s: %w", perfilVitrine, err)
	}
	if removidas > 0 {
		slog.Warn("permissões a mais removidas do perfil da vitrine",
			"perfil", perfilVitrine, "removidas", removidas,
			"motivo", "o perfil serve a uma rota sem login — só contacts:criar e reservations:criar")
	}

	const qPermissoes = `
		INSERT INTO role_permissions (role_id, resource_code, action, scope)
		SELECT ro.id, p.recurso, p.acao, p.escopo
		  FROM unnest($2::text[], $3::text[], $4::text[]) AS p(recurso, acao, escopo)
		  JOIN roles ro ON ro.code = $1
		ON CONFLICT (role_id, resource_code, action) DO UPDATE
		   SET scope = EXCLUDED.scope
		 WHERE role_permissions.scope IS DISTINCT FROM EXCLUDED.scope
		RETURNING (xmax = 0)`
	cp, err := upsert(ctx, tx, qPermissoes, perfilVitrine, recursos, acoes, escopos)
	if err != nil {
		return c, fmt.Errorf("matriz do perfil %s: %w", perfilVitrine, err)
	}
	c.somar(cp)
	c.Previstas = 1 + len(linhas)
	c.Removidas = removidas
	return c, nil
}

// contaDaVitrine cria a conta de serviço na propriedade padrão.
//
// NINGUÉM ENTRA COM ESTA CONTA. O login (auth.Service.Login) só aceita quem
// passa em `auth.Verify(password_hash, senha)`, e o hash gravado aqui é um
// argon2id legítimo de 32 bytes aleatórios (crypto/rand) gerados nesta
// execução e descartados — o texto nunca é logado, devolvido nem guardado.
// Acertá-lo é adivinhar 256 bits.
//
// Por que um hash VÁLIDO, e não um marcador como `!` ou string vazia: o login
// trata hash fora do formato como falha nossa (`apperr.Internal`, 500), e não
// como credencial errada. Um marcador faria o login desta conta responder 500
// em vez do 401 INVALID_CREDENTIALS dos outros e-mails — além de quebrar o
// login, entregaria quem tem conta. Com hash válido, o caminho é o mesmo de
// qualquer senha errada, no mesmo tempo.
//
// O hash é gerado só na criação. Reexecução não o reescreve (nem gasta o
// argon2 de 64 MiB à toa); corrige apenas nome, perfil e `broker_id`.
//
// `active` NÃO é forçado na reexecução: desativar esta conta no painel é o
// botão de emergência que fecha as escritas do site (a sessão de serviço deixa
// de carregar), e o seed roda a cada deploy — reativá-la em silêncio desfaria
// a decisão de quem apertou o botão. O mesmo para conta excluída (soft
// delete): o seed avisa e não ressuscita.
//
// Diferente das contas de desenvolvimento, esta roda TAMBÉM em produção: não
// tem senha conhecida, e sem ela o site não vende.
func contaDaVitrine(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	c := contagem{Previstas: 1}

	var (
		id       pgtype.UUID
		ativo    bool
		excluida bool
	)
	err := tx.QueryRow(ctx,
		`SELECT id, active, deleted_at IS NOT NULL FROM users WHERE lower(email) = lower($1)
		  ORDER BY deleted_at NULLS FIRST LIMIT 1`,
		emailContaVitrine).Scan(&id, &ativo, &excluida)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		hash, err := hashDeSenhaDescartada()
		if err != nil {
			return c, err
		}
		const q = `
			INSERT INTO users (property_id, role_id, name, email, password_hash, active)
			SELECT $1, ro.id, $2, $3, $4, true
			  FROM roles ro WHERE ro.code = $5
			ON CONFLICT (email) DO NOTHING
			RETURNING (xmax = 0)`
		cu, err := upsert(ctx, tx, q, st.propriedadeID, nomeContaVitrine, emailContaVitrine, hash, perfilVitrine)
		if err != nil {
			return c, fmt.Errorf("criando a conta da vitrine: %w", err)
		}
		c.somar(cu)
		return c, nil

	case err != nil:
		return c, fmt.Errorf("consultando a conta da vitrine: %w", err)

	case excluida:
		slog.Warn("conta da vitrine excluída — o seed não a recria",
			"email", emailContaVitrine,
			"efeito", "o site não consegue criar pré-reserva enquanto a conta não existir")
		c.Previstas = 0
		return c, nil
	}

	n, err := afetadas(ctx, tx, `
		UPDATE users u
		   SET name = $2, role_id = ro.id, broker_id = NULL, updated_at = now()
		  FROM roles ro
		 WHERE u.id = $1 AND ro.code = $3
		   AND (u.name, u.role_id, u.broker_id) IS DISTINCT FROM ($2, ro.id, NULL::uuid)`,
		id, nomeContaVitrine, perfilVitrine)
	if err != nil {
		return c, fmt.Errorf("corrigindo a conta da vitrine: %w", err)
	}
	c.Atualizadas = n

	if !ativo {
		slog.Warn("conta da vitrine desativada — o seed respeita",
			"email", emailContaVitrine,
			"efeito", "o site não consegue criar pré-reserva até a conta ser reativada no painel")
	}
	return c, nil
}

// hashDeSenhaDescartada devolve o argon2id de um segredo aleatório que deixa de
// existir quando a função retorna.
func hashDeSenhaDescartada() (string, error) {
	segredo := make([]byte, bytesSenhaDescarte)
	if _, err := rand.Read(segredo); err != nil {
		return "", fmt.Errorf("gerando o segredo descartável da vitrine: %w", err)
	}
	hash, err := auth.Hash(base64.RawStdEncoding.EncodeToString(segredo))
	if err != nil {
		return "", fmt.Errorf("gerando o hash da conta da vitrine: %w", err)
	}
	return hash, nil
}
