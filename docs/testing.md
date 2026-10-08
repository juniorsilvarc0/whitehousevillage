# Testes

> O que roda, o que cada suíte protege e as regras que valem para todo teste do repositório. O dono deste documento e de todo arquivo `*_test.go`, `*.test.tsx` e `tests/e2e/**` é o agente `qa-testes`.

## 1. As quatro camadas

| Camada | Comando | Precisa de quê? | Tempo |
|---|---|---|---|
| Unidade (Go) | `make test-api` | nada | ~50 s |
| Componente (painel) | `make test-admin` | nada | ~6 s |
| Integração (Go + Postgres) | `make test-integration` | Postgres efêmero | ~4 min (serializada com `-p 1`) |
| **Fumaça (navegador)** | `make smoke` / `make smoke-stack` | **stack no ar** | ~30 s |

`make check` roda lint + typecheck + as duas primeiras. A integração e a fumaça
são alvos próprios porque uma sobe um container e a outra exige a aplicação
servida.

A quarta camada não é redundante com as três: ela é a única que roda contra a
**aplicação servida**, e existe porque a suíte inteira já ficou verde com o
stack morto. `make smoke` mede o que já está no ar; `make smoke-stack` sobe a
imagem nova, migra, semeia e só então mede — que é a ordem de um deploy, e a
única que impede a fumaça de dar por boa uma tela que a árvore contém e a
imagem não.

> **`make check` se completa desde `e0bc08e` (02/10/2026).** Até ali ele não
> chegava a rodar um teste: `golangci-lint` não estava instalado em lugar
> nenhum, e quando instalado reprovava com 11 apontamentos (§3.3), e `lint` é a
> primeira dependência de `check`. Os 11 foram corrigidos, e na Rodada 5 o
> `Makefile` absorveu o que aquela correção ensinou:
>
> - **A versão vive num lugar só**: `GOLANGCI_LINT_VERSION` no `Makefile`
>   (v2.5.0). O job `lint-go` do CI lê dali (`make -s golangci-versao`); fora do
>   CI, versão diferente só avisa, porque a contagem pode divergir.
> - **Sem teto de repetição**: `make lint-golangci` roda com
>   `--max-same-issues=0 --max-issues-per-linter=0`. O padrão da ferramenta
>   esconde apontamentos iguais a partir do quarto — e quem relata número
>   precisa do número inteiro.
> - **Fora do `PATH` não é "não instalado"**: o alvo procura no `PATH`, depois
>   em `go env GOBIN` e em cada `GOPATH/bin`. Quem instalou com `go install`
>   e não pôs `~/go/bin` no `PATH` tinha o binário e o portão dizia que não.
>   Sem binário nenhum, o alvo imprime o comando de instalação da versão do CI.
>
> O lint **ainda não enxerga** os arquivos `//go:build integration`: com
> `--build-tags=integration` são **6** apontamentos (eram 15), todos
> `defer resp.Body.Close()` em `internal/router/*_integration_test.go`. Até
> zerar, `GOLANGCI_LINT_TAGS` fica vazia. Para medir:
> `make lint-golangci GOLANGCI_LINT_TAGS=integration`.
>
> **E uma lição que nenhum `Makefile` absorve**: `make check; echo $?` seguido
> de outro comando na mesma linha, ou `make check | tail`, devolve o código de
> saída do **último** comando, não do `make`. Foi assim que o lint vermelho passou
> por verde numa sessão de 02/10. Leia a saída — a última linha do `make`, o
> `ok`/`FAIL` de cada pacote, o `Test Files … passed` do vitest —, e não confie
> em código de saída de pipeline sem `set -o pipefail`.

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

O seed é **idempotente por contrato** e a suíte depende disso. Medido em 03/10/2026 (schema `20261003120000`, catálogo `teste`): num banco novo, `previstas 310, criadas 309, atualizadas 1` — o `1` é o vínculo `users.broker_id` do corretor de desenvolvimento, que é `UPDATE` por natureza —; rodá-lo de novo deixa `criadas=0, atualizadas=0, inalteradas=310` e *"nada mudou"*, e a contagem de linhas idêntica em `units`, `unit_types`, `resources`, `roles`, `role_permissions`, `users` e `rates`. Um seed que duplicasse na segunda execução quebraria toda fixture que resolve por chave natural.

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

**Remedido em 27/08/2026**, agora com as três medições saindo do próprio teste
(em 26/08 o orçamento tinha de ser medido por fora com `curl`, porque o teste
morria antes de chegar nele — era o defeito do nome do campo `guests`, desde
então corrigido):

| Medição | Teto | Medido (`-race`) | Folga |
|---|---|---|---|
| Mapa, 90 dias × 8 unidades | 300 ms | **13,3 ms** | 22× |
| Disponibilidade, 90 dias × 4 produtos | 300 ms | **10,8 ms** | 28× |
| Orçamento de 3 noites | 150 ms | **2,8 ms** | 53× |

E o barramento de tempo real, medido por
`TestAlteracaoNoBancoChegaNoStreamEmMenosDeDoisSegundos` — o caminho inteiro,
`INSERT` → gatilho → `pg_notify` → `LISTEN` do hub → fan-out → SSE no cliente:

| Medição | Teto do aceite | Medido | Folga |
|---|---|---|---|
| Escrita no banco → evento na conexão SSE | 2 s | **7 ms** | 285× |

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

### `internal/router/jornada_rodada4_integration_test.go` — a jornada que a dívida bloqueava

