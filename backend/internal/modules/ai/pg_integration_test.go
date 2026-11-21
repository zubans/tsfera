package ai

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Проверка SQL провайдеров консольных агентов и назначений задач на пустой схеме (make migrate-schema).
func TestAIPgRepository(t *testing.T) {
	dsn := os.Getenv("AI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set AI_TEST_DATABASE_URL to an empty migrated test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewPgRepository(pool)

	p := &Provider{Name: "Claude Code", Kind: ProviderClaudeCLI, Active: true, TimeoutSeconds: 600}
	if err := repo.SaveProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM ai_providers WHERE id = $1`, p.ID)
	got, err := repo.GetProvider(ctx, p.ID)
	if err != nil || got.TimeoutSeconds != 600 || got.Kind != ProviderClaudeCLI {
		t.Fatalf("provider: %+v %v", got, err)
	}
	m := &Model{ProviderID: p.ID, Name: "sonnet", ModelID: "sonnet", Capability: CapChat, ContextWindow: 200000, OpenToAll: true, Active: true}
	if err := repo.SaveModel(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveTaskAssignment(ctx, TaskCompose, &m.ID); err != nil {
		t.Fatal(err)
	}
	if id, err := repo.GetTaskModelID(ctx, TaskCompose); err != nil || id == nil || *id != m.ID {
		t.Fatalf("assignment: %v %v", id, err)
	}
	list, err := repo.ListTaskAssignments(ctx)
	if err != nil || len(list) == 0 || list[0].ModelName != "sonnet" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := repo.DeleteModel(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if id, _ := repo.GetTaskModelID(ctx, TaskCompose); id != nil {
		t.Fatalf("после удаления модели назначение снимается: %v", *id)
	}
	if err := repo.SaveTaskAssignment(ctx, TaskCompose, nil); err != nil {
		t.Fatal(err)
	}
}
