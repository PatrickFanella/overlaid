SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

API_PORT ?= 8080
WEB_PORT ?= 5173

.PHONY: help verify deps fmt lint test build compose-config open-pilot-check dev up down restart ps logs docker-build smoke smoke-alert db-shell db-migrate db-tables db-reset-postgres db-reset

help:
	@awk 'BEGIN {FS = ":.*##"; printf "Commands:\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps: ## Install frontend dependencies
	pnpm --dir web install --frozen-lockfile

verify: deps fmt lint test build compose-config open-pilot-check ## Run all checks

fmt: ## Format/check code formatting
	@git ls-files --cached --others --exclude-standard 'backend/*.go' | xargs -r gofmt -w
	pnpm --dir web run format

lint: ## Run linters/static checks
	cd backend && go vet ./...
	pnpm --dir web run lint

test: ## Run backend and frontend tests
	cd backend && go test ./...
	pnpm --dir web run test

build: ## Build backend binary and frontend assets
	mkdir -p bin
	cd backend && go build -o ../bin/over-laid-api ./cmd/app
	pnpm --dir web run build

compose-config: ## Validate Docker Compose configuration
	docker compose config --quiet

open-pilot-check: ## Verify Open Pilot templates are preserved
	@test -f .gitea/ISSUE_TEMPLATE/open-pilot-task.yaml
	@grep -q 'Test Command' .gitea/ISSUE_TEMPLATE/open-pilot-task.yaml

dev: ## Run frontend dev server
	pnpm --dir web run dev

up: ## Start the full Docker Compose stack
	docker compose up -d --remove-orphans

down: ## Stop the Docker Compose stack
	docker compose down

restart: ## Restart the Docker Compose stack
	docker compose restart

ps: ## Show Docker Compose service status
	docker compose ps

docker-build: ## Build and start the Docker Compose stack
	docker compose up --build -d --remove-orphans

logs: ## Follow Docker Compose logs
	docker compose logs -f

smoke: ## Smoke test API health and web shell
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/healthz
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/readyz
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/api/auth/twitch/status
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/api/obs/status
	curl --max-time 15 -fsS http://localhost:$(WEB_PORT)/overlay/alerts >/dev/null
	curl --max-time 15 -fsS http://localhost:$(WEB_PORT)/dashboard >/dev/null
	curl --max-time 15 -fsS http://localhost:$(WEB_PORT)/dashboard/live >/dev/null

smoke-alert: ## Send a test alert to the running API
	curl -fsS -X POST http://localhost:$(API_PORT)/api/events/test-alert \
		-H 'Content-Type: application/json' \
		-H 'X-Overlay-Operator-Secret: $(OPERATOR_SECRET)' \
		-d '{"title":"Make smoke","message":"Overlay pipeline is live","severity":"success"}'

db-shell: ## Open psql inside the Postgres container
	docker compose exec postgres sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"'

db-migrate: ## Apply SQL migrations to the Compose Postgres database
	@for file in migrations/*.sql; do \
		test -f "$$file" || continue; \
		echo "Applying $$file"; \
		docker compose exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"' < "$$file"; \
	done

db-tables: ## List tables in the Compose Postgres database
	docker compose exec -T postgres sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -c "\\dt"'

db-reset-postgres: ## Remove only the Compose Postgres volume; preserves API token/state data
	docker compose stop postgres
	docker compose rm -f postgres
	docker volume rm $$(docker volume ls -q \
		--filter label=com.docker.compose.project=$$(basename "$$PWD") \
		--filter label=com.docker.compose.volume=postgres-data)

db-reset: ## Remove all Compose volumes; destructive, includes API token/state data
	docker compose down -v
