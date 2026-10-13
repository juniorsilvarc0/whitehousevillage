package bens

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Transacionador é o que o service precisa do db.TxManager.
type Transacionador interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

var _ Transacionador = (*db.TxManager)(nil)

// Service orquestra: transação, escopo de propriedade, trilha. A transação
// nasce e morre aqui; o repositório nunca a abre.
type Service struct {
	repo *Repository
	tx   Transacionador
	dir  string
}

// NewService monta o service. `dir` é a pasta das fotos de bens, já dentro do
// volume de mídia (ver NovoHandler).
func NewService(repo *Repository, tx Transacionador, dir string) *Service {
	return &Service{repo: repo, tx: tx, dir: dir}
}

// propriedadeDoAtor resolve a casa em que a requisição opera. O módulo inteiro
// filtra por ela — e aqui ela é a única barreira entre as casas, porque
// `room_inventory` e `inventory_count_lines` não têm `property_id`. O recurso
// não tem escopo `own` (cômodo não tem dono).
func propriedadeDoAtor(ctx context.Context) (uuid.UUID, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return uuid.Nil, apperr.Unauthorized
	}
	if u.PropertyID == uuid.Nil {
		return uuid.Nil, apperr.Internal.WithCause(fmt.Errorf("usuário %s sem property_id: sessão incompleta", u.ID))
	}
	return u.PropertyID, nil
}

// atorOuNulo é o autor das colunas `*_by`. Nulo fora de requisição (script),
// que é o que as colunas anuláveis aceitam.
func atorOuNulo(ctx context.Context) *uuid.UUID {
	if id, ok := auth.UsuarioID(ctx); ok {
		return &id
	}
	return nil
}

// ═══════════════════════════ Ambientes ════════════════════════════════════

func (s *Service) ListarAmbientes(ctx context.Context, f FiltroDeAmbientes) ([]Ambiente, int64, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarAmbientes(ctx, prop, f)
}

func (s *Service) BuscarAmbiente(ctx context.Context, id uuid.UUID) (Ambiente, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Ambiente{}, err
	}
	return s.repo.BuscarAmbiente(ctx, prop, id)
}

// CriarAmbiente grava o cômodo na unidade. Unidade de outra casa é 422 (e não
// 404): o recurso pedido é o cômodo, e é o campo `unit_id` que está errado.
func (s *Service) CriarAmbiente(ctx context.Context, c AmbienteCriar) (Ambiente, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Ambiente{}, err
	}
	novo := ambienteGravado{
		UnidadeID: c.UnidadeID, Nome: strings.TrimSpace(c.Nome), Tipo: c.Tipo,
		Ordem: valorOu(c.Ordem, 0), Ativo: valorOu(c.Ativo, true),
	}

	var criado Ambiente
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if _, ok, err := s.repo.UnidadeDaPropriedade(ctx, prop, c.UnidadeID); err != nil {
			return err
		} else if !ok {
			return apperr.Validation(map[string]string{"unit_id": "unidade não encontrada nesta propriedade."})
		}
		id, err := s.inserirComCodigo(ctx, prop, &novo, c.Codigo)
		if err != nil {
			return err
		}
		novo.ID = id
		if criado, err = s.repo.BuscarAmbiente(ctx, prop, id); err != nil {
			return err
		}
		return audit.Criacao(ctx, s.repo.pool, entidadeAmbiente, audit.VerboCriado, id, novo)
	})
	return criado, err
}

// inserirComCodigo grava o cômodo com o `code` informado ou, sem ele, com o
// derivado do `name` (BaseDoCodigo), tentando `base`, `base-2`, `base-3`… até
// achar um livre na unidade. Quem decide "livre" é a constraint
// `unit_rooms_code_unico` a cada tentativa (CriarAmbiente) — sem SELECT antes,
// que perderia a corrida para outra aba criando o mesmo "Quarto" no mesmo
// instante. Código informado e já usado é 409: o cliente escolheu aquele.
func (s *Service) inserirComCodigo(ctx context.Context, prop uuid.UUID, novo *ambienteGravado, informado *string) (uuid.UUID, error) {
	if informado != nil {
		novo.Codigo = *informado
		id, ok, err := s.repo.CriarAmbiente(ctx, prop, *novo)
		if err != nil {
			return uuid.Nil, err
		}
		if !ok {
			return uuid.Nil, apperr.CodeInUse.
				WithMessage("Já existe um ambiente com este código nesta unidade.").
				WithDetails(map[string]string{"code": "já existe um ambiente com este código nesta unidade."})
		}
		return id, nil
	}

	base := BaseDoCodigo(novo.Nome)
	for n := 1; n <= tentativasDeSufixo; n++ {
		novo.Codigo = CandidatoDoCodigo(base, n)
		id, ok, err := s.repo.CriarAmbiente(ctx, prop, *novo)
		if err != nil {
			return uuid.Nil, err
		}
		if ok {
			return id, nil
		}
	}
	return uuid.Nil, apperr.CodeInUse.
		WithMessage("Não achei um código livre para este ambiente nesta unidade; informe um em `code`.").
		WithDetails(map[string]string{"code": fmt.Sprintf("%s até %s já estão em uso nesta unidade.",
			base, CandidatoDoCodigo(base, tentativasDeSufixo))})
}

