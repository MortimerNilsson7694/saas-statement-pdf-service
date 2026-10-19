package statementsvc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func NewHTTPServer(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/tenants", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var in OnboardTenantInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tenant := service.OnboardTenant(in)
		writeJSON(w, http.StatusCreated, tenant)
	})

	mux.HandleFunc("/admin/tenants/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/tenants/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		tenantID, action := parts[0], parts[1]
		switch {
		case r.Method == http.MethodPost && action == "activate":
			tenant, err := service.ActivateTenant(tenantID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, tenant)
		case r.Method == http.MethodPost && action == "suspend":
			tenant, err := service.SuspendTenant(tenantID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, tenant)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/admin/statements/export", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var in ExportStatementInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := service.ExportStatement(r.Context(), in)
		if err != nil {
			status := http.StatusBadRequest
			if err.Error() == "tenant not found" {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/admin/webhooks/account-event", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ts := r.Header.Get("X-Timestamp")
		sig := r.Header.Get("X-Signature")
		if !VerifyWebhookSignature(service.webhookSecret, ts, body, sig) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"result": service.HandleAccountEvent(body)})
	})

	return mux
}

func VerifyWebhookSignature(secret, timestamp string, body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
