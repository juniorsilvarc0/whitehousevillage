package tarifario

import (
	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// Este arquivo isola a aritmética de POST /rates/bulk: dado o estado gravado e o
// que a tela mandou, o que sai é o que precisa entrar, o que precisa sair e a
// conta que o operador lê no `meta`.
//
// Fica fora do repositório e fora do service de propósito. É a parte que a
// revisão pediu para provar (o bulk de um produto NÃO toca nos outros), e uma
// função pura se prova sem Postgres, célula a célula, em milissegundos.

// CelulaGravada é uma célula como ela está no banco AGORA.
//
// Carrega o `id` porque a remoção é feita por id — apagar por
// `(tabela, produto, tipo) NOT IN (...)` reconstruiria em SQL a mesma decisão
// que já foi tomada aqui, e as duas versões da regra divergiriam no primeiro
// ajuste. Carrega o `code` porque é ele que vai para a trilha de auditoria:
// `apto-2s.reveillon` responde "quem triplicou a tarifa" sem uma segunda
// consulta, e um uuid não.
type CelulaGravada struct {
	ID            uuid.UUID
	ProdutoID     uuid.UUID
	ProdutoCodigo string
	TipoDeData    string
	ValorCents    int64
}

// ResumoDaGrade é o `meta` da resposta do bulk.
//
// Existe porque a substituição escopada torna a remoção invisível: quem salva o
// produto A não vê o produto B na tela e não teria como notar que ele sumiu.
// `removed` é o número que faz uma remoção inesperada aparecer para quem salvou,
// em vez de ser descoberta quando a venda falhar com RATE_NOT_FOUND.
type ResumoDaGrade struct {
	// ProdutoIDs é o escopo EFETIVAMENTE aplicado — útil para o cliente que não
	// mandou a lista conferir o que a API deduziu de `rates`.
	ProdutoIDs  []uuid.UUID `json:"unit_type_ids"`
	Criadas     int         `json:"created"`
	Atualizadas int         `json:"updated"`
	Inalteradas int         `json:"unchanged"`
	Removidas   int         `json:"removed"`
}

// chaveDaCelula é a chave natural de `rates` sem a tabela (que é uma só na
// chamada inteira): `UNIQUE (rate_table_id, unit_type_id, date_type)`.
type chaveDaCelula struct {
	produto uuid.UUID
	tipo    string
}

// planoDaGrade é o que a chamada vai fazer, decidido antes de tocar no banco.
type planoDaGrade struct {
	// Remover são ids de `rates`, sempre dentro do escopo.
	Remover []uuid.UUID
	// Gravar são as células que ficam — INSERT ou UPDATE do valor.
	Gravar []CelulaDaGrade
	Resumo ResumoDaGrade
	// Antes e Depois são os documentos da trilha, já recortados para o que
	// mudou. O recorte é feito aqui, e não pelo audit.Diff, porque a chave da
	// trilha é `codigo.tipo` — legível — e não o formato da linha do banco.
	Antes  audit.Campos
	Depois audit.Campos
}

// planejarGrade compara o gravado com o enviado, DENTRO DO ESCOPO.
//
// O escopo é o coração da correção do BAIXO 10. Antes, o raio de ação era a
// tabela inteira (`DELETE FROM rates WHERE rate_table_id = $1`): medido em
// 26/08/2026 contra Postgres real, salvar as 6 células de um produto apagou as
// 18 dos outros três e a venda deles passou a responder RATE_NOT_FOUND sem
// ninguém ter pedido para apagar nada. Agora o raio de ação são os produtos
// citados, e o produto que não foi citado não é sequer lido.
//
// `atuais` chega ordenado pelo repositório e é percorrido como slice, não como
// mapa: a ordem de `Remover` e a das chaves da trilha precisam ser as mesmas em
// duas execuções iguais, senão o teste vira moeda e o `meta` fica difícil de
// conferir a olho.
func planejarGrade(escopo []uuid.UUID, codigos map[uuid.UUID]string, atuais []CelulaGravada, enviadas []CelulaDaGrade) planoDaGrade {
	plano := planoDaGrade{
		Gravar: enviadas,
		Resumo: ResumoDaGrade{ProdutoIDs: escopo},
		Antes:  audit.Campos{},
		Depois: audit.Campos{},
	}

	gravado := make(map[chaveDaCelula]CelulaGravada, len(atuais))
	for _, c := range atuais {
		gravado[chaveDaCelula{c.ProdutoID, c.TipoDeData}] = c
	}

	enviado := make(map[chaveDaCelula]bool, len(enviadas))
	for _, c := range enviadas {
		chave := chaveDaCelula{c.ProdutoID, c.TipoDeData}
		enviado[chave] = true

		rotulo := rotuloDaCelula(codigos, c.ProdutoID, c.TipoDeData)
		anterior, existia := gravado[chave]
		switch {
		case !existia:
			plano.Resumo.Criadas++
			plano.Depois[rotulo] = c.ValorCents
		case anterior.ValorCents == c.ValorCents:
			// Célula reenviada idêntica. Não entra na trilha: o rastro de uma
			// tarifa que não mudou é ruído entre as que mudaram, e é o `meta`
			// que conta ao operador que ela foi reenviada.
			plano.Resumo.Inalteradas++
		default:
			plano.Resumo.Atualizadas++
			plano.Antes[rotulo] = anterior.ValorCents
			plano.Depois[rotulo] = c.ValorCents
		}
	}

	for _, c := range atuais {
		if enviado[chaveDaCelula{c.ProdutoID, c.TipoDeData}] {
			continue
		}
		// Célula de produto DO ESCOPO que não veio no corpo: some. Produto fora
		// do escopo nem aparece em `atuais`, então nunca chega aqui.
		plano.Remover = append(plano.Remover, c.ID)
		plano.Resumo.Removidas++
		plano.Antes[rotuloDaCelula(codigos, c.ProdutoID, c.TipoDeData)] = c.ValorCents
	}

	return plano
}

// rotuloDaCelula monta a chave que vai para `audit_log`: `apto-2s.reveillon`.
//
// Cai no uuid só se o código faltar, o que não deve acontecer (o escopo é
// validado contra `unit_types` antes) — mas trilha com uuid é melhor que trilha
// com chave vazia colidindo entre células.
func rotuloDaCelula(codigos map[uuid.UUID]string, produto uuid.UUID, tipo string) string {
	codigo, ok := codigos[produto]
	if !ok {
		codigo = produto.String()
	}
	return codigo + "." + tipo
}
