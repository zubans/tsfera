# Окружение для make-целей миграции: Python, клиент PostgreSQL и Go
# (migrate.py запускает go run ./cmd/migrate-schema), bsdtar для распаковки архива файлов.
ARG GO_IMAGE
FROM ${GO_IMAGE}
# Версия утилит совпадает с сервером из docker-compose.yml: pg_restore новее сервера
# выставляет параметры, которых сервер не знает (transaction_timeout в 17 при сервере 16).
ARG PG_MAJOR=16
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 libarchive-tools postgresql-common \
 && /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y \
 && apt-get install -y --no-install-recommends postgresql-client-${PG_MAJOR} \
 && rm -rf /var/lib/apt/lists/*
ENV PG_BIN=/usr/lib/postgresql/${PG_MAJOR}/bin
