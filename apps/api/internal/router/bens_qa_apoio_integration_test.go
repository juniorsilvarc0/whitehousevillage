//go:build integration

// Apoio da bateria de QA do inventário de bens por ambiente (Fase 5, recurso
// `inventory.goods`), pela PORTA DA FRENTE: o router completo de `router.New`,
// sessões abertas por `/auth/login` com perfis do seed (`admin`, `usuario`,
// `corretor`), e o contrato lido de `openapi/openapi.yaml`.
//
// Por que aqui e não em `internal/modules/bens`: os 59 testes do módulo montam
// um chi próprio com as linhas de `rotasBens` e assinam o token direto no
// emissor. Isso prova o módulo, mas não prova a aplicação montada — a ordem dos
// middlewares, o 404/405 do chi, o limitador, o teto de 50 s, nem o login. O que
// se cobra aqui é o que o painel e o celular realmente atravessam.
//
// A lista das 40 operações e o par (recurso, ação) de cada uma vêm do CONTRATO
// (tag `Bens` e `x-rbac`), nunca de `rotas_inventario_bens.go`: teste que lê a
// tabela que ele mesmo audita passa verde quando a tabela erra.
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// ─────────────────────────── O contrato ─────────────────────────────────────

// qbOperacao é uma operação da tag Bens, como o contrato a declara.
type qbOperacao struct {
	Metodo, Path, Recurso, Acao string
}

func (o qbOperacao) chave() string { return o.Metodo + " " + o.Path }

var qbLinhaDeRBAC = regexp.MustCompile(`^      x-rbac:\s*\{\s*recurso:\s*([^,\s]+),\s*acao:\s*([^\s}]+)\s*\}`)

// qbOperacoesDeBens lê do contrato as operações com `tags: [Bens]` e o x-rbac
// de cada uma. Mesmo scanner de indentação de contract_test.go (sem YAML no
// go.mod): dois espaços para o path, quatro para o verbo, seis para os campos.
func qbOperacoesDeBens(t *testing.T) []qbOperacao {
	t.Helper()

	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato: %v", err)
	}
	type acumulada struct {
		ehDeBens        bool
		recurso, acao   string
		metodo, caminho string
	}
	var (
		ordem []*acumulada
		atual *acumulada
		path  string
	)
	for _, linha := range strings.Split(string(bruto), "\n") {
		if linha != "" && linha[0] != ' ' && linha[0] != '#' {
			// Saiu de `paths:` (ex.: `components:`).
			path, atual = "", nil
			continue
		}
		if m := linhaDePath.FindStringSubmatch(linha); m != nil {
			path, atual = m[1], nil
			continue
		}
		if path == "" {
			continue
		}
		if m := linhaDeVerbo.FindStringSubmatch(linha); m != nil {
			atual = &acumulada{metodo: strings.ToUpper(m[1]), caminho: path}
			ordem = append(ordem, atual)
			continue
		}
		if atual == nil {
			continue
		}
		if strings.HasPrefix(linha, "      tags:") && strings.Contains(linha, "Bens") {
			atual.ehDeBens = true
		}
		if m := qbLinhaDeRBAC.FindStringSubmatch(linha); m != nil {
			atual.recurso, atual.acao = m[1], m[2]
		}
	}

	var out []qbOperacao
	for _, a := range ordem {
		if !a.ehDeBens {
			continue
		}
		if a.recurso == "" || a.acao == "" {
			t.Fatalf("%s %s está na tag Bens sem x-rbac legível — o contrato não diz que permissão ela exige", a.metodo, a.caminho)
		}
		out = append(out, qbOperacao{Metodo: a.metodo, Path: a.caminho, Recurso: a.recurso, Acao: a.acao})
	}
	if len(out) != 40 {
		t.Fatalf("o contrato tem %d operações na tag Bens; o pedido desta fatia fala em 40 — o scanner ficou defasado ou o contrato mudou", len(out))
	}
	return out
}

