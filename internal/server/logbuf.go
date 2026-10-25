package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// LogEntry is one event shown in the panel's log view.
type LogEntry struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Attrs   map[string]string `json:"attrs"`
}

// logRing keeps the most recent events (Info and above) in memory for the panel.
type logRing struct {
	mu      sync.Mutex
	entries []LogEntry
	next    int
	full    bool
}

const logRingSize = 1000

func newLogRing() *logRing { return &logRing{entries: make([]LogEntry, logRingSize)} }

func (r *logRing) add(e LogEntry) {
	r.mu.Lock()
	r.entries[r.next] = e
	r.next = (r.next + 1) % logRingSize
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// list returns up to limit entries, newest first.
func (r *logRing) list(limit int) []LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.next
	if r.full {
		n = logRingSize
	}
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]LogEntry, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (r.next - 1 - i + logRingSize) % logRingSize
		out = append(out, r.entries[idx])
	}
	return out
}

// ringHandler tees records at Info and above into the ring while passing everything to the inner handler.
type ringHandler struct {
	inner slog.Handler
	ring  *logRing
	attrs []slog.Attr
}

func (h *ringHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= slog.LevelInfo || h.inner.Enabled(ctx, l)
}

func (h *ringHandler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= slog.LevelInfo {
		e := LogEntry{Time: rec.Time.UTC(), Level: rec.Level.String(), Message: rec.Message, Attrs: map[string]string{}}
		for _, a := range h.attrs {
			e.Attrs[a.Key] = fmt.Sprint(a.Value.Any())
		}
		rec.Attrs(func(a slog.Attr) bool {
			e.Attrs[a.Key] = fmt.Sprint(a.Value.Any())
			return true
		})
		h.ring.add(e)
	}
	if h.inner.Enabled(ctx, rec.Level) {
		return h.inner.Handle(ctx, rec)
	}
	return nil
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ringHandler{inner: h.inner.WithAttrs(attrs), ring: h.ring, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{inner: h.inner.WithGroup(name), ring: h.ring, attrs: h.attrs}
}

func (a *App) hLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 300
	}
	writeJSON(w, 200, a.logs.list(limit))
}
