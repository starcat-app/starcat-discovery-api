// Package config 负责读取 starcat-discovery-api 的运行时配置。
//
// 这里集中处理默认值和必填项，避免 main.go 在服务装配时散落环境变量细节。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	defaultPort                    = "5006"
	defaultStoreFile               = "./discovery.db"
	defaultSyncEnabled             = true
	defaultSyncCron                = "17 */3 * * *"
	defaultFullSyncCron            = "23 2 * * *"
	defaultCacheTTLSeconds         = 10800
	defaultMaxSearchCallsPerMinute = 28
	defaultRateLimitFloor          = 50
	defaultFeedTargetSize          = 500
)

// Config 是服务启动所需的完整配置。
type Config struct {
	Port                    string
	StoreFile               string
	APIKeys                 []string
	AdminAPIKeys            []string
	GitHubTokens            []string
	SyncEnabled             bool
	SyncCron                string
	FullSyncCron            string
	CacheTTLSeconds         int
	MaxSearchCallsPerMinute int
	RateLimitFloor          int
	FeedTargetSize          int
}

// Load 从环境变量读取配置，并校验必填项。
func Load() (Config, error) {
	cfg := Config{
		Port:                    stringEnv("PORT", defaultPort),
		StoreFile:               stringEnv("STORE_FILE", defaultStoreFile),
		APIKeys:                 listEnv("API_KEYS"),
		AdminAPIKeys:            listEnv("ADMIN_API_KEYS"),
		GitHubTokens:            listEnv("GITHUB_TOKENS"),
		SyncEnabled:             boolEnv("SYNC_ENABLED", defaultSyncEnabled),
		SyncCron:                stringEnv("SYNC_CRON", defaultSyncCron),
		FullSyncCron:            stringEnv("FULL_SYNC_CRON", defaultFullSyncCron),
		CacheTTLSeconds:         intEnv("CACHE_TTL_SECONDS", defaultCacheTTLSeconds),
		MaxSearchCallsPerMinute: intEnv("MAX_SEARCH_CALLS_PER_MINUTE", defaultMaxSearchCallsPerMinute),
		RateLimitFloor:          intEnv("RATE_LIMIT_FLOOR", defaultRateLimitFloor),
		FeedTargetSize:          intEnv("FEED_TARGET_SIZE", defaultFeedTargetSize),
	}
	if len(cfg.APIKeys) == 0 {
		return Config{}, fmt.Errorf("API_KEYS env is required")
	}
	if len(cfg.AdminAPIKeys) == 0 {
		return Config{}, fmt.Errorf("ADMIN_API_KEYS env is required")
	}
	if len(cfg.GitHubTokens) == 0 {
		return Config{}, fmt.Errorf("GITHUB_TOKENS env is required")
	}
	return cfg, nil
}

func stringEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func listEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func boolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func intEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
