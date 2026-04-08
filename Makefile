MIGRATIONS_DIR := db/migrations
SEED_FILE := db/seeds/dev_seed.sql

.PHONY: help run start migrate-up migrate-down seed reset

help:
	@echo "Targets:"
	@echo "  make run|start     	- load .env, set JWT_SECRET (if missing), and run the server"
	@echo "  make migrate-up    	- apply all *up.sql migrations (in order)"
	@echo "  make migrate-down  	- apply all *down.sql migrations (reverse order)"
	@echo "  make seed          	- run DB seed script"
	@echo "  make reset         	- migrate-down, migrate-up, then seed"
	@echo "  make docker-fresh   	- Full reset: down volumes, up infra, migrate, and up app"
	@echo "  make docker-down-v  	- Stop docker containers and remove volumes"
	@echo "  make docker-up-infra 	- Start postgres and redis in background"
	@echo "  make docker-migrate 	- Run migrations inside docker container"
	@echo "  make docker-up-app   	- Build and start the Go application"
	@echo "  make setup          	- Initial setup: prepare .env and run docker-fresh"
	@echo "  make prepare-env    	- Copy .env.example to .env if it doesn't exist"

run start:
	@set -a; \
	[ -f .env ] && . ./.env; \
	set +a; \
	export JWT_SECRET=$${JWT_SECRET:-dev_jwt_secret}; \
	go run .

migrate-up:
	bash -lc 'set -a; [ -f .env ] && . .env; set +a; if [ -z "$$DATABASE_URL" ]; then echo "DATABASE_URL not set"; exit 1; fi; for f in $$(ls $(MIGRATIONS_DIR)/*up.sql | sort); do echo "apply $$f"; psql "$$DATABASE_URL" -f "$$f"; done'

migrate-down:
	bash -lc 'set -a; [ -f .env ] && . .env; set +a; if [ -z "$$DATABASE_URL" ]; then echo "DATABASE_URL not set"; exit 1; fi; for f in $$(ls $(MIGRATIONS_DIR)/*down.sql 2>/dev/null | sort -r); do echo "apply $$f"; psql "$$DATABASE_URL" -f "$$f"; done'

seed:
	bash -lc 'set -a; [ -f .env ] && . .env; set +a; if [ -z "$$DATABASE_URL" ]; then echo "DATABASE_URL not set"; exit 1; fi; echo "seeding: $(SEED_FILE)"; psql "$$DATABASE_URL" -f "$(SEED_FILE)"'

reset: migrate-down migrate-up seed

docker-fresh: docker-down-v docker-up-infra docker-migrate docker-up-app

docker-down-v:
	docker-compose down -v

docker-up-infra:
	docker-compose up -d postgres redis

docker-migrate:
	docker-compose run --rm migrate

docker-up-app:
	docker-compose up --build app

setup: prepare-env docker-fresh

prepare-env:
	@if [ ! -f .env]; then \
			echo "Creating .env from .env.example..."; \
			cp .env.example .env; \
	else \
			echo ".env already exists, skipping copy."; \
	fi