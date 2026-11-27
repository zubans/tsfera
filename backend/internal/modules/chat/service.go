package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zubans/tsfera/backend/internal/modules/ai"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type LLMRunner interface {
	CompleteChat(ctx context.Context, auth *platform.AuthContext, req ai.CompletionRequest) (*ai.CompletionResponse, error)
	StreamChat(ctx context.Context, auth *platform.AuthContext, req ai.CompletionRequest, onDelta func(string) error) (*ai.CompletionResponse, error)
}

type KnowledgeSearcher interface {
	HybridSearch(ctx context.Context, auth *platform.AuthContext, query string, limit int) ([]ai.SearchResult, error)
}

type Service struct {
	repo     Repository
	llm      LLMRunner
	searcher KnowledgeSearcher
	resolver platform.EntityResolver
	tools    map[string]platform.ToolDefinition
	toolList []platform.ToolDefinition
	logger   *slog.Logger
	clock    func() time.Time
}

func NewService(
	repo Repository,
	llm LLMRunner,
	searcher KnowledgeSearcher,
	resolver platform.EntityResolver,
	logger *slog.Logger,
	tools ...platform.ToolDefinition,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	toolMap := make(map[string]platform.ToolDefinition, len(tools))
	for _, t := range tools {
		toolMap[t.Name] = t
	}
	return &Service{
		repo:     repo,
		llm:      llm,
		searcher: searcher,
		resolver: resolver,
		tools:    toolMap,
		toolList: tools,
		logger:   logger,
		clock:    time.Now,
	}
}

func (s *Service) RegisterTools(tools ...platform.ToolDefinition) {
	for _, t := range tools {
		if _, exists := s.tools[t.Name]; !exists {
			s.tools[t.Name] = t
			s.toolList = append(s.toolList, t)
		}
	}
}

func (s *Service) ListConversations(ctx context.Context, auth *platform.AuthContext, limit int) ([]Conversation, error) {
	return s.repo.ListConversations(ctx, auth.UserID, limit)
}

type CreateConversationInput struct {
	Title      string `json:"title"`
	Mode       Mode   `json:"mode"`
	ModelID    int64  `json:"model_id"`
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id"`
}

func (s *Service) CreateConversation(ctx context.Context, auth *platform.AuthContext, in CreateConversationInput) (*Conversation, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Новый диалог"
	}
	mode := in.Mode
	if mode != ModeAgent && mode != ModeRAG {
		mode = ModeAgent
	}
	conv := &Conversation{
		UserID:     auth.UserID,
		Title:      title,
		Mode:       mode,
		ModelID:    in.ModelID,
		EntityType: strings.TrimSpace(in.EntityType),
		EntityID:   in.EntityID,
	}
	if err := s.repo.CreateConversation(ctx, conv); err != nil {
		return nil, err
	}
	return conv, nil
}

func (s *Service) GetConversation(ctx context.Context, auth *platform.AuthContext, id int64) (*Conversation, error) {
	conv, err := s.repo.GetConversation(ctx, id)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, platform.NewNotFound("Диалог не найден")
	}
	if conv.UserID != auth.UserID && !auth.IsSuperAdmin {
		return nil, platform.NewForbidden("Нет доступа к чужому диалогу")
	}
	msgs, err := s.repo.ListMessages(ctx, conv.ID)
	if err != nil {
		return nil, err
	}
	conv.Messages = msgs
	return conv, nil
}

func (s *Service) DeleteConversation(ctx context.Context, auth *platform.AuthContext, id int64) error {
	conv, err := s.repo.GetConversation(ctx, id)
	if err != nil {
		return err
	}
	if conv == nil {
		return platform.NewNotFound("Диалог не найден")
	}
	if conv.UserID != auth.UserID && !auth.IsSuperAdmin {
		return platform.NewForbidden("Нет прав на удаление диалога")
	}
	return s.repo.SoftDeleteConversation(ctx, id)
}

