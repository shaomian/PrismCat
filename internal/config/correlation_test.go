package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeCorrelationDefaultsAndClamps(t *testing.T) {
	cases := []struct {
		name                     string
		in                       CorrelationConfig
		wantMaxEntries, wantIdle int
	}{
		{"zero uses defaults", CorrelationConfig{}, DefaultCorrelationMaxEntries, DefaultCorrelationIdleMinutes},
		{"negative uses defaults", CorrelationConfig{MaxEntries: -5, IdleMinutes: -1}, DefaultCorrelationMaxEntries, DefaultCorrelationIdleMinutes},
		{"valid values kept", CorrelationConfig{MaxEntries: 4096, IdleMinutes: 120}, 4096, 120},
		{"below minimum clamped", CorrelationConfig{MaxEntries: 8}, MinCorrelationMaxEntries, DefaultCorrelationIdleMinutes},
		{"above maximum clamped", CorrelationConfig{MaxEntries: 1_000_000, IdleMinutes: 99999}, MaxCorrelationMaxEntries, MaxCorrelationIdleMinutes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeCorrelation(tc.in)
			if got.MaxEntries != tc.wantMaxEntries || got.IdleMinutes != tc.wantIdle {
				t.Fatalf("NormalizeCorrelation(%+v) = maxEntries %d idle %d, want %d/%d", tc.in, got.MaxEntries, got.IdleMinutes, tc.wantMaxEntries, tc.wantIdle)
			}
		})
	}
}

func TestLoadCorrelationTuning(t *testing.T) {
	dir := t.TempDir()

	legacyPath := filepath.Join(dir, "legacy.yaml")
	// 旧配置只有 enabled,没有调优字段 → 必须拿到默认值
	if err := os.WriteFile(legacyPath, []byte("trace_correlation:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(legacyPath)
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}
	snap := legacy.CorrelationSnapshot()
	if !snap.Enabled || snap.MaxEntries != DefaultCorrelationMaxEntries || snap.IdleMinutes != DefaultCorrelationIdleMinutes {
		t.Fatalf("legacy config tuning: %+v", snap)
	}

	tunedPath := filepath.Join(dir, "tuned.yaml")
	if err := os.WriteFile(tunedPath, []byte("trace_correlation:\n  enabled: true\n  max_entries: 100000\n  idle_minutes: 720\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tuned, err := Load(tunedPath)
	if err != nil {
		t.Fatalf("load tuned config: %v", err)
	}
	snap = tuned.CorrelationSnapshot()
	if snap.MaxEntries != 100000 || snap.IdleMinutes != 720 || snap.IdleWindow().Minutes() != 720 {
		t.Fatalf("tuned config: %+v", snap)
	}
}
