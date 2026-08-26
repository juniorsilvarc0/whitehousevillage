# Testes

> O que roda, o que cada suíte protege e as regras que valem para todo teste do repositório. O dono deste documento e de todo arquivo `*_test.go`, `*.test.tsx` e `tests/e2e/**` é o agente `qa-testes`.

## 1. As três camadas

| Camada | Comando | Precisa de banco? | Tempo |
|---|---|---|---|
| Unidade (Go) | `make test-api` | não | ~15 s |
| Componente (painel) | `make test-admin` | não | ~4 s |
| Integração (Go + Postgres) | `make test-integration` | **sim**, efêmero | ~40 s (até ~2 min quando o cenário A do overbooking cai no impasse — ver §3) |

`make check` roda lint + typecheck + as duas primeiras. A integração é alvo próprio porque sobe um container.

### Unidade

```bash
cd apps/api && go test ./... -race -count=1
```

Não toca em rede nem em banco. `-race` não é opcional: metade do que se testa aqui — limitador, pool, tabela de rotas — vive sob concorrência, e corrida que só aparece em produção custa uma madrugada.

### Componente

```bash
cd apps/admin && pnpm test --run     # vitest, jsdom
```

Configuração em `apps/admin/vitest.config.ts`. Só entram componentes de **decisão** — os que escondem, mostram ou recusam alguma coisa. Testar um `Card` que renderiza `children` mede o React, não o produto.

### Integração

```bash
make test-integration
```

Sobe um `postgres:16-alpine` descartável (container `whv-it-postgres`, porta `IT_PORT`, padrão 55432), aplica as migrations com `cmd/migrate`, roda `go test -tags=integration ./... -race` e derruba o container mesmo se o teste falhar.

Para apontar para outro banco:

```bash
cd apps/api
DATABASE_URL="postgres://whv:whv@localhost:55440/whv?sslmode=disable" \
  go test -tags=integration ./... -race -count=1
```

**Sem `DATABASE_URL` todo teste de integração é pulado** (`t.Skip`), e não falha — é o que permite `go test ./...` continuar verde na máquina de quem não subiu banco.

Os testes de integração **montam as próprias fixtures** (propriedade, unidades, perfis, usuários, catálogo de recursos) com `ON CONFLICT DO NOTHING`. Rodam num banco só migrado e num banco já semeado, sem diferença de resultado, e limpam o que criaram. Depender de `make seed` ter rodado antes tornaria a suíte impossível de executar em CI limpo.

**Uma exceção deliberada:** `TestPerfilCorretorSemeadoSalvaSemAlteracao` roda contra o perfil `corretor` que o **seed** instala, e não contra uma fixture. O motivo é o defeito que ele guarda: a matriz que quebrava era a real, com 26 células e quase toda em escopo `own`; uma fixture montada pelo teste só prova o que o teste já sabe, e foi exatamente por isso que a versão anterior desse cenário passava por cima do defeito. Sem seed o teste faz `t.Skip` com a instrução no texto — **`skip` aqui é cobertura perdida, não teste verde**. Para exercitá-lo, semeie antes:

```bash
cd apps/api
DATABASE_URL="$URL" go run ./cmd/migrate up
DATABASE_URL="$URL" go run ./cmd/seed
DATABASE_URL="$URL" go test -tags=integration ./... -race -count=1
```

> ⚠ **`make test-integration` ainda não roda o seed** — só `migrate`. Enquanto o alvo não ganhar o passo (`go run ./cmd/seed` entre as migrations e o `go test`), esse cenário fica pulado em CI. O `Makefile` é do tech-lead.

O seed é **idempotente por contrato** e a suíte depende disso: rodá-lo duas vezes seguidas deixa `criadas=0, atualizadas=0, inalteradas=275` e a contagem de linhas idêntica em `units`, `unit_types`, `resources`, `roles`, `role_permissions`, `users` e `rates`. Um seed que duplicasse na segunda execução quebraria toda fixture que resolve por chave natural.

## 2. O que cada suíte protege

### `internal/platform/db/stayblocks_concurrency_integration_test.go` — o teste que não pode faltar

A regra 2 do `CLAUDE.md` diz que overbooking é impedido pelo **banco**, não por `SELECT` seguido de `INSERT`. Este arquivo é a prova, em quatro cenários:

