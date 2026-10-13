//go:build integration

package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// ─────────────────────────── Cenário com ids reais ──────────────────────────

// qmCenario é uma unidade com uma ordem ABERTA e bloqueio agendado — o alvo
// real de cada `{id}` das 10 operações. Com id sorteado, um 404 do handler
// esconderia a falta de checagem de permissão; com id real, só o RBAC separa
// o 403 do 200.
type qmCenario struct {
	Prop    uuid.UUID
	Unidade uuid.UUID
	Codigo  string
	Ordem   uuid.UUID
	D       calendar.Date
}

func (a *ambiente) qmMontarCenario(t *testing.T, f *qbFaxina, token string) qmCenario {
	t.Helper()
	c := qmCenario{Prop: a.qbPropriedadePadrao(t)}
	c.D = a.qmHoje(t, c.Prop)
	c.Unidade, c.Codigo = f.qbUnidade(t, c.Prop, "")
	o := a.qmAbrir(t, token, map[string]any{
		"unit_id": c.Unidade, "title": "Ar da suíte não gela", "priority": "alta",
		"block": qmPeriodo(c.D.AddDays(30), c.D.AddDays(33)),
	})
	c.Ordem = o.ID
	return c
}

func (c qmCenario) caminho(op qbOperacao) string {
	p := strings.Replace(op.Path, "{id}", c.Ordem.String(), 1)
	if strings.Contains(p, "{") {
		panic("parâmetro sem alvo no cenário: " + op.Path)
	}
	return p
}

// corpo devolve um corpo VÁLIDO: se a permissão faltar no servidor, a escrita
// teria efeito — e é isso que o teste depois confere no banco.
func (c qmCenario) corpo(op qbOperacao) any {
	switch op.chave() {
	case "POST /maintenance-orders":
		return map[string]any{"unit_id": c.Unidade, "title": "Invasão " + qbSufixo(),
			"block": qmPeriodo(c.D.AddDays(60), c.D.AddDays(61))}
	case "PUT /maintenance-orders/{id}":
		return map[string]any{"title": "Renomeada", "priority": "baixa", "cost_cents": 777}
	case "PATCH /maintenance-orders/{id}":
		return map[string]any{"title": "Renomeada", "cost_cents": 777}
	case "POST /maintenance-orders/{id}/complete":
		return map[string]any{"cost_cents": 777}
	case "PUT /maintenance-orders/{id}/block":
		return qmPeriodo(c.D.AddDays(30), c.D.AddDays(40))
	}
	return nil
}

func (a *ambiente) qmExecutar(t *testing.T, c qmCenario, op qbOperacao, token string) resposta {
	t.Helper()
	return a.chamar(t, op.Metodo, c.caminho(op), token, c.corpo(op))
}

// qmEstado é o que uma escrita recusada não pode ter mudado.
type qmEstado struct {
	Status, Titulo, Prioridade string
	Custo                      *int64
	BloqueioStatus             string
	BloqueioPeriodo            string
	OrdensNaUnidade            int
	BloqueiosNaUnidade         int
}

func (a *ambiente) qmLerEstado(t *testing.T, c qmCenario) qmEstado {
	t.Helper()
	var e qmEstado
	if err := a.pool.QueryRow(a.ctx, `
		SELECT mo.status, mo.title, mo.priority, mo.cost_cents, sb.status, sb.period::text
		  FROM maintenance_orders mo JOIN stay_blocks sb ON sb.id = mo.stay_block_id
		 WHERE mo.id = $1`, c.Ordem).Scan(&e.Status, &e.Titulo, &e.Prioridade, &e.Custo, &e.BloqueioStatus, &e.BloqueioPeriodo); err != nil {
		t.Fatalf("lendo a ordem do cenário: %v", err)
	}
	e.OrdensNaUnidade = a.qmContar(t, `SELECT count(*) FROM maintenance_orders WHERE unit_id = $1`, c.Unidade)
	e.BloqueiosNaUnidade = a.qmContar(t, `SELECT count(*) FROM stay_blocks WHERE unit_id = $1`, c.Unidade)
	return e
}

