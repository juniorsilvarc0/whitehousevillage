package contatos

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/pii"
)

// Servico concentra a regra do módulo. O handler não decide nada e o
// repositório não conhece política.
type Servico struct {
	repo *Repository
	tx   *db.TxManager
}

func NovoServico(repo *Repository, tx *db.TxManager) *Servico {
	return &Servico{repo: repo, tx: tx}
}

// ─────────────────────────── Leitura ────────────────────────────────

// Listar aplica os três filtros que o atendimento usa.
//
// `phone` mal formatado é 422, e NÃO uma busca vazia: "não achei essa pessoa" e
// "você perguntou errado" são respostas diferentes, e o robô de WhatsApp que
// confunde as duas cria um contato novo a cada mensagem.
func (s *Servico) Listar(ctx context.Context, f Filtro) ([]Contato, int64, error) {
	if err := s.exigirEscopoAplicavel(ctx, auth.AcaoVer); err != nil {
		return nil, 0, err
	}

	if f.Telefone != "" {
		e164, err := NormalizarTelefone(f.Telefone)
		if err != nil {
			return nil, 0, apperr.Validation(map[string]string{
				"phone": "informe o telefone em E.164 (+5585999990000) ou como número nacional com DDD.",
			})
		}
		f.Telefone = e164
	}
	if f.Documento != "" {
		// A coluna guarda só dígitos para CPF/CNPJ; o passaporte é
		// alfanumérico em caixa alta. Sem esta normalização, quem digita
		// "123.456.789-09" na busca do balcão não acha ninguém — e conclui que
		// a pessoa não está cadastrada.
		f.Documento = normalizarBuscaDeDocumento(f.Documento)
	}

	return s.repo.Listar(ctx, f)
}

// Buscar devolve a ficha completa e GRAVA `pii_access_log`.
//
// A ordem é deliberada: primeiro a ficha (para o 404 não deixar rastro de
// acesso a algo que não existe), depois o registro do acesso, e só então a
// resposta. Registrar antes do 404 encheria a tabela de leituras de uuid
// inexistente — que é ruído, não trilha.
func (s *Servico) Buscar(ctx context.Context, id uuid.UUID) (ContatoCompleto, error) {
	if err := s.exigirEscopoAplicavel(ctx, auth.AcaoVer); err != nil {
		return ContatoCompleto{}, err
	}

	c, err := s.repo.Buscar(ctx, id)
	if err != nil {
		return ContatoCompleto{}, err
	}
	if err := pii.Registrar(ctx, s.repo.pool, Entidade, id, pii.MotivoFicha); err != nil {
		return ContatoCompleto{}, err
	}
	v, err := s.repo.Vinculos(ctx, id)
	if err != nil {
		return ContatoCompleto{}, err
	}
	return ContatoCompleto{Contato: c, Vinculos: v}, nil
}

// Exportar monta o pacote de portabilidade (LGPD art. 18, V).
func (s *Servico) Exportar(ctx context.Context, id uuid.UUID) (Exportacao, error) {
	if err := s.exigirEscopoAplicavel(ctx, auth.AcaoVer); err != nil {
		return Exportacao{}, err
	}

	c, err := s.repo.Buscar(ctx, id)
	if err != nil {
		return Exportacao{}, err
	}
	// Exportar é a maior leitura de dado pessoal que o sistema faz numa chamada
	// só. Se alguma leitura tem de deixar rastro, é esta.
	if err := pii.Registrar(ctx, s.repo.pool, Entidade, id, pii.MotivoExportacao); err != nil {
		return Exportacao{}, err
	}

	v, err := s.repo.Vinculos(ctx, id)
	if err != nil {
		return Exportacao{}, err
	}
	reservas, leads, oportunidades, atividades, err := s.repo.Exportar(ctx, id)
	if err != nil {
		return Exportacao{}, err
	}

	return Exportacao{
		ExportadoEm: time.Now().UTC(),
		Contato:     ContatoCompleto{Contato: c, Vinculos: v},
		Consentimento: Consentimento{
			BaseLegal:       c.BaseLegal,
			OptInMarketing:  c.OptInMarketing,
			ConsentimentoEm: c.ConsentimentoEm,
		},
		Reservas:      reservas,
		Leads:         leads,
		Oportunidades: oportunidades,
		Atividades:    atividades,
	}, nil
}

// ─────────────────────────── Escrita ────────────────────────────────

