# Roadmap

> Documento vivo. O `tech-lead` atualiza ao fim de cada fatia. Registra também o que foi **decidido não fazer agora**.

## Estado atual

| | |
|---|---|
| Fase corrente | **2 — Dinheiro e rotina**, aberta. A Fase 1 está concluída e no ar; a rodada de **quitação de dívida técnica** de 27/08 está commitada em `dividas/quitacao-rodada-4` (PR #1) |
| Backlog da fase | [`docs/backlog/fase-2.md`](backlog/fase-2.md) — 24 itens, os sete primeiros de dívida |
| Schema aplicado | `20260827150000` (conferido em `/readyz`: `{"database":"ok","schema_version":20260827150000,"status":"ok"}`) |
| Dívida aberta | **D1**, **D3**, **D4**, **D6**, **D7**, **D9**, **D10**. Pagas: **D2**, **D5**, **D8** |
| Última atualização | 31/08/2026, pelo `squad-lead`, ao abrir a Fase 2 |
| Repositório | `github.com/juniorsilvarc0/whitehousevillage` |

## Fase 0 — Fundação

- [x] Repositório, monorepo, `.gitignore`, `Makefile`, `.env.example`
- [x] Documentação de produto e arquitetura (`docs/`)
- [x] Definições do time de agentes (`.claude/agents/`)
- [x] `docker-compose` dev (postgres + api + admin + worker + migrate + seed) e Dockerfiles
- [x] Migrations iniciais — núcleo de identidade/RBAC e inventário/reservas com a constraint `EXCLUDE`, aplicadas e revertidas em Postgres real
- [x] Domínio puro: motor de tarifa, orçamento e cancelamento, com testes de mesa e de invariantes
- [x] CI verde (formatação, vet, testes com `-race`, ciclo de migrations, build do painel)
- [x] Casca do painel com os tokens da marca
- [x] Esqueleto chi com **tabela declarativa de rotas** — `/readyz` consulta o banco, recusa migration `dirty` e compara a versão do schema com a que o binário espera
- [x] Auth: login, refresh rotativo com detecção de reuso de família, `/auth/me`, recuperação de senha, bloqueio por tentativas
- [x] RBAC por dados: `roles`, `resources` (catálogo com `actions` e `supports_own`), `role_permissions` com escopo `all|own`; middleware lê a matriz do banco a cada requisição
- [x] Módulos `/users` e `/roles` com os seis verbos, mais `GET /roles/resources` e `PUT /roles/{id}/permissions`
- [x] `cmd/seed` idempotente: 23 recursos, os 3 perfis com a matriz inteira e um usuário de desenvolvimento por perfil
- [x] Tela de login e casca de navegação por perfil, montada a partir do que `/auth/me` devolve
- [x] **Rodadas 1 e 2 de correção** — fechadas e conferidas em Postgres real:
  - `PUT /roles/{id}` publicado no contrato **e** servido pela tabela de rotas
  - `SchemaVersionEsperada` = `20260820140000`, igual à última migration entregue — `/readyz` recusa servir com o schema atrás
  - catálogo de RBAC só no banco; a lista paralela em Go morreu (regra 8)
  - critério de aceite do corretor no financeiro registrado na spec §1, §10 e §11
- [x] **Ferramenta de teste** — `make test-integration` e o job `integration` do CI aplicavam as migrations e **pulavam o seed**. Consequência medida: sem seed a tabela `resources` fica vazia, 12 testes de `internal/modules/users` batem na FK `role_permissions_resource_code_fkey` (23503) e `TestPerfilCorretorSemeadoSalvaSemAlteracao` se **pula** — e teste pulado conta como verde. Makefile e CI passaram a semear entre as migrations e a suíte, e o job ganhou uma etapa de **concorrência repetida** (`-count=10`), porque o defeito das datas passa verde numa execução isolada
- [x] **Rodada 3** — fechada e conferida em Postgres real:
  - **conflito de datas determinístico**: a causa medida era `40P01 deadlock detected` (não `23P01`) sem ramo de tradução → 500. Corrigido com `lock_timeout` abaixo do `deadlock_timeout`, repetição com espera exponencial e tradução de `40001`/`40P01`/`55P03`
  - **tomada de conta por e-mail**: `PATCH /users` deixava quem tem `users:editar` trocar o e-mail do administrador e assumir a conta pela recuperação de senha. Fechado com a mesma autoridade exigida para trocar papel, mais revogação de sessões
  - **autoescalada pela matriz do próprio perfil** e **corrida na rotação de refresh** (`RowsAffected` ignorado deixava vários sucessores vivos): ambos fechados com teste de corrida
  - **último administrador (TOCTOU)**: contagem movida para dentro da transação, com trava de linha
  - navegação do painel sem `allowedRoles` — só matriz de permissões
- [x] **Verificação final** (feita fora do time de agentes, porque os dois verificadores da rodada 3 bateram no limite de sessão):
  - **teto de privilégio × perfil raiz**: o perfil `is_system` passou a não ser limitado pelo teto. Sem isso, um recurso criado por migration futura ficaria inconcedível por qualquer pessoa — ninguém teria a célula nova. Não afrouxa nada: `POST /roles` grava `is_system=false` sempre
  - **suíte de integração serializada** (`-p 1`): os pacotes compartilham um Postgres e em paralelo disputavam as mesmas linhas — 3 falhas em 4 execuções paralelas contra 0 em 3 serializadas. Era contenção do banco de teste, não defeito de produto
  - **teste de conflito independente da máquina**: exigir `23P01` puro amarrava o resultado ao hardware (verde 30/30 local, vermelho no runner de 2 vCPUs). Passou a aceitar também a contenção cujo `where` aponta a própria verificação da constraint, registrando a divisão entre as duas provas e exigindo que a constraint tenha atuado ao menos uma vez
  - `apps/admin/public` vazio quebrava o `COPY` do Dockerfile no CI

**Estado: concluída.** `make up && make migrate && make seed` sobe numa máquina limpa, os três perfis logam, e o CI está verde nos cinco jobs — incluindo integração com seed e a repetição dos testes de concorrência.

**Ressalva registrada**: as correções da rodada 3 têm cobertura automatizada e passaram na verificação completa da suíte, mas **não passaram por revisão adversarial independente** — os dois verificadores da rodada bateram no limite de sessão. Um terceiro ataque sobre a troca de e-mail, o teto de matriz, a corrida de rotação e a trava do último administrador continua sendo trabalho pendente, e deve rodar antes da Fase 1.

## Fase 1 — Núcleo ponta a ponta

- [x] 1a Inventário: produtos, unidades e a composição da Completa
- [x] 1b Tarifário e políticas versionadas (Tabela V1), com `PUT` criando versão em vez de editar
- [x] 1c Motor de disponibilidade e orçamento ligado ao banco
- [x] 1d Reservas: ciclo de vida completo, alocação de unidade, expiração real da pré-reserva
- [x] Telas de configuração (inventário, tarifário, calendário, política) e o `QuoteBuilder` com semáforo de alçada
- [x] Auditoria (`audit_log`) recebendo as escritas da fase, com IP e user-agent
- [x] 1e Mapa de ocupação em tempo real (SSE) — **entrou e está no ar** (`/app/mapa`, `LISTEN/NOTIFY` nos canais `whv_calendar` e `whv_crm`)
- [x] 1f CRM (funil, oportunidade, atividades, SLA) — **entrou e está no ar** (`/app/funil`, `/app/leads`, 45 rotas)
- [ ] 1g Chat WhatsApp via uazapi — **adiado por decisão do usuário**, não esquecido. Não tem fase marcada: volta quando o usuário pedir. O painel deixou de anunciar `/app/chat` no menu justamente para não prometer o que não existe

### Rodada de dívida técnica — 27/08/2026

Rodada dedicada, sem funcionalidade nova de produto, pedida com a frase "o que não pode é deixar passar débitos técnicos". O que foi **pago**:

- **`/contacts` existia como tabela e não como API.** `GET`/`POST /contacts` respondiam 404 e `reservations.contact_id` é `NOT NULL` — não havia como vender pela API sem inserir contato por SQL. Agora são 8 rotas, contrato, módulo Go, tela (`/app/contatos`) e trilha em `audit_log` + `pii_access_log` (que existia desde 20/08 e **nunca havia recebido uma linha**)
- **Orçamento não era persistido e `/win` era inexecutável.** `POST /quotes` devolvia 16 chaves e nenhuma era `id`; o `/win` respondia `422 QUOTE_REQUIRED_TO_WIN` com a dica "emita o orçamento e vincule-o à oportunidade" — instrução impossível de seguir. Agora há tabela `quotes`/`quote_nights`, `POST /quotes?persist`, `GET /quotes/{id}` (snapshot, nunca recalculado) e um `/win` que **transcreve** o orçamento em vez de repreçar
- **Troca de `unit_types.consumes` com venda viva** ganhou constraint trigger adiável nas duas direções — a última das portas da invariante da Completa que dependia só da aplicação
- **A guarda de composição contra venda concorrente** (a "brecha declarada e não fechada" da Fase 1) foi fechada no banco em `20260827100000`
- **Tela de reservas** (`/app/reservas`), que era link morto a partir do próprio CRM
- **O menu parou de mentir**: 13 destinos para 5 telas viraram 8 destinos para 8 telas, com teste que reprova nos dois sentidos. Medido: 9 requisições 404 de prefetch por visita ao `/app` caíram para 0
- **A fumaça virou portão de verdade**: reprova em 404 (e não só em 5xx), em link de menu morto, em sessão perdida no meio da varredura, e passou a cobrir 10 telas em vez de 7. `make smoke-stack` sobe imagem, schema e seed antes; o job `smoke` do CI executa esse mesmo alvo
- **O CI passou a rodar o que o portão local já rodava**: `pnpm lint`, os 334 testes do painel e `go vet -tags=integration`

O que a rodada **não** pagou está abaixo, em D1 a D10 — cada um com efeito concreto e fase. Da lista que esta rodada deixou em aberto, **D5 e D8 foram pagas nela mesma**, pela migration `20260827150000`; **D10** foi aberta depois, na leitura de abertura da Fase 2.

### Três revisões adversariais, e o que elas ensinaram

**Rodada 1** — 11 achados, 1 crítico: a exclusividade da White House Completa **não era invariante do banco**, era consequência de uma consulta devolver 8 linhas. Desativar uma unidade permitia vender a casa inteira entregando 7, pelo preço de 8.

**Rodada 2** — 7 dos 11 fechados; o crítico voltou **por outra porta** (`PUT /unit-types/{id}/members` com venda viva).

**Rodada 3** — o agente de inventário mapeou e fechou **cinco** portas, não uma: acrescentar unidade, remover unidade, trocar `consumes` por `PATCH`, trocar por `PUT`, e produto novo compartilhando unidades (esta já estava fechada pela constraint). A pior era o `consumes` `all_members → one_member`: quem pagou R$ 20.200 pela casa inteira recebia **um** apartamento, e os outros sete eram vendidos a terceiros.

**A lição de processo**, registrada em `docs/agents.md` §4.1: posse por pasta evita colisão e **cria tarefa órfã**. `SchemaVersionEsperada` ficou atrás da migration duas vezes, sinalizada por quatro agentes; `audit.Middleware` foi pedido por três e nunca ligado; o teto do bloqueio foi recusado por "não é minha pasta" e ficou sem dono. Daí o papel de **`integrador`**.

### Estado da verificação — leia antes de confiar

- Portão completo **verde**: `gofmt`, `build`, `vet`, 15 pacotes unitários, 15 de integração serializada, `lint` (que **nunca havia rodado** — o script chamava `next lint`, removido no Next 16, e o eslint nem estava instalado), `tsc`, 104 testes do painel e build.
- Seed idempotente conferido (278 previstas, 0 criadas na segunda execução).
- **A rodada 3 não teve revisão adversarial independente**: os dois agentes de verificação bateram no limite de sessão. A verificação foi feita pelo tech-lead.
- **Brecha declarada e não fechada — FECHADA em 27/08/2026.** A guarda de composição rodava dentro da transação e serializava contra outra alteração de composição, **não contra uma venda concorrente**: em `READ COMMITTED`, entre o `SELECT` e o commit cabia um `POST /reservations`. A sonda de concorrência (`composicao_concorrente_integration_test.go`) **não reproduziu o buraco em 60 disputas** — o que nunca provou ausência. A garantia definitiva era uma invariante no banco, e ela existe: `20260827100000_invariante_da_casa_inteira` traz a constraint trigger adiável `reservation_units_composicao_completa`, que exige `|reservation_units| = |composição|` no COMMIT para produto `all_members`. `20260827140000` fechou a última porta que sobrava, a troca de `consumes` com venda viva.

## Fase 2 — Dinheiro e rotina — **ABERTA em 31/08/2026**

Financeiro (recebíveis, pagáveis, pagamentos, conciliação, caução), comissões, agenda operacional, hóspedes e LGPD.
**Pronto quando**: confirmar reserva gera recebíveis e comissão sozinho, e o fechamento do mês bate com o razão.

**Backlog de implantação: [`docs/backlog/fase-2.md`](backlog/fase-2.md)** — 24 itens, cada um com efeito, dono, entrada, prova executável e quem bloqueia, mais a auditoria de segurança item a item e os riscos da fase.

**Abre com a dívida, não com funcionalidade.** A lista mudou depois de conferida no repositório e no banco, em 31/08:

| Dívida | Estado real | Onde ficou |
|---|---|---|
| **D3** — códigos de erro fora de `apperr` | aberta (a medida estava invertida: 17 fora, não 20) | F2-03, transversal, em worktree |
| **D5** — `crm_opportunities.quote_id` | **já paga** em `20260827150000` — a coluna foi removida, conferido em `information_schema` | sobrou só a correção de `docs/db.md` → F2-07 |
| **D6** — `pii_access_log` dentro do módulo de contatos | aberta | F2-01, e é o **primeiro** item da fase |
| **D7** — `quote_validity_days` como constante | aberta | F2-04 (schema) + F2-05 (a cópia coluna a coluna, que é a metade perigosa) |
| **D8** — documento sem índice único | **já paga** em `20260827150000` — `contacts_doc_unico_idx` existe, conferido em `pg_indexes` | nada a fazer |
| **D9** — miudezas | aberta. **Uma delas ganhou fase**: a segunda cópia do controle de idempotência | F2-02, porque `POST /finance/payments` seria a **terceira** cópia. O `<Toaster/>` montado em dois lugares entra junto do F2-17. As demais seguem sem fase |
| **D10** — funil sem tempo real | aberta nesta leitura | F2-06, em paralelo |

Duas dívidas **não listadas** e medidas na abertura entraram no backlog como item de segurança, não de higiene:

- **`reservations.broker_id` não tem foreign key e não é validado.** Medido: `POST /reservations` com um UUID inexistente respondeu `201`; e o corretor `corretor@wh.local`, com escopo `own`, gravou a venda com o `broker_id` de **outro** usuário, também `201`. Não existe tabela `brokers`. Quando a comissão nascer sobre esse campo, ele vira dinheiro que o beneficiário se atribui. → **F2-09** e **F2-13**.
- **A lista de contatos serve CPF completo sem gravar `pii_access_log`.** Medido como corretor: `GET /contacts?per_page=100` devolveu 11 documentos e telefones inteiros, e a contagem de `pii_access_log` ficou em 43; um `GET /contacts/{id}` levou para 44. A trilha de LGPD é contornável por um parâmetro de paginação. → **F2-23**.

## Fase 3 — Escala comercial
Portal do corretor, contratos em PDF, BI e KPIs (ocupação, ADR, RevPAR, conversão, motivos de perda).
**Pronto quando**: corretor opera sozinho no próprio escopo e o KPI de ocupação bate com a contagem manual no mapa.

Herdou da abertura da Fase 2, com a decisão registrada abaixo em 31/08:
- **Apuração do repasse ao proprietário** por competência, com snapshot da regra. A tabela `owner_payouts` nasce na Fase 2; a apuração espera dado real para ser conferida contra alguma coisa.
- **`contacts` ganha coluna de dono**, e `contacts:criar` do corretor sai de `all` para `own` — a promessa que a spec §1 registra desde 20/08.

## Fase 4 — Canais / OTA
`ChannelProvider`, iCal bidirecional, janela de risco, painel de conflitos, stubs de Airbnb e Booking.
**Pronto quando**: bloqueio criado no Airbnb aparece no mapa em ≤ 15 min e conflito vira alerta, nunca overbooking silencioso.

## Fase 5 — Operação e plataforma
Inventário operacional, ordens de manutenção, tokens com escopo, webhooks com outbox, agente de IA e MCP.

## Fase 6 — Hardening e go-live
Carga (mapa < 300 ms p95), `EXPLAIN ANALYZE` das 10 queries mais quentes, revisão de segurança, **limitador de login e de reset de senha no Redis** (dívida **D1**, abaixo), restore testado com RTO/RPO medidos, runbook e treinamento.

---

## Dívida técnica assumida

O que está no código de propósito, com fase marcada para sair. Dívida sem dono e sem fase é dívida esquecida.

### D1 — Limitadores de login e de reset vivem na memória do processo → Redis, na Fase 6

`httpx.Limitador` (`apps/api/internal/platform/httpx/middleware.go`) é um contador por chave em janela fixa guardado **num mapa do processo**. Não há coluna de bloqueio no banco: **todo** o estado de força bruta é esse mapa — o par e-mail+IP (5 erros em 15 min), o teto global por e-mail (20 em 15 min), a isenção do IP de onde a conta já entrou (7 dias) e o disparo de recuperação de senha (3 em 15 min).

Duas consequências, ambas reais assim que a API tiver mais de uma instância:

- **Com duas réplicas atrás do balanceador, o limite multiplica.** Cada processo tem o próprio mapa e nenhum vê o do outro: o atacante que espalha as tentativas ganha 5 × número de réplicas por janela. O teto por e-mail sofre o mesmo, e é ele quem deveria segurar o ataque distribuído.
- **Todo deploy zera os contadores.** Sobe versão, some a janela inteira — inclusive a isenção do IP conhecido, que é justamente a trava que impede o ataque distribuído de trancar o dono fora da própria conta. Esperar (ou provocar) um deploy vira parte do ataque.

Fica em memória na Fase 0 porque a alternativa hoje seria tabela nova + varredura, e a Fase 0 não tem Redis nem multi-instância: com **uma** instância o mapa faz exatamente o que promete, que é barrar a força bruta oportunista. A dívida vence no dia em que subir a segunda réplica — **antes**, portanto, do go-live.

**Definitivo (Fase 6)**: contador no Redis (`INCR` + `EXPIRE`, chave por par e-mail+IP, por e-mail e por IP de reset), com o limitador em memória como degradação quando o Redis não responde — limitador que cai não pode virar login sem limite nenhum. Enquanto não existir Redis, o `README` de operação diz o que já é verdade: **uma** instância da API.

### D2 — `reservation_pricing` dentro de `reservations` — **PAGA em 26/08/2026**

Quitada pela migration `20260826100000_reservation_pricing`. Conferido no `information_schema`: `reservation_pricing` existe com 12 colunas e `reservations` caiu de 32 para **23**, dentro da regra 9. Fica registrada aqui, e não apagada, porque a decisão de adiar está na tabela abaixo e a dívida quitada é a prova de que o adiamento tinha prazo.

### D3 — Dezessete dos 37 códigos de erro do contrato são declarados fora de `apperr`, e `RESOURCE_IN_USE` existe em cinco lugares → consolidar na Fase 2

O `components.responses.Erro` da OpenAPI diz, textualmente, que o enum é "espelhado em `internal/platform/apperr`". Não é.

**Medida corrigida em 31/08/2026.** A redação anterior desta entrada dizia "20 dos 37 são declarados fora de `apperr`", e estava **invertida** — contado no repositório, `apperr` declara **20** dos 37 e **17** nascem nos módulos: `RATE_NOT_FOUND`, `POLICY_IMMUTABLE`, `CODE_IN_USE`, `INVALID_STATE_TRANSITION`, `RESERVATION_NOT_CANCELLABLE`, `UNIT_NOT_AVAILABLE`, `HOLD_LIMIT_REACHED`, `STAGE_NOT_IN_PIPELINE`, `STAGE_ORDER_INCOMPLETE`, `DEFAULT_PIPELINE_REQUIRED`, `OPPORTUNITY_ALREADY_CLOSED`, `LOSS_REASON_REQUIRED`, `QUOTE_REQUIRED_TO_WIN`, `LEAD_ALREADY_CONVERTED`, `CONTACT_DUPLICATE`, `CONTACT_ANONYMIZED`, `QUOTE_NOT_PENDING`. Cada módulo com a sua função local (`erro`, `conflito`, `invalido`) e a sua mensagem. A dívida é a mesma; o número estava errado, e número errado num documento que manda alguém trabalhar é como o trabalho sai do tamanho errado.

`RESOURCE_IN_USE` está declarado **cinco** vezes — `apperr/apperr.go:81`, `tarifario/erros.go:26`, `crm/crm.go:167`, `contatos/contatos.go:136`, `inventario/erros.go:27` —, com **quatro** frases distintas ("Ainda há vínculo ativo neste registro." aparece duas vezes).

Efeito concreto: o mesmo `code` chega ao painel com mensagens diferentes conforme a rota, e nada impede a sexta cópia de divergir também no **status HTTP** — aí o painel, que reage ao `code`, passa a receber 409 numa rota e 422 noutra para a mesma situação.

Por que é aceitável até lá: a divergência é de *mensagem*, e o contrato fixa o `code`, que é o que o painel consome. E, desde esta rodada, `internal/router/contrato_de_erros_test.go` fecha o buraco que doía de verdade — nenhuma das cópias pode inventar um `code` fora do enum, nem o enum pode crescer sem um emissor em Go. Nasceu assim porque `internal/platform` não era pasta de nenhum dos agentes que precisaram do código.

**Definitivo (Fase 2)**: um `apperr.Definir(code, mensagem, status)` por código, em `apperr`, e os módulos importando. É movimentação mecânica, sem mudança de comportamento — o que a torna candidata natural a uma fatia curta no começo da fase, e não a um item que espera funcionalidade.

### D4 — O limitador de login colapsa num contador único atrás do BFF → junto com D1, na Fase 6

**Medido em 27/08/2026, no stack de desenvolvimento.** `LimitadorDeLogin()` é `httpx.NovoLimitador(20, 15min)` com a chave `"ip:" + IPDoCliente(r)`, e conta **toda** requisição a `/auth/login`, inclusive as bem-sucedidas: 20 logins corretos seguidos do mesmo IP responderam `200`, e o **21º respondeu `429 RATE_LIMITED`**.

O agravante não é a contagem, é a chave. O painel é um BFF: o navegador nunca fala com a API, quem fala é o servidor Next. No log da API, **todos** os logins vindos do painel aparecem com o mesmo IP — `172.24.0.4`, o do contêiner `admin` — contra os IPs variados de quem chama a API direto. O limitador "por IP" vira, na prática, um teto **global de 20 logins a cada 15 minutos para a instalação inteira**.

Efeito concreto: numa troca de turno com sete pessoas entrando, mais um punhado de sessões expiradas reabrindo, a recepção inteira lê "Muitas requisições. Tente em instantes." por quinze minutos — e o log não diz que foi limitador de IP, diz 429 sem dono. Foi essa a causa provável da única reprovação intermitente da fumaça nesta rodada (1 em 10 execuções); a fumaça agora imprime o corpo da resposta do login, então a próxima ocorrência se explica sozinha.

Por que é aceitável até lá: com uma instalação e poucos operadores o teto não é alcançado no uso normal, e um limitador frouxo é pior do que um limitador rude. A correção certa **não** é aumentar o número: é o BFF encaminhar o IP do navegador em `X-Forwarded-For` — `httpx.RealIP` já sabe consumi-lo quando o peer direto é rede interna, que é exatamente o caso do contêiner do painel. Enquanto o painel não encaminhar, aumentar o teto só adia o mesmo bloqueio coletivo.

**Definitivo (Fase 6, com D1)**: contador no Redis, chave por par e-mail+IP **do usuário final**, e o painel encaminhando o IP de origem. Os dois passos vão juntos: contador distribuído com a chave errada distribui o mesmo erro.

### D5 — `crm_opportunities.quote_id` apontava para `reservations` → **PAGA em 27/08/2026** (`20260827150000`)

A coluna nasceu em `20260827110000_crm.up.sql` quando "orçamento" era uma reserva em estado `quote`; a FK `crm_opportunities_quote_id_fkey` referenciava `reservations(id)`. A tabela `quotes` chegou em `20260827130000` e a FK **não** foi repontada, de propósito: repontar no meio da rodada quebraria o CRM que estava no ar.

Efeito concreto: a coluna virou dado morto. O "orçamento vigente" do CRM é derivado de `quotes.opportunity_id` (`ORDER BY created_at DESC LIMIT 1`), e o `/win` funcionava sem migration pendente — mas o contrato afirmava, no schema `Oportunidade`, que `quote_id` "aponta para `quotes(id)` desde `20260827130000`", e o banco não tinha esse fato. Duas noções de "orçamento vigente" convivendo é como uma delas passa a ser lida por engano.

**Como foi paga.** Pela remoção, não pelo repontamento — a decisão está na tabela abaixo, em 27/08. Repontar manteria uma coluna morta com o alvo certo, e a próxima pessoa suporia que ela significa alguma coisa. Nenhuma linha de Go mudou: não havia um `SET quote_id` em todo o `apps/api`, e quem responde "qual é o orçamento vigente" continua sendo a subconsulta sobre `quotes`, coberta por `quotes_oportunidade_idx`.

Medido antes (banco no ar, antes da migration): a coluna existia, a FK apontava para `reservations(id)`, e a oportunidade ganha pelo `/win` ficava com `quote_id = NULL` — escrita por ninguém, lida por ninguém. Medido depois, conferido em 31/08/2026 no Postgres de desenvolvimento: `select column_name from information_schema.columns where table_name='crm_opportunities' and column_name='quote_id'` devolve **zero linhas**, e a tabela caiu de 24 para **23** colunas. O `down` restaura a coluna **como ela era**, com a FK para `reservations` — um `down` que "melhora" o passado deixa de ser o inverso do `up` e vira uma migration não declarada.

Se um dia o produto quiser "o orçamento **escolhido**" — diferente do último emitido —, a coluna volta apontando para `quotes(id)`, e aí com quem a escreva.

**Fica pendente, e é do `tech-lead`**: `docs/db.md` §7 ainda lista `quote_id?` entre as colunas de `crm_opportunities`, ainda diz "24 colunas" e ainda traz a subseção "`quote_id` — o orçamento vigente **é uma reserva**". Documento de contrato descrevendo schema que não existe é como a coluna volta. Virou o item **F2-07** do backlog da Fase 2.

### D6 — `pii_access_log` e a redação de PII moram dentro do módulo de contatos → Fase 2 (LGPD)

`registrarAcessoPII` (`internal/modules/contatos/pii.go`) e o `semPII` da trilha (`contatos/auditoria.go`) são plataforma disfarçada de módulo: estão ali porque `internal/platform` não era pasta do agente que os escreveu. A Fase 2 traz financeiro, hóspedes de reserva e chat — as três telas que mostram dado pessoal e vão precisar dos dois.

Efeito concreto: a próxima tela que mostrar PII vai importar o módulo de contatos para conseguir registrar o acesso, ou — pior e mais provável — não vai registrar. `audit.Redigir` protege **segredo** (filtro por substring: senha, token, hash) e não protege PII: `name` e `phone_e164` não casam com nada dele.

Por que é aceitável até lá: hoje só contatos serve PII, e ali a obrigação é cumprida e testada (o acesso falha fechado — não conseguir registrar quem leu aborta a leitura). **Definitivo (Fase 2)**: `internal/platform/pii`, irmã de `audit`.

**Item de backlog: F2-01**, o primeiro da Fase 2. E a abertura da fase mediu um agravante que esta entrada não previa: a lista de contatos serve `doc_number` e `phone_e164` **completos** e não grava `pii_access_log` (43 → 43 numa listagem de 11 fichas; 43 → 44 numa leitura de ficha). A decisão escrita no módulo — "a lista não grava: ela é a agenda do dia" — parte de uma premissa falsa, porque a lista serve a ficha inteira. Virou o item **F2-23**.

### D7 — `commercial_policies` não tem `quote_validity_days` → Fase 2

A validade de 7 dias do orçamento é constante de aplicação (`validadePadraoEmDias`, em `disponibilidade/dto_orcamento_salvo.go`), ao lado de `hold_hours` e `balance_due_days`, que são **dado versionado**. Contraria "toda regra comercial é dado versionado".

Por que é aceitável até lá: `quotes.valid_until` é `NOT NULL` e gravada por quem emite, então o orçamento já congela a sua própria validade — mudar o padrão não reescreve o passado. E há um agravante que impede a correção pela metade: `PublicarPoliticaComercial` (`tarifario/repository_politicas.go`) copia as colunas **nome a nome**, então uma coluna nova nascida sem entrar nessa cópia voltaria ao `DEFAULT` a cada versão publicada, em silêncio. Os dois passos vão juntos.

Conferido em 31/08: `commercial_policies` tem 13 colunas e nenhuma é `quote_validity_days`; a constante é `validadePadraoEmDias = 7` (`disponibilidade/dto_orcamento_salvo.go:22`); e o `INSERT` de `repository_politicas.go:150-160` continua listando coluna por coluna. **Itens de backlog: F2-04** (o schema, `db-migrations`) **e F2-05** (a cópia, `backend-go`), nesta ordem e sem intervalo — o F2-05 exige controle negativo: remover a coluna da lista do `INSERT` tem de deixar o teste vermelho.

### D8 — Deduplicação de contato por documento sem índice único → **PAGA em 27/08/2026** (`20260827150000`)

`contacts_phone_idx` é `UNIQUE` parcial, então o telefone é garantido pelo banco (`23505` → `409 CONTACT_DUPLICATE`, sem `SELECT` antes do `INSERT`). O documento não tem índice único: `contacts_doc_idx` é índice comum, e a proteção é um `pg_advisory_xact_lock(classe, hashtext(propriedade:tipo:numero))` no service.

Efeito concreto: a corrida está fechada **para quem passa por esta API** — controle negativo medido, 5 falhas em 5 execuções sem a trava, verde com ela —, e **aberta** para `psql`, importação de planilha e qualquer outro processo. O contrato promete a garantia; o schema não a tem.

**Como foi paga.** `contacts_doc_unico_idx` existe desde `20260827150000`. Nenhuma linha de Go mudou: `repository.go:traduzir` já esperava o nome, e o `23505` passou a disparar sozinho — a trava virou a redundância barata que estava prevista, nessa ordem, sem janela desprotegida.

Medido antes: dois `INSERT` com o mesmo CPF, `count(*) = 2`. Medido depois: o segundo levanta `contacts_doc_unico_idx`, e pela API o segundo `POST /contacts` responde `409 CONTACT_DUPLICATE` com o `contact_id` de quem já existe.

O índice é sobre `coalesce(doc_type, '')`, e não `doc_type` puro: a API já recusa número sem tipo, mas em índice único NULO é distinto de NULO, e dois documentos iguais **sem tipo** passariam pelo índice feito para impedi-los — justamente a escrita fora da API que motivou a dívida. Continua parcial (`WHERE doc_number IS NOT NULL`), porque anonimizar zera o documento e duas fichas anonimizadas não podem colidir. Os três casos foram medidos.

### D9 — Miudezas com dono; duas ganharam fase na abertura da Fase 2

Cada uma é pequena, todas são reais, e a lista existe para que nenhuma volte a depender de alguém lembrar:

- **`<Toaster/>` do `sonner` não está montado na casca** (nem em `app/layout.tsx`, nem no `DashboardShell`). São **duas** montagens locais (`components/crm/avisos.tsx` e `components/contatos/avisos.tsx`), hoje mutuamente exclusivas na árvore — no dia em que duas coexistirem, cada `toast()` aparece em duplicata. Montar uma na casca é o conserto; exige apagar as duas locais **na mesma mudança**. **Ganhou fase**: F2-17, junto das telas de financeiro — é ali que a segunda montagem passa a coexistir com a primeira, e cada `toast()` viraria dois.
- **Duas cópias do controle de idempotência** (`reservas/idempotencia.go`, 186 linhas, e `crm/idempotencia.go`, 161 — e o `diff` das assinaturas de função entre as duas é **vazio**), pelo mesmo impedimento de pasta que gerou D3 e D6. O lugar é `internal/platform/idempotencia`. **Ganhou fase**: item **F2-02**, no começo da Fase 2, porque `POST /finance/payments` nasceria como terceira cópia — e a chave é `(key, endpoint, actor_id, property_id)`, então uma cópia que esqueça o ator devolve o corpo guardado por outro usuário.
- **A cópia do snapshot do orçamento é feita por fixação no contexto** (`disponibilidade/snapshot.go`), porque `reservas` não era pasta de quem escreveu o `/win`. O desenho final é `reservas.Servico.EmitirDoOrcamento` recebendo o snapshot como argumento. Junto disso: `reservation_pricing.cancellation_policy_id` vem da política **vigente hoje**, e não de `quotes.cancellation_policy_id` — coincidem hoje, divergem no dia em que uma política nova for publicada entre a emissão e o ganho.
- **`lib/crm/idempotencia.ts` e `lib/crm/datas.ts` são usados por reservas**, e `lib/mapa/expiracao.ts` é a contagem regressiva da pré-reserva, usada fora do mapa. Helper de fuso com dois lugares para consertar é o que quebra no dia em que Fortaleza mudar de regra.
- **`lib/api/codigos.ts` (painel) não conhece `CONTACT_DUPLICATE`, `CONTACT_ANONYMIZED` nem `QUOTE_NOT_PENDING`**: medido, `normalizarCodigo("CONTACT_DUPLICATE", 409) === "INTERNAL"`. Contornado com espelhos locais em `lib/contatos/codigos.ts` e `lib/crm/codigos.ts`; quando o espelho central crescer, três arquivos encolhem.
- **`GET /unit-types/{id}/members` exige `inventory:ver`** e é quem alimenta o diálogo de realocação de reserva. Quem tem `reservations:editar` e não tem inventário vê a tela degradada com a explicação. Ou a matriz do seed concede, ou `/reassign-unit` ganha rota própria de destinos possíveis — é decisão de contrato, não de código.
- **As três derivações do painel inicial** (`calcularOcupacao`, `movimentoDoDia`, `alertasDeConfiguracao`) são puras e vivem dentro de `app/(app)/app/page.tsx`, sem teste unitário. O lugar é `src/lib/painel/`.
- **`golangci-lint` não está instalado nesta máquina**, e `make lint` para nele. O CI também não o roda. Ou o portão passa a instalá-lo, ou `make check` está prometendo uma etapa que ninguém executa.
- **`contacts_doc_idx` ficou redundante desde `contacts_doc_unico_idx`** (D8). Conferido em 31/08: os dois existem em `pg_indexes`. Não é defeito — é custo de escrita e uma segunda linha na página de índices para quem for ler o schema. Cai junto com a próxima migration que encostar em `contacts`.
- **`docs/db.md` §7 descreve `crm_opportunities.quote_id`, que não existe mais** (D5, paga). Documento de contrato descrevendo schema morto é como a coluna volta. Tem dono (`tech-lead`) e fase: item **F2-07**.


### D10 — O funil não tem tempo real, embora o barramento dele já esteja pronto e ligado → Fase 2

**Aberta em 31/08/2026 pelo `squad-lead`**, na leitura de abertura da Fase 2. Não foi assumida por ninguém: é dívida que apareceu porque a metade cara foi entregue e a metade barata não.

O plano da Fase 1e prometia o mapa **e** o kanban se movendo sozinhos, e a infraestrutura foi construída para os dois. Medido no stack no ar:

- `GET /api/v1/stream?topics=calendar,crm` responde `event: ready` com `{"topics":["calendar","crm"]}` — o tópico `crm` existe, é servido, e o handshake confere a permissão dele contra a matriz (`crm.opportunities:ver`), separadamente do `calendar`;
- `crm_opportunities` tem **dois** gatilhos publicando em `whv_crm` (`crm_opportunities_notificar`, `crm_opportunities_notificar_mudanca`), e a migration `20260827120000` escreve no comentário, com todas as letras: "`whv_crm` — `crm_opportunities` (**o kanban**)";
- e no painel, `grep -rn useAtualizacaoAoVivo apps/admin/src` devolve **um** consumidor: `components/mapa/mapa-de-ocupacao.tsx`. O kanban só chama `router.refresh()` **depois da ação do próprio usuário** (`components/crm/kanban.tsx:170`).

Efeito concreto: duas pessoas no funil não veem o trabalho uma da outra. Quem arrasta o card vê; quem está com a tela aberta ao lado continua vendo o card na coluna antiga até apertar F5 — e liga para o cliente que o colega acabou de ganhar. Não há corrupção de dado, porque a etapa é gravada no servidor; o que há é a operação decidindo por uma tela velha.

Por que é aceitável até aqui: com uma ou duas pessoas no funil, a janela é pequena e o refresh manual resolve. E o desenho está certo — o problema é uma ponta que não foi ligada, não uma escolha errada.

**Definitivo (Fase 2, item F2-06)**: o kanban assina `topics=crm` com o mesmo `useAtualizacaoAoVivo` que o mapa usa. Entra na Fase 2, e não vira linha adiada, por uma razão de princípio deste squad: **gatilho, canal e RBAC de tópico que ninguém consome são código morto com nome plausível** — ou ganham dono, ou somem. O custo é um componente reusando um hook que já existe, em pasta disjunta de todo o resto do bloco de dívida, então roda em paralelo e não empurra nada.

O teste é a parte que importa, e ele tem forma obrigatória: **re-renderizar com identidade nova da fábrica de conexão**. Foi exatamente essa a forma que os 13 testes do hook de SSE não sabiam falhar — todos passavam uma fábrica estável de módulo, a única que não podia quebrar, enquanto a conexão reabria **1957 vezes em 9 segundos** no build minificado.

---

## Decisões registradas

| Data | Decisão | Motivo |
|---|---|---|
| 20/08/2026 | OTAs começam por **iCal**, não por API | Airbnb (Software Partner) e Booking (Connectivity) são fechados; a *Demand API* do briefing é da ponta compradora |
| 20/08/2026 | **Unidades físicas nominais**, produto ≠ unidade | Sem `unit_id` concreto não há constraint capaz de impedir overbooking; e a operação precisa saber qual apartamento limpar |
| 20/08/2026 | **pgx nativo**, não sqlc nem ORM | `daterange`, `EXCLUDE`, `jsonb` e `LISTEN/NOTIFY` não sobrevivem bem ao codegen; filtros dinâmicos também não |
| 20/08/2026 | **SSE**, não WebSocket | Push é unidirecional; atravessa Traefik sem upgrade e reconecta sozinho |
| 20/08/2026 | **River** para jobs, sobre o próprio Postgres | Retry, unique job e agendamento sem Redis na VPS |
| 20/08/2026 | RBAC com eixo de **escopo `all` \| `own`** | É o que resolve "corretor vê só o dele" sem `if role ==` espalhado |
| 20/08/2026 | Perfis iniciais: **admin, usuario, corretor** | Definido pelo usuário; novos perfis entram por configuração, não por código |
| 20/08/2026 | O **catálogo** de RBAC é lido do banco, nunca de mapa em Go | Quais recursos existem, quais ações cada um aceita e onde `own` faz sentido vivem em `resources`. Um mapa paralelo em Go é uma segunda fonte da verdade: no dia em que alguém edita só uma, a tela oferece permissão que o banco recusa (ou esconde a que ele concede). A regra 8 já dizia RBAC é dado — o catálogo faz parte do dado |
| 20/08/2026 | Atribuir papel exige `roles:editar`, o papel alvo tem de ser **subconjunto** do ator, e ninguém altera o próprio papel | Sem isso, editar usuário é escalar privilégio: bastava se atribuir `admin`. "Subconjunto" quer dizer que ninguém concede o que não tem. A exceção do próprio papel evita tanto a escalada quanto o tiro no pé de se rebaixar e travar a instalação |
| 20/08/2026 | O vocabulário de recursos do painel é o **do catálogo do banco** | A grade de perfis e os `can()` da navegação leem `GET /roles/resources`. Nome inventado no front vira permissão que nunca casa com nenhuma linha de `role_permissions` — e falha em silêncio, escondendo menu de quem tinha acesso |
| 20/08/2026 | Corretor no financeiro: `403` em recebíveis e pagáveis, `200` em `own` nas comissões | A spec §1 dizia `403` em `/finance/*`, e o §11 promete a ele um painel de comissões previstas e pagas. Escrito ao pé da letra, o teste derrubaria a implementação correta; "consertar" a matriz tiraria o painel que a spec promete. Registrado em `spec.md` §1, §10 e §11 |
| 20/08/2026 | `reservation_pricing` **não** é extraída nesta rodada; vira a tarefa **1a** | As 32 colunas de `reservations` são dívida real, mas nenhum código depende da tabela ainda. Mexer no schema agora acopla a versão de migration às correções em curso — e `/readyz` recusa servir com schema fora da versão esperada, o que pararia a correção inteira por uma refatoração que pode esperar duas semanas |
| 20/08/2026 | **Alterar o e-mail de um terceiro exige a mesma autoridade que alterar o papel dele** | E-mail é credencial de recuperação, não cadastro: quem troca o e-mail de outra conta e dispara "esqueci a senha" fica dono dela. Se `users:editar` bastasse, quem só corrige nome e telefone teria o caminho mais curto para tomar a conta do administrador — escalada com um PATCH. Vale também o teto: não se altera o e-mail de quem está acima do ator |
| 20/08/2026 | **Toda troca de e-mail revoga as sessões da conta** | A troca só é segura se o acesso anterior morrer junto. Sem isso, o atacante que trocou o e-mail continua com o refresh na mão mesmo depois de o dono retomar o endereço, e a conta fica com dois donos. Revogar é o que transforma "recuperei meu e-mail" em "recuperei minha conta" |
| 20/08/2026 | **Ninguém edita a matriz do próprio perfil, e a matriz concedida nunca excede a do ator** | São as duas metades da mesma escalada. Editar o próprio perfil é se dar permissão sozinho; conceder a terceiro o que não se tem é se dar permissão por interposta pessoa (crio um perfil `all`, atribuo a um usuário meu, entro com ele). A trava do próprio perfil também evita o tiro no pé de se rebaixar e travar a instalação. `all` **contém** `own`: quem enxerga tudo pode delegar o recorte do dono |
| 20/08/2026 | **A navegação do painel é função apenas da matriz de permissões**; `allowedRoles` por papel foi removido | Era uma segunda fonte de verdade do RBAC, contra a regra 8: perfil novo criado por configuração não aparecia em lista nenhuma e navegava vazio, e mudar a matriz de um perfil não mudava o menu. Menu que não bate com o que a API concede engana os dois lados — esconde o que a pessoa pode e oferece o que ela não pode |
| 20/08/2026 | **Conflito de datas responde 409 de forma determinística, inclusive sob contenção** | A regra 2 do CLAUDE.md não admite "409 quando dá tempo". Sob disputa real o Postgres pode devolver `40P01` (impasse) em vez de `23P01`, e traduzir isso para 500 quebra a promessa exatamente na véspera de Réveillon, que é quando ela importa. Determinístico quer dizer: o perdedor ouve "essas datas acabaram de ser ocupadas", com quantos concorrentes forem |
| 27/08/2026 | **Orçamento é tabela própria (`quotes`), não reserva em estado `quote`** | Era o desenho do `docs/db.md` §7 e foi revisto na entrega: orçamento não ocupa unidade (não referencia `stay_blocks`), o funil emite N orçamentos por negociação e cada um gastaria um código `WH-2026-…` de reserva, e `reservations` já estava em 23 das 25 colunas da regra 9. Orçamento é **imutável**: sem `PUT`/`PATCH`/`DELETE` e sem coleção `GET /quotes`, porque reprecificar é emitir outro — e porque um par GET+POST na coleção acionaria o teste dos seis verbos, que exigiria justamente os verbos que não devem existir |
| 27/08/2026 | **`/win` transcreve o orçamento; o motor de preço não roda de novo** | Medido: com a tarifa reajustada em +R$1.000/noite entre a emissão e o ganho, a reserva nasceu com o preço do orçamento (subtotal, total, sinal, `rate_table_id` e `policy_version` idênticos, 4 noites iguais). O contrário — recalcular no ganho — é vender por um preço e cobrar outro. A contrapartida é `409 DATE_CONFLICT` como desfecho **normal** do `/win`: orçamento não bloqueia data, e quem emitiu não reservou |
| 27/08/2026 | **A mensagem da constraint trigger vai para o usuário, por allowlist** | As invariantes de negócio vivem em constraint trigger e já levantam a frase certa em `pg.Message`/`pg.Hint` ("a Completa tem 1 reserva de pé..."), e tudo isso virava `422 "valor fora do permitido pela regra do banco."` — que não diz o que houve nem o que fazer, e ainda discordava do contrato, que documenta `409 RESOURCE_IN_USE` para o mesmo caso. A tradução é **allowlist nomeada**, nunca automática: repassar `pg.Message` de qualquer `23514` publicaria texto de banco que ninguém revisou. E a invariante que só o nosso código pode violar (`quote_nights_fecham_o_orcamento`) vira **500**, não 422 — mandar o operador procurar erro num formulário que estava certo é pior do que assumir o defeito |
| 27/08/2026 | **A fumaça reprova em 404, não só em 5xx** | Tela que não existe responde 404, desenha o "This page could not be found" do Next — que não é 5xx e não tem aviso do painel — e passava por boa. Era literalmente o caso de `/app/reservas`, entregue numa rodada em que a fumaça ficou verde sem a tela existir na imagem servida. Junto: prefetch RSC 404 (link de menu morto) e volta involuntária para `/login` também reprovam |
| 27/08/2026 | **`make migrate` e `make seed` reconstroem a imagem** | `make up` reconstruía só `api` e `admin`, e os dois rodavam o binário de ontem. O sintoma foi silencioso: `make seed` respondeu "previstas 295 ... nada mudou" e deu o banco por atualizado enquanto o seed do commit semeava 298. Aplicar **schema** com o binário de ontem é a mesma falha com consequência muito maior |
| 27/08/2026 | **A identidade de um valor default de parâmetro não é dependência de efeito confiável** | `useSSE` recebia a fábrica da conexão por `criarFonte = fonteDoNavegador` e a listava nas dependências. Em `next dev` o default resolvia para a mesma referência e o efeito rodava uma vez; no build minificado passou a valer uma função nova a cada render, e a conexão reabria a cada repintura — **medido: 1957 aberturas em 9 s contra 1 em dev**, com o painel parado no mapa, e 22.738 acumuladas. A suíte ficou verde o tempo todo porque todos os 13 testes passavam um `criarFonte` estável de módulo, a única forma que não podia falhar. A fábrica virou Effect Event, como as outras três callbacks do arquivo, e o teste que faltava re-renderiza com identidade nova |
| 27/08/2026 | **Sonda de saúde mora na raiz, fora de `/api/v1`** | As duas nasceram sob o prefixo e `curl localhost:8080/readyz` devolvia 404, contra o plano e contra o que qualquer runbook tenta primeiro. Quem chama sonda é o healthcheck do Compose, o proxy da frente e o orquestrador, com caminho fixo escrito na infraestrutura: no dia do `/api/v2` esse caminho não pode ser reescrito junto nem passar a existir em duas versões. Servi-las nos dois caminhos seria pior que escolher errado — dois endereços para o mesmo fato. `TestSondasSoExistemNaRaiz` cobra a escolha **nos dois sentidos**, e a OpenAPI declara `servers` próprio nesses dois paths |
| 27/08/2026 | **Healthcheck de imagem distroless exige flag no próprio binário** | O Compose já invocava `["CMD", "/app/api", "-healthcheck"]` e o comentário admitia por escrito que a flag estava "pendente no backend-go". Sem ela o binário caía no caminho normal e **subia uma segunda API a cada 10 s**, que morria em `address already in use` e ainda abria uma conexão de banco antes de sair. O container passou a vida `unhealthy` — 272 falhas seguidas, medidas — e nenhum `depends_on: service_healthy` apontado para a API teria liberado nada. A sonda é de VIDA e não de prontidão: um `/readyz` aqui marcaria o container como doente sempre que o banco piscasse, e a resposta do orquestrador a "doente" é derrubar quem estava de pé |
| 27/08/2026 | **Coluna morta com FK errada some, em vez de ser repontada** | `crm_opportunities.quote_id` nasceu apontando para `reservations(id)`, de quando orçamento era reserva no estado `quote`. Ninguém a escrevia (a oportunidade ganha pelo `/win` ficava com `NULL`) e ninguém a lia — quem responde "qual é o orçamento vigente" é a subconsulta sobre `quotes`. Repontar manteria uma coluna morta com o alvo certo, e a próxima pessoa suporia que ela significa alguma coisa. Se um dia o produto quiser "o orçamento ESCOLHIDO", ela volta apontando para `quotes(id)` — e aí com quem a escreva |
| 31/08/2026 | **A Fase 2 abre por sete itens que não entregam tela nenhuma** | O roadmap já mandava abrir pela dívida; a leitura de abertura confirmou o preço. `internal/platform/pii`, `internal/platform/idempotencia` e a consolidação de `apperr` são exatamente as três coisas que o financeiro vai usar e que hoje existem em duplicata dentro de módulos. Escrever `POST /finance/payments` antes deles é criar a terceira cópia do controle de idempotência e a sexta do `RESOURCE_IN_USE` — e aí o conserto passa a ter de mexer no código que move dinheiro |
| 31/08/2026 | **`crm_opportunities.quote_id` foi removida, não repontada** — e D5 fica registrada como paga em vez de apagada | Já estava decidido em 27/08 e foi conferido no banco em 31/08. A entrada permanece no documento pelo mesmo motivo da D2: dívida quitada é a prova de que o adiamento tinha prazo. Apagar a entrada apaga a prova |
| 31/08/2026 | **O tempo real do funil (D10) entra na Fase 2, e não vira linha adiada** | A metade cara já está pronta e **não é consumida por ninguém**: o tópico `crm` é servido, tem RBAC próprio no handshake, e `crm_opportunities` tem dois gatilhos publicando em `whv_crm`. Caminho que o sistema produz e ninguém lê é código morto com nome plausível — a revisão deste squad recusa isso em qualquer outro contexto, e não pode abrir exceção quando o morto é infraestrutura própria. Custa um componente reusando um hook que já existe, em pasta disjunta: roda em paralelo e não empurra nada |
| 31/08/2026 | **O chat WhatsApp (1g) não entra na Fase 2 — nem como proposta** | Adiamento explícito do dono do produto. Não é esquecimento, não é prioridade baixa e não é assunto a reabrir por iniciativa do time: volta quando ele pedir. O item 1f do painel já deixou de anunciar `/app/chat` como pronto pelo mesmo motivo — não prometer o que não existe |
| 31/08/2026 | **D1 e D4 (limitador no Redis + IP real atrás do BFF) ficam na Fase 6, mas ganham uma sonda na Fase 2** | Os dois passos vão juntos, e isso não mudou: contador distribuído com a chave errada distribui o mesmo erro. O que mudou é o uso — a Fase 2 põe Agenda e Financeiro em uso diário, com mais gente entrando por turno, e o teto de 20 logins por 15 min é **global** porque o painel não encaminha `X-Forwarded-For` (conferido: zero ocorrências em `apps/admin/src`). A fumaça da fase passa a logar quatro usuários duas vezes cada e reprovar em `429`. Se reprovar, a metade do BFF sobe para a Fase 2; se não, a dívida fica onde está, com uma guarda que avisa antes do usuário |
| 31/08/2026 | **`owner_payouts` ganha schema na Fase 2 e apuração na Fase 3** | A tabela nasce junto com o resto do modelo financeiro para não abrir migration de novo depois. A apuração por competência, com snapshot da regra usada, precisa de mais de um mês de dado real para ser conferida contra alguma coisa — apurar contra base vazia é escrever um relatório que ninguém consegue verificar, e relatório não verificável é como um número errado entra em produção com cara de certo |
| 31/08/2026 | **`contacts` não ganha coluna de dono na Fase 2** — o que a fase resolve é o vazamento de leitura | O §1 da spec já registra que `contacts:criar` do corretor fica em `all` até a coluna existir. Criá-la agora obriga a decidir o que acontece com as fichas sem dono e com o contato que dois corretores atendem — decisão de produto que ninguém pediu, no meio da fase do dinheiro. O que dói hoje é medível e é outro: a lista serve CPF completo sem gravar `pii_access_log`, e isso é o item F2-23. A coluna vai para a Fase 3, com o portal do corretor |
| 20/08/2026 | O job de integração do CI **semeia o banco** e **repete os testes de concorrência** (`-count=10`) | Sem seed, `resources` fica vazio: parte dos testes bate em FK e o do perfil Corretor semeado se **pula** — teste pulado conta como verde. E o defeito das datas é probabilístico: medido nesta rodada, `-count=1` passou verde e `-count=10` reprovou. Uma execução por PR deixava ~67% de chance de a falha atravessar; dez deixam ~2% |

| 02/10/2026 | **O site de vendas entra no monorepo como `apps/site`, e reserva pública volta ao escopo** | Dois sistemas descrevendo a mesma casa divergem no dia seguinte: o MVP calculava tarifa, estadia mínima, sinal e alçada em JavaScript, com os números escritos num arquivo, enquanto o motor real já vivia em `internal/domain`. Enquanto o cliente via um preço e o banco guardava outro, a segunda fonte da verdade era justamente a que o cliente lê. Juntar os dois é o único jeito de o site vender o que a casa realmente tem — e cobra o preço escrito em `unificacao-site-crm.md`: pagamento online, limitador distribuído, LGPD na porta e defesa contra negação de inventário |

## Fora de escopo por enquanto

Aplicativo nativo · multi-tenant comercial · emissão fiscal · rodar modelo de IA internamente.

**Duas linhas saíram desta lista em 02/10/2026**: *motor de reserva público com pagamento online* e *substituir o site de marketing*. O site virou `apps/site` dentro deste monorepo e passa a ser o front de cliente. O plano, a ordem, os donos e o que a abertura ao público obriga a consertar antes estão em [`unificacao-site-crm.md`](unificacao-site-crm.md) — em particular a dívida **D1**, que deixa de ser dívida e passa a ser bloqueio.
