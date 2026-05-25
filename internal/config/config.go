package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

type Feed struct {
	URL                string `json:"url"`
	Title              string `json:"title,omitempty"`
	RemoveDeletedAfter int    `json:"remove_deleted_after,omitempty"` // Days to keep deleted articles (overrides global setting)
	Enabled            *bool  `json:"enabled,omitempty"`              // Whether the feed is enabled (default: true)
}

type Config struct {
	RefreshTime        int    `json:"refresh_time"`
	SetAsReadAfter     int    `json:"set_as_read_after"`    // Seconds to wait before auto-marking as read (default: 5)
	RemoveDeletedAfter int    `json:"remove_deleted_after"` // Days to keep deleted articles before permanent deletion (default: 30)
	Feeds              []Feed `json:"feeds"`
}

var AppFs = afero.NewOsFs()

func GetConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "noodle", "config.json"), nil
}

func LoadConfig() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	// Create default config if file doesn't exist
	if exists, _ := afero.Exists(AppFs, configPath); !exists {
		config := &Config{
			RefreshTime:        300,
			SetAsReadAfter:     5,  // Default 5 seconds
			RemoveDeletedAfter: 30, // Default 30 days
			Feeds:              []Feed{},
		}
		if err := SaveConfig(config); err != nil {
			return nil, err
		}
		return config, nil
	}

	data, err := afero.ReadFile(AppFs, configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set default values if not specified
	if config.SetAsReadAfter == 0 {
		config.SetAsReadAfter = 5
	}
	if config.RemoveDeletedAfter == 0 {
		config.RemoveDeletedAfter = 30
	}

	return &config, nil
}

func SaveConfig(config *Config) error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(configPath)
	if err := AppFs.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := afero.WriteFile(AppFs, configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func AddFeed(config *Config, feed Feed) error {
	config.Feeds = append(config.Feeds, feed)
	return SaveConfig(config)
}

func UpdateFeed(config *Config, index int, feed Feed) error {
	if index < 0 || index >= len(config.Feeds) {
		return fmt.Errorf("invalid feed index: %d", index)
	}
	config.Feeds[index] = feed
	return SaveConfig(config)
}

func DeleteFeed(config *Config, index int) error {
	if index < 0 || index >= len(config.Feeds) {
		return fmt.Errorf("invalid feed index: %d", index)
	}
	config.Feeds = append(config.Feeds[:index], config.Feeds[index+1:]...)
	return SaveConfig(config)
}

// IsEnabled returns true if the feed is enabled (defaults to true if not specified)
func (f *Feed) IsEnabled() bool {
	if f.Enabled == nil {
		return true // default to enabled
	}
	return *f.Enabled
}
