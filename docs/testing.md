# Testes

> O que roda, o que cada suíte protege e as regras que valem para todo teste do repositório. O dono deste documento e de todo arquivo `*_test.go`, `*.test.tsx` e `tests/e2e/**` é o agente `qa-testes`.

## 1. As três camadas

| Camada | Comando | Precisa de banco? | Tempo |
|---|---|---|---|
| Unidade (Go) | `make test-api` | não | ~35 s |
| Componente (painel) | `make test-admin` | não | ~4 s |
| Integração (Go + Postgres) | `make test-integration` | **sim**, efêmero | ~3 min (serializada com `-p 1`) |

`make check` roda lint + typecheck + as duas primeiras. A integração é alvo próprio porque sobe um container.

> **`make check` exige `golangci-lint`, e nada no repositório o instala.** Sem
> ele o alvo morre em `command not found` no primeiro passo, antes de rodar um
> teste. Não há `.golangci.yml` nem alvo de ferramentas; o CI também não o roda.
> Para exercitá-lo hoje: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6`
> **de fora do módulo** (`cd /tmp`), para não encostar no `go.mod` da API.

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
  go test -tags=integration -p 1 ./... -race -count=1
```

**`-p 1` não é opcional.** Os pacotes compartilham um Postgres só e, em paralelo,
disputam as mesmas linhas do seed — a mesma propriedade, as mesmas oito unidades,
o mesmo tarifário. Medido: 3 falhas em 4 execuções paralelas contra 0 em 3
serializadas. Sem a serialização, a suíte acusa defeito onde só houve dois testes
mexendo na mesma casa.

**Quando o disco do Docker estiver cheio.** A VM do Docker Desktop é um disco
só, compartilhado por todos os projetos da máquina. Cheia, o Postgres efêmero
sobe e o `initdb` até passa — e a suíte morre no meio com
`could not extend file "base/…": No space left on device (SQLSTATE 53100)`, que
parece defeito de produto e não é. Medido nesta árvore: 33 MB livres de 31,4 GB,
e 42 testes falhando por isso.

A saída que **não** mexe no disco de ninguém é pôr o `PGDATA` em `tmpfs`, que
sai da RAM da VM em vez do disco:

```bash
docker run -d --name whv-qa-pg \
  -e POSTGRES_USER=whv -e POSTGRES_PASSWORD=whv -e POSTGRES_DB=whv_qa \
  -e TZ=America/Fortaleza -e PGDATA=/pgtmp/data \
  --tmpfs /pgtmp:rw,size=3g,mode=1777 \
  -p 55480:5432 postgres:16-alpine
```

O banco morre junto com o container, que é o que já se esperava dele. 3 GB
cobrem a suíte inteira com folga (o pico medido foi bem abaixo disso).

Antes de sair apagando: volume nomeado é de projeto alheio. O que sobra dos
containers efêmeros desta árvore são volumes **anônimos** (a imagem do Postgres
declara `VOLUME` no `PGDATA`, e `docker rm -f` sem `-v` os deixa para trás) —
`docker volume ls -qf dangling=true` e `docker volume inspect` mostram data de
criação, que é como se separa o lixo do dia do dado de outra pessoa.

**Sem `DATABASE_URL` todo teste de integração é pulado** (`t.Skip`), e não falha — é o que permite `go test ./...` continuar verde na máquina de quem não subiu banco.

Os testes de integração **montam as próprias fixtures** (propriedade, unidades, perfis, usuários, catálogo de recursos) com `ON CONFLICT DO NOTHING`. Rodam num banco só migrado e num banco já semeado, sem diferença de resultado, e limpam o que criaram. Depender de `make seed` ter rodado antes tornaria a suíte impossível de executar em CI limpo.

**Uma exceção deliberada:** `TestPerfilCorretorSemeadoSalvaSemAlteracao` roda contra o perfil `corretor` que o **seed** instala, e não contra uma fixture. O motivo é o defeito que ele guarda: a matriz que quebrava era a real, com 26 células e quase toda em escopo `own`; uma fixture montada pelo teste só prova o que o teste já sabe, e foi exatamente por isso que a versão anterior desse cenário passava por cima do defeito. Sem seed o teste faz `t.Skip` com a instrução no texto — **`skip` aqui é cobertura perdida, não teste verde**. Para exercitá-lo, semeie antes:

