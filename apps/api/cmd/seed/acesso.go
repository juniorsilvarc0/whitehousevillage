package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// As quatro ações do CHECK de role_permissions.action.
const (
	ver     = "ver"
	criar   = "criar"
	editar  = "editar"
	excluir = "excluir"
)

var (
	somenteLeitura = []string{ver}
	verEditar      = []string{ver, editar}
	leituraEscrita = []string{ver, criar}
	semExcluir     = []string{ver, criar, editar}
	tudo           = []string{ver, criar, editar, excluir}
)

// recurso é uma linha do catálogo que alimenta `GET /roles/resources` — a fonte
// única da grade de permissões da tela de perfis.
//
// `acoes` é subconjunto declarado de propósito: nem todo recurso tem as quatro.
// `suportaOwn` diz se existe dono identificável na linha; onde não existe, a
// grade desabilita "só os meus" em vez de oferecer um escopo que o SQL não sabe
// aplicar.
type recurso struct {
	codigo     string
	rotulo     string
	grupo      string
	acoes      []string
	suportaOwn bool
	ordem      int32
}

// O catálogo. Acrescentar recurso novo é acrescentar linha AQUI — nunca um `if`
// no código (regra 8 do CLAUDE.md).
//
// Duas famílias não têm `excluir`, e isso é regra da spec, não descuido:
// o razão financeiro é append-only (§10 — nada é editado, tudo é estornado) e
// mensagem enviada não se apaga (§8). Painel, relatórios e auditoria só têm
// `ver` porque não há o que criar neles: são leitura de dado que já existe.
var catalogoSeed = []recurso{
	{"dashboard", "Painel", "Visão geral", somenteLeitura, false, 1},
	{"reports", "Relatórios e BI", "Visão geral", somenteLeitura, false, 2},

	{"reservations", "Reservas", "Operação", tudo, true, 10},
	{"calendar", "Calendário", "Operação", tudo, true, 11},
	{"agenda", "Agenda operacional", "Operação", tudo, true, 12},
	{"inventory", "Inventário e enxoval", "Operação", tudo, false, 13},
	{"channels", "Canais e OTA", "Operação", tudo, false, 14},

	{"quotes", "Orçamentos", "Comercial", tudo, true, 20},
	{"contacts", "Contatos", "Comercial", tudo, false, 21},
	{"brokers", "Corretores e parceiros", "Comercial", tudo, true, 22},

	{"crm.pipelines", "Funis e etapas", "CRM", tudo, false, 30},
	{"crm.leads", "Leads", "CRM", tudo, true, 31},
	{"crm.opportunities", "Oportunidades", "CRM", tudo, true, 32},
	{"crm.activities", "Atividades e tarefas", "CRM", tudo, true, 33},

	{"chat", "Chat e WhatsApp", "Atendimento", semExcluir, true, 40},

	{"finance.receivables", "Recebíveis", "Financeiro", semExcluir, false, 50},
	{"finance.payables", "Pagáveis", "Financeiro", semExcluir, false, 51},
	{"finance.commissions", "Comissões", "Financeiro", semExcluir, true, 52},

	{auth.RecursoUsuarios, "Usuários", "Configurações", tudo, false, 60},
	{auth.RecursoPerfis, "Perfis de acesso", "Configurações", tudo, false, 61},
	{"settings", "Parâmetros do sistema", "Configurações", tudo, false, 62},
	{"integrations", "Integrações e tokens", "Configurações", tudo, false, 63},
	{"audit", "Auditoria", "Configurações", somenteLeitura, false, 64},

	// Conteúdo do site de vendas (docs/site-cms.md). Só `ver` e `editar`: os
	// campos são um catálogo fixo em código — não se cria nem se apaga campo,
	// e "restaurar o original" é editar. Sem dono: o site é um só.
	{"site", "Site (textos, fotos e vídeos)", "Site", verEditar, false, 70},
}

