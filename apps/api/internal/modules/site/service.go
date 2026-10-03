package site

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Entidades da trilha de auditoria.
const (
	EntidadeConteudo = "site_content"
	EntidadeMidia    = "site_media"
)

// Transacionador é o que o service precisa do db.TxManager.
type Transacionador interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Servico orquestra: valida contra o catálogo, confere mídia, grava e audita
// na mesma transação.
type Servico struct {
	repo *Repository
	tx   Transacionador
	dir  string
}

// NovoServico monta o service. dir é o volume de mídia (MEDIA_DIR).
func NovoServico(repo *Repository, tx Transacionador, dir string) *Servico {
	return &Servico{repo: repo, tx: tx, dir: dir}
}

func campoOu404(chave string) (Campo, error) {
	c, ok := CampoDoCatalogo(chave)
	if !ok {
		return Campo{}, apperr.NotFound("Campo do site")
	}
	return c, nil
}

// ─────────────────────────── Painel ─────────────────────────────────────────

// Conteudo devolve o catálogo inteiro com o valor atual de cada campo.
func (s *Servico) Conteudo(ctx context.Context) (ConteudoResposta, error) {
	linhas, err := s.repo.ListarConteudo(ctx)
	if err != nil {
		return ConteudoResposta{}, err
	}
	editadas := make(map[string]Linha, len(linhas))
	for _, l := range linhas {
		editadas[l.Chave] = l
	}

	out := ConteudoResposta{Sections: make([]SecaoResposta, 0, len(catalogo))}
	for _, sec := range catalogo {
		sr := SecaoResposta{Key: sec.Chave, Label: sec.Rotulo, Fields: make([]CampoResposta, 0, len(sec.Campos))}
		for _, c := range sec.Campos {
			var linha *Linha
			if l, ok := editadas[c.Chave]; ok {
				linha = &l
			}
			cr, err := c.resposta(linha)
			if err != nil {
				return ConteudoResposta{}, err
			}
			sr.Fields = append(sr.Fields, cr)
		}
		out.Sections = append(out.Sections, sr)
	}
	return out, nil
}

// resposta monta o campo para o painel; linha nula = texto original.
func (c Campo) resposta(linha *Linha) (CampoResposta, error) {
	original, err := c.resolverOriginal()
	if err != nil {
		return CampoResposta{}, apperr.Internal.WithCause(err)
	}
	cr := CampoResposta{
		Key: c.Chave, Label: c.Rotulo, Kind: c.Tipo, Help: c.Ajuda, Max: c.Max,
		Value: original, DefaultValue: original, IsDefault: true,
	}
	for _, sc := range c.Itens {
		cr.ItemFields = append(cr.ItemFields, SubcampoResposta{Key: sc.Chave, Label: sc.Rotulo, Kind: sc.Tipo, Max: sc.Max})
	}
	if linha != nil {
		v, err := c.resolverGravado(linha.Valor, false)
		if err != nil {
			return CampoResposta{}, apperr.Internal.WithCause(err)
		}
		em := linha.AtualizadoEm
		cr.Value, cr.IsDefault, cr.UpdatedAt = v, false, &em
	}
	return cr, nil
}

// registroDeConteudo é o documento da trilha: a chave vai nos dois lados para
// a linha de auditoria dizer QUAL campo mudou (site_content não tem uuid).
type registroDeConteudo struct {
	Chave string          `json:"key"`
	Valor json.RawMessage `json:"value"`
}

// Gravar valida o valor contra o catálogo e publica (salvar = publicar).
func (s *Servico) Gravar(ctx context.Context, chave string, bruto json.RawMessage) (CampoResposta, error) {
	c, err := campoOu404(chave)
	if err != nil {
		return CampoResposta{}, err
	}
	v, det := c.validar(bruto)
	if len(det) > 0 {
		return CampoResposta{}, apperr.Validation(det)
	}

	var por *uuid.UUID
	if uid, ok := auth.UsuarioID(ctx); ok {
		por = &uid
	}

	var gravada Linha
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.conferirMidias(ctx, v.Midias); err != nil {
			return err
		}
		antes, existia, err := s.repo.TravarConteudo(ctx, chave)
		if err != nil {
			return err
		}
		if gravada, err = s.repo.GravarConteudo(ctx, chave, v.JSON, por); err != nil {
			return err
		}
		ev := audit.Evento{
			Acao:          audit.Acao(EntidadeConteudo, audit.VerboAlterado),
			Entidade:      EntidadeConteudo,
			AtorID:        por,
			Depois:        audit.Snapshot(registroDeConteudo{Chave: chave, Valor: gravada.Valor}),
			PropriedadeID: propriedadeDoAtor(ctx),
		}
		if existia {
			ev.Antes = audit.Snapshot(registroDeConteudo{Chave: chave, Valor: antes.Valor})
		}
		return audit.Registrar(ctx, s.repo.pool, ev)
	})
	if err != nil {
		return CampoResposta{}, err
	}
	return c.resposta(&gravada)
}

