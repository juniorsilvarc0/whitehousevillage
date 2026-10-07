package bens

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// ListarUnidades é o seletor de unidade das telas de bens. Existe porque
// `/units` exige `inventory:ver` (cadastro comercial), e quem só tem
// `inventory.goods` não teria onde escolher a unidade.
func (s *Service) ListarUnidades(ctx context.Context, f FiltroDeUnidades) ([]UnidadeDoInventario, int64, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarUnidadesDoInventario(ctx, prop, f)
}

// InventarioDaUnidade monta a tela da unidade numa chamada: cômodos na ordem
// de caminhada, os bens de cada um por nome, os totais do que foi exibido, a
// conferência aberta e a última fechada. Da unidade sai só o título.
func (s *Service) InventarioDaUnidade(ctx context.Context, id uuid.UUID, f FiltroDaUnidade) (InventarioDaUnidade, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return InventarioDaUnidade{}, err
	}
	unidade, ok, err := s.repo.UnidadeDaPropriedade(ctx, prop, id)
	if err != nil {
		return InventarioDaUnidade{}, err
	}
	if !ok {
		return InventarioDaUnidade{}, apperr.NotFound("Unidade")
	}

	ambientes, err := s.repo.AmbientesDaUnidade(ctx, prop, id, f.IncluirInativos)
	if err != nil {
		return InventarioDaUnidade{}, err
	}
	colocacoes, err := s.repo.ColocacoesDaUnidade(ctx, prop, id, f)
	if err != nil {
		return InventarioDaUnidade{}, err
	}
	porAmbiente := map[uuid.UUID][]Colocacao{}
	for _, c := range colocacoes {
		porAmbiente[c.AmbienteID] = append(porAmbiente[c.AmbienteID], c)
	}

	out := InventarioDaUnidade{Unidade: unidade, Ambientes: []AmbienteDoInventario{}}
	for _, a := range ambientes {
		bens := porAmbiente[a.ID]
		if f.Busca != "" && len(bens) == 0 {
			// Busca pelo nome do bem: cômodo sem resultado sai da resposta.
			continue
		}
		if bens == nil {
			bens = []Colocacao{}
		}
		out.Ambientes = append(out.Ambientes, AmbienteDoInventario{Ambiente: a, Bens: bens})
	}
	out.Totais = totalizar(out.Ambientes)

	if out.ConferenciaAberta, err = s.repo.ConferenciaAbertaDaUnidade(ctx, prop, id); err != nil {
		return InventarioDaUnidade{}, err
	}
	if out.UltimaFechada, err = s.repo.UltimaFechadaDaUnidade(ctx, prop, id); err != nil {
		return InventarioDaUnidade{}, err
	}
	return out, nil
}

// copiaAuditada é o resumo da cópia para a trilha.
type copiaAuditada struct {
	Origem     uuid.UUID `json:"source_unit_id"`
	SoComodos  bool      `json:"rooms_only"`
	Ambientes  []string  `json:"rooms_created"`
	Colocacoes int       `json:"placements_created"`
	Mantidas   int       `json:"kept"`
}

