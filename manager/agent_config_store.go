package manager

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

const (
	agentConfigKeyID      = "env:HOMECTL_CONFIG_ENCRYPTION_KEY"
	sharedAgentConfigName = "__shared__"
)

type AgentConfigService struct {
	repo AgentConfigRepository
	aead cipher.AEAD
}

func NewAgentConfigService(repo AgentConfigRepository) (*AgentConfigService, error) {
	return NewAgentConfigServiceWithKey(repo, os.Getenv("HOMECTL_CONFIG_ENCRYPTION_KEY"))
}

func NewAgentConfigServiceWithKey(repo AgentConfigRepository, keyValue string) (*AgentConfigService, error) {
	keyValue = strings.TrimSpace(keyValue)
	if keyValue == "" {
		return &AgentConfigService{repo: repo}, nil
	}
	key, err := base64.RawStdEncoding.DecodeString(keyValue)
	if err != nil {
		key, err = base64.StdEncoding.DecodeString(keyValue)
	}
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("HOMECTL_CONFIG_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AgentConfigService{repo: repo, aead: aead}, nil
}

func (s *AgentConfigService) Upsert(ctx context.Context, agentName string, cfg json.RawMessage) (AgentConfigRecord, error) {
	if !json.Valid(cfg) {
		return AgentConfigRecord{}, fmt.Errorf("agent config must be valid JSON")
	}
	return s.upsertPlain(ctx, agentName, cfg)
}

func (s *AgentConfigService) UpsertNodeConfig(ctx context.Context, agentName string, req common.NodeConfigUIRequest) (common.NodeConfigUIResponse, error) {
	if strings.TrimSpace(agentName) == "" {
		return common.NodeConfigUIResponse{}, fmt.Errorf("agent name required")
	}
	shared := sanitizeConfig(req.Shared)
	agent := sanitizeConfig(req.Agent)
	if agent[common.ConfigKeyAgentName] == "" {
		agent[common.ConfigKeyAgentName] = agentName
	}
	if _, err := s.upsertDocument(ctx, sharedAgentConfigName, shared); err != nil {
		return common.NodeConfigUIResponse{}, err
	}
	if _, err := s.upsertDocument(ctx, agentName, agent); err != nil {
		return common.NodeConfigUIResponse{}, err
	}
	return s.GetForUI(ctx, agentName)
}

func (s *AgentConfigService) GetForUI(ctx context.Context, agentName string) (common.NodeConfigUIResponse, error) {
	shared, sharedRec, err := s.getDocument(ctx, sharedAgentConfigName)
	if err != nil {
		return common.NodeConfigUIResponse{}, err
	}
	agent, agentRec, err := s.getDocument(ctx, agentName)
	if err != nil {
		return common.NodeConfigUIResponse{}, err
	}
	if agent == nil {
		agent = common.NodeConfigDocument{}
	}
	if agent[common.ConfigKeyAgentName] == "" {
		agent[common.ConfigKeyAgentName] = agentName
	}
	updated := latestTime(sharedRec.UpdatedAt, agentRec.UpdatedAt)
	return common.NodeConfigUIResponse{
		AgentName: agentName,
		Version:   aggregateConfigVersion(sharedRec, agentRec),
		Shared:    fillConfigKeys(shared, common.SharedAgentConfigKeys),
		Agent:     fillConfigKeys(agent, common.AgentSpecificConfigKeys),
		UpdatedAt: formatConfigTime(updated),
	}, nil
}

func (s *AgentConfigService) GetPlaintext(ctx context.Context, agentName string) (common.AgentConfigSyncResponse, error) {
	if strings.TrimSpace(agentName) == "" {
		return common.AgentConfigSyncResponse{}, fmt.Errorf("agent name required")
	}
	shared, sharedRec, err := s.getDocument(ctx, sharedAgentConfigName)
	if err != nil {
		return common.AgentConfigSyncResponse{}, err
	}
	agent, agentRec, err := s.getDocument(ctx, agentName)
	if err != nil {
		if len(shared) == 0 {
			if raw, rawRec, rawErr := s.getRaw(ctx, agentName); rawErr == nil {
				return common.AgentConfigSyncResponse{
					AgentName: rawRec.AgentName,
					Version:   rawRec.Version,
					Config:    json.RawMessage(raw),
					UpdatedAt: formatConfigTime(rawRec.UpdatedAt),
				}, nil
			}
		}
		return common.AgentConfigSyncResponse{}, err
	}
	merged := common.NodeConfigDocument{}
	for k, v := range shared {
		merged[k] = v
	}
	for k, v := range agent {
		merged[k] = v
	}
	if merged[common.ConfigKeyAgentName] == "" {
		merged[common.ConfigKeyAgentName] = agentName
	}
	if len(merged) == 0 {
		return common.AgentConfigSyncResponse{AgentName: agentName}, nil
	}
	plain, err := json.Marshal(merged)
	if err != nil {
		return common.AgentConfigSyncResponse{}, err
	}
	updated := latestTime(sharedRec.UpdatedAt, agentRec.UpdatedAt)
	return common.AgentConfigSyncResponse{
		AgentName: agentName,
		Version:   aggregateConfigVersion(sharedRec, agentRec),
		Config:    json.RawMessage(plain),
		UpdatedAt: formatConfigTime(updated),
	}, nil
}

func (s *AgentConfigService) SeedFromEnv(ctx context.Context) error {
	shared, rec, err := s.getDocument(ctx, sharedAgentConfigName)
	if err != nil {
		return err
	}
	if rec.AgentName != "" {
		return nil
	}
	shared = common.NodeConfigDocument{}
	for _, key := range common.SharedAgentConfigKeys {
		envKey := key
		if key == common.ConfigKeyLocalHAIP {
			envKey = "HOMECTL_LOCAL_HA_IP"
		}
		if val := strings.TrimSpace(os.Getenv(envKey)); val != "" {
			shared[key] = val
		}
	}
	for _, key := range common.EditableAgentEnvConfigKeys {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			shared[key] = val
		}
	}
	if len(shared) == 0 {
		return nil
	}
	_, err = s.upsertDocument(ctx, sharedAgentConfigName, shared)
	return err
}

