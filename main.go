// Command feegow-mcp runs a stateless MCP server exposing the Feegow
// Clinic ERP, in two profiles: atendimento (customer service) and admin
// (superset). It speaks two transports:
//
//   - streamable-HTTP (default): a shared, multi-tenant server. Each
//     profile is a route (POST /mcp, POST /mcp/admin) and the clinic's
//     Feegow token rides on every request as a bearer header. This server
//     performs no authentication of its own and stores no tokens.
//   - stdio (--stdio): a single local session for one clinic, spawned by
//     an MCP client such as Claude Code or Cursor. There is no HTTP
//     request to carry a header, so the token comes from FEEGOW_TOKEN.
//
// See README.md for both, including the auth contract.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lab019/feegow-mcp/internal/buildinfo"
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

// signalContext returns a context cancelled on SIGINT/SIGTERM, shared by
// both transports: the HTTP server drains on it, the stdio session ends on
// it.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// runStdio serves a single MCP session on stdin/stdout for one profile.
//
// Every diagnostic here goes to stderr (the log package's default), never
// stdout: on this transport stdout carries the JSON-RPC stream itself, and
// a stray line printed there corrupts the protocol for the client.
func runStdio(profileFlag string) error {
	profile, err := mcpserver.ParseProfile(profileFlag)
	if err != nil {
		return err
	}
	token := os.Getenv("FEEGOW_TOKEN")
	if token == "" {
		return errors.New("FEEGOW_TOKEN não definida: no modo --stdio o token da clínica " +
			"vem do ambiente, já que não há requisição HTTP para carregar o header Authorization")
	}

	ctx, stop := signalContext()
	defer stop()

	log.Printf("feegow-mcp %s: sessão stdio, perfil=%s", buildinfo.Version(), profile)
	if err := mcpserver.RunStdio(ctx, profile, token); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// runHTTP serves both profiles over streamable-HTTP until a signal
// arrives, then drains in-flight requests.
func runHTTP() error {
	port := envOr("PORT", defaultPort)
	logLevel := envOr("LOG_LEVEL", defaultLogLevel)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: newMux(),
	}

	ctx, stop := signalContext()
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("feegow-mcp %s listening on :%s (log_level=%s)", buildinfo.Version(), port, logLevel)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server error: %w", err)
	case <-ctx.Done():
	}
	log.Println("shutdown signal received, draining connections...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	} else {
		log.Println("shutdown complete")
	}
	return nil
}

func main() {
	var (
		stdio       = flag.Bool("stdio", false, "serve one MCP session on stdin/stdout instead of HTTP (token from FEEGOW_TOKEN)")
		profile     = flag.String("profile", string(mcpserver.ProfileAtendimento), "toolset for --stdio: \"atendimento\" or \"admin\"")
		showVersion = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Version())
		return
	}

	// --profile só significa algo no transporte de sessão única: no HTTP
	// cada perfil é uma rota, servida simultaneamente. Aceitar a flag em
	// silêncio faria `feegow-mcp --profile=admin` subir um servidor HTTP
	// com AS DUAS rotas, sem nada indicando que a intenção do operador foi
	// descartada.
	profileSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "profile" {
			profileSet = true
		}
	})

	var err error
	if *stdio {
		err = runStdio(*profile)
	} else {
		if profileSet {
			log.Fatalf("feegow-mcp: --profile só vale com --stdio; sem ele os dois perfis " +
				"são servidos como rotas (POST /mcp e POST /mcp/admin)")
		}
		err = runHTTP()
	}
	if err != nil {
		log.Fatalf("feegow-mcp: %v", err)
	}
}