A jornada da Fase 1 (acima) prova a venda. O que ela **não** provava é que a
venda é possível **por uma API só** — porque ela cria o hóspede com um `INSERT`
de fixture, e era exatamente esse o passo que não existia. Este arquivo tem uma
regra própria, e ela vale como asserção: **as sete escritas saem todas de
`a.chamar`/`a.chamarComChave`**; o `pool` aparece só em `exigirSeed` e no
`t.Cleanup`. Se um dia a jornada precisar de SQL para andar, a dívida voltou.

O estado de partida, medido pelos três agentes desta rodada e reproduzido aqui:
`POST /contacts` → **404**, `POST /quotes` → 200 com 16 chaves e **nenhuma
`id`**, `GET /quotes/{id}` → **404**, `/win` → **422 `QUOTE_REQUIRED_TO_WIN`**
com a dica *"emita o orçamento e vincule-o à oportunidade"* — uma instrução
impossível de seguir.

| Passo | O que a asserção protege |
|---|---|
| `POST /contacts` | O contato nasce com `id` e é **encontrável por telefone E.164** — não basta gravar, tem de gravar onde alguém acha, que é o caminho do inbound de WhatsApp. |
| `POST /crm/opportunities` | O card nasce `aberta` (feminino: o banco guarda o masculino, a porta publica o feminino). |
| `POST /quotes {persist:true}` | **201 com `id`**, vinculado ao contato e ao card, não vencido, `subtotal + limpeza = total`. Sem `persist` o comportamento de hoje fica intacto — a tela dispara a rota a cada tecla. |
| `GET /quotes/{id}` | Reabrir a proposta devolve o mesmo total, o mesmo sinal e as três diárias detalhadas. |
| `POST /win` | Reserva em `hold` com prazo, código `WH-…`, card em `ganha` apontando para a reserva — e a reserva **transcreve** o orçamento centavo a centavo. O orçamento passa a apontar para a reserva, que é o que o torna consumido. |
| `POST /confirm` | Vira `confirmed`, o prazo de pré-reserva **some**, o total não é recalculado. |
| `POST /cancel?dry_run=1` → `POST /cancel` | A simulação se declara simulação, calcula sobre o sinal **efetivamente pago**, fecha a conta (devolve + retém = pago) e **a reserva continua de pé depois dela**; a execução dá o mesmo número. |
| `GET /availability` | O estoque volta ao **número exato** de antes da venda. |

`TestADicaDoGanhoSemOrcamentoEhExecutavel` mora ao lado e cobra outra coisa: o
`/win` sem orçamento responde 422 com uma dica, e o teste **segue a dica ao pé
da letra** e exige que ela funcione. Um teste que só conferisse o `code` passa
com a dica mentindo — que é literalmente o que acontecia antes desta rodada.

**Poder de detecção verificado por mutação, e a primeira mutação encontrou um
defeito no próprio teste.** A versão inicial do passo 7 dizia "sobrou pelo menos
uma unidade"; injetando o estado do cancelamento que esquece de liberar o
calendário (`UPDATE stay_blocks SET status='confirmed'`), ela **passou verde** —
o `apto-2s` tem três unidades, e uma presa ainda deixa duas livres. A asserção
foi trocada por "o estoque volta ao número medido antes da venda", e a mesma
mutação passou a acusar:

```
2031-03-10: antes da venda havia 3 unidade(s) disponível(is) e depois do
    cancelamento há 2 — a data não voltou inteira ao estoque
```

A segunda mutação, o `/win` recalculando o preço (`UPDATE reservation_pricing`),
também dispara:

```
a reserva não copiou o orçamento: proposta total 273000 / subtotal 255000 /
    sinal 136500, reserva total 373000 / subtotal 355000 / sinal 136500 —
    o preço foi recalculado no /win
```

### `internal/router/guarda_de_concorrencia_test.go` — o teste que vigia a repetição

`make it-concorrencia` repete os testes de disputa 10 vezes e escolhe quais
**por nome**, porque Go não tem categoria de teste. O Makefile já avisava por
escrito que quem escrevesse uma disputa nova tinha de batizá-la com uma das
palavras — e a guarda que existia lá só reprova quando o regex não casa com
**nada**, ou seja, é cega para o caso real.

O caso real aconteceu. Medido em 27/08/2026: a etapa respondeu
`internal/router ... [no tests to run]` enquanto o pacote guardava dois dos
testes de disputa mais caros da casa — a composição crescendo no meio de uma
venda `all_members` e a troca de `consumes` contra uma venda em voo. Os dois
rodavam **uma vez** na suíte normal, que é exatamente a passada em que uma
corrida intermitente se esconde.

Este teste roda **sem banco e sem a tag `integration`**, de propósito: a omissão
é de nome, e tem de aparecer no `make check` de quem escreveu o teste. Ele lê o
`TESTES_CONCORRENCIA` do próprio Makefile (copiar o valor aqui reintroduziria a
divergência que ele existe para pegar) e exige que **todo `func Test` num
arquivo batizado de disputa** case com o regex. O critério é o do arquivo, e é
conservador de propósito: adivinhar pelo corpo (`go func`, `sync.WaitGroup`)
apanharia helper de fixture e a guarda seria desligada por barulho. Um piso de
quatro arquivos impede que renomear os arquivos para fora do vocabulário
devolva o buraco.

Controle negativo: desfazendo um dos dois rebatismos, ele acusa nomeando
arquivo, teste e o que fazer —

```
teste(s) de disputa que a etapa `make it-concorrencia` NUNCA repete — eles rodam
UMA vez e uma corrida intermitente atravessa:
  apps/api/internal/router/troca_de_consumes_concorrente_integration_test.go:
      TestTrocaDeConsumesEsperaVendaEmVooEDepoisRecusa
o nome do teste precisa casar com TESTES_CONCORRENCIA (…) — rebatize o teste,
não afrouxe o regex
```

