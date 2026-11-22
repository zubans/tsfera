package ai

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// Консольные агенты (Claude Code, Codex, Antigravity) запускаются процессом внутри контейнера tsfera,
// как в KB: у каждого провайдера свой каталог HOME, где лежит вход в аккаунт. Рабочая папка —
// этот каталог, собственные инструменты агента отключены, данные он получает только через <tool_calls>.
// Сессии CLI не сохраняются: каждый вызов получает всю расшифровку диалога.

const (
	defaultCLITimeout   = 300 * time.Second
	cliMaxParallel      = 2
	cliStderrKeepBytes  = 16 << 10
	cliMaxLineBytes     = 16 << 20
	codexOwnToolsNotice = "Собственные инструменты и файловая система Codex здесь отключены и не относятся к задаче: не используй их. Все инструменты работают только через <tool_calls>."
	agyOwnToolsNotice   = "Собственные инструменты недоступны — данные получай только через <tool_calls>."
)

// cliSpec — запуск процесса консольного агента.
type cliSpec struct {
	Bin   string
	Args  []string
	Env   []string
	Dir   string
	Stdin string
}

// cliProcess запускает агента и отдаёт каждую строку stdout; подменяется в тестах.
type cliProcess interface {
	Run(ctx context.Context, spec cliSpec, onLine func([]byte)) (stderr string, err error)
}

// errCLINotFound — бинарника агента нет в образе.
var errCLINotFound = errors.New("cli not found")

type execProcess struct{}

func (execProcess) Run(ctx context.Context, spec cliSpec, onLine func([]byte)) (string, error) {
	cmd := exec.CommandContext(ctx, spec.Bin, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = strings.NewReader(spec.Stdin)
	setProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr tailBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return "", errCLINotFound
		}
		return "", err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), cliMaxLineBytes)
	for sc.Scan() {
		if line := bytes.TrimSpace(sc.Bytes()); len(line) > 0 {
			onLine(line)
		}
	}
	// Дочитываем остаток, чтобы процесс не завис на заполненном канале.
	_, _ = io.Copy(io.Discard, stdout)
	err = cmd.Wait()
	if ctx.Err() != nil {
		return stderr.String(), ctx.Err()
	}
	return stderr.String(), err
}

// tailBuffer хранит только хвост stderr — его хватает для текста ошибки.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > cliStderrKeepBytes {
		t.buf = t.buf[len(t.buf)-cliStderrKeepBytes:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// cliBinary — имя исполняемого файла агента.
func cliBinary(k ProviderKind) string {
	switch k {
	case ProviderClaudeCLI:
		return "claude"
	case ProviderCodexCLI:
		return "codex"
	case ProviderAntigravityCLI:
		return "agy"
	}
	return ""
}

// cliLoginHint — как войти в аккаунт агента на сервере.
func cliLoginHint(k ProviderKind) string {
	switch k {
	case ProviderClaudeCLI:
		return "укажите токен из `claude setup-token` в поле ключа провайдера или выполните make claude-login"
	case ProviderCodexCLI:
		return "выполните make codex-login или укажите ключ API в поле ключа провайдера"
	case ProviderAntigravityCLI:
		return "выполните make agy-login"
	}
	return ""
}

// cliHomeFor — каталог входа провайдера: свой путь из настроек или <каталог CLI>/<тип>.
func (g *Gateway) cliHomeFor(p *Provider) string {
	if dir := strings.TrimSpace(p.BaseURL); dir != "" && filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(g.cliHome, string(p.Kind))
}

func providerTimeout(p *Provider) time.Duration {
	if p.TimeoutSeconds > 0 {
		return time.Duration(p.TimeoutSeconds) * time.Second
	}
	return defaultCLITimeout
}

// acquire ограничивает число одновременных процессов одного провайдера.
func (g *Gateway) acquire(ctx context.Context, providerID int64) (func(), error) {
	g.semMu.Lock()
	sem, ok := g.sems[providerID]
	if !ok {
		sem = make(chan struct{}, cliMaxParallel)
		g.sems[providerID] = sem
	}
	g.semMu.Unlock()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, platform.NewNotConfigured("Консольный агент занят другими запросами, повторите позже")
	}
}

