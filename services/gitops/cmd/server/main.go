package main

import (
	"log/slog"
	"net/http"
	"os"

	httpsrv "github.com/nexis-eco/nexis/services/gitops/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}
	logger.Info("gitops starting", "port", port)
	if err := http.ListenAndServe(":"+port, httpsrv.New("gitops")); err != nil {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}
