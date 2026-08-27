package tarifario

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// Entidades como elas aparecem em `audit_log.entity`: o NOME DA TABELA, sempre.
//
// Nome de tabela e não nome de recurso da API (`rate-tables`) porque a trilha é
// lida junto com o banco, e porque é o que `internal/modules/users` já grava.
const (
	entidadeTabela       = "rate_tables"
	entidadeTarifa       = "rates"
	entidadeFeriado      = "holidays"
	entidadePeriodo      = "special_periods"
	entidadeMinimo       = "min_nights_rules"
	entidadePolitica     = "commercial_policies"
	entidadeCancelamento = "cancellation_policies"
)

// Verbos próprios do tarifário, ao lado dos três genéricos do pacote audit.
//
// A grade do bulk é auditada sob a TABELA (`rate_tables.grade_salva`), e não
// célula a célula: a operação que a pessoa fez foi uma só — salvou a grade —, e
// `audit_log(entity, entity_id)` já indexa por tabela, então a pergunta "o que
// aconteceu com a Tabela Comercial V1?" sai de uma consulta. As células mudadas
// vão no `before`/`after` da mesma linha, com a chave `codigo.tipo`.
const (
	verboDesativada = "desativada"
	verboGradeSalva = "grade_salva"
	verboPublicada  = "publicada"
)

// Repositorio é o repositório visto pelo service. Interface, e não o tipo
// concreto, para as regras (versionamento, atomicidade da grade, cruzamento de
// datas) serem testáveis sem Postgres.
type Repositorio interface {
	PropriedadePadrao(ctx context.Context) (uuid.UUID, error)

	ListarTabelas(ctx context.Context, prop uuid.UUID, f FiltroDeTabelas, pagina, porPagina int) ([]TabelaDeTarifas, int64, error)
	BuscarTabela(ctx context.Context, prop, id uuid.UUID) (TabelaDeTarifas, error)
	CriarTabela(ctx context.Context, prop uuid.UUID, e TabelaEntrada) (uuid.UUID, error)
	SubstituirTabela(ctx context.Context, prop, id uuid.UUID, e TabelaEntrada) error
	AtualizarTabela(ctx context.Context, prop, id uuid.UUID, a TabelaAtualizar) error
	DesativarTabela(ctx context.Context, prop, id uuid.UUID) error
	ContarTabelasVigentes(ctx context.Context, prop uuid.UUID, exceto *uuid.UUID) (int, error)

	ListarTarifas(ctx context.Context, prop uuid.UUID, f FiltroDeTarifas, pagina, porPagina int) ([]Tarifa, int64, error)
	GradeDaTabela(ctx context.Context, prop, tabela uuid.UUID) ([]Tarifa, error)
	BuscarTarifa(ctx context.Context, prop, id uuid.UUID) (Tarifa, error)
	CriarTarifa(ctx context.Context, prop uuid.UUID, e TarifaEntrada) (uuid.UUID, error)
	SubstituirTarifa(ctx context.Context, prop, id uuid.UUID, e TarifaEntrada) error
	AtualizarTarifa(ctx context.Context, prop, id uuid.UUID, a TarifaAtualizar) error
	ExcluirTarifa(ctx context.Context, prop, id uuid.UUID) error
	CodigosDosProdutos(ctx context.Context, prop uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error)
	CelulasDoEscopo(ctx context.Context, tabela uuid.UUID, escopo []uuid.UUID) ([]CelulaGravada, error)
	AplicarGrade(ctx context.Context, tabela uuid.UUID, remover []uuid.UUID, gravar []CelulaDaGrade) error

	ListarFeriados(ctx context.Context, prop uuid.UUID, f FiltroDeCalendario, pagina, porPagina int) ([]Feriado, int64, error)
	BuscarFeriado(ctx context.Context, prop, id uuid.UUID) (Feriado, error)
	CriarFeriado(ctx context.Context, prop uuid.UUID, e FeriadoEntrada) (uuid.UUID, error)
	SubstituirFeriado(ctx context.Context, prop, id uuid.UUID, e FeriadoEntrada) error
	AtualizarFeriado(ctx context.Context, prop, id uuid.UUID, a FeriadoAtualizar) error
	ExcluirFeriado(ctx context.Context, prop, id uuid.UUID) error

	ListarPeriodos(ctx context.Context, prop uuid.UUID, f FiltroDeCalendario, pagina, porPagina int) ([]PeriodoEspecial, int64, error)
	BuscarPeriodo(ctx context.Context, prop, id uuid.UUID) (PeriodoEspecial, error)
	CriarPeriodo(ctx context.Context, prop uuid.UUID, e PeriodoEntrada) (uuid.UUID, error)
	SubstituirPeriodo(ctx context.Context, prop, id uuid.UUID, e PeriodoEntrada) error
	AtualizarPeriodo(ctx context.Context, prop, id uuid.UUID, a PeriodoAtualizar) error
	ExcluirPeriodo(ctx context.Context, prop, id uuid.UUID) error

	ListarMinimos(ctx context.Context, prop uuid.UUID, f FiltroDeMinimos, pagina, porPagina int) ([]MinimoDeNoites, int64, error)
	BuscarMinimo(ctx context.Context, prop, id uuid.UUID) (MinimoDeNoites, error)
	CriarMinimo(ctx context.Context, prop uuid.UUID, e MinimoEntrada) (uuid.UUID, error)
	SubstituirMinimo(ctx context.Context, prop, id uuid.UUID, e MinimoEntrada) error
	AtualizarMinimo(ctx context.Context, prop, id uuid.UUID, a MinimoAtualizar) error
	ExcluirMinimo(ctx context.Context, prop, id uuid.UUID) error

	BuscarPoliticaComercialVigente(ctx context.Context, prop uuid.UUID) (PoliticaComercial, error)
	BuscarPoliticaComercialPorVersao(ctx context.Context, prop uuid.UUID, versao int) (PoliticaComercial, error)
	UltimaPoliticaComercial(ctx context.Context, prop uuid.UUID) (PoliticaComercial, bool, error)
	PublicarPoliticaComercial(ctx context.Context, prop uuid.UUID, e PoliticaComercialEntrada, herdado PoliticaComercial) (int, error)

	BuscarPoliticaDeCancelamentoVigente(ctx context.Context, prop uuid.UUID) (PoliticaDeCancelamento, error)
	BuscarPoliticaDeCancelamentoPorVersao(ctx context.Context, prop uuid.UUID, versao int) (PoliticaDeCancelamento, error)
	UltimaPoliticaDeCancelamento(ctx context.Context, prop uuid.UUID) (PoliticaDeCancelamento, bool, error)
	Faixas(ctx context.Context, politica uuid.UUID) ([]FaixaDeCancelamento, error)
	PublicarPoliticaDeCancelamento(ctx context.Context, prop uuid.UUID, e PoliticaDeCancelamentoEntrada) (uuid.UUID, int, error)

	AuditarCriacao(ctx context.Context, entidade, verbo string, id uuid.UUID, depois any) error
	AuditarAlteracao(ctx context.Context, entidade, verbo string, id uuid.UUID, antes, depois any) error
	AuditarExclusao(ctx context.Context, entidade, verbo string, id uuid.UUID, antes any) error
	AuditarEvento(ctx context.Context, ev audit.Evento) error

	travarPublicacao(ctx context.Context, chave int64) error
}

