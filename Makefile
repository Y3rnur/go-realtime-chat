MIGRATIONS_DIR := db/migrations
SEED_FILE := db/seeds/dev_seed.sql

.PHONY: help run start migrate-up migrate-down seed reset

help:
	@echo "Targets:"
	@echo "  make run|start     - load .env, set JWT_SECRET (if missing), and run the server"
	@echo "  make migrate-up    - apply all *up.sql migrations (in order)"
	@echo "  make migrate-down  - apply all *down.sql migrations (reverse order)"
	@echo "  make seed          - run DB seed script"
	@echo "  make reset         - migrate-down, migrate-up, then seed"

run start:
	@set -a; \
	[ -f .env ] && . ./ .env; \
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