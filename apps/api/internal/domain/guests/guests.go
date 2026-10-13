// Package guests guarda as regras puras da rooming list (F2-22): quem entra na
// lista de hóspedes de uma reserva, quando ela se edita, quantos nomes cabem e
// o que acontece com ela quando o titular muda ou a reserva é remarcada.
//
// É código PURO: sem SQL, sem HTTP, sem relógio. O identificador é genérico
// (`ID comparable`) para o módulo passar `uuid.UUID` sem o domínio importar
// biblioteca de identificador — a mesma escolha de `commission`.
//
// # Uma fonte para o titular
//
// O titular É `reservations.contact_id`. A lista (`reservation_guests`) guarda
// uma linha para ele com `is_lead_guest = true`, e essa linha é consequência,
// nunca causa: troca-se o titular editando a reserva (`PATCH
// /reservations/{id}`), e a lista acompanha na mesma transação, pelo plano que
// [PlanEdit] devolve. Até o F2-22 a troca mexia só na reserva, e a linha de
// titular ficava apontando para a pessoa de antes — o cartão "Cliente" do
// painel lê o titular da lista e mostrava o nome, o telefone e o e-mail de quem
// já não era o cliente.
//
// # O que NÃO mora aqui, e por quê
//
// Três garantias são do banco (docs/db.md §5), não deste pacote:
//   - no máximo um titular por reserva — o índice único parcial
//     `reservation_guests_titular_idx`;
//   - a linha de titular é a do `contact_id` da reserva — a FK adiada
//     `reservation_guests_titular_fkey`, sobre a coluna gerada
//     `lead_contact_id`; trocar `reservations.contact_id` sem mexer na lista
//     falha no COMMIT;
//   - a mesma pessoa não aparece duas vezes na mesma lista — a chave primária.
//
// O domínio decide se a escrita é ACEITÁVEL (o estado da reserva, o teto, quem
// pode entrar) e diz o que gravar; o banco garante que, com ou sem o domínio,
// a lista nunca fica com dois titulares nem com um titular que não é o da
// reserva. "Ao menos um titular" é da aplicação: a reserva nasce com a linha
// dele, a troca a substitui e [CheckRemove] recusa tirá-la.
//
// Os erros carregam o `code` do contrato em texto, como `booking.RuleError`: o
// domínio não importa `apperr`, e quem traduz é `apperr.PorCodigo`.
package guests

import (
	"errors"
	"fmt"
)

// Códigos do contrato que este pacote carimba. Os literais são permitidos em
// `internal/domain/` e `apperr/catalogo_test.go` exige que cada um exista no
// catálogo — senão a tradução cairia em 500.
const (
	codeValidation        = "VALIDATION_ERROR"
	codeNotFound          = "NOT_FOUND"
	codeInvalidTransition = "INVALID_STATE_TRANSITION"
	codeCodeInUse         = "CODE_IN_USE"
	codeResourceInUse     = "RESOURCE_IN_USE"
	codeContactAnonymized = "CONTACT_ANONYMIZED"
	codeListFull          = "GUEST_LIST_FULL"
)

// RuleError é a violação de uma regra da lista. `Code` é estável e vira o
// `error.code` da API; `Details` vai para `error.details`. Em
// `VALIDATION_ERROR`, `Details` é o mapa campo → mensagem.
//
// Nenhuma mensagem carrega nome, documento ou telefone: dado pessoal não entra
// em mensagem de erro (o erro vai para log; a pessoa, não).
type RuleError struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *RuleError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// ErrInvalidInput é entrada que nenhum chamador correto produz — estado fora do
// vocabulário, número de hóspedes menor que 1. Não é recusa de negócio: é
// defeito de quem chamou, e o service a trata como erro interno (500).
var ErrInvalidInput = errors.New("guests: entrada inválida")

// ─────────────────────────── Estado da reserva ──────────────────────

// ReservationStatus é o estado comercial da reserva. Os nove valores são,
// palavra por palavra, o `CHECK` de `reservations.status`.
type ReservationStatus string

const (
	Quote      ReservationStatus = "quote"
	Hold       ReservationStatus = "hold"
	Confirmed  ReservationStatus = "confirmed"
	CheckedIn  ReservationStatus = "checked_in"
	CheckedOut ReservationStatus = "checked_out"
	Closed     ReservationStatus = "closed"
	Cancelled  ReservationStatus = "cancelled"
	Expired    ReservationStatus = "expired"
	NoShow     ReservationStatus = "no_show"
)

