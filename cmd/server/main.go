package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/yashk/streamvault/internal/api"
	"github.com/yashk/streamvault/internal/config"
	"github.com/yashk/streamvault/internal/leaderboard"
	"github.com/yashk/streamvault/internal/processor"
	"github.com/yashk/streamvault/internal/store"
)

func main() {
	_ = godotenv.Load()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("loading config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("connecting to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.Close()
	log.Info("database connected")

	lb := leaderboard.New(db, log)
	lb.Start(ctx, 60*time.Second)

	pipeline := processor.New(db, lb, cfg.WorkerCount, cfg.ChannelCapacity, log)
	pipeline.Start(ctx)

	router := api.NewRouter(db, pipeline, lb, log)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info("server listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown", slog.String("error", err.Error()))
	}
	if err := pipeline.Shutdown(shutdownCtx); err != nil {
		log.Error("pipeline shutdown", slog.String("error", err.Error()))
	}
	lb.Shutdown()

	log.Info("shutdown complete")
}
