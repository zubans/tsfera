package crm

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

// Read-only smoke tests against a database produced by migrate.py.
// The real dump is never committed and no customer values are printed.
func TestImportedDatabase(t *testing.T) {
	dsn := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MIGRATION_TEST_DATABASE_URL to an imported test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewPgRepository(pool)
	checks := map[string]func() error{
		"applications": func() error { _, e := repo.ListApplications(ctx, ApplicationFilter{Limit: 200}); return e },
		"certificates": func() error { _, e := repo.ListCertificates(ctx, CertificateFilter{Limit: 200}); return e },
		"tasks":        func() error { _, e := repo.ListTasks(ctx, TaskFilter{Limit: 200}); return e },
		"calls":        func() error { _, e := repo.ListCalls(ctx, CallFilter{Limit: 200}); return e },
		"contracts":    func() error { _, e := repo.ListContracts(ctx, ContractFilter{Limit: 200}); return e },
		"analytics":    func() error { _, e := repo.ListAnalytics(ctx, AnalyticFilter{Limit: 200}); return e },
		"subdivisions": func() error { _, e := repo.ListSubdivisions(ctx); return e },
		"settings":     func() error { _, e := repo.ListAppSettings(ctx); return e },
		"contractors":  func() error { _, _, e := repo.SearchContractors(ctx, ContractorFilter{Limit: 100}); return e },
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("incomplete_application_and_calendar", func(t *testing.T) {
		var id int64
		if err := pool.QueryRow(ctx, "SELECT id FROM crm_applications WHERE NOT deleted AND (contractor_id IS NULL OR address_id IS NULL) LIMIT 1").Scan(&id); err != nil {
			t.Fatal(err)
		}
		app, err := repo.GetApplication(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if app == nil || !app.Incomplete {
			t.Fatal("incomplete document was hidden")
		}
		if _, err := repo.ListCalendarEvents(ctx, app.PlanDatetimeFrom.Add(-time.Hour), app.PlanDatetimeTo.Add(time.Hour), nil, nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("task_without_deadline", func(t *testing.T) {
		var id int64
		if err := pool.QueryRow(ctx, "SELECT id FROM crm_tasks WHERE NOT deleted AND plan_datetime_to IS NULL LIMIT 1").Scan(&id); err != nil {
			t.Fatal(err)
		}
		task, err := repo.GetTask(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task == nil || !task.MissingDeadline {
			t.Fatal("missing deadline not preserved")
		}
	})
	t.Run("certificate_results_and_equipment", func(t *testing.T) {
		var id int64
		if err := pool.QueryRow(ctx, "SELECT c.id FROM crm_certificates c WHERE NOT c.deleted AND EXISTS(SELECT 1 FROM crm_certificate_equipments e WHERE e.certificate_id=c.id) LIMIT 1").Scan(&id); err != nil {
			t.Fatal(err)
		}
		cert, err := repo.GetCertificate(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if cert == nil || len(cert.Equipments) < 3 || cert.Number == nil {
			t.Fatal("document links or number lost")
		}
		var raw []byte
		if err := pool.QueryRow(ctx, "SELECT results FROM crm_certificates WHERE id=$1", id).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var telemetry CertificateTelemetry
		if err := json.Unmarshal(raw, &telemetry); err != nil {
			t.Fatal("invalid typed telemetry", err)
		}
	})
	t.Run("history_entity_mapping", func(t *testing.T) {
		var count int64
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM crm_change_logs WHERE entity_type='contractor'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			t.Fatal("contractor history not linked")
		}
	})
}
