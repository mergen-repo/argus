package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	op := chi.URLParam(r, "operator")
	resp := struct {
		Operator  string    `json:"operator"`
		Status    string    `json:"status"`
		Timestamp time.Time `json:"timestamp"`
	}{op, "ok", time.Now().UTC()}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}
