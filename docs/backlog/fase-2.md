# Backlog de implantação — Fase 2: Dinheiro e rotina

> Documento do `squad-lead`. Define **quais fatias existem e em que ordem**. Como cada
> fatia é desenhada é do `tech-lead`.
>
> Aberto em 31/08/2026, sobre o repositório em `dividas/quitacao-rodada-4` (PR #1),
> schema `20260827150000`. Tudo que este documento afirma sobre o estado atual foi
> **medido no stack no ar** — API em `localhost:8080`, painel em `localhost:3100`,
> Postgres em `localhost:5433` — e a medida está escrita junto da afirmação.

## O que a fase entrega, em linguagem de negócio

Hoje a casa **vende** e não **cobra**. A Fase 1 fechou o ciclo comercial inteiro:
o lead entra, vira oportunidade, vira orçamento, vira reserva, e a reserva
confirma. Aí o sistema para. Quem tem de receber o sinal, cobrar o saldo sete
dias antes do check-in, devolver a caução no check-out, pagar a comissão do
corretor e fechar o mês faz tudo isso **fora do sistema** — em planilha, no
extrato do banco e na memória de quem atende.

A Fase 2 acaba com essa parte de fora:

- **Confirmar uma reserva passa a produzir dinheiro por conta própria** — o sinal
  quitado, o saldo agendado para a data da política, a caução do evento e a
  comissão do corretor, todos nascendo do mesmo ato, sem ninguém digitar.
- **O dinheiro que entra e o que sai têm um lugar só** — recebível, pagável,
  pagamento e conciliação, num razão que não se edita: erro se estorna, com
  contrapartida.
- **O corretor vê a comissão dele e mais nada** — nem o caixa da casa, nem a
  comissão do colega.
- **A operação sabe o que faz amanhã** — a agenda enche sozinha a partir das
  reservas: check-in, check-out e limpeza.
- **O hóspede vira uma pessoa com direitos** — rooming list por reserva, base
  legal por finalidade, exportação, anonimização que apaga a pessoa e preserva o
  razão. E trilha de quem leu o quê.

## Pronto quando

A fase inteira está pronta quando **as duas frases do roadmap forem executáveis por
comando, sem uma linha de SQL**:

1. `POST /reservations/{id}/confirm` numa reserva com corretor produz, verificável
   por `GET /finance/receivables?reservation_id=` e `GET /finance/commissions?reservation_id=`:
   dois recebíveis (sinal `paid`, saldo `open` com `due_date = check_in − balance_due_days`),
   mais o de caução quando `is_event=true`, e **uma** comissão sobre a base de
   diárias — nunca sobre limpeza nem sobre caução.
2. `GET /finance/summary?month=` fecha contra `ledger_entries` com **diferença zero**,
   provado por um teste de integração que soma os dois lados e falha em qualquer
   centavo de diferença.

E, como em toda entrega deste projeto: `make check` verde, `make test-integration`
verde com `-p 1`, `make smoke-stack` verde com a imagem reconstruída, e o ciclo de
migration provado `up → down -all → up` com zero objeto órfão.

## Como ler um item

| Campo | O que é |
|---|---|
| **Efeito** | O que o usuário do sistema não consegue fazer hoje |
| **Dono** | Um agente de `docs/agents.md`. Nunca dois |
| **Entrada** | O que precisa existir antes |
| **Prova** | Um comando ou uma medida. "Testado manualmente" não é prova |
| **Bloqueia** | Quem fica parado se este item atrasar |
| **Auditoria** | Presente só nos itens que tocam dinheiro ou PII. O que a revisão de segurança confere **neste item** |

---

## A ordem, num quadro

Ordem por **dependência e por risco**, nunca por facilidade. Item mecânico que fica
mais caro depois que a fase encostar nele vem primeiro — é por isso que a fase abre
com sete itens que não entregam tela nenhuma.

| # | Item | Dono | Bloco |
|---|---|---|---|
| F2-01 | Trilha de PII vira plataforma (**D6**) | `backend-go` | Dívida |
| F2-02 | Idempotência vira plataforma (**D9**) | `backend-go` | Dívida |
| F2-03 | Códigos de erro num lugar só (**D3**) | `backend-go` | Dívida |
| F2-04 | `quote_validity_days` no schema (**D7**) | `db-migrations` | Dívida |
| F2-05 | Publicar política não perde coluna (**D7**) | `backend-go` | Dívida |
| F2-06 | Kanban assina o `whv_crm` (**D10**) | `next-frontend` | Dívida |
| F2-07 | `docs/db.md` para de descrever coluna morta | `tech-lead` | Dívida |
| F2-08 | Contrato do financeiro + domínio puro | `tech-lead` | Alicerce |
| F2-09 | `brokers` existe, `broker_id` ganha FK | `db-migrations` | Alicerce |
| F2-10 | Schema do financeiro | `db-migrations` | Alicerce |
| F2-11 | Catálogo e matriz do RBAC financeiro | `db-migrations` | Alicerce |
| F2-12 | Confirmar gera os recebíveis | `backend-go` | Dinheiro |
| F2-13 | Confirmar gera a comissão, e só a dele | `backend-go` | Dinheiro |
| F2-14 | Pagamento e baixa, idempotentes | `backend-go` | Dinheiro |
| F2-15 | Cancelamento e caução geram o pagável | `backend-go` | Dinheiro |
| F2-16 | Fechamento do mês bate com o razão | `backend-go` | Dinheiro |
| F2-17 | Telas de financeiro e comissões | `next-frontend` | Dinheiro |
| F2-18 | Jornada ponta a ponta, sem SQL | `qa-testes` | Dinheiro |
| F2-19 | Schema da agenda | `db-migrations` | Rotina |
| F2-20 | Confirmar enche a agenda | `backend-go` | Rotina |
| F2-21 | Tela da agenda | `next-frontend` | Rotina |
| F2-22 | Rooming list de hóspedes | `backend-go` | Rotina |
| F2-23 | Lista de contatos para de servir CPF sem rastro | `backend-go` | Rotina |
| F2-24 | Anonimizar apaga a pessoa e preserva o razão | `backend-go` | Rotina |

**Vinte e quatro itens. Os sete primeiros são dívida** — a fase abre por ela porque
o roadmap manda e porque cada um deles duplica alguma coisa que o financeiro vai
usar. O que roda em paralelo está no quadro no fim do documento.

---

# Bloco 0 — A dívida abre a fase

O roadmap manda, com todas as letras: **a Fase 2 abre pela dívida, não por
funcionalidade**. O motivo não é higiene, é preço: cada item deste bloco duplica
alguma coisa que o financeiro vai usar, e duplicar uma terceira vez custa mais do
que consertar agora.

**Correção da lista do roadmap, medida antes de escrever este documento:** das
cinco dívidas que o roadmap mandava para cá, **D5 e D8 já estão pagas** — a
migration `20260827150000` removeu `crm_opportunities.quote_id` (conferido:
`information_schema.columns` não devolve a coluna) e criou `contacts_doc_unico_idx`
(conferido: `pg_indexes` devolve o índice). Sobram **D3, D6 e D7**, e a este bloco
se somam duas dívidas que a leitura do código encontrou: a segunda cópia do
controle de idempotência (D9) e o tempo real do funil (**D10**, aberta neste
documento).

---

## F2-01 — A trilha de acesso a dado pessoal sai do módulo de contatos e vira plataforma

Paga **D6**.

- **Efeito**: hoje, a única tela que mostra dado pessoal e registra quem o leu é a
  ficha de contato. A Fase 2 traz três telas novas com PII — recebível com nome do
  hóspede, comissão com nome do corretor, rooming list com CPF e telefone. Como a
  função que registra é privada do módulo de contatos, a próxima tela ou importa o
  módulo errado ou — o que é mais provável e pior — não registra nada, e a
  obrigação da LGPD passa a valer só para a tela onde ela já valia.
- **Dono**: `backend-go`
- **Entrada**: nada. É o primeiro item da fase.
- **Prova**: `internal/platform/pii` existe com `pii.Registrar(ctx, exec, entidade, id, motivo)`
  e `pii.Redigir(campos)`; `grep -rn "registrarAcessoPII" apps/api/internal/modules`
  devolve **zero**; o controle negativo do módulo de contatos continua vermelho ao
  remover a chamada (`go test ./internal/modules/contatos/ -run PII -tags=integration`);
  e um teste novo em `internal/platform/pii` prova que `pii.Redigir` mascara
  `name`, `email`, `phone_e164`, `doc_number` e `birth_date` — **medido hoje**:
  `audit.Sensivel` tem 18 substrings (`senha`, `token`, `hash`, `cvv`…) e **nenhuma
  delas casa com um campo de pessoa**, então `audit.Redigir` publica PII inteira se
  alguém lhe entregar um contato.
- **Bloqueia**: F2-12, F2-13, F2-22, F2-23, F2-24 — todo item da fase que serve PII.
- **Auditoria**: a falha tem de continuar **fechada** — não conseguir gravar o
  acesso aborta a leitura, como já é hoje em `contatos/pii.go`. Um `pii.Registrar`
  que engole o erro e serve a ficha é a versão pior da dívida, não a correção dela.

---

## F2-02 — O controle de idempotência vira plataforma antes de nascer a terceira cópia

Paga metade de **D9**.

- **Efeito**: nenhum, para o usuário, hoje. Amanhã, sim: `POST /finance/payments`
  é literalmente "movimenta dinheiro", e o `Idempotency-Key` nele é obrigatório
  pelo `CLAUDE.md`. Escrito como está, ele nasce como **terceira** cópia da mesma
  lógica — e o dia em que uma das três divergir é o dia em que um pagamento entra
  duas vezes no razão porque o navegador repetiu o `POST`.
- **Dono**: `backend-go`
- **Entrada**: nada.
- **Prova**: `internal/platform/idempotencia` existe; `ls apps/api/internal/modules/*/idempotencia*.go`
  devolve **zero arquivos** (hoje devolve dois: `reservas/idempotencia.go`, 186
  linhas, e `crm/idempotencia.go`, 161 — e o `diff` das assinaturas de função entre
  os dois é **vazio**, ou seja, são a mesma coisa escrita duas vezes); a suíte de
  reservas e a de CRM continuam verdes sem alteração de comportamento
  (`make test-api`).
- **Bloqueia**: F2-14 (pagamento), F2-12, F2-13.
- **Auditoria**: a chave primária de `idempotency_keys` é
  `(key, endpoint, actor_id, property_id)` — **conferida no banco**. O ator faz
  parte da chave de propósito: sem ele, a chave de um usuário responderia com o
  corpo guardado por outro. Ao mover para plataforma, o controle negativo é
  obrigatório: **remova `actor_id` da chave e mostre o teste ficando vermelho.**

---

## F2-03 — Trinta e sete códigos de erro, um lugar só

Paga **D3**.

- **Efeito**: o mesmo `code` chega ao painel com frases diferentes conforme a rota,
  e nada impede que a próxima cópia divirja também no **status HTTP** — aí o painel,
  que reage ao `code`, recebe 409 numa rota e 422 noutra para a mesma situação, e a
  tela decide errado o que oferecer ao operador.
- **Dono**: `backend-go`. **Tarefa transversal: roda sozinha, em worktree, e volta
  como entrega única** — ela toca `internal/platform/apperr` e seis módulos ao mesmo
  tempo, e paralelizar isso com qualquer outra coisa em `internal/modules` é
  garantir conflito.
- **Entrada**: nada.
- **Prova**: `apperr` expõe `Definir(code, mensagem, status)` e declara os **37**
  códigos do enum da OpenAPI; `grep -rn 'RESOURCE_IN_USE' apps/api --include='*.go' | grep -v _test`
  devolve **uma** definição (hoje devolve **cinco**: `apperr/apperr.go:81`,
  `tarifario/erros.go:26`, `crm/crm.go:167`, `contatos/contatos.go:136`,
  `inventario/erros.go:27`, com **quatro** frases distintas); e
  `internal/router/contrato_de_erros_test.go` continua verde nos dois sentidos.
- **Correção da medida do roadmap**: a entrada D3 diz "20 dos 37 códigos são
  declarados fora de `apperr`". Está invertido. **Medido hoje**: `apperr` declara
  **20** dos 37; **17** nascem nos módulos (`RATE_NOT_FOUND`, `POLICY_IMMUTABLE`,
  `CODE_IN_USE`, `INVALID_STATE_TRANSITION`, `RESERVATION_NOT_CANCELLABLE`,
  `UNIT_NOT_AVAILABLE`, `HOLD_LIMIT_REACHED`, `STAGE_NOT_IN_PIPELINE`,
  `STAGE_ORDER_INCOMPLETE`, `DEFAULT_PIPELINE_REQUIRED`, `OPPORTUNITY_ALREADY_CLOSED`,
  `LOSS_REASON_REQUIRED`, `QUOTE_REQUIRED_TO_WIN`, `LEAD_ALREADY_CONVERTED`,
  `CONTACT_DUPLICATE`, `CONTACT_ANONYMIZED`, `QUOTE_NOT_PENDING`). O roadmap foi
  corrigido nesta rodada.
- **Bloqueia**: F2-08 — o financeiro traz códigos novos (`PAYMENT_EXCEEDS_BALANCE`,
  `RECEIVABLE_NOT_OPEN`, `COMMISSION_ALREADY_SETTLED`, o que o contrato decidir), e
  cada um deles nasceria numa sexta cópia se este item não vier antes.

---

## F2-04 — A validade do orçamento vira dado versionado (schema)

Paga metade de **D7**.

- **Efeito**: a gestão não consegue mudar por quanto tempo um orçamento vale. São
  sete dias porque está escrito em Go. Isso contraria a regra do projeto — toda
  regra comercial é dado versionado — e é a única das quatro regras da política
  comercial que não está na tabela. **Medido hoje**: `commercial_policies` tem 13
  colunas (`deposit_pct`, `balance_due_days`, `hold_hours`, `discount_auto_pct`,
  `discount_approval_pct`, `event_deposit_cents`, `hold_extension_hours`,
  `hold_max_extensions` entre elas) e **nenhuma** é `quote_validity_days`; a constante
  é `validadePadraoEmDias = 7`, em `disponibilidade/dto_orcamento_salvo.go:22`.
- **Dono**: `db-migrations`
- **Entrada**: nada.
- **Prova**: `\d commercial_policies` mostra `quote_validity_days integer NOT NULL DEFAULT 7`;
  o seed publica a versão 1 com o valor; `make migrate && make migrate-down && make migrate`
  fecha o ciclo sem objeto órfão.
- **Bloqueia**: F2-05.

---

## F2-05 — Publicar política para de perder coluna nova, e um teste prova

Fecha **D7**. Este item existe separado do F2-04 porque **a metade perigosa é esta**.

- **Efeito**: hoje, `Repository.PublicarPoliticaComercial` monta o `INSERT`
  **listando as colunas nome a nome** (`repository_politicas.go:150-160`, conferido).
  Uma coluna nova que não entre nessa lista volta ao `DEFAULT` a cada publicação —
  **em silêncio**. Traduzido: a gestão mudaria a validade do orçamento para 15 dias,
  publicaria qualquer outra alteração da política depois, e a validade voltaria
  para 7 sem aviso, sem erro e sem linha de auditoria que explique.
- **Dono**: `backend-go`
- **Entrada**: F2-04 aplicada.
- **Prova**: um teste de integração que **publica uma versão com todas as colunas
  fora do default, republica alterando uma só, e exige que as demais sobrevivam** —
  `go test ./internal/modules/tarifario -run PoliticaNaoPerdeColuna -tags=integration`.
  Controle negativo obrigatório: **remova `quote_validity_days` da lista do INSERT e
  mostre o teste ficando vermelho.** Um teste que passa sem a coluna na lista não é
  guarda de nada. E `grep -rn validadePadraoEmDias apps/api` devolve zero.
- **Bloqueia**: ninguém diretamente — mas é o item que impede que **toda** coluna
  criada de F2-09 em diante herde o mesmo silêncio.

---

## F2-06 — O kanban se move sozinho, com o barramento que já existe

Paga **D10**, aberta neste documento.

- **Efeito**: duas pessoas no funil não veem o trabalho uma da outra. Quem arrasta
  um card vê a mudança; quem está com a tela aberta ao lado continua vendo o card na
  coluna antiga até apertar F5 — e liga para o cliente que o colega acabou de
  ganhar. O mapa de ocupação não tem esse problema porque o mapa assina o SSE.
- **Medido hoje, e é o que faz este item barato**: a metade cara já está pronta e
  **não é consumida por ninguém**. `GET /api/v1/stream?topics=calendar,crm` responde
  `event: ready` com `{"topics":["calendar","crm"]}`; `crm_opportunities` tem dois
  gatilhos de notificação (`crm_opportunities_notificar`,
  `crm_opportunities_notificar_mudanca`) publicando em `whv_crm`; a migration
  `20260827120000` diz, no comentário, "`whv_crm` — `crm_opportunities` (**o kanban**)";
  o `/stream` confere permissão **por tópico** contra a matriz. E no painel,
  `grep -rn useAtualizacaoAoVivo apps/admin/src` devolve **um** consumidor:
  `components/mapa/mapa-de-ocupacao.tsx`. O kanban só chama `router.refresh()` depois
  da ação do próprio usuário (`components/crm/kanban.tsx:170`).
- **Por que entra na Fase 2 e não vira linha de roadmap**: um caminho que o sistema
  produz e ninguém consome é exatamente o que a revisão deste squad recusa —
  gatilho, canal e RBAC de tópico que ninguém lê são código morto com nome
  plausível. Ou ganha dono, ou some. E o custo é um componente reusando um hook que
  já existe, em pasta disjunta de todo o resto do bloco.
- **Dono**: `next-frontend`
- **Entrada**: nada. Roda **em paralelo** com F2-01 a F2-05.
- **Prova**: teste de componente que renderiza o kanban, empurra um evento
  `{"topic":"crm","entity":"opportunity",...}` pela fonte injetada e exige o refetch —
  e que **re-renderiza com identidade nova da fábrica de conexão**, porque foi
  exatamente essa a forma que os 13 testes do hook de SSE não sabiam falhar
  (1957 aberturas em 9 s, medidas em produção, com a suíte verde). Mais: `make smoke`
  com o painel aberto em `/app/funil` por 30 s e **≤ 2** aberturas de `EventSource`
  no log da API.
- **Bloqueia**: ninguém. É por isso que ela pode rodar em paralelo — e é por isso
  que ela não pode escorregar para a Fase 3: sem bloqueio, item escorrega.

---

## F2-07 — `docs/db.md` para de descrever o schema que não existe mais

- **Efeito**: quem for implementar o financeiro lê `docs/db.md` para saber onde o
  orçamento mora. **Medido hoje**, `docs/db.md §7` ainda lista `quote_id?` entre as
  colunas de `crm_opportunities`, diz "24 colunas" (o banco tem **23**) e traz uma
  subseção inteira intitulada "`quote_id` — o orçamento vigente **é uma reserva**",
  que era verdade até `20260827130000` e deixou de ser. Um agente que confie nesse
  parágrafo escreve `SET quote_id = ...` e toma `42703` — ou, pior, reintroduz a
  coluna por migration para "consertar" o que o documento promete.
- **Dono**: `tech-lead` (`docs/db.md` é pasta dele)
- **Entrada**: nada. Roda **em paralelo** com todo o Bloco 0.
- **Prova**: `grep -n 'quote_id' docs/db.md` não devolve nenhuma linha que descreva
  `crm_opportunities`; a contagem de colunas do §7 bate com
  `select count(*) from information_schema.columns where table_name='crm_opportunities'`
  (hoje 23); e a §7 passa a apontar `quotes.opportunity_id` como a fonte do
  "orçamento vigente", que é o que o código faz.
- **Bloqueia**: F2-08 — o contrato do financeiro é escrito lendo este documento.

---

# Bloco 1 — O alicerce do dinheiro

Nada deste bloco entrega tela. Ele existe porque **o financeiro só pode nascer
depois que "quem é o corretor" tiver resposta no banco** — e hoje não tem.

---

## F2-08 — Contrato do financeiro e o domínio puro da comissão e do razão

- **Efeito**: sem contrato publicado, `backend-go` e `next-frontend` não avançam em
  paralelo, e é esse paralelismo que faz a fase caber no prazo. Sem domínio puro, a
  regra "comissão incide sobre diária, nunca sobre limpeza nem sobre caução" nasce
  espalhada por três repositórios — que é exatamente como a regra comercial se
  fragmentou no sistema de referência.
- **Dono**: `tech-lead`
- **Entrada**: F2-03 (para não publicar código de erro novo em cópia) e F2-07.
- **O que o contrato precisa **decidir**, e está em aberto hoje**:
  1. **`/finance/payments` e `/finance/reconciliation` não têm célula no RBAC.**
     Medido: o catálogo tem **23** recursos, e os do financeiro são exatamente três —
     `finance.receivables` (`supports_own=false`), `finance.payables`
     (`supports_own=false`) e `finance.commissions` (`supports_own=true`). O
     `docs/api.md §6` lista as duas rotas mesmo assim. Ou elas caem sob um recurso
     existente, ou nasce recurso novo. Rota sem célula não passa no teste de contrato,
     e "resolver na implementação" é como se inventa vocabulário no front que nunca
     casa com `role_permissions`.
  2. **A base da comissão.** `subtotal − desconto`, sem limpeza e sem caução. Vira
     função pura em `internal/domain/money` (ou irmã), com os 20 cenários tabelados
     batendo centavo a centavo, do mesmo jeito que o motor de tarifa.
  3. **A regra de estorno.** O razão é append-only: erro se estorna com
     contrapartida, nunca com `UPDATE`. Isso é decisão de contrato porque muda a
     resposta de `PATCH /finance/payments/{id}` — que provavelmente **não deve
     existir**, e a ausência dele precisa estar escrita, como a ausência de
     `PUT /quotes` está.
- **Prova**: `openapi.yaml` valida no CI; o teste de contrato varre a tabela de rotas
  e não acusa rota sem permissão nem rota fora da OpenAPI; `go test ./internal/domain/...`
  cobre a base da comissão com os cenários tabelados e com teste de propriedade
  (comissão nunca negativa, nunca maior que a base, nunca tocando limpeza).
- **Bloqueia**: F2-09 a F2-18. É o item mais bloqueante da fase inteira.
- **Auditoria**: cada rota nova entra na tabela declarativa com **recurso e ação**, e
  as três de leitura de caixa (`receivables`, `payables`, `reconciliation`) precisam
  de uma frase escrita dizendo o que acontece se o corretor chamar. A resposta certa
  é `403`, e ela é do §1 da spec, não uma opinião nova.

---

## F2-09 — `brokers` existe, e `broker_id` deixa de aceitar qualquer coisa

Este item saiu de uma medida feita hoje e **não estava em nenhuma lista de dívida**.

- **Efeito**: a comissão é do corretor. Hoje o sistema não sabe quem é o corretor.
  **Não existe tabela `brokers`** (conferido: `information_schema.tables` lista 44
  tabelas e nenhuma é `brokers`), e as duas colunas que apontariam para ela —
  `reservations.broker_id` e `users.broker_id` — **não têm foreign key nenhuma**
  (conferido em `pg_constraint`: zero constraints casando `%broker%`).
- **Medido, com a API no ar**:
  - `POST /reservations` com `"broker_id": "e6f8f2c9-…"` — um UUID gerado na hora,
    que não existe em lugar nenhum — respondeu **201**, e a linha `WH-2026-0008`
    ficou no banco carregando esse valor. Zero validação: o campo vai do corpo da
    requisição direto para o `INSERT` (`reservas/repository.go:299`).
  - Pior, e é o que transforma isto em item de segurança: autenticado como
    `corretor@wh.local`, com `reservations:criar` em escopo **`own`**, `POST /reservations`
    com `"broker_id"` = o id de **outro usuário** (`corretor2@wh.local`) respondeu
    **201** e gravou a venda no nome do colega (`WH-2026-0009`). As duas reservas
    foram canceladas depois da medida.
  - Consequência quando o F2-13 entrar: a comissão passa a ser gerada sobre um campo
    que qualquer corretor escreve à vontade — para si ou para terceiro — sem que
    nada no banco recuse. Isso é **saída de caixa autorizada por um documento que o
    próprio sistema emitiu**, que é exatamente a família do defeito que a faixa do
    `deposit_paid_cents` fechou na rodada passada.
- **Dono**: `db-migrations`
- **Entrada**: F2-08 (o contrato decide se `broker_id` referencia `brokers(id)` ou
  `users(id)`; `docs/db.md §10` já prevê `brokers(id, contact_id, user_id?, commission_rule_id?, goal_cents, active)`).
- **Prova**: `\d reservations` mostra a FK; um `INSERT` direto com `broker_id`
  inexistente levanta **`23503`**; e pela API o `POST /reservations` com corretor
  inexistente responde `422 VALIDATION_ERROR` no campo `broker_id`. A prova é no
  **banco**, não no service — garantia de negócio com `SELECT` antes de `INSERT` é
  TOCTOU, e este projeto já pagou cinco portas por isso.
- **Bloqueia**: F2-13, F2-17.
- **Auditoria**: a FK impede o UUID inventado; ela **não** impede o corretor
  atribuir a venda ao id de outro corretor que existe. Essa metade é do F2-13:
  quem tem `reservations:criar` em escopo `own` só pode gravar `broker_id` igual ao
  próprio (`users.broker_id` do ator) — **conferido no SQL**, e com controle
  negativo mostrando o teste vermelho sem a linha. Quem tem escopo `all` grava
  qualquer um, e essa escrita entra em `audit_log`.

---

## F2-10 — O schema do financeiro

- **Efeito**: não há onde guardar um recebível. Medido: `GET /finance/receivables`,
  `/finance/payables`, `/finance/commissions` e `/finance/payments` respondem
  **404** com token de admin válido.
- **Dono**: `db-migrations`
- **Entrada**: F2-08 e F2-09.
- **Prova**: as tabelas de `docs/db.md §10` existem (`receivables`, `payables`,
  `payments`, `commission_rules`, `commissions`, `ledger_entries`, `accounts`,
  `brokers`), **nenhuma passando de 25 colunas** — e a migration falha sozinha se
  passar, com o mesmo bloco `DO` que a migration do CRM usa. Ciclo
  `make migrate → make migrate-down → make migrate` limpo. `make seed` idempotente:
  segunda execução cria zero.
- **Invariantes que **têm** de estar no banco, e não no service** — este é o ponto de
  revisão deste item:
  - `CHECK (paid_cents >= 0 AND paid_cents <= amount_cents)` em `receivables` e
    `payables`. Sem isso, baixar um recebível duas vezes deixa `paid > amount` e o
    fechamento do mês não fecha mais nunca.
  - `CHECK ((receivable_id IS NULL) <> (payable_id IS NULL))` em `payments`: um
    pagamento pertence a exatamente um dos dois lados.
  - `UNIQUE (reservation_id, kind)` parcial em `receivables` para `deposit` e
    `balance`. É esta linha, e não o código do service, que impede que dois
    `POST /confirm` concorrentes gerem quatro recebíveis.
  - `ledger_entries` sem `UPDATE` e sem `DELETE`: revogado por `GRANT`, ou por
    trigger que levanta. Append-only combinado por escrito e garantido por
    disciplina não é append-only.
- **Bloqueia**: F2-12 a F2-17.
- **Auditoria**: dinheiro é `bigint` em centavos, sufixo `_cents`, sem exceção.
  `due_date` é `date` (a data de vencimento é um dia, não um instante); `paid_at` e
  `reconciled_at` são `timestamptz`. Qualquer `numeric` em coluna de valor é recusa
  automática na revisão.

---

## F2-11 — O catálogo de RBAC e a matriz ganham as células do financeiro

- **Efeito**: rota nova sem célula no catálogo é rota que o teste de contrato reprova
  — e, se passasse, seria rota que ninguém consegue conceder pela tela de perfis,
  porque a grade do painel é montada a partir de `GET /roles/resources`.
- **Dono**: `db-migrations` (o catálogo é **dado**, e o seed é pasta dele)
- **Entrada**: F2-08 (quantos recursos, e quais).
- **Prova**: `select code, supports_own from resources` cobre todas as rotas do
  grupo Financeiro e Agenda declaradas na OpenAPI; `make seed` roda duas vezes e a
  segunda cria zero; e o teste de integração que já existe para os três perfis
  continua verde. A matriz do corretor **já está semeada corretamente hoje**
  (conferido: 28 células, entre elas `finance.commissions ver own`, `agenda ver own`,
  `agenda criar own`, `brokers ver own`) — este item acrescenta o que faltar, não
  reescreve o que está certo.
- **Bloqueia**: F2-12 a F2-21.
- **Auditoria**: `finance.receivables` e `finance.payables` continuam com
  `supports_own = false` (conferido hoje). Isso não é detalhe: escopo `own` num
  recurso cuja tabela não tem coluna de dono é **dado corrompido**, e a resposta
  certa é negar, não servir tudo — o módulo de contatos já implementa esse padrão
  (`exigirEscopoAplicavel`) e o financeiro copia. Concedê-lo por engano na tela de
  perfis não pode virar "o corretor viu o caixa da casa".

---

# Bloco 2 — O dinheiro nasce sozinho

---

## F2-12 — Confirmar reserva gera os recebíveis

- **Efeito**: quem confirma uma reserva hoje registra o sinal e pronto. O saldo — que
  vence sete dias antes do check-in — não existe em lugar nenhum do sistema. Quem
  cobra é quem lembrar. **Medido**: `POST /reservations/{id}/confirm` grava
  `deposit_paid_cents` na reserva (`reservas/service_acoes.go:66-89`) e não escreve
  em nenhuma outra tabela.
- **Dono**: `backend-go`
- **Entrada**: F2-01, F2-02, F2-08, F2-10, F2-11.
- **Prova**: `POST /reservations/{id}/confirm` com `Idempotency-Key`, seguido de
  `GET /finance/receivables?reservation_id={id}`, devolve **dois** recebíveis —
  `kind=deposit` com `status=paid` e `paid_cents` igual ao sinal informado, e
  `kind=balance` com `status=open`, `amount_cents = total − sinal` e
  `due_date = check_in − commercial_policies.balance_due_days` **da versão congelada
  na reserva**, não da vigente. Em reserva com `is_event=true`, um terceiro com
  `kind=security_deposit` e `refundable=true`. Teste de integração, sem uma linha
  de SQL na jornada.
- **Prova de concorrência, obrigatória**: dois `POST /confirm` **em transações
  explícitas**, com a mesma `Idempotency-Key` e com chaves diferentes, e o resultado
  é sempre dois recebíveis — nunca quatro. Corrida por HTTP não prova nada aqui: se
  a primeira fecha em 3 ms e a segunda em 10 ms, não houve disputa. A garantia é o
  índice único parcial do F2-10; o teste é o que mostra que ele está atuando.
  **Controle negativo: remova o índice e mostre o teste ficando vermelho.**
- **Bloqueia**: F2-13, F2-14, F2-15, F2-16, F2-17, F2-18.
- **Auditoria**:
  - **Idempotência**: `Idempotency-Key` já é obrigatória no `/confirm` — medido,
    `POST /reservations` sem a chave responde **422**. A reserva do recebível tem de
    acontecer **dentro da mesma transação** do `/confirm`, com a chave reservada
    antes: confirmar e falhar ao gerar o recebível não pode deixar reserva
    confirmada sem dinheiro registrado.
  - **PII em evento**: se o financeiro publicar em `pg_notify`, o payload segue magro
    como os dois canais atuais — hoje o evento carrega `topic`, `entity`, `id`,
    `unit_id`, `pipeline_id`, `property_id`, `v`, e **nada mais** (conferido em
    `platform/realtime/evento.go`). Nome de hóspede, valor e documento no evento é
    recusa automática: o barramento não passa pelo RBAC, o refetch passa.
  - **RBAC**: `POST /confirm` continua em `reservations:editar`. Gerar recebível é
    consequência, não rota — não nasce célula nova para o efeito colateral.

---

## F2-13 — Confirmar reserva gera a comissão do corretor, e só a dele

- **Efeito**: o corretor não sabe quanto vai receber, e a gestão calcula comissão
  em planilha. O §11 da spec promete a ele um painel de comissões previstas e pagas;
  a matriz já concede a célula (`finance.commissions ver own`, conferida no banco) e
  não há o que ler.
- **Dono**: `backend-go`
- **Entrada**: F2-09 (a FK), F2-10 (as tabelas), F2-12 (o recebível, de onde sai a base).
- **Prova**: `GET /finance/commissions?reservation_id={id}` devolve **uma** linha,
  com `base_cents = subtotal − desconto` (sem limpeza, sem caução), `pct` da
  `commission_rules` vigente e `amount_cents` batendo com a função pura do
  `internal/domain`. Reserva **sem** corretor gera zero comissões — e há teste para
  esse caso, porque venda direta é a maioria delas. E: autenticado como
  `corretor@wh.local`, `GET /finance/commissions` devolve `200` com **só** as dele, e
  `GET /finance/receivables` devolve **403** — os dois no mesmo teste, porque é a
  distinção que o §1 da spec registra e que já derrubou uma implementação correta por
  critério mal escrito.
- **Bloqueia**: F2-16, F2-17, F2-18.
- **Auditoria** — este é o item mais sensível da fase:
  - **Escopo `own` no SQL, não na aplicação**: o filtro é `AND c.broker_id = $ator`
    dentro do `WHERE`, como `crm/repository_oportunidades.go:118` já faz para
    oportunidades. Filtrar em memória depois da consulta faz a paginação mentir —
    `meta.total` contando linhas que o usuário não pode ver. **Controle negativo:
    remova a condição e mostre o teste do corretor ficando vermelho.**
  - **Escalada por atribuição**: fecha a metade que a FK do F2-09 não fecha. Quem
    cria reserva em escopo `own` só grava `broker_id` igual ao próprio; tentar
    gravar o de outro é `403 FORBIDDEN`, com teste. Hoje isso responde **201** —
    medido, `WH-2026-0009`.
  - **Escalada por edição**: `PATCH /reservations/{id}` aceita `broker_id`
    (`reservas/dto.go:316`, `Opt[uuid.UUID]`). Trocar o corretor de uma venda
    confirmada **transfere dinheiro entre pessoas**. Exige `reservations:editar` em
    escopo `all`, grava em `audit_log`, e **estorna a comissão anterior emitindo a
    nova** — nunca reescreve a linha, porque o razão é append-only.
  - **PII em log**: nome do corretor e do hóspede não entram em log nem em mensagem
    de erro. `commissions` referencia `broker_id`; a mensagem diz "corretor
    inválido", não "corretor Fulano".

---

## F2-14 — Pagamento e baixa de recebível, idempotentes e sem `UPDATE` no razão

- **Efeito**: não há como registrar que o hóspede pagou. O `paid_cents` do recebível
  nasce do `/confirm` e nunca mais muda.
- **Dono**: `backend-go`
- **Entrada**: F2-02, F2-10, F2-12.
- **Prova**: `POST /finance/payments` com `Idempotency-Key` cria o pagamento, soma em
  `receivables.paid_cents`, escreve **duas** linhas em `ledger_entries` (débito e
  crédito) e move o status para `paid` quando quita ou `partial` quando não. Repetir
  o mesmo `POST` com a mesma chave devolve o **mesmo** `id` e mantém
  `select count(*) from payments` idêntico. Pagar acima do saldo devolve
  `422` com o código que o F2-08 definir, e `paid_cents` não muda.
- **Bloqueia**: F2-15, F2-16, F2-17, F2-18.
- **Auditoria**:
  - **Idempotência**: obrigatória e **provada por corrida**. Duas transações
    explícitas com a mesma chave, e exatamente uma insere. Teste por HTTP em
    sequência não exercita a janela `READ COMMITTED` — que é onde este projeto
    encontrou defeito **todas** as vezes.
  - **A garantia mora no banco**: o teto `paid_cents <= amount_cents` é `CHECK`, não
    `SELECT` antes de `INSERT`. Dois pagamentos simultâneos de metade cada de um
    recebível já quitado não podem passar porque a soma bateu na leitura de ambos.
  - **RBAC**: `finance.payments` (ou a célula que o F2-08 escolher) em `criar`.
    Corretor: `403`, com teste. O corretor **nunca** dá baixa em nada.
  - **Segredo**: `external_ref` (id da transação PIX, NSU do cartão) é referência,
    não credencial — mas não entra em log de aplicação. Chave de gateway, se um dia
    houver, é variável de ambiente, e só.

---

## F2-15 — Cancelamento e check-out produzem o que a casa tem de devolver

- **Efeito**: cancelar uma reserva calcula a retenção pela política congelada e
  **para aí** — o valor a devolver não vira obrigação de ninguém. A caução do evento
  fica no limbo: entra como recebível no F2-12 e não tem caminho de volta.
- **Dono**: `backend-go`
- **Entrada**: F2-10, F2-12, F2-14.
- **Prova**: `POST /reservations/{id}/cancel` com 10 dias de antecedência retém 50%
  do sinal (a faixa da **versão congelada**, não da vigente) e cria
  `payables kind=deposit_refund` com o valor devolvido — `GET /finance/payables?reservation_id=`
  prova. `POST /reservations/{id}/check-out` com a caução aberta cria o pagável de
  devolução, integral ou parcial. E o `?dry_run=1` do cancelamento continua **não
  escrevendo nada**: `select count(*) from payables` antes e depois é igual — hoje
  o `dry_run` já existe e funciona (medido: respondeu com `refund_cents` e
  `dry_run:false` na execução real), e ele não pode ganhar efeito colateral agora.
- **Bloqueia**: F2-16, F2-18.
- **Auditoria**: o valor devolvido sai de `deposit_paid_cents` da reserva, que **já
  tem faixa** (`1 <= pago <= total`, fechada na rodada anterior depois de uma
  devolução de R$ 960.000 numa venda de R$ 1.920). Este item não pode criar um
  segundo caminho de saída de caixa sem a mesma faixa: **todo pagável de devolução
  tem teto no que entrou**, e isso é `CHECK` no banco.

---

## F2-16 — O fechamento do mês bate com o razão, e o teste falha por um centavo

- **Efeito**: o critério de "pronto" da fase inteira. Sem ele, a gestão tem telas
  bonitas e continua conferindo no extrato.
- **Dono**: `backend-go`
- **Entrada**: F2-12 a F2-15.
- **Prova**: `GET /finance/summary?month=2026-11` e a soma de `ledger_entries` do
  período têm **diferença zero**, num teste de integração que monta o mês inteiro
  pela API — confirma reservas, paga, cancela uma, devolve caução, estorna um
  pagamento — e falha se a diferença for de um centavo. Um teste que compara o
  resumo com ele mesmo não é prova; a comparação é contra o razão.
- **Bloqueia**: F2-18. É o portão da fase.
- **Auditoria**: `finance.reconciliation` é da gestão. Corretor: `403`, com teste.

---

## F2-17 — As telas de financeiro e comissões

- **Efeito**: o menu já anuncia "Financeiro" e "Comissões" com o selo
  `emConstrucao: true` (conferido em `config/navigation.ts:177-180`, com o texto
  "Fase 2"). O menu não mente — ele promete. Este item cumpre.
- **Dono**: `next-frontend`
- **Entrada**: F2-08 (o contrato; a tela **não** espera o backend). Roda **em
  paralelo** com F2-12 a F2-16.
- **Prova**: `make smoke-stack` cobre `/app/financeiro` e `/app/comissoes` — e a
  fumaça reprova em **404**, não só em 5xx, que foi exatamente como uma tela
  entregue passou por boa sem existir na imagem servida. O `emConstrucao` sai das
  duas linhas de `navigation.ts` **na mesma mudança** em que as telas entram, e
  `navigation.test.ts` reprova nos dois sentidos. Mais: o painel do corretor mostra
  comissões e **não mostra** o item Financeiro — teste de componente com a matriz
  do corretor.
- **Bloqueia**: ninguém.
- **Auditoria**: o front **esconde**, nunca protege. Toda tela deste item assume que
  a barreira é o middleware, e o teste que importa é o do F2-13 (corretor recebe
  `403` na API), não o de o botão sumir. Valor em centavos formatado na borda; nunca
  `float` no caminho.

---

## F2-18 — A jornada do dinheiro, ponta a ponta, pela API e sem SQL

- **Efeito**: a suíte pode ficar inteira verde com o sistema não servindo — já
  aconteceu neste projeto três vezes, e o `make smoke` virou portão por causa disso.
- **Dono**: `qa-testes`
- **Entrada**: F2-12 a F2-17.
- **Prova**: um teste de integração que percorre, **só por HTTP e com os três
  perfis**: cria contato → emite orçamento → ganha a oportunidade → confirma a
  reserva → confere os dois recebíveis e a comissão → paga o sinal → paga o saldo →
  faz check-in e check-out → devolve a caução → fecha o mês contra o razão. Sem uma
  linha de SQL na jornada; o SQL só aparece nas asserções de invariante que a API
  não expõe. Roda com `-p 1` e com `-count=10` nos trechos de disputa — medido na
  rodada passada: `-count=1` passou verde e `-count=10` reprovou o mesmo defeito.
- **Bloqueia**: o fechamento da fase.

---

# Bloco 3 — A rotina: agenda, hóspedes e LGPD

---

## F2-19 — O schema da agenda

- **Efeito**: `GET /agenda/events` responde **404** (medido, com token de admin). A
  operação não tem onde ver o dia.
- **Dono**: `db-migrations`
- **Entrada**: F2-08.
- **Prova**: `agenda_events`, `agenda_settings` e `agenda_blocks` de `docs/db.md §9`
  existem; ciclo `up → down -all → up` limpo; `make seed` cria o `agenda_settings`
  da propriedade e é idempotente.
- **Bloqueia**: F2-20, F2-21.
- **Auditoria**: `agenda_events.contact_id` é FK, e o evento **guarda o id, não o
  nome** — pelo mesmo motivo que `audit_log` e `pii_access_log` guardam `contact_id`:
  anonimizar a ficha não pode deixar a cópia do dado eliminado numa tabela que meio
  time consegue ler.

---

## F2-20 — Confirmar reserva enche a agenda

- **Efeito**: a limpeza descobre que tem apartamento para virar quando alguém avisa.
  A spec §9 diz "alimentada automaticamente pelas reservas"; hoje não há agenda.
- **Dono**: `backend-go`
- **Entrada**: F2-11, F2-19, F2-12 (o mesmo `/confirm`).
- **Prova**: `POST /reservations/{id}/confirm` seguido de
  `GET /agenda/events?reservation_id={id}` devolve três eventos — `checkin` na data
  de entrada, `checkout` na de saída e `limpeza` no dia do check-out — e repetir o
  `/confirm` com a mesma `Idempotency-Key` continua devolvendo três. Cancelar a
  reserva move os três para `status=cancelado`; **não os apaga**, porque a operação
  precisa saber que havia um check-in ali. Conflito de **horário** é aviso e o
  `POST` responde `201` com `warnings`; conflito de **unidade** é `409` — a spec §9
  separa os dois de propósito, e implementar os dois como bloqueio trava a operação
  em cima da hora.
- **Bloqueia**: F2-21.
- **Auditoria**: `agenda` tem `supports_own = true` no catálogo e o corretor tem
  `agenda ver own` e `agenda criar own` (conferido no banco). `own` aqui é
  `assignee_id = $ator` **no SQL**. Uma visita agendada pelo corretor A não aparece
  para o corretor B — e o teste é da lista **e** do detalhe, porque `NOT_FOUND` fora
  de escopo tem de ser `404`, não `403`: dizer "existe, mas não é seu" já vaza que
  existe.

---

## F2-21 — A tela da agenda

- **Efeito**: o menu anuncia "Agenda" com `emConstrucao: true` e o texto "Fase 2 —
  agenda operacional e visitas do corretor" (conferido em `navigation.ts:175`).
- **Dono**: `next-frontend`
- **Entrada**: F2-08. Roda **em paralelo** com F2-19 e F2-20.
- **Prova**: `make smoke-stack` cobre `/app/agenda` (reprova em 404); as quatro
  visões — dia, semana, mês e lista — têm teste de componente; o helper de fuso é
  **o único do painel** (`lib/tempo-real/`), e não uma segunda cópia dentro de
  `lib/agenda/` — `grep -rn "America/Fortaleza" apps/admin/src` devolve um lugar só.
  Fortaleza mudando de regra tem de ter um arquivo para consertar.
- **Bloqueia**: ninguém.

---

## F2-22 — A rooming list de hóspedes

- **Efeito**: a casa recebe até 24 pessoas num evento e sabe o nome de **uma** — a
  do titular da reserva. Não há como registrar quem efetivamente dormiu ali, que é
  informação que a recepção precisa e que a lei cobra.
- **Dono**: `backend-go`
- **Entrada**: F2-01 (a plataforma de PII), F2-11.
- **Prova**: `POST /reservations/{id}/guests` e `GET /reservations/{id}/guests`
  existem; a tabela **já existe e já é escrita** — `reservation_guests(reservation_id,
  contact_id, is_lead_guest)`, PK composta, 7 linhas no banco de desenvolvimento,
  escrita por `reservas/repository.go:377` — então este item é a superfície que
  falta, não o schema. Prova: acrescentar acompanhante e ler a lista devolve o
  titular com `is_lead_guest=true` e exatamente um titular por reserva (garantido
  por índice único parcial, não por `SELECT` antes do `INSERT`); e
  `select count(*) from pii_access_log` **aumenta** a cada leitura da lista.
- **Bloqueia**: F2-24.
- **Auditoria**: a rooming list é a tela mais densa em PII da fase — nome, CPF e
  telefone de até 24 pessoas numa resposta. Três exigências, todas com teste:
  ler a lista grava `pii_access_log`; a resposta **não** entra em log de aplicação
  nem em `audit_log` (que registra que a lista mudou, nunca de quem para quem); e o
  evento SSE, se houver, carrega `reservation_id` e mais nada.

---

## F2-23 — A lista de contatos para de servir CPF sem deixar rastro

Item saído de uma medida feita hoje. **Não estava em nenhuma lista de dívida.**

- **Efeito**: qualquer corretor lê a base de documentos inteira da casa, e o sistema
  não guarda que ele leu.
- **Medido, com a API no ar, autenticado como `corretor@wh.local`**:
  - `GET /api/v1/contacts?per_page=100` devolveu as **11** fichas com
    `doc_number` e `phone_e164` **completos** no corpo — CPF em texto puro na
    listagem.
  - `select count(*) from pii_access_log` antes: **43**. Depois da listagem: **43**.
    Zero linhas.
  - Um único `GET /api/v1/contacts/{id}` levou a contagem para **44**.
- **Por que isto é um defeito e não uma decisão**: a decisão registrada no módulo é
  legítima e está escrita em `contatos/contatos.go:33` — "a lista não grava: ela é a
  **agenda do dia**; a ficha é dado identificável". O problema é que a premissa é
  falsa: a lista **serve a ficha inteira**. A trilha de LGPD do projeto é contornável
  por um parâmetro de paginação, e a matriz do corretor tem `contacts:ver` em escopo
  `all` (conferido) porque `contacts` não tem coluna de dono.
- **Dono**: `backend-go`
- **Entrada**: F2-01.
- **Prova**: `GET /contacts` devolve `doc_number` mascarado (`***.***.777-35`) e
  telefone com os quatro últimos dígitos; `GET /contacts/{id}` devolve o valor
  cheio e grava `pii_access_log`, como já grava. Teste que repete a medida acima e
  falha se um `doc_number` completo aparecer numa resposta de coleção. Alternativa
  aceitável, se o `tech-lead` preferir: a lista continua completa **e** grava uma
  linha de `reason='list'` com a contagem — mas então o teste é o inverso, e a
  contagem tem de subir. O que **não** é aceitável é a situação de hoje: a resposta
  completa sem rastro nenhum.
- **Bloqueia**: ninguém. Mas é o item que evita que a Fase 2 replique o mesmo buraco
  em três coleções novas — recebíveis com nome, comissões com corretor e rooming list.
- **Auditoria**: é o item de PII da fase. Vale para **toda** coleção nova: se a
  resposta identifica a pessoa, ou ela é mascarada, ou o acesso é registrado. Não há
  terceira opção, e "é só a lista" já foi tentada.

---

## F2-24 — Anonimizar apaga a pessoa e não quebra o razão

- **Efeito**: hoje `POST /contacts/{id}/anonymize` preserva `id` e FKs e apaga a PII —
  e funciona, porque as únicas FKs para `contacts` são de reserva, orçamento e CRM.
  A partir do F2-10, `receivables.contact_id` e `payables.party_id` também apontam
  para lá. Anonimizar um hóspede com recebível aberto não pode nem falhar (a lei
  não espera o boleto) nem apagar o valor a receber.
- **Dono**: `backend-go`
- **Entrada**: F2-10, F2-12, F2-22.
- **Prova**: anonimizar um contato com recebível aberto e comissão paga responde
  `200`; `GET /finance/summary` do mês **não muda um centavo**; `GET /contacts/{id}`
  devolve a ficha sem nome, sem documento e sem telefone; e
  `GET /finance/receivables?contact_id=` continua devolvendo a linha, com o rótulo
  do titular substituído por "titular anonimizado". Teste de integração que compara
  o resumo antes e depois — igualdade exata, não aproximação.
- **Bloqueia**: ninguém. É o último item da fase.
- **Auditoria**: `contacts_doc_unico_idx` é **parcial**
  (`WHERE doc_number IS NOT NULL AND doc_number <> ''`) exatamente para que duas
  fichas anonimizadas não colidam — conferido no banco, e a razão está escrita na
  migration `20260827150000`. Qualquer índice novo sobre PII nasce com a mesma
  ressalva, ou anonimizar o segundo hóspede levanta `23505` e a casa passa a recusar
  cumprir a lei por causa de um índice.

---

# O que roda em paralelo

Paralelismo aqui é **pasta disjunta**, não otimismo. Dois agentes nunca escrevem no
mesmo diretório; quando a tarefa exige, ela vira duas com handoff.

| Fatia | Corre junto de | Por quê é seguro |
|---|---|---|
| **F2-01 → F2-02 → F2-03 → F2-05** | — | Todas em `internal/platform` e `internal/modules`. Mesmo dono (`backend-go`), mesmas pastas: **serializam**. O F2-03 ainda por cima é transversal e roda **em worktree, sozinha** |
| **F2-04** | com F2-01/02/03 | `apps/api/migrations` e `cmd/seed` são do `db-migrations` e não encostam em Go de aplicação |
| **F2-06** | com todo o Bloco 0 | `apps/admin` é do `next-frontend`. Não toca uma linha de `apps/api` |
| **F2-07** | com todo o Bloco 0 | `docs/db.md` é do `tech-lead` |
| **F2-09 + F2-10 + F2-11** | entre si, **em sequência** | Todas em `migrations/` e `cmd/seed`. Mesmo dono: serializam por timestamp |
| **F2-17** | com F2-12 → F2-16 | O painel trabalha **contra o contrato do F2-08**, não contra o backend pronto. É o ganho principal de o contrato vir primeiro |
| **F2-19 → F2-20** ‖ **F2-21** | com o Bloco 2 | Agenda e financeiro são módulos Go distintos — mas o dono é o mesmo `backend-go`, então F2-20 entra depois do F2-16. Só a **tela** (F2-21) é de fato simultânea |
| **F2-23** ‖ **F2-24** | não | Os dois mexem no módulo de contatos. Sequência obrigatória |

**A pergunta da tarefa órfã, antes de fechar qualquer fatia desta fase**: *o que
ninguém tinha permissão de escrever?* Nesta fase os candidatos já são conhecidos e
têm dono nomeado desde já:

- `internal/router/saude.go` (`SchemaVersionEsperada`) — ficou atrás da última
  migration **três vezes**, sinalizada por quatro agentes. A fase cria pelo menos
  quatro migrations. **Dono: `integrador`**, e `TestSchemaVersionEsperadaAcompanhaAUltimaMigration`
  já existe: o que faltava era o CI olhar para ele, e agora olha.
- `internal/router/routes.go` e `config/navigation.ts` — os agentes **propõem a
  linha no relatório**; quem aplica é o `tech-lead`. Rota implementada e não ligada
  é 404 com a suíte verde, e já aconteceu com as oito rotas de contatos.
- `openapi.yaml` quando o ajuste é consequência da implementação — `tech-lead`.
- O `<Toaster/>` do `sonner`, que continua montado em **dois** lugares locais
  (`components/crm/avisos.tsx` e `components/contatos/avisos.tsx`), hoje mutuamente
  exclusivos na árvore. O F2-17 e o F2-21 acrescentam telas com aviso: no dia em que
  duas coexistirem, cada `toast()` aparece em duplicata. **Dono: `next-frontend`,
  dentro do F2-17** — montar na casca e apagar as duas locais **na mesma mudança**.

E a regra que a rodada de 27/08 deixou: **quem escrever "PARA O INTEGRADOR" sobre
algo que já apareceu num relatório anterior propõe a guarda automática junto**, não
só o conserto. Item que só existe em prosa volta.

---

# Auditoria de segurança da fase

Cada item que toca dinheiro ou PII já carrega o campo **Auditoria** acima. Esta
seção é a checagem cruzada: os quatro eixos, e onde cada um é conferido. Fatia que
fecha sem a linha correspondente **não entra**.

## 1. RBAC no servidor, com `own` no SQL

O front esconde; ele nunca protege. Toda rota nova aparece na tabela declarativa com
recurso e ação, e escopo `own` vira `AND coluna_do_dono = $ator` **dentro do
`WHERE`** — filtro em memória depois da consulta faz a paginação mentir.

| Item | O que se confere, especificamente |
|---|---|
| F2-08 | Cada rota do grupo Financeiro e Agenda tem célula. **`/finance/payments` e `/finance/reconciliation` não têm recurso no catálogo hoje** (medido: 23 recursos, três de financeiro). O contrato decide qual, e não a implementação |
| F2-11 | `finance.receivables` e `finance.payables` permanecem `supports_own = false`. Escopo `own` concedido a recurso sem coluna de dono é dado corrompido: a resposta é **negar**, como o módulo de contatos já faz, não servir tudo |
| F2-13 | `AND c.broker_id = $ator` no SQL da listagem de comissões. **Controle negativo: remova a linha e mostre o teste do corretor ficando vermelho.** Guarda cujo teste passa sem ela não é guarda |
| F2-13 | `GET /finance/receivables` como corretor: **403**. `GET /finance/commissions`: **200**, só as dele. No mesmo teste — é a distinção do §1 da spec, e escrever o critério errado já derrubou uma implementação correta |
| F2-14 / F2-16 | Corretor nunca dá baixa e nunca vê conciliação: **403**, com teste |
| F2-20 | `agenda` é `supports_own = true`; `own` é `assignee_id = $ator` no SQL. Fora de escopo, o detalhe responde **404**, não 403 — "existe, mas não é seu" já vaza que existe |
| Todos | O teste de contrato varre a tabela de rotas e reprova rota sem checagem de permissão. Ele já existe; nenhuma rota da fase pode ser exceção declarada |

## 2. Idempotência em movimento financeiro

`Idempotency-Key` é **obrigatória** em todo `POST` que cria reserva ou movimenta
dinheiro. Medido hoje: `POST /reservations` sem a chave responde **422** — o padrão
existe e é o que a fase copia.

| Item | O que se confere |
|---|---|
| F2-02 | A chave de `idempotency_keys` é `(key, endpoint, actor_id, property_id)` — conferida no banco. **Controle negativo: remova `actor_id` e mostre o teste vermelho**, porque sem ele a chave de um usuário devolve o corpo guardado por outro |
| F2-12 | Recebível nasce **na mesma transação** do `/confirm`, com a chave reservada antes. Confirmar e falhar ao gerar o recebível não pode deixar reserva confirmada sem dinheiro registrado |
| F2-12 | Dois `/confirm` concorrentes dão **dois** recebíveis, nunca quatro — e a garantia é o índice único parcial do F2-10, não o código do service |
| F2-14 | Corrida provada com **transações explícitas**, não com dois `curl` em sequência: se a primeira fecha em 3 ms e a segunda em 10 ms, não houve disputa. A janela `READ COMMITTED` é onde este projeto achou defeito **todas** as vezes |
| F2-14 | O teto `paid_cents <= amount_cents` é `CHECK` no banco. `SELECT` antes de `INSERT` para garantir saldo é TOCTOU, e aqui o TOCTOU paga dinheiro duas vezes |
| F2-15 | Todo pagável de devolução tem teto no que entrou, por `CHECK`. É a mesma família do defeito que devolveu R$ 960.000 numa venda de R$ 1.920 |

## 3. PII fora de log, de erro e de evento

CPF, telefone, e-mail e nome não entram em log de aplicação, em mensagem de erro nem
em evento de SSE. Leitura de ficha individual grava `pii_access_log`.

| Item | O que se confere |
|---|---|
| F2-01 | `pii.Redigir` cobre `name`, `email`, `phone_e164`, `doc_number`, `birth_date`. Medido: `audit.Sensivel` tem 18 substrings e **nenhuma** é campo de pessoa — `audit.Redigir` publica PII inteira se lhe entregarem um contato |
| F2-01 | A falha continua **fechada**: não conseguir registrar o acesso aborta a leitura. Um `pii.Registrar` que engole o erro é a versão pior da dívida |
| F2-12 / F2-20 | Evento novo no barramento segue magro. Hoje o payload tem `topic`, `entity`, `id`, `unit_id`, `pipeline_id`, `property_id`, `v` e nada mais — conferido em `platform/realtime/evento.go`. Nome, valor ou documento no evento é **recusa automática**: o barramento não passa pelo RBAC, o refetch passa |
| F2-13 | A mensagem de erro diz "corretor inválido", nunca "corretor Fulano" |
| F2-19 | `agenda_events` guarda `contact_id`, não o nome. Anonimizar não pode deixar cópia da PII numa tabela que meio time lê |
| F2-22 | Rooming list é a resposta mais densa em PII da fase — até 24 pessoas. Ler grava `pii_access_log`; a resposta não vai para log nem para `audit_log` |
| F2-23 | **Medido e aberto hoje**: `GET /contacts?per_page=100` como corretor devolveu 11 CPFs completos e `pii_access_log` **não subiu** (43 → 43); uma leitura de ficha subiu para 44. Vale para toda coleção nova da fase: ou mascara, ou registra |
| F2-24 | Anonimizar apaga a pessoa e preserva o razão — `GET /finance/summary` não muda um centavo |

## 4. Escalada de privilégio

Ninguém concede o que não tem, ninguém edita o próprio papel nem a própria matriz, e
trocar dado que vale dinheiro exige a mesma autoridade que trocar papel.

| Item | O que se confere |
|---|---|
| F2-09 | **Medido e aberto hoje**: `POST /reservations` aceita `broker_id` de UUID inexistente (201, `WH-2026-0008`), e o corretor `corretor@wh.local` gravou a venda com o `broker_id` de **outro** usuário (201, `WH-2026-0009`). Não há FK nem validação. A FK fecha o UUID inventado |
| F2-13 | A FK **não** fecha a outra metade. Quem cria em escopo `own` só grava `broker_id` igual ao próprio; tentar o de outro é **403**, com teste. Sem isso, comissão é dinheiro que o beneficiário se atribui |
| F2-13 | `PATCH /reservations/{id}` aceita `broker_id` (`Opt[uuid.UUID]`, conferido). Trocar o corretor de uma venda confirmada **transfere dinheiro entre pessoas**: exige escopo `all`, grava em `audit_log` e **estorna a comissão anterior emitindo a nova** — nunca `UPDATE`, porque o razão é append-only |
| F2-11 | Recurso novo no catálogo entra também na matriz do perfil raiz (`is_system`), senão ninguém consegue conceder a célula nova — foi essa a correção da verificação final da Fase 0, e ela não afrouxa nada: `POST /roles` grava `is_system=false` sempre |
| Superfície nova | Toda rota, feed ou upload da fase carrega **uma frase escrita** sobre quem pode chamar e o que acontece se um estranho chamar. Sem a frase, a rota não entra |

---

# Riscos da fase

Risco sem mitigação concreta é lamento. Cada linha abaixo tem o que se faz, e quando.

### R1 — A comissão nasce sobre um campo que qualquer corretor escreve

**Já medido, e é o maior risco da fase.** `broker_id` vai do corpo da requisição
direto para o `INSERT`, sem FK e sem validação: o corretor gravou a venda com o id
de outro corretor e recebeu 201. No dia em que o F2-13 entrar, esse campo passa a
valer dinheiro.

**Mitigação**: F2-09 (a FK) entra **antes** de qualquer linha de comissão, e F2-13
não é aceito sem os dois controles negativos — o do escopo `own` na criação e o do
`PATCH` exigindo escopo `all` com estorno. A revisão deste squad recusa a fatia se o
teste do escopo passar com a linha do filtro removida.

### R2 — Dois `/confirm` concorrentes duplicam o dinheiro da casa

A janela `READ COMMITTED` é onde este projeto encontrou defeito **em todas** as
rodadas. Recebível gerado com `SELECT` antes de `INSERT` é o mesmo TOCTOU que custou
cinco portas na invariante da casa inteira, agora com valor em centavos.

**Mitigação**: a garantia é índice único parcial `(reservation_id, kind)` no F2-10,
e a prova é corrida com **transações explícitas** no F2-12, repetida com `-count=10`
no CI — porque, medido na rodada passada, `-count=1` passou verde no mesmo defeito
que `-count=10` reprovou. Corrida por HTTP em sequência não conta como prova.

### R3 — A trilha de LGPD contornável por um parâmetro de paginação se multiplica

Hoje a lista de contatos serve CPF completo sem registrar nada (43 → 43 linhas em
`pii_access_log`, medido). A fase acrescenta **três** coleções que identificam
pessoa: recebíveis com titular, comissões com corretor e rooming list.

**Mitigação**: F2-01 antes de qualquer uma delas, e F2-23 fixando a regra por
escrito — se a resposta identifica a pessoa, ou mascara ou registra. Cada item de
coleção nova só é aceito com o teste que repete a medida: uma resposta de coleção
não pode conter `doc_number` completo sem uma linha nova em `pii_access_log`.

### R4 — O limitador de login derruba a recepção inteira quando a fase dobrar o uso do painel

**D4, medida em 27/08 e conferida hoje**: o limitador conta por IP, e o painel é um
BFF — todos os logins chegam à API com o IP do contêiner `admin`, então o teto de 20
por 15 minutos é **global para a instalação**. Conferido agora: `grep -rn
"X-Forwarded-For" apps/admin/src` devolve **zero**; o painel não encaminha o IP do
navegador. A Fase 2 põe Agenda e Financeiro em uso diário — mais gente, mais turnos,
mais sessões expiradas reabrindo.

**Mitigação, sem puxar a dívida para fora da Fase 6**: a fumaça da fase ganha uma
etapa que **faz login com os quatro usuários do seed em sequência, duas vezes cada**,
e reprova em `429`. Se ela reprovar, D4 sobe para a Fase 2 com dono no `next-frontend`
(encaminhar `X-Forwarded-For`, que `httpx.RealIP` já sabe consumir quando o peer é
rede interna — exatamente o caso). Se não reprovar, a dívida fica na Fase 6, onde está,
e o teste vira a guarda que avisa antes do usuário.

### R5 — O relatório envelhece entre a leitura e o conserto

**Aconteceu nesta rodada**: o roadmap mandava cinco dívidas para a Fase 2, e **duas
já estavam pagas** (D5 e D8); uma terceira (D3) tinha a medida invertida — "20 dos 37
fora de `apperr`" quando são 17. Numa rodada anterior, de cinco itens que uma revisão
mandou para o integrador, dois já estavam corrigidos e um era exagerado.

**Mitigação**: nenhum item desta fase é aberto sem a medida refeita **no dia**. O
agente que receber a tarefa roda o comando do campo **Prova** primeiro, no estado
atual, e reporta o número de partida. Se o comando já passa, a tarefa é fechada com
a medida — não implementada por obediência ao documento.

### R6 — `PublicarPoliticaComercial` engole em silêncio toda coluna nova

A cópia é nome a nome (conferido em `repository_politicas.go:150-160`). Da F2-04 em
diante a fase acrescenta colunas de política; qualquer uma que não entre na lista
volta ao `DEFAULT` a cada publicação, sem erro e sem linha de auditoria.

**Mitigação**: F2-05 entra **imediatamente depois** do F2-04, com o controle negativo
obrigatório — remover a coluna do `INSERT` deixa o teste vermelho. Fatia de política
que entrar antes do F2-05 é recusada na revisão.

### R7 — O paralelismo vira conflito de escrita

A fase tem cinco agentes e quatro migrations. `routes.go`, `navigation.ts` e
`openapi.yaml` são ímãs de conflito.

**Mitigação**: a regra já vale e é para valer — esses três arquivos são do
`tech-lead`; os demais **propõem a linha no relatório** e não editam. Migrations por
timestamp, nunca sequenciais. F2-03 roda **em worktree, sozinha**, por ser a única
transversal. E a pergunta da tarefa órfã é feita antes de fechar cada fatia, com os
candidatos já nomeados na seção anterior.

### R8 — A suíte fica verde e o sistema não serve

Já aconteceu três vezes neste projeto: tela entregue e ausente da imagem, rota de
saúde em 404, container que nunca ficou saudável — tudo com a suíte verde. E os 13
testes do hook de SSE passavam a única forma que não podia falhar, enquanto a
conexão reabria 1957 vezes em 9 segundos em produção.

**Mitigação**: `make smoke-stack` é portão da fase, com a imagem **reconstruída**, e
reprova em 404 e não só em 5xx. As três telas novas (F2-17, F2-21) entram na
varredura na mesma mudança em que perdem o `emConstrucao`. E o teste do F2-06
re-renderiza com **identidade nova** da fábrica de conexão, que é a forma que a
suíte anterior não sabia falhar.

---

# Recusado nesta fase, e por quê

Nada aqui desaparece: o que é recusado vira linha no `roadmap.md`, com efeito e fase.

| Pedido | Decisão | Motivo |
|---|---|---|
| **Chat WhatsApp / uazapi (1g)** | Fora. Não entra no backlog, não é proposto, não é estimado | **Decisão explícita do dono do produto.** Continua sem fase marcada no roadmap: volta quando ele pedir. O painel já deixou de anunciar `/app/chat` como pronto, justamente para não prometer o que não existe |
| **D1 e D4 — limitador no Redis e IP real atrás do BFF** | Ficam na Fase 6 | O roadmap diz, e está certo: contador distribuído com a chave errada distribui o mesmo erro; os dois passos vão juntos. O que a Fase 2 faz é **vigiar** — R4 acima transforma a dívida em teste que avisa antes do usuário |
| **Portal do corretor, contrato em PDF, BI e KPIs** | Fase 3 | O corretor desta fase ganha **a comissão dele na tela** (F2-13, F2-17), que é o §11 da spec. Operar sozinho no próprio escopo é outra fase, e depende de coisas que ainda não existem |
| **Repasse ao proprietário (`owner_payouts`)** | Schema sim, apuração não | `docs/db.md §10` prevê a tabela e o F2-10 a cria com o resto do modelo, para não abrir migration de novo depois. A **apuração por competência com snapshot da regra** fica para quando houver mais de um mês de dados reais para conferir contra — apurar contra base vazia é escrever um relatório que ninguém consegue verificar |
| **Inventário operacional, ordem de manutenção, checklist de limpeza** | Fase 5 | A spec §12 liga a avaria à retenção de caução, e é o único fio que puxa para cá. A Fase 2 fecha esse fio com o laudo **anexado ao pagável de devolução** (F2-15), sem trazer o módulo inteiro |
| **`contacts` ganhar coluna de dono** | Fase 3, com o portal do corretor | O §1 da spec já registra que `contacts:criar` do corretor fica em `all` "até a coluna existir". Criar a coluna agora obriga a decidir o que acontece com as 11 fichas sem dono e com o contato que dois corretores atendem — decisão de produto que ninguém pediu. O que a Fase 2 resolve é o vazamento **de leitura** (F2-23), que é o que dói hoje |
| **`contacts_doc_idx` (índice comum, redundante desde `contacts_doc_unico_idx`)** | Linha no roadmap, sem fase | Conferido: os dois existem. É custo de escrita, não defeito. Vira linha em D9 |