// qbCodigosDeErro devolve o enum fechado de `error.code` declarado no contrato.
func qbCodigosDeErro(t *testing.T) map[string]bool {
	t.Helper()

	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato: %v", err)
	}
	texto := string(bruto)
	ini := strings.Index(texto, "enum: [VALIDATION_ERROR")
	if ini < 0 {
		t.Fatal("o enum de error.code sumiu do contrato (procurei por `enum: [VALIDATION_ERROR`)")
	}
	fim := strings.Index(texto[ini:], "]")
	lista := texto[ini+len("enum: [") : ini+fim]
	out := map[string]bool{}
	for _, c := range strings.Split(lista, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out[c] = true
		}
	}
	if !out["COUNT_CLOSED"] || !out["FORBIDDEN"] {
		t.Fatalf("enum de error.code lido pela metade: %v", out)
	}
	return out
}

// qbPropriedadesDoContrato devolve TODOS os nomes de propriedade que o schema
// `nome` declara, em qualquer profundidade, seguindo `$ref` e `allOf`. É
// permissivo de propósito (une os níveis): serve para provar que a resposta não
// carrega campo que o contrato não conhece em lugar nenhum daquela árvore — que
// é como um telefone de hóspede vaza sem ninguém ter decidido isso.
func qbPropriedadesDoContrato(t *testing.T, nome string) map[string]bool {
	t.Helper()

	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato: %v", err)
	}
	linhas := strings.Split(string(bruto), "\n")
	inicioDosSchemas := -1
	for i, l := range linhas {
		if l == "  schemas:" {
			inicioDosSchemas = i
			break
		}
	}
	if inicioDosSchemas < 0 {
		t.Fatal("components.schemas não encontrado no contrato")
	}

	refs := regexp.MustCompile(`\$ref:\s*"#/components/schemas/([A-Za-z0-9]+)"`)
	chave := regexp.MustCompile(`^(\s*)([a-z_][a-z0-9_]*):`)
	indentacao := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

	out := map[string]bool{}
	visitados := map[string]bool{}
	var visitar func(schema string)
	visitar = func(schema string) {
		if visitados[schema] {
			return
		}
		visitados[schema] = true
		ini := -1
		for i := inicioDosSchemas; i < len(linhas); i++ {
			if linhas[i] == "    "+schema+":" {
				ini = i
				break
			}
		}
		if ini < 0 {
			t.Fatalf("schema %s não encontrado no contrato", schema)
		}
		var pilhaDeProps []int // indentação das linhas `properties:` abertas
		for i := ini + 1; i < len(linhas); i++ {
			l := linhas[i]
			if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			ind := indentacao(l)
			if ind <= 4 {
				break
			}
			for len(pilhaDeProps) > 0 && ind <= pilhaDeProps[len(pilhaDeProps)-1] {
				pilhaDeProps = pilhaDeProps[:len(pilhaDeProps)-1]
			}
			if strings.TrimSpace(l) == "properties:" {
				pilhaDeProps = append(pilhaDeProps, ind)
				continue
			}
			if len(pilhaDeProps) > 0 && ind == pilhaDeProps[len(pilhaDeProps)-1]+2 {
				if m := chave.FindStringSubmatch(l); m != nil {
					out[m[2]] = true
				}
			}
			for _, m := range refs.FindAllStringSubmatch(l, -1) {
				visitar(m[1])
			}
		}
	}
	visitar(nome)
	if len(out) == 0 {
		t.Fatalf("schema %s sem nenhuma propriedade lida — o scanner ficou defasado", nome)
	}
	return out
}