// Criar cadastra a pessoa.
func (s *Servico) Criar(ctx context.Context, c Criar) (Contato, error) {
	if err := s.exigirEscopoAplicavel(ctx, auth.AcaoCriar); err != nil {
		return Contato{}, err
	}
	c.Normalizar()

	propriedade, err := s.propriedade(ctx)
	if err != nil {
		return Contato{}, err
	}

	var criado Contato
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.conferirDocumento(ctx, propriedade, c.TipoDeDocumento, c.Documento, uuid.Nil); err != nil {
			return err
		}
		criado, err = s.repo.Criar(ctx, propriedade, c)
		if err != nil {
			return err
		}
		return registrarCriacao(ctx, s.repo.pool, criado)
	})
	if err != nil {
		return Contato{}, s.completarDuplicidade(ctx, err, c.Telefone)
	}
	return criado, nil
}

// Substituir é o PUT: o corpo passa a ser a ficha inteira.
func (s *Servico) Substituir(ctx context.Context, id uuid.UUID, c Criar) (Contato, error) {
	if err := s.exigirEscopoAplicavel(ctx, auth.AcaoEditar); err != nil {
		return Contato{}, err
	}
	c.Normalizar()

	var out Contato
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		atual, err := s.travarEExigirFichaViva(ctx, id)
		if err != nil {
			return err
		}
		propriedade, err := s.repo.PropriedadeDoContato(ctx, id)
		if err != nil {
			return err
		}
		if err := s.conferirDocumento(ctx, propriedade, c.TipoDeDocumento, c.Documento, id); err != nil {
			return err
		}
		if out, err = s.repo.Substituir(ctx, id, c); err != nil {
			return err
		}
		return registrarAlteracao(ctx, s.repo.pool, atual, out)
	})
	if err != nil {
		return Contato{}, s.completarDuplicidade(ctx, err, c.Telefone)
	}
	return out, nil
}

// Atualizar é o PATCH.
func (s *Servico) Atualizar(ctx context.Context, id uuid.UUID, a Atualizar) (Contato, error) {
	if err := s.exigirEscopoAplicavel(ctx, auth.AcaoEditar); err != nil {
		return Contato{}, err
	}
	a.Normalizar()

	var (
		out      Contato
		telefone *string
	)
	if v, ok := a.Telefone.Definido(); ok {
		telefone = &v
	}

	err := s.tx.Do(ctx, func(ctx context.Context) error {
		atual, err := s.travarEExigirFichaViva(ctx, id)
		if err != nil {
			return err
		}

		// Ligar o opt-in exige data de consentimento — no corpo OU já gravada.
		// A checagem precisa do estado atual e por isso não cabe no Validar do
		// DTO, que não tem banco.
		if a.ExigeConsentimento() {
			_, noCorpo := a.ConsentimentoEm.Definido()
			if !noCorpo && atual.ConsentimentoEm == nil {
				return apperr.Validation(map[string]string{
					"consent_at": "é obrigatório para ligar marketing_opt_in: consentimento sem data não é consentimento.",
				})
			}
		}

		propriedade, err := s.repo.PropriedadeDoContato(ctx, id)
		if err != nil {
			return err
		}
		if err := s.conferirDocumentoDoPatch(ctx, propriedade, a, atual, id); err != nil {
			return err
		}
		if out, err = s.repo.Atualizar(ctx, id, a); err != nil {
			return err
		}
		return registrarAlteracao(ctx, s.repo.pool, atual, out)
	})
	if err != nil {
		return Contato{}, s.completarDuplicidade(ctx, err, telefone)
	}
	return out, nil
}

// Excluir apaga o contato que nunca foi usado — e SÓ ele.
//
// Assim que houver qualquer vínculo, a resposta é `409 RESOURCE_IN_USE` com a
// contagem por tipo e o caminho para `/anonymize`. Apagar aqui derrubaria a FK
// `reservations.contact_id`, que é `NOT NULL`: o preço de "sumir com o
// cadastro" seria sumir com a venda.
func (s *Servico) Excluir(ctx context.Context, id uuid.UUID) error {
	if err := s.exigirEscopoAplicavel(ctx, auth.AcaoExcluir); err != nil {
		return err
	}

	return s.tx.Do(ctx, func(ctx context.Context) error {
		// A trava vem ANTES da contagem: entre "contei zero" e "apaguei" cabe
		// uma reserva nascendo em outra transação, e o desfecho seria um 23503
		// traduzido para 422 genérico no lugar do 409 que o contrato promete.
		if err := s.repo.TravarContato(ctx, id); err != nil {
			return err
		}
		atual, err := s.repo.Buscar(ctx, id)
		if err != nil {
			return err
		}
		v, err := s.repo.Vinculos(ctx, id)
		if err != nil {
			return err
		}
		if v.Total() > 0 {
			return RecursoEmUso.WithDetails(map[string]any{
				"references": v,
				"hint": "este contato tem histórico. Para atender ao direito de eliminação sem apagar " +
					"a venda, use POST /contacts/" + id.String() + "/anonymize.",
			})
		}
		if err := s.repo.Excluir(ctx, id); err != nil {
			return err
		}
		return registrarExclusao(ctx, s.repo.pool, atual)
	})
}

