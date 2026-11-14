# Теплосфера — модульная CRM, документация, ИИ и чат (Go + Vue 3)

Переписанная и расширенная платформа **Теплосфера**, построенная как встраиваемый Go-модуль (`github.com/zubans/tsfera/backend`) и SPA-интерфейс на Vue 3 + TypeScript + Naive UI.

## Архитектура модулей (`backend/`)

- `pkg/platform` — общие контракты `Module`, `Authorizer`, `EventBus`, `ToolDefinition`, `IndexSource`, `EntityResolver`, `WorkerGroup`, встроенный раннер миграций `platform.Migrate`.
- `internal/modules/users` — аутентификация (bcrypt + плавный апгрейд legacy `md5(md5)`), сессии и `API-токены` (`Bearer` / `X-API-key` с правами `can_read`/`can_write`), RBAC, аудит входов, матрица доступа к ИИ-моделям.
- `internal/modules/crm` — контрагенты (взвешенный поиск, правило видимости инженера `±3 дня`), адреса, оборудование (`Type Model Manufacturer`, `pg_trgm`), договоры ТО, календарь заявок (контроль пересечений + дежурный режим + оценки + непросмотренные отметки), акты выполненных работ (9 замеров телеметрии котла, автозакрытие заявок, CSV), задачи и круговой пул (`PoolSetting`), вебхуки МТС ВАТС, WebSocket-хаб реального времени, отчёты и мобильный API `/api/doc/*`, `/api/ref/*`, `/api/enum/*`.
- `internal/modules/docs` — пространства документации, оптимистическая блокировка `expected_version` (HTTP 409), история ревизий с gzip-архивацией старше 24ч, нативный импорт `.docx` в Markdown, извлечение текста `.xlsx`/`.docx`/текста, инлайн-примечания с цитированием, привязка документов к сущностям CRM и экспорт `context.md`.
- `internal/modules/ai` — мульти-провайдерный LLM-шлюз (OpenAI, OpenAI-compat, Anthropic, Gemini, Ollama), гибридный поиск (PostgreSQL FTS `russian` + векторное сходство RRF `k=60`) с жёсткой фильтрацией по правам, ИИ-генерация черновиков документов (`SSE`), учёт токенов.
- `internal/modules/chat` — диалоги с ИИ в двух режимах (`agent` до 6 tool-раундов и `rag` top-8) с SSE-потоком (`user`, `step`, `delta`, `sources`, `done`), привязкой к карточкам CRM и share-ссылками.
- `pkg/tsfera` — точка подключения платформы в любой внешний Go-проект (`NewPlatform`, `NewCRMOnly`, `ExtraModules`).

## Встраивание в больший Go-проект

```go
import (
    "github.com/go-chi/chi/v5"
    "github.com/zubans/tsfera/backend/pkg/tsfera"
    "github.com/zubans/tsfera/backend/pkg/platform"
)

plat, err := tsfera.NewPlatform(ctx, tsfera.Config{
    Pool:            hostPgxPool,      // общий пул БД хоста
    Authorizer:      hostAuthProvider, // опционально: свой SSO/JWT провайдер
    EnableDocsAndAI: true,             // или tsfera.NewCRMOnly(...)
    ExtraModules:    []platform.Module{hostCustomModule},
})
if err != nil {
    return err
}
if err := plat.Migrate(ctx); err != nil {
    return err
}
plat.StartWorkers(ctx)
plat.Mount(hostChiRouter)
```

## Быстрый старт

```bash
docker compose up -d
```

Либо локально:

```bash
cd backend && go test -race ./... && go run ./cmd/server
cd frontend && npm install && npm run dev
```

## Подключение из GitHub

Репозиторий: https://github.com/zubans/tsfera. Go-модуль находится в `backend/`:

```bash
go get github.com/zubans/tsfera/backend@main
```

Требуются Go версии из `backend/go.mod`, PostgreSQL с pgvector и Node.js 22.22.2+ для frontend. Зависимости frontend устанавливаются через `npm ci`.

## Контроль доступа и история документов

- API-токены без `can_write` не допускаются к изменяющим HTTP-запросам. Выпуск новых токенов разрешён только пользовательской сессии.
- ИИ-поиск проверяет актуальную видимость и существование документа, не доверяя метаданным индекса. Изменение метаданных обновляет хеш чанка; удаление документа очищает его чанки.
- Изменение комментариев, загрузка вложений, добавление связей и удаление документа проверяют доступ к документу.
- Документ и его ревизия сохраняются в одной транзакции PostgreSQL. Ошибка сохранения истории откатывает изменение документа.

## Телефон и формулы

Встроенный телефон использует JsSIP: SIP-регистрация через WebSocket, исходящие и входящие аудиозвонки, ответ, завершение и защита от второго одновременного звонка. Учётная запись сотрудника должна содержать SIP-домен, логин, пароль и WebSocket-адрес. Для микрофона нужен HTTPS либо localhost и разрешение браузера; для внешней сети требуется рабочая конфигурация WebRTC/NAT на стороне АТС. На обычном HTTP-адресе сервера доступ к микрофону браузер может заблокировать.

Формулы `$...$` и `$$...$$` отображаются KaTeX. HTML проходит DOMPurify, доверенные команды KaTeX отключены.

Регрессионные тесты проверяют ограничения токенов, актуальные права документов, защиту комментариев, откат при ошибке записи ревизии, SIP-жизненный цикл и формулы. Unit-тесты SIP используют имитацию клиента; реальный звонок через МТС и полный функциональный паритет с legacy требуют отдельной приёмки на стенде.
