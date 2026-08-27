// Package config 负责读取 starcat-discovery-api 的运行时配置。
//
// 这里集中处理默认值和必填项，避免 main.go 在服务装配时散落环境变量细节。
// 环境变量解析收敛到 starcat-api-kit/env，本包只保留业务校验。
package config

import (
	"fmt"

	kitenv "github.com/starcat-app/starcat-api-kit/env"
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
	MetricsStoreFile        string
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
		Port:                    kitenv.OrDefault("PORT", defaultPort),
		StoreFile:               kitenv.OrDefault("STORE_FILE", defaultStoreFile),
		MetricsStoreFile:        kitenv.OrDefault("METRICS_STORE_FILE", "./discovery-metrics.db"),
		APIKeys:                 kitenv.LookupCSV("API_KEYS"),
		AdminAPIKeys:            kitenv.LookupCSV("ADMIN_API_KEYS"),
		GitHubTokens:            kitenv.LookupCSV("GITHUB_TOKENS"),
		SyncEnabled:             kitenv.Bool("SYNC_ENABLED", defaultSyncEnabled),
		SyncCron:                kitenv.OrDefault("SYNC_CRON", defaultSyncCron),
		FullSyncCron:            kitenv.OrDefault("FULL_SYNC_CRON", defaultFullSyncCron),
		CacheTTLSeconds:         kitenv.Int("CACHE_TTL_SECONDS", defaultCacheTTLSeconds),
		MaxSearchCallsPerMinute: kitenv.Int("MAX_SEARCH_CALLS_PER_MINUTE", defaultMaxSearchCallsPerMinute),
		RateLimitFloor:          kitenv.Int("RATE_LIMIT_FLOOR", defaultRateLimitFloor),
		FeedTargetSize:          kitenv.Int("FEED_TARGET_SIZE", defaultFeedTargetSize),
		StarHistoryEnabled:      kitenv.Bool("STAR_HISTORY_ENABLED", defaultStarHistoryEnabled),
		StarHistoryCacheTTL:     kitenv.Int("STAR_HISTORY_CACHE_TTL_SECONDS", defaultStarHistoryCacheTTL),
		StarHistoryNegativeTTL:  kitenv.Int("STAR_HISTORY_NEGATIVE_TTL_SECONDS", defaultStarHistoryNegativeTTL),
		StarHistoryBuildTimeout: kitenv.Int("STAR_HISTORY_BUILD_TIMEOUT_SECONDS", defaultStarHistoryBuildTimeout),
		StarHistoryWorkers:      kitenv.Int("STAR_HISTORY_WORKER_CONCURRENCY", defaultStarHistoryWorkers),
		StarHistoryQueue:        kitenv.Int("STAR_HISTORY_QUEUE_CAPACITY", defaultStarHistoryQueue),
		StarHistoryMaxPoints:    kitenv.Int("STAR_HISTORY_MAX_POINTS", defaultStarHistoryMaxPoints),
		GCPProjectID:            kitenv.OrDefault("GCP_PROJECT_ID", ""),
		GCPCredentialsJSON:      kitenv.OrDefault("GOOGLE_APPLICATION_CREDENTIALS_JSON", ""),
		BigQueryMaxBytesBilled:  kitenv.Int64("BIGQUERY_MAX_BYTES_BILLED", 0),
		StarHistoryDailyBudget:  kitenv.Int64("STAR_HISTORY_DAILY_MAX_BYTES_BILLED", 0),
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
