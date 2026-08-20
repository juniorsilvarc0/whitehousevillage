SHELL := /bin/bash
COMPOSE := docker compose -f infra/docker-compose.yml --env-file .env

.PHONY: help up down logs ps migrate migrate-down seed check lint test test-api test-admin build fmt psql backup restore

help: ## Lista os alvos
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

up: ## Sobe o ambiente de desenvolvimento
	$(COMPOSE) up -d --build

down: ## Derruba o ambiente (preserva volumes)
	$(COMPOSE) down

logs: ## Segue os logs
	$(COMPOSE) logs -f --tail=120

ps: ## Estado dos serviços
	$(COMPOSE) ps

migrate: ## Aplica as migrations (passo explícito — nunca no boot da API)
	$(COMPOSE) run --rm migrate up

migrate-down: ## Desfaz a última migration
	$(COMPOSE) run --rm migrate down 1

seed: ## Popula produtos, unidades, tarifas, perfis e usuários de teste
	$(COMPOSE) run --rm seed

check: lint test ## Lint + typecheck + testes (rode antes de reportar qualquer entrega)

lint: ## golangci-lint + eslint + tsc
	cd apps/api && golangci-lint run ./...
	cd apps/admin && pnpm lint && pnpm exec tsc --noEmit

test: test-api test-admin ## Todos os testes

test-api: ## Testes Go (inclui o teste de concorrência do overbooking)
	cd apps/api && go test ./... -race -count=1

test-admin: ## Testes do painel
	cd apps/admin && pnpm test --run

fmt: ## Formata
	cd apps/api && gofmt -w . && go mod tidy
	cd apps/admin && pnpm format

build: ## Build de produção das imagens
	$(COMPOSE) build

psql: ## Console do banco
	$(COMPOSE) exec postgres psql -U $${POSTGRES_USER} -d $${POSTGRES_DB}

backup: ## Dump manual
	$(COMPOSE) exec -T postgres pg_dump -U $${POSTGRES_USER} $${POSTGRES_DB} | gzip > infra/backups/manual-$$(date +%Y%m%d-%H%M%S).sql.gz
