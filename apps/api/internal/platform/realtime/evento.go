// Package realtime é o barramento de tempo real do processo: um `LISTEN` por
// API, fan-out em memória para as conexões SSE abertas.
//
// # Por que um hub, e não um LISTEN por conexão HTTP
//
// `LISTEN` é estado de SESSÃO do Postgres: quem escuta precisa de uma conexão
// dedicada, que não pode voltar para o pool (a próxima requisição a pegaria com
// os canais ainda escutados e receberia notificação que não pediu). Com um
// `LISTEN` por aba aberta, dez pessoas com o mapa de ocupação na tela seriam dez
// conexões de banco paradas — o pool desta API tem `MaxConns = 20`, então a
// décima primeira aba derrubaria a API inteira, e não só o tempo real.
//
// O hub inverte isso: UMA conexão dedicada, aberta fora do pool, e a
// multiplicação acontece em memória, onde custa um canal Go por assinante.
//
// # O que este pacote NÃO faz
//
// Não decide permissão. O evento que sai daqui é o mesmo para todo mundo; quem
// filtra por tópico, por escopo `own` e por dono é o módulo `stream`, que tem a
// identidade da requisição. Um barramento que soubesse de RBAC seria um segundo
// caminho de autorização, e o segundo caminho é sempre o que ninguém testa.
package realtime

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Tópicos do contrato (`GET /stream?topics=…`).
const (
	TopicoCalendario = "calendar"
	TopicoCRM        = "crm"
)

// Canais do Postgres criados pela migration 20260827120000. Um canal por
// assunto: a aba que só olha o kanban não precisa acordar a cada check-out.
const (
	CanalCalendario = "whv_calendar"
	CanalCRM        = "whv_crm"
)

// topicoDoCanal é a fonte da verdade do roteamento.
//
// O payload TAMBÉM traz `topic`, mas o canal é que manda: o canal é aquilo em
// que este processo deu `LISTEN`, então não há como um erro de digitação numa
// migration futura fazer um evento de CRM ser entregue a quem só pode ver o
// calendário. Divergência entre os dois vira registro no log, nunca entrega.
var topicoDoCanal = map[string]string{
	CanalCalendario: TopicoCalendario,
	CanalCRM:        TopicoCRM,
}

// Canais é o que o hub escuta. Ordem estável para o teste comparar.
var Canais = []string{CanalCalendario, CanalCRM}

// entidadesDoTopico fecha o vocabulário do envelope. Entidade desconhecida é
// descartada em vez de repassada: o cliente reagiria a um nome que não sabe
// buscar, e um evento que não se sabe buscar é ruído que só faz a tela piscar.
var entidadesDoTopico = map[string]map[string]bool{
	TopicoCalendario: {"stay_block": true, "reservation": true},
	TopicoCRM:        {"opportunity": true, "lead": true, "activity": true},
}

// TopicoConhecido responde se o nome é um tópico do contrato.
func TopicoConhecido(nome string) bool {
	_, ok := entidadesDoTopico[nome]
	return ok
}

// Evento é o que o barramento entrega — o mesmo conteúdo do `pg_notify`, mais o
// cursor que o hub atribui.
//
// Deliberadamente magro: identificadores e versão, nada de nome de hóspede, de
// valor ou de telefone. Quem quiser o dado refaz o fetch pela API, com o token
// dele e o RBAC aplicado.
type Evento struct {
	Topico   string
	Entidade string
	ID       uuid.UUID

	// UnitID vem só em `stay_block`; PipelineID, só em `opportunity`. São as
	// chaves de escopo que permitem ao cliente refazer o fetch de UMA coluna do
	// mapa em vez da matriz inteira.
	UnitID     uuid.UUID
	PipelineID uuid.UUID
	PropertyID uuid.UUID

	// V é a versão da ENTIDADE (`pg_current_xact_id()` na origem): todos os
	// eventos de uma mesma mudança atômica compartilham o mesmo V, e o cliente
	// descarta o que vier com V menor ou igual ao que já desenhou.
	V int64

	// Cursor é o número do envelope SSE (`id:`), atribuído pelo HUB e não pelo
	// banco. Não confundir com V: um é a posição no barramento, o outro é a
	// idade do dado. Ver hub.go para a razão de o cursor nascer do relógio.
	Cursor int64
}

// payloadDoNotify é a forma literal do JSON montado pelos gatilhos da migration
// 20260827120000. Mudar um nome de campo aqui exige mudar as três funções de
// gatilho — está escrito no cabeçalho daquela migration também.
type payloadDoNotify struct {
	Topic      string  `json:"topic"`
	Entity     string  `json:"entity"`
	ID         string  `json:"id"`
	UnitID     *string `json:"unit_id"`
	PipelineID *string `json:"pipeline_id"`
	PropertyID *string `json:"property_id"`
	V          int64   `json:"v"`
}

// Decodificar transforma uma notificação crua em Evento.
//
// Erro aqui NUNCA derruba o hub: payload ilegível é registrado e descartado, e o
// `LISTEN` continua. Uma migration futura que acrescente um campo não pode
// desligar o tempo real de quem está com a tela aberta.
func Decodificar(canal, bruto string) (Evento, error) {
	topico, ok := topicoDoCanal[canal]
	if !ok {
		return Evento{}, fmt.Errorf("canal desconhecido: %q", canal)
	}

	var p payloadDoNotify
	if err := json.Unmarshal([]byte(bruto), &p); err != nil {
		return Evento{}, fmt.Errorf("payload ilegível no canal %s: %w", canal, err)
	}
	if p.Topic != "" && p.Topic != topico {
		return Evento{}, fmt.Errorf("canal %s trouxe topic=%q; o canal manda", canal, p.Topic)
	}
	if !entidadesDoTopico[topico][p.Entity] {
		return Evento{}, fmt.Errorf("entidade %q não pertence ao tópico %s", p.Entity, topico)
	}

	id, err := uuid.Parse(p.ID)
	if err != nil {
		return Evento{}, fmt.Errorf("id inválido em %s/%s: %w", topico, p.Entity, err)
	}

	ev := Evento{Topico: topico, Entidade: p.Entity, ID: id, V: p.V}
	if ev.UnitID, err = uuidOpcional(p.UnitID); err != nil {
		return Evento{}, fmt.Errorf("unit_id inválido: %w", err)
	}
	if ev.PipelineID, err = uuidOpcional(p.PipelineID); err != nil {
		return Evento{}, fmt.Errorf("pipeline_id inválido: %w", err)
	}
	if ev.PropertyID, err = uuidOpcional(p.PropertyID); err != nil {
		return Evento{}, fmt.Errorf("property_id inválido: %w", err)
	}
	return ev, nil
}

func uuidOpcional(v *string) (uuid.UUID, error) {
	if v == nil || *v == "" {
		return uuid.Nil, nil
	}
	return uuid.Parse(*v)
}