func catalogoDeRecursos(ctx context.Context, tx pgx.Tx, _ *estado) (contagem, error) {
	// `actions` chega como texto separado por vírgula e vira array no SQL:
	// Postgres não tem array irregular de duas dimensões, então mandar
	// `text[][]` não é opção.
	const q = `
		INSERT INTO resources (code, label, group_label, actions, supports_own, sort_order)
		SELECT r.code, r.rotulo, r.grupo, string_to_array(r.acoes, ','), r.own, r.ordem
		  FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::bool[], $6::int[])
		       AS r(code, rotulo, grupo, acoes, own, ordem)
		ON CONFLICT (code) DO UPDATE
		   SET label        = EXCLUDED.label,
		       group_label  = EXCLUDED.group_label,
		       actions      = EXCLUDED.actions,
		       supports_own = EXCLUDED.supports_own,
		       sort_order   = EXCLUDED.sort_order
		 WHERE (resources.label, resources.group_label, resources.actions,
		        resources.supports_own, resources.sort_order)
		       IS DISTINCT FROM
		       (EXCLUDED.label, EXCLUDED.group_label, EXCLUDED.actions,
		        EXCLUDED.supports_own, EXCLUDED.sort_order)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q,
		coluna(catalogoSeed, func(r recurso) string { return r.codigo }),
		coluna(catalogoSeed, func(r recurso) string { return r.rotulo }),
		coluna(catalogoSeed, func(r recurso) string { return r.grupo }),
		coluna(catalogoSeed, func(r recurso) string { return strings.Join(r.acoes, ",") }),
		coluna(catalogoSeed, func(r recurso) bool { return r.suportaOwn }),
		coluna(catalogoSeed, func(r recurso) int32 { return r.ordem }),
	)
	c.Previstas = len(catalogoSeed)
	return c, err
}

// perfil é um papel. `sistema` marca o que a API se recusa a apagar ou renomear.
type perfil struct {
	codigo  string
	nome    string
	sistema bool
}

// Só `admin` é de sistema: perder o perfil que administra o acesso é perder o
// acesso à instalação inteira. `usuario` e `corretor` nascem do seed mas são
// editáveis — é a spec §1 pedindo que alterar a matriz mude o comportamento sem
// deploy, e não haveria como cumprir isso com o perfil travado.
var perfisSeed = []perfil{
	{"admin", "Administrador", true},
	{"usuario", "Usuário", false},
	{"corretor", "Corretor", false},
}

func perfis(ctx context.Context, tx pgx.Tx, _ *estado) (contagem, error) {
	const q = `
		INSERT INTO roles (code, name, is_system)
		SELECT p.code, p.nome, p.sistema
		  FROM unnest($1::text[], $2::text[], $3::bool[]) AS p(code, nome, sistema)
		ON CONFLICT (code) DO UPDATE
		   SET name = EXCLUDED.name, is_system = EXCLUDED.is_system
		 WHERE (roles.name, roles.is_system)
		       IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.is_system)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q,
		coluna(perfisSeed, func(p perfil) string { return p.codigo }),
		coluna(perfisSeed, func(p perfil) string { return p.nome }),
		coluna(perfisSeed, func(p perfil) bool { return p.sistema }),
	)
	c.Previstas = len(perfisSeed)
	return c, err
}

// concessao é uma linha da matriz. `acoes` nil significa "todas as ações que o
// recurso oferece" — evita repetir a lista e impede, por construção, conceder
// uma ação que o catálogo não tem.
type concessao struct {
	recurso string
	acoes   []string
	escopo  string
}

const (
	escopoAll = "all"
	escopoOwn = "own"
)

