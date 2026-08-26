//go:build integration

// Corrida de rotação de refresh contra Postgres real.
//
// O defeito que este arquivo guarda: `RotacionarRefresh` fazia
// `UPDATE ... WHERE id=$1 AND revoked_at IS NULL` e ignorava o `RowsAffected`.
// Com duas rotações simultâneas do MESMO token, a segunda afetava zero linhas,
// devolvia nil e a transação commitava assim mesmo — nasciam dois sucessores
// vivos da mesma família e nenhum dos dois ficava marcado como rotacionado.
// Resultado prático: quem roubou o token dispara duas requisições em paralelo,
// fica com um ramo próprio e permanente da família, e a vítima NUNCA vê
// TOKEN_REUSED, porque o ramo dela continua rotacionando normalmente.
package auth

import (
	"context"
	"sync"
	"testing"
)

// rotacoesSimultaneas é alto de propósito: com duas goroutines a corrida ainda
// escapa em alguns escalonamentos, e um teste de concorrência que só falha às
// vezes não prova nada.
const rotacoesSimultaneas = 8

// Em linguagem de negócio: o comprovante de renovação vale UMA vez. Se oito
// pedidos chegam juntos com o mesmo comprovante, um único renova e os outros
// sete são tratados como cópia — porque cópia é exatamente o que são. E, tendo
// aparecido cópia, a sessão inteira daquele login cai.
func TestRotacoesSimultaneasDoMesmoTokenSoDeixamUmaVencer(t *testing.T) {
	pool := poolDeIntegracao(t)
	ctx := context.Background()
	c := criarConta(t, ctx, pool, "corrida")
	svc := servicoDeAuth(t, pool)

	sess, err := svc.Login(ctx, PedidoLogin{Email: c.Email, Senha: senhaDoTeste}, Origem{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	type resultado struct {
		sess Sessao
		err  error
	}
	resultados := make([]resultado, rotacoesSimultaneas)

	// A largada única é o que faz as transações se sobreporem de verdade: sem
	// ela as goroutines saem escalonadas e a corrida não acontece.
	var largada, fim sync.WaitGroup
	largada.Add(1)
	fim.Add(rotacoesSimultaneas)
	for i := range resultados {
		go func(i int) {
			defer fim.Done()
			largada.Wait()
			s, err := svc.Refresh(ctx, sess.RefreshClaro, Origem{})
			resultados[i] = resultado{sess: s, err: err}
		}(i)
	}
	largada.Done()
	fim.Wait()

	var vencedoras, acusaramRoubo int
	var sucessores []string
	for _, r := range resultados {
		if r.err == nil {
			vencedoras++
			sucessores = append(sucessores, r.sess.RefreshClaro)
			continue
		}
		// Com N rotações simultâneas, só o PRIMEIRO perdedor observa "rotacionado
		// por outro" e dispara a revogação da família. Os seguintes chegam depois
		// dela e encontram o token revogado SEM sucessor — que é, literalmente,
		// TOKEN_INVALID. Exigir TOKEN_REUSED de todos era especificar demais e
		// tornava o teste intermitente sob contenção. O que precisa valer é: pelo
		// menos um enxergou o roubo, e nenhum recebeu sessão.
		switch cod := codigo(r.err); cod {
		case "TOKEN_REUSED":
			acusaramRoubo++
		case "TOKEN_INVALID":
		default:
			t.Errorf("rotação perdedora respondeu %q, esperado TOKEN_REUSED ou TOKEN_INVALID", cod)
		}
	}
	if acusaramRoubo == 0 {
		t.Error("nenhuma rotação perdedora acusou roubo: sem um TOKEN_REUSED, " +
			"a família não teria sido revogada por detecção, e sim por acaso")
	}
	if vencedoras != 1 {
		t.Errorf("%d rotações venceram, esperado exatamente 1: o mesmo token emitiu "+
			"vários sucessores vivos e o ladrão ficou com um ramo próprio da família", vencedoras)
	}

	// O estado do banco é a prova. Detectada a cópia, a família inteira morre —
	// inclusive o sucessor de quem venceu a corrida.
	var total, revogados, rotacionados int
	if err := pool.QueryRow(ctx, `
		SELECT count(*), count(revoked_at), count(replaced_by)
		  FROM refresh_tokens
		 WHERE user_id = $1`, c.ID).Scan(&total, &revogados, &rotacionados); err != nil {
		t.Fatalf("lendo o estado da família: %v", err)
	}
	t.Logf("tokens=%d revogados=%d rotacionados=%d vencedoras=%d", total, revogados, rotacionados, vencedoras)
	if revogados != total {
		t.Errorf("%d de %d tokens continuaram vivos depois da corrida: "+
			"sobrou ramo utilizável da família comprometida", total-revogados, total)
	}
	if rotacionados != 1 {
		t.Errorf("replaced_by preenchido em %d tokens, esperado 1: sem a marca de "+
			"rotação a reapresentação do token deixa de ser evidência de roubo", rotacionados)
	}

	// Nenhum sucessor emitido na corrida pode continuar renovando.
	for _, claro := range sucessores {
		if _, err := svc.Refresh(ctx, claro, Origem{}); err == nil {
			t.Error("um sucessor da corrida ainda renova a sessão: a revogação da família não o alcançou")
		}
	}
}