```bash
cd apps/api
DATABASE_URL="$URL" go run ./cmd/migrate up
DATABASE_URL="$URL" go run ./cmd/seed
DATABASE_URL="$URL" go test -tags=integration ./... -race -count=1
```

> **O alvo ganhou o passo do seed.** `make test-integration` hoje é `migrate` → **`seed`** → suíte serializada → repetição dos testes de disputa (`REPETICOES_CONCORRENCIA`, padrão 10). O cenário do perfil semeado deixou de ser pulado em CI.
>
> Isso passou a valer para mais do que ele: **a jornada da Fase 1 e os invariantes de `internal/router` também exigem seed** e fazem `t.Skip` com a instrução no texto quando não o encontram. Um `skip` ali é a jornada inteira do produto saindo da execução sem ninguém reparar — é por isso que a etapa do seed é obrigatória, e não uma conveniência.

O seed é **idempotente por contrato** e a suíte depende disso: rodá-lo duas vezes seguidas deixa `criadas=0, atualizadas=0, inalteradas=276` e a contagem de linhas idêntica em `units`, `unit_types`, `resources`, `roles`, `role_permissions`, `users` e `rates`. Um seed que duplicasse na segunda execução quebraria toda fixture que resolve por chave natural.

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

### `internal/router/jornada_fase1_integration_test.go` — a jornada que prova a Fase 1

Sobe o servidor **inteiro** (`router.New`, com inventário, tarifário, disponibilidade e reservas montados) contra Postgres semeado e percorre a venda na mesma ordem em que um corretor a percorre na tela:

| Passo | O que a asserção protege |
|---|---|
| `GET /availability` da Cobertura em 20–23/11/2026 | As três noites aparecem vendáveis, cada uma com a diária do seu tipo de data (fds R$ 2.400 ×2, normal R$ 1.900). A janela é half-open: pedir 20→23 devolve **3** dias, não 4. |
| `POST /quotes` | Subtotal R$ 6.700 + limpeza R$ 350 = **R$ 7.050**, sinal **R$ 3.525**. E o preço que o calendário mostrou é o mesmo que o orçamento cobra, noite a noite — duas fontes de preço na mesma tela é como o hóspede descobre um valor na consulta e outro na proposta. |
| `POST /reservations` | Nasce em `hold`, com `hold_expires_at` ~48 h à frente (o `hold_hours` da política V1), código `WH-AAAA-NNNN`, a COB-01 alocada, e os três congelamentos preenchidos (`rate_table_id`, `policy_version`, `cancellation_policy_id`). |
| Vender a Casa Completa por cima | **409 `DATE_CONFLICT`**. A Completa consome as oito unidades; com a COB-01 ocupada ela é invendável — e quem tentar ouve "a data acabou de ser ocupada", nunca "erro interno". |
| Repetir o POST com a **mesma** `Idempotency-Key` | Devolve a **mesma** reserva, e o banco fica com uma linha só. É o duplo clique do corretor e o retry do celular no elevador. |
| `POST /confirm` | Vira `confirmed`, `hold_expires_at` **some** (senão o job de expiração derrubaria uma reserva paga) e o total **não** é recalculado. |
| `POST /cancel` | O `?dry_run=1` e a execução dão o mesmo número, calculado sobre o sinal **efetivamente pago** e pela versão de política que a venda congelou. |
| A data volta ao mapa | A Cobertura volta a `available: 1`, a Casa Completa volta junto, o mapa mostra `livre` — e a reserva cancelada continua no banco, porque o histórico fica. |

Por que não vive em `internal/modules/*`: cada módulo monta só as próprias rotas. Nenhum deles atravessa a fronteira entre **disponibilidade**, que precifica, e **reservas**, que vende — e é ali que o dinheiro passa de um lado para o outro. Se um módulo mudar de contrato sem avisar o vizinho, é aqui que quebra.

