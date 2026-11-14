package crm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func mangoForm(raw string) url.Values {
	return url.Values{"vpbx_api_key": {"key"}, "json": {raw}, "sign": {mangoSign("key", raw, "secret")}}
}
func TestMangoSignatureAndSequence(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewService(repo, nil, nil, nil, Config{TelephonyProvider: "mango", MangoAPIKey: "key", MangoAPISecret: "secret"}, nil)
	event := func(seq int, state string) url.Values {
		return mangoForm(fmt.Sprintf(`{"call_id":"test-leg","entry_id":"group","seq":"%d","timestamp":1700000000,"call_state":"%s","from":{"number":"79990000001"},"to":{"number":"101","extension":"101"}}`, seq, state))
	}
	bad := event(1, "Appeared")
	bad.Set("sign", "bad")
	if _, err := svc.HandleMangoWebhook(ctx, "call", bad); err == nil {
		t.Fatal("bad signature accepted")
	}
	a, err := svc.HandleMangoWebhook(ctx, "call", event(1, "Appeared"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ExternalCallID != "mango:test-leg" || a.Direction != "inbound" {
		t.Fatal("wrong event mapping")
	}
	if _, err = svc.HandleMangoWebhook(ctx, "call", event(2, "Connected")); err != nil {
		t.Fatal(err)
	}
	c, err := svc.HandleMangoWebhook(ctx, "call", event(3, "Disconnected"))
	if err != nil {
		t.Fatal(err)
	}
	if c.EndedAt == nil {
		t.Fatal("call not ended")
	}
	older, err := svc.HandleMangoWebhook(ctx, "call", event(1, "Appeared"))
	if err != nil || older.EndedAt == nil {
		t.Fatal("old event resurrected call")
	}
	dup := event(4, "Appeared")
	dup.Add("json", "{}")
	if _, err = svc.HandleMangoWebhook(ctx, "call", dup); err == nil {
		t.Fatal("duplicate form accepted")
	}
}
func TestMangoTransferAndRecordingContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		adapter := MangoAdapter{Key: "key", Secret: "secret"}
		if _, err := adapter.verify(r.PostForm); err != nil {
			t.Error("invalid request signature")
		}
		switch r.URL.Path {
		case "/commands/transfer":
			fmt.Fprint(w, `{"result":1000}`)
		case "/queries/recording/post":
			w.Header().Set("Content-Type", "audio/mpeg")
			fmt.Fprint(w, "audio")
		default:
			t.Error("wrong API path")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	a := MangoAdapter{Key: "key", Secret: "secret", BaseURL: server.URL, Client: server.Client()}
	if err := a.Transfer(context.Background(), "leg", "101"); err != nil {
		t.Fatal(err)
	}
	typ, data, err := a.Recording(context.Background(), "record")
	if err != nil || typ != "audio/mpeg" || string(data) != "audio" {
		t.Fatal("recording contract failed", err)
	}
	if err := a.Transfer(context.Background(), "leg", "\n"); err == nil {
		t.Fatal("invalid target accepted")
	}
}
func TestMangoRejectsForeignRecordingRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://example.com/private")
		w.WriteHeader(302)
	}))
	defer server.Close()
	a := MangoAdapter{Key: "key", Secret: "secret", BaseURL: server.URL, Client: server.Client()}
	if _, _, err := a.Recording(context.Background(), "r"); err == nil {
		t.Fatal("foreign redirect accepted")
	}
}