type Service struct {
	repo Repositorio
	tx   auth.Transacionador
}

func NovoService(repo Repositorio, tx auth.Transacionador) *Service {
	return &Service{repo: repo, tx: tx}
}

// propriedade resolve de qual casa é a configuração.
//
// Vem do usuário autenticado, e não de um parâmetro da rota: o tarifário é
// configuração da propriedade a que a pessoa pertence, e aceitar `property_id`
// do cliente daria a qualquer sessão um jeito de reprecificar a casa do vizinho.
// O fallback ao padrão só existe para caminhos sem sessão (seed, teste).
func (s *Service) propriedade(ctx context.Context) (uuid.UUID, error) {
	if u, ok := auth.UserFrom(ctx); ok && u.PropertyID != uuid.Nil {
		return u.PropertyID, nil
	}
	return s.repo.PropriedadePadrao(ctx)
}

// ═══════════════════════ Tabelas de tarifas ═══════════════════════

func (s *Service) ListarTabelas(ctx context.Context, f FiltroDeTabelas, pagina, porPagina int) ([]TabelaDeTarifas, int64, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarTabelas(ctx, prop, f, pagina, porPagina)
}

func (s *Service) BuscarTabela(ctx context.Context, id uuid.UUID) (TabelaDeTarifas, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return TabelaDeTarifas{}, err
	}
	return s.repo.BuscarTabela(ctx, prop, id)
}