**Poder de detecção verificado por mutação.** Um teste de jornada é longo, e um teste longo que passa sempre é indistinguível de um teste que não afirma nada. Injetando no meio dele o estado que o defeito produziria — `UPDATE stay_blocks SET status = 'confirmed' WHERE reservation_id = ...`, que é o cancelamento esquecendo de liberar o calendário — as asserções disparam:

```
2026-11-20: depois do cancelamento a Cobertura continua com 0 disponível — a data
    ficou presa e ninguém pode vendê-la
2026-11-20: a Casa Completa continua invendável depois de a Cobertura ter sido liberada
mapa: COB-01 em 2026-11-20 ficou "confirmed" depois do cancelamento, esperado livre
```

`TestCancelarComDezDiasDeAntecedenciaReteMetadeDoSinal` mora ao lado, separado de propósito: a antecedência é medida contra o **dia da casa**, então provar a faixa do meio (7 a 29 dias → retém metade) exige um check-in relativo a hoje. Amarrar isso às datas fixas de novembro faria o resultado mudar conforme o mês em que a suíte roda.

### `internal/router/invariantes_fase1_integration_test.go`

**`TestReajusteDeTarifaNaoAlcancaVendaJaEmitida`** — a regra 7 do `CLAUDE.md` medida entre dois donos. A gestão reajusta a diária de fim de semana da Cobertura de R$ 2.400 para R$ 3.000 **pela API real** (`PATCH /rates/{id}`, com um perfil de `settings`, não de `reservations`); a reserva emitida antes continua valendo R$ 7.050, noite a noite. Tem controle: um orçamento pedido **depois** do reajuste tem de cobrar o preço novo — sem ele, um sistema que simplesmente ignorasse o `PATCH` passaria na asserção principal e ninguém notaria que o reajuste não pegou em lugar nenhum.

Mutação, do mesmo jeito: reescrevendo `reservation_nights` e `reservation_pricing` com o preço novo — que é o que um sistema sem snapshot faria sozinho — o teste acusa em linguagem de negócio:

```
depois do reajuste a reserva WH-2026-0150 passou a valer total 825000 / subtotal 790000 /
    sinal 412500, e o hóspede fechou por 705000 / 670000 / 352500
a noite de 2026-11-20 foi reprecificada para 300000 — ela foi VENDIDA por 240000
```

**`TestTempoDeRespostaDoMapaEDoOrcamento`** — os dois tetos que a operação sente, medidos por HTTP com RBAC e serialização no caminho, porque o que o corretor espera não é o tempo da consulta, é o tempo da tela. Reporta o **melhor de 5** execuções, não a média: numa máquina de desenvolvimento o pior caso mede o barulho do laptop.

| Medição | Teto | Banco recém-semeado | Com 9.448 blocos de ocupação |
|---|---|---|---|
| Mapa, 90 dias × 8 unidades | 300 ms | 22,2 ms | **33,1 ms** |
| Disponibilidade, 90 dias × 4 produtos | 300 ms | 16,7 ms | **24,1 ms** |
| Orçamento de 3 noites | 150 ms | 5,0 ms | **5,0 ms** |

(com `-race`, que é o caso pessimista; sem ele o mapa carregado responde em 5,7 ms). Folga de 9× no mapa e de 30× no orçamento.

**Remedido em 26/08/2026, na rodada de correção.** O teste passa a régua no mapa
e na disponibilidade e só então morre no orçamento, por causa do defeito 1 da
§3.2 — então os dois primeiros números saem do próprio teste e o do orçamento
teve de ser medido por fora, com a API de pé e `curl`, mandando o campo que a
implementação hoje aceita:

| Medição | Teto | Medido | Folga |
|---|---|---|---|
| Mapa, 90 dias × 8 unidades (`-race`, dentro do teste) | 300 ms | **18,8 ms** | 16× |
| Disponibilidade, 90 dias × 4 produtos (`-race`, dentro do teste) | 300 ms | **12,3 ms** | 24× |
| Mapa, por HTTP, sem `-race` | 300 ms | **3,9 ms** | 77× |
| Orçamento de 3 noites, por HTTP, sem `-race` | 150 ms | **2,8 ms** | 53× |

