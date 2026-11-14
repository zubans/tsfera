package ai

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// DocFetcher позволяет AI Compose и инструментам читать текст документа с проверкой RBAC без жёсткой связности.
type DocFetcher interface {
	FetchDocumentMarkdown(ctx context.Context, auth *platform.AuthContext, docID int64) (string, error)
}

type SearchAndComposeService struct {
	repo     Repository
	gateway  *Gateway
	sources  []platform.IndexSource
	resolver platform.EntityResolver
	docs     DocFetcher
	logger   *slog.Logger
}

func NewSearchAndComposeService(
	repo Repository,
	gateway *Gateway,
	sources []platform.IndexSource,
	resolver platform.EntityResolver,
	docs DocFetcher,
	logger *slog.Logger,
) *SearchAndComposeService {
	if logger == nil {
		logger = slog.Default()
	}
	return &SearchAndComposeService{
		repo:     repo,
		gateway:  gateway,
		sources:  sources,
		resolver: resolver,
		docs:     docs,
		logger:   logger,
	}
}

func (s *SearchAndComposeService) RegisterSources(sources ...platform.IndexSource) {
	s.sources = append(s.sources, sources...)
}

func (s *SearchAndComposeService) SetEntityResolver(r platform.EntityResolver) {
	s.resolver = r
}

func (s *SearchAndComposeService) SetDocFetcher(d DocFetcher) {
	s.docs = d
}

// ReindexAll синхронизирует все зарегистрированные IndexSource с перерасчётом чанков только при изменении SHA-256 хеша.
func (s *SearchAndComposeService) ReindexAll(ctx context.Context) (int, error) {
	indexedItems := 0
	for _, src := range s.sources {
		items, err := src.ListForIndex(ctx, nil)
		if err != nil {
			s.logger.Warn("index source list failed", "source", src.SourceType(), "error", err)
			continue
		}
		for _, item := range items {
			chunks := ChunkItem(item, DefaultChunkMaxChars)
			newHashes := make([]string, len(chunks))
			for i, ch := range chunks {
				newHashes[i] = ch.ContentHash
			}
			oldHashes, err := s.repo.ListChunkHashes(ctx, item.SourceType, item.SourceID)
			if err == nil && slices.Equal(oldHashes, newHashes) {
				continue
			}
			for i := range chunks {
				if emb, err := s.gateway.Embed(ctx, chunks[i].Title+"\n"+chunks[i].Content); err == nil && len(emb) > 0 {
					chunks[i].Embedding = emb
				}
			}
			if err := s.repo.ReplaceSourceChunks(ctx, item.SourceType, item.SourceID, chunks); err != nil {
				s.logger.Warn("replace chunks failed", "source", item.SourceType, "id", item.SourceID, "error", err)
				continue
			}
			indexedItems++
		}
	}
	return indexedItems, nil
}

// HybridSearch выполняет FTS + векторный поиск, объединяет RRF (k=60) и фильтрует через IndexSource.CanView.
func (s *SearchAndComposeService) HybridSearch(ctx context.Context, auth *platform.AuthContext, query string, limit int) ([]SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, platform.NewValidation("Поисковый запрос пуст", map[string]string{"q": "Укажите текст поиска"})
	}
	if limit <= 0 || limit > 30 {
		limit = 8
	}
	// Берём запас кандидатов, чтобы после строгой фильтрации RBAC вернуть до limit доступных результатов.
	overfetch := max(limit*4, 24)

	ftsRes, err := s.repo.SearchFTS(ctx, q, overfetch)
	if err != nil {
		return nil, err
	}

	var vecRes []SearchResult
	if qVec, embErr := s.gateway.Embed(ctx, q); embErr == nil && len(qVec) > 0 {
		vecRes, _ = s.repo.SearchVectors(ctx, qVec, overfetch)
	}

	merged := ReciprocalRankFusion(ftsRes, vecRes, 60)
	sourceMap := make(map[string]platform.IndexSource, len(s.sources))
	for _, src := range s.sources {
		sourceMap[src.SourceType()] = src
	}

	filtered := make([]SearchResult, 0, limit)
	for _, item := range merged {
		src, ok := sourceMap[item.SourceType]
		if ok {
			allowed, err := src.CanView(ctx, auth, item.SourceID, item.Metadata)
			if err != nil || !allowed {
				continue
			}
		} else if auth == nil || !auth.IsSuperAdmin {
			continue
		}
		filtered = append(filtered, item)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

// ComposeStream собирает контекст из документов и сущностей CRM и стримит Markdown-черновик нового документа.
func (s *SearchAndComposeService) ComposeStream(ctx context.Context, auth *platform.AuthContext, req ComposeRequest, onDelta func(string) error) (*CompletionResponse, error) {
	if strings.TrimSpace(req.Goal) == "" && strings.TrimSpace(req.Title) == "" {
		return nil, platform.NewValidation("Опишите цель или тему генерируемого документа", nil)
	}
	var ctxBuf strings.Builder
	for _, docID := range req.SourceDocID {
		if s.docs != nil {
			md, err := s.docs.FetchDocumentMarkdown(ctx, auth, docID)
			if err == nil && md != "" {
				fmt.Fprintf(&ctxBuf, "### Исходный документ #%d\n%s\n\n", docID, md)
			}
		}
	}
	for _, ref := range req.EntityRefs {
		if s.resolver != nil {
			sum, err := s.resolver.ResolveEntity(ctx, auth, ref.EntityType, ref.EntityID)
			if err == nil && sum != nil {
				fmt.Fprintf(&ctxBuf, "### Карточка %s #%d\n%s\n\n", ref.EntityType, ref.EntityID, sum.Markdown)
			}
		}
	}
	if strings.TrimSpace(req.ExtraNotes) != "" {
		fmt.Fprintf(&ctxBuf, "### Дополнительные заметки\n%s\n\n", req.ExtraNotes)
	}

	sysPrompt := "Ты технический редактор сервисной компании «Теплосфера». Пиши чёткие, структурированные Markdown-документы на русском языке, опираясь на переданные регламенты и данные CRM."
	userPrompt := fmt.Sprintf("Заголовок: %s\nТип: %s\nЦель: %s\n\nКонтекст:\n%s",
		req.Title, req.DocType, req.Goal, ctxBuf.String())

	return s.gateway.StreamChat(ctx, auth, CompletionRequest{
		ModelID:   req.ModelID,
		Operation: "compose",
		Messages: []Message{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: userPrompt},
		},
	}, onDelta)
}