// Anonimizar atende ao direito de eliminação (LGPD art. 18, VI) do único jeito
// que sobrevive a uma auditoria fiscal: a pessoa some, a venda fica.
func (s *Servico) Anonimizar(ctx context.Context, id uuid.UUID, p PedidoDeAnonimizacao) (ResultadoDeAnonimizacao, error) {
	if err := s.exigirEscopoAplicavel(ctx, auth.AcaoExcluir); err != nil {
		return ResultadoDeAnonimizacao{}, err
	}
	p.Normalizar()

	var out ResultadoDeAnonimizacao
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.repo.TravarContato(ctx, id); err != nil {
			return err
		}
		atual, err := s.repo.Buscar(ctx, id)
		if err != nil {
			return err
		}
		preservado, err := s.repo.Vinculos(ctx, id)
		if err != nil {
			return err
		}

		// Repetir a chamada devolve 200 com a mesma ficha e NÃO regrava
		// `anonymized_at`. A saída antecipada também evita recusar um no-op por
		// causa de reserva viva — a ficha já está vazia; não há o que eliminar.
		if atual.Anonimizado() {
			out = ResultadoDeAnonimizacao{Contato: atual, Preservado: preservado}
			return nil
		}

		// Não se elimina o dado de quem está hospedado hoje: a operação precisa
		// do nome para entregar a chave, e a base legal enquanto o contrato
		// corre é a EXECUÇÃO DO CONTRATO, não o consentimento. A ficha fica
		// elegível assim que a estadia terminar.
		vivas, err := s.repo.ReservasVivas(ctx, id)
		if err != nil {
			return err
		}
		if len(vivas) > 0 {
			return RecursoEmUso.
				WithMessage("Há reserva em andamento para este contato: a ficha fica elegível quando a estadia terminar.").
				WithDetails(map[string]any{"reservations": vivas})
		}

		anonimo, mudou, err := s.repo.Anonimizar(ctx, id)
		if err != nil {
			return err
		}
		if !mudou {
			// Só chega aqui por corrida perdida para outra anonimização; a
			// ficha atual é a verdade.
			if anonimo, err = s.repo.Buscar(ctx, id); err != nil {
				return err
			}
		} else if err := registrarAnonimizacao(ctx, s.repo.pool, id, p.Motivo, preservado); err != nil {
			return err
		}

		out = ResultadoDeAnonimizacao{Contato: anonimo, Preservado: preservado}
		return nil
	})
	if err != nil {
		return ResultadoDeAnonimizacao{}, err
	}
	return out, nil
}

// ─────────────────────────── Apoio ──────────────────────────────────

// exigirEscopoAplicavel nega o escopo `own` sobre `contacts`. Ver
// escopoOwnInaplicavel: a tabela não tem coluna de dono, e tratar `own` como
// `all` entregaria a base inteira de dados pessoais a quem foi restringido.
func (s *Servico) exigirEscopoAplicavel(ctx context.Context, acao string) error {
	if auth.ScopeOf(ctx, RecursoContatos, acao) == auth.EscopoOwn {
		return escopoOwnInaplicavel(acao)
	}
	return nil
}

// travarEExigirFichaViva trava a linha e recusa a escrita em ficha anonimizada.
//
// Ficha anonimizada que volta a receber dado pessoal seria o pior dos dois
// mundos: `anonymized_at` preenchido (a ficha diz "eliminada") com PII nova
// dentro.
func (s *Servico) travarEExigirFichaViva(ctx context.Context, id uuid.UUID) (Contato, error) {
	if err := s.repo.TravarContato(ctx, id); err != nil {
		return Contato{}, err
	}
	atual, err := s.repo.Buscar(ctx, id)
	if err != nil {
		return Contato{}, err
	}
	if atual.Anonimizado() {
		return Contato{}, ContatoAnonimizado.WithDetails(map[string]any{
			"contact_id":    id,
			"anonymized_at": atual.AnonimizadoEm,
		})
	}
	return atual, nil
}

