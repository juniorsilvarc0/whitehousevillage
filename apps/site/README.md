# `apps/site` — front de cliente

Site público do White House Village: é por aqui que o hóspede conhece a casa,
consulta datas e — quando a unificação terminar — **faz a reserva**. O painel
administrativo é o `apps/admin`; a regra de negócio é a `apps/api`.

> **Estado atual: dados mocados.** Veio do MVP de apresentação
> (`/Users/junior/DEV/spincode/whitehouse`, importado em 02/10/2026) e **ainda
> não fala com a API**. Tarifa, disponibilidade, estadia mínima, desconto e sinal
> são calculados em JavaScript, no navegador, a partir de
> `public/scripts/data.js`. Isso contraria as regras 1, 4 e 7 do `CLAUDE.md` e é
> exatamente o que a unificação desfaz — o plano, a ordem e o critério de pronto
> de cada passo estão em [`docs/unificacao-site-crm.md`](../../docs/unificacao-site-crm.md).
>
> Enquanto o passo A2 não entrar, **nenhum número desta pasta vale como preço**.

## Subir

O site entra junto com o resto do stack:

```bash
make up          # site em http://localhost:${SITE_PORT:-3200}
```

`public/` é montado como volume no desenvolvimento: editar um arquivo e dar F5
já reflete, sem rebuild.

## Páginas

| Rota | O que é |
| --- | --- |
| `/` | Home: hero em vídeo, as quatro acomodações, eventos, estrutura, localização |
| `/disponibilidade.html` | Consulta de datas, orçamento do hóspede e tabela de tarifas |
| `/admin/#/...` | **Back-office mocado, condenado.** Existe só como referência visual até o `apps/admin` cobrir o que ele desenha. Não recebe manutenção e sai no passo D1 |

Deep link que a página de disponibilidade entende:

```
/disponibilidade.html?produto=cobertura&checkin=2026-11-20&checkout=2026-11-23
```

## Estrutura

```
public/
  index.html              home
  disponibilidade.html    consulta de datas e orçamento
  admin/                  back-office mocado (condenado — ver passo D1)
  styles/                 tokens, base, componentes, home, disponibilidade, admin
  scripts/
    data.js               MOCK: produtos, tarifas, calendário, reservas, política
    main.js               header, menu, reveals, cards da home
    disponibilidade.js    calendário, orçamento, indicadores
    admin.js              back-office mocado (condenado)
  videos/hero.mp4         vídeo da home (12 MB)
nginx.conf                try_files, cache e gzip
Dockerfile                nginx:1.27-alpine, sem build
```

## Regra que não se negocia aqui

Esta pasta **não decide preço, não decide disponibilidade e não decide política**.
Ela pergunta. Enquanto `data.js` existir, cada número que ele devolve é uma
segunda fonte da verdade competindo com o banco — e a que o cliente vê.
