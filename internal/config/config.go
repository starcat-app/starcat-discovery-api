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
	defaultStarHistoryEnabled      = false
	defaultStarHistoryCacheTTL     = 86400
	defaultStarHistoryNegativeTTL  = 600
	defaultStarHistoryBuildTimeout = 300
	defaultStarHistoryWorkers      = 1
	defaultStarHistoryQueue        = 32
	defaultStarHistoryMaxPoints    = 500
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
	StarHistoryEnabled      bool
	StarHistoryCacheTTL     int
	StarHistoryNegativeTTL  int
	StarHistoryBuildTimeout int
	StarHistoryWorkers      int
	StarHistoryQueue        int
	StarHistoryMaxPoints    int
	GCPProjectID            string
	GCPCredentialsJSON      string
	BigQueryMaxBytesBilled  int64
	StarHistoryDailyBudget  int64
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
		StarHistoryEnabled:      boolEnv("STAR_HISTORY_ENABLED", defaultStarHistoryEnabled),
		StarHistoryCacheTTL:     intEnv("STAR_HISTORY_CACHE_TTL_SECONDS", defaultStarHistoryCacheTTL),
		StarHistoryNegativeTTL:  intEnv("STAR_HISTORY_NEGATIVE_TTL_SECONDS", defaultStarHistoryNegativeTTL),
		StarHistoryBuildTimeout: intEnv("STAR_HISTORY_BUILD_TIMEOUT_SECONDS", defaultStarHistoryBuildTimeout),
		StarHistoryWorkers:      intEnv("STAR_HISTORY_WORKER_CONCURRENCY", defaultStarHistoryWorkers),
		StarHistoryQueue:        intEnv("STAR_HISTORY_QUEUE_CAPACITY", defaultStarHistoryQueue),
		StarHistoryMaxPoints:    intEnv("STAR_HISTORY_MAX_POINTS", defaultStarHistoryMaxPoints),
		GCPProjectID:            stringEnv("GCP_PROJECT_ID", ""),
		GCPCredentialsJSON:      stringEnv("GOOGLE_APPLICATION_CREDENTIALS_JSON", ""),
		BigQueryMaxBytesBilled:  int64Env("BIGQUERY_MAX_BYTES_BILLED", 0),
		StarHistoryDailyBudget:  int64Env("STAR_HISTORY_DAILY_MAX_BYTES_BILLED", 0),
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
	if cfg.StarHistoryEnabled {
		if cfg.GCPProjectID == "" {
			return Config{}, fmt.Errorf("GCP_PROJECT_ID env is required when STAR_HISTORY_ENABLED=true")
		}
		if cfg.BigQueryMaxBytesBilled <= 0 {
			return Config{}, fmt.Errorf("BIGQUERY_MAX_BYTES_BILLED env must be positive when STAR_HISTORY_ENABLED=true")
		}
		if cfg.StarHistoryDailyBudget < cfg.BigQueryMaxBytesBilled {
			return Config{}, fmt.Errorf("STAR_HISTORY_DAILY_MAX_BYTES_BILLED must cover at least one query")
		}
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

func int64Env(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
