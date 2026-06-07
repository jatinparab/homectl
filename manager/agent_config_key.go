package manager

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
)

const agentConfigEncryptionKeyFile = "agent-config-encryption.key"

func ensureAgentConfigEncryptionKey(stateDir string) (string, error) {
	if key := strings.TrimSpace(os.Getenv("HOMECTL_CONFIG_ENCRYPTION_KEY")); key != "" {
		return key, nil
	}
	path := filepath.Join(stateDir, agentConfigEncryptionKeyFile)
	if data, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(data)), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	keyBytes, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	key := base64.RawStdEncoding.EncodeToString(keyBytes)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(key+"\n"), 0600); err != nil {
		return "", err
	}
	managerLog.Info("Generated agent config encryption key", "path", path)
	return key, nil
}
