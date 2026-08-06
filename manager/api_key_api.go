package manager

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type apiKeyResponse struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
}

type apiKeyCreatedResponse struct {
	apiKeyResponse
	Key string `json:"key"`
}

func (m *Manager) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		m.listAPIKeys(w, r, user)
	case http.MethodPost:
		m.createAPIKey(w, r, user)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *Manager) handleAPIKey(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/api-keys/")
	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		http.Error(w, "invalid api key id", http.StatusBadRequest)
		return
	}

	if err := m.store.APIKeys.DeleteAPIKey(r.Context(), id, user.ID); err != nil {
		writeAPIKeyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) listAPIKeys(w http.ResponseWriter, r *http.Request, user User) {
	keys, err := m.store.APIKeys.ListAPIKeys(r.Context(), user.ID)
	if err != nil {
		writeAPIKeyError(w, err)
		return
	}
	if keys == nil {
		keys = []APIKey{}
	}
	resp := make([]apiKeyResponse, len(keys))
	for i, k := range keys {
		resp[i] = apiKeyResponse{
			ID:         k.ID,
			Name:       k.Name,
			Prefix:     k.Prefix,
			CreatedAt:  k.CreatedAt.UTC().Format(time.RFC3339),
			LastUsedAt: k.LastUsedAt.UTC().Format(time.RFC3339),
		}
	}
	writeJSON(w, resp)
}

func (m *Manager) createAPIKey(w http.ResponseWriter, r *http.Request, user User) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	rawKey, err := randomToken(32)
	if err != nil {
		http.Error(w, "failed to generate key", http.StatusInternalServerError)
		return
	}
	keyHash := tokenHash(rawKey)
	prefix := rawKey[:8]

	apiKey, err := m.store.APIKeys.CreateAPIKey(r.Context(), user.ID, req.Name, keyHash, prefix)
	if err != nil {
		writeAPIKeyError(w, err)
		return
	}

	resp := apiKeyCreatedResponse{
		apiKeyResponse: apiKeyResponse{
			ID:         apiKey.ID,
			Name:       apiKey.Name,
			Prefix:     apiKey.Prefix,
			CreatedAt:  apiKey.CreatedAt.UTC().Format(time.RFC3339),
			LastUsedAt: apiKey.LastUsedAt.UTC().Format(time.RFC3339),
		},
		Key: rawKey,
	}
	writeJSON(w, resp)
}

func writeAPIKeyError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
