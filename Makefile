GOCMD ?= go
COMPOSE ?= docker compose
COMPOSE_FILE ?= deployments/docker-compose.yml
ENV_FILE ?= deployments/.env

.PHONY: build test fmt tidy check infra-up infra-down infra-reset infra-ps infra-logs

build:
	$(GOCMD) build ./...

test:
	$(GOCMD) test ./...

fmt:
	$(GOCMD) fmt ./...

tidy:
	$(GOCMD) mod tidy

check: fmt test build

infra-up:
	$(COMPOSE) --env-file $(ENV_FILE) -f $(COMPOSE_FILE) up -d --wait

infra-down:
	$(COMPOSE) --env-file $(ENV_FILE) -f $(COMPOSE_FILE) down

infra-reset:
	$(COMPOSE) --env-file $(ENV_FILE) -f $(COMPOSE_FILE) down -v --remove-orphans

infra-ps:
	$(COMPOSE) --env-file $(ENV_FILE) -f $(COMPOSE_FILE) ps

infra-logs:
	$(COMPOSE) --env-file $(ENV_FILE) -f $(COMPOSE_FILE) logs -f
