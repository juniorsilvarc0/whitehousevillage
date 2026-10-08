package bens

import (
	"context"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// Verbos da trilha da conferência — a decisão, não o verbo HTTP.
const (
	verboAberta    = "aberta"
	verboFechada   = "fechada"
	verboCancelada = "cancelada"
	verboContada   = "contada"
)

// encerrada é o 409 COUNT_CLOSED com o estado em `details`: o que está
// encerrado é história, e recontar mudaria em silêncio uma divergência que já
// virou pendência (talvez cobrança).
func encerrada(e estadoDaConferencia) error {
	return apperr.CountClosed.WithDetails(map[string]any{
		"status":    e.Status,
		"closed_at": e.EncerradaEm,
	})
}

func (s *Service) ListarConferencias(ctx context.Context, f FiltroDeConferencias) ([]Conferencia, int64, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarConferencias(ctx, prop, f)
}

// BuscarConferencia é a conferência inteira, ambiente por ambiente — a resposta
// que o celular consome e de que sai a folha de impressão.
func (s *Service) BuscarConferencia(ctx context.Context, id uuid.UUID) (ConferenciaCompleta, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return ConferenciaCompleta{}, err
	}
	return s.conferenciaCompleta(ctx, prop, id)
}

func (s *Service) conferenciaCompleta(ctx context.Context, prop, id uuid.UUID) (ConferenciaCompleta, error) {
	cab, err := s.repo.BuscarConferencia(ctx, prop, id)
	if err != nil {
		return ConferenciaCompleta{}, err
	}
	linhas, err := s.repo.LinhasDaConferencia(ctx, id)
	if err != nil {
		return ConferenciaCompleta{}, err
	}
	out := ConferenciaCompleta{Conferencia: cab, Ambientes: agruparPorAmbiente(linhas)}
	if cab.Status == StatusFechada {
		apurado, err := s.apuracao(ctx, id)
		if err != nil {
			return ConferenciaCompleta{}, err
		}
		out.Resultado = &apurado
	}
	return out, nil
}

// apuracao LÊ DE VOLTA o que o fechamento apurou: as divergências com o custo
// congelado nas linhas e as avarias nascidas no fechamento. Nada aqui consulta
// o custo de hoje do catálogo — é o que faz a perda de março sobreviver à
// recotação de junho. O próprio `POST /close` responde com esta função, para
// as duas respostas serem a mesma.
func (s *Service) apuracao(ctx context.Context, id uuid.UUID) (ApuracaoDaConferencia, error) {
	divergentes, err := s.repo.LinhasDivergentes(ctx, id)
	if err != nil {
		return ApuracaoDaConferencia{}, err
	}
	out := ApuracaoDaConferencia{Divergencias: make([]Divergencia, 0, len(divergentes))}
	for _, l := range divergentes {
		out.Divergencias = append(out.Divergencias, apurar(l).Divergencia)
	}
	if out.AvariasCriadas, err = s.repo.AvariasDoFechamento(ctx, id); err != nil {
		return ApuracaoDaConferencia{}, err
	}
	return out, nil
}

// agruparPorAmbiente parte as linhas (já na ordem de caminhada) em cômodos,
// com o rodapé de cada um.
func agruparPorAmbiente(linhas []LinhaDeConferencia) []AmbienteDaConferencia {
	out := []AmbienteDaConferencia{}
	for _, l := range linhas {
		if len(out) == 0 || out[len(out)-1].AmbienteID != l.AmbienteID {
			out = append(out, AmbienteDaConferencia{
				AmbienteID: l.AmbienteID, AmbienteNome: l.AmbienteNome, AmbienteTipo: l.AmbienteTipo,
				Linhas: []LinhaDeConferencia{},
			})
		}
		atual := &out[len(out)-1]
		atual.Linhas = append(atual.Linhas, l)
		atual.Progresso.somar(l.QtdEsperada, l.QtdContada)
	}
	return out
}