// matrizSeed — docs/spec.md §1 e §7.
//
// `admin` não está aqui: é gerado do catálogo inteiro, para que recurso novo
// não nasça inacessível até alguém lembrar de conceder.
var matrizSeed = map[string][]concessao{
	// Operação e comercial em `all`; financeiro só ver e criar (baixar e
	// estornar é da gestão); nada de usuários, perfis, configurações,
	// integrações e auditoria.
	"usuario": {
		{"dashboard", nil, escopoAll},
		{"reports", nil, escopoAll},
		{"reservations", nil, escopoAll},
		{"calendar", nil, escopoAll},
		{"agenda", nil, escopoAll},
		{"inventory", nil, escopoAll},
		{"channels", nil, escopoAll},
		{"quotes", nil, escopoAll},
		{"contacts", nil, escopoAll},
		{"brokers", nil, escopoAll},
		{"crm.pipelines", nil, escopoAll},
		{"crm.leads", nil, escopoAll},
		{"crm.opportunities", nil, escopoAll},
		{"crm.activities", nil, escopoAll},
		{"chat", nil, escopoAll},
		{"finance.receivables", leituraEscrita, escopoAll},
		{"finance.payables", leituraEscrita, escopoAll},
		{"finance.commissions", leituraEscrita, escopoAll},

		// O conteúdo do site é da gestão (docs/site-cms.md §1). Corretor e a
		// conta de serviço da vitrine não recebem: quem edita o site fala em
		// nome da casa.
		{"site", nil, escopoAll},
	},

	// Tudo que o corretor toca é `own` — o filtro vira `AND owner_id = $user`
	// no SQL do repositório, nunca peneira em memória.
	"corretor": {
		{"dashboard", somenteLeitura, escopoAll},
		{"reservations", leituraEscrita, escopoOwn},
		{"quotes", leituraEscrita, escopoOwn},
		// `calendar` com as QUATRO ações, e não só ver+criar. A revisão mediu a
		// assimetria: `POST /blocks` (calendar, criar) devolvia 201 e
		// `DELETE /blocks/{id}` (calendar, excluir) devolvia 403 — o corretor
		// bloqueava data e não conseguia desbloquear. Permissão que deixa criar
		// e não deixa desfazer não é restrição, é armadilha: o único conserto
		// vira pedir para um admin, e a operação aprende a não usar a tela.
		//
		// DEPENDE DO FILTRO POR DONO: `excluir` em `own` só é seguro porque
		// `stay_blocks.owner_id` existe (20260826120000) e o repositório aplica
		// `AND owner_id = $usuario` em `LiberarBloqueio`. Sem esse filtro no
		// SQL, `own` degrada para `all` e esta linha passa a permitir apagar o
		// bloqueio alheio — mais grave que a assimetria que ela corrige.
		{"calendar", tudo, escopoOwn},
		{"agenda", leituraEscrita, escopoOwn},

		// `contacts` em `all` é a exceção incômoda: a tabela não tem coluna de
		// dono, e sem poder criar o contato o corretor não consegue registrar o
		// próprio lead. Fica em ver+criar (nunca editar o contato alheio) e
		// entra na lista de revisão — quando `contacts` ganhar dono, vira `own`.
		{"contacts", leituraEscrita, escopoAll},

		// Ver o funil é ver a configuração das etapas, não os cards dos outros:
		// sem isto o kanban do corretor não sabe desenhar as colunas.
		{"crm.pipelines", somenteLeitura, escopoAll},
		{"crm.leads", semExcluir, escopoOwn},
		{"crm.opportunities", semExcluir, escopoOwn},
		{"crm.activities", semExcluir, escopoOwn},
		{"chat", nil, escopoOwn},

		// O próprio cadastro e as próprias comissões. Financeiro global,
		// inventário, canais e configurações ficam de fora (spec §11).
		{"brokers", somenteLeitura, escopoOwn},
		{"finance.commissions", somenteLeitura, escopoOwn},
	},
}

// linhaDaMatriz é uma permissão já resolvida — perfil, recurso, ação e escopo —
// pronta para virar linha de role_permissions.
type linhaDaMatriz struct {
	perfil, recurso, acao, escopo string
}