func (g *Gateway) cliEnv(p *Provider, home string) []string {
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "TZ=Europe/Moscow"}
	if g.cliProxy != "" {
		env = append(env, "HTTPS_PROXY="+g.cliProxy, "HTTP_PROXY="+g.cliProxy, "https_proxy="+g.cliProxy, "http_proxy="+g.cliProxy,
			"NO_PROXY=localhost,127.0.0.1", "no_proxy=localhost,127.0.0.1")
	}
	token := strings.TrimSpace(p.APIKey)
	switch p.Kind {
	case ProviderClaudeCLI:
		env = append(env, "DISABLE_AUTOUPDATER=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1")
		if token != "" {
			if isAnthropicAPIKey(token) {
				env = append(env, "ANTHROPIC_API_KEY="+token)
			} else {
				// Токен подписки из `claude setup-token`.
				env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+token)
			}
		}
	case ProviderCodexCLI:
		env = append(env, "CODEX_HOME="+home)
		if token != "" {
			env = append(env, "CODEX_API_KEY="+token)
		}
	}
	return env
}

func isAnthropicAPIKey(token string) bool {
	return strings.HasPrefix(token, "sk-ant-api")
}

// runCLI запускает агента и переводит типовые сбои в понятные ошибки.
func (g *Gateway) runCLI(ctx context.Context, p *Provider, args []string, stdin string, onEvent func(map[string]any)) error {
	home := g.cliHomeFor(p)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return platform.NewNotConfigured(fmt.Sprintf("%s: не создать каталог входа %s: %v", p.Name, home, err))
	}
	spec := cliSpec{Bin: cliBinary(p.Kind), Args: args, Env: g.cliEnv(p, home), Dir: home, Stdin: stdin}
	stderr, err := g.proc.Run(ctx, spec, func(line []byte) {
		var ev map[string]any
		if json.Unmarshal(line, &ev) == nil {
			onEvent(ev)
		} else {
			onEvent(map[string]any{"_raw": string(line)})
		}
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errCLINotFound):
		return platform.NewNotConfigured(fmt.Sprintf("%s: %s не найден в контейнере tsfera. Соберите образ с консольными агентами: make build-ai-cli && make up", p.Name, spec.Bin))
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s: нет ответа за %d с — увеличьте таймаут провайдера", p.Name, int(providerTimeout(p).Seconds()))
	case errors.Is(err, context.Canceled):
		return err
	default:
		msg := strings.TrimSpace(redact(stderr, p.APIKey))
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s: %s. Если не выполнен вход — %s", p.Name, msg, cliLoginHint(p.Kind))
	}
}

func redact(text, secret string) string {
	if s := strings.TrimSpace(secret); len(s) >= 8 {
		text = strings.ReplaceAll(text, s, "***")
	}
	return text
}

// callCLIChat выполняет ход консольного агента. onDelta получает текст по мере генерации
// (только без инструментов: блок <tool_calls> пользователю не показывается).
func (g *Gateway) callCLIChat(ctx context.Context, p *Provider, m *Model, req CompletionRequest, onDelta func(string) error) (*CompletionResponse, error) {
	release, err := g.acquire(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, providerTimeout(p))
	defer cancel()

	withTools := len(req.Tools) > 0
	system, dialog := splitSystem(req.Messages, req.Tools)
	var deltaErr error
	emit := func(s string) {
		if withTools || onDelta == nil || s == "" || deltaErr != nil {
			return
		}
		deltaErr = onDelta(s)
	}
	var text string
	var in, out int
	switch p.Kind {
	case ProviderClaudeCLI:
		text, in, out, err = g.runClaude(ctx, p, m, system, dialog, emit)
	case ProviderCodexCLI:
		text, in, out, err = g.runCodex(ctx, p, m, system, dialog, withTools, emit)
	case ProviderAntigravityCLI:
		text, in, out, err = g.runAgy(ctx, p, m, system, dialog, withTools, emit)
	default:
		return nil, platform.NewValidation("Неподдерживаемый консольный агент: "+string(p.Kind), nil)
	}
	if err != nil {
		return nil, err
	}
	if deltaErr != nil {
		return nil, deltaErr
	}
	resp, err := finishTextTools(text, withTools)
	if err != nil {
		return nil, err
	}
	resp.PromptTokens = in
	resp.CompletionTokens = out
	return resp, nil
}

