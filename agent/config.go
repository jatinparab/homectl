package agent

import "time"

// Config holds agent configuration.
type Config struct {
	RenewalInterval   time.Duration
	RetryInterval     time.Duration
	ReconcileInterval time.Duration
	ConnectionTimeout time.Duration
}

// DefaultConfig returns the default agent configuration.
func DefaultConfig() Config {
	return Config{
		RenewalInterval:   10 * time.Second,
		RetryInterval:     5 * time.Second,
		ReconcileInterval: 5 * time.Second,
		ConnectionTimeout: 10 * time.Second,
	}
}
