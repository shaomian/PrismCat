package config

import "time"

// CorrelationConfig intentionally defaults to disabled. Matching stays conservative.
//
// MaxEntries 与 IdleMinutes 是自动关联的内存边界(2024-10 起可通过配置调整):
// 显式 Trace ID 与单任务录制是权威声明,不经过这套索引,不受这两个值约束。
type CorrelationConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	// MaxEntries 内存关联索引的观测条数上限,超出按最旧淘汰。<=0 使用默认值。
	MaxEntries int `yaml:"max_entries" json:"max_entries"`
	// IdleMinutes 稀疏调用的匹配窗口(分钟),窗口外的观测不再参与自动关联。<=0 使用默认值。
	IdleMinutes int `yaml:"idle_minutes" json:"idle_minutes"`
}

// 与 internal/trace 的默认值保持一致(那边是行为常量,这边负责配置归一化)。
const (
	DefaultCorrelationMaxEntries  = 2048
	DefaultCorrelationIdleMinutes = 30

	MinCorrelationMaxEntries  = 16
	MaxCorrelationMaxEntries  = 100000
	MaxCorrelationIdleMinutes = 1440
)

// NormalizeCorrelation 把缺失(0/负值)归为默认值,把超出范围收敛到边界。
// 旧配置文件没有这两个字段时走零值路径,得到默认值,保持兼容。
func NormalizeCorrelation(in CorrelationConfig) CorrelationConfig {
	if in.MaxEntries <= 0 {
		in.MaxEntries = DefaultCorrelationMaxEntries
	}
	in.MaxEntries = min(max(in.MaxEntries, MinCorrelationMaxEntries), MaxCorrelationMaxEntries)
	if in.IdleMinutes <= 0 {
		in.IdleMinutes = DefaultCorrelationIdleMinutes
	}
	in.IdleMinutes = min(in.IdleMinutes, MaxCorrelationIdleMinutes)
	return in
}

// IdleWindow 返回空闲匹配窗口,供需要 time.Duration 的调用方使用。
func (c CorrelationConfig) IdleWindow() time.Duration {
	return time.Duration(c.IdleMinutes) * time.Minute
}

func (c *Config) CorrelationSnapshot() CorrelationConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Correlation
}
