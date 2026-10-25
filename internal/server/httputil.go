package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"chameleontak/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail maps common errors to HTTP statuses.
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "Nie znaleziono.")
	default:
		var ve validationError
		if errors.As(err, &ve) {
			httpError(w, http.StatusBadRequest, string(ve))
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
	}
}

type validationError string

func (e validationError) Error() string { return string(e) }

func invalid(msg string) error { return validationError(msg) }

var reHostLabel = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// remarshal copies a generic JSON object onto a typed value (used for partial updates).
func remarshal(in any, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func remarshalBytes(raw []byte, out any) error { return json.Unmarshal(raw, out) }

func withValue(ctx context.Context, key, val any) context.Context {
	return context.WithValue(ctx, key, val)
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return invalid("Niepoprawne dane: " + err.Error())
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, invalid("Niepoprawny identyfikator.")
	}
	return id, nil
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// randomCode returns an easy-to-type code without ambiguous characters (0/O, 1/I).
func randomCode(n int) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	for i := range b {
		k, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[k.Int64()]
	}
	return string(b)
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	const hex = "0123456789abcdef"
	out := make([]byte, 64)
	for i, v := range b {
		out[i*2], out[i*2+1] = hex[v>>4], hex[v&15]
	}
	return string(out)
}

// limiter throttles failed authentication attempts per IP.
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{hits: map[string][]time.Time{}} }

const (
	limitWindow = 5 * time.Minute
	limitMax    = 15
)

func (l *limiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip)) >= limitMax
}

func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[ip] = append(l.recent(ip), time.Now())
}

func (l *limiter) recent(ip string) []time.Time {
	cut := time.Now().Add(-limitWindow)
	hs := l.hits[ip]
	i := 0
	for i < len(hs) && hs[i].Before(cut) {
		i++
	}
	hs = hs[i:]
	if len(hs) == 0 {
		delete(l.hits, ip)
	} else {
		l.hits[ip] = hs
	}
	return hs
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