// qbChavesForaDoContrato percorre `data` da resposta e devolve toda chave de
// objeto (em qualquer nível) que o schema não declara.
func qbChavesForaDoContrato(t *testing.T, schema string, corpo []byte) []string {
	t.Helper()

	permitidas := qbPropriedadesDoContrato(t, schema)
	var env struct {
		Data any `json:"data"`
	}
	if err := json.Unmarshal(corpo, &env); err != nil {
		t.Fatalf("resposta fora do envelope {data}: %v — %s", err, corpo)
	}
	vistas := map[string]bool{}
	var andar func(v any)
	andar = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, filho := range x {
				if !permitidas[k] {
					vistas[k] = true
				}
				andar(filho)
			}
		case []any:
			for _, filho := range x {
				andar(filho)
			}
		}
	}
	andar(env.Data)
	var out []string
	for k := range vistas {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────── Envelope de erro ───────────────────────────────

type qbErroLido struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// qbErro confere status, o envelope `{error{code, message, details}}` e que o
// `code` pertence ao vocabulário fechado do contrato. `details`, quando vem,
// tem de ser objeto — o contrato o declara `type: object`.
func qbErro(t *testing.T, r resposta, status int, code, contexto string) qbErroLido {
	t.Helper()

	if r.Status != status {
		t.Fatalf("%s: status %d, esperado %d — corpo: %s", contexto, r.Status, status, r.Corpo)
	}
	return qbEnvelopeDeErro(t, r, code, contexto)
}

func qbEnvelopeDeErro(t *testing.T, r resposta, code, contexto string) qbErroLido {
	t.Helper()

	var cru map[string]json.RawMessage
	if err := json.Unmarshal(r.Corpo, &cru); err != nil {
		t.Fatalf("%s: corpo de erro não é JSON (%v): %q", contexto, err, r.Corpo)
	}
	if len(cru) != 1 || cru["error"] == nil {
		t.Fatalf("%s: o erro tem de vir só em {\"error\": ...}; veio %s", contexto, r.Corpo)
	}
	var dentro map[string]json.RawMessage
	if err := json.Unmarshal(cru["error"], &dentro); err != nil {
		t.Fatalf("%s: `error` não é objeto: %s", contexto, r.Corpo)
	}
	for k := range dentro {
		if k != "code" && k != "message" && k != "details" {
			t.Errorf("%s: `error.%s` não está no contrato (code, message, details): %s", contexto, k, r.Corpo)
		}
	}
	if d, ok := dentro["details"]; ok && !bytes.HasPrefix(bytes.TrimSpace(d), []byte("{")) {
		t.Errorf("%s: `error.details` tem de ser objeto (contrato: type object); veio %s", contexto, d)
	}
	var e qbErroLido
	if err := json.Unmarshal(cru["error"], &e); err != nil {
		t.Fatalf("%s: lendo error: %v — %s", contexto, err, r.Corpo)
	}
	if e.Message == "" {
		t.Errorf("%s: `error.message` vazio: %s", contexto, r.Corpo)
	}
	if !qbEnum(t)[e.Code] {
		t.Errorf("%s: `error.code` %q fora do enum do contrato", contexto, e.Code)
	}
	if code != "" && e.Code != code {
		t.Fatalf("%s: code %q, esperado %q — %s", contexto, e.Code, code, r.Corpo)
	}
	return e
}

var (
	qbEnumUnico sync.Once
	qbEnumLido  map[string]bool
)

func qbEnum(t *testing.T) map[string]bool {
	qbEnumUnico.Do(func() { qbEnumLido = qbCodigosDeErro(t) })
	return qbEnumLido
}

// ─────────────────────────── Leitura de respostas ───────────────────────────

func qbDado[T any](t *testing.T, r resposta, esperado int, contexto string) T {
	t.Helper()
	return envelopeDe[T](t, r, esperado, contexto)
}

type qbMeta struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func qbLista[T any](t *testing.T, r resposta, contexto string) ([]T, qbMeta) {
	t.Helper()
	if r.Status != http.StatusOK {
		t.Fatalf("%s: status %d — %s", contexto, r.Status, r.Corpo)
	}
	var env struct {
		Data []T    `json:"data"`
		Meta qbMeta `json:"meta"`
	}
	if err := json.Unmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("%s: lendo a listagem: %v — %s", contexto, err, r.Corpo)
	}
	return env.Data, env.Meta
}

