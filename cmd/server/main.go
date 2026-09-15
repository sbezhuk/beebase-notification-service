package main

import (
	"context"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/sbezhuk/beebase-common/authmw"
	"github.com/sbezhuk/beebase-common/logger"
	"github.com/sbezhuk/beebase-common/server"
	"github.com/sbezhuk/beebase-common/sessionstore"
	appnotification "github.com/sbezhuk/beebase-notification-service/internal/application/notification"
	"github.com/sbezhuk/beebase-notification-service/internal/config"
	"github.com/sbezhuk/beebase-notification-service/internal/domain/reminder"
	"github.com/sbezhuk/beebase-notification-service/internal/platform/entityclient"
	"github.com/sbezhuk/beebase-notification-service/internal/platform/firebase"
	"github.com/sbezhuk/beebase-notification-service/internal/platform/postgres"
	repopostgres "github.com/sbezhuk/beebase-notification-service/internal/repository/postgres"
	transporthttp "github.com/sbezhuk/beebase-notification-service/internal/transport/http"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
func run() error {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log := logger.New(cfg.Env, cfg.LogLevel)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cc, cancel := context.WithTimeout(ctx, cfg.DatabaseConnectTimeout)
	db, err := postgres.New(cc, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()
	log.Info("connected to database")
	rc, cancel := context.WithTimeout(ctx, cfg.RedisConnectTimeout)
	redis, err := sessionstore.NewRedisClient(rc, cfg.RedisAddr)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	defer redis.Close()
	sessions := sessionstore.NewStore(redis)
	verifier, err := authmw.NewVerifierFromJWKSURL(ctx, cfg.AuthJWKSURL, sessions)
	if err != nil {
		return fmt.Errorf("build JWKS verifier: %w", err)
	}
	sender, err := firebase.NewSender(ctx, cfg.FirebaseProjectID, cfg.FirebaseServiceAccountJSON)
	if err != nil {
		return fmt.Errorf("initialize Firebase: %w", err)
	}
	repo := repopostgres.NewPushDeviceRepository(db)
	svc := appnotification.NewService(repo, sender)
	resolver := entityclient.New(map[reminder.EntityType]string{reminder.EntityApiary: cfg.ApiaryServiceURL, reminder.EntityHive: cfg.HiveServiceURL, reminder.EntityInspection: cfg.InspectionServiceURL, reminder.EntityHarvest: cfg.HarvestServiceURL}, cfg.InternalServiceToken)
	reminderRepo := repopostgres.NewReminderRepository(db)
	reminderSvc := appnotification.NewReminderService(reminderRepo, repo, sender, resolver)
	go func() {
		ticker := time.NewTicker(cfg.ReminderWorkerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := reminderSvc.ProcessDue(ctx, time.Now().UTC(), 50); err != nil {
					log.Error("reminder worker failed", "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	router := transporthttp.NewRouter(log, db, transporthttp.NewHandler(svc, log, reminderSvc), verifier, cfg.InternalServiceToken)
	srv := server.New(server.Config{Addr: ":" + cfg.HTTPPort, Handler: router, ReadTimeout: cfg.HTTPReadTimeout, WriteTimeout: cfg.HTTPWriteTimeout, IdleTimeout: cfg.HTTPIdleTimeout})
	errCh := make(chan error, 1)
	go func() { log.Info("starting http server", "port", cfg.HTTPPort, "env", cfg.Env); errCh <- srv.Run() }()
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("run server: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}
	sc, cancel := context.WithTimeout(context.Background(), cfg.HTTPShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sc); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("server stopped cleanly")
	return nil
}