O orçamento medido por fora fecha em `total 705000 / sinal 352500` — os
R$ 7.050 e os R$ 3.525 da Tabela V1. **O motor de preço está certo; o que está
quebrado é o nome do campo na porta.**

### `internal/router/regressao_fase1_integration_test.go` — os achados da revisão, na COSTURA

Cada módulo guarda o próprio achado na própria suíte, e guarda bem. O que
nenhuma delas alcança é o **sistema montado**: `internal/modules/*` sobe só as
rotas do próprio módulo, e por isso um módulo pode fechar o defeito do lado dele
e o defeito continuar de pé para quem usa o produto. Dois achados desta rodada
são exatamente isso, e é por eles que o arquivo existe.

| Teste | O que trava |
|---|---|
| `TestNenhumCaminhoDeixaHospedeEstranhoDentroDaCasaExclusiva` | O CRÍTICO 1 percorrido inteiro: desativar AP-03 → vender a Completa → reativar. Exige que **as três portas estejam fechadas ao mesmo tempo** — basta uma aberta para o caminho voltar. A porta 2 é alcançada pelo **banco**, não pela API, porque unidade já inativa é dado legado que a porta 1 não protege. Fecha varrendo `reservations` atrás de venda `all_members` com menos `reservation_units` do que a composição declara. Reprova se alguém reintroduzir o filtro `u.active` nas candidatas. |
| `TestOMapaContinuaMostrandoQueHouveHospedeDepoisDoCheckOut` | O MÉDIO 7 perguntado ao **endpoint do mapa**, e não a uma consulta que o teste escreve. Com os dois controles: o mapa mostra o hóspede **antes** do check-out (senão passaria num mapa que nunca mostra nada) e a noite **volta a ser vendável depois** (senão passaria num sistema que não libera a data — o defeito oposto, que some com o estoque). |
| `TestTodaEscritaDaFase1DeixaTrilhaCompletaNoSistemaMontado` | O ALTO 3 pelo `router.New` de verdade. Exige ator, entidade, propriedade, `request_id`, `ip` e `user_agent` em **toda** linha, e varre `audit_log` atrás de hash `argon2`/`bcrypt`, JWT e campo de segredo em claro. |
| `TestACasaNuncaDevolveMaisDinheiroDoQueOHospedePagou` | O ALTO 2 pelos dois lados: sinal acima do total e sinal zero são recusados, **pagar a estadia inteira adiantada é aceito** (o controle positivo — sem ele, uma trava que recusasse todo sinal alto passaria), e o `/cancel` fecha a conta: devolvido + retido = pago, nada negativo, devolução nunca maior que o sinal. |
| `TestOrcamentoAceitaOCampoQueOContratoEOPainelMandam` | O corpo que o `openapi.yaml` declara e o painel manda tem de calcular orçamento. Com o controle inverso: o nome antigo `guests` tem de ser **recusado** — aceitar os dois nomes para sempre é como a ambiguidade nasceu. |

**A lição de método desta rodada está no MÉDIO 7.** A suíte de `reservas` confere
a estadia preservada com uma consulta que **o próprio teste escreve**:

```sql
SELECT ... FROM stay_blocks WHERE reservation_id = $1
  AND status IN ('hold','confirmed','completed')
```

Ela passa. E o mapa continua devolvendo `livre`, porque o endpoint usa outro
predicado. O teste provava o predicado que o mapa **deveria** usar, não o que
ele usa — verde legítimo sobre defeito vivo. Quando a asserção é sobre um
comportamento que o usuário observa, a pergunta vai para o endpoint.

### `internal/router/contract_test.go` — teste de contrato (roda sem banco)

Varre a tabela declarativa de `routes.go` e a compara com `openapi/openapi.yaml`:

