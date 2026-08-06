// Package main 是 starcat-discovery-api 的入口。
//
// 装配逻辑在可导出的 server 包中，便于 starcat-api 聚合部署复用。
package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/starcat-app/starcat-discovery-api/internal/version"
	"github.com/starcat-app/starcat-discovery-api/server"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("[env] no .env file found, using OS environment only")
	} else {
		log.Printf("[env] .env loaded")
	}

	svc, err := server.FromEnv()
	if err != nil {
		log.Fatalf("failed to init discovery server: %v", err)
	}
	defer svc.Close()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Received shutdown signal, closing service...")
		_ = svc.Close()
		os.Exit(0)
	}()

	log.Printf("starcat-discovery-api %s starting on %s", version.Version, svc.Addr())
	log.Fatal(http.ListenAndServe(svc.Addr(), svc.Handler()))
}
