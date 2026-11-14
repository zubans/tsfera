package users

import (
	"context"
	"github.com/go-chi/chi/v5"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReviewReadOnlyCannotMintWriteToken(t *testing.T) {
	repo := newStubRepo()
	repo.users[7] = &User{ID: 7, Username: "review", Active: true}
	svc := NewService(repo, time.Hour)
	_, raw, err := svc.CreateAPIToken(context.Background(), 7, CreateTokenInput{Name: "ro", CanRead: true})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewHandler(svc, nil).Mount(router)
	req := httptest.NewRequest("POST", "/tokens", strings.NewReader(`{"name":"escalated","can_read":true,"can_write":true}`))
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("read-only token minted write token: status=%d", rec.Code)
	}
}
