package main

import (
	"strings"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// O seed é a única fonte da matriz inicial; se ela sair incoerente, o erro só
// apareceria com o banco na frente. Estes testes não tocam em banco: conferem a
// declaração antes de qualquer I/O.

func TestMatrizEhCoerenteComOCatalogo(t *testing.T) {
	// montarMatriz recusa recurso fora do catálogo, ação que o recurso não
	// oferece e escopo `own` em recurso sem dono.
	if _, err := montarMatriz(); err != nil {
		t.Fatalf("matriz do seed inválida: %v", err)
	}
}

func TestAdminRecebeOCatalogoInteiro(t *testing.T) {
	linhas, err := montarMatriz()
	if err != nil {
		t.Fatal(err)
	}

	concedido := map[string]bool{}
	for _, l := range linhas {
		if l.perfil != "admin" {
			continue
		}
		if l.escopo != escopoAll {
			t.Fatalf("admin com escopo %q em %s:%s — administrador enxerga tudo", l.escopo, l.recurso, l.acao)
		}
		concedido[l.recurso+":"+l.acao] = true
	}

	for _, r := range catalogoSeed {
		for _, a := range r.acoes {
			if !concedido[r.codigo+":"+a] {
				t.Errorf("admin não recebeu %s:%s — recurso novo nasceria inacessível", r.codigo, a)
			}
		}
	}
}

// Sem `roles:editar` ninguém conserta o acesso de mais ninguém: é o que o
// `ContarAdministradoresAtivos` do módulo de usuários usa para impedir que o
// último administrador se desative.
func TestAdminPodeEditarPerfis(t *testing.T) {
	linhas, err := montarMatriz()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range linhas {
		if l.perfil == "admin" && l.recurso == auth.RecursoPerfis && l.acao == editar {
			return
		}
	}
	t.Fatal("admin sem roles:editar — a instalação ficaria sem quem administra o acesso")
}

// A spec §1 e §11 são explícitas: corretor nunca vê financeiro global,
// configurações nem dados dos outros corretores.
func TestCorretorNaoAlcancaFinanceiroGlobalNemConfiguracoes(t *testing.T) {
	proibidos := []string{
		"finance.receivables", "finance.payables",
		"inventory", "inventory.goods", "channels",
		"maintenance", // a ordem bloqueia a unidade sem passar por `calendar` (spec §12)
		auth.RecursoUsuarios, auth.RecursoPerfis, "settings", "integrations", "audit",
		"site", // o conteúdo do site fala em nome da casa (docs/site-cms.md)
	}

	linhas, err := montarMatriz()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range linhas {
		if l.perfil != "corretor" {
			continue
		}
		for _, p := range proibidos {
			if l.recurso == p {
				t.Errorf("corretor recebeu %s:%s, que a spec proíbe", l.recurso, l.acao)
			}
		}
	}
}

// O que tem dono é `own`. A exceção é `contacts`, que ainda não tem coluna de
// dono — está anotada no seed e some quando a coluna existir.
func TestCorretorOperaSobreOProprioDado(t *testing.T) {
	catalogo := map[string]recurso{}
	for _, r := range catalogoSeed {
		catalogo[r.codigo] = r
	}

	linhas, err := montarMatriz()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range linhas {
		if l.perfil != "corretor" || l.escopo == escopoOwn {
			continue
		}
		switch l.recurso {
		case "dashboard", "crm.pipelines", "contacts":
			// Painel próprio, desenho do funil e cadastro do contato do lead:
			// nenhum deles tem dono na linha.
		default:
			if catalogo[l.recurso].suportaOwn {
				t.Errorf("corretor com escopo all em %s, que tem dono", l.recurso)
			}
		}
	}
}

// docs/site-cms.md §1: recurso `site` com ver+editar, sem `own`, concedido à
// gestão (e ao admin, pelo catálogo inteiro) — e a ninguém mais.
func TestSiteEhDaGestao(t *testing.T) {
	var r *recurso
	for i := range catalogoSeed {
		if catalogoSeed[i].codigo == "site" {
			r = &catalogoSeed[i]
		}
	}
	if r == nil {
		t.Fatal("recurso site fora do catálogo")
	}
	if strings.Join(r.acoes, ",") != "ver,editar" || r.suportaOwn {
		t.Fatalf("site com ações %v e own=%v; o contrato é ver+editar, sem own", r.acoes, r.suportaOwn)
	}

	linhas, err := montarMatriz()
	if err != nil {
		t.Fatal(err)
	}
	vitrine, err := matrizDaVitrine()
	if err != nil {
		t.Fatal(err)
	}
	concedido := map[string]bool{}
	for _, l := range append(linhas, vitrine...) {
		if l.recurso != "site" {
			continue
		}
		if l.escopo != escopoAll {
			t.Errorf("%s com escopo %q em site:%s", l.perfil, l.escopo, l.acao)
		}
		concedido[l.perfil+":"+l.acao] = true
	}
	for _, perfil := range []string{"admin", "usuario"} {
		for _, a := range []string{ver, editar} {
			if !concedido[perfil+":"+a] {
				t.Errorf("%s sem site:%s", perfil, a)
			}
		}
	}
	for chave := range concedido {
		if p := strings.SplitN(chave, ":", 2)[0]; p != "admin" && p != "usuario" {
			t.Errorf("%s recebeu site — só a gestão edita o site", chave)
		}
	}
}

// Os BENS por ambiente (20261007170000) são um recurso separado do cadastro
// comercial, e este teste é o que impede a fusão de voltar.
//
// Fundir os dois faria `inventory:editar` significar duas coisas: quem corrige
// a contagem de taças passaria a poder apagar um produto que a casa vende, e
// quem cadastra unidade passaria a mexer na conferência. Quem conta é quem
// limpa, pelo celular — a distância entre as duas telas é um toque.
func TestBensSaoRecursoSeparadoDoCadastro(t *testing.T) {
	const cadastro, bens = "inventory", "inventory.goods"

	catalogo := map[string]recurso{}
	for _, r := range catalogoSeed {
		catalogo[r.codigo] = r
	}

	r, ok := catalogo[bens]
	if !ok {
		t.Fatalf("recurso %q fora do catálogo — a conferência de bens não teria como ser autorizada", bens)
	}
	if strings.Join(r.acoes, ",") != strings.Join(tudo, ",") {
		t.Errorf("%s com ações %v; o contrato é as quatro", bens, r.acoes)
	}
	if r.suportaOwn {
		t.Errorf("%s com suportaOwn=true; cômodo não tem dono no sentido do RBAC", bens)
	}
	if r.grupo != catalogo[cadastro].grupo {
		t.Errorf("%s no grupo %q e %s no grupo %q — os dois são Operação", bens, r.grupo, cadastro, catalogo[cadastro].grupo)
	}

	// O rótulo de `inventory` dizia "Inventário e enxoval" e mentia: não há uma
	// peça de enxoval nas rotas que ele protege (propriedade, produtos,
	// unidades, composição). O enxoval agora tem recurso próprio, e o rótulo
	// errado é o tipo de coisa que volta num copiar-colar.
	if strings.Contains(strings.ToLower(catalogo[cadastro].rotulo), "enxoval") {
		t.Errorf("rótulo de %s promete enxoval (%q), que mora em %s — a grade de perfis mentiria sobre o que a célula abre",
			cadastro, catalogo[cadastro].rotulo, bens)
	}

	// Quem recebe: admin (pelo catálogo inteiro) e a operação. Ninguém mais —
	// nem o corretor, nem a conta de serviço do site, que não têm o que fazer
	// com bens.
	linhas, err := montarMatriz()
	if err != nil {
		t.Fatal(err)
	}
	vitrine, err := matrizDaVitrine()
	if err != nil {
		t.Fatal(err)
	}

	concedido := map[string]bool{}
	for _, l := range append(linhas, vitrine...) {
		if l.recurso != bens {
			continue
		}
		if l.escopo != escopoAll {
			t.Errorf("%s com escopo %q em %s:%s — o recurso não tem dono", l.perfil, l.escopo, bens, l.acao)
		}
		concedido[l.perfil+":"+l.acao] = true
	}
	for _, perfil := range []string{"admin", "usuario"} {
		for _, a := range tudo {
			if !concedido[perfil+":"+a] {
				t.Errorf("%s sem %s:%s — a operação não conseguiria conferir a casa", perfil, bens, a)
			}
		}
	}
	for chave := range concedido {
		if p := strings.SplitN(chave, ":", 2)[0]; p != "admin" && p != "usuario" {
			t.Errorf("%s recebeu %s — só a gestão e a operação contam bens", chave, bens)
		}
	}
}

// As ordens de manutenção (spec §12, 20261009100000) são recurso próprio, com
// as quatro ações, sem dono, e só da gestão e da operação.
//
// O que torna a concessão delicada: a ordem BLOQUEIA a unidade dela no
// calendário sem pedir `calendar:*` (contrato, tag Manutenção). Quem recebe
// `maintenance` recebe, na prática, o poder de tirar uma unidade da venda.
// Por isso o corretor — que em `calendar` só mexe no que é dele — e a conta de
// serviço do site — cuja matriz vale para a internet inteira — ficam de fora.
func TestManutencaoEhDaOperacao(t *testing.T) {
	const manutencao = "maintenance"

	catalogo := map[string]recurso{}
	for _, r := range catalogoSeed {
		catalogo[r.codigo] = r
	}
	r, ok := catalogo[manutencao]
	if !ok {
		t.Fatalf("recurso %q fora do catálogo — as rotas /maintenance-orders não teriam como ser autorizadas", manutencao)
	}
	if strings.Join(r.acoes, ",") != strings.Join(tudo, ",") {
		t.Errorf("%s com ações %v; o contrato é as quatro (excluir = cancelar a ordem)", manutencao, r.acoes)
	}
	if r.suportaOwn {
		t.Errorf("%s com suportaOwn=true; a ordem é da casa, não de quem a abriu", manutencao)
	}
	if r.grupo != catalogo["inventory.goods"].grupo {
		t.Errorf("%s no grupo %q; é Operação, ao lado dos bens", manutencao, r.grupo)
	}

	linhas, err := montarMatriz()
	if err != nil {
		t.Fatal(err)
	}
	vitrine, err := matrizDaVitrine()
	if err != nil {
		t.Fatal(err)
	}
	concedido := map[string]bool{}
	for _, l := range append(linhas, vitrine...) {
		if l.recurso != manutencao {
			continue
		}
		if l.escopo != escopoAll {
			t.Errorf("%s com escopo %q em %s:%s — o recurso não tem dono", l.perfil, l.escopo, manutencao, l.acao)
		}
		concedido[l.perfil+":"+l.acao] = true
	}
	for _, perfil := range []string{"admin", "usuario"} {
		for _, a := range tudo {
			if !concedido[perfil+":"+a] {
				t.Errorf("%s sem %s:%s — a operação não conseguiria abrir, acompanhar ou cancelar a ordem", perfil, manutencao, a)
			}
		}
	}
	for chave := range concedido {
		if p := strings.SplitN(chave, ":", 2)[0]; p != "admin" && p != "usuario" {
			t.Errorf("%s recebeu %s — a ordem bloqueia a unidade no calendário, e só a gestão e a operação a abrem", chave, manutencao)
		}
	}

	// O formulário da ordem escolhe cômodo e bem pelas rotas de `inventory.goods`
	// (contrato, tag Manutenção). Perfil do seed com `maintenance:criar` e sem
	// `inventory.goods:ver` teria um formulário que não carrega.
	temBens := map[string]bool{}
	for _, l := range linhas {
		if l.recurso == "inventory.goods" && l.acao == ver {
			temBens[l.perfil] = true
		}
	}
	for chave := range concedido {
		if p := strings.SplitN(chave, ":", 2)[0]; !temBens[p] {
			t.Errorf("%s recebeu %s sem inventory.goods:ver — o formulário não acharia cômodo nem bem", p, manutencao)
		}
	}
}

func TestCatalogoNaoTemCodigoRepetidoNemAcaoInvalida(t *testing.T) {
	validas := map[string]bool{ver: true, criar: true, editar: true, excluir: true}
	vistos := map[string]bool{}
	ordens := map[int32]string{}

	for _, r := range catalogoSeed {
		if vistos[r.codigo] {
			t.Errorf("recurso %q repetido no catálogo", r.codigo)
		}
		vistos[r.codigo] = true

		// `sort_order` repetido deixa a grade de perfis sem ordem
		// determinística: os dois recursos trocam de lugar entre duas aberturas
		// da tela, e quem revisa permissão não acha a linha onde a deixou.
		// Acrescentar recurso no meio de um grupo (o caso de `inventory.goods`)
		// é exatamente quando a colisão acontece.
		if outro, ok := ordens[r.ordem]; ok {
			t.Errorf("recursos %q e %q com a mesma ordem %d — a grade perderia ordem estável", outro, r.codigo, r.ordem)
		}
		ordens[r.ordem] = r.codigo

		if len(r.acoes) == 0 {
			t.Errorf("recurso %q sem nenhuma ação — linha invisível na grade", r.codigo)
		}
		for _, a := range r.acoes {
			if !validas[a] {
				t.Errorf("recurso %q oferece a ação %q, fora do CHECK de role_permissions", r.codigo, a)
			}
		}
		if strings.TrimSpace(r.rotulo) == "" || strings.TrimSpace(r.grupo) == "" {
			t.Errorf("recurso %q sem rótulo ou grupo — a tela de perfis não sabe onde desenhá-lo", r.codigo)
		}
	}

	// O código Go cita estes dois; sem eles no catálogo, PUT /roles/{id}/permissions
	// recusaria por chave estrangeira o que a própria API oferece.
	for _, obrigatorio := range []string{auth.RecursoUsuarios, auth.RecursoPerfis} {
		if !vistos[obrigatorio] {
			t.Errorf("recurso %q citado em internal/auth/recursos.go está fora do catálogo", obrigatorio)
		}
	}
}

// A senha de desenvolvimento precisa passar pelo mesmo verificador do login;
// hash gerado por outro caminho entraria no banco e ninguém conseguiria entrar.
func TestSenhaDeDesenvolvimentoPassaNoVerificadorDoLogin(t *testing.T) {
	hash, err := auth.Hash(senhaDeDesenvolvimento)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash fora do formato argon2id: %q", hash[:min(12, len(hash))])
	}
	ok, err := auth.Verify(hash, senhaDeDesenvolvimento)
	if err != nil || !ok {
		t.Fatalf("senha do seed não verifica: ok=%v err=%v", ok, err)
	}
}

// As tarifas são conferidas em REAIS, nos dois catálogos. O teste existe para o
// dia em que alguém copiar o número direto para um campo `_cents` e vender a
// diária por R$ 8,50.
func TestTarifasEntramEmCentavos(t *testing.T) {
	for _, cat := range []*catalogo{&catalogoDeTeste, &catalogoDaCasa} {
		for _, tar := range cat.tarifas {
			for i, v := range tar.valores {
				if got := reais(v); got != v*100 {
					t.Fatalf("%s %s/%s: %d reais viraram %d centavos", cat.nome, tar.produto, ordemDosTipos[i], v, got)
				}
			}
		}
	}
	if len(ordemDosTipos) != len(tarifasTeste[0].valores) {
		t.Fatal("a matriz de tarifas não tem uma coluna por tipo de data")
	}
}