func (s *AgentConfigService) SeedAgentValues(ctx context.Context, agentName string, values common.NodeConfigDocument) error {
	if strings.TrimSpace(agentName) == "" || len(values) == 0 {
		return nil
	}
	shared, _, err := s.getDocument(ctx, sharedAgentConfigName)
	if err != nil {
		return err
	}
	if shared == nil {
		shared = common.NodeConfigDocument{}
	}
	sharedChanged := false
	for _, key := range common.SharedAgentConfigKeys {
		if _, exists := shared[key]; exists {
			continue
		}
		if val := strings.TrimSpace(values[key]); val != "" {
			shared[key] = val
			sharedChanged = true
		}
	}
	for _, key := range common.EditableAgentEnvConfigKeys {
		if _, exists := shared[key]; exists {
			continue
		}
		if val := strings.TrimSpace(values[key]); val != "" {
			shared[key] = val
			sharedChanged = true
		}
	}
	if sharedChanged {
		if _, err := s.upsertDocument(ctx, sharedAgentConfigName, shared); err != nil {
			return err
		}
	}

	agent, _, err := s.getDocument(ctx, agentName)
	if err != nil {
		return err
	}
	if agent == nil {
		agent = common.NodeConfigDocument{}
	}
	agentChanged := false
	for _, key := range common.AgentSpecificConfigKeys {
		if _, exists := agent[key]; exists {
			continue
		}
		if val := strings.TrimSpace(values[key]); val != "" {
			agent[key] = val
			agentChanged = true
		}
	}
	if agentChanged {
		_, err := s.upsertDocument(ctx, agentName, agent)
		return err
	}
	return nil
}

