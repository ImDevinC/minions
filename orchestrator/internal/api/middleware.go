package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// AuthMiddleware returns middleware that validates the Authorization header
// against the provided token. Uses constant-time comparison to prevent timing attacks.
//
// For WebSocket upgrade requests (which can't set custom headers from browsers),
// also checks for token in the 'token' query parameter.
func AuthMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			expected := "Bearer " + token

			// Try header auth first
			if subtle.ConstantTimeCompare([]byte(authHeader), []byte(expected)) == 1 {
				next.ServeHTTP(w, r)
				return
			}

			// For WebSocket upgrades, check query param (browsers can't set WS headers)
			if isWebSocketUpgrade(r) {
				queryToken := r.URL.Query().Get("token")
				if queryToken != "" && subtle.ConstantTimeCompare([]byte(queryToken), []byte(token)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		})
	}
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade request.
func isWebSocketUpgrade(r *http.Request) bool {
	connection := r.Header.Get("Connection")
	upgrade := r.Header.Get("Upgrade")
	return strings.ToLower(connection) == "upgrade" && strings.ToLower(upgrade) == "websocket"
}
