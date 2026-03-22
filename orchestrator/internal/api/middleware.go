package api

import (
	"crypto/subtle"
	"net/http"
)

// AuthMiddleware returns middleware that validates the Authorization header
// against the provided token. Uses constant-time comparison to prevent timing attacks.
func AuthMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			expected := "Bearer " + token

			if subtle.ConstantTimeCompare([]byte(authHeader), []byte(expected)) != 1 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
