package integration

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	httpserver "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http"
)

func TestLLMDiag_Ollama(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3.1:8b"}]}`))
	}))
	defer ollama.Close()

	srv := httpserver.New(config.Config{
		Port: "0", AppEnv: "test",
		LLMProvider: "ollama", OllamaBaseURL: ollama.URL, OllamaModelGen: "llama3.1:8b",
	}, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	req := httptest.NewRequest(http.MethodGet, "/v1/_diag/llm", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var info map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info["provider"] != "ollama" {
		t.Fatalf("want provider=ollama, got %v", info["provider"])
	}
	models, _ := info["models"].([]any)
	if len(models) != 1 || !strings.Contains(models[0].(string), "llama3.1") {
		t.Fatalf("want llama3.1 model, got %v", info["models"])
	}
}
