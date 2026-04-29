// Command server starts the pack-calculator HTTP service.
//
// Configuration:
//
//	PORT      listening port (default 8080)
//	DB_PATH   sqlite file path (default /data/pack-calculator.db)
//	SEED      comma-separated default pack sizes used only on first boot
//	          (default "250,500,1000,2000,5000")
//
// The server binds to 0.0.0.0 so the published port reaches the listener
// when the binary runs inside a container.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pack-calculator/internal/server"
	"pack-calculator/internal/store"
	webassets "pack-calculator/internal/web"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)

	port := envOr("PORT", "8080")
	dbPath := envOr("DB_PATH", "/data/pack-calculator.db")
	seed := parseSeed(envOr("SEED", "250,500,1000,2000,5000"))

	// SQLite creates the file but not its parent directory — pre-create
	// it to avoid silent startup failures on a custom DB_PATH.
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Fatalf("create db directory %q: %v", dir, err)
		}
	}

	st, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if err := st.SeedIfEmpty(context.Background(), seed); err != nil {
		logger.Fatalf("seed defaults: %v", err)
	}

	srv := server.New(st, webassets.FS(), logger)
	httpServer := &http.Server{
		Addr:              "0.0.0.0:" + port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("pack-calculator listening on %s (db=%s)", httpServer.Addr, dbPath)
		errCh <- httpServer.ListenAndServe()
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("listen: %v", err)
		}
	case sig := <-stopCh:
		logger.Printf("received %s, shutting down", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Printf("shutdown: %v", err)
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseSeed(raw string) []int {
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "ignoring invalid seed value %q\n", p)
			continue
		}
		out = append(out, n)
	}
	return out
}
