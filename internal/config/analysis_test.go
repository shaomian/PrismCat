package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestAnalysisProtocolLegacyDefaultAndPersistence(t *testing.T) {
	for _, protocol := range []string{"", "openai", " Anthropic ", "unsupported"} {
		t.Run(protocol, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			source := fmt.Sprintf("storage:\n  database: %q\n  blob_dir: %q\nanalysis:\n  protocol: %q\n  api_key: test-key\n", filepath.Join(dir, "data.db"), filepath.Join(dir, "blobs"), protocol)
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if protocol == "unsupported" {
				if err == nil {
					t.Fatal("unsupported protocol accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "openai"
			if protocol == " Anthropic " {
				want = "anthropic"
			}
			if cfg.AnalysisSnapshot().Protocol != want {
				t.Fatal("incorrect protocol default or normalization")
			}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			reloaded, err := Load(path)
			if err != nil || reloaded.Analysis.Protocol != want || reloaded.Analysis.APIKey != "test-key" {
				t.Fatal("protocol or key lost on save")
			}
		})
	}
}
