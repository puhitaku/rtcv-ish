package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/puhitaku/rtcv-ish/internal/session"
)

const heartbeatInterval = 15 * time.Second

// serveEvents streams Server-Sent Events (GET /api/events): a status event
// first, then every session event, with a comment line as heartbeat.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	events, unsubscribe := s.sess.Subscribe()
	defer unsubscribe()
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(typ string, data any) error {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, b); err != nil {
			return err
		}
		return rc.Flush()
	}
	if err := send(session.EventStatus, s.sess.Status()); err != nil {
		return
	}
	hb := time.NewTicker(heartbeatInterval)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-hb.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := send(ev.Type, ev.Data); err != nil {
				s.log.Debug("event stream closed", "err", err)
				return
			}
		}
	}
}
