package ai

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// Текстовый протокол инструментов для консольных агентов и шлюза: у них нет нативного вызова
// инструментов, поэтому схемы передаются в системном промпте, а вызовы — блоком
// <tool_calls>[{"name": "…", "arguments": {…}}]</tool_calls> в конце ответа (как в KB).

const (
	toolsOpen  = "<tool_calls>"
	toolsClose = "</tool_calls>"
	// continuePrompt завершает расшифровку диалога: агент получает её одним сообщением.
	continuePrompt = "Продолжи диалог: напиши следующее сообщение ассистента."
)

type textToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// toolsPrompt описывает инструменты и правила вызова для модели без нативных tools.
func toolsPrompt(tools []platform.ToolDefinition) string {
	defs := make([]textToolDef, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, textToolDef{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	raw, _ := json.MarshalIndent(defs, "", "  ")
	return "Доступные инструменты (JSON Schema параметров):\n" + string(raw) + `

Собственных инструментов у тебя нет: чтение файлов, поиск, терминал и правка файлов отключены и будут отклонены.
Данные CRM и документацию получай только инструментами из этого списка — через блок <tool_calls>.

Чтобы вызвать инструменты, закончи ответ блоком
<tool_calls>[{"name": "…", "arguments": {…}}]</tool_calls>
и больше ничего после него не пиши: результаты придут в следующем сообщении в блоках <tool_result>.
Можно вызвать несколько инструментов сразу. Когда информации достаточно — дай окончательный ответ без блока <tool_calls>.`
}

// splitSystem отделяет системные сообщения и добавляет к ним описание инструментов.
func splitSystem(msgs []Message, tools []platform.ToolDefinition) (string, []Message) {
	var system []string
	var dialog []Message
	for _, m := range msgs {
		if m.Role == "system" {
			if strings.TrimSpace(m.Content) != "" {
				system = append(system, m.Content)
			}
			continue
		}
		dialog = append(dialog, m)
	}
	if len(tools) > 0 {
		system = append(system, toolsPrompt(tools))
	}
	return strings.Join(system, "\n\n"), dialog
}

// renderMessage переводит сообщение в текстовую разметку расшифровки диалога.
func renderMessage(m Message) string {
	switch m.Role {
	case "tool":
		return fmt.Sprintf("<tool_result name=%q>\n%s\n</tool_result>", m.ToolName, m.Content)
	case "assistant":
		var b strings.Builder
		b.WriteString("<assistant>\n")
		b.WriteString(m.Content)
		if len(m.ToolCalls) > 0 {
			type call struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			calls := make([]call, 0, len(m.ToolCalls))
			for _, c := range m.ToolCalls {
				args := c.Arguments
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				calls = append(calls, call{Name: c.Name, Arguments: args})
			}
			raw, _ := json.Marshal(calls)
			b.WriteString("\n" + toolsOpen + string(raw) + toolsClose)
		}
		b.WriteString("\n</assistant>")
		return b.String()
	default:
		return "<user>\n" + m.Content + "\n</user>"
	}
}

// transcript — весь диалог одним текстом для агента, который каждый раз запускается заново.
func transcript(prefix string, dialog []Message, suffix string) string {
	var b strings.Builder
	b.WriteString(prefix)
	for _, m := range dialog {
		b.WriteString(renderMessage(m))
		b.WriteString("\n\n")
	}
	b.WriteString(suffix)
	return b.String()
}

// gatewayMessages готовит сообщения для шлюза: роли только system/user/assistant, результаты
// инструментов — в сообщении пользователя, вызовы ассистента — текстом.
func gatewayMessages(system string, dialog []Message) []map[string]string {
	out := []map[string]string{}
	if system != "" {
		out = append(out, map[string]string{"role": "system", "content": system})
	}
	for _, m := range dialog {
		role := m.Role
		content := m.Content
		switch m.Role {
		case "tool":
			role = "user"
			content = renderMessage(m)
		case "assistant":
			if len(m.ToolCalls) > 0 {
				content = strings.TrimSuffix(strings.TrimPrefix(renderMessage(m), "<assistant>\n"), "\n</assistant>")
			}
		default:
			role = "user"
		}
		// Подряд идущие сообщения пользователя (несколько результатов инструментов) склеиваем.
		if n := len(out); n > 0 && out[n-1]["role"] == role && role == "user" {
			out[n-1]["content"] += "\n\n" + content
			continue
		}
		out = append(out, map[string]string{"role": role, "content": content})
	}
	return out
}

// parseToolCalls выделяет из ответа блок <tool_calls> и возвращает видимый текст и вызовы.
// Допускается один объект вместо списка и отсутствие закрывающего тега, если JSON цел.
func parseToolCalls(text string) (string, []ToolCall, error) {
	start := strings.Index(text, toolsOpen)
	if start < 0 {
		return text, nil, nil
	}
	body := text[start+len(toolsOpen):]
	before := strings.TrimSpace(text[:start])
	after := ""
	if end := strings.Index(body, toolsClose); end >= 0 {
		after = strings.TrimSpace(body[end+len(toolsClose):])
		body = body[:end]
	} else if raw, ok := leadingJSON(strings.TrimSpace(body)); ok {
		body = raw
	}
	calls, err := decodeCalls(strings.TrimSpace(body))
	if err != nil {
		return text, nil, platform.NewValidation("Не разобрать вызов инструментов в ответе модели: "+err.Error(), nil)
	}
	visible := before
	if after != "" && !strings.HasPrefix(after, "<tool_result") {
		visible = strings.TrimSpace(before + "\n" + after)
	}
	return visible, calls, nil
}

func decodeCalls(raw string) ([]ToolCall, error) {
	type rawCall struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	var list []rawCall
	if strings.HasPrefix(raw, "{") {
		var one rawCall
		if err := json.Unmarshal([]byte(raw), &one); err != nil {
			return nil, err
		}
		list = []rawCall{one}
	} else if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	out := make([]ToolCall, 0, len(list))
	for i, c := range list {
		if strings.TrimSpace(c.Name) == "" {
			return nil, fmt.Errorf("вызов без имени инструмента")
		}
		args := c.Arguments
		if len(args) == 0 || string(args) == "null" {
			args = json.RawMessage("{}")
		}
		out = append(out, ToolCall{ID: fmt.Sprintf("call_%d_%s", i, randomHex(4)), Name: c.Name, Arguments: args})
	}
	return out, nil
}

// leadingJSON возвращает сбалансированный JSON-список или объект в начале строки.
func leadingJSON(s string) (string, bool) {
	if s == "" || (s[0] != '[' && s[0] != '{') {
		return "", false
	}
	depth := 0
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return s[:i+1], true
			}
		}
	}
	return "", false
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// finishTextTools разбирает ответ модели с текстовыми инструментами в CompletionResponse.
// Если инструменты не запрашивались, блок не разбирается: текст отдаётся как есть.
func finishTextTools(text string, withTools bool) (*CompletionResponse, error) {
	resp := &CompletionResponse{Content: strings.TrimSpace(text)}
	if !withTools {
		return resp, nil
	}
	visible, calls, err := parseToolCalls(text)
	if err != nil {
		return nil, err
	}
	resp.Content = visible
	resp.ToolCalls = calls
	return resp, nil
}