// SubstituirAmbiente é o PUT: os quatro campos editáveis, ausente volta ao
// padrão do schema. `unit_id` nem chega aqui (campo desconhecido, 422).
func (s *Service) SubstituirAmbiente(ctx context.Context, id uuid.UUID, c AmbienteSubstituir) (Ambiente, error) {
	return s.gravarAmbiente(ctx, id, func(a *ambienteGravado) {
		a.Nome, a.Tipo = strings.TrimSpace(c.Nome), c.Tipo
		a.Ordem, a.Ativo = valorOu(c.Ordem, 0), valorOu(c.Ativo, true)
	})
}

// AtualizarAmbiente é o PATCH. `active: false` é como se tira de linha um
// cômodo que tem histórico.
func (s *Service) AtualizarAmbiente(ctx context.Context, id uuid.UUID, p AmbienteAtualizar) (Ambiente, error) {
	return s.gravarAmbiente(ctx, id, func(a *ambienteGravado) {
		if v, ok := p.Nome.Definido(); ok {
			a.Nome = strings.TrimSpace(v)
		}
		if v, ok := p.Tipo.Definido(); ok {
			a.Tipo = v
		}
		if v, ok := p.Ordem.Definido(); ok {
			a.Ordem = v
		}
		if v, ok := p.Ativo.Definido(); ok {
			a.Ativo = v
		}
	})
}

// gravarAmbiente trava a linha, aplica a mudança, grava e audita — o mesmo
// caminho para PUT e PATCH, para a trilha registrar a decisão e não o verbo.
func (s *Service) gravarAmbiente(ctx context.Context, id uuid.UUID, mudar func(*ambienteGravado)) (Ambiente, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Ambiente{}, err
	}
	var gravado Ambiente
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarAmbiente(ctx, prop, id)
		if err != nil {
			return err
		}
		depois := antes
		mudar(&depois)
		if err := s.repo.GravarAmbiente(ctx, prop, depois); err != nil {
			return err
		}
		if gravado, err = s.repo.BuscarAmbiente(ctx, prop, id); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.pool, entidadeAmbiente, verboDaAtivacao(antes.Ativo, depois.Ativo), id, antes, depois)
	})
	return gravado, err
}

// ApagarAmbiente apaga DE VERDADE o cômodo sem histórico — o "Quarto 2" criado
// duas vezes no celular. Com histórico, quem recusa é a FK RESTRICT de
// `inventory_count_lines`/`inventory_issues`, nunca uma contagem prévia; o
// 409 só é ENRIQUECIDO depois, com o que segura e o caminho certo.
func (s *Service) ApagarAmbiente(ctx context.Context, id uuid.UUID) error {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return err
	}
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarAmbiente(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.ApagarAmbiente(ctx, prop, id); err != nil {
			return err
		}
		return audit.Exclusao(ctx, s.repo.pool, entidadeAmbiente, audit.VerboExcluido, id, antes)
	})
	if !errors.Is(err, errEmUso) {
		return err
	}

	// A transação já foi desfeita; os números vêm de uma leitura nova. São
	// informação para a tela — a decisão foi da FK, e ela não muda.
	v, errContagem := s.repo.VinculosDoAmbiente(ctx, id)
	if errContagem != nil {
		return errContagem
	}
	return apperr.ResourceInUse.
		WithMessage("Este ambiente já entrou em conferência, avaria ou ordem de manutenção e não pode ser apagado. " +
			`Para tirá-lo de linha sem apagar o histórico, desative-o (PATCH {"active": false}).`).
		WithCause(err).
		WithDetails(v)
}

// ═══════════════════════════ Bens ═════════════════════════════════════════

