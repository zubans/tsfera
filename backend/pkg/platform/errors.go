package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// Сентинельные классы ошибок сервисного слоя для единообразного маппинга в HTTP-статусы.
var (
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrNotFound      = errors.New("not_found")
	ErrConflict      = errors.New("conflict")
	ErrValidation    = errors.New("validation")
	ErrRule          = errors.New("business_rule")
	ErrNotConfigured = errors.New("not_configured")
)

// DomainError связывает класс ошибки (сентинел), понятный пользователю текст и ошибки полей.
type DomainError struct {
	Kind    error
	Message string
	Fields  map[string]string
	Cause   error
}

func (e *DomainError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	if e.Kind != nil {
		return e.Kind.Error()
	}
	return "domain error"
}

func (e *DomainError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return e.Kind
}

// Is позволяет errors.Is сопоставлять DomainError с её классом Kind.
func (e *DomainError) Is(target error) bool {
	return e.Kind != nil && errors.Is(e.Kind, target)
}

func NewUnauthorized(msg string) *DomainError {
	return &DomainError{Kind: ErrUnauthorized, Message: msg}
}

func NewForbidden(msg string) *DomainError {
	return &DomainError{Kind: ErrForbidden, Message: msg}
}

func NewNotFound(msg string) *DomainError {
	return &DomainError{Kind: ErrNotFound, Message: msg}
}

func NewConflict(msg string) *DomainError {
	return &DomainError{Kind: ErrConflict, Message: msg}
}

func NewValidation(msg string, fields map[string]string) *DomainError {
	return &DomainError{Kind: ErrValidation, Message: msg, Fields: fields}
}

func NewRuleError(msg string) *DomainError {
	return &DomainError{Kind: ErrRule, Message: msg}
}

func NewNotConfigured(msg string) *DomainError {
	return &DomainError{Kind: ErrNotConfigured, Message: msg}
}

// ErrorResponse — единый формат JSON-ответа при ошибке API.
type ErrorResponse struct {
	Error  string            `json:"error"`
	Code   string            `json:"code,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

// LegacyAPIEnvelope повторяет формат {data, errors} для обратной совместимости мобильного API.
type LegacyAPIEnvelope struct {
	Data   any `json:"data"`
	Errors any `json:"errors"`
}

// HTTPStatusForError определяет HTTP-код для доменной ошибки.
func HTTPStatusForError(err error) int {
	switch {
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrValidation), errors.Is(err, ErrRule):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrNotConfigured):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// WriteJSON сериализует ответ с заданным статусом.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil || status == http.StatusNoContent {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// DecodeJSON читает JSON-тело запроса с ограничением размера, чтобы защититься от слишком больших payload.
func DecodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return NewValidation("Пустое тело запроса", nil)
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	if err := dec.Decode(dst); err != nil {
		return NewValidation(fmt.Sprintf("Некорректный JSON: %v", err), nil)
	}
	return nil
}

// WriteError пишет безопасный JSON-ответ по классу ошибки; неизвестные ошибки логируются, но детали не уходят наружу.
func WriteError(w http.ResponseWriter, logger *slog.Logger, err error) {
	status := HTTPStatusForError(err)
	if status == http.StatusInternalServerError {
		if logger != nil {
			logger.Error("internal handler error", "error", err)
		}
		WriteJSON(w, status, ErrorResponse{
			Error: "Внутренняя ошибка сервера",
			Code:  "internal_error",
		})
		return
	}

	var domErr *DomainError
	if errors.As(err, &domErr) {
		WriteJSON(w, status, ErrorResponse{
			Error:  domErr.Error(),
			Code:   domErr.Kind.Error(),
			Fields: domErr.Fields,
		})
		return
	}

	WriteJSON(w, status, ErrorResponse{Error: err.Error()})
}
