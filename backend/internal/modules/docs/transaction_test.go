package docs

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

type revisionFailureDB struct {
	pgx.Tx
	transaction *revisionFailureTx
}

func (d *revisionFailureDB) Begin(context.Context) (pgx.Tx, error) { return d.transaction, nil }

type revisionFailureTx struct {
	pgx.Tx
	queries               int
	committed, rolledBack bool
}

func (t *revisionFailureTx) QueryRow(context.Context, string, ...any) pgx.Row {
	t.queries++
	return revisionTestRow{fail: t.queries == 2}
}
func (t *revisionFailureTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *revisionFailureTx) Rollback(context.Context) error { t.rolledBack = true; return nil }

type revisionTestRow struct{ fail bool }

func (r revisionTestRow) Scan(dest ...any) error {
	if r.fail {
		return errors.New("revision insert failed")
	}
	*dest[0].(*int) = 2
	*dest[1].(*time.Time) = time.Now()
	return nil
}

func TestRevisionFailureRollsBackDocumentUpdate(t *testing.T) {
	tx := &revisionFailureTx{}
	repo := NewPgRepository(&revisionFailureDB{transaction: tx})
	doc := &Document{ID: 1, Version: 1, Title: "Original"}
	err := repo.SaveDocumentRevision(context.Background(), doc, 1, &Revision{})
	if err == nil || tx.committed || !tx.rolledBack {
		t.Fatalf("err=%v committed=%v rollback=%v", err, tx.committed, tx.rolledBack)
	}
	if doc.Version != 1 {
		t.Fatal("failed transaction changed caller's version")
	}
}
