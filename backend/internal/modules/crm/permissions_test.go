package crm

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func importedTestRoles(t *testing.T) map[string]map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../../../scripts/migration/fixtures/translated_roles.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	result := map[string]map[string]bool{}
	for _, row := range rows {
		result[row.Role] = map[string]bool{}
		for _, p := range row.Permissions {
			result[row.Role][p] = true
		}
	}
	return result
}
func TestImportedRolesHTTPBoundaries(t *testing.T) {
	roles := importedTestRoles(t)
	cases := []struct {
		role, method, path, body string
		status                   int
	}{
		{"rukovoditel_podrazdelenia", "GET", "/api/v1/crm/pool-settings", "", 200},
		{"rukovoditel_podrazdelenia", "POST", "/api/v1/crm/pool-settings", `{"name":"bad"}`, 403},
		{"rukovoditel_podrazdelenia", "POST", "/api/v1/crm/pool-settings/run", `{}`, 403},
		{"buhgalter", "GET", "/api/v1/crm/reports/amount-spare-parts", "", 200},
		{"buhgalter", "GET", "/api/v1/crm/reports/engineers", "", 403},
		{"buhgalter", "GET", "/api/v1/crm/reports/call-conversion", "", 403},
		{"inzener", "GET", "/api/v1/crm/certificates/export", "", 403},
		{"inzener", "POST", "/api/v1/crm/applications/1/complete", `{"is_complete":true}`, 403},
		{"inzener", "PATCH", "/api/v1/crm/applications/1/rating", `{"rating":5}`, 403},
		{"menedzer_po_kacestvu", "PATCH", "/api/v1/crm/applications/1/rating", `{"rating":5}`, 200},
		{"menedzer_po_kacestvu", "PUT", "/api/v1/crm/applications/1", `{}`, 403},
		{"operator", "PUT", "/api/v1/crm/tasks/1", `{}`, 403},
		{"rukovoditel_podrazdelenia", "POST", "/api/v1/crm/refs/work_type", `{"name":"bad"}`, 403},
		{"rukovoditel_podrazdelenia", "POST", "/api/v1/crm/refs/special_note", `{"name":"Allowed"}`, 201},
	}
	for _, tc := range cases {
		t.Run(tc.role+tc.method+tc.path, func(t *testing.T) {
			repo := newMemRepo()
			repo.apps[1] = &Application{ID: 1, EngineerID: 7}
			sub := int64(1)
			auth := &platform.AuthContext{UserID: 7, SubdivisionID: &sub, Permissions: roles[tc.role]}
			svc := NewService(repo, nil, nil, nil, Config{}, nil)
			router := chi.NewRouter()
			NewHandler(svc, staticAuthorizer{ac: auth}, nil).Mount(router)
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status %d want %d: %s", response.Code, tc.status, response.Body.String())
			}
		})
	}
}

type commentRepository struct{ *memRepo }

func (r commentRepository) UpdateApplicationComment(_ context.Context, id int64, text string) error {
	r.apps[id].Comment = text
	return nil
}
func TestCommentPermissionDoesNotGrantFullUpdate(t *testing.T) {
	repo := commentRepository{newMemRepo()}
	repo.apps[1] = &Application{ID: 1, EngineerID: 7, ContractorID: 42, Comment: "old"}
	auth := &platform.AuthContext{UserID: 7, Permissions: map[string]bool{"crm.application.view": true, "crm.application.update_comment": true}}
	svc := NewService(repo, nil, nil, nil, Config{}, nil)
	router := chi.NewRouter()
	NewHandler(svc, staticAuthorizer{ac: auth}, nil).Mount(router)
	for _, method := range []string{"PATCH", "PUT"} {
		path := "/api/v1/crm/applications/1"
		if method == "PATCH" {
			path += "/comment"
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(`{"comment":"new","contractor_id":99}`)))
		expected := 200
		if method == "PUT" {
			expected = 403
		}
		if response.Code != expected {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if repo.apps[1].Comment != "new" || repo.apps[1].ContractorID != 42 {
		t.Fatal("narrow patch changed unrelated fields")
	}
	auth.IsToken = true
	auth.TokenCanRead = true
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/api/v1/crm/applications/1/comment", strings.NewReader(`{"comment":"bad"}`)))
	if response.Code != 403 {
		t.Fatal("read-only token wrote comment")
	}
}
func TestReportSubdivisionCannotBeOverriddenByUserScope(t *testing.T) {
	sub := int64(3)
	auth := &platform.AuthContext{SubdivisionID: &sub, Permissions: map[string]bool{"crm.report.sum_executor.view": true, "admin.user.without_subdivision": true}}
	f, err := scopedReport(auth, ReportFilter{SubdivisionIDs: []int64{99}, From: time.Now()}, "sum_executor")
	if err != nil || len(f.SubdivisionIDs) != 1 || f.SubdivisionIDs[0] != 3 {
		t.Fatal("foreign subdivision allowed")
	}
	auth.Permissions["crm.report.sum_executor.allow_select_subdivision"] = true
	f, err = scopedReport(auth, ReportFilter{SubdivisionIDs: []int64{99}}, "sum_executor")
	if err != nil || f.SubdivisionIDs[0] != 99 {
		t.Fatal("explicit report scope ignored")
	}
	auth.Permissions["crm.report.sum_executor.allow_select_subdivision"] = false
	auth.SubdivisionID = nil
	if _, err = scopedReport(auth, ReportFilter{}, "sum_executor"); err == nil {
		t.Fatal("missing subdivision exposed all data")
	}
}

type calendarScopeRepository struct {
	*memRepo
	engineer, subdivision *int64
}

func (r *calendarScopeRepository) ListCalendarEvents(_ context.Context, _, _ time.Time, engineer, subdivision *int64) ([]CalendarEvent, error) {
	r.engineer, r.subdivision = engineer, subdivision
	return []CalendarEvent{}, nil
}
func TestCalendarPermissionsDoNotInheritContractorScope(t *testing.T) {
	repo := &calendarScopeRepository{memRepo: newMemRepo()}
	svc := NewService(repo, nil, nil, nil, Config{}, nil)
	sub, other := int64(3), int64(99)
	auth := &platform.AuthContext{UserID: 7, SubdivisionID: &sub, Permissions: map[string]bool{"crm.contractor.view_all": true, "admin.user.without_subdivision": true}}
	if _, err := svc.ListCalendarEvents(context.Background(), auth, time.Now(), time.Now(), &other, &other); err != nil {
		t.Fatal(err)
	}
	if *repo.engineer != 7 || *repo.subdivision != 3 {
		t.Fatal("calendar scope widened by unrelated permissions")
	}
	auth.Permissions["crm.calendar.select_user"] = true
	auth.Permissions["crm.calendar.select_subdivision"] = true
	if _, err := svc.ListCalendarEvents(context.Background(), auth, time.Now(), time.Now(), &other, &other); err != nil {
		t.Fatal(err)
	}
	if *repo.engineer != 99 || *repo.subdivision != 99 {
		t.Fatal("explicit calendar permissions ignored")
	}
}
