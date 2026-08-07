// Command feegow-mcp runs a stateless MCP server exposing the Feegow
// Clinic ERP over streamable-HTTP, in two profiles: atendimento (POST
// /mcp) and admin (POST /mcp/admin). See README.md for the auth contract:
// this server performs no authentication of its own and stores no
// tokens — callers forward the clinic's Feegow token on every request.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lab019/feegow-mcp/internal/mcpserver"
)

const (
	defaultPort     = "8087"
	defaultLogLevel = "INFO"
)

// newMux builds the HTTP routing for the service: health check plus the
// two MCP profile mounts. Split out from main so it can be exercised by
// tests without touching process lifecycle (signals, graceful shutdown).
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.Handle("/mcp", mcpserver.Handler())
	mux.Handle("/mcp/admin", mcpserver.AdminHandler())
	return mux
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	port := envOr("PORT", defaultPort)
	logLevel := envOr("LOG_LEVEL", defaultLogLevel)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: newMux(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("feegow-mcp listening on :%s (log_level=%s)", port, logLevel)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutdown signal received, draining connections...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	} else {
		log.Println("shutdown complete")
	}
}