func (s *AgentConfigService) SharedValue(ctx context.Context, key string) (string, bool) {
	shared, _, err := s.getDocument(ctx, sharedAgentConfigName)
	if err != nil || shared == nil {
		return "", false
	}
	value, ok := shared[key]
	return strings.TrimSpace(value), ok
}

func (s *AgentConfigService) upsertDocument(ctx context.Context, agentName string, doc common.NodeConfigDocument) (AgentConfigRecord, error) {
	if doc == nil {
		doc = common.NodeConfigDocument{}
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return AgentConfigRecord{}, err
	}
	return s.upsertPlain(ctx, agentName, data)
}

func (s *AgentConfigService) upsertPlain(ctx context.Context, agentName string, cfg json.RawMessage) (AgentConfigRecord, error) {
	if s == nil || s.repo == nil {
		return AgentConfigRecord{}, fmt.Errorf("agent config service unavailable")
	}
	if s.aead == nil {
		return AgentConfigRecord{}, fmt.Errorf("HOMECTL_CONFIG_ENCRYPTION_KEY is required to store agent config")
	}
	nonce, err := randomBytes(s.aead.NonceSize())
	if err != nil {
		return AgentConfigRecord{}, err
	}
	ciphertext := s.aead.Seal(nil, nonce, cfg, []byte(agentName))
	return s.repo.UpsertAgentConfig(ctx, agentName, nonce, ciphertext, agentConfigKeyID)
}

func (s *AgentConfigService) getRaw(ctx context.Context, agentName string) ([]byte, AgentConfigRecord, error) {
	if s == nil || s.repo == nil {
		return nil, AgentConfigRecord{}, fmt.Errorf("agent config service unavailable")
	}
	rec, err := s.repo.GetAgentConfig(ctx, agentName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, AgentConfigRecord{}, ErrNotFound
		}
		return nil, AgentConfigRecord{}, err
	}
	if s.aead == nil {
		return nil, AgentConfigRecord{}, fmt.Errorf("HOMECTL_CONFIG_ENCRYPTION_KEY is required to read agent config")
	}
	plain, err := s.aead.Open(nil, rec.Nonce, rec.Ciphertext, []byte(agentName))
	if err != nil {
		return nil, AgentConfigRecord{}, err
	}
	return plain, rec, nil
}

func (s *AgentConfigService) getDocument(ctx context.Context, agentName string) (common.NodeConfigDocument, AgentConfigRecord, error) {
	plain, rec, err := s.getRaw(ctx, agentName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return common.NodeConfigDocument{}, AgentConfigRecord{}, nil
		}
		return nil, AgentConfigRecord{}, err
	}
	var doc common.NodeConfigDocument
	if err := json.Unmarshal(plain, &doc); err != nil {
		return nil, AgentConfigRecord{}, err
	}
	return doc, rec, nil
}

func filterConfig(in common.NodeConfigDocument, allowed []string) common.NodeConfigDocument {
	out := common.NodeConfigDocument{}
	for _, key := range allowed {
		out[key] = ""
		if in != nil {
			out[key] = strings.TrimSpace(in[key])
		}
	}
	return out
}

func sanitizeConfig(in common.NodeConfigDocument) common.NodeConfigDocument {
	out := common.NodeConfigDocument{}
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}

func fillConfigKeys(in common.NodeConfigDocument, keys []string) common.NodeConfigDocument {
	out := common.NodeConfigDocument{}
	for key, value := range in {
		out[key] = value
	}
	for _, key := range keys {
		if _, ok := out[key]; !ok {
			out[key] = ""
		}
	}
	return out
}

func latestTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func aggregateConfigVersion(a, b AgentConfigRecord) int64 {
	updated := latestTime(a.UpdatedAt, b.UpdatedAt)
	if !updated.IsZero() {
		return updated.UnixNano()
	}
	if a.Version > b.Version {
		return a.Version
	}
	return b.Version
}

func formatConfigTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
