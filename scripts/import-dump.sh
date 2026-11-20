#!/usr/bin/env bash
# Импорт дампа legacy и архива файлов в систему из docker-compose.yml.
#   init    — первичный импорт, только в пустую БД; если данные уже импортированы, ничего не делает.
#   replace — новый дамп разворачивается в отдельную БД и отдельный каталог файлов,
#             и только после успешного импорта подменяет текущие; прежние сохраняются.
# Пути берутся из окружения: DUMP, FILES_ARCHIVE (их задаёт Makefile), TOOLS_IMAGE.
set -euo pipefail

mode=${1:?init или replace}
cd "$(dirname "$0")/.."
root=$(pwd)
data=$root/data
storage=$data/attachments
backups=$data/backups
stamp=$(date +%Y%m%d-%H%M%S)
db_user=tsfera
db_password=tsfera
db=tsfera

log() { printf '\n==> %s\n' "$*"; }
fail() { printf 'Ошибка: %s\n' "$*" >&2; exit 1; }
pg() { local database=$1; shift; docker compose exec -T postgres psql -X -q -At -v ON_ERROR_STOP=1 -U "$db_user" -d "$database" "$@"; }
url() { printf 'postgresql://%s:%s@postgres:5432/%s?sslmode=disable' "$db_user" "$db_password" "$1"; }
user_tables() { pg "$1" -c "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND c.relkind IN ('r','p','v','m','S')"; }
# Права каталогов из архива могут запрещать запись, что мешает удалению.
remove() { if [ -e "$1" ]; then chmod -R u+w "$1"; rm -rf "$1"; fi; }
# Каталог с данными не удаляется, а уходит в резервные копии.
stash() { if [ -d "$1" ]; then if [ -z "$(ls -A "$1")" ]; then rmdir "$1"; else mkdir -p "$backups"; mv "$1" "$backups/$stamp-$(basename "$1")"; log "Прежний каталог перенесён в data/backups/$stamp-$(basename "$1")"; fi; fi; }

mkdir -p "$data"
log 'Запуск PostgreSQL'
docker compose up -d --wait postgres

if [ "$mode" = init ]; then
  if [ "$(user_tables "$db")" != 0 ]; then
    if [ "$(pg "$db" -c "SELECT to_regclass('public.platform_legacy_import') IS NOT NULL")" = t ]; then
      log 'Данные уже импортированы, импорт пропущен. Новый дамп: make deploy-dump'
    else
      log 'БД уже содержит данные без импорта дампа, импорт пропущен. Заменить их дампом: make deploy-dump'
    fi
    mkdir -p "$storage"; exit 0
  fi
  if [ -z "${DUMP:-}" ]; then
    log 'Дамп (*.dump в корне проекта) не найден, система запускается с пустой БД'
    mkdir -p "$storage"; exit 0
  fi
  target=$db
  destination=$storage
  stash "$storage"
elif [ "$mode" = replace ]; then
  [ -n "${DUMP:-}" ] || fail 'не найден дамп: положите *.dump в корень проекта или передайте DUMP=путь'
  if [ "${YES:-}" != 1 ]; then
    [ -t 0 ] || fail 'для запуска без терминала передайте YES=1'
    printf 'Текущие данные системы будут заменены дампом %s (прежние БД и файлы сохранятся как резервная копия). Продолжить? [yes/N] ' "$DUMP"
    read -r answer; [ "$answer" = yes ] || fail 'отменено'
  fi
  target=${db}_next
  destination=$data/attachments.next
  # Остатки прошлой неудачной попытки: в них нет опубликованных данных.
  pg postgres -c "DROP DATABASE IF EXISTS $target WITH (FORCE)"
  remove "$destination"
  pg postgres -c "CREATE DATABASE $target"
else
  fail "неизвестный режим $mode"
fi

[ -f "$DUMP" ] || fail "нет файла дампа $DUMP"
network=$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}} {{end}}' "$(docker compose ps -q postgres)" | awk '{print $1}')
report=$data/reports/migration-report-$stamp.json
mkdir -p "$data/reports"

files_root=
if [ -n "${FILES_ARCHIVE:-}" ]; then
  [ -f "$FILES_ARCHIVE" ] || fail "нет архива файлов $FILES_ARCHIVE"
  archive=$(cd "$(dirname "$FILES_ARCHIVE")" && pwd)/$(basename "$FILES_ARCHIVE")
  files_root=$root/.cache/import/files
  log "Распаковка $(basename "$archive")"
  remove "$files_root"; mkdir -p "$files_root"
  docker run --rm --user "$(id -u):$(id -g)" -v "$(dirname "$archive"):/archive:ro" -v "$files_root:/out" \
    "$TOOLS_IMAGE" bsdtar -xf "/archive/$(basename "$archive")" -C /out
else
  log 'Архив файлов (*.rar, *.zip, *.tar.gz) не найден, переносятся только метаданные файлов'
fi

log "Импорт дампа $(basename "$DUMP") в БД $target"
# Архив может быть неполным: отсутствующие файлы перечисляются в отчёте, остальные переносятся.
MIGRATION_ADMIN_URL=$(url postgres) MIGRATION_DATABASE_URL=$(url "$target") \
MIGRATION_FILES_ROOT=$files_root MIGRATION_FILES_DESTINATION=$destination MIGRATION_REPORT=$report \
  make --no-print-directory migrate-dump DUMP="$DUMP" ALLOW_MISSING_FILES=1 MIGRATION_DOCKER_NETWORK="$network" \
  || { [ "$mode" = replace ] && pg postgres -c "DROP DATABASE IF EXISTS $target WITH (FORCE)"; fail "импорт не выполнен, текущие данные не изменены. Отчёт: ${report#$root/}"; }
remove "$files_root"
mkdir -p "$destination"

if [ "$mode" = replace ]; then
  log 'Подмена данных'
  docker compose stop backend
  previous=${db}_prev_${stamp//-/_}
  pg postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='$db' AND pid<>pg_backend_pid()" >/dev/null
  pg postgres -c "ALTER DATABASE $db RENAME TO $previous"
  pg postgres -c "ALTER DATABASE $target RENAME TO $db"
  stash "$storage"
  mv "$destination" "$storage"
  log "Прежняя БД сохранена как $previous (удалить: docker compose exec postgres dropdb -U $db_user $previous)"
fi
log "Импорт завершён. Отчёт: ${report#$root/}"
# Стартовый администратор не создаётся, если такое имя уже есть среди перенесённых пользователей.
admin_user=${TSFERA_ADMIN_USER:-$(sed -n 's/^TSFERA_ADMIN_USER=//p' .env 2>/dev/null | tail -n 1)}
admin_user=${admin_user:-admin}
if [ "$(pg "$db" -v name="$admin_user" <<<"SELECT count(*) FROM users_accounts WHERE username = :'name'")" != 0 ]; then
  log "Пользователь $admin_user есть в дампе: вход по его паролю из старой системы, стартовый администратор не создаётся.
    Для нового администратора задайте в .env TSFERA_ADMIN_USER=<имя, которого нет в дампе> и TSFERA_ADMIN_PASSWORD=<пароль>, затем make up"
fi