// qbChamarCru manda o corpo como está — para campo desconhecido, chave com
// outra caixa, `{}`, `null` e corpo vazio, que `chamar` serializaria.
func (a *ambiente) qbChamarCru(t *testing.T, metodo, caminho, token, corpo string) resposta {
	t.Helper()
	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+PrefixoDaAPI+caminho, strings.NewReader(corpo))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return a.fazer(t, req)
}

// qbBaixar busca uma URL que JÁ traz o prefixo (`/api/v1/...`), como as que a
// API devolve em `url`, `thumb_url` e `Location`.
func (a *ambiente) qbBaixar(t *testing.T, token, url string) resposta {
	t.Helper()
	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, a.servidor.URL+url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return a.fazer(t, req)
}

// qbEnviarFoto faz o POST multipart de `/inventory/media`. `tipoDaParte` vazio
// deixa o `application/octet-stream` padrão; preenchido, é o cabeçalho que o
// aparelho MENTE (o contrato manda decidir pelos bytes).
func (a *ambiente) qbEnviarFoto(t *testing.T, token, campo, nome, tipoDaParte string, conteudo []byte) resposta {
	t.Helper()
	var corpo bytes.Buffer
	mw := multipart.NewWriter(&corpo)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, campo, nome))
	if tipoDaParte == "" {
		tipoDaParte = "application/octet-stream"
	}
	h.Set("Content-Type", tipoDaParte)
	parte, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parte.Write(conteudo); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodPost, a.servidor.URL+PrefixoDaAPI+"/inventory/media", &corpo)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		t.Fatalf("POST /inventory/media: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Headers: resp.Header}
}

// ─────────────────────────── Perfis e sessões ───────────────────────────────

