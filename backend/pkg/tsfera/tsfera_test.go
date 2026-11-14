package tsfera

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/internal/modules/ai"
	"github.com/zubans/tsfera/backend/internal/modules/chat"
	"github.com/zubans/tsfera/backend/internal/modules/crm"
	"github.com/zubans/tsfera/backend/internal/modules/docs"
	"github.com/zubans/tsfera/backend/internal/modules/users"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type customHostModule struct{}

func (customHostModule) Name() string      { return "host_billing" }
func (customHostModule) Migrations() fs.FS { return nil }
func (customHostModule) Permissions() []platform.PermissionDef {
	return []platform.PermissionDef{
		{Key: "billing.invoice.view", Group: "Биллинг", Resource: "invoice", Action: "view", Description: "Счета хост-системы"},
	}
}
func (customHostModule) MountHTTP(r chi.Router) {
	r.Get("/api/host/ping", func(w http.ResponseWriter, _ *http.Request) {
		platform.WriteJSON(w, http.StatusOK, map[string]string{"host": "ok"})
	})
}
func (customHostModule) Workers() []platform.PeriodicJob { return nil }
func (customHostModule) Tools() []platform.ToolDefinition {
	return []platform.ToolDefinition{{
		Name:        "host_billing_check",
		Description: "Инструмент внешнего host-проекта",
	}}
}
func (customHostModule) SearchSources() []platform.IndexSource { return nil }

func TestPlatformCompositionFullAndCRMOnlyAndHostExtension(t *testing.T) {
	// 1. Полная модульная сборка + внешний ExtraModule из большего Go-проекта.
	p, err := newPlatformWithRepos(context.Background(), Config{
		EnableDocsAndAI: true,
		ExtraModules:    []platform.Module{customHostModule{}},
	}, repositories{
		users: (*users.PgRepository)(nil),
		crm:   (*crm.PgRepository)(nil),
		docs:  (*docs.PgRepository)(nil),
		ai:    (*ai.PgRepository)(nil),
		chat:  (*chat.PgRepository)(nil),
	})
	if err != nil {
		t.Fatalf("newPlatformWithRepos full failed: %v", err)
	}
	if len(p.Modules) != 6 {
		t.Fatalf("expected 6 modules (5 core + 1 host), got %d", len(p.Modules))
	}

	// Убеждаемся, что разрешения и инструменты host-модуля попали в общие реестры.
	perms := p.Permissions()
	hasBillingPerm := false
	hasCRMContractorPerm := false
	for _, prm := range perms {
		if prm.Key == "billing.invoice.view" {
			hasBillingPerm = true
		}
		if prm.Key == "crm.contractor.view" {
			hasCRMContractorPerm = true
		}
	}
	if !hasBillingPerm || !hasCRMContractorPerm {
		t.Fatalf("missing expected permissions: billing=%v crm=%v", hasBillingPerm, hasCRMContractorPerm)
	}

	// Проверяем healthz и host route + SPA fallback.
	spaDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(spaDir, "index.html"), []byte("<!doctype html><title>Tsfera</title>"), 0o600)
	h := p.Handler(spaDir)

	for _, tc := range []struct {
		path string
		code int
	}{
		{"/healthz", http.StatusOK},
		{"/api/host/ping", http.StatusOK},
		{"/crm/calendar", http.StatusOK},          // SPA fallback -> index.html
		{"/api/unknown", http.StatusUnauthorized}, // защищено RequireAuth группой /api
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.code {
			t.Fatalf("path %s: expected %d, got %d (%s)", tc.path, tc.code, rec.Code, rec.Body.String())
		}
	}

	// Проверка содержимого /healthz.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var health map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &health)
	if health["status"] != "ok" {
		t.Fatalf("unexpected health response: %v", health)
	}

	// 2. Сборка CRM-only для минимального встраивания.
	crmOnly, err := newPlatformWithRepos(context.Background(), Config{
		EnableDocsAndAI: false,
	}, repositories{
		users: (*users.PgRepository)(nil),
		crm:   (*crm.PgRepository)(nil),
	})
	if err != nil {
		t.Fatalf("newPlatformWithRepos crm-only failed: %v", err)
	}
	if len(crmOnly.Modules) != 2 || crmOnly.Docs != nil || crmOnly.AI != nil || crmOnly.Chat != nil {
		t.Fatalf("expected only users+crm in CRM-only mode, got %d modules", len(crmOnly.Modules))
	}
}