func (s *Service) ListarBens(ctx context.Context, f FiltroDeBens) ([]Bem, int64, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarBens(ctx, prop, f)
}

// BuscarBem monta a ficha: o bem, a galeria e onde ele está — "onde está este
// prato" é a segunda pergunta de quem abre a ficha.
func (s *Service) BuscarBem(ctx context.Context, id uuid.UUID) (BemCompleto, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return BemCompleto{}, err
	}
	b, err := s.repo.BuscarBem(ctx, prop, id)
	if err != nil {
		return BemCompleto{}, err
	}
	fotos, err := s.repo.FotosDoBem(ctx, id)
	if err != nil {
		return BemCompleto{}, err
	}
	colocacoes, err := s.repo.ColocacoesDoBem(ctx, prop, id)
	if err != nil {
		return BemCompleto{}, err
	}
	return BemCompleto{Bem: b, Fotos: fotos, Colocacoes: colocacoes}, nil
}

// bemDoCorpo aplica os padrões do schema ao corpo do POST/PUT.
func bemDoCorpo(c BemCriar) bemGravado {
	return bemGravado{
		Nome:                  strings.TrimSpace(c.Nome),
		Descricao:             textoOuNulo(c.Descricao),
		Categoria:             c.Categoria,
		Medida:                valorOu(c.Medida, medidaPadrao),
		CustoDeReposicaoCents: c.CustoDeReposicaoCents,
		Ativo:                 valorOu(c.Ativo, true),
	}
}

func (s *Service) CriarBem(ctx context.Context, c BemCriar) (Bem, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Bem{}, err
	}
	novo := bemDoCorpo(c)

	var criado Bem
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.CriarBem(ctx, prop, novo)
		if err != nil {
			return err
		}
		novo.ID = id
		if criado, err = s.repo.BuscarBem(ctx, prop, id); err != nil {
			return err
		}
		return audit.Criacao(ctx, s.repo.pool, entidadeBem, audit.VerboCriado, id, novo)
	})
	return criado, err
}

// SubstituirBem é o PUT. Corrige em TODOS os ambientes de todas as unidades de
// uma vez — é a razão de o item ser catálogo.
func (s *Service) SubstituirBem(ctx context.Context, id uuid.UUID, c BemCriar) (Bem, error) {
	return s.gravarBem(ctx, id, func(b *bemGravado) {
		novo := bemDoCorpo(c)
		novo.ID = b.ID
		*b = novo
	})
}

// AtualizarBem é o PATCH: `null` limpa a descrição e devolve o custo a "não
// cotado".
func (s *Service) AtualizarBem(ctx context.Context, id uuid.UUID, p BemAtualizar) (Bem, error) {
	return s.gravarBem(ctx, id, func(b *bemGravado) {
		if v, ok := p.Nome.Definido(); ok {
			b.Nome = strings.TrimSpace(v)
		}
		b.Descricao = textoDoOpt(p.Descricao, b.Descricao)
		if v, ok := p.Categoria.Definido(); ok {
			b.Categoria = v
		}
		if v, ok := p.Medida.Definido(); ok {
			b.Medida = v
		}
		if p.CustoDeReposicaoCents.Set {
			if v, ok := p.CustoDeReposicaoCents.Definido(); ok {
				b.CustoDeReposicaoCents = &v
			} else {
				b.CustoDeReposicaoCents = nil
			}
		}
		if v, ok := p.Ativo.Definido(); ok {
			b.Ativo = v
		}
	})
}

func (s *Service) gravarBem(ctx context.Context, id uuid.UUID, mudar func(*bemGravado)) (Bem, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Bem{}, err
	}
	var gravado Bem
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarBem(ctx, prop, id)
		if err != nil {
			return err
		}
		depois := antes
		mudar(&depois)
		if err := s.repo.GravarBem(ctx, prop, depois); err != nil {
			return err
		}
		if gravado, err = s.repo.BuscarBem(ctx, prop, id); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.pool, entidadeBem, verboDaAtivacao(antes.Ativo, depois.Ativo), id, antes, depois)
	})
	return gravado, err
}

