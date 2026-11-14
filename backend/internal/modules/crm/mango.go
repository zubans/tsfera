package crm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

var errStaleCallEvent = errors.New("stale telephony event")

// MangoAdapter изолирует подпись и REST-контракты MANGO от бизнес-логики CRM.
type MangoAdapter struct {
	BaseURL, Key, Secret string
	Client               *http.Client
}

func (m *MangoAdapter) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (s *Service) mangoAdapter() *MangoAdapter {
	base := s.cfg.MangoBaseURL
	if base == "" {
		base = "https://app.mango-office.ru/vpbx"
	}
	return &MangoAdapter{BaseURL: strings.TrimRight(base, "/"), Key: s.cfg.MangoAPIKey, Secret: s.cfg.MangoAPISecret, Client: s.httpClient}
}

func mangoSign(key, raw, secret string) string {
	hash := sha256.Sum256([]byte(key + raw + secret))
	return hex.EncodeToString(hash[:])
}

func (m *MangoAdapter) verify(values url.Values) ([]byte, error) {
	if m.Key == "" || m.Secret == "" {
		return nil, platform.NewNotConfigured("MANGO API не настроен")
	}
	for _, key := range []string{"vpbx_api_key", "json", "sign"} {
		if len(values[key]) != 1 {
			return nil, platform.NewValidation("Ожидается единственное поле "+key, nil)
		}
	}
	raw := values.Get("json")
	expected := mangoSign(m.Key, raw, m.Secret)
	if subtle.ConstantTimeCompare([]byte(values.Get("vpbx_api_key")), []byte(m.Key)) != 1 || subtle.ConstantTimeCompare([]byte(strings.ToLower(values.Get("sign"))), []byte(expected)) != 1 {
		return nil, platform.NewUnauthorized("Неверная подпись MANGO")
	}
	if !json.Valid([]byte(raw)) {
		return nil, platform.NewValidation("Некорректный JSON MANGO", nil)
	}
	return []byte(raw), nil
}

// mangoScalar поддерживает строки и числа, встречающиеся в официальных примерах API.
type mangoScalar string

func (s *mangoScalar) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		*s = ""
		return nil
	}
	var text string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
	} else {
		var n json.Number
		if err := json.Unmarshal(raw, &n); err != nil {
			return err
		}
		text = n.String()
	}
	*s = mangoScalar(text)
	return nil
}
func (s mangoScalar) integer() (int64, error) { return strconv.ParseInt(string(s), 10, 64) }

type mangoEvent struct {
	CallID    string      `json:"call_id"`
	EntryID   string      `json:"entry_id"`
	Seq       mangoScalar `json:"seq"`
	Timestamp mangoScalar `json:"timestamp"`
	State     string      `json:"call_state"`
	Location  string      `json:"location"`
	From      struct {
		Extension mangoScalar `json:"extension"`
		Number    string      `json:"number"`
	} `json:"from"`
	To struct {
		Extension mangoScalar `json:"extension"`
		Number    string      `json:"number"`
	} `json:"to"`
	RecordingID    string      `json:"recording_id"`
	RecordingState string      `json:"recording_state"`
	CompletionCode mangoScalar `json:"completion_code"`
	CommandID      string      `json:"command_id"`
	Result         mangoScalar `json:"result"`
}

type TelephonyMetadataRepository interface {
	SaveMangoRecording(context.Context, string, string, int64, string) error
	GetMangoRecording(context.Context, string) (string, error)
	SaveMangoCommandResult(context.Context, string, string) error
}

