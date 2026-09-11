package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Admin struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"admin"`
}

var GlobalConfig Config

func DataPath(name string) string {
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		return name
	}
	return filepath.Join(dataDir, name)
}

func LoadConfig() error {
	GlobalConfig = Config{}
	configPaths := []string{DataPath("config.yaml"), "config/config.yaml", "config.yaml"}
	var data []byte
	var readErr error
	for _, configPath := range configPaths {
		data, readErr = os.ReadFile(configPath)
		if readErr == nil {
			break
		}
		if !os.IsNotExist(readErr) {
			return readErr
		}
	}
	if readErr == nil {
		if err := yaml.Unmarshal(data, &GlobalConfig); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}

	if username := strings.TrimSpace(os.Getenv("ADMIN_USERNAME")); username != "" {
		GlobalConfig.Admin.Username = username
	}
	if password := os.Getenv("ADMIN_PASSWORD"); password != "" {
		GlobalConfig.Admin.Password = password
	}

	username := strings.TrimSpace(GlobalConfig.Admin.Username)
	password := GlobalConfig.Admin.Password
	if username == "" || password == "" {
		return fmt.Errorf("admin username and password must be configured")
	}
	for _, insecure := range []string{"admin123", "password", "ChangeThisPassword123!", "CHANGE_ME"} {
		if password == insecure {
			return fmt.Errorf("refusing to start with the default administrator password")
		}
	}
	GlobalConfig.Admin.Username = username
	return nil
}
