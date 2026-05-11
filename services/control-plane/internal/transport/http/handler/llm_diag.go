package handler

import (
	"encoding/json"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

func LLMDiag(p domain.LLMProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := p.Info(r.Context())
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"provider": p.Name(), "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(info)
	}
}
