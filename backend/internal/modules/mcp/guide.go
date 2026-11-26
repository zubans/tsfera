package mcp

import (
	"fmt"
	"strings"

	"github.com/zubans/tsfera/backend/internal/modules/docs"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type accessLevel string

const (
	accessAnonymous accessLevel = "anonymous"
	accessAskOnly   accessLevel = "ask_only"
	accessReadOnly  accessLevel = "read_only"
	accessReadWrite accessLevel = "read_write"
)

func resolveAccessLevel(auth *platform.AuthContext) accessLevel {
	if auth == nil {
		return accessAnonymous
	}
	if auth.IsToken {
		if auth.TokenAskOnly {
			return accessAskOnly
		}
		if auth.TokenCanWrite {
			return accessReadWrite
		}
		if auth.TokenCanRead {
			return accessReadOnly
		}
		return accessAskOnly
	}
	if auth.HasOwnerPermission("docs.create") || auth.HasOwnerPermission("docs.update") || auth.HasOwnerPermission("docs.comment") {
		return accessReadWrite
	}
	return accessReadOnly
}

func canUseDocsRead(auth *platform.AuthContext) bool {
	if auth == nil {
		return false
	}
	if auth.IsToken && (auth.TokenAskOnly || !auth.TokenCanRead) {
		return false
	}
	return auth.HasPermission("docs.view")
}

func canUseDocsCreate(auth *platform.AuthContext) bool {
	if auth == nil {
		return false
	}
	if auth.IsToken && (auth.TokenAskOnly || !auth.TokenCanWrite) {
		return false
	}
	return auth.HasPermission("docs.create")
}

func canUseDocsUpdate(auth *platform.AuthContext) bool {
	if auth == nil {
		return false
	}
	if auth.IsToken && (auth.TokenAskOnly || !auth.TokenCanWrite) {
		return false
	}
	return auth.HasPermission("docs.update")
}

func canUseDocsComment(auth *platform.AuthContext) bool {
	if auth == nil {
		return false
	}
	if auth.IsToken && (auth.TokenAskOnly || !auth.TokenCanWrite) {
		return false
	}
	return auth.HasPermission("docs.comment")
}

func canUseAsk(auth *platform.AuthContext) bool {
	if auth == nil {
		return false
	}
	return auth.HasOwnerPermission("docs.view") || auth.HasOwnerPermission("chat.use") || auth.HasOwnerPermission("ai.search")
}

func canUseLogResult(auth *platform.AuthContext) bool {
	if auth == nil {
		return false
	}
	if auth.IsToken {
		if auth.TokenAskOnly {
			return auth.HasOwnerPermission("docs.create") || auth.HasOwnerPermission("docs.update")
		}
		if !auth.TokenCanWrite {
			return false
		}
		return auth.HasPermission("docs.create") || auth.HasPermission("docs.update")
	}
	return auth.HasPermission("docs.create") || auth.HasPermission("docs.update")
}

// BuildAgentGuide собирает Markdown-справку для ИИ-агента с учётом прав токена и запрошенного раздела.
func BuildAgentGuide(baseURL string, auth *platform.AuthContext, spaces []docs.SpaceSummary, tools []mcpTool, topic string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	topic = strings.ToLower(strings.TrimSpace(topic))
	level := resolveAccessLevel(auth)

	switch topic {
	case "access":
		return sectionAccess(auth, level, spaces)
	case "connect":
		return sectionConnect(baseURL)
	case "workflow":
		return sectionWorkflow(level)
	case "tools":
		return sectionTools(tools, auth == nil)
	case "rest":
		return sectionREST(baseURL, level)
	case "rules":
		return sectionRules()
	case "all":
		parts := []string{
			"# Теплосфера — документация: справка для ИИ-агента\n\nДокументация Теплосферы хранит регламенты, инструкции по котлам и оборудованию, взрыв-схемы, спецификации запчастей, чек-листы, ADR и заметки. Сверяйся с документацией, прежде чем делать выводы по памяти.",
			sectionAccess(auth, level, spaces),
			sectionConnect(baseURL),
			sectionWorkflow(level),
			sectionTools(tools, auth == nil),
			sectionREST(baseURL, level),
			sectionRules(),
		}
		return strings.Join(parts, "\n\n") + "\n"
	default:
		// Базовый компактный ответ (как в КБ для get_agent_guide без topic).
		parts := []string{
			"# Теплосфера — документация: справка для ИИ-агента\n\nДокументация Теплосферы хранит регламенты, инструкции по котлам и оборудованию, взрыв-схемы, спецификации запчастей, чек-листы, ADR и заметки. Сверяйся с документацией, прежде чем делать выводы по памяти.",
			sectionAccess(auth, level, spaces),
			sectionWorkflow(level),
			sectionRules(),
			sectionTopics(),
		}
		return strings.Join(parts, "\n\n") + "\n"
	}
}

func sectionAccess(auth *platform.AuthContext, level accessLevel, spaces []docs.SpaceSummary) string {
	var b strings.Builder
	b.WriteString("## Доступ\n\n")
	switch level {
	case accessAnonymous:
		b.WriteString("Запрос выполнен без токена. Выпустите персональный API-токен в веб-интерфейсе «Пользователи → API-токены» и передавайте его в заголовке `Authorization: Bearer <токен>`.\n\n")
		b.WriteString("Уровни доступа токена (пересекаются с ролями владельца):\n")
		b.WriteString("- **Только вопросы (`ask_only`)** — доступны `get_agent_guide`, `list_spaces`, `ask`, `log_result`; прямой REST закрыт.\n")
		b.WriteString("- **Только чтение** — чтение пространств, документов, истории версий, вложений, схем, запчастей и комментариев, плюс гибридный поиск и `ask`.\n")
		b.WriteString("- **Чтение и запись** — всё чтение, плюс создание/обновление/дополнение документов, добавление примечаний и запись в журнал работ агентов.\n")
		b.WriteString("- **Ограничение пространствами** — токен может быть ограничен конкретным списком пространств (`allowed_spaces`).\n")
		b.WriteString("- **Запрещено всегда:** удаление документов/комментариев, управление пользователями, ролями, токенами и настройками.")
		return b.String()
	case accessAskOnly:
		b.WriteString("Токен «только вопросы»: доступны `get_agent_guide`, `list_spaces`, `ask` и `log_result`. Прямое чтение и редактирование документов через MCP и REST закрыты.")
	case accessReadOnly:
		b.WriteString("Токен «только чтение»: доступен поиск, чтение документов, истории версий, вложений, схем, запчастей, комментариев и `ask`. Запись и удаление запрещены.")
	case accessReadWrite:
		b.WriteString("Токен «чтение и запись»: всё чтение, плюс создание, обновление (с `expected_version`), дополнение документов, примечания и `log_result`. Удалять нельзя.")
	}

	if auth != nil && auth.IsToken && auth.TokenName != "" {
		fmt.Fprintf(&b, "\n\n- **Токен:** `%s` (владелец: `%s`)", auth.TokenName, auth.Username)
	}
	if auth != nil && auth.IsToken && len(auth.TokenAllowedSpaces) > 0 {
		fmt.Fprintf(&b, "\n- **Ограничение пространств:** `%s`", strings.Join(auth.TokenAllowedSpaces, "`, `"))
	} else {
		b.WriteString("\n- **Пространства:** все доступные владельцу")
	}
	if len(spaces) > 0 {
		names := make([]string, 0, len(spaces))
		for _, sp := range spaces {
			names = append(names, fmt.Sprintf("`%s` (%d)", sp.Space, sp.Count))
		}
		fmt.Fprintf(&b, "\n- **Доступные пространства сейчас:** %s", strings.Join(names, ", "))
	}
	return b.String()
}

func sectionConnect(baseURL string) string {
	var b strings.Builder
	b.WriteString("## Подключение\n\n")
	fmt.Fprintf(&b, "Основной способ — MCP-сервер `%s/mcp` (Streamable HTTP, JSON-RPC 2.0 без SSE, протокол `2025-06-18`).\n\n", baseURL)
	b.WriteString("Подключение в Claude Code:\n\n")
	b.WriteString("```bash\n")
	fmt.Fprintf(&b, "claude mcp add --transport http tsfera %s/mcp --header \"Authorization: Bearer <токен>\"\n", baseURL)
	b.WriteString("```\n\n")
	fmt.Fprintf(&b, "Справка по REST: `GET %s/api/agent-guide.md` (с заголовком `Authorization: Bearer <токен>` показывает только доступное этому токену).\n\n", baseURL)
	b.WriteString("Токен — секрет: не выводи его в чат, в файлы репозитория и в коммиты.")
	return b.String()
}

func sectionWorkflow(level accessLevel) string {
	var b strings.Builder
	b.WriteString("## Как работать\n\n")
	if level == accessAskOnly {
		b.WriteString("1. **Пространства:** вызови `list_spaces`, чтобы увидеть доступные разделы документации.\n")
		b.WriteString("2. **Вопросы:** задавай конкретный вопрос через `ask` — получишь выжимку со ссылками и `thread_id` для уточнений.\n")
		b.WriteString("3. **Итог работы:** заверши задачу записью в `log_result`.")
		return b.String()
	}
	b.WriteString("1. **Ориентация:** начни с `list_spaces` и `search` по теме задачи.\n")
	b.WriteString("2. **Чтение документа:** открой нужный документ целиком через `get_document` (обращай внимание на `version` и список вложений).\n")
	b.WriteString("3. **Вложения и схемы:** текст PDF/DOCX/XLSX читай через `list_attachments` → `read_attachment` (постранично через `offset`). Для электрических/гидравлических схем используй `inspect_schematic`, для запчастей и диагностики котлов — `search_boiler_parts` и `diagnose_boiler`.\n")
	b.WriteString("4. **Вопросы с синтезом:** если нужна быстрая выжимка по нескольким документам — используй `ask` (с `thread_id` для продолжения).\n")
	if level == accessReadWrite || level == accessAnonymous {
		b.WriteString("5. **Обновление документации:** при изменении документа передавай актуальный `expected_version` из `get_document` в `update_document` (при конфликте версий перечитай документ и повтори правку), либо допиши раздел через `append_to_document`.\n")
		b.WriteString("6. **Фиксация итогов:** после завершения задачи запиши краткий итог и принятые решения через `log_result`.")
	}
	return b.String()
}

func sectionTools(tools []mcpTool, isAnonymous bool) string {
	var b strings.Builder
	b.WriteString("## Инструменты MCP\n\n")
	if isAnonymous {
		b.WriteString("Полный список инструментов (конкретный токен видит только разрешённые ему в `tools/list`):\n\n")
	} else {
		b.WriteString("Инструменты, доступные этому токену (подробные параметры — в `tools/list`):\n\n")
	}
	b.WriteString("| Инструмент | Что делает |\n|---|---|\n")
	for _, t := range tools {
		desc := strings.ReplaceAll(t.Description, "\n", " ")
		fmt.Fprintf(&b, "| `%s` | %s |\n", t.Name, desc)
	}
	return b.String()
}

func sectionREST(baseURL string, level accessLevel) string {
	var b strings.Builder
	b.WriteString("## REST без MCP\n\n")
	if level == accessAskOnly {
		fmt.Fprintf(&b, "Токену «только вопросы» REST-эндпоинты документации закрыты (доступен только `GET %s/api/agent-guide.md` и `POST %s/mcp`).", baseURL, baseURL)
		return b.String()
	}
	b.WriteString("Заголовок: `Authorization: Bearer <токен>`.\n\n")
	fmt.Fprintf(&b, "- `GET %s/api/agent-guide.md` — эта справка (`?topic=all|access|connect|workflow|tools|rest|rules`)\n", baseURL)
	fmt.Fprintf(&b, "- `GET %s/api/v1/docs/spaces` — пространства документации\n", baseURL)
	fmt.Fprintf(&b, "- `GET %s/api/v1/docs/documents?space=&doc_type=&q=&limit=` — список документов\n", baseURL)
	fmt.Fprintf(&b, "- `GET %s/api/v1/docs/documents/{id}` — документ целиком\n", baseURL)
	fmt.Fprintf(&b, "- `GET %s/api/v1/docs/documents/{id}/revisions` — история версий\n", baseURL)
	fmt.Fprintf(&b, "- `GET %s/api/v1/docs/context.md?space=` — контекст документации одним Markdown-файлом\n", baseURL)
	fmt.Fprintf(&b, "- `GET %s/api/v1/ai/search?q=&limit=` — гибридный поиск (FTS + pgvector)\n", baseURL)
	if level == accessReadWrite || level == accessAnonymous {
		fmt.Fprintf(&b, "- `POST %s/api/v1/docs/documents` — создать документ\n", baseURL)
		fmt.Fprintf(&b, "- `PUT %s/api/v1/docs/documents/{id}` — обновить документ (обязателен `expected_version`)\n", baseURL)
		fmt.Fprintf(&b, "- `POST %s/api/v1/docs/documents/{id}/append` — дописать раздел в конец документа\n", baseURL)
		fmt.Fprintf(&b, "- `POST %s/api/v1/docs/documents/{id}/comments` — добавить примечание", baseURL)
	}
	return b.String()
}

func sectionRules() string {
	return `## Правила

- Удалять документы, комментарии и вложения агент не может — это делает человек в интерфейсе.
- Не придумывай факты, артикулы запчастей, клеммы и подключения — опирайся только на найденные документы, чертежи и схемы.
- При конфликте версий (` + "`expected_version`" + `) не затирай чужую правку: перечитай документ и повтори изменение.
- Каждая правка агента сохраняется в истории версий с пометкой ` + "`[агент <имя токена>]`" + `.`
}

func sectionTopics() string {
	return `## Другие разделы справки

Вызови ` + "`get_agent_guide`" + ` с параметром ` + "`topic`" + ` (` + "`all`" + ` — справка целиком):

- ` + "`access`" + ` — права и доступные пространства этого токена
- ` + "`connect`" + ` — подключение MCP и токен
- ` + "`workflow`" + ` — порядок работы с документацией
- ` + "`tools`" + ` — таблица инструментов MCP этого токена
- ` + "`rest`" + ` — REST API без MCP
- ` + "`rules`" + ` — правила безопасности и версионирования`
}