Os dois testes rebatizados passaram a rodar na repetição e sobreviveram a
`-count=10 -race`.

### `apps/admin/e2e/fumaca.mjs` — a aplicação servida, e todo link dela

A fumaça é a única coisa que roda contra a **aplicação servida**, e é por isso
que ela existe: a suíte inteira já ficou verde com o stack morto. Ela reprova em
5xx, em **404 do documento**, em prefetch RSC 404, em aviso de erro visível e
quando uma tela **volta para `/login`**.

Nesta rodada ela ganhou a conferência que faltava: **nenhum link do painel pode
levar a 404**. A conferência de prefetch que já existia só pega o link que o
Next resolveu buscar — e ele só prefetcha o que entra no viewport, então destino
morto em menu recolhido, aba não aberta ou linha de tabela abaixo da dobra
atravessava. Agora a fumaça pergunta ao DOM quais links **existem** em cada tela
visitada e bate em todos, reaproveitando os cookies da sessão.

Foi assim que `/app/reservas/{id}` passou uma rodada inteira como link morto no
meio do fluxo de venda: o card do CRM já apontava para ele e a tela não existia.

Medido contra o stack no ar: 10 telas navegadas + **15 links internos**
conferidos, incluindo `/app/reservas/{id}`, `/app/contatos/{id}`,
`/app/oportunidades/{id}` e duas subtelas de configuração
(`/app/configuracoes/calendario`, `/app/configuracoes/politica`) que **não
estão na lista `TELAS`** — a lista deixou de ser a única fonte de cobertura.
Controle negativo: injetando um destino para `/app/chat`, a suíte reprova com
`link morto: … oferece /app/chat, que responde 404`.

Para não crescer com o volume do banco, a lista é ordenada **por forma de rota**
(`/app/contatos/{id}` é uma rota, não vinte) e tem teto de 60: o corte nunca
tira uma rota inteira, só repetição.

**O login também foi endurecido, e por uma falha observada.** Numa execução em
que o contêiner do painel tinha acabado de reiniciar, os dois `fill` rodaram, o
clique saiu, **nenhuma requisição para `/api/auth/login` foi observada** e a tela
mostrava *"Informe o e-mail. Informe a senha."*: `domcontentloaded` chega antes
da hidratação do React, e o input controlado hidratado depois volta ao valor
inicial — vazio. A fumaça agora **confere que os campos ficaram preenchidos**
(até 3 tentativas) antes de clicar, e diz isso por extenso quando não ficam.
Sem essa conferência o sintoma é sempre o mesmo — *"o login não saiu de
/login"* — para meia dúzia de causas diferentes.

### `internal/router/contract_test.go` — teste de contrato (roda sem banco)

Varre a tabela declarativa de `routes.go` e a compara com `openapi/openapi.yaml`:

- toda rota servida existe no contrato, com o mesmo verbo;
- todo recurso CRUD expõe os seis verbos, nas **duas** fontes (`docs/api.md` §2);
- toda rota protegida declara recurso + ação, e a que não checa permissão declara o motivo por escrito;
- o que é público no código é `security: []` no contrato, e vice-versa;
- `SchemaVersionEsperada` acompanha a última migration entregue.

### `apps/admin/src/config/navigation.test.ts`

O menu do corretor: sem Configurações, sem Relatórios, sem Inventário, sem Canais e sem Financeiro — recebíveis, pagáveis e conciliação são o "financeiro global" que a spec §11 fecha para ele. **Comissões aparece**, e é assim mesmo: a matriz dá `finance.commissions` em escopo `own`, que é o painel de comissões previstas e pagas prometido no §11. Também garante que a filtragem só **tira** itens — perfil nenhum ganha acesso por omissão — e que as abas do celular apontam para telas que os três perfis alcançam.

> A nota de defeito que vivia aqui saiu: `allowedRoles` deixou de existir em `navigation.ts` e cada item passou a declarar **um recurso do catálogo**.

**Na rodada de 27/08 este teste ganhou a metade que faltava, e ela pegou um caso
de verdade no meio da rodada.** Ele agora varre `src/app/(app)/**/page.tsx` e
compara com o menu **nos dois sentidos**: item de menu sem tela reprova, e tela
que existe marcada como `emConstrucao` reprova nomeando a linha e a palavra a
apagar. `/app/reservas` foi marcada como em construção e, minutos depois, a tela
chegou — a suíte ficou vermelha sozinha dizendo `"Reservas → /app/reservas: a
tela existe; apague `emConstrucao: true` da linha dele"`.

O que isso protege é concreto e foi medido: o menu inteiro fica na barra do
desktop, então cada visita ao `/app` disparava a rajada de prefetch. Com 13
destinos para 5 telas eram **9 respostas 404 por visita**; com o menu honesto,
**0**. A fumaça deixou de tolerar esse ruído e passou a reprovar nele.

### `apps/admin/src/lib/auth/permissions.test.ts`

Guarda o vocabulário: todo código de recurso citado pelo painel — no mapa de destinos e nos atalhos do Painel — existe no catálogo do banco. O teste **lê `apps/api/cmd/seed/acesso.go`** e compara conjunto a conjunto, e varre o `src` inteiro atrás de literais `recurso: "..."`. Foi assim que se descobriu que os três códigos fantasmas (`availability`, `finance`, `commissions`) estavam em **dois** lugares, não um — escondendo Mapa, Financeiro e Comissões de todo mundo, inclusive do administrador. Código fora do catálogo não protege ninguém: esconde a tela de todos, porque ninguém pode receber permissão num recurso que não existe.

### `apps/admin/src/lib/auth/menu-e-dado.test.ts` — verde desde o conserto do menu

A regra 8 dita como propriedade: **o menu é função da matriz, não do nome do perfil**. Os três casos passam:

