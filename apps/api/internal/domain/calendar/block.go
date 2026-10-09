package calendar

// Tetos do bloqueio operacional de calendário — manutenção e uso do
// proprietário, a ocupação sem reserva.
//
// Moram aqui, e não no módulo que os aplica, porque há DOIS caminhos que criam
// essa ocupação: `POST /blocks` e o bloqueio de uma ordem de manutenção
// (`internal/domain/maintenance`). Dois tetos para a mesma linha de
// `stay_blocks` seriam uma porta dos fundos: a ordem bloquearia a década que o
// `/blocks` recusa. Até 09/10/2026 o teto vivia só em
// `internal/modules/reservas/dto.go` — e `docs/agents.md` §4.1 registra que ele
// já tinha sido mandado para cá uma vez e ficado sem dono.
//
// O motivo de cada número está no contrato (`POST /blocks`, "O bloqueio tem
// janela máxima"): medido em 26/08/2026, uma tecla errada tirava 29.224
// noites-unidade do mercado a catorze anos de distância.
const (
	// MaxBlockNights — duração máxima de UM bloqueio, em noites. Manutenção e
	// uso do proprietário se medem em dias ou semanas; acima de um ano o que se
	// está fazendo é retirar a unidade do catálogo (`units.active = false`).
	MaxBlockNights = 365

	// BlockHorizonDays — até onde, a partir de HOJE no fuso da propriedade, um
	// bloqueio pode começar. Cobre com folga o horizonte de venda real; além
	// dele nem o tarifário nem a política comercial alcançam.
	BlockHorizonDays = 3 * 365
)
