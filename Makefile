# Все цели выполняются в Docker-контейнерах: на машине нужны только docker и make.
# make (make init) поднимает систему и при первом запуске импортирует дамп и архив файлов
# из корня проекта; make deploy-dump заменяет данные системы новым дампом.
# Репозиторий монтируется по тому же абсолютному пути, поэтому относительные
# и абсолютные пути внутри него одинаковы в контейнере и на хосте.

GO_VERSION := $(shell awk '/^go /{print $$2}' backend/go.mod)
GO_IMAGE ?= golang:$(GO_VERSION)
NODE_IMAGE ?= node:22-bookworm-slim
TOOLS_IMAGE ?= tsfera-tools:go$(GO_VERSION)-pg16

# Кэши и node_modules для Linux лежат в .cache/ и не смешиваются с локальными.
CACHE := $(CURDIR)/.cache
NODE_MODULES := $(CACHE)/node_modules

DOCKER_RUN = docker run --rm --init \
	--user $(shell id -u):$(shell id -g) \
	-e HOME=$(CACHE)/home \
	-v $(CURDIR):$(CURDIR)

GO_ENV = -e GOCACHE=$(CACHE)/go/build -e GOMODCACHE=$(CACHE)/go/mod -e GOTOOLCHAIN=local
GO_RUN = $(DOCKER_RUN) $(GO_ENV) -w $(CURDIR)/backend $(GO_IMAGE)
NODE_RUN = $(DOCKER_RUN) -e npm_config_cache=$(CACHE)/npm \
	-v $(NODE_MODULES):$(CURDIR)/frontend/node_modules \
	-w $(CURDIR)/frontend $(NODE_IMAGE)

.DEFAULT_GOAL := init

.PHONY: test-backend vet-backend build-backend test-frontend build-frontend test deps-frontend tools-image clean-cache

$(CACHE)/.dirs:
	mkdir -p $(CACHE)/home $(CACHE)/go $(CACHE)/npm $(CACHE)/tmp $(NODE_MODULES) frontend/node_modules
	touch $@

test-backend: $(CACHE)/.dirs
	$(GO_RUN) go test -race ./...

vet-backend: $(CACHE)/.dirs
	$(GO_RUN) go vet ./...

build-backend: $(CACHE)/.dirs
	$(GO_RUN) go build -o bin/tsfera-server ./cmd/server

# npm ci повторяется только при изменении package.json или package-lock.json.
$(NODE_MODULES)/.package-lock.json: frontend/package.json frontend/package-lock.json $(CACHE)/.dirs
	$(NODE_RUN) npm ci --no-audit --no-fund

deps-frontend: $(NODE_MODULES)/.package-lock.json

test-frontend: deps-frontend
	$(NODE_RUN) npm test

build-frontend: deps-frontend
	$(NODE_RUN) npm run build

test: vet-backend test-backend test-frontend build-frontend

tools-image:
	docker build -q --build-arg GO_IMAGE=$(GO_IMAGE) -t $(TOOLS_IMAGE) docker/tools >/dev/null

# Модули Go в кэше доступны только для чтения, поэтому перед удалением снимаем запрет.
clean-cache:
	chmod -R u+w $(CACHE) 2>/dev/null || true
	rm -rf $(CACHE)

# Дамп и архив файлов по умолчанию — самые свежие в корне проекта (в Git не попадают).
DUMP ?= $(shell ls -t *.dump 2>/dev/null | head -n 1)
FILES_ARCHIVE ?= $(shell ls -t *.rar *.zip *.tar.gz *.tgz *.tar 2>/dev/null | head -n 1)
export DUMP FILES_ARCHIVE TOOLS_IMAGE

.PHONY: init deploy-dump up down
init: tools-image $(CACHE)/.dirs
	scripts/import-dump.sh init
	docker compose up -d --build

# Прежние БД и файлы остаются в tsfera_prev_<время> и data/backups/.
deploy-dump: tools-image $(CACHE)/.dirs
	scripts/import-dump.sh replace
	docker compose up -d --build --force-recreate backend
	docker compose up -d --build

up:
	docker compose up -d --build

down:
	docker compose down

# Консольные агенты для ИИ (провайдеры claude_cli, codex_cli, antigravity_cli), как в KB.
# Агенты входят в образ бэкенда по умолчанию; вход в аккаунты хранится в томе ai_cli (/data/ai-cli).
.PHONY: build-ai-cli claude-login codex-login agy-login
build-ai-cli:
	docker compose up -d --build backend

