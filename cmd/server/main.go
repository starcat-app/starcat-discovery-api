// Package main 是 starcat-discovery-api 的入口。
//
// 本服务承载 Starcat 探索入口中的发现、热门、新发布；新版趋势只保留诊断接口。
// 首期保持独立服务边界，避免改动已有 starcat-trending-api 调用链。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/dong4j/starcat-discovery-api/internal/config"
	"github.com/dong4j/starcat-discovery-api/internal/github"
	"github.com/dong4j/starcat-discovery-api/internal/handler"
	"github.com/dong4j/starcat-discovery-api/internal/ingest"
	"github.com/dong4j/starcat-discovery-api/internal/middleware"
	"github.com/dong4j/starcat-discovery-api/internal/scheduler"
	"github.com/dong4j/starcat-discovery-api/internal/store"
	"github.com/dong4j/starcat-discovery-api/internal/tokenpool"
	"github.com/dong4j/starcat-discovery-api/internal/version"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("[env] no .env file found, using OS environment only")
	} else {
		log.Printf("[env] .env loaded")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	apiAuth := middleware.NewBearerAuth("api", cfg.APIKeys)
	adminAuth := middleware.NewBearerAuth("admin", cfg.AdminAPIKeys)

	sqliteStore, err := store.NewSQLiteStore(context.Background(), cfg.StoreFile)
	if err != nil {
		log.Fatalf("Failed to initialize SQLite: %v", err)
	}
	defer sqliteStore.Close()

	pool := tokenpool.New(cfg.GitHubTokens)
	githubClient := github.NewClient(pool, cfg.RateLimitFloor)
	ingestService := ingest.NewService(sqliteStore, githubClient, cfg.FeedTargetSize)
	discoveryHandler := handler.NewDiscoveryHandler(sqliteStore)
	bulkCache := handler.NewBulkCache(time.Duration(cfg.CacheTTLSeconds) * time.Second)
	if cfg.SyncEnabled {
		sch := scheduler.New(ingestService, cfg.SyncCron, cfg.FullSyncCron, bulkCache)
		sch.Start()
		defer sch.Stop()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthzHandler)
	mux.Handle("GET /api/v1/ping", apiAuth.Wrap(handler.HandlePingV1(version.Service)))
	mux.Handle("GET /api/v1/discovery/feed", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleFeed)))
	mux.Handle("GET /api/v1/discovery/categories/most-popular", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleMostPopular)))
	mux.Handle("GET /api/v1/discovery/categories/new-releases", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleNewReleases)))
	mux.Handle("GET /api/v1/discovery/summary", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleSummary)))
	mux.Handle("GET /api/v1/discovery/bulk", apiAuth.Wrap(discoveryHandler.HandleBulk(bulkCache)))
	mux.Handle("GET /api/v1/discovery/languages", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleLanguages)))
	mux.Handle("GET /api/v1/discovery/topics", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleTopics)))
	mux.Handle("GET /api/v1/discovery/platforms", apiAuth.Wrap(http.HandlerFunc(discoveryHandler.HandlePlatforms)))
	mux.Handle("GET /internal/discovery/trending-candidates", adminAuth.Wrap(http.HandlerFunc(discoveryHandler.HandleTrending)))
	mux.Handle("POST /internal/sync/discovery", adminAuth.Wrap(handler.HandleAdminSyncDiscovery(ingestService, bulkCache)))

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Received shutdown signal, closing service...")
		os.Exit(0)
	}()

	log.Printf("starcat-discovery-api %s starting on port %s", version.Version, cfg.Port)
	log.Printf("Endpoints:")
	log.Printf("  GET  /api/v1/ping              - Connectivity probe for Starcat client (api auth required)")
	log.Printf("  GET  /api/v1/discovery/feed    - Discovery feed (api auth required)")
	log.Printf("  GET  /api/v1/discovery/categories/most-popular - Popular ranking (api auth required)")
	log.Printf("  GET  /api/v1/discovery/categories/new-releases - New releases ranking (api auth required)")
	log.Printf("  GET  /api/v1/discovery/summary   - Sidebar totals and facet counts (api auth required)")
	log.Printf("  GET  /api/v1/discovery/bulk      - Full local-first catalog snapshot (api auth required)")
	log.Printf("  GET  /api/v1/discovery/languages - Discovery languages metadata (api auth required)")
	log.Printf("  GET  /api/v1/discovery/topics    - Discovery topic metadata (api auth required)")
	log.Printf("  GET  /api/v1/discovery/platforms - Discovery platform metadata (api auth required)")
	log.Printf("  GET  /internal/discovery/trending-candidates - New trending candidate diagnostics (admin auth required)")
	log.Printf("  POST /internal/sync/discovery  - Manual discovery sync (admin auth required)")
	log.Printf("  GET  /healthz                  - Health check (public)")
	handler := middleware.CORS(mux)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, handler))
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