- *dois perfis com a mesma matriz enxergam o mesmo menu* — duas pessoas com matriz idêntica, diferentes só no `code`, recebem o mesmo menu. Era a prova mais curta do defeito antigo;
- *não esconde tela que a matriz concedeu* — reancorado em `/app/reservas` na rodada de 27/08. Ele cobrava `/app/agenda`, que saiu do menu quando o menu passou a anunciar só o que existe; a âncora nova é a mesma prova com uma tela que existe. Ao lado dele entrou o contrapeso — *"a agenda continua ausente por falta de TELA, não por causa do papel"* —, que é o que impede a próxima ausência de ser confundida com permissão faltando;
- *continua escondendo o que a matriz não concede* — o controle, que separa "o menu obedece à matriz" de "o menu mostra tudo para todo mundo". Passava antes do conserto e continua passando depois, que é exatamente o que se pedia dele.

O teste continua no lugar como **regressão**: ele é a única coisa que impede `allowedRoles` de voltar disfarçado na próxima tela nova.

### `apps/admin/src/app/login/login-form.test.tsx`

A tela de login como componente de decisão: a mensagem de erro é **a mesma** para e-mail inexistente, senha errada e bloqueio (o contrato usa um único `INVALID_CREDENTIALS`; distinguir na tela devolveria a enumeração de usuários que a API fecha de propósito), a validação segura o envio antes de gastar uma das cinco tentativas, e a tela reage ao `code`, nunca ao texto que a API mandou.

### `apps/admin/src/app/login/login-form-antes-da-hidratacao.test.tsx` — verde desde o conserto de 07/10/2026

O HTML do login que o servidor entrega, antes da hidratação. O `<form>` não tinha `method`, então um toque em "Entrar" antes de o JavaScript carregar virava envio nativo por **GET**: e-mail e senha iam para a URL, o histórico do navegador e o log de acesso. Medido no E2E de bens, no log do `next dev`: `GET /login?email=gestao%40wh.local&password=whv%402026`. Nasceu vermelho e ficou verde com `method="post"` no formulário; continua como regressão, e aceita qualquer das três saídas (POST, senha sem `name` ou botão desabilitado até hidratar).

### `internal/router/bens_qa_*_integration_test.go` — inventário de bens pela porta da frente (Fase 5)

Router completo de `router.New`, sessões por `/auth/login` com os perfis **do seed**, e as 40 operações da tag `Bens` lidas do **contrato** (com o `x-rbac` de cada uma), nunca de `rotas_inventario_bens.go`. Complementa os 59 testes do módulo, que montam um chi próprio e assinam o token direto no emissor.

- `rbac` — corretor do seed com 403 nas 40, e o 403 aponta o mesmo `recurso:ação` do `x-rbac`; sem sessão, 401 nas 40 (inclusive a foto com id real, token na query e Basic); `usuario` e `admin` percorrem as 40 com o status de sucesso do contrato; perfil só `inventory.goods:ver` lê as 15 leituras e leva 403 nas 25 escritas e em `/units`; perfil só `inventory` leva 403 nas 40. Todo 403 confere que o banco não mudou.
- `isolamento` — segunda propriedade montada pela API por um usuário dela: colocação, avaria (cômodo, bem, conferência e reserva cruzados), foto, galeria, cópia nos dois sentidos, leituras e escritas nos recursos da outra casa.
- `isolamento` (vazamento) — nenhuma resposta (nem a planilha) traz nome, e-mail, telefone ou documento do hóspede, nem e-mail ou telefone do operador; nenhuma chave fora da árvore do schema do contrato.
- `conferencia` — fechada e cancelada recusam as cinco escritas com `COUNT_CLOSED`; o `result` do GET é byte a byte o do `/close` depois de recotar, mudar e apagar colocação e desativar cômodo e bem. Nasceu **vermelho**: apagar a avaria nascida no fechamento reescrevia `issues_created` e `issue_id` da conferência fechada. Verde desde a rodada 3 do módulo: avaria de conferência fechada leva `409 RESOURCE_IN_USE` com `details.count_id` (`TestBensQAAvariaDeConferenciaFechadaNaoSeApaga`).
- `formato` — envelope das seis listas, envelope e `details` dos erros, `Location` nos seis 201, `ContagemDaLinha` inválida, campo desconhecido com alvo real e corpo válido, CSV para o Excel (`;`, BOM, vírgula decimal, fórmula neutralizada, `Content-Disposition`). Nasceu **vermelho**: chave com outra caixa (`COUNTED_QTY`) era aceita como o campo do contrato, porque o `encoding/json` casa nome sem diferenciar maiúscula. Verde desde que `httpx.Decode` passou a exigir a chave byte a byte igual à tag do DTO — o conserto vale para a API inteira.
- `upload` — acima de 15 MB, GIF, vídeo, HEIC, SVG/HTML/PDF disfarçados e arquivo vazio dão 422 sem deixar linha; 15.000.000 bytes entram; JPEG com EXIF Orientation=6 gera miniatura em pé (conferido pixel a pixel); PNG gera miniatura JPEG; WebP sai com `thumb_url == url`.

### `tests/e2e/bens-contagem-celular.mjs` — a conferência pelo celular

Jornada do perfil `usuario` em viewport de celular, contra a aplicação **servida**: abrir a conferência na tela da unidade, contar cômodo a cômodo, tentar fechar com pendência, seguir o "Ir contar" da recusa, fechar, conferir a apuração e **recarregar**. Prepara os dados pela API com `admin@wh.local` e desativa a unidade no fim. Espera por condição (rodapé, URL pelo documento, hidratação), nunca por relógio.

