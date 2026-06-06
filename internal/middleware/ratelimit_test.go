package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowsBurstThenBlocks(t *testing.T) {
	rl := NewRateLimiter(60, 3) // 3 burst
	ip := "1.2.3.4"
	got := 0
	for i := 0; i < 5; i++ {
		if rl.allow(ip) {
			got++
		}
	}
	if got != 3 {
		t.Fatalf("want 3 allowed in burst, got %d", got)
	}
}

func TestRateLimiterRefills(t *testing.T) {
	rl := NewRateLimiter(60, 1) // 1/sec
	cur := time.Unix(0, 0)
	rl.now = func() time.Time { return cur }
	if !rl.allow("ip") {
		t.Fatal("first should pass")
	}
	if rl.allow("ip") {
		t.Fatal("second should be blocked")
	}
	cur = cur.Add(2 * time.Second) // refill ~2 tokens
	if !rl.allow("ip") {
		t.Fatal("should pass after refill")
	}
}

func TestRateLimiterDisabled(t *testing.T) {
	rl := NewRateLimiter(0, 0)
	for i := 0; i < 100; i++ {
		if !rl.allow("ip") {
			t.Fatal("disabled limiter should always allow")
		}
	}
}

func TestRateLimitMiddleware429(t *testing.T) {
	rl := NewRateLimiter(60, 1)
	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest("POST", "/", nil))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("POST", "/", nil))
	if rec1.Code != 200 || rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("want 200 then 429, got %d then %d", rec1.Code, rec2.Code)
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name string
		set  func(*http.Request)
		want string
	}{
		{"xff", func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1") }, "203.0.113.5"},
		{"xreal", func(r *http.Request) { r.Header.Set("X-Real-IP", "198.51.100.7") }, "198.51.100.7"},
		{"remote", func(r *http.Request) { r.RemoteAddr = "192.0.2.9:54321" }, "192.0.2.9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = "127.0.0.1:1"
			c.set(r)
			if got := ClientIP(r); got != c.want {
				t.Fatalf("want %q got %q", c.want, got)
			}
		})
	}
}

func TestRecoverMiddleware(t *testing.T) {
	h := Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic should yield 500, got %d", rec.Code)
	}
}