func (s *Service) CriarTabela(ctx context.Context, e TabelaEntrada) (TabelaDeTarifas, error) {
	e.Normalizar()
	prop, err := s.propriedade(ctx)
	if err != nil {
		return TabelaDeTarifas{}, err
	}

	var criada TabelaDeTarifas
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.CriarTabela(ctx, prop, e)
		if err != nil {
			return err
		}
		if criada, err = s.repo.BuscarTabela(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarCriacao(ctx, entidadeTabela, audit.VerboCriado, id, criada)
	})
	return criada, err
}

func (s *Service) SubstituirTabela(ctx context.Context, id uuid.UUID, e TabelaEntrada) (TabelaDeTarifas, error) {
	e.Normalizar()
	prop, err := s.propriedade(ctx)
	if err != nil {
		return TabelaDeTarifas{}, err
	}

	var atualizada TabelaDeTarifas
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		// O estado ANTERIOR é lido dentro da transação, antes do UPDATE: é a
		// única cópia do que a trilha vai chamar de `before`.
		antes, err := s.repo.BuscarTabela(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.SubstituirTabela(ctx, prop, id, e); err != nil {
			return err
		}
		if atualizada, err = s.repo.BuscarTabela(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarAlteracao(ctx, entidadeTabela, audit.VerboAlterado, id, antes, atualizada)
	})
	return atualizada, err
}

// AtualizarTabela é o PATCH. A vigência é cruzada com a linha GRAVADA, dentro da
// transação: num PATCH que manda só `valid_to`, o `valid_from` da comparação vem
// do banco, e julgar sem ele deixaria passar uma tabela que termina antes de
// começar.
func (s *Service) AtualizarTabela(ctx context.Context, id uuid.UUID, a TabelaAtualizar) (TabelaDeTarifas, error) {
	a.Normalizar()
	prop, err := s.propriedade(ctx)
	if err != nil {
		return TabelaDeTarifas{}, err
	}

	var atualizada TabelaDeTarifas
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		atual, err := s.repo.BuscarTabela(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := vigenciaCoerente(atual, a); err != nil {
			return err
		}
		if err := s.repo.AtualizarTabela(ctx, prop, id, a); err != nil {
			return err
		}
		if atualizada, err = s.repo.BuscarTabela(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarAlteracao(ctx, entidadeTabela, audit.VerboAlterado, id, atual, atualizada)
	})
	return atualizada, err
}

// vigenciaCoerente confere `valid_to >= valid_from` sobre o estado RESULTANTE.
func vigenciaCoerente(atual TabelaDeTarifas, a TabelaAtualizar) error {
	de := atual.ValidoDe
	if v, ok := a.ValidoDe.Definido(); ok {
		de = v
	}

	ate := atual.ValidoAte
	if v, ok := a.ValidoAte.Definido(); ok {
		ate = &v
	} else if a.ValidoAte.DeveLimpar() {
		ate = nil
	}

	if ate != nil && ate.Before(de.Date) {
		return apperr.Validation(map[string]string{
			"valid_to": fmt.Sprintf("não pode ser anterior a valid_from (%s).", de.String()),
		})
	}
	return nil
}

// DesativarTabela é o DELETE. Recusa desativar a ÚNICA tabela vigente hoje:
// sem tabela em vigor, todo orçamento passa a sair como 422 RATE_NOT_FOUND e a
// casa para de vender sem ninguém ter pedido isso.
func (s *Service) DesativarTabela(ctx context.Context, id uuid.UUID) error {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return err
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		tabela, err := s.repo.BuscarTabela(ctx, prop, id)
		if err != nil {
			return err
		}
		if !tabela.Ativa {
			return nil // já desativada: DELETE é idempotente
		}

		vigentes, err := s.repo.ContarTabelasVigentes(ctx, prop, nil)
		if err != nil {
			return err
		}
		restantes, err := s.repo.ContarTabelasVigentes(ctx, prop, &id)
		if err != nil {
			return err
		}
		// `restantes == 0` com `vigentes > 0` só acontece quando ESTA tabela é a
		// única que cobre hoje.
		if vigentes > 0 && restantes == 0 {
			return ErroRecursoEmUso.
				WithMessage("Esta é a única tabela de tarifas vigente hoje; publique outra antes de desativá-la.").
				WithDetails(map[string]any{"rate_table_id": id, "remaining_active_today": restantes})
		}

		if err := s.repo.DesativarTabela(ctx, prop, id); err != nil {
			return err
		}
		// Auditada como ALTERAÇÃO, não exclusão: a linha continua no banco e a
		// trilha honesta é `active: true → false`. Chamar de exclusão sugeriria
		// que a tabela sumiu, e as reservas antigas ainda apontam para ela.
		desativada := tabela
		desativada.Ativa = false
		return s.repo.AuditarAlteracao(ctx, entidadeTabela, verboDesativada, id, tabela, desativada)
	})
}

