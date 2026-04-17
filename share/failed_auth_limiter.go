package share

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultFailedAuthLimit           = 10
	defaultFailedAuthWindow          = time.Minute
	defaultFailedAuthCleanupInterval = time.Minute
)

type failedAuthLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]failedAuthEntry
}

type failedAuthEntry struct {
	count     int
	expiresAt time.Time
}

func newFailedAuthLimiter(limit int, window time.Duration) *failedAuthLimiter {
	if limit <= 0 {
		limit = defaultFailedAuthLimit
	}
	if window <= 0 {
		window = defaultFailedAuthWindow
	}
	return &failedAuthLimiter{
		limit:   limit,
		window:  window,
		entries: make(map[string]failedAuthEntry),
	}
}

func (l *failedAuthLimiter) Allow(ip string, now time.Time) bool {
	if l == nil || ip == "" {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[ip]
	if !ok {
		return true
	}
	if !now.Before(entry.expiresAt) {
		delete(l.entries, ip)
		return true
	}
	return entry.count < l.limit
}

func (l *failedAuthLimiter) RecordFailure(ip string, now time.Time) {
	if l == nil || ip == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[ip]
	if !ok || !now.Before(entry.expiresAt) {
		entry = failedAuthEntry{expiresAt: now.Add(l.window)}
	}
	entry.count++
	l.entries[ip] = entry
}

func (l *failedAuthLimiter) Cleanup(now time.Time) {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	for ip, entry := range l.entries {
		if !now.Before(entry.expiresAt) {
			delete(l.entries, ip)
		}
	}
}

func failedAuthRateLimit(limiter *failedAuthLimiter, next http.Handler) http.Handler {
	if limiter == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isProtectedSharePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		ip := remoteIP(r)
		if !limiter.Allow(ip, time.Now().UTC()) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed authentication attempts")
			return
		}

		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if recorder.status == http.StatusUnauthorized {
			limiter.RecordFailure(ip, time.Now().UTC())
		}
	})
}

func isProtectedSharePath(path string) bool {
	return strings.HasPrefix(path, "/s/") || strings.HasPrefix(path, "/r/")
}

func remoteIP(r *http.Request) string {
	if r == nil {
		return ""
	}

	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	if remoteAddr == "" {
		return ""
	}

	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(p)
}
