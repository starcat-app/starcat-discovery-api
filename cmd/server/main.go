// Package main 是 starcat-discovery-api 的入口。
//
// 本服务承载 Starcat 探索入口中的发现、热门、新发布和未来新版趋势能力。
// 首期保持独立服务边界，避免改动已有 starcat-trending-api 调用链。
package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/dong4j/starcat-discovery-api/internal/config"
	"github.com/dong4j/starcat-discovery-api/internal/handler"
	"github.com/dong4j/starcat-discovery-api/internal/middleware"
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthzHandler)
	mux.Handle("GET /api/v1/ping", apiAuth.Wrap(handler.HandlePingV1(version.Service)))
	mux.Handle("POST /internal/sync/discovery", adminAuth.Wrap(handler.HandleAdminSyncDiscovery(cfg.StoreFile)))

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
	log.Printf("  POST /internal/sync/discovery  - Manual discovery sync (admin auth required)")
	log.Printf("  GET  /healthz                  - Health check (public)")
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
