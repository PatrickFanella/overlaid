SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

.PHONY: help verify deps fmt lint test build compose-config open-pilot-check dev

help:
	@awk 'BEGIN {FS = ":.*##"; printf "Commands:\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps:
	pnpm --dir web install --frozen-lockfile

verify: deps fmt lint test build compose-config open-pilot-check ## Run all checks

fmt:
	@git ls-files --cached --others --exclude-standard 'backend/*.go' | xargs -r gofmt -w
	pnpm --dir web run format

lint:
	cd backend && go vet ./...
	pnpm --dir web run lint

test:
	cd backend && go test ./...
	pnpm --dir web run test

build:
	mkdir -p bin
	cd backend && go build -o ../bin/over-laid-api ./cmd/app
	pnpm --dir web run build

compose-config:
	docker compose config --quiet

open-pilot-check:
	@test -f .gitea/ISSUE_TEMPLATE/open-pilot-task.yaml
	@grep -q 'Test Command' .gitea/ISSUE_TEMPLATE/open-pilot-task.yaml

dev:
	pnpm --dir web run dev

docker-build:
	docker compose up --build -d --remove-orphans

logs:
	docker compose logs -f