```bash
E2E_PAINEL=http://localhost:3100 E2E_API=http://localhost:8080 node tests/e2e/bens-contagem-celular.mjs
```

Sem Docker (disco da VM cheio em 07/10/2026) ele rodou com a API compilada no host e o painel em `next dev --webpack` a partir de uma **cópia** no diretório temporário — o Turbopack recusa `node_modules` por symlink fora da raiz, e a cópia evita sobrescrever o `.next` de quem desenvolve.

## 3. Estado atual

### 3.0 Medido em 02/10/2026, ao fim da Rodada 5

Pelo `squad-lead`, sobre a árvore que o integrador commita, contra o Postgres de
teste da porta 55432 **recriado do zero** (migrations até `20261002180000` e
seed). Sem Docker neste ambiente: o que depende de imagem não rodou (fim da
seção).

| Passo | Resultado |
|---|---|
| `make check` (inteiro, de ponta a ponta) | ✅ `exit=0`, lido na saída: `golangci-lint (2.5.0) … 0 issues`, 24 pacotes Go `ok`, painel `40 passed (40)` / `340 passed (340)` |
| `go build`, `go vet`, `go vet -tags=integration`, `gofmt -l` | ✅ limpos |
| `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...` | ✅ **0** |
| O mesmo com `--build-tags=integration` | ❌ **6** (eram 15 na verificação de 02/10): `errcheck` de `defer resp.Body.Close()` em `internal/router/api_integration_test.go:120,159`, `jornada_fase1_integration_test.go:123`, `regressao_criticos_integration_test.go:60,103`, `regressao_rodada3_integration_test.go:68`. Fora do portão até zerar |
| `go test ./... -race -count=1` | ✅ 24 pacotes, 0 FAIL |
| Integração: banco recriado + `make it-suite` (`-p 1 -race`) | ✅ 24 pacotes, `exit=0` |
| A mesma suíte com `-v` (sem `-race`), para contar | ✅ **701 testes de topo, 978 com subtestes, 0 FAIL, 0 SKIP** |
| Seed 1ª / 2ª | ✅ `previstas 305, criadas 304, atualizadas 1` / `criadas 0, atualizadas 0, inalteradas 305` — *"nada mudou"* |
| `pnpm lint`, `pnpm exec tsc --noEmit` | ✅ zero |
| `pnpm test --run` | ✅ **40 arquivos, 340 testes** (eram 334 em 27/08) |
| Fumaça do site, `node apps/site/e2e/fumaca-site.mjs` contra nginx 1.24 local com o `nginx.conf` do repositório | ✅ APROVADO, 17 recursos internos; `/admin`, `/admin/`, `/scripts/admin.js` e caminho inventado = 404 |

**Sobre o "0 SKIP"**: sem `-v`, o `go test` não imprime `--- SKIP`, e contar
`SKIP` na saída de `make it-suite` dá zero sempre — inclusive com metade da suíte
pulada. A contagem que vale é a da execução com `-v`.

**O que não rodou, e por quê**: `make up`, `make smoke`/`smoke-stack` com o painel,
`make smoke-site-imagem`, o worker em contêiner e o `nginx:1.27-alpine` da imagem
do site — o ambiente da rodada não tinha Docker. A prova deles vem no primeiro CI
depois do commit (jobs `smoke`, `site` e `lint-go`; este último nunca rodou num
runner). `make it-concorrencia` foi rodado pelo `backend-go` depois da suíte
verde (`14 testes de concorrência x 10 repetições`, `exit=0`); não foi refeito
pelo `squad-lead`.

**Os testes novos desta rodada**, todos `PASS` na execução com `-v`, cada um com
controle negativo medido por quem o escreveu: `TestPoliticaNaoPerdeColunaAoRepublicar`,
`TestValidadeDoOrcamentoVemDaPoliticaECongelaNaEmissao`,
`TestColecaoDeContatosNaoServeDocumentoCheioComoCorretor`,
`TestPatchDeContatoRegistraLeituraERecusaEmailMascarado`,
`TestFullDaReservaRegistraCadaHospedeExibido`, `TestTelefoneDoLeadSaiSempreMascarado`,
`TestFullDaOportunidadeRegistraOContato`, `TestCorretorOwnNaoAtribuiAVendaAoColega`,
`TestCorretorOwnOmitindoGravaOProprio`, `TestCorretorOwnNaEdicao`,
`TestGuardaDoCorretorNoSQLRecusaContaQueMudou`, `TestAdminAtribuiCorretorExistenteEOInexistenteEh422`,
`TestOutraContaNaoApontaParaOCadastroDoCorretor`, `TestVendaComCorretorInexistenteEhRecusadaPeloBanco`,
`TestExcluirFichaDeCorretorDa409ComAContagem`; e, sem banco, os cinco de
`internal/platform/apperr/catalogo_test.go` e a mesa de `internal/domain/commission`
(22 casos e 864 combinações exaustivas).

**Duas intermitências**, uma fechada e uma aberta, as duas de `TestOverbookingEhImpedidoPeloBanco`:
os cenários C e D colidiam em data com as rodadas 7 e 9 do cenário B e falhavam
quando a Completa vencia a disputa (reproduzido injetando a vitória; datas
movidas, `-count=5` verde). O cenário **A** tem outra, de tempo: numa de seis
execuções isoladas, as 49 respostas vieram `DATE_CONFLICT` e nenhuma recusa foi
atribuída ao `23P01` — não mexida; a suíte completa e as 10 repetições passaram.