// Valid diz se o valor é um dos nove estados.
func (s ReservationStatus) Valid() bool {
	switch s {
	case Quote, Hold, Confirmed, CheckedIn, CheckedOut, Closed, Cancelled, Expired, NoShow:
		return true
	}
	return false
}

// editable são os estados em que a lista aceita escrita, na ordem do ciclo.
//
// São os estados em que a reserva está DE PÉ — segura a data (`hold`,
// `confirmed`) ou tem gente dentro (`checked_in`) — e são exatamente os que
// impedem anonimizar quem está na lista (`contatos.statusDeReservaViva`). As
// duas regras se cobrem: ninguém anonimizado entra numa lista viva, e ninguém
// de uma lista viva é anonimizado.
//
// Fora deles a lista não muda:
//   - `checked_out` e `closed`: a lista é o registro de quem dormiu na casa, e
//     reescrevê-la depois da saída é reescrever o fato — inclusive para tirar
//     alguém depois de um incidente. Corrigir depois da saída é decisão do
//     dono (pendente), e se vier é uma linha aqui;
//   - `cancelled`, `expired`, `no_show`: a estadia não aconteceu, e a lista
//     fica como estava, para a história da venda;
//   - `quote`: a reserva rascunho não segura data, e nenhuma rota a cria hoje.
var editable = []ReservationStatus{Hold, Confirmed, CheckedIn}

// Editable diz se a lista da reserva aceita acrescentar, retirar e trocar o
// titular no estado em que ela está. É o `guests_editable` do `/full` e o
// `meta.editable` de `GET /reservations/{id}/guests`: o painel desenha os
// botões a partir daqui, sem uma segunda cópia da regra.
func Editable(s ReservationStatus) bool {
	for _, e := range editable {
		if s == e {
			return true
		}
	}
	return false
}

// EditableStatuses devolve os estados em que a lista se edita — é o
// `details.allowed` da recusa por estado. Cópia: quem a recebe pode mexer.
func EditableStatuses() []ReservationStatus {
	return append([]ReservationStatus(nil), editable...)
}

// ─────────────────────────── A reserva e a pessoa ───────────────────

// Reservation é a reserva como as regras da lista precisam dela. O service lê
// com a linha da reserva TRAVADA (`FOR UPDATE`): toda escrita na lista trava a
// mesma linha, e é isso que torna a contagem e a decisão imunes a uma escrita
// concorrente na mesma lista.
type Reservation[ID comparable] struct {
	Status ReservationStatus
	// Lead é `reservations.contact_id` — o titular.
	Lead ID
	// GuestsCount é `reservations.guests_count`: quantas pessoas foram vendidas.
	// É o teto da lista.
	GuestsCount int
	// Capacity é a capacidade declarada do produto. Não limita a lista — quem
	// limita é GuestsCount, que já é validado contra ela —; vai em `details`
	// para a tela saber se dá para aumentar o número de hóspedes.
	Capacity int
	// Listed é o `contact_id` de cada linha de `reservation_guests` da reserva,
	// o titular incluso.
	Listed []ID
}

func (r Reservation[ID]) has(id ID) bool {
	for _, x := range r.Listed {
		if x == id {
			return true
		}
	}
	return false
}

func (r Reservation[ID]) valid() error {
	if !r.Status.Valid() {
		return fmt.Errorf("%w: estado %q", ErrInvalidInput, r.Status)
	}
	if r.GuestsCount < 1 {
		return fmt.Errorf("%w: guests_count %d", ErrInvalidInput, r.GuestsCount)
	}
	return nil
}

// Contact é a pessoa que se quer pôr na lista, como o service a encontrou.
//
// Found e Anonymized saem de UMA leitura com a ficha travada em `FOR KEY SHARE`
// — a anonimização trava a ficha em `FOR UPDATE` antes de esvaziá-la, então a
// leitura enxerga o resultado dela ou a faz esperar a escrita na lista
// terminar. Sem a trava, a pessoa podia ser anonimizada entre a leitura e o
// INSERT e entrar na lista assim mesmo.
type Contact[ID comparable] struct {
	ID ID
	// Found: a ficha existe e é desta propriedade. Ficha de outra casa e ficha
	// inexistente são a MESMA resposta — dizer qual das duas seria ensinar,
	// por tentativa, que ids existem nas outras propriedades.
	Found bool
	// Anonymized: o titular dos dados pediu a eliminação (`anonymized_at`).
	Anonymized bool
}

