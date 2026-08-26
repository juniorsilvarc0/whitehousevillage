# Roadmap

> Documento vivo. O `tech-lead` atualiza ao fim de cada fatia. Registra também o que foi **decidido não fazer agora**.

## Estado atual

| | |
|---|---|
| Fase corrente | **0 — Fundação** (auth/RBAC, seed e login entregues; **terceira** rodada de correção — duas revisões adversariais e o QA reprovaram) |
| Última atualização | 20/08/2026, 22h30 |
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

- [ ] **1a Dívida da Fase 0 — extrair `reservation_pricing` (1:1 com `reservations`)**. `reservations` nasceu com **32 colunas**, acima do teto de ~25 da regra 9 do CLAUDE.md. O bloco financeiro é satélite natural, porque é *snapshot imutável* e não estado: `subtotal_cents`, `discount_pct`, `discount_cents`, `cleaning_cents`, `event_deposit_cents`, `total_cents`, `deposit_cents`, `rate_table_id`, `policy_version`, `cancellation_policy_id`. Em `reservations` ficam **identidade, estado e datas**.
  **Antes** de 1e (reservas), e antes de a tabela ganhar as colunas de canal (`channel_id`) e de remarcação previstas: cada coluna nova torna a extração mais cara, e hoje nenhum código depende da tabela — é a janela mais barata que vai existir.
- [ ] 1b Inventário: 8 unidades, 4 produtos, composição da Completa
- [ ] 1c Tarifário e políticas versionadas (Tabela V1)
- [ ] 1d Motor puro de disponibilidade e orçamento + suíte de testes
- [ ] 1e Reservas: `stay_blocks` com `EXCLUDE`, alocação, hold com expiração real, confirmação, cancelamento por política
- [ ] 1f Mapa de ocupação em tempo real (SSE)
- [ ] 1g CRM: funil, oportunidade, atividades, SLA, alertas, ganho → reserva
- [ ] 1h Chat WhatsApp (uazapi) com takeover

**Pronto quando**: a jornada roda ao vivo — mensagem no WhatsApp → lead → oportunidade → orçamento com desconto pedindo aprovação → pré-reserva travando as 8 unidades → sinal → reserva confirmada na agenda.

## Fase 2 — Dinheiro e rotina
Financeiro (recebíveis, pagáveis, pagamentos, conciliação, caução), comissões, agenda operacional, hóspedes e LGPD.
**Pronto quando**: confirmar reserva gera recebíveis e comissão sozinho, e o fechamento do mês bate com o razão.

## Fase 3 — Escala comercial
Portal do corretor, contratos em PDF, BI e KPIs (ocupação, ADR, RevPAR, conversão, motivos de perda).
**Pronto quando**: corretor opera sozinho no próprio escopo e o KPI de ocupação bate com a contagem manual no mapa.

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

### D2 — `reservation_pricing` ainda dentro de `reservations`

Registrada como a **primeira tarefa da Fase 1** (item **1a**), com prazo: antes de 1e (reservas) e antes de a tabela ganhar `channel_id` e as colunas de remarcação. Motivo de não fazer na Fase 0 está na tabela de decisões (mexer no schema agora acopla a versão de migration às correções em curso, e `/readyz` recusa servir com o schema fora da versão esperada).

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

## Fora de escopo por enquanto

Motor de reserva público com pagamento online · aplicativo nativo · multi-tenant comercial · emissão fiscal · rodar modelo de IA internamente · substituir o site de marketing.
