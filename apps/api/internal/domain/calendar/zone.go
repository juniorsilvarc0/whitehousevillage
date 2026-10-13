package calendar

import (
	"fmt"
	"time"

	// O banco de fusos embutido no binário. A imagem de produção pode subir
	// sem /usr/share/zoneinfo (é a ressalva de `inventario/dto.go`, que por
	// isso valida `properties.timezone` por padrão de texto), e sem ele
	// `time.LoadLocation("America/Fortaleza")` falharia — a agenda inteira
	// ficaria sem relógio por um arquivo do sistema operacional. Com o import,
	// o Go cai no banco embutido quando o do sistema não existe.
	_ "time/tzdata"
)

// O relógio de parede da propriedade, e a ÚNICA conversão entre ele e um
// instante em todo o código Go.
//
// Duas espécies de tempo convivem no sistema e não se misturam:
//
//   - a DATA CIVIL (`Date`): "a estadia é de 20 a 23". Não tem fuso — é o
//     mesmo dia para o hóspede em qualquer lugar (regra 5 do CLAUDE.md);
//   - o INSTANTE (`time.Time`, `timestamptz` no banco): "o check-in é às 14h
//     do dia 20 em Fortaleza", que é um ponto único na linha do tempo.
//
// Passar de uma para a outra exige o fuso da PROPRIEDADE
// (`properties.timezone`) — nunca o do servidor, nunca o do navegador, nunca
// uma constante. Esta é a única porta, no Go, por onde essa passagem
// acontece: `Zone.At` (data + hora de parede → instante), `Zone.DateOf` e
// `Zone.ClockOf` (instante → data e hora de parede). Quem precisar de "hoje"
// pede `Zone.DateOf(agora)`, com o `agora` lido do relógio do BANCO na mesma
// transação da escrita (`now()`), e não de `time.Now()`.
//
// Até 13/10/2026 o código Go não tinha helper nenhum: cada módulo escrevia o
// seu `(now() AT TIME ZONE p.timezone)::date` em SQL (reservas, disponibilidade,
// tarifário, manutenção, bens, inventário) e a vitrine tem um `fusoDaCasa`
// fixo em `America/Fortaleza`. A agenda é a primeira a precisar da conversão
// nos dois sentidos — e com hora, não só com data —, e nasce usando esta.
// Convergir os módulos antigos para cá é dívida registrada no relatório da
// fatia, não trabalho desta.
//
// Horário de verão: o Brasil não tem desde 2019, e `America/Fortaleza` não tem
// desde 2000. Numa hora que não existe no fuso (o salto da primavera), `At`
// segue a normalização de `time.Date`, que o Go documenta como não garantida;
// os testes de propriedade cobrem um fuso SEM salto (Fortaleza) e um COM
// (Nova York), e no segundo só exigem a volta fora da hora pulada.

// Clock é a hora de parede, em minutos desde a meia-noite: 0 é 00:00 e 1440 é
// 24:00. As 24:00 existem para FECHAR um intervalo ("aberto das 18:00 às
// 24:00") — como começo de alguma coisa não fazem sentido, e quem as recebe
// como começo recusa.
//
// É minuto, e não `time.Duration`, porque é o que a operação configura e o que
// o contrato serializa ("14:00"). Segundo não existe na agenda.
type Clock int

const (
	// Midnight é 00:00.
	Midnight Clock = 0
	// EndOfDay é 24:00 — a meia-noite que FECHA o dia.
	EndOfDay Clock = 24 * 60
)

// ParseClock lê "HH:MM", de "00:00" a "24:00". Qualquer outra forma — "9:00",
// "14h", "14:00:00", "24:30" — é erro: o contrato tem um formato só, e aceitar
// variações é como dois formulários passam a gravar a mesma hora de jeitos
// diferentes.
func ParseClock(s string) (Clock, error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("hora inválida %q: use HH:MM", s)
	}
	digito := func(b byte) (int, bool) { return int(b - '0'), b >= '0' && b <= '9' }
	h1, ok1 := digito(s[0])
	h2, ok2 := digito(s[1])
	m1, ok3 := digito(s[3])
	m2, ok4 := digito(s[4])
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return 0, fmt.Errorf("hora inválida %q: use HH:MM", s)
	}
	h, m := h1*10+h2, m1*10+m2
	if m > 59 || h > 24 || (h == 24 && m != 0) {
		return 0, fmt.Errorf("hora inválida %q: de 00:00 a 24:00", s)
	}
	return Clock(h*60 + m), nil
}