// qbPerfilDoSeed devolve o perfil REAL do seed. Sem seed, o teste não tem o
// que provar sobre a matriz, e pula dizendo o que falta.
func (a *ambiente) qbPerfilDoSeed(t *testing.T, codigo string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT id FROM roles WHERE code = $1`, codigo).Scan(&id); err != nil {
		t.Skipf("perfil %q do seed ausente: rode `SEED_CATALOGO=teste go run ./cmd/seed` (%v)", codigo, err)
	}
	return id
}

// qbUsuarioEm cria a conta numa propriedade qualquer (a segunda casa do teste
// de isolamento), com telefone — que é o dado de operador que nenhuma resposta
// de bens pode carregar — e faz login pela porta da frente.
func (a *ambiente) qbUsuarioEm(t *testing.T, apelido string, perfil, propriedade uuid.UUID) usuarioDeTeste {
	t.Helper()

	sufixo := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	email := "qa-bens-" + apelido + "-" + sufixo + "@wh.local"
	hash, err := auth.Hash(senhaDeIntegracao)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO users (property_id, role_id, name, email, password_hash, phone)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		propriedade, perfil, "QA Bens "+apelido, email, hash, qbTelefone()).Scan(&id); err != nil {
		t.Fatalf("criando usuário: %v", err)
	}
	t.Cleanup(func() {
		limpeza, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = a.pool.Exec(limpeza, `DELETE FROM audit_log WHERE actor_id = $1`, id)
		_, _ = a.pool.Exec(limpeza, `DELETE FROM inventory_media WHERE created_by = $1`, id)
		if _, err := a.pool.Exec(limpeza, `DELETE FROM users WHERE id = $1`, id); err != nil {
			t.Logf("LIMPEZA INCOMPLETA: usuário %s ficou no banco (%v)", id, err)
		}
	})
	u := usuarioDeTeste{ID: id, RoleID: perfil, Email: email}
	u.Token = a.entrar(t, email, senhaDeIntegracao)
	return u
}

// qbComTelefone grava um telefone no usuário criado por `criarUsuario`.
func (a *ambiente) qbComTelefone(t *testing.T, u usuarioDeTeste) string {
	t.Helper()
	tel := qbTelefone()
	if _, err := a.pool.Exec(a.ctx, `UPDATE users SET phone = $2 WHERE id = $1`, u.ID, tel); err != nil {
		t.Fatalf("telefone do usuário: %v", err)
	}
	return tel
}

func qbTelefone() string {
	n := uuid.New()
	return fmt.Sprintf("+55859%08d", (int(n[0])<<16|int(n[1])<<8|int(n[2]))%100000000)
}

func qbSufixo() string { return strings.ReplaceAll(uuid.NewString(), "-", "")[:8] }

// ─────────────────────────── Faxina ─────────────────────────────────────────

// qbFaxina apaga, no fim, o que o teste criou — na ordem das FKs. Tem de ser
// criada DEPOIS dos usuários: t.Cleanup roda em ordem inversa, e os dados
// (conferência, avaria, foto) seguram o usuário que os registrou.
type qbFaxina struct {
	a            *ambiente
	mu           sync.Mutex
	unidades     []uuid.UUID
	itens        []uuid.UUID
	reservas     []uuid.UUID
	contatos     []uuid.UUID
	produtos     []uuid.UUID
	usuarios     []uuid.UUID
	propriedades []uuid.UUID
}

func (a *ambiente) qbFaxina(t *testing.T, usuarios ...usuarioDeTeste) *qbFaxina {
	t.Helper()
	f := &qbFaxina{a: a}
	for _, u := range usuarios {
		f.usuarios = append(f.usuarios, u.ID)
	}
	t.Cleanup(func() { f.limpar(t) })
	return f
}

func (f *qbFaxina) item(id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.itens = append(f.itens, id)
}

func (f *qbFaxina) limpar(t *testing.T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exec := func(sql string, args ...any) {
		if _, err := f.a.pool.Exec(ctx, sql, args...); err != nil {
			t.Logf("LIMPEZA INCOMPLETA (%s): %v", sql, err)
		}
	}
	for _, u := range f.unidades {
		exec(`DELETE FROM inventory_issues WHERE room_id IN (SELECT id FROM unit_rooms WHERE unit_id = $1)
		         OR count_id IN (SELECT id FROM inventory_counts WHERE unit_id = $1)`, u)
		exec(`DELETE FROM inventory_counts WHERE unit_id = $1`, u)
		exec(`DELETE FROM unit_rooms WHERE unit_id = $1`, u)
	}
	for _, i := range f.itens {
		exec(`DELETE FROM inventory_issues WHERE item_id = $1`, i)
		exec(`DELETE FROM inventory_count_lines WHERE item_id = $1`, i)
		exec(`DELETE FROM room_inventory WHERE item_id = $1`, i)
		exec(`DELETE FROM inventory_items WHERE id = $1`, i)
	}
	for _, r := range f.reservas {
		exec(`DELETE FROM inventory_issues WHERE reservation_id = $1`, r)
		exec(`DELETE FROM reservations WHERE id = $1`, r)
	}
	for _, c := range f.contatos {
		exec(`DELETE FROM contacts WHERE id = $1`, c)
	}
	for _, p := range f.produtos {
		exec(`DELETE FROM unit_types WHERE id = $1`, p)
	}
	for _, u := range f.unidades {
		exec(`DELETE FROM units WHERE id = $1`, u)
	}
	for _, u := range f.usuarios {
		exec(`DELETE FROM inventory_media WHERE created_by = $1`, u)
	}
	for _, p := range f.propriedades {
		exec(`DELETE FROM inventory_media WHERE property_id = $1`, p)
	}
}

// qbOutraCasa cria a segunda propriedade. A remoção é registrada AGORA — antes
// dos usuários dela — para rodar por último.
func (a *ambiente) qbOutraCasa(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO properties (name, slug, timezone) VALUES ('Casa B do QA', $1, 'America/Fortaleza') RETURNING id`,
		"qa-bens-b-"+qbSufixo()).Scan(&id); err != nil {
		t.Fatalf("criando a segunda propriedade: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = a.pool.Exec(ctx, `DELETE FROM inventory_media WHERE property_id = $1`, id)
		if _, err := a.pool.Exec(ctx, `DELETE FROM properties WHERE id = $1`, id); err != nil {
			t.Logf("LIMPEZA INCOMPLETA: propriedade %s ficou no banco (%v)", id, err)
		}
	})
	return id
}