func (s *Service) HandleMangoWebhook(ctx context.Context, kind string, values url.Values) (*CallRecord, error) {
	if s.cfg.TelephonyProvider != "mango" {
		return nil, platform.NewForbidden("Провайдер MANGO не выбран")
	}
	raw, err := s.mangoAdapter().verify(values)
	if err != nil {
		return nil, err
	}
	var event mangoEvent
	if err = json.Unmarshal(raw, &event); err != nil {
		return nil, platform.NewValidation("Некорректные поля MANGO", nil)
	}
	if kind == "transfer" {
		store, ok := s.repo.(TelephonyMetadataRepository)
		if !ok {
			return nil, platform.NewNotConfigured("Хранилище результатов телефонии не настроено")
		}
		if event.CommandID == "" || event.Result == "" {
			return nil, platform.NewValidation("Нет результата команды", nil)
		}
		return nil, store.SaveMangoCommandResult(ctx, event.CommandID, string(event.Result))
	}
	if event.CallID == "" || len(event.CallID) > 128 {
		return nil, platform.NewValidation("Некорректный call_id", nil)
	}
	seq, err := event.Seq.integer()
	if err != nil || seq < 1 {
		return nil, platform.NewValidation("Некорректный seq", nil)
	}
	if kind == "recording" {
		store, ok := s.repo.(TelephonyMetadataRepository)
		if !ok {
			return nil, platform.NewNotConfigured("Хранилище записей не настроено")
		}
		if event.RecordingID == "" || len(event.RecordingID) > 128 {
			return nil, platform.NewValidation("Нет recording_id", nil)
		}
		if event.RecordingState != "Started" && event.RecordingState != "Continued" && event.RecordingState != "Completed" {
			return nil, platform.NewValidation("Неизвестное состояние записи MANGO", nil)
		}
		if event.RecordingState == "Completed" && event.CompletionCode != "1000" {
			event.RecordingState = "Failed"
		}
		return nil, store.SaveMangoRecording(ctx, event.CallID, event.RecordingID, seq, event.RecordingState)
	}
	if kind != "call" {
		return nil, platform.NewNotFound("Событие MANGO не поддерживается")
	}
	stamp, err := event.Timestamp.integer()
	if err != nil || stamp <= 0 {
		return nil, platform.NewValidation("Некорректный timestamp", nil)
	}
	// Быстрый барьер в одном процессе; PostgreSQL дополнительно сравнивает seq атомарно.
	s.telephonyMu.Lock()
	defer s.telephonyMu.Unlock()
	id := "mango:" + event.CallID
	previous, err := s.repo.GetCallByExternalID(ctx, id)
	if err != nil {
		return nil, err
	}
	if previous != nil {
		var old struct {
			Seq int64 `json:"mango_seq"`
		}
		_ = json.Unmarshal(previous.RawPayload, &old)
		if old.Seq >= seq {
			return previous, nil
		}
	}
	direction, extension := "inbound", string(event.To.Extension)
	if event.From.Extension != "" {
		direction = "outbound"
		extension = string(event.From.Extension)
	}
	if event.From.Extension != "" && event.To.Extension != "" {
		direction = "internal"
	}
	input := MTSWebhookInput{ExternalCallID: id, AbonentID: extension, Direction: direction, CallerPhone: event.From.Number, CalleePhone: event.To.Number, Payload: &MTSVPBXPayload{}}
	switch event.State {
	case "Appeared":
		input.EventType = "CALL_RECEIVED"
		input.Status = "ringing"
		input.Payload.StartTime = stamp * 1000
	case "Connected":
		input.EventType = "CALL_ANSWERED"
		input.Status = "answered"
		input.Payload.AnswerTime = stamp * 1000
	case "OnHold":
		input.Status = "held"
	case "Disconnected":
		input.EventType = "CALL_RELEASED"
		input.Status = "ended"
		input.Payload.EndTime = stamp * 1000
	default:
		return nil, platform.NewValidation("Неизвестное состояние MANGO", nil)
	}
	if previous == nil && input.Payload.StartTime == 0 {
		input.Payload.StartTime = stamp * 1000
	}
	// Запоздавшее состояние не должно воскресить завершённый вызов.
	if previous != nil && previous.EndedAt != nil && event.State != "Disconnected" {
		return previous, nil
	}
	normalized, _ := json.Marshal(map[string]any{"provider": "mango", "mango_seq": seq, "entry_id": event.EntryID, "event": json.RawMessage(raw)})
	return s.applyCallEvent(ctx, input, normalized)
}

func (h *Handler) mangoWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Ожидается form-urlencoded", nil))
		return
	}
	// Подпись проверяется по телу: query-параметры не могут подменять поля формы.
	_, err := h.svc.HandleMangoWebhook(r.Context(), chi.URLParam(r, "kind"), r.PostForm)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]int{"result": 1000})
}