// montarMatriz expande a matriz declarada acima em linhas e valida a coerência
// antes de qualquer I/O: é função pura de propósito, para o teste conferir a
// matriz sem precisar de banco.
func montarMatriz() ([]linhaDaMatriz, error) {
	var linhas []linhaDaMatriz

	// admin recebe o catálogo inteiro em `all` — gerado, e não digitado, para
	// que recurso novo não nasça inacessível até alguém lembrar de conceder.
	todas := make([]concessao, 0, len(catalogoSeed))
	for _, r := range catalogoSeed {
		todas = append(todas, concessao{r.codigo, nil, escopoAll})
	}
	l, err := expandirConcessoes("admin", todas)
	if err != nil {
		return nil, err
	}
	linhas = append(linhas, l...)

	// Ordem fixa na iteração: percorrer o mapa direto tornaria o log e a
	// conferência do seed diferentes a cada execução.
	for _, codigo := range []string{"usuario", "corretor"} {
		l, err := expandirConcessoes(codigo, matrizSeed[codigo])
		if err != nil {
			return nil, err
		}
		linhas = append(linhas, l...)
	}
	return linhas, nil
}

// expandirConcessoes resolve as concessões de um perfil em linhas e recusa,
// antes de qualquer I/O, recurso fora do catálogo, ação que o recurso não
// oferece e escopo `own` em recurso sem dono. É a mesma régua para a matriz
// dos perfis de gente (acima) e para o perfil de serviço da vitrine
// (vitrine.go).
func expandirConcessoes(codigoPerfil string, cs []concessao) ([]linhaDaMatriz, error) {
	catalogo := make(map[string]recurso, len(catalogoSeed))
	for _, r := range catalogoSeed {
		catalogo[r.codigo] = r
	}

	var linhas []linhaDaMatriz
	for _, c := range cs {
		r, ok := catalogo[c.recurso]
		if !ok {
			return nil, fmt.Errorf("perfil %q concede o recurso %q, que não está no catálogo", codigoPerfil, c.recurso)
		}
		if c.escopo == escopoOwn && !r.suportaOwn {
			// Escopo `own` num recurso sem dono é permissão que o SQL não
			// consegue honrar: viraria filtro vazio ou filtro ignorado, e
			// nos dois casos a tela mente sobre o que o usuário vê.
			return nil, fmt.Errorf("perfil %q pede escopo own em %q, que não tem dono", codigoPerfil, c.recurso)
		}
		acoes := c.acoes
		if acoes == nil {
			acoes = r.acoes
		}
		for _, a := range acoes {
			if !contem(r.acoes, a) {
				return nil, fmt.Errorf("perfil %q concede %q em %q, ação que o recurso não oferece", codigoPerfil, a, c.recurso)
			}
			linhas = append(linhas, linhaDaMatriz{codigoPerfil, c.recurso, a, c.escopo})
		}
	}
	return linhas, nil
}