func (e qmEstado) igual(o qmEstado) bool {
	custo := func(v *int64) int64 {
		if v == nil {
			return -1
		}
		return *v
	}
	return e.Status == o.Status && e.Titulo == o.Titulo && e.Prioridade == o.Prioridade && custo(e.Custo) == custo(o.Custo) &&
		e.BloqueioStatus == o.BloqueioStatus && e.BloqueioPeriodo == o.BloqueioPeriodo &&
		e.OrdensNaUnidade == o.OrdensNaUnidade && e.BloqueiosNaUnidade == o.BloqueiosNaUnidade
}

// ─────────────────────────── 1. RBAC pela matriz do seed ────────────────────

// Em linguagem de negócio: o corretor vende estadia; ele não abre ordem de
// conserto, e sobretudo não tira uma unidade do ar pela porta dos fundos da
// manutenção. Logado com o perfil REAL do seed, ele recebe 403 nas 10
// operações — o 403 diz a permissão que faltou, a mesma do contrato — e nada
// muda: nem a ordem, nem o calendário.
func TestManutencaoQACorretorDoSeedRecebe403NasDezOperacoes(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	corretor := a.criarUsuario(t, "manut-corretor", a.qbPerfilDoSeed(t, "corretor"))
	f := a.qbFaxina(t, admin, corretor)
	a.qmFaxina(t, f)
	c := a.qmMontarCenario(t, f, admin.Token)
	antes := a.qmLerEstado(t, c)

	for _, op := range qmOperacoesDeManutencao(t) {
		t.Run(op.chave(), func(t *testing.T) {
			e := qbErro(t, a.qmExecutar(t, c, op, corretor.Token), http.StatusForbidden, "FORBIDDEN", "corretor em "+op.chave())
			if e.Details["resource"] != op.Recurso || e.Details["action"] != op.Acao {
				t.Errorf("o 403 diz que faltou %v:%v; o x-rbac do contrato é %s:%s",
					e.Details["resource"], e.Details["action"], op.Recurso, op.Acao)
			}
		})
	}
	if depois := a.qmLerEstado(t, c); !depois.igual(antes) {
		t.Fatalf("o corretor recebeu 403 e mesmo assim algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// A conta de serviço da vitrine (o site) também não alcança nenhuma rota de
// manutenção: ela não opera a casa.
func TestManutencaoQAContaDaVitrineRecebe403NasDezOperacoes(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	vitrine := a.criarUsuario(t, "manut-vitrine", a.qbPerfilDoSeed(t, "vitrine"))
	f := a.qbFaxina(t, admin, vitrine)
	a.qmFaxina(t, f)
	c := a.qmMontarCenario(t, f, admin.Token)
	antes := a.qmLerEstado(t, c)

	for _, op := range qmOperacoesDeManutencao(t) {
		t.Run(op.chave(), func(t *testing.T) {
			qbErro(t, a.qmExecutar(t, c, op, vitrine.Token), http.StatusForbidden, "FORBIDDEN", "vitrine em "+op.chave())
		})
	}
	if depois := a.qmLerEstado(t, c); !depois.igual(antes) {
		t.Fatalf("a vitrine recebeu 403 e mesmo assim algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Sem sessão, nenhuma das 10 responde — nem com token inválido.
func TestManutencaoQASemSessaoAsDezOperacoesDao401(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qbFaxina(t, admin)
	a.qmFaxina(t, f)
	c := a.qmMontarCenario(t, f, admin.Token)
	antes := a.qmLerEstado(t, c)

	for _, op := range qmOperacoesDeManutencao(t) {
		t.Run(op.chave(), func(t *testing.T) {
			qbErro(t, a.qmExecutar(t, c, op, ""), http.StatusUnauthorized, "UNAUTHORIZED", "sem token em "+op.chave())
			qbErro(t, a.qmExecutar(t, c, op, "nao.e.um.jwt"), http.StatusUnauthorized, "UNAUTHORIZED", "token inválido em "+op.chave())
		})
	}
	if depois := a.qmLerEstado(t, c); !depois.igual(antes) {
		t.Fatalf("chamadas sem sessão mudaram o estado:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Em linguagem de negócio: `usuario` e `admin` são quem opera a casa. Com o
// perfil REAL do seed, cada um percorre as 10 operações e recebe o status de
// sucesso que o contrato promete — inclusive o 200 (e não 204) do DELETE, que
// devolve a ordem cancelada.
func TestManutencaoQAUsuarioEAdminDoSeedPercorremAsDezOperacoes(t *testing.T) {
	for _, perfil := range []string{"usuario", "admin"} {
		t.Run(perfil, func(t *testing.T) {
			a := subirAPI(t)
			u := a.criarUsuario(t, "manut-"+perfil, a.qbPerfilDoSeed(t, perfil))
			f := a.qbFaxina(t, u)
			a.qmFaxina(t, f)
			prop := a.qbPropriedadePadrao(t)
			d := a.qmHoje(t, prop)
			unidade, _ := f.qbUnidade(t, prop, "")
			tk := u.Token

			cobertas := map[string]bool{}
			passo := func(chave string, r resposta, esperado int) resposta {
				t.Helper()
				cobertas[chave] = true
				if r.Status != esperado {
					t.Fatalf("%s: status %d, o contrato promete %d — %s", chave, r.Status, esperado, r.Corpo)
				}
				return r
			}
			o := qbDado[qmOrdem](t, passo("POST /maintenance-orders", a.chamar(t, http.MethodPost, "/maintenance-orders", tk,
				map[string]any{"unit_id": unidade, "title": "Pintura", "block": qmPeriodo(d.AddDays(10), d.AddDays(13))}), 201), 201, "abrindo")
			base := "/maintenance-orders/" + o.ID.String()
			passo("GET /maintenance-orders", a.chamar(t, http.MethodGet, "/maintenance-orders?unit_id="+unidade.String(), tk, nil), 200)
			passo("GET /maintenance-orders/{id}", a.chamar(t, http.MethodGet, base, tk, nil), 200)
			passo("PUT /maintenance-orders/{id}", a.chamar(t, http.MethodPut, base, tk,
				map[string]any{"title": "Pintura da fachada", "priority": "urgente"}), 200)
			passo("PATCH /maintenance-orders/{id}", a.chamar(t, http.MethodPatch, base, tk, map[string]any{"cost_cents": 150000}), 200)
			passo("PUT /maintenance-orders/{id}/block", a.chamar(t, http.MethodPut, base+"/block", tk, qmPeriodo(d.AddDays(10), d.AddDays(15))), 200)
			passo("DELETE /maintenance-orders/{id}/block", a.chamar(t, http.MethodDelete, base+"/block", tk, nil), 200)
			passo("POST /maintenance-orders/{id}/start", a.chamar(t, http.MethodPost, base+"/start", tk, nil), 200)
			passo("POST /maintenance-orders/{id}/complete", a.chamar(t, http.MethodPost, base+"/complete", tk, map[string]any{"cost_cents": 180000}), 200)

			outra := a.qmAbrir(t, tk, map[string]any{"unit_id": unidade, "title": "Dedetização"})
			passo("DELETE /maintenance-orders/{id}", a.chamar(t, http.MethodDelete, "/maintenance-orders/"+outra.ID.String(), tk, nil), 200)

			for _, op := range qmOperacoesDeManutencao(t) {
				if !cobertas[op.chave()] {
					t.Errorf("%s não foi exercida pela jornada de %s", op.chave(), perfil)
				}
			}
		})
	}
}

// Em linguagem de negócio: a casa quer um perfil "só acompanha" — quem vê a
// lista de consertos no celular sem poder mudar nada. Com só `maintenance:ver`
// ele lê as 2 rotas de leitura e recebe 403 nas 8 escritas, apontando a ação
// que o contrato exige — e nada muda no banco.
func TestManutencaoQAPerfilSoDeVerLeENaoEscreve(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	leitor := a.criarUsuario(t, "manut-leitor", a.criarPerfil(t, "manut_so_ver", []auth.Permissao{
		{Resource: "maintenance", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	}))
	f := a.qbFaxina(t, admin, leitor)
	a.qmFaxina(t, f)
	c := a.qmMontarCenario(t, f, admin.Token)
	antes := a.qmLerEstado(t, c)

	leituras, escritas := 0, 0
	for _, op := range qmOperacoesDeManutencao(t) {
		t.Run(op.chave(), func(t *testing.T) {
			r := a.qmExecutar(t, c, op, leitor.Token)
			if op.Acao == auth.AcaoVer {
				leituras++
				if r.Status != http.StatusOK {
					t.Fatalf("leitura negada a quem tem maintenance:ver: %d — %s", r.Status, r.Corpo)
				}
				return
			}
			escritas++
			e := qbErro(t, r, http.StatusForbidden, "FORBIDDEN", "escrita de quem só vê")
			if e.Details["resource"] != "maintenance" || e.Details["action"] != op.Acao {
				t.Errorf("o 403 aponta %v:%v; o contrato exige maintenance:%s", e.Details["resource"], e.Details["action"], op.Acao)
			}
		})
	}
	if leituras != 2 || escritas != 8 {
		t.Errorf("o contrato tem 2 leituras e 8 escritas na tag Manutenção; contei %d e %d", leituras, escritas)
	}
	if depois := a.qmLerEstado(t, c); !depois.igual(antes) {
		t.Fatalf("quem só vê recebeu 403 e mesmo assim algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Em linguagem de negócio: o contrato promete que conceder `maintenance` é
// conceder bloquear a unidade DA ORDEM, sem `calendar:*`. O encarregado da
// manutenção (só `maintenance`, nada de calendário) bloqueia e solta a unidade
// pela ordem — e a unidade sai MESMO da venda —, mas continua sem alcançar o
// calendário pela rota dele (`POST /blocks`, `DELETE /blocks/{id}`, o mapa).
func TestManutencaoQAEncarregadoSemCalendarioBloqueiaPelaOrdem(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	editor := a.criarUsuario(t, "manut-editor", a.criarPerfil(t, "manut_editor", []auth.Permissao{
		{Resource: "maintenance", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: "maintenance", Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	}))
	criador := a.criarUsuario(t, "manut-criador", a.criarPerfil(t, "manut_criador", []auth.Permissao{
		{Resource: "maintenance", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: "maintenance", Action: auth.AcaoCriar, Scope: auth.EscopoAll},
	}))
	f := a.qbFaxina(t, admin, editor, criador)
	m := a.qmFaxina(t, f)
	prop := a.qbPropriedadePadrao(t)
	d := a.qmHoje(t, prop)
	p := m.qmProdutoVendavel(t, prop)
	hospede := m.qmHospede(t, prop)

	// A ordem nasce sem bloqueio, aberta pelo admin.
	o := a.qmAbrir(t, admin.Token, map[string]any{"unit_id": p.Unidade, "title": "Troca do piso"})
	base := "/maintenance-orders/" + o.ID.String()

	// O encarregado bloqueia pela ordem.
	b := qbDado[qmOrdem](t, a.chamar(t, http.MethodPut, base+"/block", editor.Token, qmPeriodo(d.AddDays(5), d.AddDays(8))),
		http.StatusOK, "PUT /block de quem só tem maintenance:editar").Bloqueio
	if b == nil || b.Status != "confirmed" || b.De != d.AddDays(5).String() || b.Ate != d.AddDays(8).String() {
		t.Fatalf("o bloqueio pedido pelo encarregado não ficou como pedido: %+v", b)
	}
	var origem, status string
	if err := a.pool.QueryRow(a.ctx, `SELECT source, status FROM stay_blocks WHERE id = $1 AND unit_id = $2`, b.ID, p.Unidade).
		Scan(&origem, &status); err != nil || origem != "maintenance" || status != "confirmed" {
		t.Fatalf("a linha de stay_blocks do encarregado: %s %s %v", origem, status, err)
	}
	// E a unidade saiu da venda de verdade: a reserva por cima é recusada.
	r := a.qmReservar(t, admin.Token, p.ID, hospede, d.AddDays(6), d.AddDays(7))
	qbErro(t, r, http.StatusConflict, "DATE_CONFLICT", "vender por cima do bloqueio do encarregado")

	// Pela rota do calendário, ele não alcança nada.
	e := qbErro(t, a.chamar(t, http.MethodPost, "/blocks", editor.Token, map[string]any{
		"unit_ids": []uuid.UUID{p.Unidade}, "from": d.AddDays(20).String(), "to": d.AddDays(21).String(), "source": "maintenance",
	}), http.StatusForbidden, "FORBIDDEN", "POST /blocks de quem não tem calendar")
	if e.Details["resource"] != "calendar" {
		t.Errorf("o 403 de POST /blocks deveria apontar calendar: %v", e.Details["resource"])
	}
	qbErro(t, a.chamar(t, http.MethodDelete, "/blocks/"+b.ID.String(), editor.Token, nil), http.StatusForbidden, "FORBIDDEN",
		"DELETE /blocks de quem não tem calendar")
	qbErro(t, a.chamar(t, http.MethodGet, "/availability/units?from="+d.String()+"&to="+d.AddDays(3).String(), editor.Token, nil),
		http.StatusForbidden, "FORBIDDEN", "mapa de quem não tem calendar")

	// Solta pela ordem, e a data volta à venda.
	s := qbDado[qmOrdem](t, a.chamar(t, http.MethodDelete, base+"/block", editor.Token, nil), http.StatusOK, "DELETE /block do encarregado")
	if s.Bloqueio == nil || s.Bloqueio.Status != "cancelled" || s.Bloqueio.Fase != "liberado" {
		t.Fatalf("soltar o bloqueio agendado o libera inteiro: %+v", s.Bloqueio)
	}
	qbDado[reservaQA](t, a.qmReservar(t, admin.Token, p.ID, hospede, d.AddDays(6), d.AddDays(7)), http.StatusCreated,
		"vender a data que o encarregado soltou")

	// Quem só tem `maintenance:criar` abre a ordem JÁ com bloqueio.
	n := qbDado[qmOrdem](t, a.chamar(t, http.MethodPost, "/maintenance-orders", criador.Token, map[string]any{
		"unit_id": p.Unidade, "title": "Vazamento", "block": qmPeriodo(d.AddDays(12), d.AddDays(14)),
	}), http.StatusCreated, "POST com bloqueio de quem só tem maintenance:criar")
	if n.Bloqueio == nil || n.Bloqueio.Status != "confirmed" {
		t.Fatalf("a ordem nasce com o bloqueio pedido: %+v", n.Bloqueio)
	}
}

// Em linguagem de negócio: quem tem calendário, reservas e inventário de bens
// — mas não `maintenance` — não alcança nenhuma das 10 rotas. O calendário não
// é porta de entrada para a ordem (o contrário do que o encarregado tem).
func TestManutencaoQAPerfilSemMaintenanceNaoAlcancaNada(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "manut-admin", a.qbPerfilDoSeed(t, "admin"))
	var permissoes []auth.Permissao
	for _, recurso := range []string{"calendar", "reservations", "inventory", "inventory.goods"} {
		for _, acao := range auth.AcoesValidas {
			permissoes = append(permissoes, auth.Permissao{Resource: recurso, Action: acao, Scope: auth.EscopoAll})
		}
	}
	outro := a.criarUsuario(t, "manut-sem", a.criarPerfil(t, "manut_sem", permissoes))
	f := a.qbFaxina(t, admin, outro)
	a.qmFaxina(t, f)
	c := a.qmMontarCenario(t, f, admin.Token)
	antes := a.qmLerEstado(t, c)

	// A conta funciona nos recursos dela — o 403 abaixo não é sessão quebrada.
	exigirStatusQB(t, a.chamar(t, http.MethodGet, "/availability/units?from="+c.D.String()+"&to="+c.D.AddDays(1).String(), outro.Token, nil),
		http.StatusOK, "mapa de quem tem calendar")

	for _, op := range qmOperacoesDeManutencao(t) {
		t.Run(op.chave(), func(t *testing.T) {
			e := qbErro(t, a.qmExecutar(t, c, op, outro.Token), http.StatusForbidden, "FORBIDDEN", "sem maintenance em "+op.chave())
			if e.Details["resource"] != "maintenance" {
				t.Errorf("o 403 deveria apontar maintenance; apontou %v", e.Details["resource"])
			}
		})
	}
	if depois := a.qmLerEstado(t, c); !depois.igual(antes) {
		t.Fatalf("quem não tem maintenance recebeu 403 e mesmo assim algo mudou:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// A matriz do seed, lida do banco: admin e usuario com as quatro ações em
// `all`; corretor e vitrine sem nenhuma célula. É a premissa dos testes acima.
func TestManutencaoQAMatrizDoSeedParaMaintenance(t *testing.T) {
	a := subirAPI(t)
	for perfil, esperado := range map[string]string{
		"admin": "criar,editar,excluir,ver", "usuario": "criar,editar,excluir,ver", "corretor": "", "vitrine": "",
	} {
		id := a.qbPerfilDoSeed(t, perfil)
		var acoes string
		if err := a.pool.QueryRow(a.ctx, `
			SELECT coalesce(string_agg(action || CASE WHEN scope = 'all' THEN '' ELSE ':' || scope END, ',' ORDER BY action), '')
			  FROM role_permissions WHERE role_id = $1 AND resource_code = 'maintenance'`, id).Scan(&acoes); err != nil {
			t.Fatal(err)
		}
		if acoes != esperado {
			t.Errorf("perfil %s no seed: maintenance = [%s], esperado [%s]", perfil, acoes, esperado)
		}
	}
	var suportaOwn bool
	if err := a.pool.QueryRow(a.ctx, `SELECT supports_own FROM resources WHERE code = 'maintenance'`).Scan(&suportaOwn); err != nil || suportaOwn {
		t.Errorf("o recurso maintenance não tem escopo own (a ordem é da casa): supports_own=%v %v", suportaOwn, err)
	}

	// O espelho que o painel lê para desenhar o menu (`/auth/me`) diz a mesma
	// coisa que a matriz: o usuario vê as quatro ações, o corretor nenhuma.
	for perfil, quer := range map[string]int{"usuario": 4, "corretor": 0} {
		u := a.criarUsuario(t, "manut-me-"+perfil, a.qbPerfilDoSeed(t, perfil))
		var me struct {
			Data struct {
				Permissoes []auth.Permissao `json:"permissions"`
			} `json:"data"`
		}
		r := a.chamar(t, http.MethodGet, "/auth/me", u.Token, nil)
		exigirStatusQB(t, r, http.StatusOK, "GET /auth/me")
		r.decodificar(t, &me)
		n := 0
		for _, p := range me.Data.Permissoes {
			if p.Resource == "maintenance" {
				n++
				if p.Scope != auth.EscopoAll {
					t.Errorf("%s: /auth/me traz maintenance:%s em escopo %s", perfil, p.Action, p.Scope)
				}
			}
		}
		if n != quer {
			t.Errorf("%s: /auth/me traz %d ações de maintenance, esperado %d", perfil, n, quer)
		}
	}
}