# Если бэкенд собран без агентов (старый образ или TSFERA_BACKEND_TARGET=runtime) — пересобираем его с агентами.
define AI_CLI_LOGIN
	@docker compose exec backend sh -c 'command -v $(1) >/dev/null' 2>/dev/null || { \
		echo "В контейнере backend нет $(1) — пересобираю образ с консольными агентами (первый раз ~10 минут)"; \
		TSFERA_BACKEND_TARGET=ai-cli docker compose up -d --build backend; }
	docker compose exec -it $(2) backend sh -c "mkdir -p $(3) && $(4)"
endef

## Вход в Claude Code: откройте ссылку из вывода; вместо входа можно указать токен `claude setup-token` в провайдере
claude-login:
	$(call AI_CLI_LOGIN,claude,-e HOME=/data/ai-cli/claude_cli,/data/ai-cli/claude_cli,claude)

## Вход в ChatGPT для Codex: откройте ссылку или введите код из вывода
codex-login:
	$(call AI_CLI_LOGIN,codex,-e HOME=/data/ai-cli/codex_cli -e CODEX_HOME=/data/ai-cli/codex_cli,/data/ai-cli/codex_cli,codex login --device-auth)

## Вход в аккаунт Google для Antigravity: откройте ссылку, вставьте код, затем выйдите (/quit)
agy-login:
	$(call AI_CLI_LOGIN,agy,-e HOME=/data/ai-cli/antigravity_cli,/data/ai-cli/antigravity_cli,agy)

# URLs are read from environment; dump paths are never interpolated as shell code.
export MIGRATION_DUMP = $(DUMP)
.PHONY: migrate-plan migrate-dump test-migration

# Сеть хоста: адреса localhost в MIGRATION_*_URL указывают на БД хоста, как без Docker.
MIGRATION_DOCKER_NETWORK ?= host
# Каталоги входных и выходных путей монтируются по тем же абсолютным путям.
MIGRATION_MOUNT_DIRS = $(filter-out $(CURDIR),$(sort \
	$(if $(DUMP),$(patsubst %/,%,$(dir $(abspath $(DUMP))))) \
	$(if $(PERMISSION_MAP),$(patsubst %/,%,$(dir $(abspath $(PERMISSION_MAP))))) \
	$(if $(MIGRATION_FILES_ROOT),$(abspath $(MIGRATION_FILES_ROOT))) \
	$(if $(MIGRATION_FILES_DESTINATION),$(patsubst %/,%,$(dir $(abspath $(MIGRATION_FILES_DESTINATION)))))))
MIGRATION_RUN = $(DOCKER_RUN) $(GO_ENV) \
	--network $(MIGRATION_DOCKER_NETWORK) \
	-e TMPDIR=$(CACHE)/tmp \
	-e MIGRATION_DUMP -e MIGRATION_ADMIN_URL -e MIGRATION_DATABASE_URL -e MIGRATION_REPORT \
	-e MIGRATION_PERMISSION_MAP -e MIGRATION_ALLOW_MISSING_FILES -e MIGRATION_ALLOW_UNMAPPED_PERMISSIONS \
	-e MIGRATION_FILES_ROOT -e MIGRATION_FILES_DESTINATION \
	$(foreach d,$(MIGRATION_MOUNT_DIRS),-v $(d):$(d)) \
	-w $(CURDIR) $(TOOLS_IMAGE)

migrate-plan: tools-image $(CACHE)/.dirs
	$(MIGRATION_RUN) python3 scripts/migration/migrate.py

migrate-dump: tools-image $(CACHE)/.dirs
	$(MIGRATION_RUN) python3 scripts/migration/migrate.py --apply

test-migration: tools-image $(CACHE)/.dirs
	$(MIGRATION_RUN) python3 -B -m unittest discover -s scripts/migration -p 'test_*.py'

# Explicit exceptions for a metadata-only rehearsal or restricted imported accounts.
export MIGRATION_ALLOW_MISSING_FILES = $(ALLOW_MISSING_FILES)
export MIGRATION_ALLOW_UNMAPPED_PERMISSIONS = $(ALLOW_UNMAPPED_PERMISSIONS)
export MIGRATION_PERMISSION_MAP = $(PERMISSION_MAP)
