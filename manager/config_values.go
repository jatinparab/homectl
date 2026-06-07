package manager

import (
	"context"
	"os"

	"github.com/jatinparab/homectl/common"
)

func (m *Manager) configValue(key string) string {
	if m != nil && m.agentConfig != nil {
		if value, ok := m.agentConfig.SharedValue(context.Background(), key); ok {
			return value
		}
	}
	if key == common.ConfigKeyLocalHAIP {
		return os.Getenv("HOMECTL_LOCAL_HA_IP")
	}
	return os.Getenv(key)
}
