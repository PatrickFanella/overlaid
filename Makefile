SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

API_PORT ?= 8080
WEB_PORT ?= 5173
VERSION ?= dev
BUILD_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_LDFLAGS := -X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.Version=$(VERSION) -X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.Commit=$(BUILD_COMMIT) -X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.BuiltAt=$(BUILD_DATE)
BACKUP_DIR ?= backups
PROBE_URL ?= http://127.0.0.1:$(API_PORT)/readyz
PROBE_CHECKS ?=
PROBE_TIMEOUT ?= 5s

.PHONY: help verify deps fmt lint test config-contract-check build build-probe probe compose-config open-pilot-check dev up down restart ps logs docker-build wait-api smoke smoke-browser smoke-alert db-shell db-migrate db-tables db-reset-postgres db-reset api-data-volume-check api-data-backup postgres-backup

help:
	@awk 'BEGIN {FS = ":.*##"; printf "Commands:\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps: ## Install frontend dependencies
	pnpm --dir web install --frozen-lockfile

verify: deps fmt lint test build compose-config open-pilot-check ## Run all checks

fmt: ## Format/check code formatting
	@git ls-files -z --cached --others --exclude-standard 'backend/*.go' | while IFS= read -r -d '' file; do [[ -f "$$file" ]] && printf '%s\0' "$$file"; done | xargs -0 -r gofmt -w
	pnpm --dir web run format

lint: ## Run linters/static checks
	cd backend && go vet ./...
	cd backend/spikes/river && go vet ./...
	pnpm --dir web run lint

config-contract-check: ## Check deployment/docs defaults without Go's test cache
	cd backend && go test -count=1 ./internal/config -run '^TestTwitchScopeDefaultsMatchDeployment$$'

test: config-contract-check ## Run backend and frontend tests
	cd backend && go test ./...
	cd backend/spikes/river && go test ./...
	pnpm --dir web run test

build: build-probe ## Build backend binaries and frontend assets
	mkdir -p bin
	cd backend && go build -ldflags '$(GO_LDFLAGS)' -o ../bin/over-laid-api ./cmd/app
	cd backend && go build -o ../bin/overlaid-admin ./cmd/admin
	cd backend && go build -o ../bin/overlaid-pacing-probe ./cmd/pacing-probe
	cd backend && go build -ldflags '$(GO_LDFLAGS)' -o ../bin/overlaid-bot-admin ./cmd/bot-admin
	cd backend && go build -ldflags '$(GO_LDFLAGS)' -o ../bin/overlaid-app-admin ./cmd/app-admin
	cd backend && go build -ldflags '$(GO_LDFLAGS)' -o ../bin/overlaid-app-issue ./cmd/app-issue
	cd backend && go build -ldflags '$(GO_LDFLAGS)' -o ../bin/overlaid-conduit-inspect ./cmd/conduit-inspect
	cd backend && go build -ldflags '$(GO_LDFLAGS)' -o ../bin/overlaid-conduit-create ./cmd/conduit-create
	pnpm --dir web run build

build-probe: ## Build the readiness monitoring probe
	mkdir -p bin
	cd backend && go build -ldflags '$(GO_LDFLAGS)' -o ../bin/over-laid-probe ./cmd/probe

probe: build-probe ## Probe full readiness or explicitly selected checks
	bin/over-laid-probe --url "$(PROBE_URL)" --checks "$(PROBE_CHECKS)" --timeout "$(PROBE_TIMEOUT)"

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
	BUILD_VERSION=$(VERSION) BUILD_COMMIT=$(BUILD_COMMIT) BUILD_DATE=$(BUILD_DATE) docker compose up --build -d --remove-orphans

logs: ## Follow Docker Compose logs
	docker compose logs -f

wait-api: ## Wait for API health endpoint after deploy/restart
	@for attempt in {1..30}; do \
		if curl --max-time 2 -fsS http://localhost:$(API_PORT)/healthz >/dev/null; then \
			echo "API ready"; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "API did not become ready on localhost:$(API_PORT)" >&2; \
	exit 1

smoke: wait-api ## Smoke test API health and web shell
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/healthz
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/version
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/readyz
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/api/auth/twitch/status
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/api/obs/status
	curl --max-time 15 -fsS http://localhost:$(WEB_PORT)/overlay/alerts >/dev/null
	curl --max-time 15 -fsS http://localhost:$(WEB_PORT)/dashboard >/dev/null
	curl --max-time 15 -fsS http://localhost:$(WEB_PORT)/dashboard/live >/dev/null

smoke-browser: ## Browser smoke test critical dashboard and overlay routes
	pnpm --dir web run e2e:smoke

api-data-volume-check: ## Ensure the Compose api-data volume exists before backup
	@volume=$$(docker volume ls -q \
		--filter label=com.docker.compose.project=$${COMPOSE_PROJECT_NAME:-$$(basename "$$PWD")} \
		--filter label=com.docker.compose.volume=api-data); \
	if [[ -z "$$volume" ]]; then \
		echo "api-data Compose volume not found; start the stack or check COMPOSE_PROJECT_NAME" >&2; \
		exit 1; \
	fi

api-data-backup: api-data-volume-check ## Back up the Compose api-data volume into ./backups
	mkdir -p "$(BACKUP_DIR)"
	volume=$$(docker volume ls -q \
		--filter label=com.docker.compose.project=$${COMPOSE_PROJECT_NAME:-$$(basename "$$PWD")} \
		--filter label=com.docker.compose.volume=api-data); \
	api_data_backup_path=$$(cd -- "$(BACKUP_DIR)" && pwd -P) || exit 1; \
	docker run --rm \
		-v "$$volume:/data:ro" \
		-v "$$api_data_backup_path:/backup" \
		alpine:3.22 \
		sh -c 'tar -czf /backup/api-data-$$(date -u +%Y%m%dT%H%M%SZ).tar.gz -C /data .'

postgres-backup: ## Back up the Compose Postgres database into ./backups
	mkdir -p "$(BACKUP_DIR)"
	docker compose exec -T postgres sh -c 'pg_dump -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" --format=custom' > "$(BACKUP_DIR)/postgres-$$(date -u +%Y%m%dT%H%M%SZ).dump"

smoke-alert: ## Send a test alert to the running API
	@ : "$${OPERATOR_SECRET:?OPERATOR_SECRET must be set}"
	@curl -fsS -X POST http://localhost:$(API_PORT)/api/events/test-alert \
		-H 'Content-Type: application/json' \
		-H "X-Overlay-Operator-Secret: $$OPERATOR_SECRET" \
		-d '{"title":"Make smoke","message":"Overlay pipeline is live","severity":"success"}'

db-shell: ## Open psql inside the Postgres container
	docker compose exec postgres sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"'

db-migrate: ## Apply pending SQL migrations using administrator credentials
	bash scripts/db-migrate.sh

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
