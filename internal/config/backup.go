package config

import "gopkg.in/yaml.v3"

// BackupSnapshot includes stored secrets, but never captures an environment-only
// UI password. Environment overrides for portable non-secret settings are kept.
func (c *Config) BackupSnapshot() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	saved := persistedConfig{
		Server: c.Server, Upstreams: c.Upstreams, Logging: c.Logging,
		LogRules: c.LogRules, Storage: c.Storage, Overrides: c.Overrides,
		Usage: c.Usage, Analysis: c.Analysis, Correlation: c.Correlation,
	}
	if c.envUIPassword {
		saved.Server.UIPassword = c.fileUIPassword
	}
	return yaml.Marshal(saved)
}

func (c *Config) FilePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.configPath
}

// ResumeCleanup persists the protection change before allowing any deletion.
func (c *Config) ResumeCleanup() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	wasPaused := c.Storage.CleanupPaused
	c.Storage.CleanupPaused = false
	if err := c.saveLocked(); err != nil {
		c.Storage.CleanupPaused = wasPaused
		return err
	}
	return nil
}