**Higiene que ficou para o dono de `internal/router` — e já está acontecendo.**
`GET /reservations/{id}/full` e `GET /crm/opportunities/{id}/full` agora gravam
`pii_access_log` com o ator, e a FK `pii_access_log_actor_id_fkey` não tem
cascata. O cleanup do usuário descartável (`internal/router/api_integration_test.go:281`)
não apaga a trilha antes do usuário, e **dois testes já deixam lixo**: a suíte com
`-v` registrou `LIMPEZA INCOMPLETA` em `TestReajusteDeTarifaNaoAlcancaVendaJaEmitida`
e `TestRemarcarParaMaisBaratoNaoFazDinheiroDoHospedeEvaporar` — o usuário fica
(23503 em `pii_access_log_actor_id_fkey`) e, com ele, o perfil de teste (23503 em
`users_role_id_fkey`). Há uma terceira ocorrência, **anterior à rodada**:
`TestPapelAcimaDoTetoDoAtorEhRecusado` deixa um perfil no banco também em
`957e6e3` (medido rodando o teste de uma cópia do `HEAD`). Nada disso reprova,
porque a limpeza só faz `t.Logf` — **e `t.Logf` de teste que passa só aparece
com `-v`**: a saída de `make it-suite` mostra zero `LIMPEZA INCOMPLETA` com cinco
acontecendo. Os relatórios da rodada que contaram "0 `LIMPEZA INCOMPLETA`" sem
`-v` contaram nada. Conserto: apagar `pii_access_log WHERE actor_id = $1` antes
do usuário (dono de `internal/router` de teste); e a limpeza incompleta passar a
reprovar, ou o alvo da suíte rodar com `-v` e reprovar ao achar a marca.

### 3.1–3.5 Medido em 27/08/2026 — o portão verde, menos o `lint` que ninguém conseguia passar

Medido em 27/08/2026, ao fim da rodada de quitação de dívida técnica, contra
Postgres 16 efêmero próprio (`whv-qa-r5`, porta 55490, `PGDATA` em `tmpfs`),
migrations até `20260827140000` e seed completo. O stack de desenvolvimento
ficou de pé o tempo todo e não foi tocado.

### 3.1 O portão, passo a passo

| Passo de `make check` | Resultado |
|---|---|
| `gofmt -l .` | ✅ vazio |
| `go vet ./...` e `go vet -tags=integration ./...` | ✅ limpos |
| `golangci-lint run ./...` | ❌ **11 problemas** — e `make check` **para aqui** |
| `pnpm lint` (`eslint src --max-warnings 0`) | ✅ zero erros, zero avisos |
| `pnpm exec tsc --noEmit` | ✅ zero |
| `go test ./... -race -count=1` | ✅ **20 pacotes**, verde em 3 execuções seguidas |
| `pnpm test --run` | ✅ **40 arquivos, 334 testes** |

Como `lint` é a primeira dependência de `check`, **os testes nunca são
alcançados por `make check`** — foram rodados um a um. Os 11 problemas estão
todos em código de produção e nenhum em arquivo de teste (§3.3).

Fora do `make check`:

| | Resultado |
|---|---|
| `pnpm build` | ✅ **21 rotas**, incluindo `/app/reservas/[id]` e `/app/contatos/[id]` |
| `migrate up` (do zero) | ✅ `20260827140000`, `dirty=false` |
| `seed` 1ª / 2ª | ✅ `previstas 298, criadas 298` / `criadas 0, atualizadas 0, inalteradas 298` — *"nada mudou"* |
| Integração `-p 1 -race` | ✅ **20 pacotes**, **874 testes**, **0 falhas e 0 `SKIP`** |
| Concorrência `-count=10` | ✅ 10 testes × 10 repetições, verde |
| `make smoke` (stack no ar) | ✅ 10 telas + **15 links internos**, sem 5xx, sem 404, sem aviso |

**Zero `SKIP` é um número que vale ler.** A suíte de integração pula por
`t.Skip` quando falta `DATABASE_URL` ou seed, e um `skip` ali é cobertura
perdida disfarçada de verde. Rodada com `-v`, nenhum dos 874 casos foi pulado.

### 3.2 O que esta rodada mudou na suíte

**A jornada que a dívida bloqueava roda inteira pela API.** As sete escritas —
contato, oportunidade, orçamento persistido, `/win`, confirmação, `dry_run` e
cancelamento — passam por HTTP, sem um `INSERT` sequer. Antes da rodada, três
das sete respondiam 404 ou não gravavam. **A dívida está quitada nesse eixo**, e
o teste é a prova executável disso (§2).

**Um teste de disputa que nunca era repetido virou dois testes repetidos, e a
omissão virou guarda automática.** Os dois testes de corrida de
`internal/router` estavam fora do regex de `TESTES_CONCORRENCIA` e rodavam uma
vez só. Foram rebatizados e agora entram no `-count=10`; um teste novo (§2)
impede que a próxima disputa nasça de fora da lista.

**Uma intermitência de três rodadas foi diagnosticada e fechada — e era do
arreio, não do produto.** `TestLimiteDeConexoesPorUsuarioResponde429` falhava em
`handler_test.go:667` com `lendo o erro: EOF`, sempre depois de o `429` já ter
passado. Causa: o helper `abrir` liga incondicionalmente o leitor SSE, que sobe
uma goroutine para **drenar `resp.Body`** — e numa resposta de recusa esse corpo
é o envelope de erro em JSON. As duas leituras disputavam os mesmos bytes.
Reproduzido **3 em 3** injetando 50 ms de espera antes de ler o corpo. O helper
passou a devolver leitor **só quando a resposta é um stream** (status 200), e o
teste ganhou a asserção estrutural correspondente. Verde em `-count=40`
isolado, `-count=3` do pacote inteiro e três suítes completas.

**A fumaça deixou de depender da lista `TELAS`.** Ela agora bate em todo link
`/app` que qualquer tela oferecer. Medido: 15 links além das 10 telas, entre
eles duas subtelas de configuração que ninguém tinha posto na lista.

