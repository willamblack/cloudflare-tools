package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigUsesEnvironmentOverrides(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("ADMIN_USERNAME", "operator")
	t.Setenv("ADMIN_PASSWORD", "a-long-random-password")

	if err := LoadConfig(); err != nil {
		t.Fatalf("LoadConfig returned an error: %v", err)
	}
	if GlobalConfig.Admin.Username != "operator" || GlobalConfig.Admin.Password != "a-long-random-password" {
		t.Fatalf("environment overrides were not applied: %#v", GlobalConfig.Admin)
	}
}

func TestLoadConfigRejectsDefaultPassword(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	t.Setenv("ADMIN_USERNAME", "")
	t.Setenv("ADMIN_PASSWORD", "")
	configData := []byte("admin:\n  username: admin\n  password: CHANGE_ME\n")
	if err := os.WriteFile(filepath.Join(dataDir, "config.yaml"), configData, 0600); err != nil {
		t.Fatal(err)
	}

	err := LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "default administrator password") {
		t.Fatalf("expected default password rejection, got %v", err)
	}
}
