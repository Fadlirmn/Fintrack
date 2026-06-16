package middleware

import (
	"net"
	"net/http"
	"strings"
)

// APIKeyMiddleware protects endpoints by requiring a matching X-API-Key header.
func APIKeyMiddleware(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get("X-API-Key") != key {
				jsonError(w, "Unauthorized: invalid API key", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IPWhitelistMiddleware blocks requests from IPs not in the allowed list.
// If allowedIPs is empty, all IPs are allowed.
func IPWhitelistMiddleware(allowedIPs []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(allowedIPs) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				remoteIP = r.RemoteAddr
			}
			// Also check X-Forwarded-For for proxied setups
			if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
				remoteIP = strings.TrimSpace(strings.Split(fwd, ",")[0])
			}

			for _, allowed := range allowedIPs {
				if allowed == remoteIP {
					next.ServeHTTP(w, r)
					return
				}
				// Support CIDR notation (e.g. 192.168.1.0/24)
				if strings.Contains(allowed, "/") {
					_, network, err := net.ParseCIDR(allowed)
					if err == nil && network.Contains(net.ParseIP(remoteIP)) {
						next.ServeHTTP(w, r)
						return
					}
				}
			}

			jsonError(w, "Forbidden: IP not in whitelist", http.StatusForbidden)
		})
	}
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
