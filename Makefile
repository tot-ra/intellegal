SHELL := /bin/sh

.PHONY: help init up down ps logs clean clean-artifacts test test-go test-py test-fe migrate-up migrate-down migrate-version

help:
	@echo "Targets:"
	@echo "  init   - create local env file from .env.example if missing"
	@echo "  up     - start local stack in detached mode"
	@echo "  down   - stop local stack"
	@echo "  ps     - show running services"
	@echo "  logs   - tail service logs"
	@echo "  clean  - stop stack and remove volumes"
	@echo "  clean-artifacts - remove local generated build/cache artifacts"
	@echo "  test   - run all test suites"
	@echo "  test-go - run Go API tests"
	@echo "  test-py - run Python AI API tests"
	@echo "  test-fe - run frontend tests"
	@echo "  migrate-up - apply all pending PostgreSQL migrations"
	@echo "  migrate-down - roll back latest PostgreSQL migration"
	@echo "  migrate-version - show current PostgreSQL migration version"

init:
	@test -f .env || cp .env.example .env
	@mkdir -p samples/storage
	@echo "Initialized local environment."

up:
	docker compose up -d --build

down:
	docker compose down

ps:
	docker compose ps

logs:
	docker compose logs -f --tail=100

clean:
	docker compose down -v

clean-artifacts:
	find . -type d -name "__pycache__" -prune -exec rm -rf {} +
	find . -type d -name ".pytest_cache" -prune -exec rm -rf {} +
	find . -type d -name "*.egg-info" -prune -exec rm -rf {} +
	find . -type f -name "*.pyc" -delete
	rm -rf py-ai-api/.venv frontend/dist

test: test-go test-py test-fe

test-go:
	cd go-api && go test ./...

test-py:
	cd py-ai-api && \
	python3 -m venv .venv && \
	. .venv/bin/activate && \
	pip install -e .[dev] >/dev/null && \
	pytest

test-fe:
	cd frontend && if command -v bun >/dev/null 2>&1; then bun install >/dev/null && bun run test; else npm install >/dev/null && npm run test; fi

migrate-up:
	./infra/scripts/migrate.sh up

migrate-down:
	./infra/scripts/migrate.sh down

migrate-version:
	./infra/scripts/migrate.sh version
