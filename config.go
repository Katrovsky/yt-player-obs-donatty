package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type Config struct {
	Port                int    `json:"port"`
	MaxDurationMinutes  int    `json:"max_duration_minutes"`
	MinViews            int    `json:"min_views"`
	RepeatLimit         int    `json:"repeat_limit"`
	CleanupAfterHours   int    `json:"cleanup_after_hours"`
	MaxQueueSize        int    `json:"max_queue_size"`
	DonationWidgetURL   string `json:"donation_widget_url"`
	DonationMinAmount   int    `json:"donation_min_amount"`
	YouTubeAPIKey       string `json:"youtube_api_key"`
	FallbackPlaylistURL string `json:"fallback_playlist_url"`
}

type ConfigManager struct {
	mu  sync.RWMutex
	cfg Config
}

func loadConfig() (*ConfigManager, error) {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.MaxQueueSize == 0 {
		cfg.MaxQueueSize = 100
	}
	return &ConfigManager{cfg: cfg}, nil
}

func (m *ConfigManager) get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *ConfigManager) watch() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Error creating watcher:", err)
	}
	defer watcher.Close()
	if err := watcher.Add(filepath.Dir("config.json")); err != nil {
		log.Fatal("Error watching config directory:", err)
	}
	for {
		select {
		case event := <-watcher.Events:
			if filepath.Base(event.Name) == "config.json" && event.Has(fsnotify.Write) {
				m.reload()
			}
		case err := <-watcher.Errors:
			log.Println("Watcher error:", err)
		}
	}
}

func (m *ConfigManager) reload() {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	if cfg.MaxQueueSize == 0 {
		cfg.MaxQueueSize = 100
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	log.Println("Config reloaded")
}
