package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"apiservices/invoice-ocr/internal/invoice/api"
	"apiservices/invoice-ocr/internal/invoice/auth"
	"apiservices/invoice-ocr/internal/invoice/ocr"
)

func main() {
	logger := log.New(os.Stdout, "[invoice] ", log.LstdFlags)

	port := envString("PORT", "30008")
	apiKey := envString("INVOICE_API_KEY", "dev-invoice-key")
	maxUploadMB := envInt("INVOICE_MAX_UPLOAD_MB", 10)

	if apiKey == "dev-invoice-key" {
		logger.Println("INVOICE_API_KEY not set, using default development key")
	}

	service := ocr.NewService(int64(maxUploadMB) << 20)
	handler := api.NewHandler(service)

	mux := http.NewServeMux()
	mux.Handle("/v1/invoice/", auth.Middleware(apiKey)(handler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Printf("service listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("server failed: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Printf("shutdown error: %v", err)
	}
}

func envString(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