// Admit diz se a pessoa pode ENTRAR numa lista de hóspedes — como
// acompanhante, como titular novo ou como titular da reserva que nasce. É a
// regra única de entrada; as três portas a chamam.
//
//   - ficha inexistente ou de outra propriedade → `VALIDATION_ERROR` em
//     `details.contact_id`;
//   - ficha anonimizada → `CONTACT_ANONYMIZED`: quem pediu a eliminação não
//     volta a ser associado a uma estadia pela casa. Se a pessoa voltar a se
//     hospedar, ela se recadastra (o documento e o telefone dela foram
//     liberados justamente para isso).
func Admit[ID comparable](c Contact[ID]) error {
	if !c.Found {
		return &RuleError{
			Code:    codeValidation,
			Message: "Dados inválidos.",
			Details: map[string]any{"contact_id": "contato não encontrado nesta propriedade."},
		}
	}
	if c.Anonymized {
		return &RuleError{
			Code:    codeContactAnonymized,
			Message: "Este contato foi anonimizado a pedido do titular dos dados e não entra em lista de hóspedes.",
			Details: map[string]any{"contact_id": c.ID},
		}
	}
	return nil
}

// ─────────────────────────── Acrescentar e retirar ──────────────────

// CheckAdd decide `POST /reservations/{id}/guests`: a pessoa entra como
// acompanhante. As recusas, na ordem em que são conferidas:
//
//  1. reserva fora de [Editable] → `INVALID_STATE_TRANSITION`, com
//     `details.status` e `details.allowed`;
//  2. [Admit] → `VALIDATION_ERROR` ou `CONTACT_ANONYMIZED`;
//  3. a pessoa já está na lista (inclusive como titular) → `CODE_IN_USE`. A
//     chave primária `(reservation_id, contact_id)` garante o mesmo sob
//     qualquer escrita; aqui é a frase certa antes do `23505`;
//  4. a lista já tem `guests_count` nomes → `GUEST_LIST_FULL`, com
//     `details.guests_count`, `details.listed` e `details.capacity`.
//
// O teto é o número VENDIDO, e não a capacidade do produto: a lista nomeia as
// pessoas que a reserva acomoda, e não pode nomear mais gente do que ela diz
// acomodar — senão a reserva diz 6 e dormiram 9, e o número de hóspedes de
// todo relatório passa a ser o errado. Chegou mais gente, aumenta-se o número
// de hóspedes da reserva (que é validado contra a capacidade, e auditado) e
// depois se acrescenta o nome.
func CheckAdd[ID comparable](r Reservation[ID], c Contact[ID]) error {
	if err := r.valid(); err != nil {
		return err
	}
	if !Editable(r.Status) {
		return closedError(r.Status, "A lista de hóspedes só muda enquanto a reserva está de pé: pré-reserva, confirmada ou com o hóspede dentro.", "")
	}
	if err := Admit(c); err != nil {
		return err
	}
	if r.has(c.ID) || c.ID == r.Lead {
		return &RuleError{
			Code:    codeCodeInUse,
			Message: "Esta pessoa já está na lista de hóspedes desta reserva.",
			Details: map[string]any{"contact_id": c.ID, "is_lead_guest": c.ID == r.Lead},
		}
	}
	if len(r.Listed)+1 > r.GuestsCount {
		return fullError(r.GuestsCount, len(r.Listed), r.Capacity)
	}
	return nil
}

// CheckRemove decide `DELETE /reservations/{id}/guests/{contactId}`. As
// recusas, na ordem:
//
//  1. reserva fora de [Editable] → `INVALID_STATE_TRANSITION`;
//  2. a pessoa é o titular → `RESOURCE_IN_USE`: a linha dele é a projeção de
//     `reservations.contact_id`, e o titular se troca editando a reserva, que
//     traz a lista junto ([PlanEdit]). Lista sem titular seria uma reserva em
//     nome de ninguém na tela da recepção;
//  3. a pessoa não está na lista → `NOT_FOUND`.
func CheckRemove[ID comparable](r Reservation[ID], id ID) error {
	if err := r.valid(); err != nil {
		return err
	}
	if !Editable(r.Status) {
		return closedError(r.Status, "A lista de hóspedes só muda enquanto a reserva está de pé: pré-reserva, confirmada ou com o hóspede dentro.", "")
	}
	if id == r.Lead {
		return &RuleError{
			Code:    codeResourceInUse,
			Message: "O titular não sai da lista: para trocar o titular, edite a reserva.",
			Details: map[string]any{"contact_id": id, "is_lead_guest": true},
		}
	}
	if !r.has(id) {
		return &RuleError{
			Code:    codeNotFound,
			Message: "Esta pessoa não está na lista de hóspedes desta reserva.",
			Details: map[string]any{"contact_id": id},
		}
	}
	return nil
}