// CopiarInventario traz cômodos e colocações de outra unidade, só
// ACRESCENTANDO (ver planejarCopia). `dryRun` calcula o mesmo plano e não grava
// nada — as duas respostas saem da mesma função.
//
// Destino com conferência aberta é 409 COUNT_ALREADY_OPEN: as linhas da
// contagem em curso já estão congeladas, e o que nascesse agora fecharia a
// conferência "completa" sem nunca ter sido olhado. A trava por unidade
// (TravarInventarioDaUnidade) é a mesma da abertura, então "não há conferência
// aberta" continua verdade até o COMMIT da cópia.
func (s *Service) CopiarInventario(ctx context.Context, destino uuid.UUID, p PedidoDeCopia, dryRun bool) (ResultadoDaCopia, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return ResultadoDaCopia{}, err
	}
	if _, ok, err := s.repo.UnidadeDaPropriedade(ctx, prop, destino); err != nil {
		return ResultadoDaCopia{}, err
	} else if !ok {
		return ResultadoDaCopia{}, apperr.NotFound("Unidade")
	}
	if p.OrigemID == destino {
		return ResultadoDaCopia{}, apperr.Validation(map[string]string{
			"source_unit_id": "escolha outra unidade: a origem é a própria unidade de destino.",
		})
	}
	origem, ok, err := s.repo.UnidadeDaPropriedade(ctx, prop, p.OrigemID)
	if err != nil {
		return ResultadoDaCopia{}, err
	}
	if !ok {
		return ResultadoDaCopia{}, apperr.Validation(map[string]string{
			"source_unit_id": "unidade de origem não encontrada nesta propriedade.",
		})
	}
	soComodos := valorOu(p.SoComodos, false)

	var plano planoDeCopia
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.repo.TravarInventarioDaUnidade(ctx, destino); err != nil {
			return err
		}
		aberta, err := s.repo.ConferenciaAbertaDaUnidade(ctx, prop, destino)
		if err != nil {
			return err
		}
		if aberta != nil {
			return apperr.CountAlreadyOpen.
				WithMessage("A unidade de destino tem uma conferência aberta. Feche ou cancele a contagem antes de copiar.").
				WithDetails(conferenciaJaAberta{ID: aberta.ID, AbertaEm: aberta.AbertaEm})
		}

		origemAmb, err := s.repo.AmbientesParaCopia(ctx, prop, origem.ID, true)
		if err != nil {
			return err
		}
		if len(origemAmb) == 0 {
			return apperr.Validation(map[string]string{
				"source_unit_id": fmt.Sprintf("a unidade %s não tem nenhum ambiente ativo para copiar.", origem.Codigo),
			})
		}
		destinoAmb, err := s.repo.AmbientesParaCopia(ctx, prop, destino, false)
		if err != nil {
			return err
		}
		var origemCol, destinoCol []colocacaoDaCopia
		if !soComodos {
			if origemCol, err = s.repo.ColocacoesParaCopia(ctx, prop, origem.ID, true); err != nil {
				return err
			}
			if destinoCol, err = s.repo.ColocacoesParaCopia(ctx, prop, destino, false); err != nil {
				return err
			}
		}
		plano = planejarCopia(origemAmb, destinoAmb, origemCol, destinoCol, soComodos)
		if dryRun {
			return nil
		}

		for _, a := range plano.Ambientes {
			if err := s.repo.CriarAmbienteSeAusente(ctx, prop, destino, a); err != nil {
				return err
			}
		}
		if len(plano.Colocacoes) > 0 {
			ids, err := s.repo.IDsDosAmbientes(ctx, prop, destino)
			if err != nil {
				return err
			}
			for _, c := range plano.Colocacoes {
				ambiente, ok := ids[c.AmbienteNome]
				if !ok {
					return apperr.Internal.WithCause(fmt.Errorf("bens: cômodo %q sumiu do destino no meio da cópia", c.AmbienteNome))
				}
				if err := s.repo.CriarColocacaoSeAusente(ctx, ambiente, c.BemID, c.Qtd); err != nil {
					return err
				}
			}
		}

		nomes := make([]string, 0, len(plano.Ambientes))
		for _, a := range plano.Ambientes {
			nomes = append(nomes, a.Nome)
		}
		return audit.Registrar(ctx, s.repo.pool, audit.Evento{
			Acao:       audit.Acao("units", "inventario_copiado"),
			Entidade:   "units",
			EntidadeID: destino,
			Depois: audit.Snapshot(copiaAuditada{
				Origem: origem.ID, SoComodos: soComodos, Ambientes: nomes,
				Colocacoes: len(plano.Colocacoes), Mantidas: len(plano.Mantidas),
			}),
		})
	})
	if err != nil {
		return ResultadoDaCopia{}, err
	}
	return plano.resultado(dryRun, origem), nil
}

// Exportacao é a planilha pronta para sair: nome do arquivo e o escritor.
type Exportacao struct {
	NomeDoArquivo string
	linhas        []linhaExportada
}

// Escrever grava o CSV em w (ver escreverCSV).
func (e Exportacao) Escrever(w io.Writer) error { return escreverCSV(w, e.linhas) }

// Exportar monta a planilha do recorte. Sem paginação: exportação truncada em
// 25 linhas parece completa, e isso é pior do que nenhuma.
func (s *Service) Exportar(ctx context.Context, f FiltroDeExportacao) (Exportacao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Exportacao{}, err
	}
	slug, hoje, err := s.repo.DadosDoArquivo(ctx, prop)
	if err != nil {
		return Exportacao{}, err
	}

	// O nome do arquivo diz o recorte, como o contrato fixa: o código da
	// unidade quando o filtro é `unit_id`; sem ele, o slug da propriedade —
	// inclusive no recorte só por `room_id`.
	escopo := slug
	if f.UnidadeID != nil {
		u, ok, err := s.repo.UnidadeDaPropriedade(ctx, prop, *f.UnidadeID)
		if err != nil {
			return Exportacao{}, err
		}
		if !ok {
			return Exportacao{}, apperr.NotFound("Unidade")
		}
		escopo = u.Codigo
	}
	if f.AmbienteID != nil {
		if _, ok, err := s.repo.AmbienteDaPropriedade(ctx, prop, *f.AmbienteID); err != nil {
			return Exportacao{}, err
		} else if !ok {
			return Exportacao{}, apperr.NotFound("Ambiente")
		}
	}

	linhas, err := s.repo.LinhasDaExportacao(ctx, prop, f)
	if err != nil {
		return Exportacao{}, err
	}
	return Exportacao{NomeDoArquivo: nomeDoArquivo(escopo, hoje), linhas: linhas}, nil
}