| Cenário | Pergunta de negócio |
|---|---|
| **A** | 50 pessoas clicam "reservar" na mesma cobertura, no mesmo segundo. Uma leva; as 49 ouvem "a data acabou de ser ocupada" (409), nenhuma ouve "erro interno" (500). |
| **B** | Alguém fecha a cobertura enquanto outro fecha a casa inteira para um casamento. Exatamente um vence — e a casa nunca entra pela metade: ou as 8 unidades, ou nenhuma. |
| **C** | Quem sai dia 23 libera a unidade para quem entra dia 23. É o `daterange` half-open `[in, out)`, e é dinheiro: recusar back-to-back esvazia uma noite entre cada duas estadias. |
| **D** | A pré-reserva tira a data do mercado sem pagamento; expirada pelo job, a data volta a ser vendável na hora. |

Fecha com uma varredura da tabela inteira procurando duas ocupações da mesma unidade que se sobreponham (`&&`). Se qualquer cenário tivesse deixado passar, apareceria ali.

Não há mock: `EXCLUDE USING gist` não tem equivalente em memória, e um falso que "verifica antes de inserir" passaria no teste perdendo exatamente a corrida que ele existe para cobrir.

### `internal/auth/sessao_integration_test.go`

Login (senha certa, senha errada, e-mail inexistente, conta desativada e e-mail bloqueado dão respostas indistinguíveis), bloqueio após 5 erros, rotação do refresh com `replaced_by`, detecção de reuso revogando a família, expiração, logout idempotente, recuperação de senha de uso único, morte imediata da sessão de conta desativada e a promessa de que **a matriz nova vale na requisição seguinte, sem novo login**.

### `internal/router/api_integration_test.go`

A API pela porta da frente, por HTTP, contra Postgres real: cookie `wh_refresh` `HttpOnly`/`Lax`/`Path=/api/v1/auth` e o refresh **nunca** no corpo; 403 para o perfil sem permissão em todos os verbos; escopo `own` restringindo a listagem **e o `meta.total`**; 404 (não 403) fora do escopo; `409 EMAIL_IN_USE`; `DELETE` que desativa sem apagar e `PATCH {active:true}` que reativa; perfil de sistema imutável; perfil com gente dentro que não se apaga; `/readyz` com a versão do schema.

### `internal/router/regressao_criticos_integration_test.go` — regressão dos três críticos

Os três defeitos que reprovaram a Fase 0 em duas revisões independentes tinham, cada um, teste verde por cima. Este arquivo existe para que não voltem, e entra **sempre pela porta da frente** — HTTP, status, `code` — conferindo o **banco** onde o defeito era de persistência. Perguntar ao service "deu certo?" é acreditar na resposta do código sob suspeita.

| Teste | O que trava |
|---|---|
| `TestReusoDeRefreshMataAFamiliaNoBancoEDerrubaOLadrao` | Depois do `401 TOKEN_REUSED`, lê `refresh_tokens` **por `family_id`** e exige `count(*) = count(revoked_at)`; depois **tenta renovar com o token que o ladrão vinha usando** e exige 401. Falha se a revogação voltar para dentro da transação que devolve o erro — o rollback a desfaz. |
| — (mesmo teste) | Uma segunda sessão do mesmo usuário, aberta antes e nunca tocada pelo ladrão, **sobrevive**. Separa "revogou a família certa" de "deslogou a pessoa de todos os aparelhos", que é outro defeito e faria a asserção principal passar por acidente. E o dono consegue entrar de novo: o alarme não pode trancar a conta. |
| `TestLogoutNaoEhTratadoComoRouboDeSessao` | Sair do sistema devolve `TOKEN_INVALID`, não `TOKEN_REUSED`. Sem isso, todo logout vira alarme de segurança e o alarme deixa de significar alguma coisa. |
| `TestNemOAdministradorTrocaOProprioPapel` | **Ninguém** altera o próprio `role_id` — nem quem tem `roles:editar`. Com dois controles positivos: reenviar o mesmo papel passa (senão o `PUT`, que exige `role_id` no corpo, ficaria inutilizável) e o administrador continua atribuindo papel a terceiro (senão uma trava que recusasse tudo passaria). |
| `TestEscopoValidoVemDoBancoEAceitaRecursoNovoSemRecompilar` | **Insere um recurso cujo código é sorteado em tempo de execução**, com `supports_own = true`, e exige que `/roles/resources` o ofereça com `own` e que o `PUT` da matriz o aceite. Depois faz `UPDATE resources SET supports_own = false` e exige que o **mesmo processo**, sem reiniciar, passe a responder 422. Nenhum mapa Go satisfaz isso: o código não existia quando o binário foi compilado. |
| `TestAcoesValidasVemDoBancoPorRecurso` | O mesmo para `actions`: recurso que o banco diz oferecer só `ver` recusa `excluir` (422) e aceita `ver` (200). |
| `TestPerfilCorretorSemeadoSalvaSemAlteracao` | Abre o perfil **semeado** `corretor`, confere que ele tem célula em `own` (senão o cenário não alcança o defeito) e salva a matriz idêntica: 200, e volta célula por célula igual. |