// --- Claude Code ---

func (g *Gateway) runClaude(ctx context.Context, p *Provider, m *Model, system string, dialog []Message, emit func(string)) (string, int, int, error) {
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--include-partial-messages", "--model", m.ModelID, "--tools", "", "--strict-mcp-config", "--no-session-persistence"}
	if isAnthropicAPIKey(strings.TrimSpace(p.APIKey)) {
		// Быстрый старт без хуков и плагинов; с токеном подписки --bare не работает.
		args = append(args, "--bare")
	}
	if system != "" {
		file, err := writePromptFile(g.cliHomeFor(p), system)
		if err != nil {
			return "", 0, 0, err
		}
		args = append(args, "--system-prompt-file", file)
	}
	input := jsonLine(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]string{{"type": "text", "text": transcript("", dialog, continuePrompt)}},
		},
	})
	var result map[string]any
	err := g.runCLI(ctx, p, args, input, func(ev map[string]any) {
		switch str(ev["type"]) {
		case "result":
			result = ev
		case "stream_event":
			event, _ := ev["event"].(map[string]any)
			delta, _ := event["delta"].(map[string]any)
			if str(delta["type"]) == "text_delta" {
				emit(str(delta["text"]))
			}
		}
	})
	if err != nil {
		return "", 0, 0, err
	}
	if result == nil {
		return "", 0, 0, fmt.Errorf("%s: Claude Code завершился без результата. Если не выполнен вход — %s", p.Name, cliLoginHint(p.Kind))
	}
	if isErr, _ := result["is_error"].(bool); isErr {
		msg := str(result["result"])
		if msg == "" {
			msg = str(result["subtype"])
		}
		return "", 0, 0, fmt.Errorf("%s: %s. Проверьте токен из `claude setup-token` в настройках провайдера", p.Name, truncate(redact(msg, p.APIKey), 500))
	}
	usage, _ := result["usage"].(map[string]any)
	in := num(usage["input_tokens"]) + num(usage["cache_read_input_tokens"]) + num(usage["cache_creation_input_tokens"])
	return str(result["result"]), in, num(usage["output_tokens"]), nil
}

