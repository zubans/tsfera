package crm

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// SuggestAddressesDaData проксирует подсказки адресов через DaData API (при наличии ключа).
func (s *Service) SuggestAddressesDaData(ctx context.Context, query string, count int) ([]DaDataAddressSuggestion, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []DaDataAddressSuggestion{}, nil
	}
	if strings.TrimSpace(s.cfg.DaDataAPIKey) == "" {
		return []DaDataAddressSuggestion{}, nil
	}
	if count <= 0 || count > 20 {
		count = 10
	}
	baseURL := strings.TrimRight(s.cfg.DaDataBaseURL, "/")
	if baseURL == "" {
		baseURL = "https://suggestions.dadata.ru/suggestions/api/4_1/rs"
	}
	reqBody, _ := json.Marshal(map[string]any{
		"query": query,
		"count": count,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/suggest/address", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Token "+strings.TrimSpace(s.cfg.DaDataAPIKey))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса к DaData: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, platform.NewRuleError(fmt.Sprintf("DaData вернула HTTP %d", resp.StatusCode))
	}

	var parsed struct {
		Suggestions []struct {
			Value             string `json:"value"`
			UnrestrictedValue string `json:"unrestricted_value"`
			Data              struct {
				PostalCode string `json:"postal_code"`
				City       string `json:"city_with_type"`
				Street     string `json:"street_with_type"`
				House      string `json:"house"`
				Flat       string `json:"flat"`
				GeoLat     string `json:"geo_lat"`
				GeoLon     string `json:"geo_lon"`
			} `json:"data"`
		} `json:"suggestions"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([]DaDataAddressSuggestion, 0, len(parsed.Suggestions))
	for _, it := range parsed.Suggestions {
		out = append(out, DaDataAddressSuggestion{
			Value:             it.Value,
			UnrestrictedValue: it.UnrestrictedValue,
			PostalCode:        it.Data.PostalCode,
			City:              it.Data.City,
			Street:            it.Data.Street,
			House:             it.Data.House,
			Flat:              it.Data.Flat,
			GeoLat:            it.Data.GeoLat,
			GeoLon:            it.Data.GeoLon,
		})
	}
	return out, nil
}

// VerifyCloudPaymentsHMAC проверяет заголовок Content-HMAC (Base64 HMAC-SHA256) от CloudPayments.
func (s *Service) VerifyCloudPaymentsHMAC(body []byte, headerHMAC string) bool {
	secret := strings.TrimSpace(s.cfg.CloudPaymentsAPISecret)
	if secret == "" {
		return true
	}
	headerHMAC = strings.TrimSpace(headerHMAC)
	if headerHMAC == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(headerHMAC), []byte(expected))
}

func parseCloudPaymentsPayload(body []byte) map[string]string {
	res := map[string]string{}
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "{") {
		var raw map[string]any
		if json.Unmarshal(body, &raw) == nil {
			for k, v := range raw {
				res[k] = fmt.Sprint(v)
			}
			return res
		}
	}
	if vals, err := url.ParseQuery(trimmed); err == nil {
		for k, v := range vals {
			if len(v) > 0 {
				res[k] = v[0]
			}
		}
	}
	return res
}

// HandleCloudPaymentsWebhook обрабатывает события оплаты и рекуррентных подписок CloudPayments (pay / recurrent).
func (s *Service) HandleCloudPaymentsWebhook(ctx context.Context, kind string, rawBody []byte, headerHMAC string) error {
	if !s.VerifyCloudPaymentsHMAC(rawBody, headerHMAC) {
		return platform.NewUnauthorized("Неверная подпись Content-HMAC CloudPayments")
	}
	fields := parseCloudPaymentsPayload(rawBody)
	s.logChange(ctx, nil, "cloudpayments_"+kind, 0, "webhook", fields)

	// Уведомим диспетчеров / суперадминов о поступившем платеже или изменении подписки.
	txID := fields["TransactionId"]
	if txID == "" {
		txID = fields["Id"]
	}
	accountID := fields["AccountId"]
	amount := fields["Amount"]
	status := fields["Status"]

	if s.users != nil {
		if userIDs, err := s.users.ListUserIDsByPermission(ctx, "crm.contract.update"); err == nil {
			title := "CloudPayments: оплата #" + txID
			if kind == "recurrent" {
				title = "CloudPayments: рекуррентная подписка #" + txID
			}
			msg := strings.TrimSpace(fmt.Sprintf("AccountId: %s | Сумма: %s | Статус: %s", accountID, amount, status))
			for _, uid := range userIDs {
				s.pushNotification(ctx, uid, "cloudpayments", title, msg, "cloudpayments", 0)
			}
		}
	}
	return nil
}

// GetContractorSubscriptions возвращает локальные договоры ТО и (при настроенных ключах) активные подписки CloudPayments.
func (s *Service) GetContractorSubscriptions(ctx context.Context, auth *platform.AuthContext, contractorID int64) (map[string]any, error) {
	if _, err := s.GetContractor(ctx, auth, contractorID); err != nil {
		return nil, err
	}
	cid := contractorID
	contracts, err := s.repo.ListContracts(ctx, ContractFilter{ContractorID: &cid, Limit: 100})
	if err != nil {
		return nil, err
	}
	res := map[string]any{
		"contractor_id": contractorID,
		"configured":    strings.TrimSpace(s.cfg.CloudPaymentsPublicID) != "" && strings.TrimSpace(s.cfg.CloudPaymentsAPISecret) != "",
		"contracts":     contracts,
		"subscriptions": []any{},
	}
	if strings.TrimSpace(s.cfg.CloudPaymentsPublicID) == "" || strings.TrimSpace(s.cfg.CloudPaymentsAPISecret) == "" {
		return res, nil
	}

	subs, err := s.fetchCloudPaymentsSubscriptions(ctx, strconv.FormatInt(contractorID, 10))
	if err != nil {
		res["cloudpayments_error"] = err.Error()
		return res, nil
	}
	res["subscriptions"] = subs
	return res, nil
}

// CheckAddressSubscription проверяет наличие активного договора ТО и подписки CloudPayments по адресу.
func (s *Service) CheckAddressSubscription(ctx context.Context, auth *platform.AuthContext, addressID int64) (map[string]any, error) {
	addr, err := s.repo.GetAddress(ctx, addressID)
	if err != nil {
		return nil, err
	}
	if addr == nil {
		return nil, platform.NewNotFound("Адрес не найден")
	}
	if auth != nil {
		if ok, err := s.CanViewContractor(ctx, auth, addr.ContractorID); err != nil {
			return nil, err
		} else if !ok {
			return nil, platform.NewForbidden("Нет доступа к контрагенту этого адреса")
		}
	}
	aid := addressID
	contracts, err := s.repo.ListContracts(ctx, ContractFilter{AddressID: &aid, Limit: 50})
	if err != nil {
		return nil, err
	}
	var activeContracts []MaintenanceContract
	now := s.now()
	for _, c := range contracts {
		if c.Status == StatusActive && !c.ValidTo.Before(now) {
			activeContracts = append(activeContracts, c)
		}
	}
	res := map[string]any{
		"address_id":          addressID,
		"contractor_id":       addr.ContractorID,
		"has_active_contract": len(activeContracts) > 0,
		"active_contracts":    activeContracts,
		"configured":          strings.TrimSpace(s.cfg.CloudPaymentsPublicID) != "" && strings.TrimSpace(s.cfg.CloudPaymentsAPISecret) != "",
		"subscriptions":       []any{},
	}
	if strings.TrimSpace(s.cfg.CloudPaymentsPublicID) != "" && strings.TrimSpace(s.cfg.CloudPaymentsAPISecret) != "" {
		if subs, err := s.fetchCloudPaymentsSubscriptions(ctx, strconv.FormatInt(addr.ContractorID, 10)); err == nil {
			res["subscriptions"] = subs
		} else {
			res["cloudpayments_error"] = err.Error()
		}
	}
	return res, nil
}

func (s *Service) fetchCloudPaymentsSubscriptions(ctx context.Context, accountID string) ([]any, error) {
	baseURL := strings.TrimRight(s.cfg.CloudPaymentsBaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.cloudpayments.ru"
	}
	payload, _ := json.Marshal(map[string]string{"accountId": accountID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/subscriptions/find", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(strings.TrimSpace(s.cfg.CloudPaymentsPublicID), strings.TrimSpace(s.cfg.CloudPaymentsAPISecret))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("CloudPayments HTTP %d", resp.StatusCode)
	}
	var out struct {
		Success bool   `json:"Success"`
		Message string `json:"Message"`
		Model   []any  `json:"Model"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	if !out.Success && out.Message != "" {
		return nil, fmt.Errorf("%s", out.Message)
	}
	if out.Model == nil {
		return []any{}, nil
	}
	return out.Model, nil
}