// ═══════════════════════ Tarifas ═══════════════════════

func (s *Service) ListarTarifas(ctx context.Context, f FiltroDeTarifas, pagina, porPagina int) ([]Tarifa, int64, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarTarifas(ctx, prop, f, pagina, porPagina)
}

func (s *Service) BuscarTarifa(ctx context.Context, id uuid.UUID) (Tarifa, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return Tarifa{}, err
	}
	return s.repo.BuscarTarifa(ctx, prop, id)
}

func (s *Service) CriarTarifa(ctx context.Context, e TarifaEntrada) (Tarifa, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return Tarifa{}, err
	}

	var criada Tarifa
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.CriarTarifa(ctx, prop, e)
		if err != nil {
			return err
		}
		if criada, err = s.repo.BuscarTarifa(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarCriacao(ctx, entidadeTarifa, audit.VerboCriado, id, criada)
	})
	return criada, err
}

func (s *Service) SubstituirTarifa(ctx context.Context, id uuid.UUID, e TarifaEntrada) (Tarifa, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return Tarifa{}, err
	}

	var atualizada Tarifa
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarTarifa(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.SubstituirTarifa(ctx, prop, id, e); err != nil {
			return err
		}
		if atualizada, err = s.repo.BuscarTarifa(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarAlteracao(ctx, entidadeTarifa, audit.VerboAlterado, id, antes, atualizada)
	})
	return atualizada, err
}

func (s *Service) AtualizarTarifa(ctx context.Context, id uuid.UUID, a TarifaAtualizar) (Tarifa, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return Tarifa{}, err
	}

	var atualizada Tarifa
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarTarifa(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.AtualizarTarifa(ctx, prop, id, a); err != nil {
			return err
		}
		if atualizada, err = s.repo.BuscarTarifa(ctx, prop, id); err != nil {
			return err
		}
		// É esta linha que responde "quem triplicou a tarifa?": o Diff do pacote
		// audit recorta os documentos para `amount_cents`, e a trilha mostra o
		// valor velho e o novo lado a lado.
		return s.repo.AuditarAlteracao(ctx, entidadeTarifa, audit.VerboAlterado, id, antes, atualizada)
	})
	return atualizada, err
}

func (s *Service) ExcluirTarifa(ctx context.Context, id uuid.UUID) error {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return err
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		// Lida antes do DELETE: depois dele a linha não existe mais e o `before`
		// da trilha é a única cópia que sobra da tarifa que foi apagada.
		antes, err := s.repo.BuscarTarifa(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.ExcluirTarifa(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarExclusao(ctx, entidadeTarifa, audit.VerboExcluido, id, antes)
	})
}

// SalvarGrade é o POST /rates/bulk: a grade dos PRODUTOS ENVIADOS, numa
// transação.
//
// # O raio de ação é o produto, não a tabela
//
// Esta é a correção do BAIXO 10. A versão anterior apagava
// `WHERE rate_table_id = $1` e regravava — o escopo era a tabela inteira.
// Medido em 26/08/2026 contra Postgres real: a tela salvou as 6 células de um
// produto e as 18 dos outros três sumiram; `POST /quotes` daqueles produtos
// passou a devolver 422 RATE_NOT_FOUND enquanto `GET /availability` ainda
// anunciava a data com `price_cents: null`. Ninguém pediu para apagar nada, e
// não havia como perceber: o produto apagado não estava na tela.
//
// Agora só os produtos do escopo são lidos, e só eles podem ser tocados. O
// produto que não foi citado não entra em nenhuma das quatro consultas.
//
// # Tudo-ou-nada continua valendo
//
// Salvar célula a célula seriam 24 requisições que podem falhar pela metade e
// deixar a tabela comercial num estado que nunca existiu no papel. O produto
// desconhecido é recusado ANTES de qualquer escrita; qualquer falha depois
// aborta a transação e devolve a grade anterior inteira.
//
// E não reescreve nada já emitido: a reserva guarda `rate_table_id` e o preço de
// cada noite em `reservation_nights`.
func (s *Service) SalvarGrade(ctx context.Context, g GradeEntrada) ([]Tarifa, ResumoDaGrade, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return nil, ResumoDaGrade{}, err
	}

	escopo := g.Escopo()
	resumo := ResumoDaGrade{ProdutoIDs: escopo}

	var grade []Tarifa
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if _, err := s.repo.BuscarTabela(ctx, prop, g.TabelaID); err != nil {
			return err
		}

		codigos, err := s.repo.CodigosDosProdutos(ctx, prop, escopo)
		if err != nil {
			return err
		}
		if err := escopoConhecido(g, escopo, codigos); err != nil {
			return err
		}

		// A leitura vem antes da escrita porque ela é três coisas ao mesmo
		// tempo: a trava sobre as linhas do escopo, o `before` da trilha e a
		// base das contagens que o operador lê em `meta`.
		atuais, err := s.repo.CelulasDoEscopo(ctx, g.TabelaID, escopo)
		if err != nil {
			return err
		}

		plano := planejarGrade(escopo, codigos, atuais, g.Celulas)
		if err := s.repo.AplicarGrade(ctx, g.TabelaID, plano.Remover, plano.Gravar); err != nil {
			return err
		}
		resumo = plano.Resumo

		if err := s.repo.AuditarEvento(ctx, audit.Evento{
			Acao:       audit.Acao(entidadeTabela, verboGradeSalva),
			Entidade:   entidadeTabela,
			EntidadeID: g.TabelaID,
			Antes:      plano.Antes,
			Depois:     plano.Depois,
		}); err != nil {
			return err
		}

		// A resposta é a TABELA INTEIRA, inclusive os produtos que esta chamada
		// não tocou: é o estado que a tela redesenha, e vê-lo por inteiro é o
		// que torna óbvio que o resto continua lá.
		grade, err = s.repo.GradeDaTabela(ctx, prop, g.TabelaID)
		return err
	})
	return grade, resumo, err
}