func (s *Service) SendMessage(
	ctx context.Context,
	auth *platform.AuthContext,
	convID int64,
	userText string,
	overrideMode Mode,
	overrideModelID int64,
	emit func(SSEEvent),
) (*ChatMessage, error) {
	text := strings.TrimSpace(userText)
	if text == "" {
		return nil, platform.NewValidation("Сообщение не может быть пустым", map[string]string{"content": "Обязательное поле"})
	}
	conv, err := s.GetConversation(ctx, auth, convID)
	if err != nil {
		return nil, err
	}
	if s.llm == nil {
		return nil, platform.NewNotConfigured("LLM-шлюз не настроен")
	}

	userMsg := &ChatMessage{
		ConversationID: convID,
		Role:           "user",
		Content:        text,
	}
	if err := s.repo.AddMessage(ctx, userMsg); err != nil {
		return nil, err
	}
	if emit != nil {
		emit(SSEEvent{Type: "user", Message: userMsg})
	}

	mode := conv.Mode
	if overrideMode == ModeAgent || overrideMode == ModeRAG {
		mode = overrideMode
	}
	modelID := conv.ModelID
	if overrideModelID > 0 {
		modelID = overrideModelID
	}

	sysPrompt := s.buildSystemPrompt(ctx, auth, conv)
	llmMsgs := []ai.Message{{Role: "system", Content: sysPrompt}}
	// Передаём историю диалога (последние 16 сообщений для экономного контекста).
	history := conv.Messages
	if len(history) > 16 {
		history = history[len(history)-16:]
	}
	for _, h := range history {
		llmMsgs = append(llmMsgs, ai.Message{Role: h.Role, Content: h.Content})
	}
	llmMsgs = append(llmMsgs, ai.Message{Role: "user", Content: text})

	var assistantMsg *ChatMessage
	if mode == ModeRAG {
		assistantMsg, err = s.runRAG(ctx, auth, convID, modelID, text, llmMsgs, emit)
	} else {
		assistantMsg, err = s.runAgent(ctx, auth, convID, modelID, llmMsgs, emit)
	}
	if err != nil {
		if emit != nil {
			emit(SSEEvent{Type: "error", Error: err.Error()})
		}
		return nil, err
	}

	_ = s.repo.TouchConversation(ctx, convID)
	if emit != nil {
		emit(SSEEvent{Type: "done", Message: assistantMsg})
	}
	return assistantMsg, nil
}

func (s *Service) buildSystemPrompt(ctx context.Context, auth *platform.AuthContext, conv *Conversation) string {
	var b strings.Builder
	b.WriteString("Ты интеллектуальный ассистент сервисной CRM-системы «Теплосфера». Отвечай по-русски точно и опирайся только на факты из инструментов, базы знаний и CRM. Всегда проверяй права и не придумывай данные.")
	b.WriteString("\n\nСтрогие правила работы с чертежами котлов, запчастями, схемами и неисправностями:\n" +
		"- Если в контексте заявки указано несколько единиц оборудования (раздел «Оборудование в заявке»), всегда учитывай и перечисляй ВСЕ позиции заявки (или явно указывай, какие из них относятся к вопросу, а какие нет — например, котлы vs бойлеры), а также учитывай историю предыдущих заявок и актов по контрагенту.\n" +
		"- При КАЖДОМ новом или повторном вопросе про оборудование, запчасти или документацию ОБЯЗАТЕЛЬНО вызывай инструменты поиска заново — никогда не отвечай «документация отсутствует» только по истории прошлых сообщений диалога (база знаний могла пополниться).\n" +
		"- Если вопрос касается конкретной модели котла, запчасти, артикула (Material No.), позиции на чертеже или замены артикула — обязательно вызови docs_search_boiler_parts. Если docs_search_boiler_parts ничего не нашёл, ОБЯЗАТЕЛЬНО сделай fallback: вызови docs_search и ai_hybrid_search по короткому названию модели БЕЗ русских слов детали (например «BAXI EcoFour» или «EcoFour», а также заводской код котла), а для англоязычных каталогов пробуй также английские термины (PCB, pump, gas valve, heat exchanger, fan, pressure switch, sensor), после чего открой найденный документ через docs_get. В ответе указывай: модель котла, узел, номер позиции, точный артикул, пометки о комплектности/замене и ссылку на документ и страницу чертежа.\n" +
		"- Если вопрос касается неисправности, симптома или кода ошибки котла — вызови docs_diagnose_boiler (и docs_search для поиска регламентов/кодов ошибок). Чётко разделяй факты из загруженных чертежей/документов (с точными артикулами проверяемых узлов) и общие инженерные рекомендации, если таблица кодов ошибок для этой модели не загружена.\n" +
		"- Если вопрос касается электрических или гидравлических схем, взрыв-схем, клеммников, номеров контактов, цветов проводов или подключений — найди документ и вызови docs_inspect_schematic по attachment_id схемы (при мелких обозначениях укажи region: top_left / top_right / bottom_left / bottom_right / center).\n" +
		"- КАТЕГОРИЧЕСКИ ЗАПРЕЩЕНО придумывать, додумывать или подставлять по памяти артикулы запчастей, номера позиций, клемм, контактов, цвета проводов и соединения.\n" +
		"- Если на схеме или в базе знаний действительно нет точного ответа после проверки всех вариантов поиска, либо участок нечитаем — прямо скажи пользователю, что этих данных в загруженных чертежах/документации нет.")
	if conv.EntityType != "" && conv.EntityID > 0 && s.resolver != nil {
		if sum, err := s.resolver.ResolveEntity(ctx, auth, conv.EntityType, conv.EntityID); err == nil && sum != nil {
			fmt.Fprintf(&b, "\n\nПользователь открыл этот чат из карточки CRM (%s #%d):\n%s", conv.EntityType, conv.EntityID, sum.Markdown)
		}
	}
	return b.String()
}