// ─────────────────────────── Editar a reserva ───────────────────────

// Edit é o que um `PUT`/`PATCH /reservations/{id}` muda que encosta na lista.
type Edit[ID comparable] struct {
	// Lead é o `contact_id` pedido, com o que o service achou da ficha; nil
	// quando o corpo não trouxe o campo. Pedir o titular que já é o titular
	// não é troca e passa em qualquer estado — o formulário que reenvia a
	// reserva inteira não pode falhar por carregar o titular de sempre.
	Lead *Contact[ID]
	// GuestsCount é o `guests_count` pedido; nil quando o corpo não o trouxe.
	GuestsCount *int
}

// Plan é o que gravar na lista depois de uma edição aceita. O service aplica
// NESTA ordem, na transação da edição:
//
//  1. apaga a linha de Remove (o titular que sai);
//  2. grava a linha do titular novo — promove a de acompanhante quando
//     Promote, insere quando não;
//  3. atualiza `reservations.contact_id`.
//
// A ordem 1 → 2 é obrigatória: o índice único parcial de titular não é
// adiável, e promover antes de apagar levanta `23505`. A 3 pode vir antes ou
// depois — a FK do titular é adiada e só confere no COMMIT.
type Plan[ID comparable] struct {
	// LeadChanged: o titular muda. Falso, a lista não é tocada.
	LeadChanged bool
	// Remove é o titular que SAI da lista — o anterior, quando a linha dele
	// existe. Ele não vira acompanhante: ver [PlanEdit].
	Remove *ID
	// Promote: o titular novo já estava na lista como acompanhante.
	Promote bool
	// Listed é quantos nomes a lista terá depois da escrita.
	Listed int
}

// PlanEdit decide o que uma edição da reserva faz com a lista.
//
// # Trocar o titular
//
// O titular anterior SAI da lista; não vira acompanhante. A troca de titular
// é, quase sempre, correção — o contato errado escolhido na venda, um
// homônimo, a agência no lugar do hóspede —, e rebaixar a pessoa a
// acompanhante deixaria um estranho associado à estadia, lido com documento e
// telefone por quem abre a reserva, sem que ninguém tenha pedido. Quando o
// titular anterior também vem (a mãe que reservou para a filha), a recepção o
// acrescenta de volta, e isso fica na trilha como o que é: um acréscimo. Pelo
// mesmo motivo a troca nunca aumenta a lista quando a linha do titular
// anterior existe: sai um, entra um (ou o novo já estava lá, e a lista
// diminui).
//
// Recusas da troca:
//   - reserva fora de [Editable] → `INVALID_STATE_TRANSITION` com
//     `details.field = "contact_id"`: trocar o titular de uma estadia que
//     terminou é reescrever quem dormiu na casa;
//   - [Admit] do titular novo → `CONTACT_ANONYMIZED` (a ficha inexistente o
//     service já respondeu antes, com o `404` que a rota sempre deu).
//
// # O número de hóspedes
//
// A lista nunca fica maior que `guests_count` por causa DESTA escrita. A
// escrita que deixa a lista acima do número é recusada quando ela aumentou a
// lista ou diminuiu o número:
//   - reduzir `guests_count` abaixo dos nomes da lista → `VALIDATION_ERROR` em
//     `details.guests_count` (retira-se alguém da lista antes);
//   - a troca aumentar a lista além do número — só acontece se a linha do
//     titular anterior não existia, o que a migration do F2-22 repara →
//     `GUEST_LIST_FULL`.
//
// Lista que JÁ estava acima do número (dado anterior ao F2-22) não trava a
// edição que não a piora: o formulário que só muda a anotação passa.
func PlanEdit[ID comparable](r Reservation[ID], e Edit[ID]) (Plan[ID], error) {
	if err := r.valid(); err != nil {
		return Plan[ID]{}, err
	}
	count := r.GuestsCount
	if e.GuestsCount != nil {
		if *e.GuestsCount < 1 {
			return Plan[ID]{}, fmt.Errorf("%w: guests_count pedido %d", ErrInvalidInput, *e.GuestsCount)
		}
		count = *e.GuestsCount
	}

	p := Plan[ID]{Listed: len(r.Listed)}
	if e.Lead != nil && e.Lead.ID != r.Lead {
		if !Editable(r.Status) {
			return Plan[ID]{}, closedError(r.Status, "O titular só muda enquanto a reserva está de pé: pré-reserva, confirmada ou com o hóspede dentro.", "contact_id")
		}
		if err := Admit(*e.Lead); err != nil {
			return Plan[ID]{}, err
		}
		p.LeadChanged = true
		if r.has(r.Lead) {
			old := r.Lead
			p.Remove = &old
			p.Listed--
		}
		p.Promote = r.has(e.Lead.ID)
		if !p.Promote {
			p.Listed++
		}
	}

	if p.Listed > count {
		shrank := count < r.GuestsCount
		grew := p.Listed > len(r.Listed)
		switch {
		case shrank:
			return Plan[ID]{}, &RuleError{
				Code:    codeValidation,
				Message: "Dados inválidos.",
				Details: map[string]any{"guests_count": fmt.Sprintf(
					"a lista de hóspedes tem %d nomes: retire alguém da lista antes de reduzir para %d.", p.Listed, count)},
			}
		case grew:
			return Plan[ID]{}, fullError(count, len(r.Listed), r.Capacity)
		}
	}
	return p, nil
}

