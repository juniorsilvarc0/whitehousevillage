# Roadmap

> Documento vivo. O `squad-lead` atualiza ao fim de cada rodada (o arquivo é pasta dele, `docs/agents.md` §2). Registra também o que foi **decidido não fazer agora**.

## Estado atual

| | |
|---|---|
| Fase corrente | **2 — Dinheiro e rotina**, aberta. Bloco 0 (dívida) **fechado**; Bloco 1 começou pelo F2-09. Em paralelo, o plano de [unificação do site](unificacao-site-crm.md) está no passo A0 mais a limpeza D1/D2/D4 |
| Backlog da fase | [`docs/backlog/fase-2.md`](backlog/fase-2.md) — 24 itens. Estado item a item no quadro "Estado em 02/10/2026": **9 feitos**, **2 parciais**, **13 abertos** |
| Schema da árvore | `20261002180000` (`brokers_e_fk_do_corretor`) = `router.SchemaVersionEsperada`. Conferido em 02/10 no Postgres de teste recriado do zero: `banco pronto: 20261002180000\|f`. O banco do stack de desenvolvimento **não** foi conferido nesta rodada (o ambiente dela não tinha Docker) |
| Dívida aberta | **D1** (agora pré-requisito da reserva pública, não da Fase 6), **D4**, **D9** (parcial). Pagas: **D2**, **D3**, **D5**, **D6**, **D7**, **D8**, **D10**, **D11** |
| Decisões do dono pendentes | 13, cada uma com o que bloqueia — ver [Decisões pendentes do dono do negócio](#decisões-pendentes-do-dono-do-negócio) |
| Última atualização | 02/10/2026, pelo `squad-lead`, ao fechar a Rodada 5 |
| Repositório | `github.com/juniorsilvarc0/whitehousevillage`. `main` = `957e6e3`; a Rodada 5 está na árvore e é commitada pelo integrador ao fim dela. PR #2 (`ci/actions-node-24`, `c8037be`) aberto, esperando autorização do dono |

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

**Abre com a dívida, não com funcionalidade.** A lista mudou depois de conferida no repositório e no banco, em 31/08. A coluna da direita é o estado em 02/10:

| Dívida | Estado em 31/08 | Onde ficou | Estado em 02/10/2026 |
|---|---|---|---|
| **D3** — códigos de erro fora de `apperr` | aberta (a medida estava invertida: 17 fora, não 20) | F2-03, transversal, em worktree | **paga** na Rodada 5 |
| **D5** — `crm_opportunities.quote_id` | **já paga** em `20260827150000` — a coluna foi removida, conferido em `information_schema` | sobrou só a correção de `docs/db.md` → F2-07 | paga; F2-07 feito em `91d2388` |
| **D6** — `pii_access_log` dentro do módulo de contatos | aberta | F2-01, e é o **primeiro** item da fase | **paga** em `91d2388` |
| **D7** — `quote_validity_days` como constante | aberta | F2-04 (schema) + F2-05 (a cópia coluna a coluna, que é a metade perigosa) | F2-04 em `91d2388`; F2-05 na Rodada 5 → **paga** |
| **D8** — documento sem índice único | **já paga** em `20260827150000` — `contacts_doc_unico_idx` existe, conferido em `pg_indexes` | nada a fazer | paga |
| **D9** — miudezas | aberta. **Uma delas ganhou fase**: a segunda cópia do controle de idempotência | F2-02, porque `POST /finance/payments` seria a **terceira** cópia. O `<Toaster/>` montado em dois lugares entra junto do F2-17. As demais seguem sem fase | **parcial**: idempotência paga em `91d2388`; `golangci-lint` no CI na Rodada 5 |
| **D10** — funil sem tempo real | aberta nesta leitura | F2-06, em paralelo | **paga** em `91d2388` |

Duas dívidas **não listadas** e medidas na abertura entraram no backlog como item de segurança, não de higiene. **As duas foram fechadas na Rodada 5** (abaixo):

- **`reservations.broker_id` não tem foreign key e não é validado.** Medido: `POST /reservations` com um UUID inexistente respondeu `201`; e o corretor `corretor@wh.local`, com escopo `own`, gravou a venda com o `broker_id` de **outro** usuário, também `201`. Não existe tabela `brokers`. Quando a comissão nascer sobre esse campo, ele vira dinheiro que o beneficiário se atribui. → **F2-09** e **F2-13**.
- **A lista de contatos serve CPF completo sem gravar `pii_access_log`.** Medido como corretor: `GET /contacts?per_page=100` devolveu 11 documentos e telefones inteiros, e a contagem de `pii_access_log` ficou em 43; um `GET /contacts/{id}` levou para 44. A trilha de LGPD é contornável por um parâmetro de paginação. → **F2-23**.

### Commits de 31/08 e 02/10 que entraram sem PR

`125d5fd` (abertura da Fase 2), `91d2388` (PII, idempotência, validade no banco, kanban ao vivo), `e0bc08e` (lint zerado) e `957e6e3` (o site no monorepo) foram para a `main` **por push direto**, sem PR e sem a revisão adversarial que `docs/agents.md` e o `squad-lead` exigem. O PR #1 aparece como `merged` em 02/10 14:47 só porque o push levou a `main` até o head dele (`merge_commit_sha` = `head.sha` = `5cc80bb`); o único PR que existe além dele é o #2, ainda aberto. O CI de push rodou uma vez, no topo (`957e6e3`), com os seis jobs verdes — `91d2388` e `e0bc08e` não têm execução própria.

Não houve defeito atribuído a isso até aqui; a verificação de 02/10 (sete leitores sobre `957e6e3`) achou o que esses commits deixaram para trás — `docs/db.md` afirmando que a validade já saía do banco quando o Go ainda usava a constante, e o F2-04 entrando sem o F2-05, que é exatamente o risco R6 do backlog materializado. É o tipo de achado que a revisão antes de entrar existe para pegar. **A Rodada 5 passou por revisão adversarial antes do commit** (a seção seguinte diz o que ela recusou e o que achou).

### Rodada 5 — exposição e defeitos ativos — 02/10/2026

Rodada de correção, aberta pela verificação de 02/10. Seis agentes em pastas disjuntas (`tech-lead`, `db-migrations`, `backend-go` em duas frentes, `next-frontend` no site, `devops`), o `squad-lead` fechando a documentação e o integrador commitando.

**O que entrou, em linguagem de negócio:**

- **O corretor não consegue mais pôr a venda no nome de outro, nem num corretor inventado.** Existe cadastro de corretor (`brokers`); `reservations.broker_id` e `users.broker_id` têm FK (a de `users` é composta, `(broker_id, id) → brokers(id, user_id)`: a conta só aponta para o cadastro que aponta de volta para ela). Em escopo `own`, a escrita só aceita `null` ou o próprio corretor, conferido **no SQL da escrita** além do domínio (`commission.ResolveBroker`); omitir o campo grava o próprio. Trocar o corretor depois de `confirmed` exige escopo `all`.
- **A lista de contatos parou de servir CPF, telefone e e-mail inteiros.** A coleção devolve `***.***.777-35`, `+*********0000`, `f***@gmail.com`, sem `birth_date` e sem `notes`; a ficha devolve cheio e grava `pii_access_log`. Fecharam-se também três portas que a verificação achou ao lado: `PATCH /contacts/{id}` com corpo vazio devolvia a ficha cheia sem rastro; `GET /reservations/{id}/full` servia nome e telefone dos hóspedes sem rastro (agora uma linha `rooming_list` por hóspede); `GET /crm/opportunities/{id}/full` idem (agora `opportunity`). O telefone do lead sai sempre mascarado. E-mail com `*` é recusado (`422`), para que a máscara nunca seja gravada por cima do endereço verdadeiro.
- **A gestão passou a poder mudar a validade do orçamento sem recompilar**, e publicar outra regra da política não a devolve mais para 7 dias. A versão nova da política é **a anterior copiada pelo banco** com o pedido por cima — coluna que nascer amanhã é herdada sem ninguém lembrar de listá-la.
- **O mesmo código de erro chega ao painel sempre com o mesmo status.** Os 37 códigos do contrato vivem num catálogo só.
- **O site público deixou de expor o que não é dele**: o back-office falso em `/admin/` (1143 linhas, sem autenticação, com link no rodapé) e o controle de desconto de 0 a 15% com a alçada interna sumiram; o calendário deixou de mostrar o nome de quem ocupa a data; as fontes deixaram de vir do Google (que recebia o IP de cada visitante); qualquer caminho inexistente responde 404 de verdade.
- **A pré-reserva volta a expirar sozinha no ambiente que o time roda**: o worker saiu do profile `full` e sobe no `make up` e na fumaça do CI.
- **O lint Go entrou no CI**, com a versão fixada num lugar só (`Makefile`, `GOLANGCI_LINT_VERSION`), e a fumaça do site entrou no `make smoke` e num job próprio.

**A medida, refeita pelo `squad-lead` sobre a árvore final (02/10, 19h):**

| | Antes (verificação de 02/10 sobre `957e6e3`) | Depois |
|---|---|---|
| `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...` | 0 (desde `e0bc08e`) | **0** |
| O mesmo, com `--build-tags=integration` | 15 (errcheck 11, staticcheck 3, unused 1) | **6** — todos `defer resp.Body.Close()` em `internal/router/*_integration_test.go` |
| `go test ./... -race -count=1` | 23 pacotes ok | **24 pacotes ok**, 0 FAIL |
| Integração (banco recriado do zero + `make it-suite`, `-p 1 -race`) | 23 pacotes (o `backend-go` mediu `exit=2` duas vezes antes de corrigir `TestUsuarioDevolveBrokerIDGravadoNoBanco` e a colisão de datas de `TestOverbookingEhImpedidoPeloBanco/C`) | **24 pacotes ok, `exit=0`**. Repetida com `-v` (sem `-race`): **701 testes de topo, 978 com subtestes, 0 FAIL, 0 SKIP** — sem `-v` a contagem de SKIP é sempre zero e não prova nada |
| Painel: `pnpm lint`, `tsc --noEmit`, `pnpm test --run` | 340 testes | lint 0, tsc 0, **40 arquivos, 340 testes** |
| Fumaça do site (`node apps/site/e2e/fumaca-site.mjs`, nginx 1.24 local) | não existia; o equivalente feito à mão dava `/admin/` 200 e caminho inventado 200 | **APROVADO**, 17 recursos internos; `/admin`, `/admin/`, `/scripts/admin.js` e `/nao-existe` = **404** |
| `grep -rn '"RESOURCE_IN_USE"' apps/api --include='*.go' \| grep -v _test` | 5 linhas, 4 frases | **1** (`apperr/catalogo.go:51`) |
| `grep -rn validadePadraoEmDias apps/api/internal` | constante viva (`dto_orcamento_salvo.go:22`) | **0** |
| WhatsApp fictício em `apps/site` | 8 ocorrências | **1** (`apps/site/nginx.conf:17`, chega ao HTML por SSI) |
| `pii_access_log` numa listagem de 11 contatos como corretor | 43 → 43, CPF inteiro no corpo | coleção mascarada; ficha, `PATCH`, `/full` da reserva e da oportunidade gravam (testes `TestColecaoDeContatosNaoServeDocumentoCheioComoCorretor`, `TestPatchDeContatoRegistraLeituraERecusaEmailMascarado`, `TestFullDaReservaRegistraCadaHospedeExibido`, `TestFullDaOportunidadeRegistraOContato`) |

Cada guarda nova veio com controle negativo medido pelo agente que a escreveu (remover a linha deixa o teste vermelho): a cópia da política (duas mutações), a validade, a máscara, o registro do `PATCH`, a recusa do `*`, a rooming list, o lead, a guarda do corretor no SQL (as duas escritas), a FK composta de `users`, o catálogo de erros (três mutações), `commission.ResolveBroker` (116, 32 e 12 falhas ao remover cada regra) e a fumaça do site (seis mutantes, todos reprovados). O `squad-lead` **não** refez as mutações — elas exigem editar `.go`; refez os portões e os `grep`.

**O que a revisão recusou ou corrigiu antes de entrar:**

- **A lista de contatos mascarada quebra o "Editar" da própria lista no painel.** `components/contatos/lista.tsx:174` abre o formulário com a linha da coleção, e o formulário salva por `PUT` com a ficha inteira. Com a linha agora mascarada, editar um contato pela lista responde `422` em campos que o operador não tocou (`e-mail mascarado não é aceito`); e num contato sem e-mail, telefone nem documento, o `PUT` sairia sem `notes` e sem `birth_date`, que a coleção não traz mais — **apagaria a anotação**. O telefone mascarado também vira `tel:` morto no painel de leads (`app/leads/painel.tsx:162`). O painel não foi tocado nesta rodada (`git status apps/admin` vazio às 19h). **Não bloqueia o commit da API**, porque a alternativa é a lista continuar servindo CPF sem rastro; vira **D11**, com dono e prazo (abaixo), e é o primeiro item do `next-frontend` na próxima rodada.
- **O `F2-13` foi escrito com "o PATCH de `broker_id` exige escopo `all`", e o contrato desta rodada deixa o `own` editar.** A contradição foi apontada pelo `tech-lead` e **aceita** pela revisão, com a trava que a torna inofensiva: em `own`, só entre `null` e o próprio, só sobre venda que não é de outro corretor, e só em `quote`/`hold`; de `confirmed` em diante, `all`. A comissão nasce no `/confirm`, então antes dele nenhuma troca move dinheiro. O backlog foi corrigido.
- **O `F2-03` pedia `apperr.Definir` público, e entrou `definir` privado.** Aceito: um construtor público é a porta para um módulo voltar a declarar código, que é a dívida que o item paga. A prova do backlog foi reescrita.
- **"0 SKIP" e "0 `LIMPEZA INCOMPLETA`" sem `-v` não são medida.** Dois relatórios da rodada contaram as duas marcas na saída de `make it-suite`, que roda sem `-v` — e sem `-v` o `go test` não imprime `--- SKIP` nem o `t.Logf` de teste que passa. Refeita com `-v`: 0 SKIP (confirmado) e **5** `LIMPEZA INCOMPLETA` (item abaixo).
- **A prova do `F2-05` (`grep -rn validadePadraoEmDias apps/api` devolve zero) é impossível como escrita**: a migration `20260831100000`, já aplicada, cita o nome no comentário (linha 15) e não se edita. A prova passa a ser `apps/api/internal`. Sobram o comentário de `cmd/seed/tarifario.go:147` e o texto de `openapi.yaml:6519-6522` (que repete a prova velha), ambos com dono no relatório.

**O que ficou de fora, com dono:**

- O painel não acompanhou a mudança de formato de `GET /contacts` e do telefone do lead (**D11**), nem expõe `quote_validity_days` no formulário de política.
- 6 apontamentos de `golangci-lint` com a tag `integration`, todos em `internal/router` (dono: `tech-lead`/`qa-testes`). Até zerar, o CI roda o lint **sem** a tag (`GOLANGCI_LINT_TAGS` vazia).
- **A suíte de integração já deixa lixo por causa do rastro novo, e esconde isso.** O `pii_access_log` gravado pelos `/full` não tem cascata, e o cleanup do usuário descartável não o apaga (`internal/router/api_integration_test.go:276`). Medido pelo `squad-lead` com `-v`: `TestReajusteDeTarifaNaoAlcancaVendaJaEmitida` e `TestRemarcarParaMaisBaratoNaoFazDinheiroDoHospedeEvaporar` deixam usuário e perfil de teste no banco a cada execução; e `TestPapelAcimaDoTetoDoAtorEhRecusado` deixa um perfil desde antes da rodada (conferido numa cópia de `957e6e3`). A limpeza só faz `t.Logf`, que sem `-v` não aparece: a saída de `make it-suite` mostra **zero** `LIMPEZA INCOMPLETA` com **cinco** acontecendo — e os relatórios que contaram "0" sem `-v` não contaram nada. Dono: `qa-testes` (testes de `internal/router`), com a guarda junto: limpeza incompleta passa a reprovar.
- Nada que dependa de Docker rodou aqui: `make up`, `make smoke-stack`, `smoke-site-imagem`, o worker em container e o nginx 1.27 da imagem. A prova na imagem vem no primeiro CI depois do commit (jobs `site`, `smoke` e `lint-go`, este nunca rodado num runner).
- O número real do WhatsApp, o texto da pré-reserva do site (promete "data bloqueada por 48h" e grava só na memória do navegador) e o restante da unificação dependem de decisão do dono (abaixo).
- Corretor inativo (`brokers.active=false`) e corretor de outra propriedade continuam aceitos na venda: precisam de decisão de produto e, se entrarem, de FK composta com `property_id`.
- A mesma `Idempotency-Key` reusada com `{}` e depois com `{"broker_id":null}` devolve a resposta guardada em vez de `422`: a impressão do corpo não distingue ausente de nulo. Mexer nela afeta todos os módulos — entra em **D9**.

**A pergunta da tarefa órfã — o que ninguém tinha permissão de escrever:**

- O `README.md` da raiz não tinha dono na tabela de `docs/agents.md` e prometia River, Traefik, backup, `/metrics` e uma pasta `features/**` que não existem. Corrigido nesta rodada pelo `squad-lead`, sob ordem explícita, e **atribuído ao `tech-lead`** em `docs/agents.md` §2, que também deixou de contradizer a própria tabela sobre o dono de `docs/db.md`.
- `config.go:49` exige `JWT_SECRET` em produção também do worker, que não assina token: o compose passou a entregar ao worker um segredo que ele não usa (`backend-go`).
- `docs/infra.md` descreve Traefik, backup diário cifrado, `/metrics` e HSTS que não existem, e diz que o worker usa River (`tech-lead`).
- `.claude/agents/devops.md` listava jobs de CI que não existem (`contract`, `e2e`) e não citava o serviço `site`: corrigido nesta rodada, junto com o aviso de que compose de produção, Traefik e backup automático não existem. `.claude/agents/tech-lead.md` dizia ser "o único agente que aciona os demais" e o dono do roadmap, contra `squad-lead.md` e `docs/agents.md`: corrigido. Não existe `.claude/agents/integrador.md` — fica para a próxima passada em `.claude/agents/**`, com a decisão de criar o agente ou tirar o papel de `docs/agents.md`.
- `docs/agents.md` ainda tem pastas com dois donos (`apps/site/Dockerfile`: `next-frontend` por `apps/site/**` e `devops` por `Dockerfile*` — a definição do `next-frontend` já manda combinar no relatório; `internal/router/rotas_*.go`: o código diz "de quem implementa o módulo", a tabela dá `routes.go` ao `tech-lead`) e pastas sem dono (`cmd/migrate`, `internal/router/router.go` e `saude.go`, `.env.example`). A de `docs/db.md` foi resolvida nesta rodada. O resto é do `tech-lead`, e o papel de `integrador`, que `docs/agents.md` §4.1 cria e o backlog usa como dono, continua sem definição em `.claude/agents/`.

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
Carga (mapa < 300 ms p95), `EXPLAIN ANALYZE` das 10 queries mais quentes, revisão de segurança, restore testado com RTO/RPO medidos, runbook e treinamento.

O **limitador de login e de reset no Redis** (dívida **D1**) estava aqui e **saiu em 02/10/2026**: com o site vendendo ao público, ele é pré-requisito de qualquer rota `/public/*` em produção (passo B0 da unificação), e não pode esperar o go-live do painel.

---

## Dívida técnica assumida

O que está no código de propósito, com fase marcada para sair. Dívida sem dono e sem fase é dívida esquecida.

### D1 — Limitadores de login e de reset vivem na memória do processo → Redis, **antes de qualquer rota `/public` ir para produção** (era Fase 6)

> **Prazo antecipado em 02/10/2026.** Esta entrada, o quadro "Recusado nesta fase" do backlog da Fase 2 e a linha da Fase 6 diziam que o limitador distribuído espera o go-live. A decisão de 02/10 (o site vende ao público, [`unificacao-site-crm.md`](unificacao-site-crm.md)) muda o prazo: toda rota pública é limitada por taxa, e o limitador de uma porta aberta à internet **não pode** ser um mapa por processo. A dívida morre no passo **B0** do plano de unificação, que é pré-requisito de qualquer `/public/*` em produção — seja qual for a ordem entre a unificação e o financeiro (decisão pendente do dono, abaixo). Continua valendo que **com uma instância e só a gestão** o mapa faz o que promete; o que mudou é que a reserva pública não espera a Fase 6. A medida de pronto é a do B0: com duas réplicas no ar, a sexta tentativa de login é recusada também na réplica que não viu as cinco primeiras. Conferido em 02/10: `grep -rni redis apps/api infra .env.example` só acha o comentário de `httpx/middleware.go`; não há serviço Redis no compose.

`httpx.Limitador` (`apps/api/internal/platform/httpx/middleware.go`) é um contador por chave em janela fixa guardado **num mapa do processo**. Não há coluna de bloqueio no banco: **todo** o estado de força bruta é esse mapa — o par e-mail+IP (5 erros em 15 min), o teto global por e-mail (20 em 15 min), a isenção do IP de onde a conta já entrou (7 dias) e o disparo de recuperação de senha (3 em 15 min).

Duas consequências, ambas reais assim que a API tiver mais de uma instância:

- **Com duas réplicas atrás do balanceador, o limite multiplica.** Cada processo tem o próprio mapa e nenhum vê o do outro: o atacante que espalha as tentativas ganha 5 × número de réplicas por janela. O teto por e-mail sofre o mesmo, e é ele quem deveria segurar o ataque distribuído.
- **Todo deploy zera os contadores.** Sobe versão, some a janela inteira — inclusive a isenção do IP conhecido, que é justamente a trava que impede o ataque distribuído de trancar o dono fora da própria conta. Esperar (ou provocar) um deploy vira parte do ataque.

Fica em memória na Fase 0 porque a alternativa hoje seria tabela nova + varredura, e a Fase 0 não tem Redis nem multi-instância: com **uma** instância o mapa faz exatamente o que promete, que é barrar a força bruta oportunista. A dívida vence no dia em que subir a segunda réplica — **antes**, portanto, do go-live.

**Definitivo (Fase 6)**: contador no Redis (`INCR` + `EXPIRE`, chave por par e-mail+IP, por e-mail e por IP de reset), com o limitador em memória como degradação quando o Redis não responde — limitador que cai não pode virar login sem limite nenhum. Enquanto não existir Redis, o `README` de operação diz o que já é verdade: **uma** instância da API.

### D2 — `reservation_pricing` dentro de `reservations` — **PAGA em 26/08/2026**

Quitada pela migration `20260826100000_reservation_pricing`. Conferido no `information_schema`: `reservation_pricing` existe com 12 colunas e `reservations` caiu de 32 para **23**, dentro da regra 9. Fica registrada aqui, e não apagada, porque a decisão de adiar está na tabela abaixo e a dívida quitada é a prova de que o adiamento tinha prazo.

### D3 — Dezessete dos 37 códigos de erro do contrato são declarados fora de `apperr`, e `RESOURCE_IN_USE` existe em cinco lugares → **PAGA em 02/10/2026** (Rodada 5, F2-03)

**Como foi paga.** Os 37 códigos vivem em `internal/platform/apperr/catalogo.go`: uma constante `apperr.Code*` (o literal existe só nela) e um erro base com status e frase padrão por código, registrados por uma função **privada**, `definir`, que derruba o pacote na carga se o mesmo código for registrado duas vezes. `PorCodigo` e `Codigos()` expõem o catálogo; a tradução do `booking.RuleError` tira o status de lá. `tarifario/erros.go`, `inventario/erros.go` e `disponibilidade/erros.go` foram apagados, e as funções locais `erro`, `conflito` e `invalido` sumiram. O backlog pedia `Definir` público; entrou privado de propósito — construtor público é a porta para o módulo voltar a declarar código, que é a dívida.

Medido antes (verificação de 02/10 sobre `957e6e3`): `apperr` com 20 `define(...)`, 17 códigos nascendo fora (16 em módulos e `RATE_NOT_FOUND` em `internal/domain/booking`), `RESOURCE_IN_USE` em 5 lugares com 4 frases, e mais cópias que esta entrada não contava — `CODE_IN_USE` ×3, `COMPOSITION_INCOMPLETE` ×3, `INVALID_STATE_TRANSITION` ×2, `CONTACT_ANONYMIZED` ×2. Medido depois (02/10, 19h): `grep -rn '"RESOURCE_IN_USE"' apps/api --include='*.go' | grep -v _test` devolve **uma** linha (`apperr/catalogo.go:51`); `ls internal/modules/*/erros.go` devolve só `reservas/erros.go`, que monta detalhes sobre erros do catálogo (`apperr.CompositionIncomplete`, `apperr.InvalidStateTransition`, `apperr.DateConflict` para o `23P01`) e não declara código. Nenhum status HTTP mudou: o `backend-go` comparou `(code, status)` de cada símbolo antes e depois, 37 códigos, diff vazio. A garantia de que não volta é `apperr/catalogo_test.go`: os 37 do catálogo são exatamente os do enum; cada código aparece uma vez como literal; nenhum arquivo de `internal/` fora do `apperr` tem literal com cara de código, `apperr.Error{` nem `.Code =`; e todo código carimbado em `internal/domain` existe no catálogo — três controles negativos medidos (a cópia de `tarifario/erros.go` de volta, um `apperr.Error{…}` montado num módulo, um `definir` repetido), os três vermelhos.

Ficou uma mudança de comportamento num caso que hoje não ocorre: código do domínio fora do catálogo saía `422` com esse código e agora sai `500 INTERNAL` — e o teste acima impede que esse caso exista. Nove frases **padrão** mudaram; nenhuma das que chegavam ao cliente, que foram mantidas por `WithMessage` no ponto de uso.

O texto abaixo é a entrada como foi escrita, mantida pela mesma razão da D2.

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

**Atualizado em 02/10/2026.** Como a D1 subiu para o passo B0 da unificação, esta entrada sobe junto — pela regra da linha acima. E ganhou uma segunda porta com o mesmo defeito: se o site passar a falar com a API por um proxy `/api` no nginx de `apps/site` (o caminho que o A2 do plano sugere para não abrir CORS) **sem** `proxy_set_header X-Forwarded-For` e `X-Real-IP`, todo visitante chega à API com o IP do contêiner do site, e o limitador das rotas `/public` vira, outra vez, um contador único para a internet inteira. `httpx.RealIP` (`platform/httpx/middleware.go:82-115`) já consome o cabeçalho quando o peer é rede interna — o que falta é quem está na frente mandá-lo. Conferido em 02/10: `grep -rni x-forwarded-for apps/admin/src` continua em **zero**, e `apps/site/nginx.conf` não tem `location /api`. A sonda de 429 na fumaça, decidida em 31/08, também não existe ainda.

### D5 — `crm_opportunities.quote_id` apontava para `reservations` → **PAGA em 27/08/2026** (`20260827150000`)

A coluna nasceu em `20260827110000_crm.up.sql` quando "orçamento" era uma reserva em estado `quote`; a FK `crm_opportunities_quote_id_fkey` referenciava `reservations(id)`. A tabela `quotes` chegou em `20260827130000` e a FK **não** foi repontada, de propósito: repontar no meio da rodada quebraria o CRM que estava no ar.

Efeito concreto: a coluna virou dado morto. O "orçamento vigente" do CRM é derivado de `quotes.opportunity_id` (`ORDER BY created_at DESC LIMIT 1`), e o `/win` funcionava sem migration pendente — mas o contrato afirmava, no schema `Oportunidade`, que `quote_id` "aponta para `quotes(id)` desde `20260827130000`", e o banco não tinha esse fato. Duas noções de "orçamento vigente" convivendo é como uma delas passa a ser lida por engano.

**Como foi paga.** Pela remoção, não pelo repontamento — a decisão está na tabela abaixo, em 27/08. Repontar manteria uma coluna morta com o alvo certo, e a próxima pessoa suporia que ela significa alguma coisa. Nenhuma linha de Go mudou: não havia um `SET quote_id` em todo o `apps/api`, e quem responde "qual é o orçamento vigente" continua sendo a subconsulta sobre `quotes`, coberta por `quotes_oportunidade_idx`.

Medido antes (banco no ar, antes da migration): a coluna existia, a FK apontava para `reservations(id)`, e a oportunidade ganha pelo `/win` ficava com `quote_id = NULL` — escrita por ninguém, lida por ninguém. Medido depois, conferido em 31/08/2026 no Postgres de desenvolvimento: `select column_name from information_schema.columns where table_name='crm_opportunities' and column_name='quote_id'` devolve **zero linhas**, e a tabela caiu de 24 para **23** colunas. O `down` restaura a coluna **como ela era**, com a FK para `reservations` — um `down` que "melhora" o passado deixa de ser o inverso do `up` e vira uma migration não declarada.

Se um dia o produto quiser "o orçamento **escolhido**" — diferente do último emitido —, a coluna volta apontando para `quotes(id)`, e aí com quem a escreva.

**Fica pendente, e é do `tech-lead`**: `docs/db.md` §7 ainda lista `quote_id?` entre as colunas de `crm_opportunities`, ainda diz "24 colunas" e ainda traz a subseção "`quote_id` — o orçamento vigente **é uma reserva**". Documento de contrato descrevendo schema que não existe é como a coluna volta. Virou o item **F2-07** do backlog da Fase 2.

### D6 — `pii_access_log` e a redação de PII moram dentro do módulo de contatos → **PAGA em 02/10/2026** (`91d2388`, F2-01)

**Como foi paga.** `internal/platform/pii` existe, irmã de `audit`: `pii.Registrar(ctx, exec, entidade, id, motivo)` com falha fechada (não conseguir gravar aborta a leitura) e `pii.Redigir` cobrindo `name`, `email`, `phone_e164`, `doc_number` e `birth_date`. Medido antes: `contatos/pii.go` (67 linhas) e o `semPII` de `contatos/auditoria.go` eram a única implementação, privada do módulo. Medido depois (02/10, 19h): `grep -rn "registrarAcessoPII\|semPII" apps/api/internal/modules` devolve **0**; `contatos/pii.go` não existe. Na Rodada 5 a plataforma ganhou o que a Fase 2 vai precisar: as máscaras do contrato (`pii.MascararDocumento`, `MascararTelefone`, `MascararEmail`), `RegistrarVarios` (uma instrução para N pessoas) e dois motivos novos, `rooming_list` e `opportunity` — e passou a ter **quatro** consumidores fora de contatos (`reservas.Completa`, `crm` `/full`, o telefone do lead e a lista de contatos).

O `91d2388` entrou sem PR e sem revisão (seção da Fase 2), e a mensagem dele não cita a D6; o pagamento foi conferido na verificação de 02/10 e de novo pelo `squad-lead` na Rodada 5. O texto abaixo é a entrada original.

`registrarAcessoPII` (`internal/modules/contatos/pii.go`) e o `semPII` da trilha (`contatos/auditoria.go`) são plataforma disfarçada de módulo: estão ali porque `internal/platform` não era pasta do agente que os escreveu. A Fase 2 traz financeiro, hóspedes de reserva e chat — as três telas que mostram dado pessoal e vão precisar dos dois.

Efeito concreto: a próxima tela que mostrar PII vai importar o módulo de contatos para conseguir registrar o acesso, ou — pior e mais provável — não vai registrar. `audit.Redigir` protege **segredo** (filtro por substring: senha, token, hash) e não protege PII: `name` e `phone_e164` não casam com nada dele.

Por que é aceitável até lá: hoje só contatos serve PII, e ali a obrigação é cumprida e testada (o acesso falha fechado — não conseguir registrar quem leu aborta a leitura). **Definitivo (Fase 2)**: `internal/platform/pii`, irmã de `audit`.

**Item de backlog: F2-01**, o primeiro da Fase 2. E a abertura da fase mediu um agravante que esta entrada não previa: a lista de contatos serve `doc_number` e `phone_e164` **completos** e não grava `pii_access_log` (43 → 43 numa listagem de 11 fichas; 43 → 44 numa leitura de ficha). A decisão escrita no módulo — "a lista não grava: ela é a agenda do dia" — parte de uma premissa falsa, porque a lista serve a ficha inteira. Virou o item **F2-23**.

### D7 — `commercial_policies` não tem `quote_validity_days` → **PAGA em 02/10/2026** (F2-04 em `91d2388`, F2-05 na Rodada 5)

**Como foi paga, e por que levou duas entregas.** O `91d2388` criou a coluna (`20260831100000`, `NOT NULL DEFAULT 7 CHECK > 0`) e o seed — e parou aí. A medida de 02/10 sobre `957e6e3` achou o risco R6 do backlog **materializado**: a coluna existia, nenhum Go a lia (`disponibilidade/service_orcamentos.go:154` seguia com `agora.AddDate(0,0,validadePadraoEmDias)`), o `INSERT` de `PublicarPoliticaComercial` listava 11 colunas sem ela, e toda publicação a devolvia para 7. Pior: `docs/db.md` e a mensagem do commit afirmavam que mudar a validade "deixou de exigir recompilar", o que era falso.

A Rodada 5 fechou a outra metade. `PublicarPoliticaComercial` deixou de listar colunas: a versão nova é a anterior **copiada pelo banco** (`INSERT … SELECT … FROM LATERAL jsonb_populate_record(linha_anterior, pedido || {id, version+1, created_at})`), então coluna que nascer depois é herdada sem ninguém lembrar dela; na primeira publicação, campo omitido fica fora da lista e cai no `DEFAULT`. A política no contrato ganhou `quote_validity_days` (1..365, `booking.QuoteValidityMaxDays`), e a emissão de orçamento chama `booking.QuoteValidUntil` com **a mesma versão** que precificou, congelando o resultado em `quotes.valid_until`. Medido depois (02/10, 19h): `grep -rn validadePadraoEmDias apps/api/internal` devolve **0**. `TestPoliticaNaoPerdeColunaAoRepublicar` fica vermelho com as duas mutações medidas pelo `backend-go` — copiar sobrescrevendo a coluna, e voltar à lista nome a nome sem ela (`quote_validity_days: v1=15 v2=7`) —, e `TestValidadeDoOrcamentoVemDaPoliticaECongelaNaEmissao` fica vermelho com a validade fixa em 7.

Ressalva da cópia pelo banco, escrita no próprio código: coluna nova que seja **identidade ou autoria** da linha (um `created_by`, por exemplo) tem de entrar na lista do que não se herda, senão a versão nova sai assinada pelo autor da anterior. O teste confere o catálogo de colunas e reprova coluna que não conhece, para forçar essa decisão. O texto abaixo é a entrada original.

A validade de 7 dias do orçamento é constante de aplicação (`validadePadraoEmDias`, em `disponibilidade/dto_orcamento_salvo.go`), ao lado de `hold_hours` e `balance_due_days`, que são **dado versionado**. Contraria "toda regra comercial é dado versionado".

Por que é aceitável até lá: `quotes.valid_until` é `NOT NULL` e gravada por quem emite, então o orçamento já congela a sua própria validade — mudar o padrão não reescreve o passado. E há um agravante que impede a correção pela metade: `PublicarPoliticaComercial` (`tarifario/repository_politicas.go`) copia as colunas **nome a nome**, então uma coluna nova nascida sem entrar nessa cópia voltaria ao `DEFAULT` a cada versão publicada, em silêncio. Os dois passos vão juntos.

Conferido em 31/08: `commercial_policies` tem 13 colunas e nenhuma é `quote_validity_days`; a constante é `validadePadraoEmDias = 7` (`disponibilidade/dto_orcamento_salvo.go:22`); e o `INSERT` de `repository_politicas.go:150-160` continua listando coluna por coluna. **Itens de backlog: F2-04** (o schema, `db-migrations`) **e F2-05** (a cópia, `backend-go`), nesta ordem e sem intervalo — o F2-05 exige controle negativo: remover a coluna da lista do `INSERT` tem de deixar o teste vermelho.

### D8 — Deduplicação de contato por documento sem índice único → **PAGA em 27/08/2026** (`20260827150000`)

`contacts_phone_idx` é `UNIQUE` parcial, então o telefone é garantido pelo banco (`23505` → `409 CONTACT_DUPLICATE`, sem `SELECT` antes do `INSERT`). O documento não tem índice único: `contacts_doc_idx` é índice comum, e a proteção é um `pg_advisory_xact_lock(classe, hashtext(propriedade:tipo:numero))` no service.

Efeito concreto: a corrida está fechada **para quem passa por esta API** — controle negativo medido, 5 falhas em 5 execuções sem a trava, verde com ela —, e **aberta** para `psql`, importação de planilha e qualquer outro processo. O contrato promete a garantia; o schema não a tem.

**Como foi paga.** `contacts_doc_unico_idx` existe desde `20260827150000`. Nenhuma linha de Go mudou: `repository.go:traduzir` já esperava o nome, e o `23505` passou a disparar sozinho — a trava virou a redundância barata que estava prevista, nessa ordem, sem janela desprotegida.

Medido antes: dois `INSERT` com o mesmo CPF, `count(*) = 2`. Medido depois: o segundo levanta `contacts_doc_unico_idx`, e pela API o segundo `POST /contacts` responde `409 CONTACT_DUPLICATE` com o `contact_id` de quem já existe.

O índice é sobre `coalesce(doc_type, '')`, e não `doc_type` puro: a API já recusa número sem tipo, mas em índice único NULO é distinto de NULO, e dois documentos iguais **sem tipo** passariam pelo índice feito para impedi-los — justamente a escrita fora da API que motivou a dívida. Continua parcial (`WHERE doc_number IS NOT NULL`), porque anonimizar zera o documento e duas fichas anonimizadas não podem colidir. Os três casos foram medidos.

### D9 — Miudezas com dono; duas ganharam fase na abertura da Fase 2 — **parcial em 02/10/2026**

Cada uma é pequena, todas são reais, e a lista existe para que nenhuma volte a depender de alguém lembrar.

**Estado em 02/10/2026** (conferido pelo `squad-lead` na árvore da Rodada 5):

- **Paga — idempotência em dois lugares** (`91d2388`, F2-02). Antes: `reservas/idempotencia.go` (186 linhas) e `crm/idempotencia.go` (161). Depois: `internal/platform/idempotencia`, e `ls apps/api/internal/modules/*/idempotencia*.go` não acha arquivo nenhum. O controle negativo do ator (`TestChaveNaoVazaEntreAtores`) está no pacote.
- **Paga — `docs/db.md` descrevendo `quote_id`** (`91d2388`, F2-07).
- **Paga em parte — `golangci-lint`.** `e0bc08e` zerou os 11 apontamentos; a Rodada 5 pôs o lint no CI (job `lint-go`, versão fixada em `Makefile:GOLANGCI_LINT_VERSION` = 2.5.0, que o CI lê com `make -s golangci-versao`) e fez `make lint-golangci` achar o binário também em `go env GOBIN` e em `GOPATH/bin`. Medido: **0** apontamentos sem a tag. **Falta**: com `--build-tags=integration` são **6** (eram 15), todos `defer resp.Body.Close()` em `internal/router/*_integration_test.go`; até zerar, o CI roda sem a tag. Dono: quem tiver `internal/router` de teste (`qa-testes`). E o job `lint-go` nunca rodou num runner.
- **Aberto — `<Toaster/>` duas vezes** (`components/crm/avisos.tsx` e `components/contatos/avisos.tsx`, conferido): continua com o F2-17.
- **Aberto — `contacts_doc_idx` redundante**: conferido no banco recriado em 02/10, os dois índices existem. A migration `20261002180000` criou a FK `brokers.contact_id → contacts` mas não alterou `contacts`, então não era "a próxima que encosta".
- **Nova — a impressão do corpo para idempotência não distingue ausente de `null`.** Com `ReservaCriar.BrokerID` virando `Opt`, a mesma `Idempotency-Key` reusada com `{}` e depois com `{"broker_id":null}` devolve a resposta guardada em vez de `422` por corpo divergente. Efeito hoje: nenhum dinheiro (as duas formas gravam o mesmo corretor em escopo `all`, e em `own` o ausente grava o próprio — **então em `own` os dois corpos significam coisas diferentes e recebem a mesma resposta**). Dono: `backend-go`, em `platform/idempotencia`; morre antes do F2-14, que é onde corpo divergente vira pagamento.
- **Nova — o worker loga um `ERROR 42P01` por minuto** entre o `make up` e o `make migrate` (medido pelo `devops` contra banco vazio) e **exige `JWT_SECRET` em produção** sem assinar token (`platform/config/config.go:49`; com `APP_ENV=production` e sem a variável, sai com 1). Erro esperado ensina a ignorar erro. Dono: `backend-go` (`cmd/worker`, `config`).
- As demais (snapshot por fixação no contexto, helpers do painel em `lib/crm`, espelho de códigos do painel, `members` exigindo `inventory:ver`, derivações do painel inicial) seguem como descritas abaixo.

O texto abaixo é a lista como foi escrita em 31/08:

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


### D10 — O funil não tem tempo real, embora o barramento dele já esteja pronto e ligado → **PAGA em 02/10/2026** (`91d2388`, F2-06)

**Como foi paga.** O kanban assina `topics=crm` com o mesmo `useAtualizacaoAoVivo` do mapa (`components/crm/kanban.tsx:175`), e o teste tem a forma que esta entrada exigia: re-renderiza com **identidade nova** de `criarFonte` e cobra uma conexão só (`kanban.test.tsx`). A fumaça ganhou o bloco "funil ao vivo": 30 s em `/app/funil`, reprovando com menos de 1 ou mais de 2 aberturas de `/api/stream`. Medido antes: `grep -rn useAtualizacaoAoVivo apps/admin/src` com **um** consumidor (o mapa). Medido depois (02/10, 19h): **dois** (`mapa-de-ocupacao.tsx:100`, `kanban.tsx:175`). A fumaça com o bloco novo passou no CI de `957e6e3` (job `smoke` verde); não foi refeita na Rodada 5, que não tinha Docker. O texto abaixo é a entrada original.

**Aberta em 31/08/2026 pelo `squad-lead`**, na leitura de abertura da Fase 2. Não foi assumida por ninguém: é dívida que apareceu porque a metade cara foi entregue e a metade barata não.

O plano da Fase 1e prometia o mapa **e** o kanban se movendo sozinhos, e a infraestrutura foi construída para os dois. Medido no stack no ar:

- `GET /api/v1/stream?topics=calendar,crm` responde `event: ready` com `{"topics":["calendar","crm"]}` — o tópico `crm` existe, é servido, e o handshake confere a permissão dele contra a matriz (`crm.opportunities:ver`), separadamente do `calendar`;
- `crm_opportunities` tem **dois** gatilhos publicando em `whv_crm` (`crm_opportunities_notificar`, `crm_opportunities_notificar_mudanca`), e a migration `20260827120000` escreve no comentário, com todas as letras: "`whv_crm` — `crm_opportunities` (**o kanban**)";
- e no painel, `grep -rn useAtualizacaoAoVivo apps/admin/src` devolve **um** consumidor: `components/mapa/mapa-de-ocupacao.tsx`. O kanban só chama `router.refresh()` **depois da ação do próprio usuário** (`components/crm/kanban.tsx:170`).

Efeito concreto: duas pessoas no funil não veem o trabalho uma da outra. Quem arrasta o card vê; quem está com a tela aberta ao lado continua vendo o card na coluna antiga até apertar F5 — e liga para o cliente que o colega acabou de ganhar. Não há corrupção de dado, porque a etapa é gravada no servidor; o que há é a operação decidindo por uma tela velha.

Por que é aceitável até aqui: com uma ou duas pessoas no funil, a janela é pequena e o refresh manual resolve. E o desenho está certo — o problema é uma ponta que não foi ligada, não uma escolha errada.

**Definitivo (Fase 2, item F2-06)**: o kanban assina `topics=crm` com o mesmo `useAtualizacaoAoVivo` que o mapa usa. Entra na Fase 2, e não vira linha adiada, por uma razão de princípio deste squad: **gatilho, canal e RBAC de tópico que ninguém consome são código morto com nome plausível** — ou ganham dono, ou somem. O custo é um componente reusando um hook que já existe, em pasta disjunta de todo o resto do bloco de dívida, então roda em paralelo e não empurra nada.

O teste é a parte que importa, e ele tem forma obrigatória: **re-renderizar com identidade nova da fábrica de conexão**. Foi exatamente essa a forma que os 13 testes do hook de SSE não sabiam falhar — todos passavam uma fábrica estável de módulo, a única que não podia quebrar, enquanto a conexão reabria **1957 vezes em 9 segundos** no build minificado.

### D11 — O painel ainda trata a lista de contatos como ficha cheia → **PAGA em 09/10/2026**

**Aberta em 02/10/2026 pelo `squad-lead`**, na revisão da Rodada 5. Não foi assumida por ninguém: é o efeito colateral de o F2-23 ter mudado o formato de `GET /contacts` numa rodada em que o painel não teve agente.

A API passou a devolver, na coleção, o schema `ContatoNaLista`: documento, telefone e e-mail mascarados, e **sem** as chaves `birth_date` e `notes`. O painel continua tipando essa resposta como `Contato` e usando a linha como se fosse a ficha. Lido no código em 02/10 (`git status apps/admin` vazio):

- `components/contatos/lista.tsx:174` abre o formulário de edição com a **linha da lista** (`modal.abrir(contato)`), e `modal-de-contato.tsx:38-57` preenche e-mail, telefone, documento, nascimento e anotação a partir dela. O salvamento é `PUT` com o formulário inteiro (`app/contatos/acoes.ts:27`). **Efeito**: "Editar" pela lista responde `422` em campos que o operador não tocou — a API recusa e-mail com `*` (`contatos/dto.go:277`) e o telefone mascarado não é E.164. Num contato **sem** e-mail, telefone e documento, nada recusa, e o `PUT` grava `notes` e `birth_date` vazios, porque a lista não os trouxe: **a anotação some**. Este último caminho foi lido, não medido.
- `lista.tsx:132-142` passa a máscara por `formatarTelefone`/`formatarDocumento`, que a devolvem como veio (não casa com E.164 nem com 11 dígitos) — a tela mostra a máscara crua, o que é aceitável, mas por acaso.
- `app/leads/painel.tsx:162` monta `tel:` com o telefone do lead, que agora sai sempre mascarado: o botão de ligar disca `+*********0000`.

**Por que é aceitável até a próxima rodada, e não até depois**: a alternativa era manter a lista servindo 11 CPFs inteiros a qualquer corretor sem gravar rastro, que é um vazamento medido; o defeito do painel é de **escrita recusada** no caso comum, e o caso de perda (contato só com nome e anotação) é estreito. Não é aceitável por mais tempo do que isso, porque o caminho de perda é silencioso.

**Definitivo**: o formulário de edição carrega a ficha (`GET /contacts/{id}`, que grava `pii_access_log` como deve), nunca a linha; `lib/contatos/tipos.ts` ganha `ContatoNaLista`, e o `tsc` passa a recusar o uso da linha como ficha; o lead liga pela ficha do contato, não por `tel:` da lista. **Prova**: teste de componente que abre "Editar" a partir da lista com uma linha mascarada e exige que o formulário só monte depois da ficha chegar — e o controle negativo, com a linha passada direto, vermelho; e a fumaça reprovando se a lista de contatos tiver um `href="tel:` com `*`.

**Paga em 09/10/2026**, como o definitivo pedia: `ContatoNaLista` em `lib/contatos/tipos.ts` (o `tsc` recusa a linha onde se espera a ficha — `TS2739`); "Editar" pela lista lê a ficha por `GET /contacts/{id}` e só monta o formulário quando ela chega; o botão de ligar do lead lê a ficha no clique e disca o E.164 dela; todo `tel:` do painel passa por `lib/contatos/ligacao.ts`, que só aceita E.164; o zod recusa e-mail com `*`; e a fumaça reprova `tel:` ou `wa.me` com `*` em qualquer tela. Prova: `lista.test.tsx` (8) e `leads/painel.test.tsx` (5), com três controles negativos vermelhos — entre eles a perda silenciosa da anotação (`expected '' to be 'Indicado pelo Carlos…'`) e o `tel:+*********0000`.

---

## Decisões pendentes do dono do negócio

Nenhuma delas é técnica, e nenhuma pode ser adivinhada por quem implementa. Cada linha diz **o que fica parado** enquanto ela não vier. As seis primeiras da unificação estão detalhadas em [`unificacao-site-crm.md`](unificacao-site-crm.md) §8.

| # | Decisão | O que bloqueia |
|---|---|---|
| 1 | **Prioridade entre a unificação do site e o financeiro.** Os dois disputam `tech-lead`, `backend-go`, `devops` e `next-frontend`; nenhum documento os põe em ordem | A ordem das próximas rodadas. Sem ela, o Bloco 1 da Fase 2 (F2-08 em diante) e os passos A1/B0 da unificação avançam por quem chegar primeiro. A proposta do `squad-lead` está logo abaixo da tabela |
| 2 | **A reserva pública nasce `hold` ou nasce pedido?** (unificação 8.3) | O B1 inteiro (`POST /public/holds` existe ou vira registro de intenção), a parte de pré-reserva do contrato A1, o formato do B3 e a urgência do B0. **E o texto do site hoje**: o botão "Gerar pré-reserva" diz ao visitante que "a data ficou bloqueada por 48h" e grava só na memória do navegador |
| 3 | **Provedor de pagamento** — Pix, cartão ou os dois, e quem concilia (8.1) | O B2 inteiro; `method` e `external_ref` de `POST /finance/payments` (F2-14); e se a confirmação automática do site passa pelo mesmo `/confirm` que gera os recebíveis (F2-12) |
| 4 | **O público vê o tarifário do ano inteiro?** (8.4) | O desenho de `GET /public/products` e do preço por dia no calendário (A1, A2). O orçamento de uma data (`POST /public/quotes`) não depende dela |
| 5 | **O calendário público pode revelar a ocupação?** (8.5) | O desenho de `GET /public/availability` (janela, granularidade, por unidade ou por produto) e o calendário do A2 |
| 6 | **Domínios** — site e painel juntos ou separados (8.6) | A configuração de produção do A2 (CORS, cookie, Traefik) e a URL pública do webhook do B2. Não bloqueia o desenvolvimento local |
| 7 | **O número real do WhatsApp de reservas** | O fechamento do passo D6 da unificação. Trocar é uma linha (`apps/site/nginx.conf:17`); a fumaça do site avisa que o número atual parece fictício, mas não reprova |
| 8 | **Texto e base legal do consentimento LGPD** do formulário público | O contrato de `POST /public/leads` e da pré-reserva pública (A1) e a parte de consentimento do B3 |
| 9 | **A caução de evento entra no total e na base do sinal?** Hoje `internal/domain/booking/booking.go:186-188` faz `Total = Subtotal − Desconto + Limpeza + Caução`, `Sinal = Total × deposit_pct` e `Saldo = Total − Sinal` | O F2-12 e o F2-15. Escrito à letra, o F2-12 (`saldo = total − sinal` **mais** um recebível `security_deposit`) **cobra a caução duas vezes**: uma dentro do saldo, outra no recebível próprio. E o sinal hoje incide sobre a caução, o que talvez não seja a intenção |
| 10 | **Regra da comissão e quando ela é liberada** — percentual por corretor, por produto ou por faixa; mínimo; base `subtotal − desconto`; liberação na confirmação, no saldo ou no check-out; o que acontece no cancelamento e na remarcação | O F2-08 (os cenários tabelados da função pura), o seed de `commission_rules` (F2-10) e o F2-13 |
| 11 | **Retenção da caução no check-out** — quem autoriza reter parte dela e com que comprovação | O pagável de devolução parcial do F2-15 |
| 12 | **Horários da operação** — check-in, check-out e janela de limpeza | O seed de `agenda_settings` (F2-19) e a agenda que se enche sozinha (F2-20) |
| 13 | **Desconto de 6 a 10% "com aprovação do proprietário"** está escrito em `spec.md:118`, `ui.md:125` e na unificação, e **não existe no código**: `booking.AuthorityOwner` é só um rótulo devolvido em `discount_authority`; o motor recusa apenas acima de 10% (`booking.go`), e a venda com 8% fecha sem ninguém aprovar | Ou vira fluxo (quem aprova, onde fica registrado, o que a reserva guarda) — e entra no backlog com prova —, ou sai dos documentos. Enquanto isso, a jornada 5.3 do PRD (corretor) e o semáforo do `QuoteBuilder` prometem um controle que não há |

Além dessas, duas autorizações que só o dono dá: o **merge do PR #2** (`ci/actions-node-24`, CI verde; o `devops` provou que o `ci.yml` desta rodada mescla com ele sem conflito) e a **remoção das branches remotas já contidas na `main`** (`dividas/quitacao-rodada-4`, `fase-2/bloco-0`).

**Proposta do `squad-lead` para a decisão 1**, para o dono aprovar ou trocar: terminar o **Bloco 1 da Fase 2** (F2-08 → F2-10 → F2-11) antes de abrir a superfície pública, porque o F2-08 é o item mais bloqueante da fase e não depende de nenhuma decisão de produto além da 9 e da 10; e, em paralelo e sem disputa de pasta, o `next-frontend` paga a D11 e o `devops` sobe o Redis do B0 — que é pré-requisito da reserva pública **qualquer que seja** a resposta às decisões 2 a 8. A superfície pública (A1) entra quando a decisão 2 vier.

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
| 20/08/2026 | O job de integração do CI **semeia o banco** e **repete os testes de concorrência** (`-count=10`) | Sem seed, `resources` fica vazio: parte dos testes bate em FK e o do perfil Corretor semeado se **pula** — teste pulado conta como verde. E o defeito das datas é probabilístico: medido nesta rodada, `-count=1` passou verde e `-count=10` reprovou. Uma execução por PR deixava ~67% de chance de a falha atravessar; dez deixam ~2% |
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
| 02/10/2026 | **O site de vendas entra no monorepo como `apps/site`, e reserva pública volta ao escopo** | Dois sistemas descrevendo a mesma casa divergem no dia seguinte: o MVP calculava tarifa, estadia mínima, sinal e alçada em JavaScript, com os números escritos num arquivo, enquanto o motor real já vivia em `internal/domain`. Enquanto o cliente via um preço e o banco guardava outro, a segunda fonte da verdade era justamente a que o cliente lê. Juntar os dois é o único jeito de o site vender o que a casa realmente tem — e cobra o preço escrito em `unificacao-site-crm.md`: pagamento online, limitador distribuído, LGPD na porta e defesa contra negação de inventário |
| 02/10/2026 | **O limitador distribuído (D1) deixa de esperar a Fase 6**: é pré-requisito de qualquer rota `/public/*` em produção (passo B0) | A linha de 31/08 que deixava D1 e D4 na Fase 6 partia de um sistema sem porta pública. Com o site vendendo, toda rota pública é limitada por taxa, e um mapa por processo numa porta aberta à internet é limite nenhum. A linha de 31/08 continua certa no que diz — os dois passos vão juntos —, e por isso a D4 sobe junto |
| 02/10/2026 | **Coleção mascara e não grava; registro individual devolve cheio e grava `pii_access_log`** — inclusive `PATCH /contacts/{id}`, a rooming list do `/full` (uma linha por hóspede) e o `/full` da oportunidade | Era a regra que o F2-23 deixava em aberto entre "mascarar" e "registrar a listagem". Registrar a listagem deixaria a lista exportável como mala direta com um rastro que ninguém lê; mascarar tira o dado de onde ele não serve. É **quebra deliberada de contrato** em `/api/v1` (`docs/api.md` §8 promete não quebrar): o formato da coleção mudou para corrigir um vazamento, e a exceção está escrita na OpenAPI. O e-mail também é mascarado: mascarar só o telefone deixaria a base exportável. E-mail com `*` vira `422`, para a máscara nunca ser gravada por cima do endereço. O preço é a D11 |
| 02/10/2026 | **Em escopo `own`, o corretor pode trocar `broker_id` só entre `null` e ele mesmo, só em `quote`/`hold`, e só em venda que não é de outro corretor**; de `confirmed` em diante, só `all` | O F2-13 dizia "o `PATCH` de `broker_id` exige `all`". O contrato desta rodada abriu a edição ao `own` com essas três travas, e a revisão aceitou: a comissão nasce no `/confirm`, então antes dele nenhuma troca move dinheiro, e "atribuir a mim a venda que é minha" é o uso legítimo que o `all` puro proibiria. A recusa é `403` com `details.reason` (`not_actor_broker`, `replaces_other_broker`, `reservation_confirmed`), e não `422`: dizer "esse corretor não existe" a quem não tem autoridade ensinaria quais ids existem |
| 02/10/2026 | **`users.broker_id` tem FK composta** `(broker_id, id) → brokers(id, user_id)` | Com a FK simples, a conta da gestão podia apontar para o cadastro do corretor e "virar" ele no escopo `own`. A composta só aceita a conta para a qual o cadastro aponta de volta. Controle negativo medido pelo `db-migrations`: trocada pela simples, `TestOutraContaNaoApontaParaOCadastroDoCorretor` fica vermelho |
| 02/10/2026 | **Os códigos de erro são registrados por `definir`, privado, e não por `Definir`, público** | O backlog pedia o público. Um construtor público em `apperr` é a porta para um módulo voltar a declarar código próprio — que é a D3 de novo. O catálogo é fechado; módulo importa a variável |
| 02/10/2026 | **A versão nova da política comercial é a anterior copiada pelo banco** (`jsonb_populate_record`), não um `INSERT` com as colunas listadas | Listar colunas é como `quote_validity_days` virou `DEFAULT` em toda publicação. A cópia herda o que nascer depois sem ninguém lembrar; o que não se herda (`id`, `version`, `created_at`) é a lista curta e explícita |
| 02/10/2026 | **O worker sobe no stack padrão**, sem profile | Job que depende de opt-in é esquecido: com `profiles: ["full"]`, nem `make up` nem a fumaça do CI o ligavam, e nenhuma pré-reserva expirava em ambiente nenhum que o time roda. Medido pelo `devops`: sem schema o worker não cai (loga `42P01` por minuto) e se recupera sozinho depois do `migrate` |
| 02/10/2026 | **O River nunca entrou**; o worker é um loop próprio com um job (`holds.expire`) | A linha de 20/08 escolheu o River e o código não o usou (`grep -i river apps/api/go.mod` = 0). A decisão fica registrada e **sem efeito**: se um job futuro precisar de retry, unicidade e agendamento, o River volta a ser a opção, por decisão escrita, e não por um README que diz que ele já está lá |
| 03/10/2026 | **A vitrine pública é um módulo próprio (`vitrine`) que reusa o motor do painel**, e não um cálculo do site nem um intermediário com credencial de serviço | É a opção (A) do `docs/unificacao-site-crm.md` §5, agora implementada: quatro rotas `/public/*` de leitura, auditáveis pela tabela de rotas. A casa da requisição sem sessão viaja no contexto (`disponibilidade.ComCasaPublica`) para o MESMO `Servico` responder painel e site — dois cálculos são a origem do defeito que a unificação existe para acabar |
| 03/10/2026 | **Toda rota `AcessoPublico` declara `Motivo`**, inclusive sondas e `/auth` | Abrir rota para a internet é a decisão mais cara da tabela; até aqui era a única classificação que não pedia justificativa. `ValidarTabela` derruba o boot sem ela |
| 03/10/2026 | **O calendário público diz livre/ocupado, com preço só no dia livre**; o catálogo público mostra a tabela vigente por tipo de data | Mantém o que o site já mostrava (decisões 8.4 e 8.5 do plano seguem abertas para o dono), sem a contagem de unidades nem o motivo da indisponibilidade, que revelariam a ocupação e a operação |
| 03/10/2026 | **API e worker ganham `restart: unless-stopped` no compose de produção** | A âncora `x-imagem-api` não declarava restart: num reboot da VPS ou num crash, os dois não voltavam sozinhos, enquanto painel, site e banco voltavam. Achado ao pôr a API na rede `borda` para o site alcançá-la |

## Fora de escopo por enquanto

Aplicativo nativo · multi-tenant comercial · emissão fiscal · rodar modelo de IA internamente.

**Duas linhas saíram desta lista em 02/10/2026**: *motor de reserva público com pagamento online* e *substituir o site de marketing*. O site virou `apps/site` dentro deste monorepo e passa a ser o front de cliente. O plano, a ordem, os donos e o que a abertura ao público obriga a consertar antes estão em [`unificacao-site-crm.md`](unificacao-site-crm.md) — em particular a dívida **D1**, que deixa de ser dívida e passa a ser bloqueio (registrado na própria D1 e na linha da Fase 6 em 02/10, na Rodada 5). O `docs/prd.md` §10 marca as duas linhas como revertidas desde a mesma rodada.
