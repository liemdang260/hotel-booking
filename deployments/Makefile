COMPOSE ?= docker compose

.PHONY: up down reset ps logs

up:
	$(COMPOSE) --env-file .env -f docker-compose.yml up -d

down:
	$(COMPOSE) --env-file .env -f docker-compose.yml down

reset:
	$(COMPOSE) --env-file .env -f docker-compose.yml down -v --remove-orphans

ps:
	$(COMPOSE) --env-file .env -f docker-compose.yml ps

logs:
	$(COMPOSE) --env-file .env -f docker-compose.yml logs -f