// AbrirConferencia abre a contagem da unidade e CONGELA a quantidade esperada.
//
// Tudo numa transação: o cabeçalho, as linhas copiadas de `room_inventory` e a
// trilha. A segunda abertura simultânea perde no índice único parcial
// `inventory_counts_aberta_idx` e recebe 409 COUNT_ALREADY_OPEN com a
// conferência que venceu — sem `Idempotency-Key`, de propósito: o 409 que
// nomeia a aberta é mais útil ao operador do que um 201 repetido.
func (s *Service) AbrirConferencia(ctx context.Context, c ConferenciaAbrir) (ConferenciaCompleta, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return ConferenciaCompleta{}, err
	}

	var id uuid.UUID
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if _, ok, err := s.repo.UnidadeDaPropriedade(ctx, prop, c.UnidadeID); err != nil {
			return err
		} else if !ok {
			return apperr.Validation(map[string]string{"unit_id": "unidade não encontrada nesta propriedade."})
		}
		// Serializa com a CÓPIA para esta unidade (ver TravarInventarioDaUnidade).
		// Entre duas aberturas quem decide continua sendo o índice único.
		if err := s.repo.TravarInventarioDaUnidade(ctx, c.UnidadeID); err != nil {
			return err
		}

		aberta, ja, err := s.repo.AbrirConferencia(ctx, prop, c.UnidadeID, textoOuNulo(c.Nota), atorOuNulo(ctx))
		if err != nil {
			return err
		}
		if ja != nil {
			conflito := apperr.CountAlreadyOpen
			if ja.ID != uuid.Nil {
				conflito = conflito.WithDetails(ja)
			}
			return conflito
		}

		n, err := s.repo.CongelarLinhas(ctx, prop, aberta, c.UnidadeID)
		if err != nil {
			return err
		}
		if n == 0 {
			// Desfaz o cabeçalho junto: conferência de zero itens é de nada.
			return apperr.Validation(map[string]string{
				"unit_id": "esta unidade não tem nenhum bem colocado em ambiente ativo — não há o que conferir.",
			})
		}
		id = aberta
		return audit.Criacao(ctx, s.repo.pool, entidadeConferencia, verboAberta, id, map[string]any{
			"unit_id": c.UnidadeID, "lines": n, "note": textoOuNulo(c.Nota),
		})
	})
	if err != nil {
		return ConferenciaCompleta{}, err
	}
	return s.conferenciaCompleta(ctx, prop, id)
}

// SubstituirConferencia é o PUT: o único campo editável é a observação.
func (s *Service) SubstituirConferencia(ctx context.Context, id uuid.UUID, c ConferenciaSubstituir) (Conferencia, error) {
	return s.gravarNota(ctx, id, func(*string) *string { return textoOuNulo(c.Nota) })
}

// AtualizarConferencia é o PATCH: ausente não mexe, `null` limpa.
func (s *Service) AtualizarConferencia(ctx context.Context, id uuid.UUID, p ConferenciaAtualizar) (Conferencia, error) {
	return s.gravarNota(ctx, id, func(atual *string) *string { return textoDoOpt(p.Nota, atual) })
}

func (s *Service) gravarNota(ctx context.Context, id uuid.UUID, nova func(atual *string) *string) (Conferencia, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Conferencia{}, err
	}
	var out Conferencia
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarConferencia(ctx, prop, id, true)
		if err != nil {
			return err
		}
		if antes.Status != StatusAberta {
			return encerrada(antes)
		}
		depois := antes
		depois.Nota = nova(antes.Nota)
		if err := s.repo.GravarNotaDaConferencia(ctx, id, depois.Nota); err != nil {
			return err
		}
		if out, err = s.repo.BuscarConferencia(ctx, prop, id); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.pool, entidadeConferencia, audit.VerboAlterado, id, antes, depois)
	})
	return out, err
}

// CancelarConferencia é o DELETE: CANCELA, não apaga. As linhas contadas até
// ali ficam — alguém andou pela casa, e esse trabalho é informação. É assim que
// se libera a unidade para uma contagem nova.
func (s *Service) CancelarConferencia(ctx context.Context, id uuid.UUID) error {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return err
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarConferencia(ctx, prop, id, true)
		if err != nil {
			return err
		}
		if antes.Status != StatusAberta {
			return encerrada(antes)
		}
		if err := s.repo.EncerrarConferencia(ctx, id, StatusCancelada, antes.Nota, atorOuNulo(ctx)); err != nil {
			return err
		}
		depois := antes
		depois.Status = StatusCancelada
		return audit.Alteracao(ctx, s.repo.pool, entidadeConferencia, verboCancelada, id, antes, depois)
	})
}

// ContarLinha é o gesto do celular: um número, e a resposta já traz o rodapé.
//
// A conferência é travada FOR SHARE: contagens simultâneas de linhas diferentes
// não se bloqueiam, mas TODAS esperam um fechamento em curso e, depois dele,
// leem `fechada` e recebem COUNT_CLOSED. Sem a trava, a contagem que chegasse
// no meio do fechamento entraria numa conferência já apurada.
func (s *Service) ContarLinha(ctx context.Context, conferencia, linha uuid.UUID, c ContagemDaLinha) (LinhaContada, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return LinhaContada{}, err
	}
	var out LinhaContada
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		estado, err := s.repo.TravarConferencia(ctx, prop, conferencia, false)
		if err != nil {
			return err
		}
		if estado.Status != StatusAberta {
			return encerrada(estado)
		}
		antes, err := s.repo.TravarLinha(ctx, conferencia, linha)
		if err != nil {
			return err
		}

		// `counted_qty` ausente não mexe na contagem (só a nota muda); presente,
		// grava ou — com `null` — desfaz, e o instante e o autor seguem junto.
		depois := antes
		depois.Nota = textoDoOpt(c.Nota, antes.Nota)
		mexer := c.QtdContada.Set
		if mexer {
			depois.QtdContada, depois.ContadaPor = nil, nil
			if v, ok := c.QtdContada.Definido(); ok {
				depois.QtdContada, depois.ContadaPor = &v, atorOuNulo(ctx)
			}
		}
		if err := s.repo.GravarContagem(ctx, linha, mexer, depois.QtdContada, depois.Nota, depois.ContadaPor); err != nil {
			return err
		}

		if out.Linha, err = s.repo.BuscarLinha(ctx, conferencia, linha); err != nil {
			return err
		}
		if out.Progresso, err = s.repo.ProgressoDaConferencia(ctx, conferencia); err != nil {
			return err
		}
		depois.ContadaEm = out.Linha.ContadaEm
		return audit.Alteracao(ctx, s.repo.pool, entidadeLinha, verboContada, linha, antes, depois)
	})
	return out, err
}