// ApagarBem apaga o item nunca colocado, conferido, avariado nem citado por
// ordem de manutenção. Quem recusa é a FK RESTRICT (`room_inventory`,
// `inventory_count_lines`, `inventory_issues`, `maintenance_orders`); os
// `details` separam os motivos, e a mensagem diz o caminho de cada um.
func (s *Service) ApagarBem(ctx context.Context, id uuid.UUID) error {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return err
	}
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarBem(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.ApagarBem(ctx, prop, id); err != nil {
			return err
		}
		return audit.Exclusao(ctx, s.repo.pool, entidadeBem, audit.VerboExcluido, id, antes)
	})
	if !errors.Is(err, errEmUso) {
		return err
	}

	v, errContagem := s.repo.VinculosDoBem(ctx, id)
	if errContagem != nil {
		return errContagem
	}
	msg := "Este bem ainda está colocado em algum ambiente. Tire-o dos ambientes antes de apagar, " +
		`ou desative-o (PATCH {"active": false}).`
	if v.LinhasDeConferencia > 0 || v.Avarias > 0 || v.OrdensDeManutencao > 0 {
		msg = "Este bem já entrou em conferência, avaria ou ordem de manutenção e não pode ser apagado. " +
			`Para tirá-lo de linha sem apagar o histórico, desative-o (PATCH {"active": false}).`
	}
	return apperr.ResourceInUse.WithMessage(msg).WithCause(err).WithDetails(v)
}

// ─────────────────────────── Galeria ────────────────────────────────────────

func (s *Service) FotosDoBem(ctx context.Context, id uuid.UUID) ([]FotoDoBem, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, err
	}
	if ok, err := s.repo.BemDaPropriedade(ctx, prop, id); err != nil {
		return nil, err
	} else if !ok {
		return nil, apperr.NotFound("Bem")
	}
	return s.repo.FotosDoBem(ctx, id)
}

// galeriaAuditada é o retrato da galeria para a trilha (objeto, não lista nua).
type galeriaAuditada struct {
	MidiaIDs []uuid.UUID `json:"media_ids"`
}

// SubstituirFotos troca a galeria inteira. Lista vazia é aceita; foto de outra
// casa ou inexistente é 422 com o índice; desvincular não apaga o arquivo.
func (s *Service) SubstituirFotos(ctx context.Context, id uuid.UUID, g GaleriaDoBem) ([]FotoDoBem, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, err
	}
	var fotos []FotoDoBem
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		// A trava do bem enfileira duas trocas simultâneas da mesma galeria.
		if _, err := s.repo.TravarBem(ctx, prop, id); err != nil {
			return err
		}
		existentes, err := s.repo.MidiasDaPropriedade(ctx, prop, g.MidiaIDs)
		if err != nil {
			return err
		}
		faltando := map[string]string{}
		for i, mid := range g.MidiaIDs {
			if !existentes[mid] {
				faltando[fmt.Sprintf("media_ids[%d]", i)] = "foto não encontrada nesta propriedade. Envie o arquivo de novo."
			}
		}
		if len(faltando) > 0 {
			return apperr.Validation(faltando)
		}

		antes, err := s.repo.FotosDoBem(ctx, id)
		if err != nil {
			return err
		}
		if err := s.repo.SubstituirFotos(ctx, id, g.MidiaIDs); err != nil {
			return err
		}
		if fotos, err = s.repo.FotosDoBem(ctx, id); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.pool, entidadeBem, "fotos_substituidas", id,
			galeriaAuditada{MidiaIDs: idsDasFotos(antes)}, galeriaAuditada(g))
	})
	return fotos, err
}

func idsDasFotos(fotos []FotoDoBem) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(fotos))
	for _, f := range fotos {
		out = append(out, f.Midia.ID)
	}
	return out
}

// ═══════════════════════════ Colocações ═══════════════════════════════════

func (s *Service) ListarColocacoes(ctx context.Context, f FiltroDeColocacoes) ([]Colocacao, int64, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarColocacoes(ctx, prop, f)
}

func (s *Service) BuscarColocacao(ctx context.Context, ambiente, bem uuid.UUID) (Colocacao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Colocacao{}, err
	}
	return s.repo.BuscarColocacao(ctx, prop, ambiente, bem)
}

