package admin

import (
	"net/http"

	"github.com/debridnest/debridnest/internal/auth"
)

func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.auth != nil {
			user, ok := h.auth.ValidateToken(r.Context(), r.Header.Get("Authorization"))
			if !ok {
				writeError(w, http.StatusUnauthorized, "bad_token")
				return
			}
			ctx := auth.WithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		token := r.Header.Get("Authorization")
		if len(token) < 8 || token[:7] != "Bearer " || token[7:] != h.cfg.APIToken {
			writeError(w, http.StatusUnauthorized, "bad_token")
			return
		}
		ctx := auth.WithUser(r.Context(), auth.User{ID: "legacy", Name: "owner", Role: "admin", Admin: true})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func adminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok || !user.Admin {
			writeError(w, http.StatusForbidden, "admin required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
