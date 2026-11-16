package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// hubEvent is a compact change notification. The UI uses it to refresh visible
// data; the full resource is fetched through the regular scoped API.
type hubEvent struct {
	Type       string    `json:"type"`
	InstanceID string    `json:"instance_id,omitempty"`
	AppID      int64     `json:"app_id,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Status     string    `json:"status,omitempty"`
	Trigger    string    `json:"trigger,omitempty"`
	At         time.Time `json:"at"`
}

type eventHub struct {
	mu   sync.Mutex
	subs map[chan hubEvent]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: make(map[chan hubEvent]struct{})}
}

func (h *eventHub) subscribe() (<-chan hubEvent, func()) {
	ch := make(chan hubEvent, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *eventHub) publish(ev hubEvent) {
	if h == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
			// A slow browser must never block agents reporting runs.
		}
	}
}

func (s *Server) publishEvent(ev hubEvent) {
	if s.events != nil {
		s.events.publish(ev)
	}
}

// handleEvents streams server-sent change events to the UI. EventSource cannot
// send Basic credentials, so the browser reads the stream with fetch().
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "retry: 3000\n\n: connected\n\n")
	flusher.Flush()

	ch, unsubscribe := s.events.subscribe()
	defer unsubscribe()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
