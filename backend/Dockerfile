FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /tsfera-server ./cmd/server

# Образ с консольными агентами для провайдеров claude_cli, codex_cli и antigravity_cli (как в KB).
# Собирается целью ai-cli: TSFERA_BACKEND_TARGET=ai-cli (make build-ai-cli). Вход в аккаунты — в томе
# /data/ai-cli, у каждого агента своя папка (HOME задаёт tsfera при запуске).
FROM debian:bookworm-slim AS ai-cli
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tzdata curl bash nodejs npm \
 && rm -rf /var/lib/apt/lists/*
# Claude Code: бинарник общий, настройки и вход — в /data/ai-cli/claude_cli.
RUN curl -fsSL https://claude.ai/install.sh | bash \
 && install -m 755 "$(readlink -f /root/.local/bin/claude)" /usr/local/bin/claude \
 && rm -rf /root/.local/bin/claude /root/.local/share/claude /root/.claude /root/.claude.json
# Codex CLI из npm: Node.js нужен как launcher нативного бинарника.
RUN npm install -g @openai/codex && npm cache clean --force
# Antigravity CLI ставится целиком в /opt/agy: рядом с бинарником лежат его файлы.
RUN mkdir -p /opt/agy && HOME=/opt/agy sh -c 'curl -fsSL https://antigravity.google/cli/install.sh | bash' \
 && chmod -R a+rX /opt/agy \
 && ln -s "$(readlink -f /opt/agy/.local/bin/agy)" /usr/local/bin/agy
ENV TZ=Europe/Moscow
WORKDIR /app
COPY --from=builder /tsfera-server /usr/local/bin/tsfera-server
EXPOSE 8080
CMD ["tsfera-server"]

# Обычный образ без консольных агентов (цель по умолчанию).
FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Europe/Moscow
WORKDIR /app
COPY --from=builder /tsfera-server /usr/local/bin/tsfera-server
EXPOSE 8080
CMD ["tsfera-server"]