// ─────────────────────────── Remarcar ───────────────────────────────

// Carry decide quem acompanha a reserva remarcada
// (`POST /reservations/{id}/reschedule`): os acompanhantes da original vão
// para a nova, que nasce com o mesmo titular. A original fica com a lista dela,
// congelada — é a história da venda cancelada por remarcação.
//
// `listed` é a lista da original (o titular incluso, que é ignorado aqui —
// a reserva nova já nasce com ele), com o que o service leu de cada ficha.
// `Found` não é lido: quem está na lista existe (a FK garante). Quem foi
// anonimizado não é carregado: entrar na reserva nova é entrar numa lista, e
// [Admit] vale para todas as portas. (Numa reserva viva isso não acontece — a
// anonimização recusa quem está numa lista viva —; a regra está aqui para não
// depender disso.)
//
// A nova reserva nasce com `guestsCount` (o pedido, ou o da original): se o
// titular e os acompanhantes não cabem → `VALIDATION_ERROR` em
// `details.guests_count`, ANTES de qualquer escrita. Não se escolhe quem fica
// de fora: retira-se alguém da lista da original, ou remarca-se com mais
// hóspedes.
func Carry[ID comparable](lead ID, listed []Contact[ID], guestsCount int) ([]ID, error) {
	if guestsCount < 1 {
		return nil, fmt.Errorf("%w: guests_count %d", ErrInvalidInput, guestsCount)
	}
	out := []ID{}
	for _, c := range listed {
		if c.ID == lead || c.Anonymized {
			continue
		}
		out = append(out, c.ID)
	}
	if 1+len(out) > guestsCount {
		return nil, &RuleError{
			Code:    codeValidation,
			Message: "Dados inválidos.",
			Details: map[string]any{"guests_count": fmt.Sprintf(
				"a lista de hóspedes tem %d nomes e a reserva remarcada seria para %d: retire alguém da lista antes de remarcar.", 1+len(out), guestsCount)},
		}
	}
	return out, nil
}

// ─────────────────────────── Recusas ────────────────────────────────

func closedError(s ReservationStatus, msg, field string) *RuleError {
	allowed := make([]string, len(editable))
	for i, e := range editable {
		allowed[i] = string(e)
	}
	d := map[string]any{"status": string(s), "allowed": allowed}
	if field != "" {
		d["field"] = field
	}
	return &RuleError{Code: codeInvalidTransition, Message: msg, Details: d}
}

func fullError(guestsCount, listed, capacity int) *RuleError {
	msg := "A lista já tem o número de hóspedes da reserva: aumente o número de hóspedes antes de acrescentar alguém."
	if guestsCount >= capacity {
		msg = "A lista já tem o número de hóspedes da reserva, que é a capacidade do produto."
	}
	return &RuleError{
		Code:    codeListFull,
		Message: msg,
		Details: map[string]any{"guests_count": guestsCount, "listed": listed, "capacity": capacity},
	}
}
