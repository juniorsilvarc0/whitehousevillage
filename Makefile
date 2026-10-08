SHELL := /bin/bash
COMPOSE := docker compose -f infra/docker-compose.yml --env-file .env

# Banco efêmero do `test-integration`: container e porta próprios, para nunca
# encostar no Postgres de desenvolvimento nem depender do .env do dev.
IT_CONTAINER := whv-it-postgres
IT_PORT      ?= 55432
IT_URL       := postgres://whv:whv@localhost:$(IT_PORT)/whv_integration?sslmode=disable

# Testes de concorrência: QUAIS são e QUANTAS vezes repetem.
# São selecionados por NOME porque Go não tem categoria de teste — quem escrever
# uma disputa nova batiza com uma destas palavras, senão o teste fica de fora da
# repetição (o alvo `it-concorrencia` falha se o regex não casar com nada, para
# que renomear um teste apareça como erro em vez de virar etapa vazia e verde).
TESTES_CONCORRENCIA     ?= Overbooking|Concorren|Simultane|Corrida|Disputa
# 10 repetições: com a intermitência medida (uma execução isolada chegou a passar
# verde com o defeito presente), (2/3)^10 ~ 2% de chance de a falha atravessar.
REPETICOES_CONCORRENCIA ?= 10

# Endereços que a fumaça usa. SMOKE_URL continua sendo o argumento opcional do
# `make smoke` (vazio = o padrão do próprio script); SMOKE_URL_OU_PADRAO é o que
# o `smoke-stack` sonda, porque ele precisa de um endereço concreto.
SMOKE_URL_OU_PADRAO ?= $(if $(SMOKE_URL),$(SMOKE_URL),http://localhost:$(ADMIN_PORT))
# API_HOST_PORT, não API_PORT: a porta do host e a porta que o processo escuta
# dentro do container são variáveis diferentes (ver a nota em infra/docker-compose.yml).
SMOKE_API           ?= http://localhost:$(API_HOST_PORT)
ADMIN_PORT          ?= 3100
API_HOST_PORT       ?= 8080
# Site público. Mesmo padrão do .env.example (SITE_PORT=3200): o Makefile não lê
# o .env, então a porta do host precisa de padrão aqui também.
SITE_PORT           ?= 3200
SITE_URL            ?= http://localhost:$(SITE_PORT)
# `smoke-site-imagem` sobe um container próprio da imagem do site. Porta
# diferente da do stack para rodar com o `make up` de pé sem disputar a 3200.
SITE_IMAGEM         ?= whv-site:fumaca
SITE_IMAGEM_PORT    ?= 3201

# golangci-lint com versão FIXADA. O job `lint-go` do CI instala exatamente esta
# (lê com `make -s golangci-versao`): um lugar só para o número, e o alvo
# `lint-golangci` avisa quando a máquina roda outra. É a desta máquina em
# 02/10/2026 (`golangci-lint --version` = 2.5.0); docs/testing.md fala em v2.1.6.
GOLANGCI_LINT_VERSION := v2.5.0
# Sem teto de repetição. O padrão do golangci-lint esconde apontamentos iguais
# (3 por texto, 50 por linter): no e0bc08e, consertar três `dado :=` revelou
# outros dois. O número que aparece tem de ser o número que existe.
GOLANGCI_LINT_ARGS    := --max-same-issues=0 --max-issues-per-linter=0
# Tags de build do lint. VAZIO por ora, e é um ponto cego conhecido: arquivo com
# `//go:build integration` é invisível ao lint, como era ao vet (ver `vet`).
# Medido em 02/10/2026 com a tag: 15 apontamentos, todos em
# *_integration_test.go (11 errcheck, 3 staticcheck, 1 unused); sem a tag, 0.
# Ligar agora fecharia o `make check` de todo mundo por código de teste. Passa a
# `integration` quando os 15 forem pagos; medir hoje:
#   make lint-golangci GOLANGCI_LINT_TAGS=integration
GOLANGCI_LINT_TAGS    ?=

.PHONY: help up down logs logs-api ps migrate migrate-down migrate-version seed importar-bens smoke smoke-painel smoke-site smoke-site-imagem smoke-stack worker-vivo esperar check lint lint-golangci golangci-versao fmt-check vet test test-api test-admin test-integration it-schema it-seed it-suite it-concorrencia build fmt psql backup restore

help: ## Lista os alvos
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

up: ## Sobe o ambiente de desenvolvimento
	$(COMPOSE) up -d --build

down: ## Derruba o ambiente (preserva volumes)
	$(COMPOSE) down

logs: ## Segue os logs
	$(COMPOSE) logs -f --tail=120

logs-api: ## Segue os logs só da API
	$(COMPOSE) logs -f --tail=200 api

ps: ## Estado dos serviços
	$(COMPOSE) ps

# `--build` nos quatro alvos abaixo, e não só no `up`.
#
# Medido em 27/08/2026: `make up` reconstrói apenas `api` e `admin`, então
# `migrate` e `seed` rodavam a imagem ANTIGA. O sintoma foi silencioso e
# enganador — `make seed` respondeu "previstas 295 ... nada mudou" e deu o banco
# por atualizado, enquanto o seed do commit já semeava 298 linhas; os três
# contatos novos só apareceram depois de reconstruir a imagem à mão. O mesmo
# vale, e é bem pior, para o `migrate`: aplicar schema com o binário de ontem é
# como uma migration entregue "some" de um ambiente sem nenhuma linha vermelha.
# O custo com cache quente é de segundos.
migrate: ## Aplica as migrations (passo explícito — nunca no boot da API)
	$(COMPOSE) run --rm --build migrate up

migrate-down: ## Desfaz a última migration
	$(COMPOSE) run --rm --build migrate down 1

migrate-version: ## Versão aplicada do schema e estado dirty
	$(COMPOSE) run --rm --build migrate version

seed: ## Popula produtos, unidades, tarifas, perfis e usuários de teste
	$(COMPOSE) run --rm --build seed

importar-bens: ## Importa o levantamento fotográfico de bens (UNIDADE=GV-01 opcional; DRY_RUN=1 só imprime o plano)
	$(COMPOSE) run --rm --build importar-bens --dir /levantamento $(if $(UNIDADE),--unidade $(UNIDADE)) $(if $(DRY_RUN),--dry-run)

smoke: ## Fumaça de aplicação: painel (navegador, login, telas) e site público
	# Existe porque a suíte inteira ficou verde enquanto o stack NÃO SUBIA:
	# a imagem do painel morria no boot, o BFF respondia 502 e /crm/pipelines
	# devolvia 500. Teste roda contra código; isto roda contra a aplicação servida.
	#
	# Roda contra o que JÁ ESTÁ no ar. Se o stack não estiver na versão da
	# árvore, o alvo abaixo (`smoke-stack`) é o que sobe tudo antes.
	#
	# O site entrou em 02/10/2026: até ali o /admin/ falso (sem autenticação)
	# e um fallback que respondia 200 para qualquer caminho estavam no ar em :3200
	# sem nenhuma fumaça olhando. As duas rodam SEMPRE, mesmo com a primeira
	# vermelha: uma reprovação não pode esconder a outra. Para só uma delas:
	# `make smoke-painel` ou `make smoke-site`.
	@reprovou=""; \
	$(MAKE) --no-print-directory smoke-painel || reprovou="$$reprovou painel"; \
	$(MAKE) --no-print-directory smoke-site || reprovou="$$reprovou site"; \
	if [ -n "$$reprovou" ]; then echo "==> fumaça REPROVADA em:$$reprovou"; exit 1; fi; \
	echo "==> fumaça aprovada: painel e site"

smoke-painel: ## Fumaça só do painel (navegador, login, telas) contra o que está no ar
	cd apps/admin && node e2e/fumaca.mjs $(SMOKE_URL)

smoke-site: ## Fumaça do site público: 200, 404 real, /admin 404, recursos, hosts externos, WhatsApp
	# Node 22 puro, sem `pnpm install`: o script só usa fetch, fs e path.
	# SITE_EXIGE_API=1: aqui o stack está de pé, então a vitrine (/api/v1/public)
	# TEM de responder pelo site. A fumaça da imagem isolada não liga a variável.
	SITE_EXIGE_API=1 node apps/site/e2e/fumaca-site.mjs $(SITE_URL)

smoke-site-imagem: ## Constrói a imagem do site, sobe um container dela (sem volume) e roda a fumaça
	# Por que existe além do `smoke-site` que o `smoke-stack` já roda: no compose
	# de dev o public/ do repositório é MONTADO por cima do que a imagem copiou.
	# A fumaça do stack prova o nginx.conf dentro do nginx:1.27-alpine, mas não
	# prova o COPY — um .dockerignore que engolisse fonts/ ou 404.html passaria
	# verde lá e iria ao ar quebrado. Aqui roda a imagem crua, como em produção.
	# É o alvo do job `site` do CI.
	@set -uo pipefail; \
	docker build -f apps/site/Dockerfile -t $(SITE_IMAGEM) . || exit 1; \
	docker rm -f whv-site-fumaca >/dev/null 2>&1 || true; \
	docker run -d --name whv-site-fumaca -e TZ=America/Fortaleza \
		-p $(SITE_IMAGEM_PORT):80 $(SITE_IMAGEM) >/dev/null || exit 1; \
	trap 'docker rm -f whv-site-fumaca >/dev/null 2>&1 || true' EXIT; \
	pronto=0; \
	for _ in $$(seq 1 30); do \
		if curl -fsS -o /dev/null "http://localhost:$(SITE_IMAGEM_PORT)/"; then pronto=1; break; fi; \
		sleep 1; \
	done; \
	if [ "$$pronto" != "1" ]; then \
		echo "imagem do site não respondeu em http://localhost:$(SITE_IMAGEM_PORT)/ depois de 30s"; \
		docker logs --tail=60 whv-site-fumaca; exit 1; \
	fi; \
	node apps/site/e2e/fumaca-site.mjs "http://localhost:$(SITE_IMAGEM_PORT)" || { \
		echo "--- log do nginx da imagem ---"; docker logs --tail=60 whv-site-fumaca; exit 1; \
	}

smoke-stack: ## Sobe/atualiza o stack inteiro, migra, semeia e roda a fumaça
	# A ordem importa e é a mesma de um deploy: imagem nova → schema → seed →
	# fumaça. Sem o `up --build` a fumaça mede a imagem de ontem e dá por boa
	# uma tela que a árvore nem contém — foi exatamente o que aconteceu nesta
	# rodada com /app/reservas e /app/contatos, entregues e ausentes da imagem
	# servida em :3100.
	#
	# É este alvo que o job `smoke` do CI executa: o CI não deve carregar uma
	# segunda cópia da sequência (ver a nota do job `integration`).
	#
	# `desde` é marcado depois do seed: o worker loga ERROR a cada minuto
	# ENQUANTO o schema não existe (esperado), e só o que vier depois do schema
	# conta contra ele. Fumaça e worker rodam os dois, mesmo com um vermelho.
	$(MAKE) up
	@$(MAKE) --no-print-directory esperar ALVO="$(SMOKE_API)/healthz" QUEM=api
	$(MAKE) migrate
	$(MAKE) seed
	@$(MAKE) --no-print-directory esperar ALVO="$(SMOKE_URL_OU_PADRAO)/login" QUEM=admin
	@$(MAKE) --no-print-directory esperar ALVO="$(SITE_URL)/" QUEM=site
	@desde=$$(date -u +%Y-%m-%dT%H:%M:%SZ); reprovou=""; \
	$(MAKE) --no-print-directory smoke || reprovou="$$reprovou fumaça"; \
	$(MAKE) --no-print-directory worker-vivo DESDE="$$desde" || reprovou="$$reprovou worker"; \
	if [ -n "$$reprovou" ]; then echo "==> smoke-stack REPROVADO em:$$reprovou"; exit 1; fi

# worker-vivo: o worker está no stack, de pé, sem ter reiniciado, e sem ERROR
# desde DESDE (RFC 3339; vazio pula a leitura do log).
#
# Existe porque o worker passou da fundação até 02/10/2026 fora do stack (num
# profile que nenhum alvo ligava) sem nenhuma linha vermelha: a fumaça olha
# telas, e hold que não expira não aparece em tela nenhuma. RestartCount > 0
# pega o crash loop (env faltando, banco recusando). O ERROR depois do schema
# pega o job que roda e falha todo minuto — uma coluna renomeada por migration,
# por exemplo. Limite honesto: o job roda a cada minuto e não loga sucesso, então
# a leitura do log só cobre os tiques que couberam entre DESDE e agora.
worker-vivo: ## Confere que o worker está no stack, de pé e sem reinício (o smoke-stack chama)
	@set -uo pipefail; \
	id=$$($(COMPOSE) ps -q worker 2>/dev/null); \
	if [ -z "$$id" ]; then \
		echo "worker não está no stack (profile de volta? serviço renomeado?): nenhum hold expira"; \
		exit 1; \
	fi; \
	estado=$$(docker inspect -f '{{.State.Status}} {{.RestartCount}}' "$$id"); \
	if [ "$$estado" != "running 0" ]; then \
		echo "worker em '$$estado' (status reinícios); esperado 'running 0'"; \
		$(COMPOSE) logs --tail=60 worker; exit 1; \
	fi; \
	extra=""; \
	if [ -n "$(DESDE)" ]; then \
		erros=$$($(COMPOSE) logs --no-log-prefix --since "$(DESDE)" worker | grep -c '"level":"ERROR"'); \
		if [ "$$erros" != "0" ]; then \
			echo "worker de pé, mas com $$erros ERROR desde $(DESDE) (depois do schema):"; \
			$(COMPOSE) logs --no-log-prefix --since "$(DESDE)" worker | grep '"level":"ERROR"' | tail -5; \
			exit 1; \
		fi; \
		extra=", nenhum ERROR desde $(DESDE)"; \
	fi; \
	echo "==> worker de pé (running, 0 reinícios$$extra)"

# esperar: sonda ALVO até responder, e despeja o log do serviço QUEM se desistir.
# O log no fracasso é o que separa "a fumaça reprovou" de "a fumaça nem chegou
# a abrir o navegador, e o motivo estava no boot do container".
esperar:
	@pronto=0; \
	for _ in $$(seq 1 60); do \
		if curl -fsS -o /dev/null "$(ALVO)"; then pronto=1; break; fi; \
		sleep 2; \
	done; \
	if [ "$$pronto" != "1" ]; then \
		echo "$(QUEM) não respondeu em $(ALVO) depois de 120s"; \
		$(COMPOSE) logs --tail=60 $(QUEM); \
		exit 1; \
	fi; \
	echo "==> $(QUEM) respondendo em $(ALVO)"

check: lint test ## Lint + typecheck + testes (rode antes de reportar qualquer entrega)

lint: fmt-check vet lint-golangci ## gofmt + vet (com e sem tags) + golangci-lint + eslint + tsc
	cd apps/admin && pnpm lint && pnpm exec tsc --noEmit

# Procura o binário no PATH e, se não achar, em `go env GOBIN` e em cada
# `GOPATH/bin` — é onde `go install` e o script oficial de instalação o põem, e
# esse diretório costuma ficar fora do PATH. Lição do e0bc08e: só com o PATH,
# `make check` reprovava dizendo que a ferramenta não existia, numa máquina onde
# ela estava em ~/go/bin.
#
# Versão diferente da fixada: no CI (CI=true) reprova, porque lá a action instala
# a versão que este Makefile pede e divergir é defeito do job; na máquina do
# desenvolvedor só avisa, porque outra versão conta outros apontamentos, mas
# ainda é lint.
lint-golangci: ## golangci-lint na versão fixada, sem teto de apontamentos (o mesmo comando do CI)
	@set -uo pipefail; \
	bin=$$(command -v golangci-lint 2>/dev/null || true); \
	if [ -z "$$bin" ]; then \
		for dir in $$(go env GOBIN 2>/dev/null) $$(go env GOPATH 2>/dev/null | tr ':' '\n' | sed 's#$$#/bin#'); do \
			if [ -x "$$dir/golangci-lint" ]; then bin="$$dir/golangci-lint"; break; fi; \
		done; \
	fi; \
	if [ -z "$$bin" ]; then \
		echo "golangci-lint não encontrado no PATH, em \`go env GOBIN\` nem em \`go env GOPATH\`/bin:"; \
		echo "\`make check\` não se completa sem ele. Instale a versão do CI ($(GOLANGCI_LINT_VERSION)):"; \
		echo "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b \"\$$(go env GOPATH)/bin\" $(GOLANGCI_LINT_VERSION)"; \
		exit 1; \
	fi; \
	versao=$$("$$bin" --version 2>/dev/null | sed -n 's/.*version v\{0,1\}\([0-9][0-9.]*\).*/\1/p'); \
	if [ "v$$versao" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		if [ "$${CI:-}" = "true" ]; then \
			echo "golangci-lint $${versao:-desconhecida} no CI, mas o Makefile fixa $(GOLANGCI_LINT_VERSION)"; exit 1; \
		fi; \
		echo "AVISO: golangci-lint $${versao:-desconhecida} nesta máquina, $(GOLANGCI_LINT_VERSION) no CI: a contagem de apontamentos pode divergir."; \
	fi; \
	echo "==> $$bin ($$versao) run $(GOLANGCI_LINT_ARGS)$(if $(GOLANGCI_LINT_TAGS), --build-tags=$(GOLANGCI_LINT_TAGS))"; \
	cd apps/api && "$$bin" run $(GOLANGCI_LINT_ARGS) $(if $(GOLANGCI_LINT_TAGS),--build-tags=$(GOLANGCI_LINT_TAGS)) ./...

golangci-versao: ## (interno) Versão fixada do golangci-lint; o job `lint-go` do CI lê daqui
	@echo $(GOLANGCI_LINT_VERSION)

fmt-check: ## Falha se algum .go está fora do gofmt (mesmo comando do CI)
	@cd apps/api && saida=$$(gofmt -l .); \
	if [ -n "$$saida" ]; then echo "fora do gofmt:"; echo "$$saida"; exit 1; fi

# vet roda DUAS vezes, e a segunda é a que importa.
#
# Arquivo com `//go:build integration` é invisível para `go vet ./...`, para
# `go build ./...` e para `go test ./...` — as três coisas que `make check`
# executava. Resultado: uma suíte de integração que nem COMPILA passava por
# `make check` verde, e o erro só aparecia no job `integration` do CI, ou na mão
# de quem tivesse lembrado de digitar a tag. Cada agente desta rodada rodou o
# vet com a tag por conta própria, e isso é a definição de passo que depende de
# alguém lembrar.
vet: ## go vet com e sem a tag integration
	cd apps/api && go vet ./...
	cd apps/api && go vet -tags=integration ./...

test: test-api test-admin ## Todos os testes

test-api: ## Testes Go (inclui o teste de concorrência do overbooking)
	cd apps/api && go test ./... -race -count=1

test-admin: ## Testes do painel
	cd apps/admin && pnpm test --run

test-integration: ## Postgres efêmero + migrations + seed + suíte + concorrência repetida
	@set -uo pipefail; \
	docker rm -f $(IT_CONTAINER) >/dev/null 2>&1 || true; \
	docker run -d --name $(IT_CONTAINER) \
		-e POSTGRES_USER=whv -e POSTGRES_PASSWORD=whv -e POSTGRES_DB=whv_integration \
		-e TZ=America/Fortaleza -p $(IT_PORT):5432 postgres:16-alpine >/dev/null || exit 1; \
	trap 'docker rm -f $(IT_CONTAINER) >/dev/null 2>&1 || true' EXIT; \
	echo "==> aguardando postgres em localhost:$(IT_PORT)"; \
	pronto=0; \
	for _ in $$(seq 1 40); do \
		if docker exec $(IT_CONTAINER) pg_isready -U whv -d whv_integration >/dev/null 2>&1; then pronto=1; break; fi; \
		sleep 1; \
	done; \
	if [ "$$pronto" != "1" ]; then echo "postgres efêmero não respondeu"; exit 1; fi; \
	echo "==> migrations"; \
	DATABASE_URL="$(IT_URL)" $(MAKE) --no-print-directory it-schema || exit 1; \
	echo "==> seed"; \
	DATABASE_URL="$(IT_URL)" $(MAKE) --no-print-directory it-seed || exit 1; \
	echo "==> suíte de integração"; \
	DATABASE_URL="$(IT_URL)" $(MAKE) --no-print-directory it-suite || exit 1; \
	echo "==> concorrência repetida"; \
	DATABASE_URL="$(IT_URL)" $(MAKE) --no-print-directory it-concorrencia

# ── Etapas da integração ────────────────────────────────────────────────────
# As quatro etapas abaixo assumem DATABASE_URL apontando para um banco
# DESCARTÁVEL e existem para que `make test-integration` e o job `integration`
# do CI executem exatamente os mesmos comandos. Comando duplicado entre Makefile
# e workflow é como um passo entra num lado e some do outro sem ninguém notar.

it-schema: ## (interno) Aplica as migrations no DATABASE_URL corrente
	cd apps/api && go run ./cmd/migrate up

it-seed: ## (interno) Semeia o banco de integração com o catálogo de TESTE — sem isso a suíte não roda
	cd apps/api && SEED_CATALOGO=teste go run ./cmd/seed

it-suite: ## (interno) Suíte com a tag integration, uma passada
	# -p 1 serializa os PACOTES: eles compartilham um Postgres só, e em paralelo
	# disputam as mesmas linhas. Medido nesta árvore: 3 falhas em 4 execuções
	# paralelas (login legítimo recebendo 401, token válido recebendo 401) contra
	# 3 execuções serializadas sem nenhuma falha. Era contenção do banco de teste,
	# não defeito de produto — mas intermitência assim é o que faz uma revisão
	# acreditar em verde falso.
	cd apps/api && go test -tags=integration -p 1 ./... -race -count=1

it-concorrencia: ## (interno) Repete os testes de disputa (REPETICOES_CONCORRENCIA vezes)
	@set -uo pipefail; cd apps/api; \
	lista=$$(go test -tags=integration -p 1 ./... -list '$(TESTES_CONCORRENCIA)') || { \
		echo "falha ao listar os testes de concorrência (erro de compilação acima)"; exit 1; \
	}; \
	casados=$$(printf '%s\n' "$$lista" | grep -cE '^Test' || true); \
	if [ "$$casados" -eq 0 ]; then \
		echo "nenhum teste casou com '$(TESTES_CONCORRENCIA)': regex desatualizado ou teste renomeado."; \
		echo "etapa vazia passaria verde sem rodar nada — falhando de propósito."; \
		exit 1; \
	fi; \
	echo "==> $$casados testes de concorrência x $(REPETICOES_CONCORRENCIA) repetições"; \
	go test -tags=integration -p 1 ./... -race -count=$(REPETICOES_CONCORRENCIA) \
		-run '$(TESTES_CONCORRENCIA)' -timeout 15m

fmt: ## Formata
	cd apps/api && gofmt -w . && go mod tidy
	cd apps/admin && pnpm format

build: ## Build de produção das imagens
	$(COMPOSE) build

# psql e backup leem POSTGRES_USER/POSTGRES_DB DENTRO do container postgres (o
# `sh -c` entre aspas simples), onde o compose já as pôs a partir do .env. Antes
# o `$${...}` era expandido no shell do HOST, e o Makefile não lê o .env: numa
# máquina limpa as duas saíam vazias.
psql: ## Console do banco
	$(COMPOSE) exec postgres sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"'

# O backup só é aceito se o pg_dump chegou ao fim. Medido em 02/10/2026 com a
# receita anterior: `pg_dump -U` sem usuário morria com "option requires an
# argument", o gzip comprimia o nada, e `make backup` saía 0 deixando um .gz de
# 20 bytes com 0 bytes de dump — backup vazio com cara de backup. Agora:
# - pipefail: a falha do pg_dump não some atrás do gzip;
# - o fim do dump tem de trazer o marcador que o pg_dump só escreve quando
#   termina. Janela de 10 linhas, não 3: desde o 16.10 o pg_dump fecha com
#   `\unrestrict <chave>` DEPOIS do marcador (medido no 16.14, 4 linhas);
# - grava em `.parcial` e só renomeia depois de verificado: arquivo com nome de
#   backup é backup completo, e uma falha nunca apaga nem ocupa o nome de outro
#   (duas execuções no mesmo segundo geram o mesmo nome).
#
# Fotos e vídeos do site (volume `midia`, docs/site-cms.md §8) vão no MESMO
# backup, com o MESMO carimbo de hora do dump: o banco aponta para ids de
# arquivo, e um dump sem os arquivos daquele momento restaura links quebrados.
# O tar sai por um container descartável com o volume montado SÓ LEITURA (a
# imagem da API é distroless, sem tar); a imagem é a do Postgres, que o stack já
# baixou. Mesmo critério do dump: `.parcial` até `tar -t` ler o arquivo inteiro.
# Volume ainda inexistente (nunca subiu com mídia) não é erro: avisa e segue.
MIDIA_VOLUME := whitehousevillage_midia
backup: ## Dump do banco + tar das fotos/vídeos em infra/backups/ (reprova se algo vier incompleto)
	@set -uo pipefail; \
	mkdir -p infra/backups; \
	carimbo="$$(date +%Y%m%d-%H%M%S)"; \
	arquivo="infra/backups/manual-$$carimbo.sql.gz"; \
	parcial="$$arquivo.$$$$.parcial"; \
	if ! $(COMPOSE) exec -T postgres sh -c 'pg_dump -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"' | gzip > "$$parcial"; then \
		rm -f "$$parcial"; echo "backup FALHOU: o pg_dump não terminou (nada gravado)"; exit 1; \
	fi; \
	if ! gunzip -c "$$parcial" | tail -n 10 | grep -q 'PostgreSQL database dump complete'; then \
		echo "backup INCOMPLETO: sem o marcador de fim do pg_dump; mantido para inspeção em $$parcial"; exit 1; \
	fi; \
	if [ -e "$$arquivo" ]; then \
		echo "backup FALHOU: $$arquivo já existe (outro backup no mesmo segundo); este dump, completo, ficou em $$parcial"; exit 1; \
	fi; \
	mv "$$parcial" "$$arquivo"; \
	echo "==> backup em $$arquivo ($$(du -h "$$arquivo" | cut -f1), dump completo)"; \
	if ! docker volume inspect $(MIDIA_VOLUME) >/dev/null 2>&1; then \
		echo "==> sem volume $(MIDIA_VOLUME) (nenhuma foto/vídeo enviado ainda): só o banco"; exit 0; \
	fi; \
	midia="infra/backups/manual-$$carimbo-midia.tar.gz"; \
	parcial="$$midia.$$$$.parcial"; \
	if ! docker run --rm --network none -v $(MIDIA_VOLUME):/midia:ro postgres:16-alpine tar -C /midia -czf - . > "$$parcial"; then \
		rm -f "$$parcial"; echo "backup da MÍDIA FALHOU: o tar não terminou (o dump do banco ficou em $$arquivo)"; exit 1; \
	fi; \
	if ! tar -tzf "$$parcial" >/dev/null; then \
		echo "backup da MÍDIA ILEGÍVEL: mantido para inspeção em $$parcial"; exit 1; \
	fi; \
	mv "$$parcial" "$$midia"; \
	echo "==> mídia em $$midia ($$(du -h "$$midia" | cut -f1), $$(tar -tzf "$$midia" | grep -vc '/$$') arquivo(s))"

# `restore` constava do .PHONY sem receita: `make restore` respondia "Nothing to
# be done" e saía 0 — o comando de desastre fingindo que restaurou. Até existir
# a restauração verificada (docs/infra.md §5: sempre em base nova, nunca por cima
# da produção), ele reprova e aponta o procedimento manual.
restore: ## NÃO implementado: reprova e aponta o procedimento manual (docs/infra.md §5)
	@echo "make restore ainda não existe. Restaure à mão, SEMPRE numa base nova, seguindo docs/infra.md §5."; \
	exit 1