// CriarColocacao põe o bem no cômodo. As duas pontas têm de ser DESTA casa —
// e é esta conferência, e não o banco, que impede a mistura (`room_inventory`
// não tem `property_id`). A colisão do par é decidida pela chave primária.
func (s *Service) CriarColocacao(ctx context.Context, c ColocacaoCriar) (Colocacao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Colocacao{}, err
	}
	nova := colocacaoGravada{AmbienteID: c.AmbienteID, BemID: c.BemID, QtdEsperada: *c.QtdEsperada, Nota: textoOuNulo(c.Nota)}

	var criada Colocacao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.exigirPontasDaPropriedade(ctx, prop, c.AmbienteID, c.BemID); err != nil {
			return err
		}
		ok, existente, err := s.repo.CriarColocacao(ctx, nova)
		if err != nil {
			return err
		}
		if !ok {
			conflito := apperr.CodeInUse.WithMessage("Este bem já está colocado neste ambiente: para mudar a quantidade, edite a colocação que já existe.")
			if existente != nil {
				conflito = conflito.WithDetails(map[string]any{
					"id":           ChaveDaColocacao(c.AmbienteID, c.BemID),
					"expected_qty": *existente,
				})
			}
			return conflito
		}
		if criada, err = s.repo.BuscarColocacao(ctx, prop, c.AmbienteID, c.BemID); err != nil {
			return err
		}
		return audit.Criacao(ctx, s.repo.pool, entidadeColocacao, audit.VerboCriado, c.AmbienteID, nova)
	})
	return criada, err
}

// exigirPontasDaPropriedade devolve 422 com o campo certo quando o cômodo ou o
// bem não existe nesta casa.
func (s *Service) exigirPontasDaPropriedade(ctx context.Context, prop, ambiente, bem uuid.UUID) error {
	erros := map[string]string{}
	if _, ok, err := s.repo.AmbienteDaPropriedade(ctx, prop, ambiente); err != nil {
		return err
	} else if !ok {
		erros["room_id"] = "ambiente não encontrado nesta propriedade."
	}
	if ok, err := s.repo.BemDaPropriedade(ctx, prop, bem); err != nil {
		return err
	} else if !ok {
		erros["item_id"] = "bem não encontrado nesta propriedade."
	}
	if len(erros) > 0 {
		return apperr.Validation(erros)
	}
	return nil
}

// SubstituirColocacao é o PUT: `expected_qty` e `note`.
func (s *Service) SubstituirColocacao(ctx context.Context, ambiente, bem uuid.UUID, c ColocacaoSubstituir) (Colocacao, error) {
	return s.gravarColocacao(ctx, ambiente, bem, func(g *colocacaoGravada) {
		g.QtdEsperada, g.Nota = *c.QtdEsperada, textoOuNulo(c.Nota)
	})
}

// AtualizarColocacao é o PATCH. Mudar `expected_qty` NÃO reescreve conferência
// nenhuma: a linha congelada continua dizendo o que se esperava naquele dia.
func (s *Service) AtualizarColocacao(ctx context.Context, ambiente, bem uuid.UUID, p ColocacaoAtualizar) (Colocacao, error) {
	return s.gravarColocacao(ctx, ambiente, bem, func(g *colocacaoGravada) {
		if v, ok := p.QtdEsperada.Definido(); ok {
			g.QtdEsperada = v
		}
		g.Nota = textoDoOpt(p.Nota, g.Nota)
	})
}

func (s *Service) gravarColocacao(ctx context.Context, ambiente, bem uuid.UUID, mudar func(*colocacaoGravada)) (Colocacao, error) {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return Colocacao{}, err
	}
	var gravada Colocacao
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarColocacao(ctx, prop, ambiente, bem)
		if err != nil {
			return err
		}
		depois := antes
		mudar(&depois)
		if err := s.repo.GravarColocacao(ctx, depois); err != nil {
			return err
		}
		if gravada, err = s.repo.BuscarColocacao(ctx, prop, ambiente, bem); err != nil {
			return err
		}
		return audit.Alteracao(ctx, s.repo.pool, entidadeColocacao, audit.VerboAlterado, ambiente, antes, depois)
	})
	return gravada, err
}

// ApagarColocacao tira o bem do ambiente. Não apaga histórico nem o bem.
func (s *Service) ApagarColocacao(ctx context.Context, ambiente, bem uuid.UUID) error {
	prop, err := propriedadeDoAtor(ctx)
	if err != nil {
		return err
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.TravarColocacao(ctx, prop, ambiente, bem)
		if err != nil {
			return err
		}
		if err := s.repo.ApagarColocacao(ctx, ambiente, bem); err != nil {
			return err
		}
		return audit.Exclusao(ctx, s.repo.pool, entidadeColocacao, audit.VerboExcluido, ambiente, antes)
	})
}

// verboDaAtivacao separa "desativado"/"reativado" de "alterado" na trilha:
// "quem tirou o cômodo de linha?" é UMA consulta por `action`, não um LIKE no
// documento.
func verboDaAtivacao(antes, depois bool) string {
	switch {
	case antes && !depois:
		return "desativado"
	case !antes && depois:
		return "reativado"
	default:
		return audit.VerboAlterado
	}
}