// escopoConhecido recusa a grade ANTES de qualquer escrita quando algum produto
// do escopo não é desta propriedade.
//
// Descobrir isso no INSERT também abortaria a transação, mas o erro chegaria
// como violação de chave estrangeira — sem dizer QUAL célula da tela está
// errada. E o campo apontado nos `details` é aquele de onde o id veio: a tela
// destaca a lista de escopo quando foi ela que trouxe o id, e a grade quando foi
// a grade.
func escopoConhecido(g GradeEntrada, escopo []uuid.UUID, codigos map[uuid.UUID]string) error {
	if len(codigos) == len(escopo) {
		return nil
	}
	campo := "rates"
	if g.EscopoDeclarado() {
		campo = "unit_type_ids"
	}
	return apperr.Validation(map[string]string{
		campo: "há produto (unit_type_id) que não existe nesta propriedade.",
	})
}

// ═══════════════════════ Feriados ═══════════════════════

func (s *Service) ListarFeriados(ctx context.Context, f FiltroDeCalendario, pagina, porPagina int) ([]Feriado, int64, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarFeriados(ctx, prop, f, pagina, porPagina)
}

func (s *Service) BuscarFeriado(ctx context.Context, id uuid.UUID) (Feriado, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return Feriado{}, err
	}
	return s.repo.BuscarFeriado(ctx, prop, id)
}

func (s *Service) CriarFeriado(ctx context.Context, e FeriadoEntrada) (Feriado, error) {
	e.Normalizar()
	prop, err := s.propriedade(ctx)
	if err != nil {
		return Feriado{}, err
	}

	var criado Feriado
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.CriarFeriado(ctx, prop, e)
		if err != nil {
			return err
		}
		if criado, err = s.repo.BuscarFeriado(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarCriacao(ctx, entidadeFeriado, audit.VerboCriado, id, criado)
	})
	return criado, err
}

func (s *Service) SubstituirFeriado(ctx context.Context, id uuid.UUID, e FeriadoEntrada) (Feriado, error) {
	e.Normalizar()
	prop, err := s.propriedade(ctx)
	if err != nil {
		return Feriado{}, err
	}

	var atualizado Feriado
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarFeriado(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.SubstituirFeriado(ctx, prop, id, e); err != nil {
			return err
		}
		if atualizado, err = s.repo.BuscarFeriado(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarAlteracao(ctx, entidadeFeriado, audit.VerboAlterado, id, antes, atualizado)
	})
	return atualizado, err
}

func (s *Service) AtualizarFeriado(ctx context.Context, id uuid.UUID, a FeriadoAtualizar) (Feriado, error) {
	a.Normalizar()
	prop, err := s.propriedade(ctx)
	if err != nil {
		return Feriado{}, err
	}

	var atualizado Feriado
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarFeriado(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.AtualizarFeriado(ctx, prop, id, a); err != nil {
			return err
		}
		if atualizado, err = s.repo.BuscarFeriado(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarAlteracao(ctx, entidadeFeriado, audit.VerboAlterado, id, antes, atualizado)
	})
	return atualizado, err
}