// FecharConferencia apura a divergência e encerra.
//
// À prova de corrida: a conferência é travada FOR UPDATE antes de tudo. Duas
// abas fechando ao mesmo tempo se enfileiram — a segunda relê `fechada` e
// recebe COUNT_CLOSED, em vez de apurar de novo e abrir as avarias em dobro —,
// e nenhuma contagem entra enquanto a apuração roda (ContarLinha trava
// FOR SHARE, que espera este FOR UPDATE).
//
// Fechar exige ter contado tudo: pendente é NULL, e "esperava 12, contou nada"
// não pode virar "esperava 12, não achei nenhum" no relatório. Quem não vai
// terminar CANCELA.
//
// O custo de reposição é CONGELADO nas linhas aqui, e a perda sai dele. Cada
// FALTA abre uma avaria `faltando` com `count_id` desta conferência (o vínculo
// que transforma divergência em cobrança), salvo `raise_issues: false`. Sobra
// não abre nada.
func (s *Service) FecharConferencia(ctx context.Context, id uuid.UUID, p PedidoDeFechamento) (ResultadoDoFechamento, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return ResultadoDoFechamento{}, err
	}
	abrirAvarias := valorOu(p.AbrirAvarias, true)

	var out ResultadoDoFechamento
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarConferencia(ctx, prop, id, true)
		if err != nil {
			return err
		}
		if antes.Status != StatusAberta {
			return encerrada(antes)
		}

		pendencias, err := s.repo.PendenciasPorAmbiente(ctx, id)
		if err != nil {
			return err
		}
		if len(pendencias) > 0 {
			total := 0
			for _, pd := range pendencias {
				total += pd.Pendentes
			}
			return apperr.CountHasPendingLines.WithDetails(map[string]any{
				"pending":         total,
				"pending_by_room": pendencias,
			})
		}

		// Congela o custo de reposição em todas as linhas ANTES de valorar a
		// perda: daqui em diante a apuração só lê a linha.
		if err := s.repo.CongelarCustos(ctx, id); err != nil {
			return err
		}
		divergentes, err := s.repo.LinhasDivergentes(ctx, id)
		if err != nil {
			return err
		}
		por := atorOuNulo(ctx)
		if abrirAvarias {
			for _, l := range divergentes {
				falta := apurar(l).Falta
				if falta == 0 {
					continue
				}
				avaria := avariaGravada{
					AmbienteID: l.AmbienteID, BemID: l.BemID, Tipo: AvariaFaltando, Qtd: falta, ConferenciaID: &id,
				}
				if avaria.ID, err = s.repo.CriarAvaria(ctx, prop, avaria, por); err != nil {
					return err
				}
				if err := audit.Criacao(ctx, s.repo.pool, entidadeAvaria, audit.VerboCriado, avaria.ID, avaria); err != nil {
					return err
				}
			}
		}

		nota := textoDoOpt(p.Nota, antes.Nota)
		if err := s.repo.EncerrarConferencia(ctx, id, StatusFechada, nota, por); err != nil {
			return err
		}
		depois := antes
		depois.Status, depois.Nota = StatusFechada, nota
		if err := audit.Alteracao(ctx, s.repo.pool, entidadeConferencia, verboFechada, id, antes, depois); err != nil {
			return err
		}

		// A resposta é a mesma leitura de volta do `GET` (`result`), dentro
		// desta transação: o `closed_at` recém-gravado é o que reconhece as
		// avarias nascidas aqui.
		if out.Conferencia, err = s.repo.BuscarConferencia(ctx, prop, id); err != nil {
			return err
		}
		apurado, err := s.apuracao(ctx, id)
		if err != nil {
			return err
		}
		out.Divergencias, out.AvariasCriadas = apurado.Divergencias, apurado.AvariasCriadas
		return nil
	})
	if err != nil {
		return ResultadoDoFechamento{}, err
	}
	return out, nil
}
