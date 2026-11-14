package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseAndFormatPhone(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantClean string
		wantFmt   string
		wantErr   bool
	}{
		{name: "russian +7", input: "+7 (999) 123-45-67", wantClean: "9991234567", wantFmt: "+7 (999) 123-45-67"},
		{name: "leading 8", input: "89165554433", wantClean: "9165554433", wantFmt: "+7 (916) 555-44-33"},
		{name: "bare 10 digits", input: "4951112233", wantClean: "4951112233", wantFmt: "+7 (495) 111-22-33"},
		{name: "too short", input: "12345", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePhone10(tt.input)
			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("ParsePhone10(%q) err = %v, want ErrValidation", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tt.wantClean {
				t.Errorf("ParsePhone10(%q) = %q, want %q", tt.input, got, tt.wantClean)
			}
			if fmtGot := FormatPhoneRU(got); fmtGot != tt.wantFmt {
				t.Errorf("FormatPhoneRU(%q) = %q, want %q", got, fmtGot, tt.wantFmt)
			}
		})
	}
}

func TestWriteErrorMapsDomainErrorsWithoutLeakingInternals(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantSubstr string
	}{
		{name: "not found", err: NewNotFound("Заявка не найдена"), wantStatus: 404, wantSubstr: "Заявка не найдена"},
		{name: "forbidden", err: NewForbidden("Нет доступа"), wantStatus: 403, wantSubstr: "Нет доступа"},
		{name: "conflict", err: NewConflict("Версия устарела"), wantStatus: 409, wantSubstr: "Версия устарела"},
		{name: "validation", err: NewValidation("Ошибка полей", map[string]string{"sum": "обязательно"}), wantStatus: 422, wantSubstr: `"sum"`},
		{name: "not configured", err: NewNotConfigured("LLM не настроен"), wantStatus: 503, wantSubstr: "LLM не настроен"},
		{name: "unknown internal", err: errors.New("pq: password authentication failed"), wantStatus: 500, wantSubstr: "Внутренняя ошибка сервера"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, nil, tt.err)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tt.wantSubstr) {
				t.Errorf("body = %q, want substring %q", body, tt.wantSubstr)
			}
			if tt.wantStatus == 500 && strings.Contains(body, "pq:") {
				t.Errorf("internal database error leaked in response body: %s", body)
			}
		})
	}
}

func TestAuthContextPermissionsAndTokens(t *testing.T) {
	super := &AuthContext{IsSuperAdmin: true, Permissions: map[string]bool{}}
	if !super.HasPermission("crm.application.delete") {
		t.Fatal("superadmin must have all permissions")
	}

	readOnlyToken := &AuthContext{
		IsSuperAdmin:  true,
		IsToken:       true,
		TokenCanRead:  true,
		TokenCanWrite: false,
	}
	if !readOnlyToken.HasPermission("crm.application.view") {
		t.Error("read-only token should have .view permission")
	}
	if readOnlyToken.HasPermission("crm.application.create") {
		t.Error("read-only token must not have .create permission")
	}

	regular := &AuthContext{Permissions: map[string]bool{"crm.task.view": true}}
	if !regular.HasPermission("crm.task.view") || regular.HasPermission("crm.task.delete") {
		t.Error("unexpected regular permission resolution")
	}
}

func TestInMemoryEventBusAndWorkerGroup(t *testing.T) {
	bus := NewInMemoryEventBus()
	var received atomic.Int32
	unsub := bus.Subscribe("crm.call", func(_ context.Context, ev Event) {
		if ev.Topic == "crm.call" {
			received.Add(1)
		}
	})
	bus.Publish(context.Background(), Event{Topic: "crm.call", Payload: "ping"})
	unsub()
	bus.Publish(context.Background(), Event{Topic: "crm.call", Payload: "ignored"})
	if received.Load() != 1 {
		t.Fatalf("received = %d, want 1", received.Load())
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg := NewWorkerGroup(nil)
	var panicHits, normalHits atomic.Int32
	wg.Add(
		PeriodicJob{
			Name:      "panic-worker",
			Interval:  15 * time.Millisecond,
			Immediate: true,
			Run: func(context.Context) error {
				panicHits.Add(1)
				panic("boom")
			},
		},
		PeriodicJob{
			Name:      "normal-worker",
			Interval:  15 * time.Millisecond,
			Immediate: true,
			Run: func(context.Context) error {
				normalHits.Add(1)
				return nil
			},
		},
	)
	wg.Start(ctx)
	time.Sleep(40 * time.Millisecond)
	cancel()
	wg.Wait()

	if panicHits.Load() == 0 || normalHits.Load() < 2 {
		t.Fatalf("unexpected worker execution counts: panic=%d normal=%d", panicHits.Load(), normalHits.Load())
	}
}

func TestTokenizeSearchQuery(t *testing.T) {
	words, nums, phones := TokenizeSearchQuery("Иванов Ленина 15 9991234567 12345")
	if !reflect.DeepEqual(words, []string{"иванов", "ленина"}) {
		t.Errorf("words = %v", words)
	}
	if !reflect.DeepEqual(nums, []string{"12345"}) {
		t.Errorf("nums = %v", nums)
	}
	if !reflect.DeepEqual(phones, []string{"9991234567"}) {
		t.Errorf("phones = %v", phones)
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"test"}`))
	var payload map[string]string
	if err := DecodeJSON(req, &payload); err != nil {
		t.Fatalf("DecodeJSON error: %v", err)
	}
	b, _ := json.Marshal(payload)
	if string(b) != `{"name":"test"}` {
		t.Errorf("payload = %s", b)
	}
}
