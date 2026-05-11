package integration

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func TestHealthz(t *testing.T) {
	srv := httpserver.New(
		config.Config{Port: "8080", AppEnv: "test"},
		slog.New(slog.NewTextHandler(os.Stdout, nil)),
	)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf(`want status="ok", got %q`, body["status"])
	}
	if body["service"] != "control-plane" {
		t.Fatalf(`want service="control-plane", got %q`, body["service"])
	}
}
