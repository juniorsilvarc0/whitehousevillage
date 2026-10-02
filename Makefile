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

.PHONY: help up down logs logs-api ps migrate migrate-down migrate-version seed smoke smoke-stack esperar check lint fmt-check vet test test-api test-admin test-integration it-schema it-seed it-suite it-concorrencia build fmt psql backup restore

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

smoke: ## Fumaça de aplicação: sobe o navegador, faz login e percorre as telas
	# Existe porque a suíte inteira ficou verde enquanto o stack NÃO SUBIA:
	# a imagem do painel morria no boot, o BFF respondia 502 e /crm/pipelines
	# devolvia 500. Teste roda contra código; isto roda contra a aplicação servida.
	#
	# Roda contra o que JÁ ESTÁ no ar. Se o stack não estiver na versão da
	# árvore, o alvo abaixo (`smoke-stack`) é o que sobe tudo antes.
	cd apps/admin && node e2e/fumaca.mjs $(SMOKE_URL)

smoke-stack: ## Sobe/atualiza o stack inteiro, migra, semeia e roda a fumaça
	# A ordem importa e é a mesma de um deploy: imagem nova → schema → seed →
	# fumaça. Sem o `up --build` a fumaça mede a imagem de ontem e dá por boa
	# uma tela que a árvore nem contém — foi exatamente o que aconteceu nesta
	# rodada com /app/reservas e /app/contatos, entregues e ausentes da imagem
	# servida em :3100.
	#
	# É este alvo que o job `smoke` do CI executa: o CI não deve carregar uma
	# segunda cópia da sequência (ver a nota do job `integration`).
	$(MAKE) up
	@$(MAKE) --no-print-directory esperar ALVO="$(SMOKE_API)/healthz" QUEM=api
	$(MAKE) migrate
	$(MAKE) seed
	@$(MAKE) --no-print-directory esperar ALVO="$(SMOKE_URL_OU_PADRAO)/login" QUEM=admin
	$(MAKE) smoke

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

lint: fmt-check vet ## gofmt + vet (com e sem tags) + golangci-lint + eslint + tsc
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint não está instalado — \`make check\` não consegue se completar."; \
		echo "instale com: brew install golangci-lint   (ou veja golangci-lint.run/welcome/install)"; \
		exit 1; \
	}
	cd apps/api && golangci-lint run ./...
	cd apps/admin && pnpm lint && pnpm exec tsc --noEmit

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

it-seed: ## (interno) Semeia o banco de integração — sem isso a suíte não roda
	cd apps/api && go run ./cmd/seed

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

psql: ## Console do banco
	$(COMPOSE) exec postgres psql -U $${POSTGRES_USER} -d $${POSTGRES_DB}

backup: ## Dump manual
	$(COMPOSE) exec -T postgres pg_dump -U $${POSTGRES_USER} $${POSTGRES_DB} | gzip > infra/backups/manual-$$(date +%Y%m%d-%H%M%S).sql.gz
