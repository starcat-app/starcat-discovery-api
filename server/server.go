// Package server 导出 discovery-api 的可装配 HTTP 服务。
//
// 单仓部署走 cmd/server；聚合部署（starcat-api）import 本包并挂到网关。
// 业务实现仍在 internal/，本包负责配置装配、路由、sync scheduler 与 star-history 生命周期。
package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/awesome"
	"github.com/starcat-app/starcat-discovery-api/internal/config"
	"github.com/starcat-app/starcat-discovery-api/internal/github"
	"github.com/starcat-app/starcat-discovery-api/internal/handler"
	"github.com/starcat-app/starcat-discovery-api/internal/ingest"
	"github.com/starcat-app/starcat-discovery-api/internal/middleware"
	"github.com/starcat-app/starcat-discovery-api/internal/scheduler"
	"github.com/starcat-app/starcat-discovery-api/internal/starhistory"
	"github.com/starcat-app/starcat-discovery-api/internal/store"
	"github.com/starcat-app/starcat-discovery-api/internal/tokenpool"
	"github.com/starcat-app/starcat-discovery-api/internal/version"
)

const defaultPort = "5006"

// Service 是已装配的 discovery HTTP 服务。
type Service struct {
	cfg                config.Config
	handler            http.Handler
	store              *store.SQLiteStore
	scheduler          *scheduler.Scheduler
	starHistoryService *starhistory.Service

	closeOnce sync.Once
}

// Name 返回聚合网关识别用的稳定服务名。
func Name() string { return "discovery" }

// DefaultPort 返回单仓默认监听端口。
func DefaultPort() string { return defaultPort }

// FromEnv 从环境变量装配服务（缺失必填项时返回 error）。
func FromEnv() (*Service, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return New(cfg)
}

