package realtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// intervaloDaSonda é de quanto em quanto tempo a escuta interrompe a espera para
// perguntar ao Postgres se ele ainda está lá.
//
// Não é enfeite: `LISTEN` é uma conexão que fica MUDA por horas. Quando o cabo
// cai (ou o Postgres reinicia, ou um NAT esquece a tradução), o `recv` do lado
// do cliente não acorda — ele fica esperando bytes que nunca chegarão, e o
// sintoma é o pior possível: a API continua saudável, as conexões SSE continuam
// abertas mandando batimento, e nenhum evento chega nunca mais. O tempo real
// morre em silêncio, e só um F5 revela.
//
// A sonda transforma isso num erro em no máximo 30 s, que é o que dispara a
// reconexão do hub e o resync de quem estava conectado.
const intervaloDaSonda = 30 * time.Second

// AbridorDePool devolve o Abridor que o hub usa em produção.
//
// A conexão é aberta FORA do pool (`pgx.ConnectConfig` sobre a mesma
// configuração), e não emprestada dele. `pgxpool.Acquire` seguraria uma das 20
// conexões da API para sempre — e num pico de reservas a conexão que falta é a
// que fecha uma venda, não a que pinta o calendário.
func AbridorDePool(pool *pgxpool.Pool) Abridor {
	return func(ctx context.Context, canais []string) (Escuta, error) {
		cfg := pool.Config().ConnConfig.Copy()

		// `application_name` é o que faz esta conexão ser reconhecível num
		// `pg_stat_activity` às três da manhã: sem ele, a conexão eternamente
		// ociosa parece exatamente uma transação esquecida.
		if cfg.RuntimeParams == nil {
			cfg.RuntimeParams = map[string]string{}
		}
		cfg.RuntimeParams["application_name"] = "whv-api-listen"

		conn, err := pgx.ConnectConfig(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("abrindo conexão de escuta: %w", err)
		}
		for _, canal := range canais {
			// Sanitize e não interpolação: nome de canal vem de constante hoje,
			// mas o dia em que vier de configuração é o dia em que a
			// interpolação vira injeção.
			if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{canal}.Sanitize()); err != nil {
				_ = conn.Close(context.WithoutCancel(ctx))
				return nil, fmt.Errorf("LISTEN %s: %w", canal, err)
			}
		}
		return &escutaPgx{conn: conn, sonda: intervaloDaSonda}, nil
	}
}

type escutaPgx struct {
	conn  *pgx.Conn
	sonda time.Duration
}

// Esperar bloqueia até a próxima notificação, sondando a conexão no intervalo.
//
// O laço depende de um detalhe do pgx que vale escrever: `peekMessage` só marca
// a conexão como morta quando o erro NÃO é timeout de rede. O cancelamento por
// deadline de contexto chega como timeout, então a conexão continua íntegra e a
// espera pode recomeçar de onde parou. Fosse fatal, a sonda derrubaria a cada
// 30 s exatamente aquilo que ela existe para vigiar.
func (e *escutaPgx) Esperar(ctx context.Context) (string, string, error) {
	for {
		aguardo, cancelar := context.WithTimeout(ctx, e.sonda)
		n, err := e.conn.WaitForNotification(aguardo)
		cancelar()

		if err == nil {
			return n.Channel, n.Payload, nil
		}
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			return "", "", err
		}
		if err := e.conn.Ping(ctx); err != nil {
			return "", "", fmt.Errorf("sonda da escuta: %w", err)
		}
	}
}

func (e *escutaPgx) Fechar(ctx context.Context) error {
	ctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
	defer cancelar()
	return e.conn.Close(ctx)
}
