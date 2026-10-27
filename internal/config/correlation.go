package config

// CorrelationConfig intentionally defaults to disabled. Matching stays conservative.
type CorrelationConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

func (c *Config) CorrelationSnapshot() CorrelationConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Correlation
}