func (s *Service) ExcluirFeriado(ctx context.Context, id uuid.UUID) error {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return err
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarFeriado(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.ExcluirFeriado(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarExclusao(ctx, entidadeFeriado, audit.VerboExcluido, id, antes)
	})
}

// ═══════════════════════ Períodos especiais ═══════════════════════

func (s *Service) ListarPeriodos(ctx context.Context, f FiltroDeCalendario, pagina, porPagina int) ([]PeriodoEspecial, int64, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarPeriodos(ctx, prop, f, pagina, porPagina)
}

func (s *Service) BuscarPeriodo(ctx context.Context, id uuid.UUID) (PeriodoEspecial, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return PeriodoEspecial{}, err
	}
	return s.repo.BuscarPeriodo(ctx, prop, id)
}

// CriarPeriodo NÃO confere sobreposição, e isso é decisão de negócio, não
// esquecimento: o Réveillon mora dentro da alta temporada de propósito, e a
// precedência de `date_type_rules` é que resolve qual tipo a noite recebe. Uma
// constraint de exclusão aqui tornaria o calendário comercial da casa
// impossível de cadastrar.
func (s *Service) CriarPeriodo(ctx context.Context, e PeriodoEntrada) (PeriodoEspecial, error) {
	e.Normalizar()
	prop, err := s.propriedade(ctx)
	if err != nil {
		return PeriodoEspecial{}, err
	}

	var criado PeriodoEspecial
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.CriarPeriodo(ctx, prop, e)
		if err != nil {
			return err
		}
		if criado, err = s.repo.BuscarPeriodo(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarCriacao(ctx, entidadePeriodo, audit.VerboCriado, id, criado)
	})
	return criado, err
}

func (s *Service) SubstituirPeriodo(ctx context.Context, id uuid.UUID, e PeriodoEntrada) (PeriodoEspecial, error) {
	e.Normalizar()
	prop, err := s.propriedade(ctx)
	if err != nil {
		return PeriodoEspecial{}, err
	}

	var atualizado PeriodoEspecial
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarPeriodo(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.SubstituirPeriodo(ctx, prop, id, e); err != nil {
			return err
		}
		if atualizado, err = s.repo.BuscarPeriodo(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarAlteracao(ctx, entidadePeriodo, audit.VerboAlterado, id, antes, atualizado)
	})
	return atualizado, err
}

func (s *Service) AtualizarPeriodo(ctx context.Context, id uuid.UUID, a PeriodoAtualizar) (PeriodoEspecial, error) {
	a.Normalizar()
	prop, err := s.propriedade(ctx)
	if err != nil {
		return PeriodoEspecial{}, err
	}

	var atualizado PeriodoEspecial
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		atual, err := s.repo.BuscarPeriodo(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := faixaCoerente(atual, a); err != nil {
			return err
		}
		if err := s.repo.AtualizarPeriodo(ctx, prop, id, a); err != nil {
			return err
		}
		if atualizado, err = s.repo.BuscarPeriodo(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarAlteracao(ctx, entidadePeriodo, audit.VerboAlterado, id, atual, atualizado)
	})
	return atualizado, err
}

// faixaCoerente confere `ends_on >= starts_on` sobre o estado resultante — o
// CHECK do banco também barraria, mas como violação de constraint, sem apontar o
// campo que a tela precisa destacar.
func faixaCoerente(atual PeriodoEspecial, a PeriodoAtualizar) error {
	comeca, termina := atual.ComecaEm, atual.TerminaEm
	if v, ok := a.ComecaEm.Definido(); ok {
		comeca = v
	}
	if v, ok := a.TerminaEm.Definido(); ok {
		termina = v
	}
	if termina.Before(comeca.Date) {
		return apperr.Validation(map[string]string{
			"ends_on": fmt.Sprintf("não pode ser anterior a starts_on (%s).", comeca.String()),
		})
	}
	return nil
}

func (s *Service) ExcluirPeriodo(ctx context.Context, id uuid.UUID) error {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return err
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarPeriodo(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.ExcluirPeriodo(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarExclusao(ctx, entidadePeriodo, audit.VerboExcluido, id, antes)
	})
}

// ═══════════════════════ Mínimo de noites ═══════════════════════

func (s *Service) ListarMinimos(ctx context.Context, f FiltroDeMinimos, pagina, porPagina int) ([]MinimoDeNoites, int64, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.ListarMinimos(ctx, prop, f, pagina, porPagina)
}