- toda rota servida existe no contrato, com o mesmo verbo;
- todo recurso CRUD expõe os seis verbos, nas **duas** fontes (`docs/api.md` §2);
- toda rota protegida declara recurso + ação, e a que não checa permissão declara o motivo por escrito;
- o que é público no código é `security: []` no contrato, e vice-versa;
- `SchemaVersionEsperada` acompanha a última migration entregue.

### `apps/admin/src/config/navigation.test.ts`

O menu do corretor: sem Configurações, sem Relatórios, sem Inventário, sem Canais e sem Financeiro — recebíveis, pagáveis e conciliação são o "financeiro global" que a spec §11 fecha para ele. **Comissões aparece**, e é assim mesmo: a matriz dá `finance.commissions` em escopo `own`, que é o painel de comissões previstas e pagas prometido no §11. Também garante que a filtragem só **tira** itens — perfil nenhum ganha acesso por omissão — e que as abas do celular apontam para telas que os três perfis alcançam.

> A nota de defeito que vivia aqui saiu: `allowedRoles` deixou de existir em `navigation.ts`, cada item passou a declarar **um recurso do catálogo**, e `/app/agenda` entrou na lista de destinos do corretor — que é onde a matriz do seed sempre disse que ela estava.

### `apps/admin/src/lib/auth/permissions.test.ts`

Guarda o vocabulário: todo código de recurso citado pelo painel — no mapa de destinos e nos atalhos do Painel — existe no catálogo do banco. O teste **lê `apps/api/cmd/seed/acesso.go`** e compara conjunto a conjunto, e varre o `src` inteiro atrás de literais `recurso: "..."`. Foi assim que se descobriu que os três códigos fantasmas (`availability`, `finance`, `commissions`) estavam em **dois** lugares, não um — escondendo Mapa, Financeiro e Comissões de todo mundo, inclusive do administrador. Código fora do catálogo não protege ninguém: esconde a tela de todos, porque ninguém pode receber permissão num recurso que não existe.

### `apps/admin/src/lib/auth/menu-e-dado.test.ts` — verde desde o conserto do menu

A regra 8 dita como propriedade: **o menu é função da matriz, não do nome do perfil**. Os três casos passam:

- *dois perfis com a mesma matriz enxergam o mesmo menu* — duas pessoas com matriz idêntica, diferentes só no `code`, recebem o mesmo menu. Era a prova mais curta do defeito antigo;
- *não esconde tela que a matriz concedeu* — o corretor tem `agenda` em `own` e **vê** o item;
- *continua escondendo o que a matriz não concede* — o controle, que separa "o menu obedece à matriz" de "o menu mostra tudo para todo mundo". Passava antes do conserto e continua passando depois, que é exatamente o que se pedia dele.

O teste continua no lugar como **regressão**: ele é a única coisa que impede `allowedRoles` de voltar disfarçado na próxima tela nova.

### `apps/admin/src/app/login/login-form.test.tsx`

A tela de login como componente de decisão: a mensagem de erro é **a mesma** para e-mail inexistente, senha errada e bloqueio (o contrato usa um único `INVALID_CREDENTIALS`; distinguir na tela devolveria a enumeração de usuários que a API fecha de propósito), a validação segura o envio antes de gastar uma das cinco tentativas, e a tela reage ao `code`, nunca ao texto que a API mandou.

## 3. Estado atual: o portão, e nove testes vermelhos em quatro defeitos

Medido em 26/08/2026, ao fim da rodada de correção, contra Postgres 16 efêmero
próprio com migrations `20260826120000` e seed completo.

### 3.1 O portão, passo a passo

| Passo de `make check` | Resultado |
|---|---|
| `golangci-lint run ./...` | **3 problemas** — para o `make check` aqui |
| `pnpm lint` (`eslint src --max-warnings 0`) | ✅ zero |
| `pnpm exec tsc --noEmit` | ✅ zero |
| `go test ./... -race -count=1` | **3 falhas** (2 pacotes) |
| `pnpm test --run` | ✅ 13 arquivos, 93 testes |

Fora do `make check`, medidos à parte:

