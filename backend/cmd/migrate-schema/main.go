// migrate-schema создаёт только схему: без сервера, воркеров и первичного администратора.
package main

import (
	"context"
	"fmt"
	"github.com/zubans/tsfera/backend/pkg/tsfera"
	"os"
)

func main() {
	ctx := context.Background()
	pool, err := tsfera.OpenPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "database connection failed")
		os.Exit(1)
	}
	defer pool.Close()
	p, err := tsfera.NewPlatform(ctx, tsfera.Config{Pool: pool, EnableDocsAndAI: true})
	if err == nil {
		err = p.Migrate(ctx)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "schema migration failed:", err)
		os.Exit(1)
	}
}