**Poder de detecção verificado por mutação.** Injetando no teste, depois do 401, o estado exato que o defeito produzia (`UPDATE refresh_tokens SET revoked_at = NULL WHERE family_id = $1`), as duas asserções disparam:

```
regressao_criticos_integration_test.go:264: 2 de 2 tokens da família ficaram SEM revoked_at — a revogação
    foi desfeita pelo rollback da transação que devolveu TOKEN_REUSED; a sessão roubada continua viva
regressao_criticos_integration_test.go:274: o token que o ladrão vinha usando respondeu 200 — ele continua
    renovando a sessão depois de o sistema ter acusado o roubo
```

O crítico 3 carrega a própria mutação embutida: o `UPDATE supports_own` no meio do teste **é** o experimento de controle, e roda toda vez.

### `internal/router/contract_test.go` — teste de contrato (roda sem banco)

Varre a tabela declarativa de `routes.go` e a compara com `openapi/openapi.yaml`:

- toda rota servida existe no contrato, com o mesmo verbo;
- todo recurso CRUD expõe os seis verbos, nas **duas** fontes (`docs/api.md` §2);
- toda rota protegida declara recurso + ação, e a que não checa permissão declara o motivo por escrito;
- o que é público no código é `security: []` no contrato, e vice-versa;
- `SchemaVersionEsperada` acompanha a última migration entregue.

### `apps/admin/src/config/navigation.test.ts`

O menu do corretor: sem Configurações, sem Relatórios, sem Inventário, sem Canais e sem Financeiro — recebíveis, pagáveis e conciliação são o "financeiro global" que a spec §11 fecha para ele. **Comissões aparece**, e é assim mesmo: a matriz dá `finance.commissions` em escopo `own`, que é o painel de comissões previstas e pagas prometido no §11. Também garante que a filtragem só **tira** itens — perfil nenhum ganha acesso por omissão — e que as abas do celular apontam para telas que os três perfis alcançam.

> Estes testes descrevem o comportamento **de hoje**, que inclui um defeito: eles fixam a lista literal de destinos do corretor, e nessa lista falta `/app/agenda`, que a matriz do seed concede. Ver `menu-e-dado.test.ts` logo abaixo e a seção 3.

### `apps/admin/src/lib/auth/permissions.test.ts`

Guarda o vocabulário: todo código de recurso citado pelo painel — no mapa de destinos e nos atalhos do Painel — existe no catálogo do banco. O teste **lê `apps/api/cmd/seed/acesso.go`** e compara conjunto a conjunto, e varre o `src` inteiro atrás de literais `recurso: "..."`. Foi assim que se descobriu que os três códigos fantasmas (`availability`, `finance`, `commissions`) estavam em **dois** lugares, não um — escondendo Mapa, Financeiro e Comissões de todo mundo, inclusive do administrador. Código fora do catálogo não protege ninguém: esconde a tela de todos, porque ninguém pode receber permissão num recurso que não existe.

### `apps/admin/src/lib/auth/menu-e-dado.test.ts` — ⚠ vermelho por defeito de produção

A regra 8 dita como propriedade: **o menu é função da matriz, não do nome do perfil**. Três casos — dois vermelhos, um verde de controle:

- *dois perfis com a mesma matriz enxergam o mesmo menu* — a prova mais curta do defeito. Duas pessoas com matriz idêntica, diferentes só no `code`, recebem menus diferentes;
- *não esconde tela que a matriz concedeu* — o dano pelo lado do usuário: o corretor tem `agenda` em `own` e não vê o item;
- *continua escondendo o que a matriz não concede* — o controle. Sem ele, "mostrar tudo para todo mundo" satisfaria os dois primeiros e abriria o financeiro global ao corretor. Passa hoje e tem de continuar passando depois do conserto.

### `apps/admin/src/app/login/login-form.test.tsx`

A tela de login como componente de decisão: a mensagem de erro é **a mesma** para e-mail inexistente, senha errada e bloqueio (o contrato usa um único `INVALID_CREDENTIALS`; distinguir na tela devolveria a enumeração de usuários que a API fecha de propósito), a validação segura o envio antes de gastar uma das cinco tentativas, e a tela reage ao `code`, nunca ao texto que a API mandou.

## 3. Estado atual: três testes vermelhos, dois defeitos abertos

Dos seis defeitos que a rodada anterior registrou aqui, **cinco foram corrigidos e verificados** — os quatro do backend, o do `/readyz` e o do painel. Os blocos `⚠ ESTE TESTE ESTÁ VERMELHO` correspondentes saíram dos arquivos, e cada um ganhou cobertura de regressão (seção 2).

Sobram **dois** — um que já estava aberto e um que ninguém guardava:

| Teste | O que quebra para o negócio | Conserto em |
|---|---|---|
| `TestOverbookingEhImpedidoPeloBanco/A_...` | Sob 50 pedidos simultâneos da mesma data, o Postgres devolve `40P01 deadlock detected` em vez de `23P01`. Quem perde a corrida recebe **"erro interno" (500)** no lugar de "a data acabou de ser ocupada" (409) — vai embora achando que o site quebrou, e a operação recebe alarme de incidente numa situação que é rotina de véspera de Réveillon. | `internal/platform/db` |
| `menu-e-dado.test.ts` (2 casos) | O menu do painel ainda decide por **nome de perfil**: `navigation.ts` carrega `allowedRoles: readonly Role[]` com `"admin" \| "usuario" \| "corretor"` escrito no código. A lista compilada **tira telas que a matriz concedeu** — o corretor tem `agenda` em escopo `own` e não vê o item. A gestão marca a permissão na tela de perfis, salva, e nada muda. | `src/config/navigation.ts` (tech-lead) |

**Medido, não estimado:** 12 execuções isoladas do cenário A contra um Postgres 16 limpo → **8 passaram, 4 falharam (33%)**. Nas piores, **49 dos 49 perdedores** receberam 500 e a resposta demorou ~2 s cada (o detector de impasse do Postgres só age depois de `deadlock_timeout`). Numa das falhas o estrago foi parcial (46 × 409, 3 × 500), o que mostra que não há um limiar seguro — só probabilidade.

Duas causas somadas, ambas em `internal/platform/db`:

1. **`MapError` não traduz `40P01`.** A constante `sqlstateDeadlock` existe em `errors.go` e é usada por `ehTransiente`, mas **não há `case` para ela no `switch pg.Code`** — o erro cai no `default` e vira `apperr.Internal` → HTTP 500.
2. **`TxManager.Do` repete a transação uma única vez** (`for tentativa := 0; tentativa < 2`). O log do teste mostra `transações executadas: 100` para 50 pedidos: todos repetiram, e todos caíram no mesmo impasse. Sob dezenas de disputantes, uma repetição não basta.

### O defeito do painel, em uma linha

Mesma matriz, menus diferentes — só o nome do perfil muda:

```
corretor     → /app, /app/mapa, /app/reservas, /app/funil, /app/chat, /app/comissoes
plantonista  → /app, /app/mapa, /app/reservas, /app/funil, /app/chat, /app/comissoes, /app/agenda
```

O perfil novo criado em `/roles` não aparece em `allowedRoles` nenhum, então cai na filtragem só por matriz — e enxerga a Agenda que a matriz concede. O corretor, que tem **exatamente a mesma permissão**, não enxerga, porque `navigation.ts` marca a Agenda como `GESTAO`. É a regra 8 invertida: o dado concede e o código retira.