func (s *Service) BuscarMinimo(ctx context.Context, id uuid.UUID) (MinimoDeNoites, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return MinimoDeNoites{}, err
	}
	return s.repo.BuscarMinimo(ctx, prop, id)
}

func (s *Service) CriarMinimo(ctx context.Context, e MinimoEntrada) (MinimoDeNoites, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return MinimoDeNoites{}, err
	}

	var criado MinimoDeNoites
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		id, err := s.repo.CriarMinimo(ctx, prop, e)
		if err != nil {
			return err
		}
		if criado, err = s.repo.BuscarMinimo(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarCriacao(ctx, entidadeMinimo, audit.VerboCriado, id, criado)
	})
	return criado, err
}

func (s *Service) SubstituirMinimo(ctx context.Context, id uuid.UUID, e MinimoEntrada) (MinimoDeNoites, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return MinimoDeNoites{}, err
	}

	var atualizado MinimoDeNoites
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarMinimo(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.SubstituirMinimo(ctx, prop, id, e); err != nil {
			return err
		}
		if atualizado, err = s.repo.BuscarMinimo(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarAlteracao(ctx, entidadeMinimo, audit.VerboAlterado, id, antes, atualizado)
	})
	return atualizado, err
}

func (s *Service) AtualizarMinimo(ctx context.Context, id uuid.UUID, a MinimoAtualizar) (MinimoDeNoites, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return MinimoDeNoites{}, err
	}

	var atualizado MinimoDeNoites
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarMinimo(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.AtualizarMinimo(ctx, prop, id, a); err != nil {
			return err
		}
		if atualizado, err = s.repo.BuscarMinimo(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarAlteracao(ctx, entidadeMinimo, audit.VerboAlterado, id, antes, atualizado)
	})
	return atualizado, err
}

func (s *Service) ExcluirMinimo(ctx context.Context, id uuid.UUID) error {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return err
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, err := s.repo.BuscarMinimo(ctx, prop, id)
		if err != nil {
			return err
		}
		if err := s.repo.ExcluirMinimo(ctx, prop, id); err != nil {
			return err
		}
		return s.repo.AuditarExclusao(ctx, entidadeMinimo, audit.VerboExcluido, id, antes)
	})
}

// ═══════════════════════ Políticas ═══════════════════════

// PoliticaComercial devolve a vigente ou, com versão, a versão exata — que é
// como a tela de uma reserva antiga mostra a política congelada nela.
func (s *Service) PoliticaComercial(ctx context.Context, versao *int) (PoliticaComercial, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return PoliticaComercial{}, err
	}
	if versao != nil {
		return s.repo.BuscarPoliticaComercialPorVersao(ctx, prop, *versao)
	}
	return s.repo.BuscarPoliticaComercialVigente(ctx, prop)
}

// PublicarPoliticaComercial cria uma VERSÃO NOVA. Nunca edita a vigente.
//
// É a regra 7 do CLAUDE.md aplicada à política: a reserva grava
// `policy_version` na criação e é essa versão que vale para ela até o fim.
// Reescrever a linha vigente mudaria, retroativamente, o sinal, o prazo do saldo
// e a alçada de desconto de toda venda que apontou para aquele número.
//
// Voltar atrás é publicar de novo a versão antiga: ela entra como versão nova e
// deixa rastro, em vez de apagar o que houve.
func (s *Service) PublicarPoliticaComercial(ctx context.Context, e PoliticaComercialEntrada) (PoliticaComercial, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return PoliticaComercial{}, err
	}

	var publicada PoliticaComercial
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.repo.travarPublicacao(ctx, ChaveDaTravaDePoliticaComercial); err != nil {
			return err
		}

		// Lida DEPOIS da trava: antes dela, a última versão é um retrato que
		// outra publicação pode estar reescrevendo neste instante.
		ultima, existe, err := s.repo.UltimaPoliticaComercial(ctx, prop)
		if err != nil {
			return err
		}
		if !existe {
			// Primeira publicação da casa: os campos que o contrato ainda não
			// declara herdam do PADRÃO, não do zero value. Sem isto,
			// `hold_extension_hours = 0` bateria no CHECK do banco e a primeira
			// política seria impossível de publicar.
			ultima = politicaComercialPadrao()
		}
		if existe && e.ValidoDe.Before(ultima.ValidoDe.Date) {
			return ErroPoliticaImutavel.
				WithMessage("A nova versão não pode entrar em vigor antes da última publicada.").
				WithDetails(map[string]any{
					"version":        ultima.Versao,
					"valid_from":     ultima.ValidoDe.String(),
					"requested_from": e.ValidoDe.String(),
				})
		}

		versao, err := s.repo.PublicarPoliticaComercial(ctx, prop, e, ultima)
		if err != nil {
			return err
		}
		if publicada, err = s.repo.BuscarPoliticaComercialPorVersao(ctx, prop, versao); err != nil {
			return err
		}
		// Criação, e não alteração: publicar é gravar uma LINHA NOVA, e a versão
		// anterior continua intacta (regra 7 do CLAUDE.md). Um `before` aqui
		// insinuaria que a política vigente foi reescrita.
		return s.repo.AuditarCriacao(ctx, entidadePolitica, verboPublicada, publicada.ID, publicada)
	})
	return publicada, err
}