### 3.3 Os 11 achados do `golangci-lint` — nenhum é de teste, nenhum é bug

> **Corrigidos em `e0bc08e` (02/10/2026).** Conferido no código pela
> verificação de 02/10 e medido de novo na Rodada 5: `golangci-lint run
> --max-same-issues=0 --max-issues-per-linter=0 ./...` = **0 issues**. A tabela
> fica como registro do que travava o portão.

| Onde | O que | Juízo |
|---|---|---|
| `cmd/api/main.go:41,165` | `os.Stderr.WriteString` e `resp.Body.Close` sem checar retorno (errcheck) | Cosmético. |
| `cmd/migrate/main.go:167,173,179` | `banco.Close()` sem checar retorno (errcheck) — os três nasceram no `NoticeHandler` desta rodada | Cosmético, mas é código novo: fecha barato. |
| `crm/handler.go:464,683,902` · `crm/service_oportunidades.go:1018` | atribuição inicial sobrescrita em todos os ramos (ineffassign) | Cosmético, sem defeito de comportamento. |
| `crm/dto.go:1099` · `crm/service.go:246` | De Morgan aplicável (staticcheck) | Estilo. |

**Nenhum deles justifica o portão estar quebrado, e é isso que os torna caros:**
são 11 correções triviais entre a equipe e um `make check` que se completa. Está
registrado como **D9** no roadmap.

### 3.4 Os dois defeitos abertos que a suíte não pode fechar sozinha

| # | Defeito | Efeito | De quem |
|---|---|---|---|
| **1** | `crm_opportunities.quote_id` referencia `reservations(id)`, e a persistência de orçamento é `quotes` | A coluna virou dado morto: o `/win` funciona porque deriva o orçamento vigente de `quotes.opportunity_id`, mas o contrato afirma um fato que o schema não tem. **Nenhum teste pode acusar isso** — o comportamento está certo; o que está errado é a declaração. | `db-migrations` + `tech-lead` |
| **2** | `contacts(property_id, doc_type, doc_number)` não tem índice único | A deduplicação por documento é garantida por `pg_advisory_xact_lock`, que protege quem passa pela API e **não** protege `psql`, importação ou outro serviço. `TestCorridaDeDocumentoNaoCriaDuasPessoas` passa — ele mede o caminho da API, e o caminho da API está certo. | `db-migrations` |

Os dois são dívida **declarada**, com dono e com o passo escrito. Não escrevi
teste vermelho para nenhum: o QA guarda comportamento observável, e nos dois
casos o comportamento observável pela API está correto. Um teste vermelho ali
seria teatro.

### 3.5 Higiene da própria suíte

Continuam valendo as duas correções da rodada anterior (a suíte devolvendo as
contas do seed que desativa, e a limpeza que reporta o que não conseguiu levar).
Desta rodada, três medições sobre o custo de rodar a suíte:

- **Cache de build corrompido em execuções concorrentes.** Rodando
  `go test -tags=integration` em segundo plano ao mesmo tempo que
  `golangci-lint`, o pacote `internal/router` reprovou com
  `could not import errors (open : no such file or directory)` — falha do
  cache do toolchain, não do produto. Reexecutado sozinho: verde. **Não rode
  duas ferramentas Go pesadas na mesma árvore ao mesmo tempo**; a mensagem não
  se parece nem um pouco com a causa.
- **Container próprio, porta própria, `tmpfs`.** As portas 55432, 55440, 55441,
  55447, 55450, 55461, 55481 e 55484 já foram usadas por agentes desta e das
  rodadas anteriores; a desta foi a 55490. Derrubado no fim.
- **A suíte não encosta no banco de desenvolvimento.** Conferido depois de tudo:
  `/api/v1/readyz` continua `schema_version 20260827140000`.

## 4. Política

- **Sem `sleep`.** Espera é por condição. Onde o tempo faz parte da regra (expiração de pré-reserva, refresh vencido), o teste **simula o efeito** — o `UPDATE` que o job faria, a data gravada no passado — em vez de esperar o relógio. Teste que dorme é teste que fica lento e, mais cedo ou mais tarde, intermitente.
- **Datas determinísticas.** Nada de `time.Now()` solto. Os cenários de calendário usam datas fixas e distantes (2031), para não colidirem com dado real nem entre si.
- **Dinheiro em centavos inteiros.** Comparação de valor é `int64`; float em asserção de dinheiro é o mesmo bug de produção, só que no teste.
- **Integração contra Postgres real, nunca mock de banco.** Constraint, `daterange`, escopo `own` no `WHERE` e violação de unicidade não existem fora do Postgres. Um dublê de repositório só prova que o dublê concorda com o service.
- **Comportamento observável se pergunta ao endpoint, não ao banco.** Quando a promessa é "o mapa mostra", a asserção chama o mapa. Escrever no teste a consulta que o endpoint *deveria* fazer prova que o teste sabe a regra, não que o produto a cumpre — e o teste fica verde por cima do defeito vivo. Foi o que aconteceu com o MÉDIO 7 da rodada de 26/08, e é a lição de método mais cara que esta suíte já pagou. Vale igual para middleware: suíte que monta a cadeia à mão prova o pacote, não o produto.
- **Toda asserção de recusa vem com o controle positivo ao lado.** "Recusa sinal acima do total" é satisfeito por um sistema que recusa todo sinal; "recusa reativar a unidade" é satisfeito por um sistema que nunca reativa nada. O caso legítimo que **tem** de passar mora no mesmo teste, e é ele que separa a trava certa da trava burra.
- **Teste contra o contrato, não contra a implementação.** As asserções são sobre status, `code` de erro, envelope e efeito observável. Teste que só passa porque conhece o caminho interno do service não protege ninguém e quebra na primeira refatoração honesta.
- **Fixture própria e limpeza própria.** Cada teste cria o que precisa com sufixo único e apaga no `t.Cleanup`. Teste que depende do estado deixado por outro falha na ordem errada.
- **O arreio de teste é código, e erra igual.** Antes de acusar o produto por uma falha intermitente, pergunte se o helper não é a causa. A intermitência de três rodadas do `stream` era o helper `abrir` drenando o corpo da resposta por baixo de quem ia lê-lo — e o produto estava certo desde o começo. O que separou uma coisa da outra foi um experimento: injetar 50 ms de espera tornou a falha determinística (3 em 3) e apontou a corrida.
- **Asserção frouxa passa em mutação.** "Sobrou pelo menos uma unidade" e "voltou ao número de antes" parecem a mesma frase e não são: a primeira sobrevive a um cancelamento que não libera nada, num produto com três unidades. **Toda asserção sobre estoque, saldo ou contagem compara com o valor medido antes**, nunca com um piso. A mutação é o que revela a diferença, e é por isso que ela não é opcional em teste de jornada.
- **Defeito encontrado vira teste vermelho, não conserto.** O `qa-testes` não edita código de produção: escreve o teste que expõe o defeito, deixa falhando com mensagem em linguagem de negócio e reporta. Todo teste vermelho por defeito de produção carrega, no próprio arquivo, um bloco `⚠ ESTE TESTE ESTÁ VERMELHO E É DEFEITO DE PRODUÇÃO` dizendo a causa e de quem é o conserto.