// conferirDocumento serializa e checa a duplicidade por documento. Ver
// Repository.TravarDocumento para por que a garantia aqui é uma trava e não um
// índice.
func (s *Servico) conferirDocumento(ctx context.Context, propriedade uuid.UUID, tipo, numero *string, exceto uuid.UUID) error {
	if tipo == nil || numero == nil || *numero == "" {
		return nil
	}
	if err := s.repo.TravarDocumento(ctx, propriedade, *tipo, *numero); err != nil {
		return err
	}
	existente, achou, err := s.repo.BuscarIDPorDocumento(ctx, propriedade, *tipo, *numero, exceto)
	if err != nil {
		return err
	}
	if achou {
		return ContatoDuplicado.WithDetails(map[string]any{
			"field":      "doc_number",
			"contact_id": existente,
		})
	}
	return nil
}

// conferirDocumentoDoPatch resolve o par (tipo, número) que VAI VALER depois do
// PATCH antes de conferir a duplicidade.
//
// Conferir só o que veio no corpo erraria nos dois sentidos: um PATCH que muda
// só o número passaria sem tipo, e um PATCH que limpa o número dispararia a
// checagem à toa.
func (s *Servico) conferirDocumentoDoPatch(ctx context.Context, propriedade uuid.UUID, a Atualizar, atual Contato, id uuid.UUID) error {
	tipo := atual.TipoDeDocumento
	numero := atual.Documento

	if a.TipoDeDocumento.Set {
		if v, ok := a.TipoDeDocumento.Definido(); ok {
			tipo = &v
		} else {
			tipo = nil
		}
	}
	if a.Documento.Set {
		if v, ok := a.Documento.Definido(); ok {
			numero = &v
		} else {
			numero = nil
		}
	}
	// Nada mudou no eixo do documento: o valor já é dele, e conferir de novo só
	// gastaria uma trava.
	if igualPonteiro(tipo, atual.TipoDeDocumento) && igualPonteiro(numero, atual.Documento) {
		return nil
	}
	return s.conferirDocumento(ctx, propriedade, tipo, numero, id)
}

// completarDuplicidade transforma a violação do índice de telefone no `409` do
// contrato, COM o id do contato que já existe.
//
// A consulta acontece aqui, e não no repositório, porque o `23505` abortou a
// transação: dentro dela qualquer comando novo responderia `25P02`. Depois do
// rollback, o pool está limpo e a leitura funciona.
func (s *Servico) completarDuplicidade(ctx context.Context, err error, telefone *string) error {
	var dup *ErroDeDuplicidade
	if !errors.As(err, &dup) {
		return err
	}

	detalhes := map[string]any{"field": dup.Campo}
	if dup.Campo == "phone_e164" && telefone != nil && *telefone != "" {
		if id, achou, errBusca := s.repo.BuscarIDPorTelefone(ctx, *telefone); errBusca == nil && achou {
			detalhes["contact_id"] = id
		}
		detalhes["phone_e164"] = *telefone
	}
	return ContatoDuplicado.WithCause(err).WithDetails(detalhes)
}

// propriedade resolve onde o contato nasce: a do usuário logado; sem usuário
// (seed, script), a única propriedade ativa.
func (s *Servico) propriedade(ctx context.Context) (uuid.UUID, error) {
	if u, ok := auth.UserFrom(ctx); ok && u.PropertyID != uuid.Nil {
		return u.PropertyID, nil
	}
	return s.repo.PropriedadePadrao(ctx)
}

// normalizarBuscaDeDocumento aceita o documento como o balcão digita.
//
// Se o texto tem qualquer letra, é passaporte (que a coluna guarda em caixa
// alta); senão é CPF/CNPJ, e a coluna guarda só dígitos.
func normalizarBuscaDeDocumento(bruto string) string {
	if temLetra(bruto) {
		return normalizarPassaporteDeBusca(bruto)
	}
	return somenteDigitos(bruto)
}

func temLetra(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func igualPonteiro(a, b *string) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

// normalizarPassaporteDeBusca deixa o passaporte como a coluna guarda: caixa
// alta, sem espaços nem pontuação.
func normalizarPassaporteDeBusca(bruto string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(bruto) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