// politicaComercialPadrao são os valores de partida dos campos que o corpo do
// PUT pode omitir. Espelham os DEFAULT que a migration 20260826110000 deu às
// colunas — o mesmo número em dois lugares é ruim, mas a alternativa (ler o
// DEFAULT do catálogo do Postgres em tempo de execução) é pior de entender e de
// testar. Quando `hold_extension_hours` entrar no contrato, isto vira exigência
// de campo e some.
func politicaComercialPadrao() PoliticaComercial {
	return PoliticaComercial{ExtensaoDeHoldHoras: 24, ExtensoesDeHoldMax: 1}
}

// PoliticaDeCancelamento devolve o cabeçalho com as faixas ordenadas.
func (s *Service) PoliticaDeCancelamento(ctx context.Context, versao *int) (PoliticaDeCancelamento, error) {
	prop, err := s.propriedade(ctx)
	if err != nil {
		return PoliticaDeCancelamento{}, err
	}

	var pol PoliticaDeCancelamento
	if versao != nil {
		pol, err = s.repo.BuscarPoliticaDeCancelamentoPorVersao(ctx, prop, *versao)
	} else {
		pol, err = s.repo.BuscarPoliticaDeCancelamentoVigente(ctx, prop)
	}
	if err != nil {
		return PoliticaDeCancelamento{}, err
	}

	pol.Faixas, err = s.repo.Faixas(ctx, pol.ID)
	if err != nil {
		return PoliticaDeCancelamento{}, err
	}
	return pol, nil
}

// PublicarPoliticaDeCancelamento cria versão nova COM as faixas inteiras.
//
// As faixas substituem integralmente porque uma faixa sozinha não é uma
// política: a validação de cobertura só faz sentido sobre o conjunto, e um
// endpoint de faixa avulsa permitiria salvar um estado intermediário com buraco
// — que o motor traduziria em retenção total silenciosa.
func (s *Service) PublicarPoliticaDeCancelamento(ctx context.Context, e PoliticaDeCancelamentoEntrada) (PoliticaDeCancelamento, error) {
	e.Normalizar()
	prop, err := s.propriedade(ctx)
	if err != nil {
		return PoliticaDeCancelamento{}, err
	}

	var publicada PoliticaDeCancelamento
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.repo.travarPublicacao(ctx, ChaveDaTravaDePoliticaDeCancelamento); err != nil {
			return err
		}

		ultima, existe, err := s.repo.UltimaPoliticaDeCancelamento(ctx, prop)
		if err != nil {
			return err
		}
		if existe && e.ValidoDe.Before(ultima.ValidoDe.Date) {
			return ErroPoliticaImutavel.
				WithMessage("A nova versão não pode entrar em vigor antes da última publicada.").
				WithDetails(map[string]any{
					"version":        ultima.Versao,
					"valid_from":     ultima.ValidoDe.String(),
					"requested_from": e.ValidoDe.String(),
				})
		}

		id, versao, err := s.repo.PublicarPoliticaDeCancelamento(ctx, prop, e)
		if err != nil {
			return err
		}
		publicada, err = s.repo.BuscarPoliticaDeCancelamentoPorVersao(ctx, prop, versao)
		if err != nil {
			return err
		}
		if publicada.Faixas, err = s.repo.Faixas(ctx, id); err != nil {
			return err
		}
		// Auditada DEPOIS das faixas: política sem faixa não é política, e o
		// `after` da trilha tem de mostrar a versão inteira — cabeçalho e as
		// faixas que decidem quanto se devolve em cada cancelamento.
		return s.repo.AuditarCriacao(ctx, entidadeCancelamento, verboPublicada, id, publicada)
	})
	return publicada, err
}