func (m *MangoAdapter) request(ctx context.Context, path string, payload any) (*http.Response, error) {
	if m.Key == "" || m.Secret == "" {
		return nil, platform.NewNotConfigured("MANGO API не настроен")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	form := url.Values{"vpbx_api_key": {m.Key}, "json": {string(raw)}, "sign": {mangoSign(m.Key, string(raw), m.Secret)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(m.BaseURL, "/")+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Не пересылаем ключ и подпись на redirect: запись скачивается отдельным GET.
	client := *m.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(req)
}

// Transfer подтверждает принятие команды API; окончательный результат приходит вебхуком.
func (m *MangoAdapter) Transfer(ctx context.Context, callID, target string) error {
	target = strings.TrimSpace(target)
	if target == "" || len(target) > 128 || strings.ContainsAny(target, "\r\n") {
		return platform.NewValidation("Некорректная цель перевода", nil)
	}
	if !strings.HasPrefix(target, "sip:") {
		for i, c := range target {
			if (c < '0' || c > '9') && !(c == '+' && i == 0) {
				return platform.NewValidation("Укажите номер или SIP URI", nil)
			}
		}
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	response, err := m.request(ctx, "/commands/transfer", map[string]string{"command_id": hex.EncodeToString(token), "call_id": callID, "to_number": target, "method": "blind"})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return platform.NewRuleError(fmt.Sprintf("MANGO: HTTP %d", response.StatusCode))
	}
	var result struct {
		Result mangoScalar `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return platform.NewRuleError("Некорректный ответ MANGO")
	}
	if result.Result != "1000" {
		return platform.NewRuleError("MANGO отклонила команду: " + string(result.Result))
	}
	return nil
}

func (m *MangoAdapter) Recording(ctx context.Context, id string) (string, []byte, error) {
	response, err := m.request(ctx, "/queries/recording/post", map[string]string{"recording_id": id, "action": "download"})
	if err != nil {
		return "", nil, err
	}
	if response.StatusCode == http.StatusFound || response.StatusCode == http.StatusSeeOther {
		location, err := response.Location()
		response.Body.Close()
		if err != nil {
			return "", nil, platform.NewRuleError("MANGO не вернула адрес записи")
		}
		// Redirect-URL не сохраняется и не получает API-секреты; разрешён только HTTPS MANGO.
		if location.Scheme != "https" || (location.Hostname() != "mango-office.ru" && !strings.HasSuffix(location.Hostname(), ".mango-office.ru")) || location.User != nil {
			return "", nil, platform.NewForbidden("Недопустимый адрес записи MANGO")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
		if err != nil {
			return "", nil, err
		}
		client := *m.client()
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		response, err = client.Do(req)
		if err != nil {
			return "", nil, err
		}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil, platform.NewRuleError(fmt.Sprintf("Запись MANGO: HTTP %d", response.StatusCode))
	}
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "audio/") && contentType != "application/octet-stream" {
		return "", nil, platform.NewRuleError("Вместо записи MANGO вернула другой тип данных")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (50<<20)+1))
	if err != nil {
		return "", nil, err
	}
	if len(data) > 50<<20 {
		return "", nil, platform.NewRuleError("Запись превышает 50 МиБ")
	}
	return contentType, data, nil
}

func (r *PgRepository) SaveMangoRecording(ctx context.Context, callID, id string, seq int64, state string) error {
	_, err := r.db.Exec(ctx, `INSERT INTO crm_telephony_recordings(provider,call_id,recording_id,seq,state) VALUES('mango',$1,$2,$3,$4)
 ON CONFLICT(provider,call_id,recording_id) DO UPDATE SET seq=EXCLUDED.seq,state=EXCLUDED.state WHERE crm_telephony_recordings.seq<EXCLUDED.seq`, callID, id, seq, state)
	return err
}
func (r *PgRepository) GetMangoRecording(ctx context.Context, callID string) (string, error) {
	var id string
	err := r.db.QueryRow(ctx, `SELECT recording_id FROM crm_telephony_recordings WHERE provider='mango' AND call_id=$1 AND state='Completed' ORDER BY seq DESC,recording_id LIMIT 1`, callID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", platform.NewNotFound("Готовая запись MANGO отсутствует")
	}
	return id, err
}
func (r *PgRepository) SaveMangoCommandResult(ctx context.Context, id, result string) error {
	_, err := r.db.Exec(ctx, `INSERT INTO crm_telephony_command_results(provider,command_id,result) VALUES('mango',$1,$2) ON CONFLICT(provider,command_id) DO UPDATE SET result=EXCLUDED.result,received_at=now()`, id, result)
	return err
}