func (s *Service) runRAG(
	ctx context.Context,
	auth *platform.AuthContext,
	convID, modelID int64,
	query string,
	llmMsgs []ai.Message,
	emit func(SSEEvent),
) (*ChatMessage, error) {
	var sources []platform.SourceRef
	if s.searcher != nil {
		results, err := s.searcher.HybridSearch(ctx, auth, query, 8)
		if err == nil && len(results) > 0 {
			var ctxBuf strings.Builder
			ctxBuf.WriteString("Релевантные фрагменты из базы знаний и CRM:\n")
			for i, r := range results {
				fmt.Fprintf(&ctxBuf, "[%d] %s (%s): %s\n", i+1, r.Title, r.Path, r.Content)
				sources = append(sources, platform.SourceRef{
					SourceType: r.SourceType,
					SourceID:   r.SourceID,
					Title:      r.Title,
					Anchor:     r.Anchor,
					Snippet:    r.Snippet,
					Path:       r.Path,
				})
			}
			// Вставляем найденный контекст перед последним вопросом пользователя.
			last := llmMsgs[len(llmMsgs)-1]
			llmMsgs = append(llmMsgs[:len(llmMsgs)-1], ai.Message{Role: "system", Content: ctxBuf.String()}, last)
		}
	}
	if emit != nil && len(sources) > 0 {
		emit(SSEEvent{Type: "sources", Sources: sources})
	}

	resp, err := s.llm.StreamChat(ctx, auth, ai.CompletionRequest{
		ModelID:   modelID,
		Messages:  llmMsgs,
		Operation: "chat_rag",
	}, func(delta string) error {
		if emit != nil {
			emit(SSEEvent{Type: "delta", Delta: delta})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	msg := &ChatMessage{
		ConversationID: convID,
		Role:           "assistant",
		Content:        resp.Content,
		Sources:        sources,
		TokensUsed:     resp.PromptTokens + resp.CompletionTokens,
	}
	if err := s.repo.AddMessage(ctx, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func (s *Service) runAgent(
	ctx context.Context,
	auth *platform.AuthContext,
	convID, modelID int64,
	llmMsgs []ai.Message,
	emit func(SSEEvent),
) (*ChatMessage, error) {
	var steps []ToolStep
	var allSources []platform.SourceRef
	sourceSeen := map[string]bool{}
	totalTokens := 0
	var finalContent string

	for round := 0; round < MaxAgentRounds; round++ {
		resp, err := s.llm.CompleteChat(ctx, auth, ai.CompletionRequest{
			ModelID:   modelID,
			Messages:  llmMsgs,
			Tools:     s.toolList,
			Operation: "chat_agent",
		})
		if err != nil {
			return nil, err
		}
		totalTokens += resp.PromptTokens + resp.CompletionTokens
		if len(resp.ToolCalls) == 0 {
			finalContent = resp.Content
			break
		}

		llmMsgs = append(llmMsgs, ai.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			tStarted := time.Now()
			tool, ok := s.tools[tc.Name]
			step := ToolStep{
				Name:      tc.Name,
				Arguments: string(tc.Arguments),
			}
			var toolContent string
			if !ok {
				step.Error = "неизвестный инструмент: " + tc.Name
				step.Summary = step.Error
				toolContent = step.Error
			} else {
				res, execErr := tool.Execute(ctx, auth, tc.Arguments)
				if execErr != nil {
					step.Error = execErr.Error()
					step.Summary = "Ошибка: " + execErr.Error()
					toolContent = step.Summary
				} else {
					toolContent = res.Content
					step.Summary = ai.MakeSnippet(res.Content, 140)
					for _, src := range res.Sources {
						key := src.SourceType + ":" + src.SourceID + ":" + src.Path
						if !sourceSeen[key] {
							sourceSeen[key] = true
							allSources = append(allSources, src)
						}
					}
				}
			}
			step.DurationMs = time.Since(tStarted).Milliseconds()
			steps = append(steps, step)
			if emit != nil {
				stCopy := step
				emit(SSEEvent{Type: "step", Step: &stCopy})
			}
			llmMsgs = append(llmMsgs, ai.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Content:    toolContent,
			})
		}
	}

	// Если цикл исчерпал лимит раундов без финального текста — просим подвести итог без инструментов.
	if strings.TrimSpace(finalContent) == "" {
		resp, err := s.llm.StreamChat(ctx, auth, ai.CompletionRequest{
			ModelID:   modelID,
			Messages:  append(llmMsgs, ai.Message{Role: "user", Content: "Подведи краткий итог по собранным данным."}),
			Operation: "chat_agent_final",
		}, func(delta string) error {
			if emit != nil {
				emit(SSEEvent{Type: "delta", Delta: delta})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		finalContent = resp.Content
		totalTokens += resp.PromptTokens + resp.CompletionTokens
	} else if emit != nil {
		emit(SSEEvent{Type: "delta", Delta: finalContent})
	}

	if emit != nil && len(allSources) > 0 {
		emit(SSEEvent{Type: "sources", Sources: allSources})
	}

	msg := &ChatMessage{
		ConversationID: convID,
		Role:           "assistant",
		Content:        finalContent,
		Steps:          steps,
		Sources:        allSources,
		TokensUsed:     totalTokens,
	}
	if err := s.repo.AddMessage(ctx, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// --- Sharing ---

func (s *Service) CreateShare(ctx context.Context, auth *platform.AuthContext, convID int64, ttl time.Duration) (*ConversationShare, error) {
	conv, err := s.GetConversation(ctx, auth, convID)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	share := &ConversationShare{
		ConversationID: conv.ID,
		OwnerID:        auth.UserID,
		ShareToken:     hex.EncodeToString(buf),
	}
	if ttl > 0 {
		exp := s.clock().Add(ttl)
		share.ExpiresAt = &exp
	}
	if err := s.repo.CreateShare(ctx, share); err != nil {
		return nil, err
	}
	return share, nil
}

func (s *Service) RevokeConversationShares(ctx context.Context, auth *platform.AuthContext, convID int64) error {
	return s.repo.RevokeSharesByConversation(ctx, convID, auth.UserID)
}

func (s *Service) GetSharedConversation(ctx context.Context, token string) (*Conversation, error) {
	share, err := s.repo.GetShareByToken(ctx, strings.TrimSpace(token))
	if err != nil {
		return nil, err
	}
	if share == nil || share.Revoked {
		return nil, platform.NewNotFound("Ссылка на диалог не найдена или отозвана")
	}
	if share.ExpiresAt != nil && s.clock().After(*share.ExpiresAt) {
		return nil, platform.NewNotFound("Срок действия ссылки на диалог истёк")
	}
	conv, err := s.repo.GetConversation(ctx, share.ConversationID)
	if err != nil || conv == nil {
		return nil, platform.NewNotFound("Диалог удалён")
	}
	msgs, err := s.repo.ListMessages(ctx, conv.ID)
	if err != nil {
		return nil, err
	}
	conv.Messages = msgs
	return conv, nil
}

func (s *Service) RevokeOwnerShares(ctx context.Context, ownerID int64) error {
	return s.repo.RevokeSharesByOwner(ctx, ownerID)
}
