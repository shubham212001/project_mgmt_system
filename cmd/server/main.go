package main

import (
	"context"
	"log"
	"time"

	"project-management-platform/internal/api"
	"project-management-platform/internal/platform/cache"
	"project-management-platform/internal/platform/config"
	"project-management-platform/internal/platform/database"
	"project-management-platform/internal/platform/migrations"
	"project-management-platform/internal/realtime"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()

	if err := migrations.Run(ctx, db); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	rdb := cache.NewClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err := cache.Ping(ctx, rdb); err != nil {
		log.Fatalf("redis connection failed: %v", err)
	}

	hub := realtime.NewHub(rdb, cfg.WebsocketPing, cfg.WebsocketPongWait)
	router := api.NewRouter(db, hub)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