// New 按配置装配服务；若 SyncEnabled 则启动 cron。
func New(cfg config.Config) (*Service, error) {
	if cfg.Port == "" {
		cfg.Port = defaultPort
	}

	apiAuth := middleware.NewBearerAuth("api", cfg.APIKeys)
	adminAuth := middleware.NewBearerAuth("admin", cfg.AdminAPIKeys)

	sqliteStore, err := store.NewSQLiteStore(context.Background(), cfg.StoreFile)
	if err != nil {
		return nil, fmt.Errorf("initialize SQLite: %w", err)
	}

	pool := tokenpool.New(cfg.GitHubTokens)
	githubClient := github.NewClient(pool, cfg.RateLimitFloor)
	ingestService := ingest.NewService(sqliteStore, githubClient, cfg.FeedTargetSize)
	discoveryHandler := handler.NewDiscoveryHandler(sqliteStore)
	bulkCache := handler.NewBulkCache(time.Duration(cfg.CacheTTLSeconds) * time.Second)
	awesomeService := awesome.NewService(sqliteStore, githubClient)
	awesomeHandler := handler.NewAwesomeHandler(awesomeService)
	awesomeAdminHandler := handler.NewAwesomeAdminHandler(awesomeService)

	var starHistoryService *starhistory.Service
	if cfg.StarHistoryEnabled {
		runner, runnerErr := starhistory.NewAuthorizedBigQueryRESTRunner(
			context.Background(),
			[]byte(cfg.GCPCredentialsJSON),
		)
		if runnerErr != nil {
			_ = sqliteStore.Close()
			return nil, fmt.Errorf("initialize BigQuery credentials: %w", runnerErr)
		}
		provider, providerErr := starhistory.NewGHArchiveBigQueryProvider(cfg.GCPProjectID, runner)
		if providerErr != nil {
			_ = sqliteStore.Close()
			return nil, fmt.Errorf("initialize star history provider: %w", providerErr)
		}
		starHistoryService, err = starhistory.NewService(
			sqliteStore,
			provider,
			starhistory.ServiceConfig{
				CacheTTL:           time.Duration(cfg.StarHistoryCacheTTL) * time.Second,
				NegativeCacheTTL:   time.Duration(cfg.StarHistoryNegativeTTL) * time.Second,
				BuildTimeout:       time.Duration(cfg.StarHistoryBuildTimeout) * time.Second,
				WorkerConcurrency:  cfg.StarHistoryWorkers,
				QueueCapacity:      cfg.StarHistoryQueue,
				MaximumPoints:      cfg.StarHistoryMaxPoints,
				MaximumBytesBilled: cfg.BigQueryMaxBytesBilled,
				DailyMaximumBytes:  cfg.StarHistoryDailyBudget,
			},
		)
		if err != nil {
			_ = sqliteStore.Close()
			return nil, fmt.Errorf("initialize star history service: %w", err)
		}
	}

	starHistoryHandler := handler.NewStarHistoryHandler(
		starHistoryService,
		githubClient,
		cfg.StarHistoryEnabled,
	)

	var sch *scheduler.Scheduler
	if cfg.SyncEnabled {
		sch = scheduler.New(ingestService, cfg.SyncCron, cfg.FullSyncCron, bulkCache, awesomeService)
		sch.Start()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthzHandler)
	mux.Handle("GET /api/v1/ping", apiAuth.Wrap(handler.HandlePingV1(version.Service, version.Version)))
	mux.Handle("GET /api/v1/discovery/feed", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleFeed)))
	mux.Handle("GET /api/v1/discovery/categories/most-popular", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleMostPopular)))
	mux.Handle("GET /api/v1/discovery/categories/new-releases", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleNewReleases)))
	mux.Handle("GET /api/v1/discovery/summary", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleSummary)))
	mux.Handle("GET /api/v1/discovery/bulk", apiAuth.Wrap(discoveryHandler.HandleBulk(bulkCache)))
	mux.Handle("GET /api/v1/discovery/languages", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleLanguages)))
	mux.Handle("GET /api/v1/discovery/topics", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleTopics)))
	mux.Handle("GET /api/v1/discovery/platforms", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandlePlatforms)))
	mux.Handle("GET /api/v1/discovery/awesome/sources", apiAuth.Wrap(http.HandlerFunc(awesomeHandler.HandleSources)))
	mux.Handle("GET /api/v1/discovery/awesome/sources/{source_id}/entries", apiAuth.Wrap(http.HandlerFunc(awesomeHandler.HandleEntries)))
	mux.Handle(
		"GET /api/v1/repos/{owner}/{repo}/star-history",
		apiAuth.Wrap(http.HandlerFunc(starHistoryHandler.HandleStarHistory)),
	)
	mux.Handle("GET /internal/discovery/trending-candidates", adminAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleTrending)))
	mux.Handle("POST /internal/sync/discovery", adminAuth.Wrap(handler.HandleAdminSyncDiscovery(ingestService, bulkCache)))
	mux.Handle("GET /internal/discovery/awesome/sources", adminAuth.Wrap(http.HandlerFunc(awesomeAdminHandler.HandleList)))
	mux.Handle("POST /internal/discovery/awesome/sources", adminAuth.Wrap(http.HandlerFunc(awesomeAdminHandler.HandleCreate)))
	mux.Handle("PATCH /internal/discovery/awesome/sources/{source_id}", adminAuth.Wrap(http.HandlerFunc(awesomeAdminHandler.HandleUpdate)))
	mux.Handle("POST /internal/discovery/awesome/sources/{source_id}/sync", adminAuth.Wrap(http.HandlerFunc(awesomeAdminHandler.HandleSync)))
	mux.Handle("POST /internal/discovery/awesome/sources/{source_id}/publish", adminAuth.Wrap(http.HandlerFunc(awesomeAdminHandler.HandlePublish)))
	mux.Handle("POST /internal/discovery/awesome/sources/{source_id}/archive", adminAuth.Wrap(http.HandlerFunc(awesomeAdminHandler.HandleArchive)))
	mux.Handle("GET /internal/discovery/awesome/sources/{source_id}/sync-runs", adminAuth.Wrap(http.HandlerFunc(awesomeAdminHandler.HandleSyncRuns)))

	log.Printf("starcat-discovery-api %s endpoints ready", version.Version)

	return &Service{
		cfg:                cfg,
		handler:            middleware.CORS(mux),
		store:              sqliteStore,
		scheduler:          sch,
		starHistoryService: starHistoryService,
	}, nil
}

// Handler 返回已包 CORS 的根 handler。
func (s *Service) Handler() http.Handler { return s.handler }

// Addr 返回建议监听地址。
func (s *Service) Addr() string { return ":" + s.cfg.Port }

// Close 停止 scheduler / star-history 并关闭 SQLite。
func (s *Service) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		if s.scheduler != nil {
			s.scheduler.Stop()
		}
		if s.starHistoryService != nil {
			s.starHistoryService.Close()
		}
		if s.store != nil {
			closeErr = s.store.Close()
		}
	})
	return closeErr
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
