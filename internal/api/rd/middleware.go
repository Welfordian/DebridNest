package rd

import (
	"net/http"
	"strings"

	"github.com/debridnest/debridnest/internal/auth"
)

func AuthMiddleware(authSvc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if strings.HasPrefix(path, "/rest/1.0/dl/") || strings.HasPrefix(path, "/dl/") || strings.HasPrefix(path, "/d/") {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := authSvc.ValidateToken(r.Context(), r.Header.Get("Authorization")); !ok {
				writeError(w, http.StatusUnauthorized, "bad_token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func corsMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
