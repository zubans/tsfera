FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /tsfera-server ./cmd/server

# Образ с консольными агентами для провайдеров claude_cli, codex_cli и antigravity_cli (как в KB).
# В docker compose используется по умолчанию. Вход в аккаунты — в томе /data/ai-cli,
# у каждого агента своя папка (HOME задаёт tsfera при запуске).
FROM debian:bookworm-slim AS ai-cli
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tzdata curl bash nodejs npm poppler-utils \
 && rm -rf /var/lib/apt/lists/*
# Каждый агент ставится отдельно, и сбой одного не роняет сборку: из части регионов недоступны
# claude.ai и antigravity.google. Что не установилось, видно в логе сборки (ПРЕДУПРЕЖДЕНИЕ) и
# в `make <агент>-login`. Прокси для сборки — стандартные build-аргументы HTTP_PROXY / HTTPS_PROXY.
# Claude Code и Codex — из npm (registry.npmjs.org), Node.js нужен как launcher нативных бинарников.
RUN npm install -g @anthropic-ai/claude-code \
 || echo "ПРЕДУПРЕЖДЕНИЕ: Claude Code не установлен (npm недоступен) — провайдер claude_cli работать не будет"
RUN npm install -g @openai/codex \
 || echo "ПРЕДУПРЕЖДЕНИЕ: Codex не установлен (npm недоступен) — провайдер codex_cli работать не будет"
RUN npm cache clean --force
# Antigravity CLI ставится целиком в /opt/agy: рядом с бинарником лежат его файлы.
# Скрипт сначала скачивается и проверяется: при блокировке региона вместо него приходит HTML-страница.
RUN mkdir -p /opt/agy \
 && if curl -fsSL https://antigravity.google/cli/install.sh -o /tmp/agy-install.sh \
      && head -c 200 /tmp/agy-install.sh | grep -q '^#!' \
      && HOME=/opt/agy bash /tmp/agy-install.sh \
      && [ -x "$(readlink -f /opt/agy/.local/bin/agy)" ]; then \
      chmod -R a+rX /opt/agy && ln -sf "$(readlink -f /opt/agy/.local/bin/agy)" /usr/local/bin/agy; \
    else \
      echo "ПРЕДУПРЕЖДЕНИЕ: Antigravity не установлен (antigravity.google недоступен) — провайдер antigravity_cli работать не будет"; \
    fi; \
    rm -f /tmp/agy-install.sh
ENV TZ=Europe/Moscow
WORKDIR /app
COPY --from=builder /tsfera-server /usr/local/bin/tsfera-server
EXPOSE 8080
CMD ["tsfera-server"]

# Облегчённый образ без консольных агентов: TSFERA_BACKEND_TARGET=runtime.
# Последняя стадия — цель по умолчанию для docker build без --target.
FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates tzdata poppler-utils
ENV TZ=Europe/Moscow
WORKDIR /app
COPY --from=builder /tsfera-server /usr/local/bin/tsfera-server
EXPOSE 8080
CMD ["tsfera-server"]
