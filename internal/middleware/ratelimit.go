package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter is a per-client-IP token-bucket limiter. It's intended for the
// job-submission and webhook endpoints — cheap protection against a runaway or
// hostile client, not a full traffic shaper.
type RateLimiter struct {
	rpm   float64 // tokens added per minute
	burst float64 // bucket capacity
	mu    sync.Mutex
	buck  map[string]*bucket
	now   func() time.Time // injectable for tests
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter allows rpm requests/minute per IP with the given burst.
// A non-positive rpm disables limiting.
func NewRateLimiter(rpm, burst int) *RateLimiter {
	return &RateLimiter{
		rpm:   float64(rpm),
		burst: float64(burst),
		buck:  make(map[string]*bucket),
		now:   time.Now,
	}
}

func (rl *RateLimiter) allow(ip string) bool {
	if rl.rpm <= 0 {
		return true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	b := rl.buck[ip]
	if b == nil {
		rl.buck[ip] = &bucket{tokens: rl.burst - 1, last: now}
		return true
	}
	// Refill since last seen.
	b.tokens += (rl.rpm / 60.0) * now.Sub(b.last).Seconds()
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Middleware wraps a handler, returning 429 when a client exceeds its rate.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(ClientIP(r)) {
			w.Header().Set("Retry-After", "5")
			http.Error(w, `{"ok":false,"error":"rate limited"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP extracts the best-effort client IP, honouring X-Forwarded-For and
// X-Real-IP (set by a trusted reverse proxy) before falling back to RemoteAddr.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return strings.TrimSpace(xr)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