| | Resultado |
|---|---|
| `pnpm build` | ✅ 6/6 páginas, 13 rotas |
| `migrate up` (do zero) | ✅ 6/6, `20260826120000 dirty=false` |
| `seed` 1ª / 2ª | ✅ 278 criadas / **0 criadas, 278 inalteradas** — "nada mudou" |
| Integração `-p 1 -race` | ✅ **14 pacotes verdes**, 1 vermelho (`internal/router`) |
| Concorrência `-count=10` | ✅ 8 testes × 10 repetições, verde (2 min 26 s) |

**`golangci-lint` não está instalado na máquina de desenvolvimento e nada no
repositório o instala** — não há `.golangci.yml`, não há alvo de ferramentas, e
o `make check` morre em `command not found` antes de rodar um teste sequer. Foi
preciso instalá-lo à mão (`go install …/golangci-lint@v2.1.6`) para saber o que
ele diz. Os 3 problemas que ele encontrou estão na tabela de defeitos abaixo.

**O CI não é o portão.** `.github/workflows/ci.yml` roda `gofmt`, `go vet`,
`go test`, migrations, integração, `tsc --noEmit` e `pnpm build`. Ele **não**
roda `golangci-lint`, **não** roda `pnpm lint` e **não** roda `pnpm test --run`.
Ou seja: os 93 testes do painel e os dois lints nunca rodaram em CI, e o
`make check` cobre coisas que o CI não cobre — e vice-versa. Enquanto os dois
divergirem, "passou no CI" e "passou no portão" são frases diferentes.

### 3.2 Os defeitos abertos

Nove testes vermelhos, **quatro causas**. Nenhum é do QA consertar.

| # | Defeito | Testes que derruba | De quem |
|---|---|---|---|
| **1** | `POST /quotes` exige `guests`; o contrato e o painel mandam `guests_count` (`disponibilidade/dto.go:182`) | 6: `TestOrcamentoAceitaOCampoQueOContratoEOPainelMandam`, `TestJornadaDaFase1DaConsultaAoCancelamento`, `TestReajusteDeTarifaNaoAlcancaVendaJaEmitida`, `TestTempoDeRespostaDoMapaEDoOrcamento`, `TestDecodeDoPedidoAcusaOsCamposObrigatorios`, `TestOrcarResponde200ENaoGravaNada` | `disponibilidade` |
| **2** | O mapa usa `hold, confirmed` como predicado de **exibição** (`disponibilidade/repository.go:27`, usado na linha 490) | `TestOMapaContinuaMostrandoQueHouveHospedeDepoisDoCheckOut` | `disponibilidade` |
| **3** | `audit.Middleware` não está montado em `router.go` | `TestTodaEscritaDaFase1DeixaTrilhaCompletaNoSistemaMontado` | `tech-lead` |
| **4** | `SchemaVersionEsperada = 20260826110000`, última migration `20260826120000` | `TestSchemaVersionEsperadaAcompanhaAUltimaMigration` | `tech-lead` |

**O defeito 1 é o mais caro, e nasceu nesta rodada.** Não é divergência de
documento: a tela de orçamento do painel manda `guests_count`, a API responde
`422 {"guests":"é obrigatório."}`, e **a Fase 1 não calcula orçamento nenhum**.
O rename foi aplicado no contrato, no painel e em `reservas`; ficou de fora o
único lugar que atende `/quotes`. Como campo desconhecido passou a ser recusado,
os dois lados agora se recusam mutuamente.

Os dois últimos testes da linha 1 são **de unidade e eram verdes**:
`disponibilidade/dto_test.go` e `handler_test.go` mandavam `guests` porque foi
assim que o DTO nasceu. Teste escrito a partir da implementação em vez do
contrato não acusa a divergência — ele a certifica. Os dois passaram a mandar
`guests_count` e ficaram vermelhos junto com os outros quatro.

**Os quatro consertos foram medidos, não deduzidos.** Aplicando as quatro
linhas prescritas nesta tabela — o predicado de exibição separado na consulta do
mapa, a tag `json:"guests_count"`, `r.Use(audit.Middleware)` e a constante do
schema — e rodando a suíte: **`internal/router` inteiro fica verde**. Os
arquivos de produção foram devolvidos byte a byte (conferido por `shasum`); o
experimento serviu só para provar que cada teste vermelho aponta para um
conserto real e do tamanho anunciado, e não para um requisito inventado.

