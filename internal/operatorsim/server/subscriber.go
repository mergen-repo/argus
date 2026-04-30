package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) subscriberHandler(w http.ResponseWriter, r *http.Request) {
	op := chi.URLParam(r, "operator")
	imsi := chi.URLParam(r, "imsi")
	resp := struct {
		IMSI     string `json:"imsi"`
		Operator string `json:"operator"`
		Plan     string `json:"plan"`
		Status   string `json:"status"`
	}{imsi, op, s.cfg.Stubs.SubscriberPlan, s.cfg.Stubs.SubscriberStatus}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}