Consequência prática: qualquer perfil novo que a gestão criar copiando o Corretor vai se comportar **diferente** do Corretor, e a diferença não está em lugar nenhum que a gestão possa ver.

O conserto (`allowedRoles` → recurso + ação, deixando a matriz decidir sozinha) vai virar vermelhas duas expectativas que hoje codificam o defeito: o caso de `navigation.test.ts` que exige `/app/agenda` escondido do corretor, e a lista literal de hrefs do corretor em `permissions.test.ts`. As duas precisam ser atualizadas **no mesmo commit** do conserto — estão anotadas no cabeçalho de `menu-e-dado.test.ts`.

> **Esta falha é intermitente, e a intermitência é o perigo.** `go test -tags=integration ./...` já passou verde nesta mesma árvore, com este mesmo defeito presente — foi assim que ele chegou até aqui. Um CI que rode a suíte uma vez por commit vai **relatar sucesso em 2 de 3 execuções**. Enquanto o conserto não sai, o cenário A precisa ser lido como "ainda vermelho", não como "passou hoje".

O conserto é do backend/tech-lead. O QA não edita código de produção: o teste fica vermelho, com a mensagem em linguagem de negócio, até o defeito sair.

## 4. Política

- **Sem `sleep`.** Espera é por condição. Onde o tempo faz parte da regra (expiração de pré-reserva, refresh vencido), o teste **simula o efeito** — o `UPDATE` que o job faria, a data gravada no passado — em vez de esperar o relógio. Teste que dorme é teste que fica lento e, mais cedo ou mais tarde, intermitente.
- **Datas determinísticas.** Nada de `time.Now()` solto. Os cenários de calendário usam datas fixas e distantes (2031), para não colidirem com dado real nem entre si.
- **Dinheiro em centavos inteiros.** Comparação de valor é `int64`; float em asserção de dinheiro é o mesmo bug de produção, só que no teste.
- **Integração contra Postgres real, nunca mock de banco.** Constraint, `daterange`, escopo `own` no `WHERE` e violação de unicidade não existem fora do Postgres. Um dublê de repositório só prova que o dublê concorda com o service.
- **Teste contra o contrato, não contra a implementação.** As asserções são sobre status, `code` de erro, envelope e efeito observável. Teste que só passa porque conhece o caminho interno do service não protege ninguém e quebra na primeira refatoração honesta.
- **Fixture própria e limpeza própria.** Cada teste cria o que precisa com sufixo único e apaga no `t.Cleanup`. Teste que depende do estado deixado por outro falha na ordem errada.
- **Defeito encontrado vira teste vermelho, não conserto.** O `qa-testes` não edita código de produção: escreve o teste que expõe o defeito, deixa falhando com mensagem em linguagem de negócio e reporta. Todo teste vermelho por defeito de produção carrega, no próprio arquivo, um bloco `⚠ ESTE TESTE ESTÁ VERMELHO E É DEFEITO DE PRODUÇÃO` dizendo a causa e de quem é o conserto.

## 5. Como ler uma falha

A mensagem de falha é escrita para quem **não** está com o código aberto. `"de 50 pedidos simultâneos, só 0 receberiam 409 DATE_CONFLICT"` diz o que o hóspede veria; `"expected true to be false"` não diz nada. Quando a falha for de concorrência, a mensagem traz também quantas transações precisaram ser repetidas — é o número que separa "o banco resolveu" de "o cliente levou o erro".

## 6. O que ainda não existe

- **e2e (Playwright)** — `tests/e2e/` ainda não tem suíte: login por perfil, criar pré-reserva, mover card no funil, mandar mensagem no chat e cancelar aplicando política dependem de telas que ainda não foram construídas.
- **Mesa de 20 cenários de tarifa e teste de propriedade** (spec §4) — o motor de orçamento em `internal/domain` ainda não recebeu a implementação completa; a mesa entra junto com ela.
- **Teste de idempotência (`Idempotency-Key`)** — nenhum endpoint atual cria reserva ou dinheiro.
- **Job de expiração de pré-reserva** — o cenário D simula o `UPDATE` que o job faz; quando o job existir, ele ganha teste próprio.