// writePromptFile кладёт системный промпт в файл: Claude Code читает его при старте.
func writePromptFile(home, system string) (string, error) {
	dir := filepath.Join(home, "tsfera-prompts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(system))
	file := filepath.Join(dir, hex.EncodeToString(sum[:])+".md")
	now := time.Now()
	if _, err := os.Stat(file); err == nil {
		// Свежая дата: уборка ниже в соседнем запросе не удалит файл, который сейчас прочтёт CLI.
		_ = os.Chtimes(file, now, now)
	} else {
		tmp := file + "." + randomHex(4)
		if err := os.WriteFile(tmp, []byte(system), 0o600); err != nil {
			return "", err
		}
		if err := os.Rename(tmp, file); err != nil {
			_ = os.Remove(tmp)
			return "", err
		}
	}
	if old, err := filepath.Glob(filepath.Join(dir, "*.md")); err == nil {
		for _, f := range old {
			if info, err := os.Stat(f); err == nil && f != file && now.Sub(info.ModTime()) > 24*time.Hour {
				_ = os.Remove(f)
			}
		}
	}
	return file, nil
}

// --- Codex ---

func (g *Gateway) runCodex(ctx context.Context, p *Provider, m *Model, system string, dialog []Message, withTools bool, emit func(string)) (string, int, int, error) {
	prefix := ""
	if system != "" {
		prefix = "<system>\n" + system + "\n</system>\n\n"
	}
	suffix := continuePrompt
	if withTools {
		suffix += " " + codexOwnToolsNotice
	}
	home := g.cliHomeFor(p)
	args := []string{"exec", "--json", "--color", "never", "--sandbox", "read-only", "--skip-git-repo-check",
		"--ignore-rules", "--ignore-user-config", "--cd", home, "--model", m.ModelID, "--ephemeral", "-"}
	var answer, failure string
	var in, out int
	err := g.runCLI(ctx, p, args, transcript(prefix, dialog, suffix), func(ev map[string]any) {
		if usage, ok := ev["usage"].(map[string]any); ok {
			in = num(usage["input_tokens"]) + num(usage["cached_input_tokens"])
			out = num(usage["output_tokens"]) + num(usage["reasoning_output_tokens"])
		}
		switch str(ev["type"]) {
		case "turn.failed":
			if e, ok := ev["error"].(map[string]any); ok {
				failure = str(e["message"])
			} else {
				failure = str(ev["error"])
			}
			if failure == "" {
				failure = "Codex не завершил ход"
			}
		case "item.completed":
			item, _ := ev["item"].(map[string]any)
			if str(item["type"]) != "agent_message" {
				return
			}
			if text := codexMessageText(item); text != "" {
				answer = text
				emit(text)
			}
		}
	})
	if err != nil {
		return "", 0, 0, err
	}
	if failure != "" {
		return "", 0, 0, fmt.Errorf("%s: %s", p.Name, truncate(redact(failure, p.APIKey), 500))
	}
	return answer, in, out, nil
}

func codexMessageText(item map[string]any) string {
	if s, ok := item["text"].(string); ok {
		return s
	}
	parts, _ := item["content"].([]any)
	var b strings.Builder
	for _, part := range parts {
		switch v := part.(type) {
		case string:
			b.WriteString(v)
		case map[string]any:
			b.WriteString(str(v["text"]))
		}
	}
	return b.String()
}

// --- Antigravity ---

func (g *Gateway) runAgy(ctx context.Context, p *Provider, m *Model, system string, dialog []Message, withTools bool, emit func(string)) (string, int, int, error) {
	if err := prepareAgyHome(g.cliHomeFor(p)); err != nil {
		return "", 0, 0, err
	}
	prefix := ""
	if system != "" {
		prefix = "<system>\n" + system + "\n</system>\n\n"
	}
	suffix := continuePrompt
	if withTools {
		suffix += " " + agyOwnToolsNotice
	}
	args := []string{"--input-format", "stream-json", "--output-format", "stream-json"}
	if name := agyModelName(m.ModelID); name != "" {
		args = append(args, "--model", name)
	}
	input := jsonLine(map[string]any{"event": "user", "message": map[string]string{"content": transcript(prefix, dialog, suffix)}})
	var result map[string]any
	var streamed strings.Builder
	err := g.runCLI(ctx, p, args, input, func(ev map[string]any) {
		switch str(ev["event"]) {
		case "result":
			result, _ = ev["result"].(map[string]any)
		case "step_update":
			step, _ := ev["step_update"].(map[string]any)
			if str(step["step_type"]) == "agent_response" {
				if d := str(step["text_delta"]); d != "" {
					streamed.WriteString(d)
					emit(d)
				}
			}
		}
	})
	if err != nil {
		return "", 0, 0, err
	}
	if result == nil {
		return "", 0, 0, fmt.Errorf("%s: Antigravity завершился без результата. Если не выполнен вход — %s", p.Name, cliLoginHint(p.Kind))
	}
	if str(result["status"]) != "SUCCESS" {
		msg := str(result["error"])
		if msg == "" {
			msg = str(result["status"])
		}
		return "", 0, 0, fmt.Errorf("%s: %s. Если не выполнен вход — %s", p.Name, truncate(redact(msg, p.APIKey), 500), cliLoginHint(p.Kind))
	}
	answer := strings.TrimSpace(str(result["response"]))
	if answer == "" {
		answer = streamed.String()
	}
	usage, _ := result["usage"].(map[string]any)
	return answer, num(usage["input_tokens"]), num(usage["output_tokens"]) + num(usage["thinking_tokens"]), nil
}

// prepareAgyHome запрещает агенту собственные инструменты и доступ вне рабочей папки.
func prepareAgyHome(home string) error {
	dir := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file := filepath.Join(dir, "settings.json")
	settings := map[string]any{}
	if raw, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	if settings["toolPermission"] == "strict" && settings["allowNonWorkspaceAccess"] == false {
		return nil
	}
	settings["toolPermission"] = "strict"
	settings["allowNonWorkspaceAccess"] = false
	raw, _ := json.MarshalIndent(settings, "", "  ")
	return os.WriteFile(file, raw, 0o600)
}

// agyModelName берёт имя модели из строки списка `agy models` (колонки через табуляцию).
func agyModelName(line string) string {
	var cols []string
	for _, c := range strings.Split(line, "\t") {
		if c = strings.TrimSpace(c); c != "" {
			cols = append(cols, c)
		}
	}
	if len(cols) > 1 {
		return cols[1]
	}
	if len(cols) == 1 {
		return cols[0]
	}
	return ""
}

// --- Список моделей ---

// ListCLIModels возвращает модели, доступные консольному агенту после входа в аккаунт.
func (g *Gateway) listCLIModels(ctx context.Context, p *Provider) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var lines []string
	collect := func(ev map[string]any) {
		if raw, ok := ev["_raw"].(string); ok {
			lines = append(lines, raw)
			return
		}
		b, _ := json.Marshal(ev)
		lines = append(lines, string(b))
	}
	switch p.Kind {
	case ProviderClaudeCLI:
		if err := g.runCLI(ctx, p, []string{"--version"}, "", collect); err != nil {
			return nil, err
		}
		return []string{"opus", "sonnet", "haiku"}, nil
	case ProviderCodexCLI:
		if err := g.runCLI(ctx, p, []string{"debug", "models"}, "", collect); err != nil {
			return nil, err
		}
		models := parseCodexModels(strings.Join(lines, "\n"))
		if len(models) == 0 {
			return nil, fmt.Errorf("%s: Codex не вернул ни одной модели — %s", p.Name, cliLoginHint(p.Kind))
		}
		return models, nil
	case ProviderAntigravityCLI:
		if err := prepareAgyHome(g.cliHomeFor(p)); err != nil {
			return nil, err
		}
		if err := g.runCLI(ctx, p, []string{"models"}, "", collect); err != nil {
			return nil, err
		}
		models := parseAgyModels(lines)
		if len(models) == 0 {
			return nil, fmt.Errorf("%s: agy models не вернул ни одной модели — %s", p.Name, cliLoginHint(p.Kind))
		}
		return models, nil
	}
	return nil, platform.NewValidation("Неподдерживаемый консольный агент", nil)
}

func parseCodexModels(output string) []string {
	var data struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if json.Unmarshal([]byte(output), &data) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range data.Models {
		if m.Slug == "" || (m.Visibility != "" && m.Visibility != "list") || seen[m.Slug] {
			continue
		}
		seen[m.Slug] = true
		out = append(out, m.Slug)
	}
	return out
}

func parseAgyModels(lines []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range lines {
		name := agyModelName(line)
		lower := strings.ToLower(name)
		if name == "" || strings.HasSuffix(name, ":") || lower == "name" || strings.HasPrefix(lower, "available") ||
			strings.HasPrefix(lower, "models") || strings.HasPrefix(lower, "доступн") || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// jsonLine — строка NDJSON без экранирования <, > и & (разметка диалога остаётся читаемой для агента).
func jsonLine(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return b.String()
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
