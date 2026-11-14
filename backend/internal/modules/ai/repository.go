package ai

import "context"

type Repository interface {
	ListProviders(ctx context.Context) ([]Provider, error)
	GetProvider(ctx context.Context, id int64) (*Provider, error)
	SaveProvider(ctx context.Context, p *Provider) error
	DeleteProvider(ctx context.Context, id int64) error

	ListModels(ctx context.Context, capability *ModelCapability) ([]Model, error)
	GetModel(ctx context.Context, id int64) (*Model, error)
	GetDefaultModel(ctx context.Context, capability ModelCapability) (*Model, error)
	SaveModel(ctx context.Context, m *Model) error
	DeleteModel(ctx context.Context, id int64) error

	ListChunkHashes(ctx context.Context, sourceType, sourceID string) ([]string, error)
	ReplaceSourceChunks(ctx context.Context, sourceType, sourceID string, chunks []SearchChunk) error
	DeleteSourceChunks(ctx context.Context, sourceType, sourceID string) error
	SearchFTS(ctx context.Context, query string, limit int) ([]SearchResult, error)
	SearchVectors(ctx context.Context, queryVec []float64, limit int) ([]SearchResult, error)

	LogUsage(ctx context.Context, entry *UsageLog) error
	ListUsageLogs(ctx context.Context, limit int) ([]UsageLog, error)
}
