package server

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// cdrHandler always drains the request body (keep-alive safety) regardless
// of Stubs.CDREcho. CDREcho is retained in config for future use to control
// response-metadata shape; it does not gate body draining.
func (s *Server) cdrHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	resp := struct {
		Received   bool      `json:"received"`
		IngestedAt time.Time `json:"ingested_at"`
	}{true, time.Now().UTC()}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}