**O defeito 2 mostra por que a asserção vai ao endpoint.** A suíte de `reservas`
prova a estadia preservada com uma consulta que ela mesma escreve, com o
predicado certo — e passa. O mapa continua devolvendo `livre`.

**O defeito 3 mostra a mesma armadilha na auditoria.** Cada módulo monta
`audit.Middleware` à mão para provar que `ip` e `user_agent` chegam. Nenhum
monta o `router.New` que roda em produção, onde as duas colunas saem `NULL`.

### 3.3 Os três achados do `golangci-lint`

| Onde | O que | Juízo |
|---|---|---|
| `router.go:87` | `middleware.RealIP` está **deprecado por vulnerabilidade** (GHSA-3fxj-6jh8-hvhx e outros dois): ele reescreve `r.RemoteAddr` com o `X-Forwarded-For` mais à esquerda, que o cliente controla | O mais sério dos três, e fica pior com a auditoria: é exatamente esse valor que vai para `audit_log.ip`. Trilha com IP escolhido por quem se quer esconder é pior que trilha sem IP. |
| `reservas/handler.go:150,297` | `dado := Reserva{}` sobrescrito em todos os ramos | Cosmético, sem defeito de comportamento. |

### 3.4 Dois defeitos de higiene da própria suíte, corrigidos aqui

Os dois eram invisíveis num banco efêmero e destrutivos num banco de
desenvolvimento — que é onde a suíte também roda.

- **A suíte desativava as três contas do seed e não devolvia.**
  `corrida_matriz_integration_test.go` e `corrida_ultimo_admin_integration_test.go`
  faziam `UPDATE users SET active = false` **sem `WHERE` e sem `t.Cleanup`**,
  para tornar determinística a contagem de administradores. Medido: depois de
  uma passada, `admin@wh.local`, `gestao@wh.local` e `corretor@wh.local`
  ficavam com `active = false` e ninguém mais entrava. **O seed não conserta**:
  ele é idempotente por chave natural e não toca em quem já existe, então
  rodá-lo de novo responde "nada mudou" com o ambiente quebrado. Agora passa por
  `silenciarPopulacao`, que fotografa quem estava ativo e devolve no fim —
  reativando **só** quem estava ativo, senão ressuscitaria as contas que o
  próprio teste desativou de propósito. Verificado: as três sobrevivem à suíte.
- **A limpeza engolia o erro e vazava fixture.** `audit_log.actor_id` referencia
  `users` sem `ON DELETE` (e está certo: apagar a pessoa não pode apagar a prova
  do que ela fez). Desde que a Fase 1 passou a auditar, o `DELETE` do usuário
  descartável estoura `23503` — e o `_, _ =` do `t.Cleanup` escondia isso.
  Medido: 7 usuários e 9 perfis órfãos por passada. A limpeza agora apaga a
  trilha do usuário de teste antes de apagá-lo, e **reporta** o que não
  conseguiu levar.


## 4. Política

