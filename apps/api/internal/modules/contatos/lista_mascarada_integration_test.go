//go:build integration

package contatos_test

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// F2-23, repetindo a medida do backlog: autenticado como `corretor@wh.local`
// (o usuário do SEED, com a matriz do seed — `contacts:ver` em `all`),
// `GET /contacts?per_page=100` devolvia 11 CPFs completos e `pii_access_log`
// ficava parado (43 → 43).
//
// O contrato fechou a regra: coleção mascara e não grava; a ficha devolve
// cheio e grava. Este teste reprova se QUALQUER linha de coleção trouxer
// documento, telefone ou e-mail inteiros — não só as fichas que ele criou.
func TestColecaoDeContatosNaoServeDocumentoCheioComoCorretor(t *testing.T) {
	a := subir(t)
	corretor, token := a.sessaoDoSeed(t, "corretor@wh.local")

	marca := "Mascara " + sufixo()
	cpf, cnpj := cpfDeTeste(t), cnpjDeTeste(t)
	fichas := []struct {
		tipo, numero, telefone, email string
		id                            uuid.UUID
	}{
		{tipo: "cpf", numero: cpf},
		{tipo: "cnpj", numero: cnpj},
		{tipo: "passaporte", numero: "QA" + cpf[:7]},
	}
	for i := range fichas {
		f := &fichas[i]
		f.telefone = telefoneDeTeste(t)
		f.email = fmt.Sprintf("titular.%s@exemplo.invalid", sufixo())
		if err := a.pool.QueryRow(a.ctx, `
			INSERT INTO contacts (property_id, name, email, phone_e164, doc_type, doc_number,
			                      birth_date, notes, lgpd_basis)
			VALUES ($1, $2, $3, $4, $5, $6, '1990-05-02', 'pediu para não ligar depois das 18h', 'contrato')
			RETURNING id`,
			a.propriedade, fmt.Sprintf("%s %d", marca, i), f.email, f.telefone, f.tipo, f.numero).Scan(&f.id); err != nil {
			t.Fatalf("criando a ficha %s: %v", f.tipo, err)
		}
		a.limparContato(t, f.id)
	}
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM pii_access_log WHERE actor_id = $1 AND contact_id = ANY($2)`, corretor, idsDe(fichas))
	})

	antes := a.contar(t, `SELECT count(*) FROM pii_access_log`)

	// As consultas de coleção: a medida literal, a busca por nome, e os dois
	// filtros pelo valor CHEIO — que continuam achando a pessoa.
	consultas := []string{
		"/contacts?per_page=100",
		"/contacts?per_page=100&q=" + urlescape(marca),
		"/contacts?doc_number=" + cpf,
		"/contacts?phone=" + urlescape(fichas[0].telefone),
	}
	for _, caminho := range consultas {
		r := a.chamar(t, http.MethodGet, caminho, token, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", caminho, r.Status, r.Corpo)
		}
		exigirColecaoMascarada(t, caminho, r.Corpo)
		for _, f := range fichas {
			for _, cheio := range []string{f.numero, f.telefone, f.email} {
				if strings.Contains(string(r.Corpo), cheio) {
					t.Errorf("GET %s devolveu o valor cheio %q", caminho, cheio)
				}
			}
		}
	}

	// O filtro pelo CPF cheio acha exatamente a ficha, e ela sai mascarada no
	// formato do contrato.
	achadas, _ := lista[map[string]any](t, a.chamar(t, http.MethodGet, "/contacts?doc_number="+cpf, token, nil))
	if len(achadas) != 1 || achadas[0]["id"] != fichas[0].id.String() {
		t.Fatalf("doc_number=%s achou %v, esperado só a ficha %s", cpf, achadas, fichas[0].id)
	}
	if esperado := "***.***." + cpf[6:9] + "-" + cpf[9:]; achadas[0]["doc_number"] != esperado {
		t.Errorf("doc_number = %v, esperado %q", achadas[0]["doc_number"], esperado)
	}
	pelaMarca, _ := lista[map[string]any](t, a.chamar(t, http.MethodGet, "/contacts?per_page=100&q="+urlescape(marca), token, nil))
	if len(pelaMarca) != len(fichas) {
		t.Fatalf("q=%s achou %d fichas, esperado %d", marca, len(pelaMarca), len(fichas))
	}

	if depois := a.contar(t, `SELECT count(*) FROM pii_access_log`); depois != antes {
		t.Fatalf("pii_access_log foi de %d para %d com leituras de coleção: a coleção mascara e não grava", antes, depois)
	}

	// A ficha individual devolve cheio e soma +1, com o corretor como ator.
	r := a.chamar(t, http.MethodGet, "/contacts/"+fichas[0].id.String(), token, nil)
	if r.Status != http.StatusOK || !strings.Contains(string(r.Corpo), cpf) {
		t.Fatalf("a ficha não devolveu o CPF cheio (%d): %s", r.Status, r.Corpo)
	}
	if depois := a.contar(t, `SELECT count(*) FROM pii_access_log`); depois != antes+1 {
		t.Fatalf("GET /contacts/{id}: pii_access_log foi de %d para %d, esperado +1", antes, depois)
	}
	if n := a.contar(t, `SELECT count(*) FROM pii_access_log WHERE actor_id = $1 AND contact_id = $2 AND reason = 'detail'`,
		corretor, fichas[0].id); n != 1 {
		t.Fatalf("%d linhas detail do corretor para a ficha; esperado 1", n)
	}
}

// PATCH devolve a ficha cheia — inclusive o que o corpo não trouxe — e por
// isso registra, como a ficha. E o e-mail mascarado da lista não volta como
// entrada: `f***@gmail.com` passa no validador de formato (a RFC aceita `*`) e
// gravaria a máscara por cima do endereço verdadeiro.
func TestPatchDeContatoRegistraLeituraERecusaEmailMascarado(t *testing.T) {
	a := subir(t)
	ator, token := a.gestor(t)
	contato := a.contatoDireto(t, "Patch Lido "+sufixo())
	var original string
	if err := a.pool.QueryRow(a.ctx, `SELECT email FROM contacts WHERE id = $1`, contato).Scan(&original); err != nil {
		t.Fatalf("lendo o e-mail: %v", err)
	}

	r := a.chamar(t, http.MethodPatch, "/contacts/"+contato.String(), token, map[string]any{})
	if r.Status != http.StatusOK {
		t.Fatalf("PATCH {} = %d: %s", r.Status, r.Corpo)
	}
	if n := a.contar(t, `SELECT count(*) FROM pii_access_log WHERE contact_id = $1 AND actor_id = $2 AND reason = 'detail'`,
		contato, ator); n != 1 {
		t.Fatalf("PATCH {} gravou %d linhas em pii_access_log; esperado 1 — a resposta é a ficha cheia", n)
	}

	mascarado := string(original[0]) + "***" + original[strings.LastIndex(original, "@"):]
	for _, metodo := range []string{http.MethodPatch, http.MethodPut} {
		r = a.chamar(t, metodo, "/contacts/"+contato.String(), token, map[string]any{
			"name": "Patch Lido", "email": mascarado,
		})
		if r.Status != http.StatusUnprocessableEntity || r.codigoDoErro() != "VALIDATION_ERROR" {
			t.Fatalf("%s com e-mail %q = %d/%s, esperado 422 VALIDATION_ERROR: %s", metodo, mascarado, r.Status, r.codigoDoErro(), r.Corpo)
		}
		if d := r.detalhes(t); d["email"] == nil {
			t.Fatalf("%s: details sem email: %v", metodo, d)
		}
	}
	var atual string
	if err := a.pool.QueryRow(a.ctx, `SELECT email FROM contacts WHERE id = $1`, contato).Scan(&atual); err != nil {
		t.Fatalf("relendo o e-mail: %v", err)
	}
	if atual != original {
		t.Fatalf("o e-mail gravado mudou para %q", atual)
	}
	if n := a.contar(t, `SELECT count(*) FROM pii_access_log WHERE contact_id = $1`, contato); n != 1 {
		t.Fatalf("escrita recusada gravou leitura: %d linhas, esperado continuar em 1", n)
	}
}

// exigirColecaoMascarada confere CAMPO A CAMPO cada linha da coleção — inclusive
// as fichas do seed e de outros testes —, e que as chaves que a coleção não tem
// (`birth_date`, `notes`) não aparecem nem como null. Varrer o corpo cru com
// `\d{11}` daria falso positivo: um uuid pode ter 11 dígitos seguidos no último
// grupo.
func exigirColecaoMascarada(t *testing.T, caminho string, corpo []byte) {
	t.Helper()
	var env struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(corpo, &env); err != nil {
		t.Fatalf("GET %s: corpo ilegível: %v", caminho, err)
	}
	docCheio := regexp.MustCompile(`^[A-Za-z0-9]{4,}$`)
	foneCheio := regexp.MustCompile(`^\+?\d{5,}$`)
	for _, linha := range env.Data {
		for _, proibida := range []string{"birth_date", "notes"} {
			if _, existe := linha[proibida]; existe {
				t.Errorf("GET %s: a linha %s traz a chave %q", caminho, linha["id"], proibida)
			}
		}
		var doc, fone, email *string
		_ = json.Unmarshal(linha["doc_number"], &doc)
		_ = json.Unmarshal(linha["phone_e164"], &fone)
		_ = json.Unmarshal(linha["email"], &email)
		if doc != nil && (docCheio.MatchString(*doc) || !strings.Contains(*doc, "*")) {
			t.Errorf("GET %s: doc_number cheio na coleção: %q", caminho, *doc)
		}
		if fone != nil && (foneCheio.MatchString(*fone) || !regexp.MustCompile(`^\+\*+\d{0,4}$`).MatchString(*fone)) {
			t.Errorf("GET %s: phone_e164 fora da máscara: %q", caminho, *fone)
		}
		if email != nil && *email != "***" && !regexp.MustCompile(`^.?\*\*\*@`).MatchString(*email) {
			t.Errorf("GET %s: email fora da máscara: %q", caminho, *email)
		}
	}
}

// sessaoDoSeed assina um token para um usuário do SEED — com o perfil e a
// matriz que o seed deu a ele, não um perfil montado pelo teste. É o que torna
// esta a medida do backlog, e não uma parecida.
func (a *ambiente) sessaoDoSeed(t *testing.T, email string) (uuid.UUID, string) {
	t.Helper()
	var (
		id     uuid.UUID
		codigo string
	)
	if err := a.pool.QueryRow(a.ctx, `
		SELECT u.id, r.code FROM users u JOIN roles r ON r.id = u.role_id WHERE u.email = $1`, email).
		Scan(&id, &codigo); err != nil {
		t.Fatalf("usuário %s do seed (rodou cmd/seed?): %v", email, err)
	}
	assinado, _, err := a.emissor.Issue(id, codigo)
	if err != nil {
		t.Fatalf("assinando token: %v", err)
	}
	return id, assinado
}

func idsDe(fichas []struct {
	tipo, numero, telefone, email string
	id                            uuid.UUID
}) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(fichas))
	for _, f := range fichas {
		out = append(out, f.id)
	}
	return out
}

// cpfDeTeste sorteia um CPF VÁLIDO (dígitos verificadores certos): a máscara
// só usa a forma bonita para CPF de 11 dígitos, e o teste precisa exercitá-la.
func cpfDeTeste(t *testing.T) string {
	t.Helper()
	d := digitosSorteados(t, 9)
	for _, pos := range []int{9, 10} {
		soma := 0
		for i := 0; i < pos; i++ {
			soma += d[i] * (pos + 1 - i)
		}
		dv := soma * 10 % 11
		if dv == 10 {
			dv = 0
		}
		d = append(d, dv)
	}
	return juntar(d)
}

func cnpjDeTeste(t *testing.T) string {
	t.Helper()
	d := append(digitosSorteados(t, 8), 0, 0, 0, 1)
	pesos := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	for _, pos := range []int{12, 13} {
		p := pesos[len(pesos)-pos:]
		soma := 0
		for i := 0; i < pos; i++ {
			soma += d[i] * p[i]
		}
		dv := soma % 11
		if dv < 2 {
			dv = 0
		} else {
			dv = 11 - dv
		}
		d = append(d, dv)
	}
	return juntar(d)
}

func digitosSorteados(t *testing.T, n int) []int {
	t.Helper()
	out := make([]int, 0, n+2)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			t.Fatalf("sorteando dígito: %v", err)
		}
		out = append(out, int(v.Int64()))
	}
	return out
}

func juntar(d []int) string {
	var b strings.Builder
	for _, x := range d {
		b.WriteByte(byte('0' + x))
	}
	return b.String()
}

// A ficha de um corretor (`brokers.contact_id`, FK RESTRICT desde
// 20261002180000) não se apaga: 409 RESOURCE_IN_USE com `references.brokers`.
// Sem a contagem, o DELETE seguia, o banco respondia 23503 em
// brokers_contact_id_fkey e a API devolvia 422 genérico.
func TestExcluirFichaDeCorretorDa409ComAContagem(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contatoDireto(t, "Corretor Cadastrado "+sufixo())
	var corretor uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`INSERT INTO brokers (property_id, contact_id) VALUES ($1, $2) RETURNING id`,
		a.propriedade, contato).Scan(&corretor); err != nil {
		t.Fatalf("criando o cadastro de corretor: %v", err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM brokers WHERE id = $1`, corretor) })

	r := a.chamar(t, http.MethodDelete, "/contacts/"+contato.String(), token, nil)
	if r.Status != http.StatusConflict || r.codigoDoErro() != "RESOURCE_IN_USE" {
		t.Fatalf("DELETE = %d/%s, esperado 409 RESOURCE_IN_USE: %s", r.Status, r.codigoDoErro(), r.Corpo)
	}
	refs, _ := r.detalhes(t)["references"].(map[string]any)
	if refs == nil || refs["brokers"] != float64(1) {
		t.Fatalf("details.references = %v, esperado brokers = 1", refs)
	}
}
