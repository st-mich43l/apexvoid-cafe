package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/st-mich43l/apexvoid-cafe/internal/bootstrap"
	"github.com/st-mich43l/apexvoid-cafe/internal/service"
)

func main() {
	log.SetFlags(0)
	config := bootstrap.Config{
		StateDir:         env("BOOTSTRAP_STATE_DIR", "/var/lib/apexvoid/bootstrap"),
		MigrationPath:    os.Getenv("SCHEMA_PATH"),
		MigrationDir:     env("SCHEMA_DIR", "db/migrations"),
		AppVersion:       env("APEXVOID_APP_VERSION", "0.2.0"),
		MigrationVersion: env("APEXVOID_MIGRATION_BUNDLE_VERSION", "0.2.0"),
		DatabaseHost:     env("DATABASE_HOST", "postgres"),
		DatabasePort:     envInt("DATABASE_PORT", 5432),
		DatabaseSSLMode:  env("DATABASE_SSLMODE", "disable"),
		PlatformURL:      env("APEXVOID_URL", "http://backend:6868"),
		ServiceURL:       env("SERVICE_URL", "http://cafe:8090"),
	}
	manager, firstStart, err := bootstrap.New(config)
	if err != nil {
		log.Fatalf("bootstrap initialization failed: %v", err)
	}
	if firstStart {
		log.Printf("ApexVoid Café registration pending")
		log.Printf("Application: cafe")
		log.Printf("Service URL: %s", config.ServiceURL)
		log.Printf("Manifest: %s/.well-known/apexvoid/manifest.json", config.ServiceURL)
		log.Printf("One-time enrollment code: %s", manager.SetupCodeForOperator())
		log.Printf("Keep this code private; it is required by Enterprise registration")
	} else if manager.Phase() == "ACTIVE" {
		log.Printf("ApexVoid Café enrollment state loaded; verifying shared database")
	}

	app := service.New(nil, nil, env("WEB_DIST", "web/dist"))
	app.SetRuntimeProvider(func() (service.Store, service.Authorizer, bool) {
		store, auth, active := manager.Snapshot()
		return store, auth, active
	})
	handler := manager.Handler(app.Handler())
	server := &http.Server{Addr: env("LISTEN_ADDR", ":8090"), Handler: handler, ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go manager.Start(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("ApexVoid Café listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