// Restaurar volta o campo ao texto original (apaga a linha). Restaurar o que
// já está original não é erro — e não gera trilha, porque nada mudou.
func (s *Servico) Restaurar(ctx context.Context, chave string) error {
	if _, err := campoOu404(chave); err != nil {
		return err
	}
	return s.tx.Do(ctx, func(ctx context.Context) error {
		antes, existia, err := s.repo.TravarConteudo(ctx, chave)
		if err != nil || !existia {
			return err
		}
		if err := s.repo.ApagarConteudo(ctx, chave); err != nil {
			return err
		}
		ev := audit.Evento{
			Acao:          audit.Acao(EntidadeConteudo, audit.VerboExcluido),
			Entidade:      EntidadeConteudo,
			Antes:         audit.Snapshot(registroDeConteudo{Chave: chave, Valor: antes.Valor}),
			Depois:        audit.Campos{"key": chave, "restaurado": true},
			PropriedadeID: propriedadeDoAtor(ctx),
		}
		if uid, ok := auth.UsuarioID(ctx); ok {
			ev.AtorID = &uid
		}
		return audit.Registrar(ctx, s.repo.pool, ev)
	})
}

// conferirMidias exige que cada mídia citada exista e seja do tipo do campo.
func (s *Servico) conferirMidias(ctx context.Context, refs []refMidia) error {
	if len(refs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	tipos, err := s.repo.TiposDeMidia(ctx, ids)
	if err != nil {
		return err
	}
	det := map[string]string{}
	for _, r := range refs {
		tipo, ok := tipos[r.ID]
		switch {
		case !ok && r.Tipo == TipoVideo:
			det[r.Caminho] = "Este vídeo não foi encontrado. Envie o arquivo de novo."
		case !ok:
			det[r.Caminho] = "Esta foto não foi encontrada. Envie o arquivo de novo."
		case Tipo(tipo) != r.Tipo && r.Tipo == TipoVideo:
			det[r.Caminho] = "Este campo pede um vídeo, e o arquivo escolhido é uma foto."
		case Tipo(tipo) != r.Tipo:
			det[r.Caminho] = "Este campo pede uma foto, e o arquivo escolhido é um vídeo."
		}
	}
	if len(det) > 0 {
		return apperr.Validation(det)
	}
	return nil
}

func propriedadeDoAtor(ctx context.Context) uuid.UUID {
	if u, ok := auth.UserFrom(ctx); ok {
		return u.PropertyID
	}
	return uuid.Nil
}

// ─────────────────────────── Site ───────────────────────────────────────────

// Publico devolve só os campos editados, com mídia como {url, alt}. Linha de
// chave que saiu do catálogo é ignorada (o site não saberia onde pô-la).
func (s *Servico) Publico(ctx context.Context) (PublicoResposta, error) {
	linhas, err := s.repo.ListarConteudo(ctx)
	if err != nil {
		return PublicoResposta{}, err
	}
	out := PublicoResposta{Values: make(map[string]json.RawMessage, len(linhas))}
	for _, l := range linhas {
		c, ok := CampoDoCatalogo(l.Chave)
		if !ok {
			continue
		}
		v, err := c.resolverGravado(l.Valor, true)
		if err != nil {
			return PublicoResposta{}, apperr.Internal.WithCause(fmt.Errorf("site: %s: %w", l.Chave, err))
		}
		out.Values[l.Chave] = v
	}
	return out, nil
}

// garante em tempo de compilação que o TxManager serve.
var _ Transacionador = (*db.TxManager)(nil)

// Recurso é o código RBAC do módulo (ações ver e editar, sem escopo own).
const Recurso = "site"