// qbPropriedadePadrao é a casa do seed — a mesma em que `criarUsuario` põe a conta.
func (a *ambiente) qbPropriedadePadrao(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT id FROM properties WHERE slug = 'white-house-village'`).Scan(&id); err != nil {
		t.Skipf("propriedade do seed ausente: rode o seed (%v)", err)
	}
	return id
}

// qbUnidade cria a unidade física pelo banco — o cadastro comercial não é
// desta fatia. `codigo` vazio sorteia um.
func (f *qbFaxina) qbUnidade(t *testing.T, propriedade uuid.UUID, codigo string) (uuid.UUID, string) {
	t.Helper()
	if codigo == "" {
		codigo = "QA-BENS-" + strings.ToUpper(qbSufixo())
	}
	var id uuid.UUID
	if err := f.a.pool.QueryRow(f.a.ctx, `
		INSERT INTO units (property_id, code, name, sort_order) VALUES ($1, $2, $3, 991) RETURNING id`,
		propriedade, codigo, "Unidade "+codigo).Scan(&id); err != nil {
		t.Fatalf("criando unidade %s: %v", codigo, err)
	}
	f.mu.Lock()
	f.unidades = append(f.unidades, id)
	f.mu.Unlock()
	return id, codigo
}

// qbHospede é o contato com TODO dado pessoal preenchido, e uma estadia
// encerrada na unidade de produto própria. Datas fixas.
type qbHospede struct {
	ContatoID uuid.UUID
	Nome      string
	Email     string
	Telefone  string
	Documento string
	ReservaID uuid.UUID
	Codigo    string
}

func (f *qbFaxina) qbHospedeComEstadia(t *testing.T, propriedade uuid.UUID) qbHospede {
	t.Helper()
	s := qbSufixo()
	h := qbHospede{
		Nome:      "Hóspede Sigiloso " + s,
		Email:     "sigiloso-" + s + "@exemplo.invalid",
		Telefone:  qbTelefone(),
		Documento: fmt.Sprintf("9%010d", int(uuid.New()[0])*1000003%1000000000),
	}
	var produto uuid.UUID
	if err := f.a.pool.QueryRow(f.a.ctx, `
		INSERT INTO unit_types (property_id, code, name, capacity, consumes, sort_order)
		VALUES ($1, $2, 'Produto do QA de bens', 2, 'one_member', 991) RETURNING id`,
		propriedade, "qa-bens-"+s).Scan(&produto); err != nil {
		t.Fatalf("criando produto: %v", err)
	}
	if err := f.a.pool.QueryRow(f.a.ctx, `
		INSERT INTO contacts (property_id, name, email, phone_e164, doc_type, doc_number)
		VALUES ($1, $2, $3, $4, 'cpf', $5) RETURNING id`,
		propriedade, h.Nome, h.Email, h.Telefone, h.Documento).Scan(&h.ContatoID); err != nil {
		t.Fatalf("criando contato: %v", err)
	}
	if err := f.a.pool.QueryRow(f.a.ctx, `
		INSERT INTO reservations (property_id, unit_type_id, contact_id, status, check_in, check_out, guests_count)
		VALUES ($1, $2, $3, 'checked_out', DATE '2026-09-10', DATE '2026-09-13', 2)
		RETURNING id, code`, propriedade, produto, h.ContatoID).Scan(&h.ReservaID, &h.Codigo); err != nil {
		t.Fatalf("criando reserva: %v", err)
	}
	f.mu.Lock()
	f.produtos = append(f.produtos, produto)
	f.contatos = append(f.contatos, h.ContatoID)
	f.reservas = append(f.reservas, h.ReservaID)
	f.mu.Unlock()
	return h
}

// ─────────────────────────── Fixtures pela API ──────────────────────────────

type qbIDResp struct {
	ID uuid.UUID `json:"id"`
}

func (a *ambiente) qbComodo(t *testing.T, token string, unidade uuid.UUID, nome, tipo string, ordem int) uuid.UUID {
	t.Helper()
	r := a.chamar(t, http.MethodPost, "/rooms", token, map[string]any{
		"unit_id": unidade, "name": nome, "kind": tipo, "sort_order": ordem,
	})
	return qbDado[qbIDResp](t, r, http.StatusCreated, "POST /rooms "+nome).ID
}

func (a *ambiente) qbBem(t *testing.T, f *qbFaxina, token, nome string, custo *int64) uuid.UUID {
	t.Helper()
	corpo := map[string]any{"name": nome, "category": "louca"}
	if custo != nil {
		corpo["replacement_cost_cents"] = *custo
	}
	r := a.chamar(t, http.MethodPost, "/inventory/items", token, corpo)
	id := qbDado[qbIDResp](t, r, http.StatusCreated, "POST /inventory/items "+nome).ID
	f.item(id)
	return id
}

func (a *ambiente) qbColocar(t *testing.T, token string, comodo, bem uuid.UUID, qtd int) string {
	t.Helper()
	r := a.chamar(t, http.MethodPost, "/inventory/placements", token, map[string]any{
		"room_id": comodo, "item_id": bem, "expected_qty": qtd,
	})
	c := qbDado[struct {
		ID string `json:"id"`
	}](t, r, http.StatusCreated, "POST /inventory/placements")
	return c.ID
}

type qbLinha struct {
	ID          uuid.UUID `json:"id"`
	RoomID      uuid.UUID `json:"room_id"`
	ItemID      uuid.UUID `json:"item_id"`
	Esperada    int       `json:"expected_qty"`
	Contada     *int      `json:"counted_qty"`
	Custo       *int64    `json:"replacement_cost_cents"`
	ContadaEm   *string   `json:"counted_at"`
	ContadaPorN *string   `json:"counted_by_name"`
}

type qbConferencia struct {
	ID      uuid.UUID `json:"id"`
	Status  string    `json:"status"`
	Fechada *string   `json:"closed_at"`
	Rooms   []struct {
		RoomID uuid.UUID `json:"room_id"`
		Nome   string    `json:"room_name"`
		Linhas []qbLinha `json:"lines"`
	} `json:"rooms"`
	Result json.RawMessage `json:"result"`
}

func (c qbConferencia) linhaDo(t *testing.T, bem uuid.UUID) qbLinha {
	t.Helper()
	for _, r := range c.Rooms {
		for _, l := range r.Linhas {
			if l.ItemID == bem {
				return l
			}
		}
	}
	t.Fatalf("conferência %s sem linha do bem %s", c.ID, bem)
	return qbLinha{}
}

func (a *ambiente) qbAbrir(t *testing.T, token string, unidade uuid.UUID) qbConferencia {
	t.Helper()
	r := a.chamar(t, http.MethodPost, "/inventory/counts", token, map[string]any{"unit_id": unidade})
	return qbDado[qbConferencia](t, r, http.StatusCreated, "POST /inventory/counts")
}

func (a *ambiente) qbContar(t *testing.T, token string, conferencia, linha uuid.UUID, qtd any) resposta {
	t.Helper()
	return a.chamar(t, http.MethodPatch, fmt.Sprintf("/inventory/counts/%s/lines/%s", conferencia, linha), token,
		map[string]any{"counted_qty": qtd})
}

// qbPNG gera um PNG de w×h com um degradê — bytes reais, decodificáveis.
func qbPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	return qbCodificarPNG(t, qbImagem(w, h, nil))
}
