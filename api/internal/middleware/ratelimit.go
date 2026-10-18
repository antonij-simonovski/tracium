package middleware

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter is a fixed-window, in-memory rate limiter keyed by client IP.
//
// It is intentionally dependency-free and per-process: it protects the
// unauthenticated auth endpoints (register/login) against brute-force and
// enumeration from a single source without requiring Redis. In a multi-replica
// deployment each replica keeps its own window, so the effective global limit is
// limit × replicas; size the limit accordingly, or place a shared limiter at the
// ingress for a hard global bound.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration
	// trustedProxies are the networks whose X-Forwarded-For header we honour.
	// When empty (the default) the header is ignored entirely and the socket
	// peer is always used as the key, so a client cannot spoof its source.
	trustedProxies []*net.IPNet
	// Explicit dns: entries identify proxy services with changing addresses.
	// Kubernetes callers must use a headless Service, which resolves to pod IPs.
	proxyMu         sync.Mutex
	proxyNames      []string
	proxyIPs        []net.IPAddr
	proxyExpires    time.Time
	proxyRefreshing bool
	lookupProxy     func(context.Context, string) ([]net.IPAddr, error)
}

type visitor struct {
	count       int
	windowStart time.Time
}

// NewRateLimiter builds a limiter allowing at most limit requests per window per
// client IP and starts a background sweeper to evict stale entries.
//
// trustedProxies is a list of IPs, CIDRs, or explicit dns:service-name entries of
// reverse proxies permitted to set X-Forwarded-For. Only when the socket peer is
// one of these is the header consulted; otherwise, and by default (empty list),
// the socket peer alone is the rate-limit key. Unparseable entries are skipped
// with no effect on trust.
func NewRateLimiter(limit int, window time.Duration, trustedProxies []string) *RateLimiter {
	rl := &RateLimiter{
		visitors:       make(map[string]*visitor),
		limit:          limit,
		window:         window,
		trustedProxies: parseCIDRs(trustedProxies),
		lookupProxy:    net.DefaultResolver.LookupIPAddr,
	}
	for _, entry := range trustedProxies {
		if name, ok := strings.CutPrefix(strings.TrimSpace(entry), "dns:"); ok && name != "" {
			rl.proxyNames = append(rl.proxyNames, name)
		}
	}
	go rl.cleanupLoop()
	return rl
}

// parseCIDRs turns a list of IP or CIDR strings into networks. A bare IP is
// treated as a single-address network. Invalid entries are dropped: a
// misconfigured proxy address must never widen trust.
func parseCIDRs(entries []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, ipNet, err := net.ParseCIDR(e); err == nil {
			nets = append(nets, ipNet)
			continue
		}
		if ip := net.ParseIP(e); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return nets
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if now.Sub(v.windowStart) > rl.window {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// allow reports whether a request from ip is within the limit, and if not, how
// long until the current window resets.
func (rl *RateLimiter) allow(ip string) (bool, time.Duration) {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[ip]
	if !ok || now.Sub(v.windowStart) > rl.window {
		rl.visitors[ip] = &visitor{count: 1, windowStart: now}
		return true, 0
	}
	if v.count >= rl.limit {
		return false, rl.window - now.Sub(v.windowStart)
	}
	v.count++
	return true, 0
}

// Middleware returns an http middleware enforcing the limit. Requests over the
// limit get a 429 with a Retry-After header.
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := rl.clientIP(r)
			ok, retryAfter := rl.allow(ip)
			if !ok {
				secs := int(retryAfter.Seconds())
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"code":    "RATE_LIMITED",
					"message": "too many requests; please retry later",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extracts the address used as the rate-limit key.
//
// The socket peer (RemoteAddr) is authoritative and is the only value a caller
// cannot forge. X-Forwarded-For is consulted only when the peer is a configured
// trusted proxy; then the right-most XFF entry that is not itself a trusted
// proxy is the real client (the trusted proxy appends the peer it observed, so
// values further left are caller-supplied and must be skipped). Anything that
// does not parse as an IP falls back to the peer, so a client cannot inject an
// arbitrary string as a limiter key to grow the map.
func (rl *RateLimiter) clientIP(r *http.Request) string {
	peer := peerHost(r.RemoteAddr)

	if !rl.isTrustedProxy(peer) {
		return peer
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		ip := net.ParseIP(candidate)
		if ip == nil {
			// A non-IP entry breaks the chain's trust; stop and use the peer.
			return peer
		}
		if rl.isTrustedProxy(candidate) {
			continue
		}
		return candidate
	}
	return peer
}

// peerHost strips the port from a RemoteAddr, tolerating a value without one.
func peerHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// isTrustedProxy reports whether ip (a bare host string) falls in a configured
// trusted-proxy network.
func (rl *RateLimiter) isTrustedProxy(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range rl.trustedProxies {
		if n.Contains(parsed) {
			return true
		}
	}
	return rl.isNamedProxy(parsed)
}

// Cache service discovery briefly, including failures, to bound DNS work on the
// request path. A failed refresh removes old addresses instead of extending trust
// to a pod/container IP that may have been reassigned. One request refreshes at
// a time, outside the lock; others keep using the previous snapshot meanwhile.
func (rl *RateLimiter) isNamedProxy(ip net.IP) bool {
	if len(rl.proxyNames) == 0 {
		return false
	}
	rl.proxyMu.Lock()
	addresses := rl.proxyIPs
	refresh := !rl.proxyRefreshing && !time.Now().Before(rl.proxyExpires)
	if refresh {
		rl.proxyRefreshing = true
	}
	rl.proxyMu.Unlock()
	if refresh {
		addresses = rl.refreshProxies()
	}
	for _, address := range addresses {
		if address.IP.Equal(ip) {
			return true
		}
	}
	return false
}

func (rl *RateLimiter) refreshProxies() []net.IPAddr {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var addresses []net.IPAddr
	for _, name := range rl.proxyNames {
		if resolved, err := rl.lookupProxy(ctx, name); err == nil {
			addresses = append(addresses, resolved...)
		}
	}
	rl.proxyMu.Lock()
	rl.proxyIPs, rl.proxyExpires, rl.proxyRefreshing = addresses, time.Now().Add(5*time.Second), false
	rl.proxyMu.Unlock()
	return addresses
}