- **Sem `sleep`.** Espera é por condição. Onde o tempo faz parte da regra (expiração de pré-reserva, refresh vencido), o teste **simula o efeito** — o `UPDATE` que o job faria, a data gravada no passado — em vez de esperar o relógio. Teste que dorme é teste que fica lento e, mais cedo ou mais tarde, intermitente.
- **Datas determinísticas.** Nada de `time.Now()` solto. Os cenários de calendário usam datas fixas e distantes (2031), para não colidirem com dado real nem entre si.
- **Dinheiro em centavos inteiros.** Comparação de valor é `int64`; float em asserção de dinheiro é o mesmo bug de produção, só que no teste.
- **Integração contra Postgres real, nunca mock de banco.** Constraint, `daterange`, escopo `own` no `WHERE` e violação de unicidade não existem fora do Postgres. Um dublê de repositório só prova que o dublê concorda com o service.
- **Comportamento observável se pergunta ao endpoint, não ao banco.** Quando a promessa é "o mapa mostra", a asserção chama o mapa. Escrever no teste a consulta que o endpoint *deveria* fazer prova que o teste sabe a regra, não que o produto a cumpre — e o teste fica verde por cima do defeito vivo. Foi o que aconteceu com o MÉDIO 7 desta rodada (§3.2). Vale igual para middleware: suíte que monta a cadeia à mão prova o pacote, não o produto.
- **Toda asserção de recusa vem com o controle positivo ao lado.** "Recusa sinal acima do total" é satisfeito por um sistema que recusa todo sinal; "recusa reativar a unidade" é satisfeito por um sistema que nunca reativa nada. O caso legítimo que **tem** de passar mora no mesmo teste, e é ele que separa a trava certa da trava burra.
- **Teste contra o contrato, não contra a implementação.** As asserções são sobre status, `code` de erro, envelope e efeito observável. Teste que só passa porque conhece o caminho interno do service não protege ninguém e quebra na primeira refatoração honesta.
- **Fixture própria e limpeza própria.** Cada teste cria o que precisa com sufixo único e apaga no `t.Cleanup`. Teste que depende do estado deixado por outro falha na ordem errada.
- **Defeito encontrado vira teste vermelho, não conserto.** O `qa-testes` não edita código de produção: escreve o teste que expõe o defeito, deixa falhando com mensagem em linguagem de negócio e reporta. Todo teste vermelho por defeito de produção carrega, no próprio arquivo, um bloco `⚠ ESTE TESTE ESTÁ VERMELHO E É DEFEITO DE PRODUÇÃO` dizendo a causa e de quem é o conserto.

## 5. Como ler uma falha

A mensagem de falha é escrita para quem **não** está com o código aberto. `"de 50 pedidos simultâneos, só 0 receberiam 409 DATE_CONFLICT"` diz o que o hóspede veria; `"expected true to be false"` não diz nada. Quando a falha for de concorrência, a mensagem traz também quantas transações precisaram ser repetidas — é o número que separa "o banco resolveu" de "o cliente levou o erro".

## 6. O que ainda não existe

- **e2e (Playwright)** — `tests/e2e/` continua sem suíte. As telas da Fase 1 existem (inventário, tarifário, calendário, política, orçamento), mas nenhuma delas fala com uma API montada: os módulos só foram ligados ao `router.New` no fim desta rodada. A suíte e2e entra quando houver um ambiente com API e painel de pé ao mesmo tempo — login por perfil, emitir pré-reserva pela tela, cancelar aplicando política.
- **Mesa de 20 cenários de tarifa e teste de propriedade** (spec §4) — o motor de `internal/domain/booking` está implementado e a jornada confere os valores da Tabela V1 ponta a ponta, mas a mesa completa de 20 combinações (feriado × fim de semana × período especial × evento × desconto) ainda não foi escrita. É o próximo alvo natural: é teste puro, sem banco, e cada linha da mesa é uma conversa de venda que já aconteceu.
- **O limite comercial de bloqueio (8 unidades × 364 dias) não tem teste porque não tem dono.** A revisão mediu o corretor fechando a Cobertura por dois meses de alta temporada com um `POST /blocks`. O `db-migrations` decidiu, com razão, que tamanho de bloqueio é regra de negócio e pertence a `internal/domain`, não a um `CHECK`; `reservas` não o implementou por ser pasta alheia. Ninguém o pegou. **Não escrevi teste vermelho para ele**: o QA guarda requisito acordado, e este ainda é proposta — a spec §11 não fixa limite nenhum. Precisa de decisão comercial antes de virar teste.
- **`pii_access_log`** (trilha de LEITURA de dado pessoal, LGPD, spec §16) não existe nem em código nem em teste. O `audit_log` cobre escrita; quem consultou o telefone de um hóspede não deixa rastro.
- **Carga sustentada.** O desempenho está medido por requisição isolada (§2). Ninguém mediu ainda o comportamento com dezenas de operadores simultâneos no mapa durante a véspera de Réveillon.
