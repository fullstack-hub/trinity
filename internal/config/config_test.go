package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
servers:
  claude:
    url: http://localhost:3100
  gemini:
    url: http://localhost:3200
default_agent: claude
`), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Servers["claude"].URL != "http://localhost:3100" {
		t.Errorf("unexpected claude url: %s", cfg.Servers["claude"].URL)
	}
	if cfg.DefaultAgent != "claude" {
		t.Errorf("unexpected default: %s", cfg.DefaultAgent)
	}
}