## 5. Como ler uma falha

A mensagem de falha é escrita para quem **não** está com o código aberto. `"de 50 pedidos simultâneos, só 0 receberiam 409 DATE_CONFLICT"` diz o que o hóspede veria; `"expected true to be false"` não diz nada. Quando a falha for de concorrência, a mensagem traz também quantas transações precisaram ser repetidas — é o número que separa "o banco resolveu" de "o cliente levou o erro".

## 6. O que ainda não existe

- **e2e de jornada (Playwright)** — `apps/admin/e2e/` hoje tem a **fumaça**, que percorre 10 telas e 15 links com o navegador de verdade, mas ela **não executa nenhuma escrita**: navega, lê e confere. Continua faltando a jornada pela TELA — emitir a pré-reserva clicando, confirmar o sinal no diálogo, cancelar lendo o número da simulação. Ela é a única camada que provaria que a Server Action, o BFF e a API concordam sobre o corpo de escrita; hoje isso é conferido por um teste de forma (`corpo-de-escrita.test.ts`), que é **one-way** (painel → Go) e por construção não vê campo novo que só existe do lado Go. O ambiente para escrevê-la já existe (`make smoke-stack`); o que falta é a escrita ser reversível — uma venda de teste na tela deixa reserva no banco de desenvolvimento, e a fumaça é rodada em cima do ambiente de todo mundo.
- **Mesa de 20 cenários de tarifa e teste de propriedade** (spec §4) — o motor de `internal/domain/booking` está implementado e a jornada confere os valores da Tabela V1 ponta a ponta, mas a mesa completa de 20 combinações (feriado × fim de semana × período especial × evento × desconto) ainda não foi escrita. É o próximo alvo natural: é teste puro, sem banco, e cada linha da mesa é uma conversa de venda que já aconteceu.
- **O limite comercial de bloqueio (8 unidades × 364 dias) não tem teste porque não tem dono.** A revisão mediu o corretor fechando a Cobertura por dois meses de alta temporada com um `POST /blocks`. O `db-migrations` decidiu, com razão, que tamanho de bloqueio é regra de negócio e pertence a `internal/domain`, não a um `CHECK`; `reservas` não o implementou por ser pasta alheia. Ninguém o pegou. **Não escrevi teste vermelho para ele**: o QA guarda requisito acordado, e este ainda é proposta — a spec §11 não fixa limite nenhum. Precisa de decisão comercial antes de virar teste.
- ~~**`pii_access_log`**~~ — **pago em 27/08, e a plataforma em 02/10.** O helper mora em `internal/platform/pii` desde `91d2388` (D6 do roadmap), e na Rodada 5 ganhou as máscaras do contrato e os motivos `rooming_list` e `opportunity`. O parágrafo abaixo é o de 27/08. **Pago nesta rodada.** A tabela existia desde `20260820120000` e nunca havia sido escrita por linha nenhuma de Go; agora `GET /contacts/{id}` e `/contacts/{id}/export` gravam, e o módulo de contatos tem teste de integração para isso. O que **falta** é o helper morar em `internal/platform/pii`, irmão de `audit`: hoje ele vive dentro de `contatos/pii.go`, e a próxima tela que mostrar dado pessoal (financeiro, hóspedes de uma reserva, chat) teria de importar o módulo de contatos para conseguir a trilha. É dívida com dono declarado.
- **A mesa de PII na auditoria não tem controle negativo em `internal/router`.** `contatos/auditoria.go` mascara nome e telefone antes de gravar em `audit_log`, e o próprio módulo prova isso. O que não existe é a varredura no **sistema montado** — o equivalente de `TestTodaEscritaDaFase1DeixaTrilhaCompletaNoSistemaMontado` para PII: uma passada por `audit_log` inteiro atrás de telefone em formato E.164 e de nome de contato em claro. É teste barato e é o tipo de coisa que só quebra quando um módulo novo esquece de mascarar.
- **Carga sustentada.** O desempenho está medido por requisição isolada (§2). Ninguém mediu ainda o comportamento com dezenas de operadores simultâneos no mapa durante a véspera de Réveillon.