func permissoes(ctx context.Context, tx pgx.Tx, _ *estado) (contagem, error) {
	linhas, err := montarMatriz()
	if err != nil {
		return contagem{}, err
	}

	// Sem DELETE do que não está na matriz: a permissão que a gestão concedeu a
	// mais na tela é decisão dela, e o seed não é dono do perfil depois que ele
	// nasce. O DO UPDATE só corrige o escopo que divergiu.
	const q = `
		INSERT INTO role_permissions (role_id, resource_code, action, scope)
		SELECT ro.id, p.recurso, p.acao, p.escopo
		  FROM unnest($1::text[], $2::text[], $3::text[], $4::text[])
		       AS p(perfil, recurso, acao, escopo)
		  JOIN roles ro ON ro.code = p.perfil
		ON CONFLICT (role_id, resource_code, action) DO UPDATE
		   SET scope = EXCLUDED.scope
		 WHERE role_permissions.scope IS DISTINCT FROM EXCLUDED.scope
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q,
		coluna(linhas, func(l linhaDaMatriz) string { return l.perfil }),
		coluna(linhas, func(l linhaDaMatriz) string { return l.recurso }),
		coluna(linhas, func(l linhaDaMatriz) string { return l.acao }),
		coluna(linhas, func(l linhaDaMatriz) string { return l.escopo }),
	)
	c.Previstas = len(linhas)
	return c, err
}

func contem(lista []string, v string) bool {
	for _, item := range lista {
		if item == v {
			return true
		}
	}
	return false
}

// usuarioSeed é uma conta de desenvolvimento. A senha é a mesma para os três e
// vive só nesta constante — nunca em log, nunca em resposta de API.
type usuarioSeed struct {
	nome   string
	email  string
	perfil string
}

const senhaDeDesenvolvimento = "whv@2026"

var usuariosSeed = []usuarioSeed{
	{"Admin", "admin@wh.local", "admin"},
	{"Gestão Comercial", "gestao@wh.local", "usuario"},
	{"Corretor Demo", "corretor@wh.local", "corretor"},
}

// contasDeDesenvolvimentoLiberadas é a trava das contas de senha conhecida, num
// lugar só porque duas etapas dependem dela: as contas (aqui) e o cadastro
// comercial do corretor de desenvolvimento (corretores.go). Se cada uma lesse a
// variável por conta própria, um ajuste numa deixaria a outra criando cadastro
// de corretor para uma conta que não existe.
func contasDeDesenvolvimentoLiberadas(st *estado) bool {
	return !st.producao || os.Getenv("SEED_DEV_USERS") == "true"
}

func usuariosDeDesenvolvimento(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	var c contagem
	c.Previstas = len(usuariosSeed)

	// Conta com senha publicada num repositório é porta dos fundos, não
	// conveniência. Em produção o bloco só roda se alguém pedir explicitamente
	// — e o aviso fica no log para quem pediu não esquecer de trocar depois.
	if !contasDeDesenvolvimentoLiberadas(st) {
		slog.Warn("usuários de desenvolvimento ignorados em produção",
			"motivo", "senha conhecida não entra em produção sem pedido explícito",
			"como_forcar", "SEED_DEV_USERS=true")
		c.Previstas = 0
		return c, nil
	}

	emails := coluna(usuariosSeed, func(u usuarioSeed) string { return strings.ToLower(u.email) })

	// Quem já existe não é tocado: reescrever o hash apagaria a senha que o dev
	// trocou, e recalcular argon2id (64 MiB × 3) a cada execução é caro à toa.
	// A consulta usa lower(email) porque é assim que o índice único parcial de
	// users está definido — comparar com sensibilidade a caixa deixaria passar
	// um duplicado que o banco depois recusa com 23505.
	existentes := map[string]bool{}
	linhas, err := tx.Query(ctx, `SELECT lower(email) FROM users WHERE lower(email) = ANY($1::text[])`, emails)
	if err != nil {
		return c, fmt.Errorf("consultando usuários existentes: %w", err)
	}
	for linhas.Next() {
		var e string
		if err := linhas.Scan(&e); err != nil {
			linhas.Close()
			return c, err
		}
		existentes[e] = true
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		return c, err
	}

	var nomes, novos, papeis, hashes []string
	for _, u := range usuariosSeed {
		if existentes[strings.ToLower(u.email)] {
			continue
		}
		hash, err := auth.Hash(senhaDeDesenvolvimento)
		if err != nil {
			return c, fmt.Errorf("gerando hash de %s: %w", u.email, err)
		}
		nomes = append(nomes, u.nome)
		novos = append(novos, u.email)
		papeis = append(papeis, u.perfil)
		hashes = append(hashes, hash)
	}
	if len(novos) == 0 {
		return c, nil
	}

	// DO NOTHING e não DO UPDATE: em concorrência (duas execuções do seed ao
	// mesmo tempo) o perdedor não pode sobrescrever a senha do vencedor.
	const q = `
		INSERT INTO users (property_id, role_id, name, email, password_hash)
		SELECT $1, ro.id, u.nome, u.email, u.hash
		  FROM unnest($2::text[], $3::text[], $4::text[], $5::text[]) AS u(nome, email, perfil, hash)
		  JOIN roles ro ON ro.code = u.perfil
		ON CONFLICT (email) DO NOTHING
		RETURNING (xmax = 0)`

	cu, err := upsert(ctx, tx, q, st.propriedadeID, nomes, novos, papeis, hashes)
	c.Criadas = cu.Criadas
	c.Atualizadas = cu.Atualizadas
	return c, err
}
