package server

import "net/http"

// serveEvents streams Server-Sent Events (GET /api/events).
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	writeError(w, errNotImplemented)
}