// MustParseClock é para teste e constante de seed — entra em pânico com hora
// inválida.
func MustParseClock(s string) Clock {
	c, err := ParseClock(s)
	if err != nil {
		panic(err)
	}
	return c
}

// Valid diz se a hora está entre 00:00 e 24:00.
func (c Clock) Valid() bool { return c >= Midnight && c <= EndOfDay }

// Minutes devolve os minutos desde a meia-noite.
func (c Clock) Minutes() int { return int(c) }

func (c Clock) String() string { return fmt.Sprintf("%02d:%02d", int(c)/60, int(c)%60) }

// Zone é o fuso de UMA propriedade. Construa com LoadZone; o valor zero não é
// fuso nenhum, e usá-lo é defeito de programação (pânico), não UTC por
// omissão — UTC calado é exatamente o check-in que vira 11h em vez de 14h.
type Zone struct {
	loc *time.Location
}

// LoadZone carrega o fuso pelo nome IANA gravado em `properties.timezone`.
//
// Não é I/O de negócio: o nome resolve contra o banco de fusos do sistema ou,
// na falta dele, contra o embutido no binário (`time/tzdata`, acima). Nome
// desconhecido é erro — e quem chama o trata como erro interno, porque o
// cadastro da propriedade só aceita fuso válido.
func LoadZone(name string) (Zone, error) {
	if name == "" {
		return Zone{}, fmt.Errorf("calendar: fuso vazio")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return Zone{}, fmt.Errorf("calendar: fuso %q: %w", name, err)
	}
	return Zone{loc: loc}, nil
}

// MustLoadZone é para teste — entra em pânico com fuso desconhecido.
func MustLoadZone(name string) Zone {
	z, err := LoadZone(name)
	if err != nil {
		panic(err)
	}
	return z
}

// IsZero diz se o fuso não foi carregado.
func (z Zone) IsZero() bool { return z.loc == nil }

func (z Zone) location() *time.Location {
	if z.loc == nil {
		panic("calendar: Zone sem fuso — construa com LoadZone(properties.timezone)")
	}
	return z.loc
}

// Name devolve o nome IANA do fuso.
func (z Zone) Name() string { return z.location().String() }

// At é o instante em que o relógio de parede da propriedade marca `c` no dia
// `d`. `At(d, EndOfDay)` é a meia-noite que começa o dia seguinte — o fim
// exclusivo do dia `d`.
func (z Zone) At(d Date, c Clock) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, int(c), 0, 0, z.location())
}

// StartOfDay é `At(d, Midnight)`: o começo do dia `d` na propriedade.
func (z Zone) StartOfDay(d Date) time.Time { return z.At(d, Midnight) }

// DateOf é a data civil, na propriedade, do instante `t`. "Hoje" é
// `DateOf(agora)`.
func (z Zone) DateOf(t time.Time) Date {
	l := t.In(z.location())
	return Date{l.Year(), l.Month(), l.Day()}
}

// ClockOf é a hora de parede, na propriedade, do instante `t`, truncada no
// minuto. Nunca devolve EndOfDay: a meia-noite é 00:00 do dia seguinte.
func (z Zone) ClockOf(t time.Time) Clock {
	l := t.In(z.location())
	return Clock(l.Hour()*60 + l.Minute())
}

// In devolve `t` expresso no fuso da propriedade — é o que a API serializa, de
// modo que `"2026-11-20T14:00:00-03:00"` traga na própria string a data e a
// hora de parede da casa.
func (z Zone) In(t time.Time) time.Time { return t.In(z.location()) }
