package main

import (
	"log/slog"
	"net/http"
	"os"

	httpsrv "github.com/nexis-eco/nexis/services/validator/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	logger.Info("validator starting", "port", port)
	if err := http.ListenAndServe(":"+port, httpsrv.New("validator")); err != nil {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}
